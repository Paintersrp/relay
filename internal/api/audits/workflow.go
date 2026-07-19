package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	appaudits "relay/internal/app/audits"

	"github.com/go-chi/chi/v5"
)

type WorkflowAuditService interface {
	Prepare(context.Context, appaudits.PrepareWorkflowAuditInput) (appaudits.PrepareWorkflowAuditResult, error)
	GetCurrentPacket(context.Context, string) (appaudits.GetWorkflowAuditPacketResult, error)
	GetCurrentArtifact(context.Context, appaudits.GetWorkflowAuditArtifactInput) (appaudits.GetWorkflowAuditArtifactResult, error)
	GetStatus(context.Context, string) (appaudits.WorkflowAuditStatus, error)
	RecordDecision(context.Context, appaudits.RecordWorkflowAuditDecisionInput) (appaudits.RecordWorkflowAuditDecisionResult, error)
}

type WorkflowHandler struct {
	service WorkflowAuditService
}

func NewWorkflowHandler(service WorkflowAuditService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

type prepareWorkflowAuditRequest struct {
	AuditedCommit string `json:"auditedCommit"`
}

type recordWorkflowAuditDecisionRequest struct {
	AuditPacketID     string                                   `json:"auditPacketId"`
	PacketSHA256      string                                   `json:"packetSha256"`
	AuditedCommit     string                                   `json:"auditedCommit"`
	Decision          string                                   `json:"decision"`
	Rationale         string                                   `json:"rationale"`
	MaterialFindings  []appaudits.WorkflowAuditMaterialFinding `json:"materialFindings"`
	Observations      []string                                 `json:"observations"`
	OperatorConfirmed bool                                     `json:"operatorConfirmed"`
}

type workflowAuditPacketResponse struct {
	AuditPacketID           string `json:"auditPacketId"`
	ImplementationActorKind string `json:"implementationActorKind"`
	AuditedCommit           string `json:"auditedCommit"`
	PacketSHA256            string `json:"packetSha256"`
	Status                  string `json:"status"`
	StaleReason             string `json:"staleReason,omitempty"`
	CreatedAt               string `json:"createdAt"`
	SupersededAt            string `json:"supersededAt,omitempty"`
}

func (h *WorkflowHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	var request prepareWorkflowAuditRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid audit preparation request")
		return
	}
	result, err := h.service.Prepare(r.Context(), appaudits.PrepareWorkflowAuditInput{
		RunID: runID, AuditedCommit: request.AuditedCommit,
	})
	if err != nil {
		writeWorkflowAuditError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{
		"success":   true,
		"runId":     result.Run.RunID,
		"runStatus": result.Run.Status,
		"packet":    workflowAuditPacketDTO(result.Packet),
		"artifact": map[string]any{
			"artifactId": result.Artifact.ArtifactID,
			"kind":       result.Artifact.Kind,
			"sha256":     result.Artifact.SHA256,
			"sizeBytes":  result.Artifact.SizeBytes,
			"contentUrl": "/api/artifacts/" + result.Artifact.ArtifactID + "/content",
		},
	})
}

func (h *WorkflowHandler) Status(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	status, err := h.service.GetStatus(r.Context(), runID)
	if err != nil {
		writeWorkflowAuditError(w, err)
		return
	}
	response := map[string]any{
		"runId":     status.RunID,
		"runStatus": status.RunStatus,
	}
	if status.CurrentPacket != nil {
		response["currentPacket"] = workflowAuditPacketDTO(*status.CurrentPacket)
	}
	if status.LatestPacket != nil {
		response["latestPacket"] = workflowAuditPacketDTO(*status.LatestPacket)
	}
	if status.Decision != nil {
		response["decision"] = map[string]any{
			"auditDecisionId": status.Decision.AuditDecisionID,
			"auditedCommit":   status.Decision.AuditedCommit,
			"packetSha256":    status.Decision.PacketSHA256,
			"decision":        status.Decision.Decision,
			"rationale":       status.Decision.Rationale,
			"createdAt":       status.Decision.CreatedAt,
		}
	}
	shared.JSON(w, http.StatusOK, response)
}

// Packet returns the exact current packet body and, when present, its bounded
// ticket-package evidence. Both reads use the shared audit owner so a stale
// packet, undeclared artifact, or integrity failure remains fail-closed.
func (h *WorkflowHandler) Packet(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.GetCurrentPacket(r.Context(), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeWorkflowAuditError(w, err)
		return
	}
	var document appaudits.WorkflowAuditPacket
	if err := json.Unmarshal(current.PacketBytes, &document); err != nil {
		shared.Error(w, http.StatusConflict, "AUDIT_CONFLICT", "Current audit packet could not be decoded")
		return
	}
	var rawDocument any
	if err := json.Unmarshal(current.PacketBytes, &rawDocument); err != nil {
		shared.Error(w, http.StatusConflict, "AUDIT_CONFLICT", "Current audit packet could not be decoded")
		return
	}
	response := map[string]any{
		"runId":     current.Run.RunID,
		"runStatus": current.Run.Status,
		"packet":    workflowAuditPacketDTO(current.Packet),
		"document":  rawDocument,
	}
	for _, artifact := range document.Artifacts {
		if artifact.ArtifactType != "ticket_package_evidence" {
			continue
		}
		evidence, err := h.service.GetCurrentArtifact(r.Context(), appaudits.GetWorkflowAuditArtifactInput{
			RunID: current.Run.RunID, ArtifactReference: artifact.ArtifactReference, MaxBytes: appaudits.MaxWorkflowAuditReadBytes,
		})
		if err != nil {
			writeWorkflowAuditError(w, err)
			return
		}
		var ticketPackage appaudits.WorkflowAuditTicketPackageEvidence
		if err := json.Unmarshal(evidence.Content, &ticketPackage); err != nil {
			shared.Error(w, http.StatusConflict, "AUDIT_CONFLICT", "Current ticket package evidence could not be decoded")
			return
		}
		response["ticketPackage"] = workflowAuditTicketPackageDTO(ticketPackage)
		break
	}
	shared.JSON(w, http.StatusOK, response)
}

func (h *WorkflowHandler) RecordDecision(w http.ResponseWriter, r *http.Request) {
	var request recordWorkflowAuditDecisionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid audit decision request")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid audit decision request")
		return
	}
	result, err := h.service.RecordDecision(r.Context(), appaudits.RecordWorkflowAuditDecisionInput{
		RunID: strings.TrimSpace(chi.URLParam(r, "runID")), AuditPacketID: request.AuditPacketID,
		PacketSHA256: request.PacketSHA256, AuditedCommit: request.AuditedCommit, Decision: request.Decision,
		Rationale: request.Rationale, MaterialFindings: request.MaterialFindings, Observations: request.Observations,
		OperatorConfirmed: request.OperatorConfirmed,
	})
	if err != nil {
		writeWorkflowAuditError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, workflowAuditDecisionResultDTO(result))
}

func workflowAuditPacketDTO(packet appaudits.AuditPacket) workflowAuditPacketResponse {
	response := workflowAuditPacketResponse{
		AuditPacketID:           packet.AuditPacketID,
		ImplementationActorKind: packet.ImplementationActorKind,
		AuditedCommit:           packet.AuditedCommit,
		PacketSHA256:            packet.PacketSHA256,
		Status:                  packet.Status,
		StaleReason:             packet.StaleReason,
		CreatedAt:               packet.CreatedAt,
	}
	if packet.SupersededAt.Valid {
		response.SupersededAt = packet.SupersededAt.String
	}
	return response
}

func workflowAuditDecisionResultDTO(result appaudits.RecordWorkflowAuditDecisionResult) map[string]any {
	decisions := make([]map[string]any, 0, len(result.TicketRevisionDecisions))
	for _, decision := range result.TicketRevisionDecisions {
		decisions = append(decisions, map[string]any{
			"auditTicketRevisionDecisionRowId": decision.ID,
			"auditPacketTicketObligationRowId": decision.AuditPacketTicketObligationRowID,
		})
	}
	satisfactions := make([]map[string]any, 0, len(result.TicketSatisfactions))
	for _, satisfaction := range result.TicketSatisfactions {
		satisfactions = append(satisfactions, map[string]any{
			"deliveryTicketRevisionRowId":      satisfaction.DeliveryTicketRevisionRowID,
			"auditTicketRevisionDecisionRowId": satisfaction.AuditTicketRevisionDecisionRowID,
		})
	}
	seeds := make([]map[string]any, 0, len(result.RemediationSeeds))
	for _, seed := range result.RemediationSeeds {
		seeds = append(seeds, map[string]any{
			"remediationSeedId":     seed.RemediationSeedID,
			"auditPacketRowId":      seed.AuditPacketRowID,
			"executionPackageRowId": seed.ExecutionPackageRowID,
			"auditedCommit":         seed.AuditedCommit,
		})
	}
	return map[string]any{
		"runId": result.Run.RunID, "runStatus": result.Run.Status,
		"packet": workflowAuditPacketDTO(result.Packet),
		"decision": map[string]any{
			"auditDecisionId": result.Decision.AuditDecisionID, "auditedCommit": result.Decision.AuditedCommit,
			"packetSha256": result.Decision.PacketSHA256, "decision": result.Decision.Decision,
			"rationale": result.Decision.Rationale, "createdAt": result.Decision.CreatedAt,
		},
		"effects": map[string]any{
			"ticketRevisionDecisions": decisions, "ticketSatisfactions": satisfactions, "remediationSeeds": seeds,
		},
	}
}

func workflowAuditTicketPackageDTO(value appaudits.WorkflowAuditTicketPackageEvidence) map[string]any {
	tickets := make([]map[string]any, 0, len(value.Tickets))
	for _, ticket := range value.Tickets {
		tickets = append(tickets, map[string]any{
			"sequence": ticket.Sequence, "ticketId": ticket.TicketID, "revisionRowId": ticket.DeliveryTicketRevisionID,
			"revisionNumber": ticket.RevisionNumber, "memberSha256": ticket.MemberSHA256,
			"approvalId": ticket.Approval.ApprovalID, "approvalBasisSha256": ticket.Approval.ApprovalBasisSHA256,
			"authorityRevisionRowId": ticket.Approval.AuthorityRevisionRowID, "sourceClosureRowId": ticket.Approval.SourceClosureRowID,
			"designBrief": map[string]any{"artifactReference": ticket.DesignBrief.ArtifactReference, "sha256": ticket.DesignBrief.SHA256},
		})
	}
	leases := make([]map[string]any, 0, len(value.MutationLeases))
	for _, lease := range value.MutationLeases {
		leases = append(leases, map[string]any{
			"leaseId": lease.LeaseID, "state": lease.State, "certainty": lease.Certainty,
			"reconciliationState": lease.ReconciliationState, "releasedAt": lease.ReleasedAt,
		})
	}
	return map[string]any{
		"package": map[string]any{
			"packageId": value.Package.PackageID, "packageSha256": value.Package.PackageSHA256,
			"workspaceId": value.Package.WorkspaceID, "featureSlug": value.Package.FeatureSlug,
			"selectionId": value.Package.SelectionID, "selectionState": value.Package.SelectionState,
			"authorityRevisionId": value.Package.Authority.AuthorityRevisionID,
			"authoritySha256":     value.Package.Authority.SHA256, "sourceClosureId": value.Package.Source.ClosureID,
			"sourceCommit": value.Package.Source.CommitOID,
		},
		"tickets": tickets, "mutationLeases": leases,
		"bundleIntegration": map[string]any{
			"runId": value.BundleIntegration.RunID, "executionPackageId": value.BundleIntegration.ExecutionPackageID,
			"selectionId": value.BundleIntegration.SelectionID, "selectionState": value.BundleIntegration.SelectionState,
			"approvedRunStatus": value.BundleIntegration.ApprovedRunStatus,
		},
	}
}

func writeWorkflowAuditError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, appaudits.ErrWorkflowAuditPacketNotFound):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Workflow Run or audit packet was not found")
	case errors.Is(err, appaudits.ErrWorkflowAuditNotReady),
		errors.Is(err, appaudits.ErrWorkflowAuditPacketStale),
		errors.Is(err, appaudits.ErrWorkflowAuditDecisionRecorded),
		errors.Is(err, appaudits.ErrWorkflowAuditTicketIneligible):
		shared.Error(w, http.StatusConflict, "AUDIT_CONFLICT", err.Error())
	case errors.Is(err, appaudits.ErrWorkflowAuditConfirmation), errors.Is(err, appaudits.ErrWorkflowAuditDecisionInput):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, appaudits.ErrWorkflowAuditPacketTooLarge),
		strings.Contains(err.Error(), "required"),
		strings.Contains(err.Error(), "does not exist"),
		strings.Contains(err.Error(), "not descended"),
		strings.Contains(err.Error(), "contains no changes"),
		strings.Contains(err.Error(), "repository_"),
		strings.Contains(err.Error(), "branch_"),
		strings.Contains(err.Error(), "head_"):
		shared.Error(w, http.StatusBadRequest, "AUDIT_PREPARATION_BLOCKED", err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Workflow audit operation failed")
	}
}
