package audits

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appaudits "relay/internal/app/audits"
	"relay/internal/api/shared"
	"relay/internal/auditor"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *appaudits.Service
}

func NewHandler(service *appaudits.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateLocalAudit(w http.ResponseWriter, r *http.Request) {
	var req localAuditAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	if msg := validateLocalAuditAPIRequest(req); msg != "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", msg)
		return
	}
	result, err := h.service.CreateLocalAudit(r.Context(), appaudits.LocalAuditInput{
		Mode:             req.Mode,
		ProjectID:        req.ProjectID,
		RepoIDs:          req.RepoIDs,
		PlanID:           req.PlanID,
		PassID:           req.PassID,
		SourceSnapshotID: req.SourceSnapshotID,
		ContextPacketID:  req.ContextPacketID,
		Title:            req.Title,
		Description:      req.Description,
		Paths:            req.Paths,
		SearchTerms:      req.SearchTerms,
		DiffMode:         req.DiffMode,
		MaxFiles:         req.MaxFiles,
		MaxBytes:         req.MaxBytes,
		ContextLines:     req.ContextLines,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, result)
}

func (h *Handler) GetLocalAudit(w http.ResponseWriter, r *http.Request) {
	auditID := strings.TrimSpace(chi.URLParam(r, "auditId"))
	if auditID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "auditId is required")
		return
	}
	result, err := h.service.GetLocalAudit(r.Context(), auditID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Local audit not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, result)
}

func (h *Handler) ListProjectLocalAudits(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode != "" && !isAllowedLocalAuditMode(mode) {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid local audit mode")
		return
	}
	limit := int64(50)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 || parsed > 100 {
			shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
			return
		}
		limit = parsed
	}
	result, err := h.service.ListProjectLocalAudits(r.Context(), projectID, mode, limit)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"project_id": projectID,
		"audits":     result,
	})
}

func (h *Handler) GetAuditStatus(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	status, err := h.service.GetAuditStatus(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to build audit status: "+err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, mapAuditStatusToRelay(status))
}

func (h *Handler) GenerateAudit(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	result, err := h.service.GenerateAudit(r.Context(), id)
	if err != nil {
		var conflictErr *appaudits.AuditGenerationConflictError
		if errors.As(err, &conflictErr) {
			shared.Error(w, http.StatusConflict, "CONFLICT", conflictErr.Message)
			return
		}
		shared.Error(w, http.StatusConflict, "CONFLICT", err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"runId":            idStr,
		"status":           result.Status,
		"inputSummary":     result.InputSummary,
		"evidenceManifest": result.EvidenceManifest,
		"auditPacket":      result.AuditPacket,
		"decision":         result.Decision,
		"warnings":         result.Warnings,
		"lifecycleState":   "audit",
	})
}

func (h *Handler) SubmitAuditPacket(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	var req submitAuditPacketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	result, err := h.service.SubmitAuditPacket(r.Context(), appaudits.SubmitAuditPacketInput{
		RunID:               id,
		AuditPacketMarkdown: req.AuditPacketMarkdown,
		Decision:            req.Decision,
		Notes:               req.Notes,
	})
	if err != nil {
		writeAuditDecisionError(w, err)
		return
	}

	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":              true,
		"runId":                idStr,
		"auditPacket":          result.AuditPacketPath,
		"decision":             result.Decision,
		"status":               result.Status,
		"lifecycleState":       result.LifecycleState,
		"decisionArtifactPath": result.DecisionArtifactPath,
		"updatedAt":            result.CreatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) ApproveAudit(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	var req approveAuditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	if req.Decision != "accepted" && req.Decision != "accepted_with_warnings" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid audit decision %q: must be accepted or accepted_with_warnings", req.Decision))
		return
	}

	result, err := h.service.SubmitAuditDecision(r.Context(), appaudits.AuditDecisionInput{
		RunID:    id,
		Decision: auditor.Decision(req.Decision),
		Notes:    req.Notes,
	})
	if err != nil {
		writeAuditDecisionError(w, err)
		return
	}

	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          idStr,
		"status":         result.Status,
		"lifecycleState": result.LifecycleState,
		"updatedAt":      result.CreatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) RequestAuditRevision(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	var req requestRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Notes = ""
		req.Reason = ""
	}

	result, err := h.service.RequestRevision(r.Context(), appaudits.RevisionInput{
		RunID:  id,
		Notes:  req.Notes,
		Reason: req.Reason,
	})
	if err != nil {
		writeAuditDecisionError(w, err)
		return
	}

	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          idStr,
		"status":         result.Status,
		"lifecycleState": result.LifecycleState,
		"updatedAt":      result.CreatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) PrepareCommitMessage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	result, err := h.service.PrepareCommitMessage(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "must be accepted or accepted_with_warnings") {
			shared.Error(w, http.StatusConflict, "CONFLICT", err.Error())
			return
		}
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"runId":         idStr,
		"commitMessage": result.CommitMessage,
		"artifactPath":  result.ArtifactPath,
		"artifactKind":  result.ArtifactKind,
	})
}

func (h *Handler) CloseRun(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	result, err := h.service.CloseRun(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "must be accepted or accepted_with_warnings") {
			shared.Error(w, http.StatusConflict, "CONFLICT", err.Error())
			return
		}
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          idStr,
		"status":         "completed",
		"lifecycleState": "completed",
		"updatedAt":      result.UpdatedAt.Format(time.RFC3339),
	})
}

func writeAuditDecisionError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, auditor.ErrUnsupportedDecision):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case strings.Contains(err.Error(), "audit_packet_markdown is required"):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, auditor.ErrCompletedRun), errors.Is(err, auditor.ErrAuditDecisionNotReady):
		shared.Error(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
	}
}

func validateLocalAuditAPIRequest(req localAuditAPIRequest) string {
	mode := strings.TrimSpace(req.Mode)
	if !isAllowedLocalAuditMode(mode) {
		return "Invalid local audit mode"
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return "project_id is required"
	}
	if req.MaxFiles < 0 || req.MaxFiles > 1000 {
		return "max_files must be between 0 and 1000"
	}
	if req.MaxBytes < 0 || req.MaxBytes > 1048576 {
		return "max_bytes must be between 0 and 1048576"
	}
	if req.ContextLines < 0 || req.ContextLines > 5 {
		return "context_lines must be between 0 and 5"
	}
	if len(req.Paths) > 50 {
		return "paths is limited to 50 entries"
	}
	if len(req.SearchTerms) > 20 {
		return "search_terms is limited to 20 entries"
	}
	for _, p := range req.Paths {
		if invalidLocalAuditPath(p) {
			return "paths must be repository-relative and may not contain .. or absolute paths"
		}
	}
	switch mode {
	case string(auditor.LocalAuditModeRecentCommit):
		if len(nonEmptyStrings(req.RepoIDs)) != 1 {
			return "recent_commit requires exactly one repo_id"
		}
	case string(auditor.LocalAuditModeSelectedPassChanges):
		if strings.TrimSpace(req.PlanID) == "" || strings.TrimSpace(req.PassID) == "" {
			return "selected_pass_changes requires plan_id and pass_id"
		}
	case string(auditor.LocalAuditModeFeatureSlice):
		if len(nonEmptyStrings(req.Paths)) == 0 && len(nonEmptyStrings(req.SearchTerms)) == 0 {
			return "feature_slice requires paths or search_terms"
		}
	}
	return ""
}

func isAllowedLocalAuditMode(mode string) bool {
	switch mode {
	case string(auditor.LocalAuditModeRecentCommit),
		string(auditor.LocalAuditModeSelectedPassChanges),
		string(auditor.LocalAuditModeFeatureSlice),
		string(auditor.LocalAuditModeFullRepository):
		return true
	default:
		return false
	}
}

func invalidLocalAuditPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return true
	}
	cleaned := path.Clean(value)
	return path.IsAbs(value) || filepath.IsAbs(value) || cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func mapAuditStatusToRelay(status *appaudits.AuditStatus) *RelayAuditStatus {
	return &RelayAuditStatus{
		RunID:                        status.RunID,
		RunStatus:                    status.RunStatus,
		AuditState:                   status.AuditState,
		CanGenerateAudit:             status.CanGenerateAudit,
		CanSubmitDecision:            status.CanSubmitDecision,
		CanApprove:                   status.CanApprove,
		CanRequestRevision:           status.CanRequestRevision,
		CanCloseRun:                  status.CanCloseRun,
		EvidenceManifestArtifact:     mapAppArtifactToRelay(status.EvidenceManifestArtifact),
		GeneratedAuditPacketArtifact: mapAppArtifactToRelay(status.GeneratedAuditPacketArtifact),
		ManualAuditPacketArtifact:    mapAppArtifactToRelay(status.ManualAuditPacketArtifact),
		DecisionArtifact:             mapAppArtifactToRelay(status.DecisionArtifact),
		Blockers:                     status.Blockers,
		Warnings:                     status.Warnings,
		RevisionRequirements:         status.RevisionRequirements,
		LocalOnly:                    status.LocalOnly,
	}
}

func mapAppArtifactToRelay(art *appaudits.AuditArtifact) *RelayArtifact {
	if art == nil {
		return nil
	}
	return &RelayArtifact{
		ID:          art.ID,
		Label:       art.Label,
		Path:        art.Path,
		Kind:        art.Kind,
		StorageKind: art.StorageKind,
		ContentURL:  art.ContentURL,
		SizeHint:    art.SizeHint,
		CreatedAt:   art.CreatedAt,
		Status:      art.Status,
		Filename:    art.Filename,
		Preview:     art.Preview,
	}
}
