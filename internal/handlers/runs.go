package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"
	"relay/internal/views"

	"github.com/go-chi/chi/v5"
)

type agentCommandRunner func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult

type RunsHandler struct {
	store               *store.Store
	log                 *slog.Logger
	runAgentCommandArgs agentCommandRunner
}

func NewRunsHandler(s *store.Store, log *slog.Logger) *RunsHandler {
	return &RunsHandler{
		store:               s,
		log:                 log,
		runAgentCommandArgs: pipeline.RunLocalAgentCommandArgs,
	}
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

	// Compute prompt size estimate
	agentPromptEstimate := ""
	if agentPromptPreview != "" {
		est := pipeline.EstimateTokens(agentPromptPreview)
		agentPromptEstimate = formatPromptEstimate(est)
	}

	// Compute handoff preflight
	preflightStatus := ""
	var preflightChecks []views.HandoffPreflightCheckView
	if repo != nil && run != nil {
		agentPromptPath := ""
		if a := findArtifactByKind(artifactsList, "agent_prompt"); a != nil {
			agentPromptPath = a.Path
		}
		opencodePacketPath := ""
		if a := findArtifactByKind(artifactsList, "opencode_handoff_packet"); a != nil {
			opencodePacketPath = a.Path
		}
		requiredPaths := make(map[string]string)
		if agentPromptPath != "" {
			requiredPaths["agent_prompt"] = agentPromptPath
		}
		preflight := pipeline.BuildHandoffPreflight(repo.Path, run.BranchName, run.SelectedModel, agentPromptPath, opencodePacketPath, requiredPaths)
		preflightStatus = preflight.Status
		for _, c := range preflight.Checks {
			preflightChecks = append(preflightChecks, views.HandoffPreflightCheckView{
				Key:     c.Key,
				Status:  c.Status,
				Summary: c.Summary,
			})
		}
	}

	// Build OpenCode adapter preview (best-effort)
	openCodeBinary := ""
	openCodeModel := ""
	openCodeAgent := ""
	openCodeVariant := ""
	openCodeStdinSource := ""
	openCodeStdinBytes := 0
	openCodeWorkDir := ""
	var openCodeArgs []string
	openCodeCommandPreview := ""
	openCodeAdapterError := ""

	if repo != nil {
		invocation, err := h.buildOpenCodeInvocationForRun(id)
		if err == nil {
			openCodeBinary = invocation.Binary
			openCodeArgs = invocation.Args
			openCodeWorkDir = invocation.WorkDir
			openCodeModel = invocation.Model
			openCodeAgent = invocation.Agent
			openCodeVariant = invocation.Variant
			openCodeStdinSource = invocation.StdinSource
			openCodeStdinBytes = invocation.StdinBytes
			openCodeCommandPreview = invocation.Preview
		} else {
			openCodeAdapterError = err.Error()
		}
	}
	// Load latest execution
	hasOpenCodeExecution := false
	openCodeExecStatus := ""
	openCodeExecExitCode := ""
	openCodeExecStarted := ""
	openCodeExecFinished := ""
	hasOpenCodeStdout := false
	hasOpenCodeStderr := false
	hasOpenCodeCombinedLog := false

	if exec, err := h.store.GetLatestAgentExecutionByRun(id); err == nil {
		hasOpenCodeExecution = true
		openCodeExecStatus = exec.Status
		if exec.ExitCode.Valid {
			openCodeExecExitCode = strconv.FormatInt(exec.ExitCode.Int64, 10)
		}
		if exec.StartedAt.Valid {
			openCodeExecStarted = exec.StartedAt.String
		}
		if exec.FinishedAt.Valid {
			openCodeExecFinished = exec.FinishedAt.String
		}
		for _, a := range artifactsList {
			if a.Kind == "opencode_stdout" {
				hasOpenCodeStdout = true
			} else if a.Kind == "opencode_stderr" {
				hasOpenCodeStderr = true
			} else if a.Kind == "opencode_combined_log" {
				hasOpenCodeCombinedLog = true
			}
		}
	}

	dryRunPreview := readArtifactPreview(id, "opencode_dry_run_json")

	previews := views.RunPreviews{
		OriginalHandoff:            originalPreview,
		ValidationJSON:             readArtifactPreview(id, "handoff_validation_json"),
		AgentPrompt:                agentPromptPreview,
		OpenCodePacket:             readArtifactPreview(id, "opencode_handoff_packet"),
		AgentPromptEstimate:        agentPromptEstimate,
		HandoffPreflightStatus:     preflightStatus,
		HandoffPreflightChecks:     preflightChecks,
		OpenCodeCommandPreview:     openCodeCommandPreview,
		OpenCodeExecutionStatus:    openCodeExecStatus,
		OpenCodeExecutionExitCode:  openCodeExecExitCode,
		OpenCodeExecutionStarted:   openCodeExecStarted,
		OpenCodeExecutionFinished:  openCodeExecFinished,
		OpenCodeStdoutArtifactID:   0,
		OpenCodeStderrArtifactID:   0,
		OpenCodeCombinedArtifactID: 0,
		HasOpenCodeExecution:       hasOpenCodeExecution,
		OpenCodeBinary:             openCodeBinary,
		OpenCodeArgs:               openCodeArgs,
		OpenCodeWorkDir:            openCodeWorkDir,
		OpenCodeModel:              openCodeModel,
		OpenCodeAgent:              openCodeAgent,
		OpenCodeVariant:            openCodeVariant,
		OpenCodeStdinSource:        openCodeStdinSource,
		OpenCodeStdinBytes:         openCodeStdinBytes,
		OpenCodeAdapterError:       openCodeAdapterError,
		OpenCodeDryRunPreview:      dryRunPreview,
		HasOpenCodeDryRun:          dryRunPreview != "",
		HasOpenCodeStdout:          hasOpenCodeStdout,
		HasOpenCodeStderr:          hasOpenCodeStderr,
		HasOpenCodeCombinedLog:     hasOpenCodeCombinedLog,
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

	// Determine active step — default to intake, override with valid ?step=
	activeStep := normalizeRunStep(r.URL.Query().Get("step"))

	views.RunDetail(run, repo, artifactsList, checksList, eventsList, previews, &intakeReview, activeStep).Render(r.Context(), w)
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
	case "dry-run-opencode-go":
		h.dryRunOpenCodeGo(w, r, id)
	case "start-opencode-go":
		h.startOpenCodeGo(w, r, id)
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

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
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

	prompt := pipeline.BuildCompactAgentPrompt(string(handoffData))

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

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=prompt", http.StatusSeeOther)
}

func (h *RunsHandler) markStatus(w http.ResponseWriter, r *http.Request, runID int64, status string) {
	h.store.UpdateRunStatus(runID, status)

	h.store.CreateEvent(runID, "info", "Run status changed to "+status)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
}

// persistAgentResult saves agent result artifacts and creates checks/events.
// Used from both manual submission and OpenCode execution.
func (h *RunsHandler) persistAgentResult(runID int64, raw string) error {
	result := pipeline.ParseAgentResult(raw)

	rawPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte(raw))
	if err != nil {
		return fmt.Errorf("write agent result raw: %w", err)
	}
	h.store.CreateArtifact(runID, "agent_result_raw", rawPath, "text/plain")

	resultJSON, err := result.JSON()
	if err != nil {
		return fmt.Errorf("marshal agent result json: %w", err)
	}
	jsonPath, err := artifacts.Write(runID, "agent_result_json", pipeline.ArtifactFilename("agent_result_json"), resultJSON)
	if err != nil {
		return fmt.Errorf("write agent result json: %w", err)
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
	return nil
}

// buildOpenCodeInvocationForRun builds the OpenCode invocation from run data.
// Shared between Dry Run and Start to avoid drift.
func (h *RunsHandler) buildOpenCodeInvocationForRun(runID int64) (pipeline.OpenCodeRunInvocation, error) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		return pipeline.OpenCodeRunInvocation{}, err
	}

	repo, err := h.store.GetRepo(run.RepoID)
	if err != nil {
		return pipeline.OpenCodeRunInvocation{}, err
	}

	artifactsList, err := h.store.ListArtifactsByRun(runID)
	if err != nil {
		return pipeline.OpenCodeRunInvocation{}, err
	}

	agentPromptPath := ""
	packetPath := ""
	for _, a := range artifactsList {
		switch a.Kind {
		case "agent_prompt":
			agentPromptPath = a.Path
		case "opencode_handoff_packet":
			packetPath = a.Path
		}
	}

	if agentPromptPath == "" {
		return pipeline.OpenCodeRunInvocation{}, fmt.Errorf("Agent Prompt artifact not found; generate it first")
	}
	if packetPath == "" {
		return pipeline.OpenCodeRunInvocation{}, fmt.Errorf("OpenCode handoff packet not found; generate it first")
	}

	promptData, err := os.ReadFile(agentPromptPath)
	if err != nil {
		return pipeline.OpenCodeRunInvocation{}, fmt.Errorf("read agent prompt artifact: %w", err)
	}

	cfg := pipeline.OpenCodeRunConfigFromEnv()

	return pipeline.BuildOpenCodeRunInvocation(cfg, pipeline.OpenCodeRunInput{
		RepoPath:        repo.Path,
		BranchName:      run.BranchName,
		SelectedModel:   run.SelectedModel,
		AgentPromptPath: agentPromptPath,
		AgentPromptText: string(promptData),
		PacketPath:      packetPath,
		ArtifactDir:     artifacts.Dir(runID),
	})
}

func (h *RunsHandler) dryRunOpenCodeGo(w http.ResponseWriter, r *http.Request, runID int64) {
	invocation, err := h.buildOpenCodeInvocationForRun(runID)
	if err != nil {
		h.store.CreateEvent(runID, "warn", "OpenCode dry run failed: "+err.Error())
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
		return
	}

	preview := struct {
		Binary          string   `json:"binary"`
		Args            []string `json:"args"`
		WorkDir         string   `json:"work_dir"`
		StdinSource     string   `json:"stdin_source"`
		StdinBytes      int      `json:"stdin_bytes"`
		AgentPromptPath string   `json:"agent_prompt_path"`
		PacketPath      string   `json:"packet_path"`
		Model           string   `json:"model"`
		Agent           string   `json:"agent"`
		Variant         string   `json:"variant,omitempty"`
		Preview         string   `json:"preview"`
	}{
		Binary:          invocation.Binary,
		Args:            invocation.Args,
		WorkDir:         invocation.WorkDir,
		StdinSource:     invocation.StdinSource,
		StdinBytes:      invocation.StdinBytes,
		AgentPromptPath: invocation.AgentPromptPath,
		PacketPath:      invocation.PacketPath,
		Model:           invocation.Model,
		Agent:           invocation.Agent,
		Variant:         invocation.Variant,
		Preview:         invocation.Preview,
	}

	data, _ := json.MarshalIndent(preview, "", "  ")
	p, err := artifacts.Write(runID, "opencode_dry_run_json", pipeline.ArtifactFilename("opencode_dry_run_json"), data)
	if err != nil {
		h.log.Error("write opencode dry run preview", "error", err)
		http.Error(w, "failed to save dry run preview", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "opencode_dry_run_json", p, "application/json")
	h.store.CreateEvent(runID, "info", "OpenCode dry run preview prepared")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
}

func (h *RunsHandler) startOpenCodeGo(w http.ResponseWriter, r *http.Request, runID int64) {
	// Build the real OpenCode adapter invocation
	invocation, err := h.buildOpenCodeInvocationForRun(runID)
	if err != nil {
		h.store.CreateEvent(runID, "warn", "OpenCode start blocked: "+err.Error())
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
		return
	}

	// Confirm repo path exists
	if info, err := os.Stat(invocation.WorkDir); err != nil || !info.IsDir() {
		h.store.CreateEvent(runID, "warn", "Repo path does not exist: "+invocation.WorkDir)
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
		return
	}

	// Create execution record with status starting
	exec, err := h.store.CreateAgentExecution(runID, "opencode_go", "starting", invocation.Preview)
	if err != nil {
		h.log.Error("create agent execution record", "error", err)
		http.Error(w, "failed to create execution record", http.StatusInternalServerError)
		return
	}

	// Update to running
	startedAt := time.Now().Format("2006-01-02 15:04:05")
	h.store.UpdateAgentExecutionStatus(exec.ID, "running", nil, &startedAt, nil, nil, nil, nil, nil, nil)

	h.store.CreateEvent(runID, "info", "OpenCode Go execution started")

	// Run command synchronously with timeout using the real adapter
	runResult := h.runAgentCommandArgs(
		r.Context(),
		invocation.WorkDir,
		invocation.Binary,
		invocation.Args,
		invocation.Stdin,
		pipeline.DefaultAgentCommandTimeout,
	)

	// Write stdout/stderr/combined artifacts
	stdoutPath := ""
	if runResult.Stdout != "" {
		p, err := artifacts.Write(runID, "opencode_stdout", pipeline.ArtifactFilename("opencode_stdout"), []byte(runResult.Stdout))
		if err == nil {
			h.store.CreateArtifact(runID, "opencode_stdout", p, "text/plain")
			stdoutPath = p
		}
	}

	stderrPath := ""
	if runResult.Stderr != "" {
		p, err := artifacts.Write(runID, "opencode_stderr", pipeline.ArtifactFilename("opencode_stderr"), []byte(runResult.Stderr))
		if err == nil {
			h.store.CreateArtifact(runID, "opencode_stderr", p, "text/plain")
			stderrPath = p
		}
	}

	combinedLog := runResult.Stdout
	if runResult.Stderr != "" {
		if combinedLog != "" {
			combinedLog += "\n\n--- STDERR ---\n\n"
		}
		combinedLog += runResult.Stderr
	}

	combinedPath := ""
	if combinedLog != "" {
		p, err := artifacts.Write(runID, "opencode_combined_log", pipeline.ArtifactFilename("opencode_combined_log"), []byte(combinedLog))
		if err == nil {
			h.store.CreateArtifact(runID, "opencode_combined_log", p, "text/plain")
			combinedPath = p
		}
	}

	// Determine execution status
	execStatus := "completed"
	if runResult.TimedOut {
		execStatus = "failed"
	} else if runResult.ExitCode != 0 {
		execStatus = "failed"
	}

	ec := int64(runResult.ExitCode)
	startedStr := runResult.StartedAt.Format("2006-01-02 15:04:05")
	finishedStr := runResult.FinishedAt.Format("2006-01-02 15:04:05")

	var errPtr *string
	if runResult.Error != "" {
		errPtr = &runResult.Error
	}

	h.store.UpdateAgentExecutionStatus(exec.ID, execStatus, &ec, &startedStr, &finishedStr,
		&stdoutPath, &stderrPath, &combinedPath, nil, errPtr)

	// Extract assistant text from JSONL stdout
	if runResult.Stdout != "" {
		assistantText := pipeline.ExtractOpenCodeAssistantText(runResult.Stdout)
		parsed := pipeline.ParseAgentResult(assistantText)
		if parsed.Status == pipeline.AgentResultDone || parsed.Status == pipeline.AgentResultBlocked {
			if err := h.persistAgentResult(runID, assistantText); err != nil {
				h.log.Warn("failed to persist opencode agent result", "error", err)
			}
			h.store.CreateEvent(runID, "info", "OpenCode Go execution completed with result: "+string(parsed.Status))
			h.log.Info("opencode go execution completed", "run_id", runID, "exit_code", runResult.ExitCode, "status", parsed.Status)
			http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
			return
		}
	}

	eventMsg := "OpenCode Go execution completed with exit code " + strconv.Itoa(runResult.ExitCode)
	if runResult.TimedOut {
		eventMsg = "OpenCode Go execution timed out"
	}
	h.store.CreateEvent(runID, "info", eventMsg)

	h.log.Info("opencode go execution completed", "run_id", runID, "exit_code", runResult.ExitCode)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
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

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
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

	// Build artifact manifest from stored artifacts
	kindPaths := make(map[string]string)
	for _, a := range artifactsList {
		if a.Kind == "agent_prompt" || a.Kind == "original_handoff" || a.Kind == "handoff_validation_json" {
			kindPaths[a.Kind] = a.Path
		}
	}
	packet.Artifacts = pipeline.BuildArtifactManifest(artifacts.Dir(run.ID), kindPaths)

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

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=packet", http.StatusSeeOther)
}

func findArtifactByKind(artifacts []store.Artifact, kind string) *store.Artifact {
	for i := range artifacts {
		if artifacts[i].Kind == kind {
			return &artifacts[i]
		}
	}
	return nil
}

func hasArtifactKind(artifacts []store.Artifact, kind string) bool {
	for _, a := range artifacts {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

func formatPromptEstimate(est pipeline.PromptEstimate) string {
	kb := float64(est.Bytes) / 1024.0
	return fmt.Sprintf("%.1f KB (~%d tokens, approximate)", kb, est.ApproxTokens)
}

func hasCheckKind(checks []store.Check, kind string) bool {
	for _, c := range checks {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// normalizeRunStep maps a step query value to a known step identifier.
// Invalid or empty values default to "intake".
func normalizeRunStep(step string) string {
	switch step {
	case "intake", "prompt", "packet", "handoff", "result", "validation", "audit":
		return step
	default:
		return "intake"
	}
}

func defaultActiveRunStep(_ []store.Artifact, _ []store.Check) string {
	return "intake"
}

func (h *RunsHandler) submitAgentResult(w http.ResponseWriter, r *http.Request, runID int64) {
	raw := strings.TrimSpace(r.FormValue("agent_result_text"))
	if raw == "" {
		http.Error(w, "agent result text is required", http.StatusBadRequest)
		return
	}

	if err := h.persistAgentResult(runID, raw); err != nil {
		h.log.Error("submit agent result", "error", err)
		http.Error(w, "failed to save agent result", http.StatusInternalServerError)
		return
	}

	h.log.Info("agent result submitted", "run_id", runID)
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
}
