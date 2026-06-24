package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	appplans "relay/internal/app/plans"
)

// ----------------------------------------------------------------------------
// Tool schemas -- orchestrator work packet retrieval tools.
// ----------------------------------------------------------------------------

var getNextPassWorkSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    }
  }
}`)

var getNextAuditWorkSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    },
    "pass_id": {
      "type": "string",
      "minLength": 1,
      "description": "Optional Relay pass identifier to scope the audit work selection."
    },
    "run_id": {
      "type": "string",
      "minLength": 1,
      "description": "Optional Relay run identifier to select a specific run for audit."
    }
  }
}`)

// ----------------------------------------------------------------------------
// Tool definitions.
// ----------------------------------------------------------------------------

var ToolGetNextPassWork = ToolDefinition{
	Name:        appplans.NextPassWorkTool,
	Description: "Return the next eligible project-scoped plan pass work packet for Planner handoff creation. Retrieval-only: does not create runs, submit plans, generate handoffs, create context packets, mutate git, run shell commands, or expose arbitrary filesystem access.",
	InputSchema: getNextPassWorkSchema,
}

var ToolGetNextAuditWork = ToolDefinition{
	Name:        appplans.NextAuditWorkTool,
	Description: "Return the next audit-ready project-scoped work packet for an Auditor agent. Retrieval-only: does not generate audit judgments, apply audit decisions, create runs, mutate git, run shell commands, or expose arbitrary filesystem access.",
	InputSchema: getNextAuditWorkSchema,
}

// ----------------------------------------------------------------------------
// Argument structs.
// ----------------------------------------------------------------------------

type getNextPassWorkArgs struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
}

type getNextAuditWorkArgs struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
	PassID    string `json:"pass_id"`
	RunID     string `json:"run_id"`
}

// ----------------------------------------------------------------------------
// Helpers -- top-level orchestrator work tool payload marshaling.
// ----------------------------------------------------------------------------

// orchestratorWorkToolPayload marshals the service response as top-level JSON
// text content without a broker-style wrapper.
func orchestratorWorkToolPayload(payload interface{}, isError bool) ToolCallResult {
	data, err := json.Marshal(payload)
	if err != nil {
		return ToolCallResult{
			IsError: true,
			Content: []ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf(`{"ok":false,"error":{"code":"INTERNAL_ERROR","message":"failed to marshal response: %v"}}`, err),
			}},
		}
	}
	return ToolCallResult{
		IsError: isError,
		Content: []ContentBlock{{
			Type: "text",
			Text: string(data),
		}},
	}
}

// orchestratorWorkToolErr builds a top-level error payload shaped as a work packet blocker response.
func orchestratorWorkToolErr(toolName string, code string, message string) ToolCallResult {
	payload := map[string]interface{}{
		"ok":   false,
		"tool": toolName,
		"blockers": []map[string]interface{}{
			{
				"code":        code,
				"message":     message,
				"recoverable": false,
			},
		},
	}
	return orchestratorWorkToolPayload(payload, true)
}

// ----------------------------------------------------------------------------
// Handlers.
// ----------------------------------------------------------------------------

// HandleGetNextPassWork retrieves the next eligible Planner work packet
// for a project-scoped managed plan.
func (s *Server) HandleGetNextPassWork(rawArgs json.RawMessage) ToolCallResult {
	var args getNextPassWorkArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return orchestratorWorkToolErr(appplans.NextPassWorkTool, appplans.BlockerUnsafeRequest, "invalid params: "+err.Error())
	}

	if s.deps == nil || s.deps.Store == nil {
		return orchestratorWorkToolErr(appplans.NextPassWorkTool, appplans.BlockerUnsafeRequest, "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	svc := appplans.NewOrchestratorWorkService(s.deps.Store)
	req := appplans.NextPassWorkRequest{
		ProjectID: args.ProjectID,
		PlanID:    args.PlanID,
	}

	resp, err := svc.GetNextPassWork(context.Background(), req)
	if err != nil {
		return orchestratorWorkToolErr(appplans.NextPassWorkTool, appplans.BlockerUnsafeRequest, fmt.Sprintf("service error: %v", err))
	}

	return orchestratorWorkToolPayload(resp, false)
}

// HandleGetNextAuditWork retrieves the next eligible audit work packet
// for a project-scoped managed plan.
func (s *Server) HandleGetNextAuditWork(rawArgs json.RawMessage) ToolCallResult {
	var args getNextAuditWorkArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return orchestratorWorkToolErr(appplans.NextAuditWorkTool, appplans.BlockerUnsafeRequest, "invalid params: "+err.Error())
	}

	if s.deps == nil || s.deps.Store == nil {
		return orchestratorWorkToolErr(appplans.NextAuditWorkTool, appplans.BlockerUnsafeRequest, "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	svc := appplans.NewOrchestratorWorkService(s.deps.Store)
	req := appplans.NextAuditWorkRequest{
		ProjectID: args.ProjectID,
		PlanID:    args.PlanID,
		PassID:    args.PassID,
		RunID:     args.RunID,
	}

	resp, err := svc.GetNextAuditWork(context.Background(), req)
	if err != nil {
		return orchestratorWorkToolErr(appplans.NextAuditWorkTool, appplans.BlockerUnsafeRequest, fmt.Sprintf("service error: %v", err))
	}

	return orchestratorWorkToolPayload(resp, false)
}
