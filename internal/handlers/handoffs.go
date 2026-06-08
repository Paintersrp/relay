package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"
	"relay/internal/views"
)

type HandoffsHandler struct {
	store *store.Store
	log   *slog.Logger
}

func NewHandoffsHandler(s *store.Store, log *slog.Logger) *HandoffsHandler {
	return &HandoffsHandler{store: s, log: log}
}

func (h *HandoffsHandler) NewForm(w http.ResponseWriter, r *http.Request) {
	repos, err := h.store.ListRepos()
	if err != nil {
		repos = nil
	}
	views.NewHandoff(repos).Render(r.Context(), w)
}

func (h *HandoffsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	repoName := r.FormValue("repo_name")
	repoPath := r.FormValue("repo_path")
	title := r.FormValue("title")
	recommendedModel := r.FormValue("recommended_model")
	selectedModel := r.FormValue("selected_model")
	branchName := r.FormValue("branch_name")
	handoffText := r.FormValue("handoff_text")

	if selectedModel == "" {
		selectedModel = recommendedModel
	}
	if repoName == "" {
		repoName = "default"
	}

	// find or create repo
	repo, err := h.store.GetRepoByName(repoName)
	if err != nil {
		repo, err = h.store.CreateRepo(repoName, repoPath)
		if err != nil {
			h.log.Error("create repo", "error", err)
			http.Error(w, "failed to create repo", http.StatusInternalServerError)
			return
		}
	}

	// create run
	run, err := h.store.CreateRun(repo.ID, title, "draft", recommendedModel, selectedModel, branchName)
	if err != nil {
		h.log.Error("create run", "error", err)
		http.Error(w, "failed to create run", http.StatusInternalServerError)
		return
	}

	// save original handoff to disk
	artifactPath, err := artifacts.Write(run.ID, "original_handoff", pipeline.ArtifactFilename("original_handoff"), []byte(handoffText))
	if err != nil {
		h.log.Error("write handoff artifact", "error", err)
		http.Error(w, "failed to save handoff", http.StatusInternalServerError)
		return
	}

	// create artifact metadata
	_, err = h.store.CreateArtifact(run.ID, "original_handoff", artifactPath, "text/plain")
	if err != nil {
		h.log.Error("create artifact record", "error", err)
	}

	// create event
	h.store.CreateEvent(run.ID, "info", "Handoff created")

	h.log.Info("handoff created", "run_id", run.ID, "repo", repoName)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(run.ID, 10), http.StatusSeeOther)
}
