package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	appplans "relay/internal/app/plans"
)

var submitPlannerPassPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["unmanaged_acknowledged"],
  "oneOf": [
    {"required": ["planner_pass_plan"]},
    {"required": ["planner_pass_plan_json"]}
  ],
  "properties": {
    "planner_pass_plan": {
      "type": "object",
      "description": "Preferred reviewed Planner pass plan JSON object."
    },
    "planner_pass_plan_json": {
      "type": "string",
      "minLength": 1,
      "description": "Legacy raw JSON string for backward compatibility. Prefer planner_pass_plan for large plans."
    },
    "source_artifact_path": {
      "type": "string",
      "description": "Optional repo-relative durable artifact path; not a local filesystem path."
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
	PlannerPassPlan       json.RawMessage `json:"planner_pass_plan,omitempty"`
	PlannerPassPlanJSON   json.RawMessage `json:"planner_pass_plan_json,omitempty"`
	SourceArtifactPath    string          `json:"source_artifact_path,omitempty"`
	Source                string          `json:"source,omitempty"`
	UnmanagedAcknowledged bool            `json:"unmanaged_acknowledged"`
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

// normalizePlanPayload extracts raw plan JSON bytes from either the preferred
// object field or the legacy string field. It returns a structured error for
// invalid payload combinations or malformed input.
func normalizePlanPayload(args *submitPlannerPassPlanArgs) ([]byte, error) {
	hasObject := len(bytes.TrimSpace(args.PlannerPassPlan)) > 0 &&
		!bytes.Equal(bytes.TrimSpace(args.PlannerPassPlan), []byte("null"))
	hasString := len(bytes.TrimSpace(args.PlannerPassPlanJSON)) > 0 &&
		!bytes.Equal(bytes.TrimSpace(args.PlannerPassPlanJSON), []byte("null"))

	if hasObject && hasString {
		return nil, errors.New("must supply exactly one of planner_pass_plan or planner_pass_plan_json")
	}
	if !hasObject && !hasString {
		return nil, errors.New("must supply exactly one of planner_pass_plan or planner_pass_plan_json")
	}

	if hasObject {
		raw := bytes.TrimSpace(args.PlannerPassPlan)
		if len(raw) == 0 || raw[0] != '{' {
			return nil, errors.New("planner_pass_plan must be a JSON object")
		}
		return raw, nil
	}

	// Legacy string path: require a valid JSON string value.
	raw := bytes.TrimSpace(args.PlannerPassPlanJSON)
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errors.New("planner_pass_plan_json must be a valid JSON string")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("planner_pass_plan_json must not be empty")
	}
	if !json.Valid([]byte(s)) {
		return nil, errors.New("planner_pass_plan_json must contain valid JSON")
	}
	return []byte(s), nil
}

// HandleSubmitPlannerPassPlan implements the submit_planner_pass_plan MCP tool.
func (s *Server) HandleSubmitPlannerPassPlan(args json.RawMessage) ToolCallResult {
	var in submitPlannerPassPlanArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return submitPlannerPassPlanToolErr("VALIDATION_ERROR", "invalid params: "+err.Error(), nil)
	}

	rawJSON, err := normalizePlanPayload(&in)
	if err != nil {
		return submitPlannerPassPlanToolErr("VALIDATION_ERROR", err.Error(), nil)
	}

	if s.deps == nil || s.deps.Store == nil {
		return submitPlannerPassPlanToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set", nil)
	}

	svc := appplans.NewService(s.deps.Store)
	result, err := svc.SubmitPlan(context.Background(), appplans.SubmitPlanRequest{
		RawJSON:               rawJSON,
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
