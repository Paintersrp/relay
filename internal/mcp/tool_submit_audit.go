package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"relay/internal/auditor"
)

// submitAuditInput is the expected input for submit_audit_packet.
//
// WARNING: Do NOT include secrets, tokens, auth headers, private keys, or signed URLs
// in audit_packet_markdown. Relay stores this content as a persistent artifact.
type submitAuditInput struct {
	// RunID is the numeric Relay run identifier. Required.
	RunID string `json:"run_id"`
	// AuditPacketMarkdown is the audit/review content from the current chat. Required.
	// The MCP client/LLM should pass this as an explicit argument derived from chat context.
	// Relay does not read chat messages directly.
	AuditPacketMarkdown string `json:"audit_packet_markdown"`
	// Decision is the audit outcome. Required.
	// Must be one of: accepted, accepted_with_warnings, revision_required, blocked, manual_review_required.
	Decision string `json:"decision"`
	// Notes is an optional human-readable note to attach to the run event.
	Notes string `json:"notes,omitempty"`
	// ClientTraceID is an optional opaque trace identifier from the calling client.
	ClientTraceID string `json:"client_trace_id,omitempty"`
}

// submitAuditOutput is the structured success payload for submit_audit_packet.
type submitAuditOutput struct {
	OK             bool   `json:"ok"`
	Tool           string `json:"tool"`
	RunID          string `json:"run_id"`
	Decision       string `json:"decision"`
	Status         string `json:"status"`
	LifecycleState string `json:"lifecycle_state"`
	ArtifactKind   string `json:"artifact_kind"`
	ReviewURL      string `json:"review_url"`
}

// supportedAuditDecisions are the valid values for the decision field.
var supportedAuditDecisions = map[string]bool{
	"accepted":               true,
	"accepted_with_warnings": true,
	"revision_required":      true,
	"blocked":                true,
	"manual_review_required": true,
}

// HandleSubmitAuditPacket implements the submit_audit_packet MCP tool.
//
// Safe state transitions applied:
//   - accepted               → accepted
//   - accepted_with_warnings → accepted_with_warnings
//   - revision_required      → revision_required
//   - blocked                → revision_required (with event noting blocked decision)
//   - manual_review_required → revision_required (with event noting manual-review decision)
//
// This tool does NOT close the run, commit, push, stage, merge, branch, checkout,
// reset, or mutate the target repository in any way.
func (s *Server) HandleSubmitAuditPacket(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return toolErr("DEPENDENCY_ERROR: MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	var input submitAuditInput
	if err := json.Unmarshal(rawArgs, &input); err != nil {
		return toolErr(fmt.Sprintf("invalid arguments: %s", err))
	}

	// Validate required fields.
	if input.RunID == "" {
		return toolErr("VALIDATION_ERROR: run_id is required and must not be empty")
	}
	if strings.TrimSpace(input.AuditPacketMarkdown) == "" {
		return toolErr("VALIDATION_ERROR: audit_packet_markdown is required and must not be empty")
	}
	if input.Decision == "" {
		return toolErr("VALIDATION_ERROR: decision is required; must be one of: accepted, accepted_with_warnings, revision_required, blocked, manual_review_required")
	}
	if !supportedAuditDecisions[input.Decision] {
		return toolErr(fmt.Sprintf("VALIDATION_ERROR: unsupported decision %q; must be one of: accepted, accepted_with_warnings, revision_required, blocked, manual_review_required", input.Decision))
	}

	runIDInt, err := strconv.ParseInt(input.RunID, 10, 64)
	if err != nil {
		return toolErr(fmt.Sprintf("VALIDATION_ERROR: run_id must be a numeric string, got %q", input.RunID))
	}

	submitResult, err := auditor.NewSubmissionService(s.deps.Store).SubmitDecision(auditor.DecisionSubmission{
		RunID:               runIDInt,
		Decision:            auditor.Decision(input.Decision),
		AuditPacketMarkdown: input.AuditPacketMarkdown,
		Notes:               input.Notes,
		Source:              "mcp",
		ClientTraceID:       input.ClientTraceID,
	})
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "get run:"):
			return toolErr(fmt.Sprintf("NOT_FOUND: %s", err))
		case errors.Is(err, auditor.ErrCompletedRun), errors.Is(err, auditor.ErrAuditDecisionNotReady):
			return toolErr(fmt.Sprintf("STATE_ERROR: %s", err))
		case errors.Is(err, auditor.ErrUnsupportedDecision), strings.Contains(err.Error(), "audit_packet_markdown is required"):
			return toolErr(fmt.Sprintf("VALIDATION_ERROR: %s", err))
		default:
			return toolErr(fmt.Sprintf("SUBMISSION_ERROR: %s", err))
		}
	}

	result := submitAuditOutput{
		OK:             true,
		Tool:           "submit_audit_packet",
		RunID:          input.RunID,
		Decision:       input.Decision,
		Status:         submitResult.Status,
		LifecycleState: submitResult.LifecycleState,
		ArtifactKind:   "audit_packet",
		ReviewURL:      fmt.Sprintf("/runs/%s/audit", input.RunID),
	}

	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

func auditDecisionToStatus(decision string) (status string, note string) {
	switch decision {
	case "accepted":
		return "accepted", ""
	case "accepted_with_warnings":
		return "accepted_with_warnings", ""
	case "revision_required":
		return "revision_required", ""
	case "blocked":
		return "revision_required", "decision preserved as blocked"
	case "manual_review_required":
		return "revision_required", "decision preserved as manual_review_required"
	default:
		return "revision_required", "decision preserved as revision_required"
	}
}

// submitAuditSchema is the JSON Schema for submit_audit_packet.
var submitAuditSchema = json.RawMessage(`{
  "type": "object",
  "required": ["run_id", "audit_packet_markdown", "decision"],
  "properties": {
    "run_id": {
      "type": "string",
      "description": "Numeric Relay run identifier (e.g., '42'). Obtain from list_open_runs or get_run_status."
    },
    "audit_packet_markdown": {
      "type": "string",
      "description": "Audit or review content from the current chat conversation. The MCP client/LLM must extract and pass this content explicitly — Relay does not read chat messages directly. WARNING: do not include secrets, tokens, auth headers, private keys, or signed URLs."
    },
    "decision": {
      "type": "string",
      "description": "Audit outcome decision.",
      "enum": ["accepted", "accepted_with_warnings", "revision_required", "blocked", "manual_review_required"]
    },
    "notes": {
      "type": "string",
      "description": "Optional human-readable note to attach to the run event."
    },
    "client_trace_id": {
      "type": "string",
      "description": "Optional opaque trace identifier from the calling MCP client."
    }
  }
}`)

// ToolSubmitAuditPacket is the ToolDefinition for submit_audit_packet.
var ToolSubmitAuditPacket = ToolDefinition{
	Name: "submit_audit_packet",
	Description: "Submit an audit or review result from the current chat back to an existing Relay run. " +
		"The MCP client/LLM must extract the audit content from the chat and pass it as the " +
		"audit_packet_markdown argument — Relay does not read chat messages directly. " +
		"Writes a bounded audit artifact, creates a run event, and applies a safe status transition. " +
		"Transitions: accepted→accepted, accepted_with_warnings→accepted_with_warnings, " +
		"revision_required→revision_required, blocked→revision_required, manual_review_required→revision_required. " +
		"Does NOT close the run. Does NOT commit, push, stage, merge, branch, checkout, reset, or mutate the target repository. " +
		"WARNING: do not include secrets, tokens, auth headers, private keys, or signed URLs in audit_packet_markdown.",
	InputSchema: submitAuditSchema,
}
