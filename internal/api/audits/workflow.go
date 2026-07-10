package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	appaudits "relay/internal/app/audits"

	"github.com/go-chi/chi/v5"
)

type WorkflowAuditService interface {
	Prepare(context.Context, appaudits.PrepareWorkflowAuditInput) (appaudits.PrepareWorkflowAuditResult, error)
	GetStatus(context.Context, string) (appaudits.WorkflowAuditStatus, error)
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

func writeWorkflowAuditError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, appaudits.ErrWorkflowAuditPacketNotFound):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Workflow Run or audit packet was not found")
	case errors.Is(err, appaudits.ErrWorkflowAuditNotReady),
		errors.Is(err, appaudits.ErrWorkflowAuditPacketStale),
		errors.Is(err, appaudits.ErrWorkflowAuditDecisionRecorded):
		shared.Error(w, http.StatusConflict, "AUDIT_CONFLICT", err.Error())
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
