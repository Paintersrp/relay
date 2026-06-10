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
	store                *store.Store
	log                  *slog.Logger
	runAgentCommandArgs  agentCommandRunner
	launchAgentExecution func(func())
}

func NewRunsHandler(s *store.Store, log *slog.Logger) *RunsHandler {
	return &RunsHandler{
		store:               s,
		log:                 log,
		runAgentCommandArgs: pipeline.RunLocalAgentCommandArgs,
		launchAgentExecution: func(fn func()) {
			go fn()
		},
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
	openCodeThinking := "max"

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
	openCodeFailureHint := ""

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
		// Compute failure hint if execution failed
		if exec.Status == "failed" && openCodeBinary != "" {
			exitCode := 0
			if exec.ExitCode.Valid {
				exitCode = int(exec.ExitCode.Int64)
			}
			runResult := pipeline.AgentCommandRunResult{
				ExitCode: exitCode,
				Stderr:   readArtifactPreview(id, "opencode_stderr"),
				Stdout:   readArtifactPreview(id, "opencode_stdout"),
				TimedOut: exitCode == -2,
				Error:    exec.Error.String,
			}
			invocation := pipeline.OpenCodeRunInvocation{
				Binary:  openCodeBinary,
				Args:    openCodeArgs,
				WorkDir: openCodeWorkDir,
				Model:   openCodeModel,
				Agent:   openCodeAgent,
			}
			openCodeFailureHint = pipeline.OpenCodeFailureHint(runResult, invocation)
		}
	}

	// Build transcript and parsed result for Step 5
	var openCodeTranscript []views.OpenCodeTranscriptEventView
	openCodeParsedResultStatus := ""
	openCodeParsedBuildStatus := ""
	openCodeParsedTestStatus := ""
	openCodeParsedLOCChanged := ""
	openCodeParsedResultRaw := ""

	if hasOpenCodeExecution {
		stdoutPreview := readArtifactPreview(id, "opencode_stdout")
		stderrPreview := readArtifactPreview(id, "opencode_stderr")
		if stdoutPreview != "" || stderrPreview != "" {
			events := pipeline.BuildOpenCodeTranscript(stdoutPreview, stderrPreview, 200)
			for _, ev := range events {
				openCodeTranscript = append(openCodeTranscript, views.OpenCodeTranscriptEventView{
					Kind: ev.Kind,
					Text: ev.Text,
				})
			}
		}
		if stdoutPreview != "" {
			assistantText := pipeline.ExtractOpenCodeAssistantText(stdoutPreview)
			parsed := pipeline.ParseAgentResult(assistantText)
			openCodeParsedResultStatus = string(parsed.Status)
			openCodeParsedBuildStatus = parsed.BuildStatus
			openCodeParsedTestStatus = parsed.TestStatus
			openCodeParsedLOCChanged = parsed.LOCChanged
			openCodeParsedResultRaw = parsed.Raw
		}
	}

	dryRunPreview := readArtifactPreview(id, "opencode_dry_run_json")

	// Parse CLI check artifact if present
	cliCheckPreview := readArtifactPreview(id, "opencode_cli_check_json")
	hasCLICheck := cliCheckPreview != ""
	cliCheckBinary := ""
	cliCheckResolvedModel := ""
	cliCheckModelAvailable := ""
	cliCheckVersionExitCode := ""
	cliCheckModelsExitCode := ""
	cliCheckCheckedAt := ""
	cliCheckError := ""
	cliCheckStatus := ""

	if hasCLICheck {
		var cliResult struct {
			Binary          string `json:"binary"`
			VersionExitCode int    `json:"version_exit_code"`
			ModelsExitCode  int    `json:"models_exit_code"`
			ResolvedModel   string `json:"resolved_model"`
			ModelAvailable  bool   `json:"model_available"`
			CheckedAt       string `json:"checked_at"`
			Error           string `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(cliCheckPreview), &cliResult); err == nil {
			cliCheckBinary = cliResult.Binary
			cliCheckResolvedModel = cliResult.ResolvedModel
			cliCheckCheckedAt = cliResult.CheckedAt
			cliCheckError = cliResult.Error
			cliCheckVersionExitCode = strconv.Itoa(cliResult.VersionExitCode)
			cliCheckModelsExitCode = strconv.Itoa(cliResult.ModelsExitCode)

			if cliResult.ModelAvailable {
				cliCheckModelAvailable = "yes"
			} else if cliResult.ResolvedModel != "" {
				cliCheckModelAvailable = "no"
			} else {
				cliCheckModelAvailable = "unknown"
			}

			// Compute display status
			if cliResult.VersionExitCode != 0 || cliResult.ModelsExitCode != 0 || cliResult.Error != "" {
				cliCheckStatus = "fail"
			} else if cliResult.ResolvedModel != "" && cliResult.ModelAvailable {
				cliCheckStatus = "pass"
			} else {
				cliCheckStatus = "warn"
			}
		} else {
			hasCLICheck = false
		}
	}

	// Compute validation command availability
	repoDefaults := ""
	if repo != nil {
		repoDefaults = repo.DefaultValidationCommands
	}
	hasValidationCommands := hasValidationCommandsForPreview(originalPreview, repoDefaults)

	intakeRemediationHandoffPreview := readArtifactPreview(id, "intake_remediation_handoff")
	hasIntakeRemediationHandoff := intakeRemediationHandoffPreview != "" || hasArtifactKind(artifactsList, "intake_remediation_handoff")

	// Compute intake review
	var intakeReview pipeline.IntakeReview
	if handoffText := readArtifactPreview(id, "original_handoff"); handoffText != "" {
		repoPath := ""
		if repo != nil {
			repoPath = repo.Path
		}
		metadata := pipeline.ParseHandoffMetadata(handoffText, repoDefaults)
		intakeReview = pipeline.BuildIntakeReview(metadata, repoPath)
	}

	// Parse validation run preview
	validationRunPreview := parseValidationRunPreview(readArtifactPreview(id, "validation_run_json"))

	// Compute audit handoff availability
	auditHandoffPreview := readArtifactPreview(id, "audit_handoff")
	hasAuditHandoff := auditHandoffPreview != "" || hasArtifactKind(artifactsList, "audit_handoff")

	// Compute next action
	hasIntakeReview := len(intakeReview.Warnings) > 0 || len(intakeReview.Blockers) > 0 || originalPreview != ""
	hasAgentResult := hasArtifactKind(artifactsList, "agent_result_raw")
	agentResultStatus := ""
	if hasAgentResult {
		if c := findFirstCheckByKind(checksList, "agent_result"); c != nil {
			agentResultStatus = c.Status
		}
	}
	hasValidationRun := hasArtifactKind(artifactsList, "validation_run_json")
	validationPassed := hasCheckKindWithStatus(checksList, "validation_run", "pass")
	validationFailed := hasCheckKindWithStatus(checksList, "validation_run", "fail")

	nextActionInput := pipeline.WorkbenchNextActionInput{
		HasOriginalHandoff:          originalPreview != "" || hasArtifactKind(artifactsList, "original_handoff"),
		HasIntakeReview:             hasIntakeReview,
		IntakeHasBlockers:           len(intakeReview.Blockers) > 0,
		IntakeHasWarnings:           len(intakeReview.Warnings) > 0,
		HasIntakeRemediationHandoff: hasIntakeRemediationHandoff,
		HasAgentPrompt:              hasArtifactKind(artifactsList, "agent_prompt"),
		HasAgentPacket:              hasArtifactKind(artifactsList, "opencode_handoff_packet"),
		HandoffPreflightStatus:      preflightStatus,
		OpenCodeAdapterError:        openCodeAdapterError,
		HasOpenCodeCLICheck:         hasCLICheck,
		OpenCodeCLICheckStatus:      cliCheckStatus,
		HasOpenCodeDryRun:           dryRunPreview != "",
		HasOpenCodeExecution:        hasOpenCodeExecution,
		OpenCodeExecutionStatus:     openCodeExecStatus,
		HasAgentResult:              hasAgentResult,
		AgentResultStatus:           agentResultStatus,
		HasValidationCommands:       hasValidationCommands,
		HasValidationRun:            hasValidationRun,
		ValidationPassed:            validationPassed,
		ValidationFailed:            validationFailed,
		HasAuditHandoff:             hasAuditHandoff,
	}

	nextAction := pipeline.BuildWorkbenchNextAction(nextActionInput)
	nextActionView := views.WorkbenchNextActionView{
		Kind:              string(nextAction.Kind),
		Title:             nextAction.Title,
		Summary:           nextAction.Summary,
		Step:              nextAction.Step,
		PrimaryAction:     nextAction.PrimaryAction,
		PrimaryFormAction: nextAction.PrimaryFormAction,
		PrimaryHref:       nextAction.PrimaryHref,
		Disabled:          nextAction.Disabled,
		DisabledReason:    nextAction.DisabledReason,
		Severity:          nextAction.Severity,
	}

	previews := views.RunPreviews{
		NextAction:                      nextActionView,
		OriginalHandoff:                 originalPreview,
		ValidationJSON:                  readArtifactPreview(id, "handoff_validation_json"),
		AgentPrompt:                     agentPromptPreview,
		OpenCodePacket:                  readArtifactPreview(id, "opencode_handoff_packet"),
		AgentPromptEstimate:             agentPromptEstimate,
		HandoffPreflightStatus:          preflightStatus,
		HandoffPreflightChecks:          preflightChecks,
		OpenCodeCommandPreview:          openCodeCommandPreview,
		OpenCodeExecutionStatus:         openCodeExecStatus,
		OpenCodeExecutionExitCode:       openCodeExecExitCode,
		OpenCodeExecutionStarted:        openCodeExecStarted,
		OpenCodeExecutionFinished:       openCodeExecFinished,
		OpenCodeStdoutArtifactID:        0,
		OpenCodeStderrArtifactID:        0,
		OpenCodeCombinedArtifactID:      0,
		HasOpenCodeExecution:            hasOpenCodeExecution,
		OpenCodeBinary:                  openCodeBinary,
		OpenCodeArgs:                    openCodeArgs,
		OpenCodeWorkDir:                 openCodeWorkDir,
		OpenCodeModel:                   openCodeModel,
		OpenCodeAgent:                   openCodeAgent,
		OpenCodeVariant:                 openCodeVariant,
		OpenCodeThinking:                openCodeThinking,
		OpenCodeStdinSource:             openCodeStdinSource,
		OpenCodeStdinBytes:              openCodeStdinBytes,
		OpenCodeAdapterError:            openCodeAdapterError,
		OpenCodeFailureHint:             openCodeFailureHint,
		OpenCodeDryRunPreview:           dryRunPreview,
		HasOpenCodeDryRun:               dryRunPreview != "",
		HasOpenCodeStdout:               hasOpenCodeStdout,
		HasOpenCodeStderr:               hasOpenCodeStderr,
		HasOpenCodeCombinedLog:          hasOpenCodeCombinedLog,
		HasValidationCommands:           hasValidationCommands,
		HasOpenCodeCLICheck:             hasCLICheck,
		OpenCodeCLICheckPreview:         cliCheckPreview,
		OpenCodeCLICheckBinary:          cliCheckBinary,
		OpenCodeCLICheckResolvedModel:   cliCheckResolvedModel,
		OpenCodeCLICheckModelAvailable:  cliCheckModelAvailable,
		OpenCodeCLICheckVersionExitCode: cliCheckVersionExitCode,
		OpenCodeCLICheckModelsExitCode:  cliCheckModelsExitCode,
		OpenCodeCLICheckCheckedAt:       cliCheckCheckedAt,
		OpenCodeCLICheckError:           cliCheckError,
		OpenCodeCLICheckStatus:          cliCheckStatus,
		IntakeRemediationHandoff:        intakeRemediationHandoffPreview,
		HasIntakeRemediationHandoff:     hasIntakeRemediationHandoff,
		HasOpenCodeRunning:              openCodeExecStatus == "starting" || openCodeExecStatus == "running",
		OpenCodeTranscript:              openCodeTranscript,
		OpenCodeParsedResultStatus:      openCodeParsedResultStatus,
		OpenCodeParsedBuildStatus:       openCodeParsedBuildStatus,
		OpenCodeParsedTestStatus:        openCodeParsedTestStatus,
		OpenCodeParsedLOCChanged:        openCodeParsedLOCChanged,
		OpenCodeParsedResultRaw:         openCodeParsedResultRaw,
		ValidationRun:                   validationRunPreview,
		HasAuditHandoff:                 hasAuditHandoff,
		AuditHandoff:                    auditHandoffPreview,
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
	case "generate-audit-handoff":
		h.generateAuditHandoff(w, r, id)
	case "submit-agent-result":
		h.submitAgentResult(w, r, id)
	case "generate-opencode-packet":
		h.generateOpenCodePacket(w, r, id)
	case "dry-run-opencode-go":
		h.dryRunOpenCodeGo(w, r, id)
	case "start-opencode-go":
		h.startOpenCodeGo(w, r, id)
	case "check-opencode-cli":
		h.checkOpenCodeCLI(w, r, id)
	case "generate-intake-remediation-handoff":
		h.generateIntakeRemediationHandoff(w, r, id)
	case "replace-original-handoff":
		h.replaceOriginalHandoff(w, r, id)
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

	// If ready with no blockers or warnings, guide user to prompt generation
	if report.Status == "ready" {
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=prompt", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
	}
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
			"Cannot generate Agent Prompt while Intake Review has blockers: "+strings.Join(review.Blockers, "; "))
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

// runOpenCodeExecution runs the OpenCode command in the background and persists results.
// This method never writes an HTTP response or redirects.
func (h *RunsHandler) runOpenCodeExecution(ctx context.Context, runID int64, execID int64, invocation pipeline.OpenCodeRunInvocation) {
	// Update to running
	startedAt := time.Now().Format("2006-01-02 15:04:05")
	h.store.UpdateAgentExecutionStatus(execID, "running", nil, &startedAt, nil, nil, nil, nil, nil, nil)

	// Run command with timeout
	runResult := h.runAgentCommandArgs(
		ctx,
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

	h.store.UpdateAgentExecutionStatus(execID, execStatus, &ec, &startedStr, &finishedStr,
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
			return
		}
	}

	eventMsg := "OpenCode Go execution completed with exit code " + strconv.Itoa(runResult.ExitCode)
	if runResult.TimedOut {
		eventMsg = "OpenCode Go execution timed out"
	}
	h.store.CreateEvent(runID, "info", eventMsg)
	h.log.Info("opencode go execution completed", "run_id", runID, "exit_code", runResult.ExitCode)
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

	// Check if latest execution is already running
	if exec, err := h.store.GetLatestAgentExecutionByRun(runID); err == nil {
		if exec.Status == "starting" || exec.Status == "running" {
			h.store.CreateEvent(runID, "warn", "OpenCode Go execution is already running.")
			http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=run", http.StatusSeeOther)
			return
		}
	}

	// Create execution record with status starting
	exec, err := h.store.CreateAgentExecution(runID, "opencode_go", "starting", invocation.Preview)
	if err != nil {
		if isMissingAgentExecutionsSchemaError(err) {
			h.store.CreateEvent(runID, "warn", "Database schema is missing agent_executions. Run goose -dir internal/db/migrations sqlite3 data/relay.sqlite up and restart Relay.")
			http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
			return
		}
		h.log.Error("create agent execution record", "error", err)
		http.Error(w, "failed to create execution record", http.StatusInternalServerError)
		return
	}

	h.store.CreateEvent(runID, "info", "OpenCode Go execution started")

	// Launch background execution
	h.launchAgentExecution(func() {
		h.runOpenCodeExecution(context.Background(), runID, exec.ID, invocation)
	})

	// Redirect immediately to Step 5
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=run", http.StatusSeeOther)
}

func (h *RunsHandler) AgentRunMonitor(w http.ResponseWriter, r *http.Request) {
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

	artifactsList, _ := h.store.ListArtifactsByRun(id)
	checksList, _ := h.store.ListChecksByRun(id)

	// Build minimal previews for the monitor display
	previews := h.buildExecutionPreviews(id, run, artifactsList)

	// Populate adapter info for display from run data
	repo, _ := h.store.GetRepo(run.RepoID)
	if repo != nil {
		if invocation, err := h.buildOpenCodeInvocationForRun(id); err == nil {
			previews.OpenCodeBinary = invocation.Binary
			previews.OpenCodeArgs = invocation.Args
			previews.OpenCodeWorkDir = invocation.WorkDir
			previews.OpenCodeModel = invocation.Model
			previews.OpenCodeAgent = invocation.Agent
			previews.OpenCodeThinking = "max"
		}
	}

	views.AgentRunMonitorStepPanel(run, artifactsList, checksList, previews).Render(r.Context(), w)
}

func (h *RunsHandler) checkOpenCodeCLI(w http.ResponseWriter, r *http.Request, runID int64) {
	cfg := pipeline.OpenCodeRunConfigFromEnv()
	binary := cfg.Binary
	now := time.Now().Format(time.RFC3339)

	// Get run to resolve its selected model
	resolvedModel := ""
	modelResolutionError := ""
	run, runErr := h.store.GetRun(runID)
	if runErr == nil && run.SelectedModel != "" {
		if m, err := pipeline.ResolveOpenCodeModel(run.SelectedModel); err == nil {
			resolvedModel = m
		} else {
			modelResolutionError = err.Error()
		}
	}

	type cliCheckResult struct {
		Binary               string `json:"binary"`
		VersionExitCode      int    `json:"version_exit_code"`
		VersionStdout        string `json:"version_stdout,omitempty"`
		VersionStderr        string `json:"version_stderr,omitempty"`
		ModelsExitCode       int    `json:"models_exit_code"`
		ModelsStdout         string `json:"models_stdout,omitempty"`
		ModelsStderr         string `json:"models_stderr,omitempty"`
		ResolvedModel        string `json:"resolved_model"`
		ModelAvailable       bool   `json:"model_available"`
		CheckedAt            string `json:"checked_at"`
		Error                string `json:"error,omitempty"`
		ModelResolutionError string `json:"model_resolution_error,omitempty"`
	}

	result := cliCheckResult{
		Binary:               binary,
		ResolvedModel:        resolvedModel,
		CheckedAt:            now,
		ModelResolutionError: modelResolutionError,
	}

	// Run opencode --version
	verResult := h.runAgentCommandArgs(r.Context(), ".", binary, []string{"--version"}, "", 30*time.Second)
	result.VersionExitCode = verResult.ExitCode
	result.VersionStdout = verResult.Stdout
	result.VersionStderr = verResult.Stderr

	if verResult.ExitCode != 0 {
		errMsg := "opencode --version failed"
		if verResult.Stderr != "" {
			errMsg += ": " + strings.TrimSpace(verResult.Stderr)
		}
		result.Error = errMsg
		resultJSON, _ := json.MarshalIndent(result, "", "  ")
		p, _ := artifacts.Write(runID, "opencode_cli_check_json", pipeline.ArtifactFilename("opencode_cli_check_json"), resultJSON)
		if p != "" {
			h.store.CreateArtifact(runID, "opencode_cli_check_json", p, "application/json")
		}
		h.store.CreateEvent(runID, "warn", "OpenCode CLI check failed: binary not found or not working")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
		return
	}

	// Run opencode models
	modelsResult := h.runAgentCommandArgs(r.Context(), ".", binary, []string{"models"}, "", 30*time.Second)
	result.ModelsExitCode = modelsResult.ExitCode
	result.ModelsStdout = modelsResult.Stdout
	result.ModelsStderr = modelsResult.Stderr

	// Check if resolved model appears in models output
	if resolvedModel != "" && modelsResult.ExitCode == 0 {
		result.ModelAvailable = strings.Contains(modelsResult.Stdout, resolvedModel) ||
			strings.Contains(modelsResult.Stdout, strings.Split(resolvedModel, "/")[1])
	}

	persistErr := ""
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		persistErr = err.Error()
	} else {
		p, err := artifacts.Write(runID, "opencode_cli_check_json", pipeline.ArtifactFilename("opencode_cli_check_json"), resultJSON)
		if err != nil {
			persistErr = err.Error()
		} else {
			h.store.CreateArtifact(runID, "opencode_cli_check_json", p, "application/json")
		}
	}

	if persistErr != "" {
		h.log.Error("persist opencode cli check result", "error", persistErr)
	}

	if modelsResult.ExitCode != 0 {
		h.store.CreateEvent(runID, "warn", "OpenCode CLI check: `opencode models` failed")
	} else if resolvedModel != "" && !result.ModelAvailable {
		h.store.CreateEvent(runID, "warn", "OpenCode CLI check: model "+resolvedModel+" not found in `opencode models` output")
	} else if resolvedModel != "" && result.ModelAvailable {
		h.store.CreateEvent(runID, "info", "OpenCode CLI check: binary and model OK")
	} else {
		h.store.CreateEvent(runID, "info", "OpenCode CLI check: binary OK (model not resolved)")
	}

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

func (h *RunsHandler) generateAuditHandoff(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, _ := h.store.GetRepo(run.RepoID)
	repoName := ""
	if repo != nil {
		repoName = repo.Name
	}

	originalHandoff := readArtifactPreview(runID, "original_handoff")
	agentResultRaw := readArtifactPreview(runID, "agent_result_raw")

	// Parse agent result
	agentResultStatus := ""
	buildStatus := ""
	testStatus := ""
	locChanged := ""
	if agentResultRaw != "" {
		parsed := pipeline.ParseAgentResult(agentResultRaw)
		agentResultStatus = string(parsed.Status)
		buildStatus = parsed.BuildStatus
		testStatus = parsed.TestStatus
		locChanged = parsed.LOCChanged
	}

	// Parse validation commands from artifact
	validationJSON := readArtifactPreview(runID, "validation_run_json")
	validationStatus := ""
	validationRepoPath := ""
	var validationCommands []pipeline.CommandRunResult
	if validationJSON != "" {
		var raw struct {
			Status   string                      `json:"status"`
			RepoPath string                      `json:"repo_path"`
			Commands []pipeline.CommandRunResult `json:"commands"`
		}
		if err := json.Unmarshal([]byte(validationJSON), &raw); err == nil {
			validationStatus = raw.Status
			validationRepoPath = raw.RepoPath
			validationCommands = raw.Commands
		}
	}

	input := pipeline.AuditHandoffInput{
		RunID:              runID,
		Title:              run.Title,
		RepoName:           repoName,
		BranchName:         run.BranchName,
		Status:             run.Status,
		OriginalHandoff:    originalHandoff,
		AgentResultStatus:  agentResultStatus,
		BuildStatus:        buildStatus,
		TestStatus:         testStatus,
		LOCChanged:         locChanged,
		ResultRaw:          agentResultRaw,
		ValidationStatus:   validationStatus,
		ValidationRepoPath: validationRepoPath,
		ValidationCommands: validationCommands,
	}

	content := pipeline.BuildAuditHandoff(input)

	artifactPath, err := artifacts.Write(runID, "audit_handoff", pipeline.ArtifactFilename("audit_handoff"), []byte(content))
	if err != nil {
		h.log.Error("write audit handoff", "error", err)
		http.Error(w, "failed to save audit handoff", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "audit_handoff", artifactPath, "text/markdown")
	h.store.CreateEvent(runID, "info", "Audit handoff generated")
	h.log.Info("audit handoff generated", "run_id", runID)

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
}

func (h *RunsHandler) replaceOriginalHandoff(w http.ResponseWriter, r *http.Request, runID int64) {
	rawText := r.FormValue("handoff_text")
	handoffText := strings.TrimSpace(rawText)
	if handoffText == "" {
		h.store.CreateEvent(runID, "warn", "Replace handoff skipped: handoff text is empty")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
		return
	}

	// Write new handoff to disk (use raw text to preserve original content including trailing newline)
	handoffPath, err := artifacts.Write(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"), []byte(rawText))
	if err != nil {
		h.log.Error("write replaced original handoff", "error", err)
		http.Error(w, "failed to save handoff", http.StatusInternalServerError)
		return
	}

	// Replace artifact record: delete existing original_handoff rows, create new one
	h.store.DeleteArtifactsByRunKind(runID, "original_handoff")
	h.store.CreateArtifact(runID, "original_handoff", handoffPath, "text/plain")

	// Clear stale downstream artifacts that depend on the old handoff
	staleKinds := []string{
		"handoff_validation_json",
		"agent_prompt",
		"ready_prompt",
		"opencode_handoff_packet",
		"opencode_dry_run_json",
		"opencode_cli_check_json",
		"validation_run_json",
		"validation_stdout",
		"validation_stderr",
	}
	for _, kind := range staleKinds {
		h.store.DeleteArtifactsByRunKind(runID, kind)
	}

	// Clear stale checks
	h.store.DeleteChecksByRunKind(runID, "validation")
	h.store.DeleteChecksByRunKind(runID, "validation_run")

	// Reset run status to draft so validation can re-run
	h.store.UpdateRunStatus(runID, "draft")

	h.store.CreateEvent(runID, "info", "Original handoff replaced; re-running Intake Review")

	h.log.Info("original handoff replaced", "run_id", runID)

	// Re-run Intake Review using the new handoff text
	h.validateHandoff(w, r, runID)
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

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
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

func isMissingAgentExecutionsSchemaError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such table: agent_executions")
}

func (h *RunsHandler) generateIntakeRemediationHandoff(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, _ := h.store.GetRepo(run.RepoID)
	repoName := ""
	repoPath := ""
	repoDefaults := ""
	if repo != nil {
		repoName = repo.Name
		repoPath = repo.Path
		repoDefaults = repo.DefaultValidationCommands
	}

	handoffData, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		h.log.Error("read handoff for remediation", "error", err)
		h.store.CreateEvent(runID, "warn", "Cannot generate fix handoff: original handoff not found.")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
		return
	}

	metadata := pipeline.ParseHandoffMetadata(string(handoffData), repoDefaults)
	review := pipeline.BuildIntakeReview(metadata, repoPath)

	if len(review.Warnings) == 0 && len(review.Blockers) == 0 {
		h.store.CreateEvent(runID, "info", "No intake review warnings or blockers found; no fix handoff needed.")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
		return
	}

	scopedFiles := make([]string, len(metadata.ScopedFiles))
	for i, sf := range metadata.ScopedFiles {
		scopedFiles[i] = sf.Path
	}

	input := pipeline.IntakeRemediationInput{
		RunID:       run.ID,
		RepoName:    repoName,
		RepoPath:    repoPath,
		BranchName:  run.BranchName,
		RunStatus:   run.Status,
		Warnings:    review.Warnings,
		Blockers:    review.Blockers,
		ScopedFiles: scopedFiles,
	}

	content := pipeline.BuildIntakeRemediationHandoff(input)

	artifactPath, err := artifacts.Write(runID, "intake_remediation_handoff", pipeline.ArtifactFilename("intake_remediation_handoff"), []byte(content))
	if err != nil {
		h.log.Error("write intake remediation handoff", "error", err)
		http.Error(w, "failed to save fix handoff", http.StatusInternalServerError)
		return
	}

	h.store.CreateArtifact(runID, "intake_remediation_handoff", artifactPath, "text/markdown")
	h.store.CreateEvent(runID, "info", "Intake remediation handoff generated")

	h.log.Info("intake remediation handoff generated", "run_id", runID, "warnings", len(review.Warnings), "blockers", len(review.Blockers))

	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
}

// buildExecutionPreviews populates transcript and parsed result previews from the latest execution.
func (h *RunsHandler) buildExecutionPreviews(runID int64, run *store.Run, artifactsList []store.Artifact) views.RunPreviews {
	var previews views.RunPreviews

	// Load latest execution
	if exec, err := h.store.GetLatestAgentExecutionByRun(runID); err == nil {
		previews.HasOpenCodeExecution = true
		previews.OpenCodeExecutionStatus = exec.Status
		if exec.ExitCode.Valid {
			previews.OpenCodeExecutionExitCode = strconv.FormatInt(exec.ExitCode.Int64, 10)
		}
		if exec.StartedAt.Valid {
			previews.OpenCodeExecutionStarted = exec.StartedAt.String
		}
		if exec.FinishedAt.Valid {
			previews.OpenCodeExecutionFinished = exec.FinishedAt.String
		}
		previews.OpenCodeCommandPreview = exec.CommandPreview
		previews.HasOpenCodeRunning = exec.Status == "starting" || exec.Status == "running"

		for _, a := range artifactsList {
			switch a.Kind {
			case "opencode_stdout":
				previews.HasOpenCodeStdout = true
			case "opencode_stderr":
				previews.HasOpenCodeStderr = true
			case "opencode_combined_log":
				previews.HasOpenCodeCombinedLog = true
			}
		}

		// Build transcript from stdout/stderr
		stdoutPreview := readArtifactPreview(runID, "opencode_stdout")
		stderrPreview := readArtifactPreview(runID, "opencode_stderr")
		events := pipeline.BuildOpenCodeTranscript(stdoutPreview, stderrPreview, 200)
		for _, ev := range events {
			previews.OpenCodeTranscript = append(previews.OpenCodeTranscript, views.OpenCodeTranscriptEventView{
				Kind: ev.Kind,
				Text: ev.Text,
			})
		}

		// Parse final result if stdout exists
		if stdoutPreview != "" {
			assistantText := pipeline.ExtractOpenCodeAssistantText(stdoutPreview)
			parsed := pipeline.ParseAgentResult(assistantText)
			previews.OpenCodeParsedResultStatus = string(parsed.Status)
			previews.OpenCodeParsedBuildStatus = parsed.BuildStatus
			previews.OpenCodeParsedTestStatus = parsed.TestStatus
			previews.OpenCodeParsedLOCChanged = parsed.LOCChanged
			if parsed.Raw != "" {
				previews.OpenCodeParsedResultRaw = parsed.Raw
			}
		}
	}

	return previews
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

func findFirstCheckByKind(checks []store.Check, kind string) *store.Check {
	for i := range checks {
		if checks[i].Kind == kind {
			return &checks[i]
		}
	}
	return nil
}

func hasCheckKindWithStatus(checks []store.Check, kind string, status string) bool {
	for _, c := range checks {
		if c.Kind == kind && c.Status == status {
			return true
		}
	}
	return false
}

// normalizeRunStep maps a step query value to a known step identifier.
// Invalid or empty values default to "intake".
func normalizeRunStep(step string) string {
	switch step {
	case "intake", "prompt", "packet", "handoff", "run", "validation", "audit":
		return step
	default:
		return "intake"
	}
}

func hasValidationCommandsForPreview(handoffText string, repoDefaults string) bool {
	return len(pipeline.ExtractValidationCommands(handoffText, repoDefaults)) > 0
}

func defaultActiveRunStep(_ []store.Artifact, _ []store.Check) string {
	return "intake"
}

func parseValidationRunPreview(jsonData string) views.ValidationRunPreview {
	if jsonData == "" {
		return views.ValidationRunPreview{}
	}

	var raw struct {
		Status   string                      `json:"status"`
		RepoPath string                      `json:"repo_path"`
		Commands []pipeline.CommandRunResult `json:"commands"`
	}
	if err := json.Unmarshal([]byte(jsonData), &raw); err != nil {
		return views.ValidationRunPreview{}
	}

	preview := views.ValidationRunPreview{
		Status:          raw.Status,
		RepoPath:        raw.RepoPath,
		CommandCount:    len(raw.Commands),
		TotalDurationMs: 0,
	}

	for _, cmd := range raw.Commands {
		vcmd := views.ValidationCommandPreview{
			Label:      cmd.Label,
			Command:    cmd.Command,
			Source:     cmd.Source,
			ExitCode:   cmd.ExitCode,
			TimedOut:   cmd.TimedOut,
			DurationMs: cmd.DurationMS,
			HasStdout:  cmd.Stdout != "",
			HasStderr:  cmd.Stderr != "",
		}
		preview.TotalDurationMs += cmd.DurationMS
		if cmd.TimedOut {
			vcmd.Status = "timed_out"
			preview.TimedOutCount++
		} else if cmd.ExitCode != 0 {
			vcmd.Status = "fail"
			preview.FailedCount++
		} else {
			vcmd.Status = "pass"
			preview.PassedCount++
		}
		preview.Commands = append(preview.Commands, vcmd)
	}

	return preview
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
