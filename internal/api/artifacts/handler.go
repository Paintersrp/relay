package artifacts

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	runsapi "relay/internal/api/runs"
	"relay/internal/api/shared"
	appruns "relay/internal/app/runs"

	"github.com/go-chi/chi/v5"
)

// Handler is the run artifact feature HTTP transport adapter.
type Handler struct {
	runs *appruns.Service
}

// NewHandler constructs an artifact Handler.
func NewHandler(runs *appruns.Service) *Handler {
	return &Handler{runs: runs}
}

// GET /api/runs/{id}/artifacts
func (h *Handler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	exists, _ := h.runs.RunExists(r.Context(), id)
	if !exists {
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	views, err := h.runs.ListRunArtifactViews(r.Context(), id)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list artifacts")
		return
	}

	shared.JSON(w, http.StatusOK, runsapi.BuildArtifactDTOs(idStr, views))
}

// GET /api/runs/{id}/artifacts/{kind}
func (h *Handler) GetArtifactContent(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}
	if kind == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Artifact kind is required")
		return
	}

	content, err := h.runs.GetRunArtifactContent(r.Context(), id, kind)
	if err != nil {
		if errors.Is(err, appruns.ErrArtifactNotFound) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("No artifact found for run %s kind %s", idStr, kind))
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to read artifact file")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content.Data)
}
