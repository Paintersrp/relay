package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	appaudits "relay/internal/app/audits"
	workflowstore "relay/internal/store/workflow"
)

var getAuditPacketSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["run_id"],
  "properties": {
    "run_id": {"type": "string", "minLength": 1}
  }
}`)

var recordAuditDecisionSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "run_id",
    "audit_packet_id",
    "packet_sha256",
    "audited_commit",
    "decision",
    "rationale",
    "operator_confirmed"
  ],
  "properties": {
    "run_id": {"type": "string", "minLength": 1},
    "audit_packet_id": {"type": "string", "minLength": 1},
    "packet_sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
    "audited_commit": {"type": "string", "pattern": "^[0-9a-f]{40}$"},
    "decision": {"type": "string", "enum": ["accepted", "needs_revision"]},
    "rationale": {"type": "string"},
    "material_findings": {
      "type": "array",
      "maxItems": 32,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["source", "summary", "evidence", "required_remediation"],
        "properties": {
          "source": {"type": "string", "enum": ["executor_implementation", "execution_spec", "both"]},
          "summary": {"type": "string", "minLength": 1},
          "evidence": {"type": "string", "minLength": 1},
          "required_remediation": {"type": "string", "minLength": 1}
        }
      }
    },
    "observations": {"type": "array", "maxItems": 32, "items": {"type": "string", "minLength": 1}},
    "operator_confirmed": {"const": true}
  }
}`)

var (
	ToolGetAuditPacket = ToolDefinition{
		Name:        "get_audit_packet",
		Description: "Retrieve the current authoritative audit packet for one workflow Run. Revalidates packet freshness against the selected attempt and local repository before returning the bounded packet body.",
		InputSchema: getAuditPacketSchema,
	}
	ToolRecordAuditDecision = ToolDefinition{
		Name:        "record_audit_decision",
		Description: "Record one operator-confirmed accepted or needs_revision decision against the exact current audit packet SHA-256 and audited commit.",
		InputSchema: recordAuditDecisionSchema,
	}
)

type WorkflowAuditToolService interface {
	GetCurrentPacket(context.Context, string) (appaudits.GetWorkflowAuditPacketResult, error)
	GetCurrentArtifact(context.Context, appaudits.GetWorkflowAuditArtifactInput) (appaudits.GetWorkflowAuditArtifactResult, error)
	RecordDecision(context.Context, appaudits.RecordWorkflowAuditDecisionInput) (appaudits.RecordWorkflowAuditDecisionResult, error)
}

type getAuditPacketArgs struct {
	RunID string `json:"run_id"`
}

type recordAuditDecisionArgs struct {
	RunID             string                                   `json:"run_id"`
	AuditPacketID     string                                   `json:"audit_packet_id"`
	PacketSHA256      string                                   `json:"packet_sha256"`
	AuditedCommit     string                                   `json:"audited_commit"`
	Decision          string                                   `json:"decision"`
	Rationale         string                                   `json:"rationale"`
	MaterialFindings  []appaudits.WorkflowAuditMaterialFinding `json:"material_findings"`
	Observations      []string                                 `json:"observations"`
	OperatorConfirmed bool                                     `json:"operator_confirmed"`
}

func (s *Server) workflowAuditService() (WorkflowAuditToolService, error) {
	if s != nil && s.deps != nil && s.deps.WorkflowAuditService != nil {
		return s.deps.WorkflowAuditService, nil
	}
	service, err := appaudits.NewWorkflowAuditService(s.workflowStore())
	if err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Server) HandleGetWorkflowAuditPacket(rawArgs json.RawMessage) ToolCallResult {
	var input getAuditPacketArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return workflowBlocked("get_audit_packet", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "get_audit_packet", nil)
	}
	service, err := s.workflowAuditService()
	if err != nil {
		return workflowBlocked("get_audit_packet", MCPBlockerToolUnavailable, "workflow audit service is unavailable", false, "workflow_store", nil)
	}
	result, err := service.GetCurrentPacket(context.Background(), strings.TrimSpace(input.RunID))
	if err != nil {
		return workflowAuditBlocked("get_audit_packet", err)
	}
	var packet any
	if err := json.Unmarshal(result.PacketBytes, &packet); err != nil {
		return workflowBlocked("get_audit_packet", submissionBlockerPersistenceFailed, "stored audit packet could not be decoded", false, "audit_packet", nil)
	}
	return workflowOK(map[string]any{
		"ok":              true,
		"tool":            "get_audit_packet",
		"run_id":          result.Run.RunID,
		"run_status":      result.Run.Status,
		"audit_packet_id": result.Packet.AuditPacketID,
		"packet_sha256":   result.Packet.PacketSHA256,
		"audited_commit":  result.Packet.AuditedCommit,
		"packet":          packet,
	})
}

func (s *Server) HandleRecordWorkflowAuditDecision(rawArgs json.RawMessage) ToolCallResult {
	var input recordAuditDecisionArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return workflowBlocked("record_audit_decision", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "record_audit_decision", nil)
	}
	service, err := s.workflowAuditService()
	if err != nil {
		return workflowBlocked("record_audit_decision", MCPBlockerToolUnavailable, "workflow audit service is unavailable", false, "workflow_store", nil)
	}
	result, err := service.RecordDecision(context.Background(), appaudits.RecordWorkflowAuditDecisionInput{
		RunID:             input.RunID,
		AuditPacketID:     input.AuditPacketID,
		PacketSHA256:      input.PacketSHA256,
		AuditedCommit:     input.AuditedCommit,
		Decision:          input.Decision,
		Rationale:         input.Rationale,
		MaterialFindings:  input.MaterialFindings,
		Observations:      input.Observations,
		OperatorConfirmed: input.OperatorConfirmed,
	})
	if err != nil {
		return workflowAuditBlocked("record_audit_decision", err)
	}
	out := map[string]any{
		"ok":                true,
		"tool":              "record_audit_decision",
		"run_id":            result.Run.RunID,
		"run_status":        result.Run.Status,
		"audit_packet_id":   result.Packet.AuditPacketID,
		"packet_sha256":     result.Packet.PacketSHA256,
		"audited_commit":    result.Decision.AuditedCommit,
		"decision":          result.Decision.Decision,
		"audit_decision_id": result.Decision.AuditDecisionID,
	}
	if result.Pass != nil {
		out["pass_id"] = result.Pass.PassID
		out["pass_status"] = result.Pass.Status
	}
	if result.Plan != nil {
		out["plan_id"] = result.Plan.PlanID
		out["plan_status"] = result.Plan.Status
	}
	ticketDecisions := make([]map[string]any, 0, len(result.TicketRevisionDecisions))
	for _, decision := range result.TicketRevisionDecisions {
		ticketDecisions = append(ticketDecisions, map[string]any{
			"audit_ticket_revision_decision_row_id": decision.ID,
			"audit_packet_ticket_obligation_row_id": decision.AuditPacketTicketObligationRowID,
		})
	}
	satisfactions := make([]map[string]any, 0, len(result.TicketSatisfactions))
	for _, satisfaction := range result.TicketSatisfactions {
		satisfactions = append(satisfactions, map[string]any{
			"delivery_ticket_revision_row_id":       satisfaction.DeliveryTicketRevisionRowID,
			"audit_ticket_revision_decision_row_id": satisfaction.AuditTicketRevisionDecisionRowID,
		})
	}
	seeds := make([]map[string]any, 0, len(result.RemediationSeeds))
	for _, seed := range result.RemediationSeeds {
		seeds = append(seeds, map[string]any{
			"remediation_seed_id":      seed.RemediationSeedID,
			"audit_packet_row_id":      seed.AuditPacketRowID,
			"execution_package_row_id": seed.ExecutionPackageRowID,
			"audited_commit":           seed.AuditedCommit,
		})
	}
	out["ticket_effects"] = map[string]any{
		"ticket_revision_decisions": ticketDecisions,
		"ticket_satisfactions":      satisfactions,
		"remediation_seeds":         seeds,
	}
	return workflowOK(out)
}

func workflowAuditBlocked(tool string, err error) ToolCallResult {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, appaudits.ErrWorkflowAuditPacketNotFound):
		return workflowBlocked(tool, MCPBlockerUnknownResource, "workflow Run or audit packet was not found", true, "run_id", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditArtifactReference):
		return workflowBlocked(tool, "artifact_reference_not_declared", "artifact_reference is not declared by the current audit packet", true, "artifact_reference", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditArtifactOwnership):
		return workflowBlocked(tool, "artifact_ownership_mismatch", "artifact_reference does not belong to the current packet execution attempt", false, "artifact_reference", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditArtifactIntegrity):
		return workflowBlocked(tool, "artifact_integrity_failed", "stored artifact size, SHA-256, or packet metadata verification failed", false, "artifact_reference", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditArtifactUnsupported):
		return workflowBlocked(tool, "artifact_content_unsupported", "artifact content is not supported for bounded UTF-8 readback", true, "artifact_reference", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditConfirmation):
		return workflowBlocked(tool, "operator_confirmation_required", "operator_confirmed must be true after explicit operator confirmation", true, "operator_confirmed", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditPacketStale):
		return workflowBlocked(tool, "audit_packet_stale", "audit packet is no longer current for the repository or selected execution attempt", true, "audit_packet_id", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditDecisionInput):
		return workflowBlocked(tool, "audit_decision_invalid", "decision rationale, findings, or observations do not satisfy the audit contract", true, "decision", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditTicketIneligible):
		return workflowBlocked(tool, "audit_ticket_ineligible", "the exact ticket package is no longer current and eligible for the requested audit effect", true, "audit_packet_id", nil)
	case errors.Is(err, appaudits.ErrWorkflowAuditNotReady), errors.Is(err, appaudits.ErrWorkflowAuditDecisionRecorded):
		return workflowBlocked(tool, "audit_state_conflict", err.Error(), true, "run_id", nil)
	default:
		return workflowBlocked(tool, submissionBlockerPersistenceFailed, "workflow audit operation failed", false, "workflow_store", nil)
	}
}

var _ WorkflowAuditToolService = (*appaudits.WorkflowAuditService)(nil)
var _ = workflowstore.AuditDecisionAccepted
