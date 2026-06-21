package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"relay/internal/artifacts"
	"relay/internal/auditor"
	"relay/internal/plans"
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

// terminalStatuses are run statuses that should not receive new audit handbacks.
var terminalStatuses = map[string]bool{
	"completed": true,
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

	run, err := s.deps.Store.GetRun(runIDInt)
	if err != nil {
		return toolErr(fmt.Sprintf("NOT_FOUND: run %q not found: %s", input.RunID, err))
	}

	// Reject terminal runs.
	if terminalStatuses[run.Status] {
		return toolErr(fmt.Sprintf("STATE_ERROR: run %q is in terminal status %q and cannot accept an audit handback", input.RunID, run.Status))
	}

	// Write bounded audit handback artifact through Relay artifact conventions.
	// Reuse auditor.SubmissionService.SubmitManual to share artifact-write semantics.
	submissionSvc := auditor.NewSubmissionService(s.deps.Store)
	_, serr := submissionSvc.SubmitManual(auditor.ManualAuditSubmission{
		RunID:               runIDInt,
		AuditPacketMarkdown: input.AuditPacketMarkdown,
		Decision:            auditor.Decision(input.Decision),
		Notes:               input.Notes,
	})
	if serr != nil {
		return toolErr(fmt.Sprintf("SUBMISSION_ERROR: %s", serr))
	}

	// Write a secondary MCP-specific audit handback artifact for provenance.
	mcpHandbackContent := fmt.Sprintf(
		"# MCP Audit Handback\n\n"+
			"## Metadata\n\n"+
			"- Tool: submit_audit_packet\n"+
			"- Run ID: %s\n"+
			"- Decision: %s\n"+
			"- Submitted: %s\n"+
			"- Source: MCP chat handback (Pass 16)\n\n"+
			"## Notes\n\n%s\n\n"+
			"## Submitted Packet Content\n\n%s\n",
		input.RunID,
		input.Decision,
		time.Now().UTC().Format(time.RFC3339),
		input.Notes,
		input.AuditPacketMarkdown,
	)
	_, _ = artifacts.Write(runIDInt, "audit_packet", "mcp_audit_handback.md", []byte(mcpHandbackContent))

	// Safe status transition.
	targetStatus, eventNote := auditDecisionToStatus(input.Decision)
	updatedRun, serr := s.deps.Store.UpdateRunStatus(runIDInt, targetStatus)
	if serr != nil {
		return toolErr(fmt.Sprintf("STATUS_ERROR: failed to apply status transition to %q: %s", targetStatus, serr))
	}
	if serr := plans.NewRunLifecycleService(s.deps.Store).ApplyAuditDecision(updatedRun, targetStatus); serr != nil {
		return toolErr(fmt.Sprintf("STATUS_ERROR: failed to update associated pass status for %q: %s", targetStatus, serr))
	}

	// Create run event noting MCP audit handback.
	eventMsg := fmt.Sprintf("MCP audit handback: decision=%s → status=%s", input.Decision, targetStatus)
	if eventNote != "" {
		eventMsg += fmt.Sprintf(" (%s)", eventNote)
	}
	if input.Notes != "" {
		eventMsg += fmt.Sprintf("; notes: %s", input.Notes)
	}
	_, _ = s.deps.Store.CreateEvent(runIDInt, "info", eventMsg)

	idStr := strconv.FormatInt(run.ID, 10)
	result := submitAuditOutput{
		OK:             true,
		Tool:           "submit_audit_packet",
		RunID:          idStr,
		Decision:       input.Decision,
		Status:         updatedRun.Status,
		LifecycleState: lifecycleStateFromStatus(updatedRun.Status),
		ArtifactKind:   "audit_packet",
		ReviewURL:      fmt.Sprintf("/runs/%s/intake", idStr),
	}

	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

// auditDecisionToStatus maps an audit decision to the target run status and an optional note.
func auditDecisionToStatus(decision string) (status string, note string) {
	switch decision {
	case "accepted":
		return "accepted", ""
	case "accepted_with_warnings":
		return "accepted_with_warnings", ""
	case "revision_required":
		return "revision_required", ""
	case "blocked":
		return "revision_required", "decision was blocked; mapped to revision_required"
	case "manual_review_required":
		return "revision_required", "decision was manual_review_required; mapped to revision_required"
	default:
		return "revision_required", "unknown decision; defaulting to revision_required"
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
