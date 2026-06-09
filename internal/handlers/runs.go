package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

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

func readAgentPromptPreview(runID int64) string {
	data := readArtifactPreview(runID, "agent_prompt")
	if data != "" {
		return data
	}
	return readArtifactPreview(runID, "ready_prompt")
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

	originalPreview := readArtifactPreview(id, "original_handoff")
	agentPromptPreview := readAgentPromptPreview(id)

	previews := views.RunPreviews{
		OriginalHandoff: originalPreview,
		ValidationJSON:  readArtifactPreview(id, "handoff_validation_json"),
		AgentPrompt:     agentPromptPreview,
	}

	if originalPreview != "" && agentPromptPreview != "" {
		pipelineDiff := pipeline.BuildPreviewDiff(originalPreview, agentPromptPreview, 300)
		var diffLines []views.PreviewDiffLine
		for _, l := range pipelineDiff.Lines {
			diffLines = append(diffLines, views.PreviewDiffLine{Kind: l.Kind, Text: l.Text})
		}
		previews.AgentPromptDiff = views.PreviewDiff{
			Lines:     diffLines,
			Truncated: pipelineDiff.Truncated,
		}
	}

	// compute intake review
	var intakeReview pipeline.IntakeReview
	if handoffText := readArtifactPreview(id, "original_handoff"); handoffText != "" {
		repoPath := ""
		if repo != nil {
			repoPath = repo.Path
		}
		repoDefaults := ""
		if repo != nil {
			repoDefaults = repo.DefaultValidationCommands
		}
		metadata := pipeline.ParseHandoffMetadata(handoffText, repoDefaults)
		intakeReview = pipeline.BuildIntakeReview(metadata, repoPath)
	}

	views.RunDetail(run, repo, artifactsList, checksList, eventsList, previews, &intakeReview).Render(r.Context(), w)
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
		h.runValidation(w, r, id)
	case "inspect-diff":
		h.notImplemented(w, r, id, "Git diff inspection is not yet implemented")
	case "generate-audit-packet":
		h.notImplemented(w, r, id, "Audit packet generation is not yet implemented")
	case "submit-agent-result":
		h.submitAgentResult(w, r, id)
	case "generate-opencode-packet":
		h.generateOpenCodePacket(w, r, id)
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

	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, _ := h.store.GetRepo(run.RepoID)
	repoPath := ""
	repoDefaults := ""
	if repo != nil {
		repoPath = repo.Path
		repoDefaults = repo.DefaultValidationCommands
	}

	metadata := pipeline.ParseHandoffMetadata(string(handoffData), repoDefaults)
	review := pipeline.BuildIntakeReview(metadata, repoPath)

	if len(review.Blockers) > 0 {
		h.store.CreateEvent(runID, "warn",
			"Cannot generate Agent Prompt while Intake Review has blockers. Fix repo selection or handoff scope first.")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
		return
	}

	prompt := pipeline.PreparePrompt(string(handoffData))

	promptPath, err := artifacts.Write(runID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt"), []byte(prompt))
	if err != nil {
		h.log.Error("write agent prompt", "error", err)
		http.Error(w, "failed to save agent prompt", http.StatusInternalServerError)
		return
	}

	h.store.CreateArtifact(runID, "agent_prompt", promptPath, "text/plain")

	h.store.UpdateRunStatus(runID, "ready")

	h.store.CreateEvent(runID, "info", "Agent prompt generated")

	h.log.Info("agent prompt prepared", "run_id", runID)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) markStatus(w http.ResponseWriter, r *http.Request, runID int64, status string) {
	h.store.UpdateRunStatus(runID, status)

	h.store.CreateEvent(runID, "info", "Run status changed to "+status)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) runValidation(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		h.log.Error("get run for validation", "error", err)
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, err := h.store.GetRepo(run.RepoID)
	if err != nil {
		h.log.Error("get repo for validation", "error", err)
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if repo.Path == "" {
		h.store.CreateEvent(runID, "warn", "No repo path configured for validation commands")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
		return
	}

	info, err := os.Stat(repo.Path)
	if err != nil || !info.IsDir() {
		h.store.CreateEvent(runID, "warn", "Repo path does not exist or is not a directory: "+repo.Path)
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
		return
	}

	handoffData, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		h.log.Error("read handoff for validation commands", "error", err)
		handoffData = []byte{}
	}

	commands := pipeline.ExtractValidationCommands(string(handoffData), repo.DefaultValidationCommands)
	if len(commands) == 0 {
		h.store.CreateEvent(runID, "warn", "No validation commands found")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
		return
	}

	var results []pipeline.CommandRunResult
	allPassed := true
	var combinedStdout, combinedStderr strings.Builder

	for _, cmd := range commands {
		result := pipeline.RunValidationCommand(context.Background(), repo.Path, cmd, pipeline.DefaultValidationCommandTimeout)
		results = append(results, result)

		if combinedStdout.Len() > 0 {
			combinedStdout.WriteString("\n---\n")
		}
		combinedStdout.WriteString("$ " + cmd.Command + "\n")
		combinedStdout.WriteString(result.Stdout)

		if combinedStderr.Len() > 0 {
			combinedStderr.WriteString("\n---\n")
		}
		combinedStderr.WriteString("$ " + cmd.Command + "\n")
		combinedStderr.WriteString(result.Stderr)

		if result.ExitCode != 0 || result.TimedOut {
			allPassed = false
		}
	}

	aggregate := struct {
		Status   string                      `json:"status"`
		RepoPath string                      `json:"repo_path"`
		Commands []pipeline.CommandRunResult `json:"commands"`
	}{
		Status:   "fail",
		RepoPath: repo.Path,
		Commands: results,
	}
	if allPassed {
		aggregate.Status = "pass"
	}

	aggregateJSON, _ := json.MarshalIndent(aggregate, "", "  ")

	jsonPath, err := artifacts.Write(runID, "validation_run_json", pipeline.ArtifactFilename("validation_run_json"), aggregateJSON)
	if err != nil {
		h.log.Error("write validation run json", "error", err)
		http.Error(w, "failed to save validation result", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "validation_run_json", jsonPath, "application/json")

	stdoutPath, err := artifacts.Write(runID, "validation_stdout", pipeline.ArtifactFilename("validation_stdout"), []byte(combinedStdout.String()))
	if err != nil {
		h.log.Error("write validation stdout", "error", err)
	} else {
		h.store.CreateArtifact(runID, "validation_stdout", stdoutPath, "text/plain")
	}

	stderrPath, err := artifacts.Write(runID, "validation_stderr", pipeline.ArtifactFilename("validation_stderr"), []byte(combinedStderr.String()))
	if err != nil {
		h.log.Error("write validation stderr", "error", err)
	} else {
		h.store.CreateArtifact(runID, "validation_stderr", stderrPath, "text/plain")
	}

	h.store.DeleteChecksByRunKind(runID, "validation_run")

	for _, result := range results {
		status := "pass"
		if result.ExitCode != 0 || result.TimedOut {
			status = "fail"
		}
		summary := result.Label + " passed"
		if status == "fail" {
			if result.TimedOut {
				summary = result.Label + " timed out"
			} else {
				summary = result.Label + " failed with exit code " + strconv.Itoa(result.ExitCode)
			}
		}
		detailsJSON, _ := json.Marshal(result)
		h.store.CreateCheck(runID, "validation_run", status, summary, string(detailsJSON))
	}

	if allPassed {
		h.store.UpdateRunStatus(runID, "validation_passed")
		h.store.CreateEvent(runID, "info", "Validation commands passed")
	} else {
		h.store.UpdateRunStatus(runID, "validation_failed")
		h.store.CreateEvent(runID, "info", "Validation commands failed")
	}

	h.log.Info("validation commands executed", "run_id", runID, "status", aggregate.Status, "commands", len(commands))

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) notImplemented(w http.ResponseWriter, r *http.Request, runID int64, msg string) {
	h.store.CreateEvent(runID, "warn", msg)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) generateOpenCodePacket(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, err := h.store.GetRepo(run.RepoID)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	artifactsList, err := h.store.ListArtifactsByRun(runID)
	if err != nil {
		http.Error(w, "failed to list artifacts", http.StatusInternalServerError)
		return
	}

	var promptArtifact *store.Artifact
	for i := range artifactsList {
		if artifactsList[i].Kind == "agent_prompt" {
			promptArtifact = &artifactsList[i]
			break
		}
	}
	if promptArtifact == nil {
		for i := range artifactsList {
			if artifactsList[i].Kind == "ready_prompt" {
				promptArtifact = &artifactsList[i]
				break
			}
		}
	}
	if promptArtifact == nil {
		http.Error(w, "generate agent prompt first", http.StatusBadRequest)
		return
	}

	packet := pipeline.NewOpenCodeHandoffPacket(
		run.ID,
		repo.Path,
		run.BranchName,
		run.SelectedModel,
		run.RecommendedModel,
		promptArtifact.Path,
		artifacts.Dir(run.ID),
	)

	packetJSON, err := pipeline.MarshalOpenCodeHandoffPacket(packet)
	if err != nil {
		h.log.Error("marshal opencode packet", "error", err)
		http.Error(w, "failed to marshal packet", http.StatusInternalServerError)
		return
	}

	packetPath, err := artifacts.Write(runID, "opencode_handoff_packet", pipeline.ArtifactFilename("opencode_handoff_packet"), packetJSON)
	if err != nil {
		h.log.Error("write opencode packet", "error", err)
		http.Error(w, "failed to save packet", http.StatusInternalServerError)
		return
	}

	h.store.CreateArtifact(runID, "opencode_handoff_packet", packetPath, "application/json")

	h.store.CreateEvent(runID, "info", "OpenCode handoff packet generated")

	h.log.Info("opencode handoff packet generated", "run_id", runID)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}

func (h *RunsHandler) submitAgentResult(w http.ResponseWriter, r *http.Request, runID int64) {
	raw := strings.TrimSpace(r.FormValue("agent_result_text"))
	if raw == "" {
		http.Error(w, "agent result text is required", http.StatusBadRequest)
		return
	}

	result := pipeline.ParseAgentResult(raw)

	rawPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte(raw))
	if err != nil {
		h.log.Error("write agent result raw", "error", err)
		http.Error(w, "failed to save agent result", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "agent_result_raw", rawPath, "text/plain")

	resultJSON, err := result.JSON()
	if err != nil {
		h.log.Error("marshal agent result json", "error", err)
		http.Error(w, "failed to parse agent result", http.StatusInternalServerError)
		return
	}
	jsonPath, err := artifacts.Write(runID, "agent_result_json", pipeline.ArtifactFilename("agent_result_json"), resultJSON)
	if err != nil {
		h.log.Error("write agent result json", "error", err)
		http.Error(w, "failed to save agent result metadata", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "agent_result_json", jsonPath, "application/json")

	h.store.DeleteChecksByRunKind(runID, "agent_result")

	var checkStatus, checkSummary, runStatus, eventMsg string
	switch result.Status {
	case pipeline.AgentResultDone:
		checkStatus = "pass"
		checkSummary = "Agent reported DONE"
		runStatus = "agent_done"
		eventMsg = "Agent result submitted: DONE"
	case pipeline.AgentResultBlocked:
		checkStatus = "fail"
		checkSummary = "Agent reported BLOCKED"
		runStatus = "agent_blocked"
		eventMsg = "Agent result submitted: BLOCKED"
	default:
		checkStatus = "warn"
		checkSummary = "Agent result status unknown"
		runStatus = "agent_result_needs_review"
		eventMsg = "Agent result submitted: UNKNOWN"
	}

	h.store.CreateCheck(runID, "agent_result", checkStatus, checkSummary, string(resultJSON))
	h.store.UpdateRunStatus(runID, runStatus)
	h.store.CreateEvent(runID, "info", eventMsg)

	h.log.Info("agent result submitted", "run_id", runID, "status", result.Status)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10), http.StatusSeeOther)
}
