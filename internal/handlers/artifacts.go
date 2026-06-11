package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"
	"relay/internal/views"

	"github.com/go-chi/chi/v5"
)

const artifactPreviewMaxBytes = 64 * 1024

func artifactPreviewContent(data []byte) (string, bool) {
	if len(data) <= artifactPreviewMaxBytes {
		return strings.ToValidUTF8(string(data), "�"), false
	}
	return strings.ToValidUTF8(string(data[:artifactPreviewMaxBytes]), "�"), true
}

type ArtifactsHandler struct {
	store *store.Store
}

func NewArtifactsHandler(s *store.Store) *ArtifactsHandler {
	return &ArtifactsHandler{store: s}
}

func (h *ArtifactsHandler) View(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	runID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	kind := chi.URLParam(r, "kind")

	data, err := artifacts.Read(runID, kind, pipeline.ArtifactFilename(kind))
	if err != nil {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

func (h *ArtifactsHandler) Download(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	runID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	kind := chi.URLParam(r, "kind")
	filename := pipeline.ArtifactFilename(kind)

	data, err := artifacts.Read(runID, kind, filename)
	if err != nil {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	w.Write(data)
}

func (h *ArtifactsHandler) Preview(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	runID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	kind := chi.URLParam(r, "kind")

	data, err := artifacts.Read(runID, kind, pipeline.ArtifactFilename(kind))
	if err != nil {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}

	content, truncated := artifactPreviewContent(data)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	views.ArtifactInlinePreview(runID, kind, content, truncated).Render(r.Context(), w)
}
