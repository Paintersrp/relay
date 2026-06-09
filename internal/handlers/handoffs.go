package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/repos"
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
	reposList, err := h.store.ListReposByName()
	if err != nil {
		reposList = nil
	}
	var repoOptions []views.RepoOption
	for _, repo := range reposList {
		branches, err := repos.ListLocalBranches(repo.Path)
		ro := views.RepoOption{Repo: repo}
		if err == nil {
			ro.Branches = branches
		}
		repoOptions = append(repoOptions, ro)
	}
	views.NewHandoff(repoOptions).Render(r.Context(), w)
}

func (h *HandoffsHandler) Create(w http.ResponseWriter, r *http.Request) {
	handoffText, handoffSource, err := resolveHandoffText(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	repoIDText := r.FormValue("repo_id")
	repoName := r.FormValue("repo_name")
	repoPath := r.FormValue("repo_path")
	title := deriveRunTitle(handoffText)
	selectedModelOption := r.FormValue("selected_model_option")
	selectedModelCustom := r.FormValue("selected_model_custom")
	branchName := strings.TrimSpace(r.FormValue("branch_name"))

	recommendedModel, _ := pipeline.ParseRecommendedModel(handoffText)
	selectedModel, _ := pipeline.ResolveSelectedModel(selectedModelOption, selectedModelCustom, recommendedModel)

	var repo *store.Repo

	if repoIDText != "" {
		repoID, err := strconv.ParseInt(repoIDText, 10, 64)
		if err != nil {
			http.Error(w, "invalid repo selection", http.StatusBadRequest)
			return
		}
		repo, err = h.store.GetRepo(repoID)
		if err != nil {
			http.Error(w, "selected repo not found", http.StatusBadRequest)
			return
		}
	} else {
		var err error
		repoName, repoPath, err = normalizeManualRepoInput(repoName, repoPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		existing, err := h.store.GetRepoByPath(repoPath)
		if err == nil {
			repo = existing
		} else {
			repo, err = h.store.CreateRepo(repoName, repoPath)
			if err != nil {
				h.log.Error("create repo", "error", err)
				http.Error(w, "failed to create repo", http.StatusInternalServerError)
				return
			}
		}
	}

	run, err := h.store.CreateRun(repo.ID, title, "draft", recommendedModel, selectedModel, branchName)
	if err != nil {
		h.log.Error("create run", "error", err)
		http.Error(w, "failed to create run", http.StatusInternalServerError)
		return
	}

	artifactPath, err := artifacts.Write(run.ID, "original_handoff", pipeline.ArtifactFilename("original_handoff"), []byte(handoffText))
	if err != nil {
		h.log.Error("write handoff artifact", "error", err)
		http.Error(w, "failed to save handoff", http.StatusInternalServerError)
		return
	}

	_, err = h.store.CreateArtifact(run.ID, "original_handoff", artifactPath, "text/plain")
	if err != nil {
		h.log.Error("create artifact record", "error", err)
	}

	h.store.CreateEvent(run.ID, "info", "Handoff created from "+handoffSource)

	h.log.Info("handoff created", "run_id", run.ID, "repo", repo.Name)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(run.ID, 10), http.StatusSeeOther)
}

func normalizeManualRepoInput(repoName, repoPath string) (name string, path string, err error) {
	repoName = strings.TrimSpace(repoName)
	rawRepoPath := strings.TrimSpace(repoPath)
	if rawRepoPath == "" {
		return "", "", fmt.Errorf("repo path is required for manual repo entry")
	}

	path = repos.NormalizePath(rawRepoPath)
	if path == "" || path == "." {
		return "", "", fmt.Errorf("repo path is required for manual repo entry")
	}

	if repoName == "" {
		repoName = filepath.Base(path)
	}
	if repoName == "" || repoName == "." {
		return "", "", fmt.Errorf("repo name is required for manual repo entry")
	}

	return repoName, path, nil
}
