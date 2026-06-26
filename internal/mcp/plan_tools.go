package mcp

import (
	"context"
	"encoding/json"
	"strings"

	appplans "relay/internal/app/plans"
)

var submitPlannerPassPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["planner_pass_plan_json", "unmanaged_acknowledged"],
  "properties": {
    "planner_pass_plan_json": {
      "type": "string",
      "minLength": 1,
      "description": "The reviewed Planner pass plan JSON content. Do not pass chat context or a Planner handoff Markdown artifact."
    },
    "source_artifact_path": {
      "type": "string",
      "description": "Optional repo-relative path to the reviewed .planner-pass-plan.json source artifact."
    },
    "source": {
      "type": "string",
      "description": "Optional caller/source label for audit context."
    },
    "unmanaged_acknowledged": {
      "type": "boolean",
      "description": "Must be true to acknowledge direct unmanaged plan submission outside the file-based plan-attempt approval flow."
    }
  }
}`)

type submitPlannerPassPlanArgs struct {
	PlannerPassPlanJSON   string `json:"planner_pass_plan_json"`
	SourceArtifactPath    string `json:"source_artifact_path,omitempty"`
	Source                string `json:"source,omitempty"`
	UnmanagedAcknowledged bool   `json:"unmanaged_acknowledged"`
}

type submitPlannerPassPlanOutput struct {
	OK                 bool                          `json:"ok"`
	Tool               string                        `json:"tool"`
	PlanID             string                        `json:"plan_id"`
	PlanRowID          int64                         `json:"plan_row_id"`
	Status             string                        `json:"status"`
	PassCount          int                           `json:"pass_count"`
	Passes             []submitPlannerPassOutput     `json:"passes"`
	Validation         appplans.PlanValidationReport `json:"validation"`
	SourceArtifactPath string                        `json:"source_artifact_path,omitempty"`
}

type submitPlannerPassOutput struct {
	PassID   string `json:"pass_id"`
	RowID    int64  `json:"row_id"`
	Sequence int64  `json:"sequence"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

type submitPlannerPassPlanErrorOutput struct {
	OK         bool                           `json:"ok"`
	Tool       string                         `json:"tool"`
	Error      string                         `json:"error"`
	Message    string                         `json:"message"`
	Validation *appplans.PlanValidationReport `json:"validation,omitempty"`
}

// ToolSubmitPlannerPassPlan is the ToolDefinition for submit_planner_pass_plan.
var ToolSubmitPlannerPassPlan = ToolDefinition{
	Name: "submit_planner_pass_plan",
	Description: "Submit a reviewed Planner pass plan JSON artifact to Relay. " +
		"This creates Relay plan/pass records only after explicit user confirmation outside the tool. " +
		"It does not create runs, attach runs to passes, dispatch executors, mutate git, or read chat context.",
	InputSchema: submitPlannerPassPlanSchema,
}

// HandleSubmitPlannerPassPlan implements the submit_planner_pass_plan MCP tool.
func (s *Server) HandleSubmitPlannerPassPlan(args json.RawMessage) ToolCallResult {
	var in submitPlannerPassPlanArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return submitPlannerPassPlanToolErr("VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	trimmedPlanJSON := strings.TrimSpace(in.PlannerPassPlanJSON)
	if trimmedPlanJSON == "" {
		return submitPlannerPassPlanToolErr("VALIDATION_ERROR", "planner_pass_plan_json is required and must not be empty", nil)
	}

	if s.deps == nil || s.deps.Store == nil {
		return submitPlannerPassPlanToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set", nil)
	}

	svc := appplans.NewService(s.deps.Store)
	result, err := svc.SubmitPlan(context.Background(), appplans.SubmitPlanRequest{
		RawJSON:               []byte(trimmedPlanJSON),
		SourceArtifactPath:    strings.TrimSpace(in.SourceArtifactPath),
		UnmanagedAcknowledged: in.UnmanagedAcknowledged,
	})
	if err != nil {
		var report *appplans.PlanValidationReport
		if result != nil {
			report = &result.Report
		}
		return submitPlannerPassPlanToolErr("PLAN_SUBMISSION_ERROR", err.Error(), report)
	}
	if result == nil {
		return submitPlannerPassPlanToolErr("PLAN_SUBMISSION_ERROR", "plan submission returned no result", nil)
	}
	if !result.Report.Valid {
		return submitPlannerPassPlanToolErr("PLAN_VALIDATION_FAILED", "planner pass plan validation failed", &result.Report)
	}

	out := submitPlannerPassPlanOutput{
		OK:                 true,
		Tool:               "submit_planner_pass_plan",
		PlanID:             result.Plan.PlanID,
		PlanRowID:          result.Plan.ID,
		Status:             result.Plan.Status,
		PassCount:          len(result.Passes),
		Passes:             make([]submitPlannerPassOutput, 0, len(result.Passes)),
		Validation:         result.Report,
		SourceArtifactPath: strings.TrimSpace(in.SourceArtifactPath),
	}
	for _, pass := range result.Passes {
		out.Passes = append(out.Passes, submitPlannerPassOutput{
			PassID:   pass.PassID,
			RowID:    pass.ID,
			Sequence: pass.Sequence,
			Name:     pass.Name,
			Status:   pass.Status,
		})
	}

	text, err := marshalTool(out)
	if err != nil {
		return submitPlannerPassPlanToolErr("PLAN_SUBMISSION_ERROR", err.Error(), nil)
	}
	return toolOK(text)
}

func submitPlannerPassPlanToolErr(code, message string, validation *appplans.PlanValidationReport) ToolCallResult {
	text, err := marshalTool(submitPlannerPassPlanErrorOutput{
		OK:         false,
		Tool:       "submit_planner_pass_plan",
		Error:      code,
		Message:    message,
		Validation: validation,
	})
	if err != nil {
		return toolErr(`{"ok":false,"tool":"submit_planner_pass_plan","error":"PLAN_SUBMISSION_ERROR","message":"failed to marshal tool error"}`)
	}
	return toolErr(text)
}
