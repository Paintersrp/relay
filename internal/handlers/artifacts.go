package handlers

import (
	"net/http"
	"strconv"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

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
