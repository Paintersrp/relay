package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"relay/internal/auditor"

	"github.com/go-chi/chi/v5"
)

type localAuditAPIRequest struct {
	Mode             string   `json:"mode"`
	ProjectID        string   `json:"project_id"`
	RepoIDs          []string `json:"repo_ids"`
	PlanID           string   `json:"plan_id"`
	PassID           string   `json:"pass_id"`
	SourceSnapshotID string   `json:"source_snapshot_id"`
	ContextPacketID  string   `json:"context_packet_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Paths            []string `json:"paths"`
	SearchTerms      []string `json:"search_terms"`
	DiffMode         string   `json:"diff_mode"`
	MaxFiles         int      `json:"max_files"`
	MaxBytes         int      `json:"max_bytes"`
	ContextLines     int      `json:"context_lines"`
}

func (h *APIHandler) CreateLocalAudit(w http.ResponseWriter, r *http.Request) {
	var req localAuditAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	if msg := validateLocalAuditAPIRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", msg)
		return
	}
	result, err := auditor.NewLocalAuditService(h.store).Create(r.Context(), auditor.LocalAuditInput{
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
			writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *APIHandler) GetLocalAudit(w http.ResponseWriter, r *http.Request) {
	auditID := strings.TrimSpace(chi.URLParam(r, "auditId"))
	if auditID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "auditId is required")
		return
	}
	result, err := auditor.NewLocalAuditService(h.store).Get(r.Context(), auditID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Local audit not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *APIHandler) ListProjectLocalAudits(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode != "" && !isAllowedLocalAuditMode(mode) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid local audit mode")
		return
	}
	limit := int64(50)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
			return
		}
		limit = parsed
	}
	result, err := auditor.NewLocalAuditService(h.store).ListByProject(r.Context(), projectID, mode, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project_id": projectID,
		"audits":     result,
	})
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
