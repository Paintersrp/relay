package mcp

import (
	"context"
	"encoding/json"

	appplans "relay/internal/app/plans"
)

const (
	toolCreatePlanAttemptWithIntent = "create_plan_attempt_with_intent"
	toolGetPlanIntentReviewPacket   = "get_plan_intent_review_packet"
	toolSubmitIntentDriftReview     = "submit_intent_drift_review"
	toolRevisePlanAttempt           = "revise_plan_attempt"
	toolVoidPlanAttempt             = "void_plan_attempt"
	toolApprovePlanAttempt          = "approve_plan_attempt"
	toolSubmitPlanAttempt           = "submit_plan_attempt"
)

var (
	planAttemptCreateSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_artifact_ref", "raw_plan_json", "intent_packet"],
  "properties": {
    "project_id": {"type": "string", "minLength": 1},
    "plan_attempt_id": {"type": "string"},
    "intent_packet_id": {"type": "string"},
    "intent_thread_id": {"type": "string"},
    "plan_artifact_ref": {"$ref": "#/$defs/artifact_ref"},
    "optional_markdown_ref": {"$ref": "#/$defs/artifact_ref"},
    "raw_plan_json": {"type": "object"},
    "drift_review_mode": {"type": "string", "enum": ["disabled", "manual", "automatic", "external"]},
    "model_tier": {"type": "string", "enum": ["economy", "standard", "high_assurance", "auto_escalate"]},
    "intent_packet": {"$ref": "#/$defs/intent_packet"}
  },
  "$defs": {
    "artifact_ref": {
      "type": "object",
      "additionalProperties": false,
      "required": ["path", "sha256", "artifact_kind"],
      "properties": {
        "path": {"type": "string"},
        "sha256": {"type": "string"},
        "artifact_kind": {"type": "string"}
      }
    },
    "intent_packet": {
      "type": "object",
      "additionalProperties": false,
      "required": ["summary", "literal_user_request", "constraints", "source"],
      "properties": {
        "summary": {"type": "string"},
        "literal_user_request": {"type": "string"},
        "constraints": {"type": "array", "items": {"type": "string"}},
        "source": {
          "type": "object",
          "additionalProperties": false,
          "required": ["captured_from", "captured_by", "source_artifact_path"],
          "properties": {
            "captured_from": {"type": "string"},
            "captured_by": {"type": "string"},
            "source_artifact_path": {"type": "string"}
          }
        },
        "redaction_status": {"type": "string"},
        "content_hash": {"type": "string"}
      }
    }
  }
}`)
	planAttemptByIDSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_attempt_id"],
  "properties": {
    "project_id": {"type": "string", "minLength": 1},
    "plan_attempt_id": {"type": "string", "minLength": 1}
  }
}`)
	driftReviewSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_attempt_id", "drift_review"],
  "properties": {
    "project_id": {"type": "string", "minLength": 1},
    "plan_attempt_id": {"type": "string", "minLength": 1},
    "drift_review": {
      "type": "object",
      "additionalProperties": false,
      "required": ["plan_attempt_id", "intent_thread_id", "root_intent_packet_id", "reviewed_intent_packet_id", "review_packet_hash", "review_source", "overall_alignment", "confidence", "findings_json", "recommended_action", "approval_gate_status", "input_hash", "output_hash"],
      "properties": {
        "intent_drift_review_id": {"type": "string"},
        "plan_attempt_id": {"type": "string"},
        "intent_thread_id": {"type": "string"},
        "root_intent_packet_id": {"type": "string"},
        "reviewed_intent_packet_id": {"type": "string"},
        "review_packet_hash": {"type": "string"},
        "review_source": {"type": "string", "enum": ["external", "internal"]},
        "submitted_by": {"type": "string"},
        "source_artifact_path": {"type": "string"},
        "overall_alignment": {"type": "string"},
        "confidence": {"type": "number"},
        "findings_json": {"type": ["object", "array"]},
        "recommended_action": {"type": "string"},
        "approval_gate_status": {"type": "string"},
        "model_metadata_json": {"type": "object"},
        "input_hash": {"type": "string"},
        "output_hash": {"type": "string"}
      }
    }
  }
}`)
	planAttemptReviseSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_attempt_id", "plan_artifact_ref", "raw_plan_json", "new_intent_packet"],
  "properties": {
    "project_id": {"type": "string", "minLength": 1},
    "plan_attempt_id": {"type": "string", "minLength": 1},
    "new_plan_attempt_id": {"type": "string"},
    "new_intent_packet_id": {"type": "string"},
    "plan_artifact_ref": {"type": "object"},
    "optional_markdown_ref": {"type": "object"},
    "raw_plan_json": {"type": "object"},
    "new_intent_packet": {"type": "object"}
  }
}`)
	planAttemptApproveSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_attempt_id", "approved"],
  "properties": {
    "project_id": {"type": "string", "minLength": 1},
    "plan_attempt_id": {"type": "string", "minLength": 1},
    "approved": {"type": "boolean"},
    "accepted_drift_review_id": {"type": "string"},
    "drift_acknowledged": {"type": "boolean"},
    "no_drift_review_acknowledged": {"type": "boolean"}
  }
}`)
)

var (
	ToolCreatePlanAttemptWithIntent = ToolDefinition{
		Name:        toolCreatePlanAttemptWithIntent,
		Description: "Create a draft plan attempt and bounded intent packet. Does not create managed plans, runs, model calls, git changes, or executor dispatch.",
		InputSchema: planAttemptCreateSchema,
	}
	ToolGetPlanIntentReviewPacket = ToolDefinition{
		Name:        toolGetPlanIntentReviewPacket,
		Description: "Retrieve the bounded plan intent review packet for one draft attempt. Retrieval-only: no model call, state mutation, approval, submission, run creation, or file reads.",
		InputSchema: planAttemptByIDSchema,
	}
	ToolSubmitIntentDriftReview = ToolDefinition{
		Name:        toolSubmitIntentDriftReview,
		Description: "Persist structured intent drift review evidence for one plan attempt. Does not approve, submit, create runs, dispatch executors, call models, or mutate git.",
		InputSchema: driftReviewSchema,
	}
	ToolRevisePlanAttempt = ToolDefinition{
		Name:        toolRevisePlanAttempt,
		Description: "Supersede a draft plan attempt with a replacement draft attempt and revision intent packet. Does not submit or dispatch work.",
		InputSchema: planAttemptReviseSchema,
	}
	ToolVoidPlanAttempt = ToolDefinition{
		Name:        toolVoidPlanAttempt,
		Description: "Void a draft plan attempt. Does not create replacement attempts, managed plans, runs, or git changes.",
		InputSchema: planAttemptByIDSchema,
	}
	ToolApprovePlanAttempt = ToolDefinition{
		Name:        toolApprovePlanAttempt,
		Description: "Approve a draft plan attempt after app-layer review gates pass. Does not submit the managed plan.",
		InputSchema: planAttemptApproveSchema,
	}
	ToolSubmitPlanAttempt = ToolDefinition{
		Name:        toolSubmitPlanAttempt,
		Description: "Submit an approved plan attempt into managed plan/pass records using the stored reviewed Plan JSON. Does not create runs or dispatch executors.",
		InputSchema: planAttemptByIDSchema,
	}
)

func planAttemptToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		ToolCreatePlanAttemptWithIntent,
		ToolGetPlanIntentReviewPacket,
		ToolSubmitIntentDriftReview,
		ToolRevisePlanAttempt,
		ToolVoidPlanAttempt,
		ToolApprovePlanAttempt,
		ToolSubmitPlanAttempt,
	}
}

type planAttemptToolOutput struct {
	OK           bool                             `json:"ok"`
	Tool         string                           `json:"tool"`
	Status       string                           `json:"status,omitempty"`
	BlockerCode  string                           `json:"blocker_code,omitempty"`
	Message      string                           `json:"message,omitempty"`
	IntentPacket any                              `json:"intent_packet,omitempty"`
	PlanAttempt  any                              `json:"plan_attempt,omitempty"`
	DriftReview  any                              `json:"drift_review,omitempty"`
	Plan         any                              `json:"plan,omitempty"`
	Passes       any                              `json:"passes,omitempty"`
	ReviewPacket *appplans.PlanIntentReviewPacket `json:"review_packet,omitempty"`
}

func (s *Server) HandleCreatePlanAttemptWithIntent(args json.RawMessage) ToolCallResult {
	var req appplans.CreatePlanAttemptWithIntentRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return planAttemptToolErr(toolCreatePlanAttemptWithIntent, "validation_error", err.Error())
	}
	return s.handlePlanAttemptResult(toolCreatePlanAttemptWithIntent, func(svc *appplans.Service) (*appplans.PlanAttemptResult, error) {
		return svc.CreatePlanAttemptWithIntent(context.Background(), req)
	})
}

func (s *Server) HandleGetPlanIntentReviewPacket(args json.RawMessage) ToolCallResult {
	var req appplans.GetPlanIntentReviewPacketRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return planAttemptToolErr(toolGetPlanIntentReviewPacket, "validation_error", err.Error())
	}
	return s.handlePlanAttemptResult(toolGetPlanIntentReviewPacket, func(svc *appplans.Service) (*appplans.PlanAttemptResult, error) {
		return svc.GetPlanIntentReviewPacket(context.Background(), req)
	})
}

func (s *Server) HandleSubmitIntentDriftReview(args json.RawMessage) ToolCallResult {
	var req appplans.SubmitIntentDriftReviewRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return planAttemptToolErr(toolSubmitIntentDriftReview, "validation_error", err.Error())
	}
	return s.handlePlanAttemptResult(toolSubmitIntentDriftReview, func(svc *appplans.Service) (*appplans.PlanAttemptResult, error) {
		return svc.SubmitIntentDriftReview(context.Background(), req)
	})
}

func (s *Server) HandleRevisePlanAttempt(args json.RawMessage) ToolCallResult {
	var req appplans.RevisePlanAttemptRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return planAttemptToolErr(toolRevisePlanAttempt, "validation_error", err.Error())
	}
	return s.handlePlanAttemptResult(toolRevisePlanAttempt, func(svc *appplans.Service) (*appplans.PlanAttemptResult, error) {
		return svc.RevisePlanAttempt(context.Background(), req)
	})
}

func (s *Server) HandleVoidPlanAttempt(args json.RawMessage) ToolCallResult {
	var req appplans.VoidPlanAttemptRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return planAttemptToolErr(toolVoidPlanAttempt, "validation_error", err.Error())
	}
	return s.handlePlanAttemptResult(toolVoidPlanAttempt, func(svc *appplans.Service) (*appplans.PlanAttemptResult, error) {
		return svc.VoidPlanAttempt(context.Background(), req)
	})
}

func (s *Server) HandleApprovePlanAttempt(args json.RawMessage) ToolCallResult {
	var req appplans.ApprovePlanAttemptRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return planAttemptToolErr(toolApprovePlanAttempt, "validation_error", err.Error())
	}
	return s.handlePlanAttemptResult(toolApprovePlanAttempt, func(svc *appplans.Service) (*appplans.PlanAttemptResult, error) {
		return svc.ApprovePlanAttempt(context.Background(), req)
	})
}

func (s *Server) HandleSubmitPlanAttempt(args json.RawMessage) ToolCallResult {
	var req appplans.SubmitPlanAttemptRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return planAttemptToolErr(toolSubmitPlanAttempt, "validation_error", err.Error())
	}
	return s.handlePlanAttemptResult(toolSubmitPlanAttempt, func(svc *appplans.Service) (*appplans.PlanAttemptResult, error) {
		return svc.SubmitPlanAttempt(context.Background(), req)
	})
}

func (s *Server) handlePlanAttemptResult(tool string, fn func(*appplans.Service) (*appplans.PlanAttemptResult, error)) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return planAttemptToolErr(tool, "dependency_error", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, err := fn(appplans.NewService(s.deps.Store))
	if err != nil {
		return planAttemptToolErr(tool, "internal_error", err.Error())
	}
	if result == nil {
		return planAttemptToolErr(tool, "internal_error", "plan attempt action returned no result")
	}
	out := planAttemptOutput(tool, result)
	text, err := marshalTool(out)
	if err != nil {
		return planAttemptToolErr(tool, "internal_error", err.Error())
	}
	if !result.OK {
		return toolErr(text)
	}
	return toolOK(text)
}

func planAttemptOutput(tool string, result *appplans.PlanAttemptResult) planAttemptToolOutput {
	out := planAttemptToolOutput{
		OK:           result.OK,
		Tool:         tool,
		BlockerCode:  string(result.BlockerCode),
		Message:      result.Message,
		IntentPacket: result.IntentPacket,
		PlanAttempt:  result.PlanAttempt,
		DriftReview:  result.DriftReview,
		Plan:         result.Plan,
		Passes:       result.Passes,
		ReviewPacket: result.ReviewPacket,
	}
	if result.OK {
		out.Status = "ok"
	} else {
		out.Status = "blocked"
	}
	return out
}

func planAttemptToolErr(tool, code, message string) ToolCallResult {
	text, err := marshalTool(planAttemptToolOutput{
		OK:          false,
		Tool:        tool,
		Status:      "blocked",
		BlockerCode: code,
		Message:     message,
	})
	if err != nil {
		return toolErr(`{"ok":false,"status":"blocked","blocker_code":"internal_error","message":"failed to marshal plan attempt tool error"}`)
	}
	return toolErr(text)
}
