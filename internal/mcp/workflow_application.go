package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	workflowplans "relay/internal/app/plans/workflow"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowsubmissions "relay/internal/app/submissions"
	workflowstore "relay/internal/store/workflow"
)

var listProjectsSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "status": {"type": "string", "enum": ["active", "archived"]},
    "limit": {"type": "integer", "minimum": 1, "maximum": 100}
  }
}`)

var ToolListProjects = ToolDefinition{
	Name:        "list_projects",
	Description: "List bounded Relay Projects so the Planner can select the required external Project association before Plan submission.",
	InputSchema: listProjectsSchema,
}

type listProjectsArgs struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type projectMetadata struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

type projectsOutput struct {
	OK       bool              `json:"ok"`
	Tool     string            `json:"tool"`
	Projects []projectMetadata `json:"projects"`
	Count    int               `json:"count"`
}

func (s *Server) submissionService() (*workflowsubmissions.Service, error) {
	if s.workflowStore() == nil {
		return nil, errors.New("MCP server is not connected to a workflow store")
	}
	return workflowsubmissions.NewService(s.workflowStore())
}

func (s *Server) HandleListProjects(rawArgs json.RawMessage) ToolCallResult {
	if s.workflowStore() == nil {
		return workflowBlocked("list_projects", MCPBlockerToolUnavailable, "MCP server is not connected to a workflow store.", false, "workflow_store", nil)
	}
	var input listProjectsArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return workflowBlocked("list_projects", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "list_projects", nil)
	}
	service, err := workflowprojects.NewService(s.workflowStore())
	if err != nil {
		return workflowBlocked("list_projects", MCPBlockerToolUnavailable, "workflow Project service is unavailable", false, "workflow_store", nil)
	}
	values, err := service.ListProjects(context.Background(), workflowprojects.ListProjectsInput{
		Status: strings.TrimSpace(input.Status),
		Limit:  input.Limit,
	})
	if err != nil {
		return submissionApplicationBlocked("list_projects", err, nil)
	}
	projects := make([]projectMetadata, 0, len(values))
	for _, value := range values {
		projects = append(projects, projectOut(value))
	}
	return workflowOK(projectsOutput{
		OK:       true,
		Tool:     "list_projects",
		Projects: projects,
		Count:    len(projects),
	})
}

func submissionApplicationBlocked(tool string, err error, provenance any) ToolCallResult {
	metadata := map[string]any{}
	if provenance != nil {
		metadata["provenance"] = provenance
	}

	applicationError, ok := workflowsubmissions.AsApplicationError(err)
	if !ok {
		switch {
		case errors.Is(err, workflowplans.ErrPlanNotFound):
			return workflowBlocked(tool, MCPBlockerUnknownResource, "referenced Plan was not found", true, "plan_id", emptyMetadata(metadata))
		case errors.Is(err, workflowprojects.ErrInvalidProjectRequest):
			return workflowBlocked(tool, MCPBlockerSchemaMismatch, err.Error(), true, "request", emptyMetadata(metadata))
		case errors.Is(err, sql.ErrNoRows):
			return workflowBlocked(tool, MCPBlockerUnknownResource, "referenced Project or Project child was not found", true, "association", emptyMetadata(metadata))
		default:
			return workflowBlocked(tool, submissionBlockerPersistenceFailed, "workflow persistence failed", false, "workflow_store", emptyMetadata(metadata))
		}
	}

	if len(applicationError.Diagnostics) != 0 {
		metadata["diagnostics"] = applicationError.Diagnostics
	}
	if len(applicationError.Notices) != 0 {
		metadata["notices"] = applicationError.Notices
	}
	ref := applicationError.Ref
	if ref == "" {
		ref = "request"
	}

	switch applicationError.Code {
	case workflowsubmissions.ErrorCompilerRejected:
		return workflowBlocked(tool, submissionBlockerCompilerRejected, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorInvalidExpectedHash:
		return workflowBlocked(tool, MCPBlockerSchemaMismatch, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorExpectedHashMismatch:
		return workflowBlocked(tool, MCPBlockerExpectedHashMismatch, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorInvalidArtifactKind:
		return workflowBlocked(tool, submissionBlockerArtifactKind, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorProjectNotFound:
		return workflowBlocked(tool, MCPBlockerUnknownResource, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorUnknownResource:
		return workflowBlocked(tool, MCPBlockerUnknownResource, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorProjectArchived:
		return workflowBlocked(tool, "project_archived", applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorRepositoryNotFound:
		return workflowBlocked(tool, MCPBlockerUnknownRepository, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorPlanPassAssociation,
		workflowsubmissions.ErrorSelectedPassFilename,
		workflowsubmissions.ErrorRemediationAssociation:
		return workflowBlocked(tool, submissionBlockerAssociationInvalid, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorPersistence:
		return workflowBlocked(tool, submissionBlockerPersistenceFailed, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	default:
		return workflowBlocked(tool, submissionBlockerPersistenceFailed, "workflow persistence failed", false, "workflow_store", emptyMetadata(metadata))
	}
}

func emptyMetadata(metadata map[string]any) any {
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func projectOut(value workflowstore.Project) projectMetadata {
	return projectMetadata{
		ProjectID: value.ProjectID,
		Name:      value.Name,
		Status:    value.Status,
	}
}
