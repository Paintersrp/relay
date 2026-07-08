package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	workflowsubmissions "relay/internal/app/submissions"
	workflowplans "relay/internal/app/plans/workflow"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowstore "relay/internal/store/workflow"
)

var listCanonicalProjectsSchema = json.RawMessage(`{
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
	InputSchema: listCanonicalProjectsSchema,
}

type listCanonicalProjectsArgs struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type projectMetadata struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

type canonicalProjectsOutput struct {
	OK       bool              `json:"ok"`
	Tool     string            `json:"tool"`
	Projects []projectMetadata `json:"projects"`
	Count    int               `json:"count"`
}

func (s *Server) canonicalWorkflowService() (*workflowsubmissions.Service, error) {
	if s.workflowStore() == nil {
		return nil, errors.New("MCP server is not connected to a workflow store")
	}
	return workflowsubmissions.NewService(s.workflowStore())
}

func (s *Server) HandleListCanonicalProjects(rawArgs json.RawMessage) ToolCallResult {
	if s.workflowStore() == nil {
		return canonicalBlocked("list_projects", MCPBlockerToolUnavailable, "MCP server is not connected to a workflow store.", false, "workflow_store", nil)
	}
	var input listCanonicalProjectsArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return canonicalBlocked("list_projects", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "list_projects", nil)
	}
	service, err := workflowprojects.NewService(s.workflowStore())
	if err != nil {
		return canonicalBlocked("list_projects", MCPBlockerToolUnavailable, "workflow Project service is unavailable", false, "workflow_store", nil)
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
	return canonicalOK(canonicalProjectsOutput{
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
			return canonicalBlocked(tool, MCPBlockerUnknownResource, "referenced Plan was not found", true, "plan_id", emptyMetadata(metadata))
		case errors.Is(err, workflowprojects.ErrInvalidProjectRequest):
			return canonicalBlocked(tool, MCPBlockerSchemaMismatch, err.Error(), true, "request", emptyMetadata(metadata))
		case errors.Is(err, sql.ErrNoRows):
			return canonicalBlocked(tool, MCPBlockerUnknownResource, "referenced Project or Project child was not found", true, "association", emptyMetadata(metadata))
		default:
			return canonicalBlocked(tool, canonicalBlockerPersistenceFailed, "workflow persistence failed", false, "workflow_store", emptyMetadata(metadata))
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
		return canonicalBlocked(tool, canonicalBlockerCompilerRejected, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorInvalidExpectedHash:
		return canonicalBlocked(tool, MCPBlockerSchemaMismatch, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorExpectedHashMismatch:
		return canonicalBlocked(tool, MCPBlockerExpectedHashMismatch, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorInvalidArtifactKind:
		return canonicalBlocked(tool, canonicalBlockerArtifactKind, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorProjectNotFound:
		return canonicalBlocked(tool, MCPBlockerUnknownResource, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorUnknownResource:
		return canonicalBlocked(tool, MCPBlockerUnknownResource, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorProjectArchived:
		return canonicalBlocked(tool, "project_archived", applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorRepositoryNotFound:
		return canonicalBlocked(tool, MCPBlockerUnknownRepository, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorPlanPassAssociation,
		workflowsubmissions.ErrorSelectedPassFilename,
		workflowsubmissions.ErrorRemediationAssociation:
		return canonicalBlocked(tool, canonicalBlockerAssociationInvalid, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	case workflowsubmissions.ErrorPersistence:
		return canonicalBlocked(tool, canonicalBlockerPersistenceFailed, applicationError.Message, applicationError.Recoverable, ref, emptyMetadata(metadata))
	default:
		return canonicalBlocked(tool, canonicalBlockerPersistenceFailed, "workflow persistence failed", false, "workflow_store", emptyMetadata(metadata))
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
