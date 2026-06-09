package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"
	"relay/internal/views"

	"github.com/go-chi/chi/v5"
)

type RunsHandler struct {
	store *store.Store
	log   *slog.Logger
}

func NewRunsHandler(s *store.Store, log *slog.Logger) *RunsHandler {
	return &RunsHandler{store: s, log: log}
}

func readArtifactPreview(runID int64, kind string) string {
	data, err := artifacts.Read(runID, kind, pipeline.ArtifactFilename(kind))
	if err != nil {
		return ""
	}
	return string(data)
}

func (h *RunsHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, _ := h.store.GetRepo(run.RepoID)

	artifactsList, _ := h.store.ListArtifactsByRun(id)
	checksList, _ := h.store.ListChecksByRun(id)
	eventsList, _ := h.store.ListEventsByRun(id)

	previews := views.RunPreviews{
		OriginalHandoff: readArtifactPreview(id, "original_handoff"),
		ValidationJSON:  readArtifactPreview(id, "handoff_validation_json"),
		ReadyPrompt:     readArtifactPreview(id, "ready_prompt"),
	}

	views.RunDetail(run, repo, artifactsList, checksList, eventsList, previews).Render(r.Context(), w)
}

func (h *RunsHandler) Action(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	action := r.FormValue("action")

	switch action {
	case "validate-handoff":
		h.validateHandoff(w, r, id)
	case "prepare-prompt":
		h.preparePrompt(w, r, id)
	case "mark-accepted":
		h.markStatus(w, r, id, "accepted")
	case "mark-needs-cleanup":
		h.markStatus(w, r, id, "needs_cleanup")
	case "run-agent":
		h.notImplemented(w, r, id, "Agent execution is not yet implemented")
	case "run-validation":
		h.notImplemented(w, r, id, "Validation command execution is not yet implemented")
	case "inspect-diff":
		h.notImplemented(w, r, id, "Git diff inspection is not yet implemented")
	case "generate-audit-packet":
		h.notImplemented(w, r, id, "Audit packet generation is not yet implemented")
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func (h *RunsHandler) validateHandoff(w http.ResponseWriter, r *http.Request, runID int64) {
	handoffData, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		h.log.Error("read handoff for validation", "error", err)
		http.Error(w, "handoff not found on disk", http.StatusInternalServerError)
		return
	}

	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	report := pipeline.ValidateHandoff(string(handoffData), run.RecommendedModel)

	reportJSON, _ := report.JSON()
	reportPath, err := artifacts.Write(runID, "handoff_validation_json", pipeline.ArtifactFilename("handoff_validation_json"), reportJSON)
	if err != nil {
		h.log.Error("write validation report", "error", err)
		http.Error(w, "failed to save validation report", http.StatusInternalServerError)
		return
	}

	h.store.CreateArtifact(runID, "handoff_validation_json", reportPath, "application/json")

	h.store.DeleteChecksByRunKind(runID, "validation")

	for _, c := range report.Checks {
		detailsJSON, _ := json.Marshal(c)
		h.store.CreateCheck(runID, "validation", c.Status, c.Summary, string(detailsJSON))
	}

	newStatus := "draft"
	switch report.Status {
	case "ready":
		newStatus = "validated"
	case "needs_fix":
		newStatus = "needs_cleanup"
	case "needs_review":
		newStatus = "needs_review"
	}
	h.store.UpdateRunStatus(runID, newStatus)

	h.store.CreateEvent(runID, "info", "Handoff validation completed: "+report.Status)

	h.log.Info("handoff validated", "run_id", runID, "status", report.Status)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) preparePrompt(w http.ResponseWriter, r *http.Request, runID int64) {
	handoffData, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		h.log.Error("read handoff for prompt prep", "error", err)
		http.Error(w, "handoff not found on disk", http.StatusInternalServerError)
		return
	}

	prompt := pipeline.PreparePrompt(string(handoffData))

	promptPath, err := artifacts.Write(runID, "ready_prompt", pipeline.ArtifactFilename("ready_prompt"), []byte(prompt))
	if err != nil {
		h.log.Error("write ready prompt", "error", err)
		http.Error(w, "failed to save ready prompt", http.StatusInternalServerError)
		return
	}

	h.store.CreateArtifact(runID, "ready_prompt", promptPath, "text/plain")

	h.store.UpdateRunStatus(runID, "ready")

	h.store.CreateEvent(runID, "info", "Ready prompt generated")

	h.log.Info("ready prompt prepared", "run_id", runID)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) markStatus(w http.ResponseWriter, r *http.Request, runID int64, status string) {
	h.store.UpdateRunStatus(runID, status)

	h.store.CreateEvent(runID, "info", "Run status changed to "+status)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) notImplemented(w http.ResponseWriter, r *http.Request, runID int64, msg string) {
	h.store.CreateEvent(runID, "warn", msg)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}
