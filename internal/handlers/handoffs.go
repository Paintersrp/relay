package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/events"
	"relay/internal/pipeline"
	"relay/internal/repos"
	"relay/internal/store"
	"relay/internal/views"
)

// handoffWebBaseURL returns the configured React workbench base URL for
// post-create redirects. Mirrors the same logic in internal/server/routes.go.
func handoffWebBaseURL() string {
	base := os.Getenv("RELAY_WEB_BASE_URL")
	if base == "" {
		base = "http://localhost:3000"
	}
	return strings.TrimRight(base, "/")
}

type HandoffsHandler struct {
	store       *store.Store
	log         *slog.Logger
	eventHub    *events.Hub
	runsHandler *RunsHandler
}

func NewHandoffsHandler(s *store.Store, log *slog.Logger, hub ...*events.Hub) *HandoffsHandler {
	var eventHub *events.Hub
	if len(hub) > 0 {
		eventHub = hub[0]
	}
	return &HandoffsHandler{store: s, log: log, eventHub: eventHub}
}

// SetRunsHandler provides access to run-level operations for auto-setup.
func (h *HandoffsHandler) SetRunsHandler(rh *RunsHandler) {
	h.runsHandler = rh
}

func (h *HandoffsHandler) publishRunEvent(runID int64, kind, source, status string) {
	if h == nil || h.eventHub == nil {
		return
	}
	h.eventHub.Publish(events.RunEvent{
		RunID:  runID,
		Kind:   kind,
		Source: source,
		Status: status,
	})
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

	// Capture run-created Git baseline (best-effort, non-blocking)
	gitSnap := repos.CaptureGitSnapshot(repo.Path, "run_created")
	baselineArtifact := repos.GitBaselineArtifact{
		RunCreated:                 gitSnap,
		AuthoritativeBaselineStage: "run_created",
	}
	if gitSnap.HeadSHA != "" {
		baselineArtifact.AuthoritativeBaselineSHA = gitSnap.HeadSHA
	}
	baselineJSON, _ := json.MarshalIndent(baselineArtifact, "", "  ")

	if gitSnap.IsGitRepo && gitSnap.HeadSHA != "" {
		if _, err := h.store.UpdateRunBranch(run.ID, gitSnap.Branch, gitSnap.HeadSHA, gitSnap.HeadSHA); err != nil {
			h.log.Warn("update run branch with baseline", "run_id", run.ID, "error", err)
		}
		h.store.CreateEvent(run.ID, "info", "Git baseline captured at run creation: "+shortSHA(gitSnap.HeadSHA)+" on "+gitSnap.Branch)
		h.publishRunEvent(run.ID, events.KindRunSummary, "handoff", "baseline")
	} else {
		h.store.CreateEvent(run.ID, "warn", "Git baseline unavailable: "+gitSnap.Error)
		h.publishRunEvent(run.ID, events.KindRunSummary, "handoff", "warning")
	}

	if baselineJSON != nil {
		if bp, err := artifacts.Write(run.ID, "git_baseline_json", pipeline.ArtifactFilename("git_baseline_json"), baselineJSON); err == nil {
			h.store.CreateArtifact(run.ID, "git_baseline_json", bp, "application/json")
		} else {
			h.log.Warn("write git baseline artifact", "run_id", run.ID, "error", err)
		}
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
	h.publishRunEvent(run.ID, events.KindRunSummary, "handoff", "created")

	h.log.Info("handoff created", "run_id", run.ID, "repo", repo.Name)

	// Auto-run setup pipeline
	if h.runsHandler != nil {
		result := h.runsHandler.prepareRunForReview(run.ID)
		if result.Blocked {
			h.log.Info("auto-setup blocked by intake review", "run_id", run.ID, "blockers", result.Blockers)
		} else {
			h.log.Info("auto-setup complete", "run_id", run.ID,
				"prompt", result.PromptGenerated, "packet", result.PacketGenerated)
		}
	} else {
		h.log.Warn("auto-setup skipped: runsHandler not set", "run_id", run.ID)
	}

	// Redirect to React workbench intake route after successful run creation.
	intakeURL := handoffWebBaseURL() + "/runs/" + strconv.FormatInt(run.ID, 10) + "/intake"
	http.Redirect(w, r, intakeURL, http.StatusSeeOther)
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

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
