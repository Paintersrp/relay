package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"relay/internal/repos"
	"relay/internal/store"
	"relay/internal/views"

	"github.com/go-chi/chi/v5"
)

type RepoSettingsHandler struct {
	store       *store.Store
	repoService *repos.Service
	log         *slog.Logger
}

func NewRepoSettingsHandler(s *store.Store, rs *repos.Service, log *slog.Logger) *RepoSettingsHandler {
	return &RepoSettingsHandler{store: s, repoService: rs, log: log}
}

func (h *RepoSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	roots, err := h.store.ListRepoRoots()
	if err != nil {
		h.log.Error("list repo roots", "error", err)
		roots = nil
	}
	reposList, err := h.store.ListReposByName()
	if err != nil {
		h.log.Error("list repos", "error", err)
		reposList = nil
	}
	views.RepoSettings(roots, reposList, nil).Render(r.Context(), w)
}

func (h *RepoSettingsHandler) AddRoot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	path := repos.NormalizePath(r.FormValue("path"))
	if path == "" || path == "." {
		http.Error(w, "invalid repo root path", http.StatusBadRequest)
		return
	}

	if _, err := h.store.CreateRepoRoot(path); err != nil {
		h.log.Error("create repo root", "error", err)
		http.Error(w, "failed to add repo root", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings/repos", http.StatusSeeOther)
}

func (h *RepoSettingsHandler) ToggleRoot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid root id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	enabled := r.FormValue("enabled") == "1" || r.FormValue("enabled") == "true"

	if _, err := h.store.SetRepoRootEnabled(id, enabled); err != nil {
		h.log.Error("toggle repo root", "error", err)
		http.Error(w, "failed to toggle repo root", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings/repos", http.StatusSeeOther)
}

func (h *RepoSettingsHandler) DeleteRoot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid root id", http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteRepoRoot(id); err != nil {
		h.log.Error("delete repo root", "error", err)
		http.Error(w, "failed to delete repo root", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings/repos", http.StatusSeeOther)
}

func (h *RepoSettingsHandler) Scan(w http.ResponseWriter, r *http.Request) {
	summary := h.repoService.ScanEnabledRoots(r.Context())

	roots, err := h.store.ListRepoRoots()
	if err != nil {
		roots = nil
	}
	reposList, err := h.store.ListReposByName()
	if err != nil {
		reposList = nil
	}

	views.RepoSettings(roots, reposList, &summary).Render(r.Context(), w)
}
