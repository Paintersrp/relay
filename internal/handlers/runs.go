package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"relay/internal/artifacts"
	"relay/internal/events"
	"relay/internal/pipeline"
	"relay/internal/repos"
	"relay/internal/store"
	"relay/internal/views"

	"github.com/go-chi/chi/v5"
)

type agentCommandRunner func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult

type RunsHandler struct {
	store                *store.Store
	log                  *slog.Logger
	eventHub             *events.Hub
	runAgentCommandArgs  agentCommandRunner
	launchAgentExecution func(func())
	launchValidation     func(func())
}

func NewRunsHandler(s *store.Store, log *slog.Logger, hub ...*events.Hub) *RunsHandler {
	var eventHub *events.Hub
	if len(hub) > 0 {
		eventHub = hub[0]
	}
	return &RunsHandler{
		store:               s,
		log:                 log,
		eventHub:            eventHub,
		runAgentCommandArgs: pipeline.RunLocalAgentCommandArgsStreaming,
		launchAgentExecution: func(fn func()) {
			go fn()
		},
		launchValidation: func(fn func()) {
			go fn()
		},
	}
}

func (h *RunsHandler) publishRunEvent(runID int64, kind, source, status string) {
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

const (
	openCodeStaleOutputThreshold = 2 * time.Minute
	openCodeStartupNoOutputGrace = 2 * time.Minute
	openCodeTimeoutGrace         = 1 * time.Minute
	openCodeRecoveryActionLabel  = "Recover Stale OpenCode Run"
)

var runEventHeartbeatInterval = 20 * time.Second

func isOpenCodeExecutionRunning(status string) bool {
	return status == "starting" || status == "running"
}

func parseExecutionTimestamp(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func executionTimestampNow() string {
	return time.Now().Format(time.RFC3339Nano)
}

func formatDurationCompact(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second) / time.Second)
	if seconds <= 0 {
		return "0s"
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func formatOpenCodeRuntime(startedAt string, finishedAt string, now time.Time) string {
	started, ok := parseExecutionTimestamp(startedAt)
	if !ok {
		return ""
	}
	end := now
	if finished, ok := parseExecutionTimestamp(finishedAt); ok {
		end = finished
	}
	return formatDurationCompact(end.Sub(started))
}

func openCodeArtifactModTime(runID int64, kind string) (time.Time, bool) {
	path, err := artifacts.Path(runID, kind, pipeline.ArtifactFilename(kind))
	if err != nil {
		return time.Time{}, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

func latestOpenCodeOutputTime(runID int64) (time.Time, bool) {
	kinds := []string{"opencode_stdout", "opencode_stderr", "opencode_combined_log"}
	var latest time.Time
	found := false
	for _, kind := range kinds {
		modTime, ok := openCodeArtifactModTime(runID, kind)
		if !ok {
			continue
		}
		if !found || modTime.After(latest) {
			latest = modTime
			found = true
		}
	}
	return latest, found
}

type openCodeExecutionLiveness struct {
	Running             bool
	PlausiblyActive     bool
	Stale               bool
	State               string
	Reason              string
	Runtime             string
	LastOutputAt        string
	LastOutputAge       string
	LastChunkAt         string
	LastChunkAge        string
	HasOutput           bool
	HasStream           bool
	CanRecover          bool
	RecoveryActionLabel string
}

func evaluateOpenCodeExecutionLiveness(runID int64, exec *store.AgentExecution, now time.Time) openCodeExecutionLiveness {
	liveness := openCodeExecutionLiveness{
		Running: exec != nil && isOpenCodeExecutionRunning(exec.Status),
	}
	if exec == nil {
		return liveness
	}

	startedAt, startedOK := parseExecutionTimestamp(exec.StartedAt.String)
	finishedAt, finishedOK := parseExecutionTimestamp(exec.FinishedAt.String)
	if startedOK {
		end := now
		if !liveness.Running && finishedOK {
			end = finishedAt
		}
		liveness.Runtime = formatDurationCompact(end.Sub(startedAt))
	}

	var lastChunkAt time.Time
	lastChunkOK := false
	if progressData := readArtifactPreview(runID, "opencode_stream_progress_json"); progressData != "" {
		var sp pipeline.StreamProgress
		if err := json.Unmarshal([]byte(progressData), &sp); err == nil {
			hasStreamEvidence := sp.StdoutChunks > 0 || sp.StderrChunks > 0 || sp.LastChunkAt != ""
			if hasStreamEvidence {
				liveness.HasStream = true
				liveness.HasOutput = true
				if sp.LastChunkAt != "" {
					liveness.LastChunkAt = sp.LastChunkAt
					if parsedLastChunkAt, ok := parseExecutionTimestamp(sp.LastChunkAt); ok {
						lastChunkAt = parsedLastChunkAt
						lastChunkOK = true
						liveness.LastChunkAge = formatDurationCompact(now.Sub(parsedLastChunkAt)) + " ago"
					}
				}
			}
		}
	}

	var lastOutputAt time.Time
	lastOutputOK := false
	if parsedLastOutputAt, ok := latestOpenCodeOutputTime(runID); ok {
		liveness.HasOutput = true
		liveness.LastOutputAt = parsedLastOutputAt.Local().Format("2006-01-02 15:04:05")
		liveness.LastOutputAge = formatDurationCompact(now.Sub(parsedLastOutputAt)) + " ago"
		lastOutputAt = parsedLastOutputAt
		lastOutputOK = true
	}

	var latestActivity time.Time
	switch {
	case lastChunkOK:
		latestActivity = lastChunkAt
	case lastOutputOK:
		latestActivity = lastOutputAt
	case exec.UpdatedAt != "":
		if updatedAt, ok := parseExecutionTimestamp(exec.UpdatedAt); ok {
			latestActivity = updatedAt
		}
	case startedOK:
		latestActivity = startedAt
	}

	if !liveness.Running {
		switch exec.Status {
		case "completed":
			liveness.State = "completed"
		case "failed":
			liveness.State = "failed"
		default:
			liveness.State = "none"
		}
		return liveness
	}

	liveness.PlausiblyActive = true

	if startedOK && now.Sub(startedAt) > pipeline.DefaultAgentCommandTimeout+openCodeTimeoutGrace {
		liveness.Stale = true
		liveness.PlausiblyActive = false
		liveness.State = "stale_timeout"
		if liveness.Runtime != "" {
			liveness.Reason = "OpenCode runtime " + liveness.Runtime + " exceeded the timeout window."
		} else {
			liveness.Reason = "OpenCode runtime exceeded the timeout window."
		}
		liveness.CanRecover = true
		liveness.RecoveryActionLabel = openCodeRecoveryActionLabel
		return liveness
	}

	if !latestActivity.IsZero() && now.Sub(latestActivity) > openCodeStaleOutputThreshold {
		liveness.Stale = true
		liveness.PlausiblyActive = false
		liveness.State = "stale_output"
		switch {
		case liveness.LastChunkAge != "":
			liveness.Reason = "Last streamed chunk: " + liveness.LastChunkAge + "."
		case liveness.LastOutputAge != "":
			liveness.Reason = "Last output: " + liveness.LastOutputAge + "."
		default:
			liveness.Reason = "OpenCode output stopped before a final result."
		}
		liveness.CanRecover = true
		liveness.RecoveryActionLabel = openCodeRecoveryActionLabel
		return liveness
	}

	if liveness.HasStream {
		liveness.State = "active_streaming"
		liveness.Reason = "Relay is still receiving stream chunks."
		return liveness
	}

	if liveness.HasOutput {
		liveness.State = "active_output"
		if liveness.LastOutputAge != "" {
			liveness.Reason = "Output artifacts were updated " + liveness.LastOutputAge + " ago."
		} else {
			liveness.Reason = "Output artifacts are present."
		}
		return liveness
	}

	if !startedOK || now.Sub(startedAt) <= openCodeStartupNoOutputGrace {
		liveness.State = "running_no_output"
		if startedOK {
			liveness.Reason = "OpenCode is still within the startup grace period and has not emitted output yet."
		} else {
			liveness.Reason = "OpenCode has not emitted output yet."
		}
		return liveness
	}

	liveness.State = "waiting_output"
	liveness.Reason = "OpenCode is running but has not emitted first output yet."
	return liveness
}

func applyOpenCodeExecutionLiveness(previews *views.RunPreviews, liveness openCodeExecutionLiveness) {
	if previews == nil {
		return
	}
	previews.HasOpenCodeRunning = liveness.PlausiblyActive
	previews.HasOpenCodeStaleRunning = liveness.Stale
	previews.OpenCodeLifecycleState = liveness.State
	previews.OpenCodeStaleReason = liveness.Reason
	previews.OpenCodeCanRecover = liveness.CanRecover
	previews.OpenCodeRecoveryActionLabel = liveness.RecoveryActionLabel
	if liveness.Runtime != "" {
		previews.OpenCodeRuntime = liveness.Runtime
	}
	if liveness.LastOutputAt != "" {
		previews.OpenCodeLastOutputAt = liveness.LastOutputAt
	}
	if liveness.LastOutputAge != "" {
		previews.OpenCodeLastOutputAge = liveness.LastOutputAge
	}
	if liveness.LastChunkAt != "" {
		previews.OpenCodeStreamLastChunkAt = liveness.LastChunkAt
	}
	if liveness.LastChunkAge != "" {
		previews.OpenCodeStreamLastChunkAge = liveness.LastChunkAge
	}
	previews.HasOpenCodeOutput = liveness.HasOutput
	previews.HasOpenCodeStreamActivity = liveness.HasStream
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

	openCodePreviews, openCodeChanged := h.buildExecutionPreviews(id, run, artifactsList)
	if openCodeChanged {
		if refreshedRun, err := h.store.GetRun(id); err == nil {
			run = refreshedRun
		}
		if refreshedArtifacts, err := h.store.ListArtifactsByRun(id); err == nil {
			artifactsList = refreshedArtifacts
		}
		if refreshedChecks, err := h.store.ListChecksByRun(id); err == nil {
			checksList = refreshedChecks
		}
		if refreshedEvents, err := h.store.ListEventsByRun(id); err == nil {
			eventsList = refreshedEvents
		}
	}

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
		} else {
			openCodeAdapterError = err.Error()
		}
	}
	// OpenCode execution preview fields come from the shared helper so the full render
	// and the monitor path stay in sync.
	hasOpenCodeExecution := openCodePreviews.HasOpenCodeExecution
	openCodeExecStatus := openCodePreviews.OpenCodeExecutionStatus
	openCodeExecExitCode := openCodePreviews.OpenCodeExecutionExitCode
	openCodeFailureHint := ""
	if openCodeExecStatus == "failed" && openCodeBinary != "" {
		exitCode := 0
		if openCodeExecExitCode != "" {
			if parsedExitCode, err := strconv.Atoi(openCodeExecExitCode); err == nil {
				exitCode = parsedExitCode
			}
		}
		execError := ""
		if exec, err := h.store.GetLatestAgentExecutionByRun(id); err == nil && exec.Error.Valid {
			execError = exec.Error.String
		}
		runResult := pipeline.AgentCommandRunResult{
			ExitCode: exitCode,
			Stderr:   readArtifactPreview(id, "opencode_stderr"),
			Stdout:   readArtifactPreview(id, "opencode_stdout"),
			TimedOut: exitCode == -2,
			Error:    execError,
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

	// Parse validation progress
	validationProgressPreview := parseValidationProgressPreview(readArtifactPreview(id, "validation_progress_json"))
	isValidationRunning := validationProgressPreview.Status == "starting" || validationProgressPreview.Status == "running"

	// Stale guard: if progress is running but updated >30 min ago, mark stale
	validationProgressStale := false
	if isValidationRunning && validationProgressPreview.UpdatedAt != "" {
		if updated, err := time.Parse(time.RFC3339, validationProgressPreview.UpdatedAt); err == nil {
			if time.Since(updated) > 30*time.Minute {
				validationProgressStale = true
			}
		}
	}

	// Compute repo path for previews
	previewsRepoPath := ""
	if repo != nil {
		previewsRepoPath = repo.Path
	}

	// Compute audit handoff availability
	auditHandoffPreview := readArtifactPreview(id, "audit_handoff")
	hasAuditHandoff := auditHandoffPreview != "" || hasArtifactKind(artifactsList, "audit_handoff")

	// Compute git diff evidence preview
	gitStatusPreview := readArtifactPreview(id, "git_status_text")
	hasGitStatus := gitStatusPreview != ""
	hasGitDiffNameStatus := hasArtifactKind(artifactsList, "git_diff_name_status")
	gitDiffStatPreview := readArtifactPreview(id, "git_diff_stat")
	hasGitDiffStat := gitDiffStatPreview != ""
	gitDiffPatchPreview := readArtifactPreview(id, "git_diff_patch")
	hasGitDiffPatch := gitDiffPatchPreview != ""
	hasGitDiffEvidence := hasGitStatus || hasGitDiffStat || hasGitDiffPatch || hasGitDiffNameStatus
	gitDiffSummary := ""
	gitChangedFileCount := int64(0)
	if gitDiffStatPreview != "" {
		lines := strings.Split(gitDiffStatPreview, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, " ") {
				gitChangedFileCount++
			}
		}
		maxLen := 200
		if len(gitDiffStatPreview) > maxLen {
			gitDiffSummary = gitDiffStatPreview[:maxLen] + "\n..."
		} else {
			gitDiffSummary = gitDiffStatPreview
		}
	}
	if hasGitStatus {
		maxLen := 300
		if len(gitStatusPreview) > maxLen {
			gitStatusPreview = gitStatusPreview[:maxLen] + "\n..."
		}
	}
	if hasGitDiffStat {
		maxLen := 200
		if len(gitDiffStatPreview) > maxLen {
			gitDiffStatPreview = gitDiffStatPreview[:maxLen] + "\n..."
		}
	}
	if hasGitDiffPatch {
		maxLen := 500
		if len(gitDiffPatchPreview) > maxLen {
			gitDiffPatchPreview = gitDiffPatchPreview[:maxLen] + "\n..."
		}
	}

	// Compute commit suggestion preview
	commitMessagePreview := readArtifactPreview(id, "commit_message_text")
	commitSuggestionJSONStr := readArtifactPreview(id, "commit_suggestion_json")
	hasCommitSuggestion := commitMessagePreview != "" || hasArtifactKind(artifactsList, "commit_message_text")
	commitSuggestionStatus := ""
	commitSuggestionGeneratedAt := ""
	if commitSuggestionJSONStr != "" {
		var cs struct {
			Status      string `json:"status"`
			Message     string `json:"message"`
			GeneratedAt string `json:"generated_at"`
		}
		if err := json.Unmarshal([]byte(commitSuggestionJSONStr), &cs); err == nil {
			commitSuggestionStatus = cs.Status
			commitSuggestionGeneratedAt = cs.GeneratedAt
			if commitMessagePreview == "" {
				commitMessagePreview = cs.Message
			}
		}
	}

	// Compute git baseline state for display
	gitBaselineBaselineSHA := run.BaseCommit
	gitBaselineHeadSHA := run.HeadCommit
	gitBaselineBranch := run.BranchName
	gitBaselineState := ""
	gitBaselineAvailable := false
	if run.BaseCommit != "" {
		gitBaselineAvailable = true
		if run.BaseCommit == run.HeadCommit {
			gitBaselineState = "head unchanged"
		} else if run.HeadCommit != "" {
			gitBaselineState = "head moved since baseline"
		} else {
			gitBaselineState = "baseline captured"
		}
	}

	// Parse change evidence JSON if available
	changeEvidenceData := readArtifactPreview(id, "git_change_evidence_json")
	hasGitChangeEvidence := changeEvidenceData != ""
	gitChangeEvidenceMode := ""
	gitChangeEvidenceBaseline := ""
	gitChangeEvidenceHead := ""
	gitChangeEvidenceBranch := ""
	gitChangeEvidenceCommitCnt := 0
	gitChangeEvidenceWarning := ""
	if hasGitChangeEvidence {
		type gitChangeEvidencePreview struct {
			Mode           string `json:"mode"`
			BaselineSHA    string `json:"baseline_sha,omitempty"`
			CurrentHeadSHA string `json:"current_head_sha,omitempty"`
			Branch         string `json:"branch,omitempty"`
			CommitCount    int    `json:"commit_count"`
			Warning        string `json:"warning,omitempty"`
		}
		var gce gitChangeEvidencePreview
		if err := json.Unmarshal([]byte(changeEvidenceData), &gce); err == nil {
			gitChangeEvidenceMode = gce.Mode
			gitChangeEvidenceBaseline = gce.BaselineSHA
			gitChangeEvidenceHead = gce.CurrentHeadSHA
			gitChangeEvidenceBranch = gce.Branch
			gitChangeEvidenceCommitCnt = gce.CommitCount
			gitChangeEvidenceWarning = gce.Warning
		}
	}

	// Compute commit state for Step 8
	auditClearanceData := readArtifactPreview(id, "audit_clearance_json")
	hasAuditClearance := auditClearanceData != ""
	auditClearanceStatus := ""
	auditClearanceAcceptedAt := ""
	auditClearanceSource := ""
	if hasAuditClearance {
		var ac repos.AuditClearance
		if err := json.Unmarshal([]byte(auditClearanceData), &ac); err == nil {
			auditClearanceStatus = ac.Status
			auditClearanceAcceptedAt = ac.AcceptedAt
			auditClearanceSource = ac.Source
		}
	}

	// Parse commit result (needed before ResolveCommitState)
	commitResultData := readArtifactPreview(id, "git_commit_result_json")
	hasCommitResult := commitResultData != ""
	commitResultSuccess := false
	commitResultSHA := ""
	commitResultSubject := ""
	if hasCommitResult {
		var cr repos.GitCommitResult
		if err := json.Unmarshal([]byte(commitResultData), &cr); err == nil {
			commitResultSuccess = cr.Success
			commitResultSHA = cr.SHA
			commitResultSubject = cr.Subject
		}
	}

	// Parse push result (needed before ResolveCommitState)
	pushResultData := readArtifactPreview(id, "git_push_result_json")
	hasPushResult := pushResultData != ""
	pushResultSuccess := false
	if hasPushResult {
		var pr repos.GitPushResult
		if err := json.Unmarshal([]byte(pushResultData), &pr); err == nil {
			pushResultSuccess = pr.Success
		}
	}

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

	hasValidationProgress := validationProgressPreview.Status != ""
	validationProgressRunning := isValidationRunning
	validationProgressStatus := validationProgressPreview.Status

	validationAcceptedWithFailure := run.Status == "validation_failed_accepted"

	commitStateResult := repos.ResolveCommitState(repos.CommitStateInput{
		RepoPath:                 previewsRepoPath,
		ValidationPassed:         validationPassed,
		ValidationFailedAccepted: validationAcceptedWithFailure,
		AuditAccepted:            auditClearanceStatus == "accepted",
		EvidenceMode:             gitChangeEvidenceMode,
		HasGitDiffEvidence:       hasGitChangeEvidence,
		EvidenceHeadSHA:          gitChangeEvidenceHead,
		EvidenceBranch:           gitChangeEvidenceBranch,
		HasCommitResult:          hasCommitResult,
		CommitResultSuccess:      commitResultSuccess,
		CommitResultSHA:          commitResultSHA,
		HasPushResult:            hasPushResult,
		PushResultSuccess:        pushResultSuccess,
	})

	// Compute next action
	nextActionInput := pipeline.WorkbenchNextActionInput{
		HasOriginalHandoff:            originalPreview != "" || hasArtifactKind(artifactsList, "original_handoff"),
		HasIntakeReview:               hasIntakeReview,
		IntakeHasBlockers:             len(intakeReview.Blockers) > 0,
		IntakeHasWarnings:             len(intakeReview.Warnings) > 0,
		HasIntakeRemediationHandoff:   hasIntakeRemediationHandoff,
		HasAgentPrompt:                hasArtifactKind(artifactsList, "agent_prompt"),
		HasAgentPacket:                hasArtifactKind(artifactsList, "opencode_handoff_packet"),
		HandoffPreflightStatus:        preflightStatus,
		OpenCodeAdapterError:          openCodeAdapterError,
		HasOpenCodeCLICheck:           hasCLICheck,
		OpenCodeCLICheckStatus:        cliCheckStatus,
		HasOpenCodeDryRun:             dryRunPreview != "",
		HasOpenCodeExecution:          hasOpenCodeExecution,
		OpenCodeExecutionStatus:       openCodeExecStatus,
		HasOpenCodeStaleRunning:       openCodePreviews.HasOpenCodeStaleRunning,
		OpenCodeLifecycleState:        openCodePreviews.OpenCodeLifecycleState,
		OpenCodeCanRecover:            openCodePreviews.OpenCodeCanRecover,
		HasAgentResult:                hasAgentResult,
		AgentResultStatus:             agentResultStatus,
		HasValidationCommands:         hasValidationCommands,
		HasValidationRun:              hasValidationRun,
		ValidationPassed:              validationPassed,
		ValidationFailed:              validationFailed,
		HasValidationProgress:         hasValidationProgress,
		ValidationProgressRunning:     validationProgressRunning,
		ValidationProgressStatus:      validationProgressStatus,
		HasAuditHandoff:               hasAuditHandoff,
		HasGitDiffEvidence:            hasGitDiffEvidence,
		HasGitStatus:                  hasGitStatus,
		HasGitDiffStat:                hasGitDiffStat,
		HasGitDiffPatch:               hasGitDiffPatch,
		HasGitDiffNameStatus:          hasGitDiffNameStatus,
		HasCommitSuggestion:           hasCommitSuggestion,
		ValidationAcceptedWithFailure: validationAcceptedWithFailure,
		CommitState:                   string(commitStateResult.State),
		CommitStateMsg:                commitStateResult.Error,
		CommitHasUpstream:             commitStateResult.HasUpstream,
		CommitAheadCount:              commitStateResult.AheadCount,
		CommitBehindCount:             commitStateResult.BehindCount,
		CommitSHA:                     commitStateResult.CommitSHA,
		CommitSubject:                 commitStateResult.CommitSubject,
		HasCommitResult:               hasCommitResult,
		CommitResultSuccess:           commitResultSuccess,
		HasPushResult:                 hasPushResult,
		PushResultSuccess:             pushResultSuccess,
		AuditClearanceStatus:          auditClearanceStatus,
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

	// Write commit state artifact
	commitStateJSON, _ := json.MarshalIndent(commitStateResult, "", "  ")
	h.store.DeleteArtifactsByRunKind(id, "git_commit_state_json")
	if csp, err := artifacts.Write(id, "git_commit_state_json", pipeline.ArtifactFilename("git_commit_state_json"), commitStateJSON); err == nil {
		h.store.CreateArtifact(id, "git_commit_state_json", csp, "application/json")
	}

	// Parse commit suggestion for selected message
	commitSuggestionSelected := ""
	commitSuggestionSource := ""
	commitSuggestionConfidence := ""
	var commitWarnings []string
	if commitSuggestionJSONStr != "" {
		var cs struct {
			Selected   string   `json:"selected"`
			Source     string   `json:"source"`
			Confidence string   `json:"confidence"`
			Warnings   []string `json:"warnings"`
		}
		if err := json.Unmarshal([]byte(commitSuggestionJSONStr), &cs); err == nil {
			commitSuggestionSelected = cs.Selected
			commitSuggestionSource = cs.Source
			commitSuggestionConfidence = cs.Confidence
			commitWarnings = cs.Warnings
		}
	}

	// Parse push dry run
	pushDryRunData := readArtifactPreview(id, "git_push_dry_run_json")
	hasPushDryRun := pushDryRunData != ""
	pushDryRunPass := false
	if hasPushDryRun {
		var pr repos.GitPushResult
		if err := json.Unmarshal([]byte(pushDryRunData), &pr); err == nil {
			pushDryRunPass = pr.DryRunPass
		}
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
		OpenCodeCommandPreview:          openCodePreviews.OpenCodeCommandPreview,
		OpenCodeExecutionStatus:         openCodePreviews.OpenCodeExecutionStatus,
		OpenCodeExecutionExitCode:       openCodePreviews.OpenCodeExecutionExitCode,
		OpenCodeExecutionStarted:        openCodePreviews.OpenCodeExecutionStarted,
		OpenCodeExecutionFinished:       openCodePreviews.OpenCodeExecutionFinished,
		OpenCodeExecutionError:          openCodePreviews.OpenCodeExecutionError,
		OpenCodeRuntime:                 openCodePreviews.OpenCodeRuntime,
		OpenCodeLastOutputAt:            openCodePreviews.OpenCodeLastOutputAt,
		OpenCodeLastOutputAge:           openCodePreviews.OpenCodeLastOutputAge,
		OpenCodePermissionWarning:       openCodePreviews.OpenCodePermissionWarning,
		OpenCodeStdoutArtifactID:        0,
		OpenCodeStderrArtifactID:        0,
		OpenCodeCombinedArtifactID:      0,
		HasOpenCodeExecution:            openCodePreviews.HasOpenCodeExecution,
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
		HasOpenCodeStdout:               openCodePreviews.HasOpenCodeStdout,
		HasOpenCodeStderr:               openCodePreviews.HasOpenCodeStderr,
		HasOpenCodeCombinedLog:          openCodePreviews.HasOpenCodeCombinedLog,
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
		HasOpenCodeRunning:              openCodePreviews.HasOpenCodeRunning,
		HasOpenCodeStaleRunning:         openCodePreviews.HasOpenCodeStaleRunning,
		HasOpenCodeOutput:               openCodePreviews.HasOpenCodeOutput,
		OpenCodeLifecycleState:          openCodePreviews.OpenCodeLifecycleState,
		OpenCodeStaleReason:             openCodePreviews.OpenCodeStaleReason,
		OpenCodeCanRecover:              openCodePreviews.OpenCodeCanRecover,
		OpenCodeRecoveryActionLabel:     openCodePreviews.OpenCodeRecoveryActionLabel,
		OpenCodeTranscript:              openCodePreviews.OpenCodeTranscript,
		OpenCodeParsedResultStatus:      openCodePreviews.OpenCodeParsedResultStatus,
		OpenCodeParsedBuildStatus:       openCodePreviews.OpenCodeParsedBuildStatus,
		OpenCodeParsedTestStatus:        openCodePreviews.OpenCodeParsedTestStatus,
		OpenCodeParsedLOCChanged:        openCodePreviews.OpenCodeParsedLOCChanged,
		OpenCodeParsedResultRaw:         openCodePreviews.OpenCodeParsedResultRaw,
		OpenCodeStreamStdoutChunks:      openCodePreviews.OpenCodeStreamStdoutChunks,
		OpenCodeStreamStderrChunks:      openCodePreviews.OpenCodeStreamStderrChunks,
		OpenCodeStreamStdoutBytes:       openCodePreviews.OpenCodeStreamStdoutBytes,
		OpenCodeStreamStderrBytes:       openCodePreviews.OpenCodeStreamStderrBytes,
		OpenCodeStreamLastChunkAt:       openCodePreviews.OpenCodeStreamLastChunkAt,
		OpenCodeStreamLastChunkAge:      openCodePreviews.OpenCodeStreamLastChunkAge,
		HasOpenCodeStreamActivity:       openCodePreviews.HasOpenCodeStreamActivity,
		ValidationRun:                   validationRunPreview,
		HasValidationProgress:           hasValidationProgress,
		ValidationProgressRunning:       validationProgressRunning,
		ValidationProgressStale:         validationProgressStale,
		ValidationProgressPreview:       validationProgressPreview,
		ValidationFailedAccepted:        validationAcceptedWithFailure,
		HasAuditHandoff:                 hasAuditHandoff,
		AuditHandoff:                    auditHandoffPreview,
		RepoPath:                        previewsRepoPath,
		SuggestedCommitMessage:          commitMessagePreview,
		HasCommitSuggestion:             hasCommitSuggestion,
		CommitMessage:                   commitMessagePreview,
		CommitSuggestionJSON:            commitSuggestionJSONStr,
		CommitSuggestionStatus:          commitSuggestionStatus,
		CommitSuggestionGeneratedAt:     commitSuggestionGeneratedAt,
		HasGitStatus:                    hasGitStatus,
		GitStatusPreview:                gitStatusPreview,
		HasGitDiffStat:                  hasGitDiffStat,
		GitDiffStatPreview:              gitDiffStatPreview,
		HasGitDiffPatch:                 hasGitDiffPatch,
		GitDiffPatchPreview:             gitDiffPatchPreview,
		GitChangedFileCount:             gitChangedFileCount,
		GitDiffSummary:                  gitDiffSummary,
		GitBaselineBaselineSHA:          gitBaselineBaselineSHA,
		GitBaselineHeadSHA:              gitBaselineHeadSHA,
		GitBaselineBranch:               gitBaselineBranch,
		GitBaselineState:                gitBaselineState,
		GitBaselineAvailable:            gitBaselineAvailable,
		HasGitChangeEvidence:            hasGitChangeEvidence,
		GitChangeEvidenceMode:           gitChangeEvidenceMode,
		GitChangeEvidenceBaseline:       gitChangeEvidenceBaseline,
		GitChangeEvidenceHead:           gitChangeEvidenceHead,
		GitChangeEvidenceBranch:         gitChangeEvidenceBranch,
		GitChangeEvidenceCommitCnt:      gitChangeEvidenceCommitCnt,
		GitChangeEvidenceWarning:        gitChangeEvidenceWarning,
		CommitState:                     string(commitStateResult.State),
		CommitStateMsg:                  commitStateResult.Error,
		CommitValidationPassed:          commitStateResult.ValidationPassed || commitStateResult.ValidationFailedAccepted,
		CommitAuditAccepted:             commitStateResult.AuditAccepted,
		CommitEvidenceMode:              commitStateResult.EvidenceMode,
		CommitBranch:                    commitStateResult.Branch,
		CommitHeadSHA:                   commitStateResult.HeadSHA,
		CommitUpstreamRemote:            commitStateResult.UpstreamRemote,
		CommitUpstreamBranch:            commitStateResult.UpstreamBranch,
		CommitAheadCount:                commitStateResult.AheadCount,
		CommitBehindCount:               commitStateResult.BehindCount,
		CommitHasUpstream:               commitStateResult.HasUpstream,
		CommitWorktreeClean:             commitStateResult.WorktreeClean,
		CommitSHA:                       commitStateResult.CommitSHA,
		CommitSubject:                   commitStateResult.CommitSubject,
		CommitSuggestionSelected:        commitSuggestionSelected,
		CommitSuggestionSource:          commitSuggestionSource,
		CommitSuggestionConfidence:      commitSuggestionConfidence,
		CommitWarnings:                  commitWarnings,
		HasAuditClearance:               hasAuditClearance,
		AuditClearanceStatus:            auditClearanceStatus,
		AuditClearanceAcceptedAt:        auditClearanceAcceptedAt,
		AuditClearanceSource:            auditClearanceSource,
		HasCommitResult:                 hasCommitResult,
		CommitResultSuccess:             commitResultSuccess,
		CommitResultSHA:                 commitResultSHA,
		CommitResultSubject:             commitResultSubject,
		HasPushDryRun:                   hasPushDryRun,
		PushDryRunPass:                  pushDryRunPass,
		HasPushResult:                   hasPushResult,
		PushResultSuccess:               pushResultSuccess,
	}

	if validationStdout := readArtifactPreview(id, "validation_stdout"); validationStdout != "" {
		previews.ValidationStdoutPreview = truncatePreviewText(validationStdout, 1200)
	}
	if validationStderr := readArtifactPreview(id, "validation_stderr"); validationStderr != "" {
		previews.ValidationStderrPreview = truncatePreviewText(validationStderr, 1200)
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

func writeRunEventSSE(w io.Writer, flusher http.Flusher, event events.RunEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Kind); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func (h *RunsHandler) Events(w http.ResponseWriter, r *http.Request) {
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

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var eventCh <-chan events.RunEvent
	unsubscribe := func() {}
	if h.eventHub != nil {
		eventCh, unsubscribe = h.eventHub.Subscribe(id)
	}
	defer unsubscribe()

	if err := writeRunEventSSE(w, flusher, events.RunEvent{
		RunID:  id,
		Kind:   events.KindRunSummary,
		Source: "summary",
		Status: run.Status,
		At:     time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return
	}

	heartbeat := time.NewTicker(runEventHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-eventCh:
			if !ok {
				return
			}
			if err := writeRunEventSSE(w, flusher, ev); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
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
		h.startValidation(w, r, id)
	case "inspect-diff":
		h.inspectDiff(w, r, id)
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
	case "accept-validation-failure":
		h.acceptValidationFailure(w, r, id)
	case "prepare-git-commit":
		h.prepareGitCommit(w, r, id)
	case "recover-stale-opencode-execution":
		h.reconcileOpenCodeResult(w, r, id)
	case "reconcile-opencode-result":
		h.reconcileOpenCodeResult(w, r, id)
	case "update-selected-model":
		h.updateSelectedModel(w, r, id)
	case "accept-audit-clearance":
		h.acceptAuditClearance(w, r, id)
	case "revoke-audit-clearance":
		h.revokeAuditClearance(w, r, id)
	case "create-git-commit":
		h.createGitCommit(w, r, id)
	case "push-git-commit":
		h.pushGitCommit(w, r, id)
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
	h.publishRunEvent(runID, events.KindRunSummary, "intake", report.Status)

	h.log.Info("handoff validated", "run_id", runID, "status", report.Status)

	// If ready with no blockers or warnings, guide user to prompt generation
	if report.Status == "ready" {
		setHXPushURL(w, runID, "prompt")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=prompt", http.StatusSeeOther)
	} else {
		setHXPushURL(w, runID, "intake")
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
		setHXPushURL(w, runID, "intake")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
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
	h.publishRunEvent(runID, events.KindStepArtifacts, "prompt", "ready")

	h.log.Info("agent prompt prepared", "run_id", runID)

	setHXPushURL(w, runID, "prompt")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=prompt", http.StatusSeeOther)
}

func (h *RunsHandler) markStatus(w http.ResponseWriter, r *http.Request, runID int64, status string) {
	h.store.UpdateRunStatus(runID, status)

	h.store.CreateEvent(runID, "info", "Run status changed to "+status)
	h.publishRunEvent(runID, events.KindRunSummary, "status", status)

	setHXPushURL(w, runID, "validation")
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
	h.publishRunEvent(runID, events.KindStepAgent, "agent_result", runStatus)
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
	h.publishRunEvent(runID, events.KindStepArtifacts, "opencode", "dry_run")
	setHXPushURL(w, runID, "handoff")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
}

// openCodeReconcileResult describes what the reconciliation helper did.
type openCodeReconcileResult struct {
	Changed           bool
	ParsedAgentResult bool
	FinalStatus       string
	Message           string
}

type agentExecutionPreservedFields struct {
	exitCode     *int64
	startedAt    *string
	stdoutPath   *string
	stderrPath   *string
	combinedPath *string
	resultPath   *string
}

func preservedAgentExecutionFields(exec *store.AgentExecution) agentExecutionPreservedFields {
	if exec == nil {
		return agentExecutionPreservedFields{}
	}

	var preserved agentExecutionPreservedFields
	if exec.ExitCode.Valid {
		exitCode := exec.ExitCode.Int64
		preserved.exitCode = &exitCode
	}
	if exec.StartedAt.Valid {
		startedAt := exec.StartedAt.String
		preserved.startedAt = &startedAt
	}
	if exec.StdoutArtifactPath.Valid {
		stdoutPath := exec.StdoutArtifactPath.String
		preserved.stdoutPath = &stdoutPath
	}
	if exec.StderrArtifactPath.Valid {
		stderrPath := exec.StderrArtifactPath.String
		preserved.stderrPath = &stderrPath
	}
	if exec.CombinedArtifactPath.Valid {
		combinedPath := exec.CombinedArtifactPath.String
		preserved.combinedPath = &combinedPath
	}
	if exec.ResultArtifactPath.Valid {
		resultPath := exec.ResultArtifactPath.String
		preserved.resultPath = &resultPath
	}
	return preserved
}

// reconcileOpenCodeExecution reconciles a stale/running OpenCode execution from captured output artifacts.
// It is idempotent: running it twice will not duplicate agent result artifacts or corrupt terminal state.
func (h *RunsHandler) reconcileOpenCodeExecution(runID int64) (openCodeReconcileResult, error) {
	exec, err := h.store.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		return openCodeReconcileResult{}, fmt.Errorf("get latest execution: %w", err)
	}
	preserved := preservedAgentExecutionFields(exec)
	now := time.Now()

	// If execution is already terminal, check if we can recover agent_result_raw from stdout
	terminal := exec.Status != "starting" && exec.Status != "running"
	if terminal {
		// Check if agent_result_raw is missing but stdout has DONE/BLOCKED
		artifactsList, err := h.store.ListArtifactsByRun(runID)
		if err != nil {
			return openCodeReconcileResult{}, fmt.Errorf("list artifacts: %w", err)
		}
		hasRaw := false
		for _, a := range artifactsList {
			if a.Kind == "agent_result_raw" {
				hasRaw = true
				break
			}
		}
		if hasRaw {
			return openCodeReconcileResult{Changed: false, Message: "Execution is already terminal with agent result."}, nil
		}

		// Try to recover from stdout
		stdoutData, err := artifacts.Read(runID, "opencode_stdout", pipeline.ArtifactFilename("opencode_stdout"))
		if err != nil || len(stdoutData) == 0 {
			return openCodeReconcileResult{Changed: false, Message: "Execution is terminal but no stdout to recover from."}, nil
		}
		assistantText := pipeline.ExtractOpenCodeAssistantText(string(stdoutData))
		parsed := pipeline.ParseAgentResult(assistantText)
		if parsed.Status == pipeline.AgentResultDone || parsed.Status == pipeline.AgentResultBlocked {
			if err := h.persistAgentResult(runID, assistantText); err != nil {
				return openCodeReconcileResult{}, fmt.Errorf("persist recovered agent result: %w", err)
			}
			return openCodeReconcileResult{
				Changed:           true,
				ParsedAgentResult: true,
				FinalStatus:       string(parsed.Status),
				Message:           "Recovered missing agent result from captured stdout.",
			}, nil
		}
		return openCodeReconcileResult{Changed: false, Message: "Execution is terminal but stdout has no DONE/BLOCKED result."}, nil
	}

	// Execution is starting or running — check for captured output artifacts
	stdoutData, err := artifacts.Read(runID, "opencode_stdout", pipeline.ArtifactFilename("opencode_stdout"))
	hasStdout := err == nil && len(stdoutData) > 0

	stderrData, err := artifacts.Read(runID, "opencode_stderr", pipeline.ArtifactFilename("opencode_stderr"))
	hasStderr := err == nil && len(stderrData) > 0

	combinedData, err := artifacts.Read(runID, "opencode_combined_log", pipeline.ArtifactFilename("opencode_combined_log"))
	hasCombined := err == nil && len(combinedData) > 0

	// Try to parse agent result from stdout
	finishedAt := now.Format(time.RFC3339Nano)
	errorMsg := ""
	if hasStdout {
		assistantText := pipeline.ExtractOpenCodeAssistantText(string(stdoutData))
		parsed := pipeline.ParseAgentResult(assistantText)
		if parsed.Status == pipeline.AgentResultDone || parsed.Status == pipeline.AgentResultBlocked {
			// Persist the agent result
			if err := h.persistAgentResult(runID, assistantText); err != nil {
				h.log.Warn("reconcile: failed to persist agent result from captured stdout", "error", err)
			}
			// Mark execution completed/failed based on parsed result
			execStatus := "completed"
			if parsed.Status == pipeline.AgentResultBlocked {
				execStatus = "failed"
				errorMsg = "Agent reported BLOCKED"
			}
			if _, err := h.store.UpdateAgentExecutionStatus(exec.ID, execStatus, preserved.exitCode, preserved.startedAt, &finishedAt, preserved.stdoutPath, preserved.stderrPath, preserved.combinedPath, preserved.resultPath, &errorMsg); err != nil {
				h.log.Error("reconcile: update execution status", "error", err)
			}
			h.store.CreateEvent(runID, "info", "OpenCode execution reconciled from captured output: "+string(parsed.Status))
			h.publishRunEvent(runID, events.KindStepAgent, "opencode", execStatus)
			return openCodeReconcileResult{
				Changed:           true,
				ParsedAgentResult: true,
				FinalStatus:       execStatus,
				Message:           "Reconciled execution with captured result: " + string(parsed.Status),
			}, nil
		}
	}

	// No DONE/BLOCKED found — mark execution as failed with a useful message
	liveness := evaluateOpenCodeExecutionLiveness(runID, exec, now)
	if !liveness.Stale {
		return openCodeReconcileResult{Changed: false, Message: "Execution is still active and does not need recovery."}, nil
	}

	if !hasStdout && !hasStderr && !hasCombined && liveness.State == "stale_timeout" {
		errorMsg = "OpenCode execution recovered as failed: runtime exceeded the timeout window and no stdout/stderr artifacts were captured. Relay may have restarted, lost the worker, or OpenCode exited before producing output."
		if _, err := h.store.UpdateAgentExecutionStatus(exec.ID, "failed", preserved.exitCode, preserved.startedAt, &finishedAt, preserved.stdoutPath, preserved.stderrPath, preserved.combinedPath, preserved.resultPath, &errorMsg); err != nil {
			h.log.Error("recover stale no-output opencode execution", "error", err)
			return openCodeReconcileResult{}, err
		}
		h.store.CreateEvent(runID, "warn", "OpenCode execution recovered as failed: timeout exceeded with no captured output.")
		h.publishRunEvent(runID, events.KindStepAgent, "opencode", "failed")
		return openCodeReconcileResult{
			Changed:     true,
			FinalStatus: "failed",
			Message:     errorMsg,
		}, nil
	}

	if liveness.State == "stale_timeout" {
		errorMsg = "OpenCode execution recovered as failed: runtime exceeded the timeout window before a final DONE/BLOCKED result. Review stdout/stderr/combined log. OpenCode may have hung, exited without final output, or Relay may have lost the worker."
	} else {
		errorMsg = "OpenCode execution recovered as failed: streamed output stopped before a final DONE/BLOCKED result. OpenCode may have hung, exited without final output, or Relay may have lost the worker."
	}
	if _, err := h.store.UpdateAgentExecutionStatus(exec.ID, "failed", preserved.exitCode, preserved.startedAt, &finishedAt, preserved.stdoutPath, preserved.stderrPath, preserved.combinedPath, preserved.resultPath, &errorMsg); err != nil {
		h.log.Error("reconcile: update execution status to failed", "error", err)
	}
	h.store.CreateEvent(runID, "warn", errorMsg)
	h.publishRunEvent(runID, events.KindStepAgent, "opencode", "failed")
	return openCodeReconcileResult{
		Changed:     true,
		FinalStatus: "failed",
		Message:     errorMsg,
	}, nil
}

// reconcileOpenCodeResult is the HTTP action handler for reconciling a stale/running execution.
func (h *RunsHandler) reconcileOpenCodeResult(w http.ResponseWriter, r *http.Request, runID int64) {
	result, err := h.reconcileOpenCodeExecution(runID)
	if err != nil {
		h.log.Error("reconcile opencode result", "error", err)
		h.store.CreateEvent(runID, "warn", "OpenCode reconciliation failed: "+err.Error())
		h.publishRunEvent(runID, events.KindStepAgent, "opencode", "error")
	} else if !result.Changed {
		h.store.CreateEvent(runID, "info", result.Message)
		h.publishRunEvent(runID, events.KindStepAgent, "opencode", "noop")
	}
	setHXPushURL(w, runID, "run")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=run", http.StatusSeeOther)
}

// runOpenCodeExecution runs the OpenCode command in the background and persists results.
// This method never writes an HTTP response or redirects.
func (h *RunsHandler) runOpenCodeExecution(ctx context.Context, runID int64, execID int64, invocation pipeline.OpenCodeRunInvocation) {
	// Update to running
	startedAt := executionTimestampNow()
	h.store.UpdateAgentExecutionStatus(execID, "running", nil, &startedAt, nil, nil, nil, nil, nil, nil)
	h.publishRunEvent(runID, events.KindStepAgent, "opencode", "running")

	for _, kind := range []string{
		"opencode_stdout",
		"opencode_stderr",
		"opencode_combined_log",
		"opencode_stream_progress_json",
		"agent_result_raw",
		"agent_result_json",
	} {
		h.deleteRunArtifactKind(runID, kind)
	}

	var streamMu sync.Mutex
	var streamedStdout strings.Builder
	var streamedStderr strings.Builder
	artifactRecorded := map[string]bool{}
	writeErrors := map[string]string{}
	streamProgress := pipeline.StreamProgress{}
	lastProgressWrite := time.Time{}

	recordWriteError := func(key string, err error) {
		if err == nil {
			return
		}
		if _, exists := writeErrors[key]; !exists {
			writeErrors[key] = err.Error()
		}
	}

	ensureArtifactRecordedLocked := func(kind, path, mimeType string) {
		if artifactRecorded[kind] {
			return
		}
		if _, err := h.store.CreateArtifact(runID, kind, path, mimeType); err != nil {
			recordWriteError("record_"+kind, err)
			return
		}
		artifactRecorded[kind] = true
	}

	appendArtifactLocked := func(kind string, chunk []byte) string {
		if len(chunk) == 0 {
			return ""
		}

		if err := artifacts.EnsureDir(runID); err != nil {
			recordWriteError("dir_"+kind, err)
			return ""
		}

		path, err := artifacts.Path(runID, kind, pipeline.ArtifactFilename(kind))
		if err != nil {
			recordWriteError("path_"+kind, err)
			return ""
		}

		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			recordWriteError("append_"+kind, err)
			return ""
		}
		if _, err := f.Write(chunk); err != nil {
			recordWriteError("append_"+kind, err)
		}
		if err := f.Close(); err != nil {
			recordWriteError("close_"+kind, err)
		}

		ensureArtifactRecordedLocked(kind, path, "text/plain")
		return path
	}

	writeArtifactSnapshotLocked := func(kind string, data string) string {
		if data == "" {
			return ""
		}
		path, err := artifacts.Write(runID, kind, pipeline.ArtifactFilename(kind), []byte(data))
		if err != nil {
			recordWriteError("write_"+kind, err)
			return ""
		}
		ensureArtifactRecordedLocked(kind, path, "text/plain")
		return path
	}

	combinedLogText := func(stdout, stderr string) string {
		combined := stdout
		if stderr != "" {
			if combined != "" {
				combined += "\n\n--- STDERR ---\n\n"
			}
			combined += stderr
		}
		return combined
	}

	writeCombinedSnapshotLocked := func(stdout, stderr string) string {
		combined := combinedLogText(stdout, stderr)
		if combined == "" {
			return ""
		}
		return writeArtifactSnapshotLocked("opencode_combined_log", combined)
	}

	writeStreamProgressLocked := func() {
		data, err := json.MarshalIndent(streamProgress, "", "  ")
		if err != nil {
			recordWriteError("stream_progress_marshal", err)
			return
		}
		path, err := artifacts.Write(runID, "opencode_stream_progress_json", pipeline.ArtifactFilename("opencode_stream_progress_json"), data)
		if err != nil {
			recordWriteError("stream_progress_write", err)
			return
		}
		ensureArtifactRecordedLocked("opencode_stream_progress_json", path, "application/json")
		lastProgressWrite = time.Now()
		h.publishRunEvent(runID, events.KindStepAgent, "opencode", "running")
	}

	if err := artifacts.EnsureDir(runID); err != nil {
		streamMu.Lock()
		recordWriteError("dir_init", err)
		streamMu.Unlock()
	}

	// Run command with timeout
	runResult := h.runAgentCommandArgs(
		ctx,
		invocation.WorkDir,
		invocation.Binary,
		invocation.Args,
		invocation.Stdin,
		pipeline.DefaultAgentCommandTimeout,
		pipeline.AgentCommandStreamCallbacks{
			OnStdout: func(chunk []byte) {
				if len(chunk) == 0 {
					return
				}
				streamMu.Lock()
				defer streamMu.Unlock()

				streamedStdout.Write(chunk)
				appendArtifactLocked("opencode_stdout", chunk)
				writeCombinedSnapshotLocked(streamedStdout.String(), streamedStderr.String())
				streamProgress.UpdateStreamProgressFromStdout(chunk)
				if time.Since(lastProgressWrite) > 500*time.Millisecond {
					writeStreamProgressLocked()
				}
			},
			OnStderr: func(chunk []byte) {
				if len(chunk) == 0 {
					return
				}
				streamMu.Lock()
				defer streamMu.Unlock()

				streamedStderr.Write(chunk)
				appendArtifactLocked("opencode_stderr", chunk)
				writeCombinedSnapshotLocked(streamedStdout.String(), streamedStderr.String())
				streamProgress.UpdateStreamProgressFromStderr(chunk)
				if time.Since(lastProgressWrite) > 500*time.Millisecond {
					writeStreamProgressLocked()
				}
			},
		},
	)

	streamMu.Lock()
	stdoutPath := writeArtifactSnapshotLocked("opencode_stdout", runResult.Stdout)
	stderrPath := writeArtifactSnapshotLocked("opencode_stderr", runResult.Stderr)
	combinedPath := writeCombinedSnapshotLocked(runResult.Stdout, runResult.Stderr)
	writeStreamProgressLocked()
	streamMu.Unlock()

	// Determine execution status
	execStatus := "completed"
	if runResult.TimedOut {
		execStatus = "failed"
	} else if runResult.ExitCode != 0 {
		execStatus = "failed"
	}

	ec := int64(runResult.ExitCode)
	startedStr := runResult.StartedAt.Format(time.RFC3339Nano)
	finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)

	var errPtr *string
	if runResult.Error != "" {
		errPtr = &runResult.Error
	}
	// Include write errors in the execution error field
	streamMu.Lock()
	var writeErrorList []string
	for key, msg := range writeErrors {
		writeErrorList = append(writeErrorList, key+": "+msg)
	}
	streamMu.Unlock()
	if len(writeErrorList) > 0 {
		errSummary := "artifact write errors: " + strings.Join(writeErrorList, "; ")
		if errPtr != nil {
			combined := *errPtr + "; " + errSummary
			errPtr = &combined
		} else {
			errPtr = &errSummary
		}
	}

	if _, err := h.store.UpdateAgentExecutionStatus(execID, execStatus, &ec, &startedStr, &finishedStr,
		&stdoutPath, &stderrPath, &combinedPath, nil, errPtr); err != nil {
		h.log.Error("finalize agent execution status", "exec_id", execID, "error", err)
	}

	// Extract assistant text from JSONL stdout
	if runResult.Stdout != "" {
		assistantText := pipeline.ExtractOpenCodeAssistantText(runResult.Stdout)
		parsed := pipeline.ParseAgentResult(assistantText)
		if parsed.Status == pipeline.AgentResultDone || parsed.Status == pipeline.AgentResultBlocked {
			if err := h.persistAgentResult(runID, assistantText); err != nil {
				h.log.Warn("failed to persist opencode agent result", "error", err)
			}
			h.store.CreateEvent(runID, "info", "OpenCode Go execution completed with result: "+string(parsed.Status))
			h.publishRunEvent(runID, events.KindStepAgent, "opencode", string(parsed.Status))
			h.log.Info("opencode go execution completed", "run_id", runID, "exit_code", runResult.ExitCode, "status", parsed.Status)
			return
		}
	}

	eventMsg := "OpenCode Go execution completed with exit code " + strconv.Itoa(runResult.ExitCode)
	if runResult.TimedOut {
		eventMsg = "OpenCode Go execution timed out"
	}
	h.store.CreateEvent(runID, "info", eventMsg)
	finalStatus := "completed"
	if runResult.TimedOut || runResult.ExitCode != 0 {
		finalStatus = "failed"
	}
	h.publishRunEvent(runID, events.KindStepAgent, "opencode", finalStatus)
	h.log.Info("opencode go execution completed", "run_id", runID, "exit_code", runResult.ExitCode)
}

func (h *RunsHandler) startOpenCodeGo(w http.ResponseWriter, r *http.Request, runID int64) {
	// Build the real OpenCode adapter invocation
	invocation, err := h.buildOpenCodeInvocationForRun(runID)
	if err != nil {
		h.store.CreateEvent(runID, "warn", "OpenCode start blocked: "+err.Error())
		h.publishRunEvent(runID, events.KindStepAgent, "opencode", "blocked")
		setHXPushURL(w, runID, "handoff")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
		return
	}

	// Confirm repo path exists
	if info, err := os.Stat(invocation.WorkDir); err != nil || !info.IsDir() {
		h.store.CreateEvent(runID, "warn", "Repo path does not exist: "+invocation.WorkDir)
		h.publishRunEvent(runID, events.KindStepAgent, "opencode", "blocked")
		setHXPushURL(w, runID, "handoff")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
		return
	}

	// Check if latest execution is already running
	if exec, err := h.store.GetLatestAgentExecutionByRun(runID); err == nil {
		if exec.Status == "starting" || exec.Status == "running" {
			h.store.CreateEvent(runID, "warn", "OpenCode Go execution is already running.")
			h.publishRunEvent(runID, events.KindStepAgent, "opencode", "running")
			setHXPushURL(w, runID, "run")
			http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=run", http.StatusSeeOther)
			return
		}
	}

	// Capture authoritative agent-start baseline immediately before execution
	h.captureAgentStartBaseline(runID, invocation.WorkDir)

	// Create execution record with status starting
	exec, err := h.store.CreateAgentExecution(runID, "opencode_go", "starting", invocation.Preview)
	if err != nil {
		if isMissingAgentExecutionsSchemaError(err) {
			h.store.CreateEvent(runID, "warn", "Database schema is missing agent_executions. Run goose -dir internal/db/migrations sqlite3 data/relay.sqlite up and restart Relay.")
			setHXPushURL(w, runID, "handoff")
			http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
			return
		}
		h.log.Error("create agent execution record", "error", err)
		http.Error(w, "failed to create execution record", http.StatusInternalServerError)
		return
	}

	h.store.CreateEvent(runID, "info", "OpenCode Go execution started")
	h.publishRunEvent(runID, events.KindStepAgent, "opencode", "starting")

	// Launch background execution
	h.launchAgentExecution(func() {
		h.runOpenCodeExecution(context.Background(), runID, exec.ID, invocation)
	})

	// Redirect immediately to Step 5
	setHXPushURL(w, runID, "run")
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

	// Build minimal previews for the monitor display.
	previews, changed := h.buildExecutionPreviews(id, run, artifactsList)
	if changed {
		if refreshedRun, err := h.store.GetRun(id); err == nil {
			run = refreshedRun
		}
		if refreshedArtifacts, err := h.store.ListArtifactsByRun(id); err == nil {
			artifactsList = refreshedArtifacts
		}
		if refreshedChecks, err := h.store.ListChecksByRun(id); err == nil {
			checksList = refreshedChecks
		}
	}

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
	verResult := h.runAgentCommandArgs(r.Context(), ".", binary, []string{"--version"}, "", 30*time.Second, pipeline.AgentCommandStreamCallbacks{})
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
		setHXPushURL(w, runID, "handoff")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
		return
	}

	// Run opencode models
	modelsResult := h.runAgentCommandArgs(r.Context(), ".", binary, []string{"models"}, "", 30*time.Second, pipeline.AgentCommandStreamCallbacks{})
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

	setHXPushURL(w, runID, "handoff")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
}

func (h *RunsHandler) startValidation(w http.ResponseWriter, r *http.Request, runID int64) {
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
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "blocked")
		setHXPushURL(w, runID, "intake")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
		return
	}

	info, err := os.Stat(repo.Path)
	if err != nil || !info.IsDir() {
		h.store.CreateEvent(runID, "warn", "Repo path does not exist or is not a directory: "+repo.Path)
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "blocked")
		setHXPushURL(w, runID, "intake")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
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
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "blocked")
		setHXPushURL(w, runID, "intake")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
		return
	}

	// Check for existing active or stale DB-backed execution
	if active, checkErr := h.store.GetActiveValidationExecutionByRun(runID); checkErr == nil && active != nil {
		if isValidationExecutionStale(active) {
			if err := h.store.MarkStaleValidationExecutionError(runID, time.Now().Add(-30*time.Minute)); err != nil {
				h.log.Error("mark stale validation execution error", "error", err)
			}
			h.store.CreateEvent(runID, "info", "Stale validation execution cleared.")
			h.publishRunEvent(runID, events.KindStepValidation, "validation", "recovered")
		} else {
			h.store.CreateEvent(runID, "warn", "Validation commands are already running.")
			h.publishRunEvent(runID, events.KindStepValidation, "validation", "running")
			setHXPushURL(w, runID, "validation")
			http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
			return
		}
	}

	// Atomically acquire DB-backed validation execution
	executionID, acquired, err := h.store.TryCreateValidationExecution(runID)
	if err != nil {
		h.log.Error("try create validation execution", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !acquired {
		h.store.CreateEvent(runID, "warn", "Validation commands are already running.")
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "running")
		setHXPushURL(w, runID, "validation")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
		return
	}

	// Write initial progress artifact with the full planned command list.
	vp := pipeline.NewValidationProgressFromCommands(repo.Path, commands)
	writeProgress := func(p pipeline.ValidationProgress) {
		data, _ := json.MarshalIndent(p, "", "  ")
		h.store.DeleteArtifactsByRunKind(runID, "validation_progress_json")
		path, err := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), data)
		if err != nil {
			h.log.Error("write validation progress", "error", err)
			return
		}
		h.store.CreateArtifact(runID, "validation_progress_json", path, "application/json")
	}
	writeProgress(vp)
	h.publishRunEvent(runID, events.KindStepValidation, "validation", "starting")

	h.store.CreateEvent(runID, "info", "Validation commands started")

	// Launch background worker with executionID
	h.launchValidation(func() {
		h.executeValidation(runID, executionID, repo.Path, commands, writeProgress)
	})

	setHXPushURL(w, runID, "validation")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
}

// executeValidation runs validation commands in the background and persists results.
func (h *RunsHandler) executeValidation(runID int64, validationExecutionID int64, repoPath string, commands []pipeline.ValidationCommand, writeProgress func(pipeline.ValidationProgress)) {
	vp := pipeline.NewValidationProgressFromCommands(repoPath, commands)
	progressData := readArtifactPreview(runID, "validation_progress_json")
	if progressData != "" {
		var loaded pipeline.ValidationProgress
		if err := json.Unmarshal([]byte(progressData), &loaded); err == nil {
			if len(loaded.Commands) == len(commands) {
				vp = loaded
			}
		}
	}
	if len(vp.Commands) != len(commands) {
		vp = pipeline.NewValidationProgressFromCommands(repoPath, commands)
	}
	vp.RepoPath = repoPath
	vp.TotalCommands = len(commands)
	for i, cmd := range commands {
		if i >= len(vp.Commands) {
			break
		}
		if vp.Commands[i].Index == 0 {
			vp.Commands[i].Index = i + 1
		}
		if vp.Commands[i].Label == "" {
			vp.Commands[i].Label = cmd.Label
		}
		if vp.Commands[i].Command == "" {
			vp.Commands[i].Command = cmd.Command
		}
		if vp.Commands[i].Source == "" {
			vp.Commands[i].Source = cmd.Source
		}
		if vp.Commands[i].Status == "" {
			vp.Commands[i].Status = "pending"
		}
	}

	// Ensure execution state and progress get finalized on any exit path
	defer func() {
		if r := recover(); r != nil {
			if vp.CurrentIndex > 0 && vp.CurrentIndex <= len(vp.Commands) && vp.Commands[vp.CurrentIndex-1].Status == "running" {
				vp.MarkCommandError(vp.CurrentIndex, "worker panic")
			}
			vp.MarkRemainingSkipped()
			vp.MarkError("worker panic")
			h.store.FinishValidationExecution(validationExecutionID, "error", "worker panic")
			writeProgress(vp)
			h.store.CreateEvent(runID, "warn", "Validation worker failed unexpectedly")
			h.publishRunEvent(runID, events.KindStepValidation, "validation", "error")
			h.log.Error("validation worker panic", "run_id", runID, "recover", r)
		}
	}()

	// Mark DB execution running
	if err := h.store.MarkValidationExecutionRunning(validationExecutionID); err != nil {
		h.log.Error("mark validation execution running", "error", err)
	}

	vp.MarkRunning()
	writeProgress(vp)
	h.publishRunEvent(runID, events.KindStepValidation, "validation", "running")

	var results []pipeline.CommandRunResult
	allPassed := true
	var combinedStdout, combinedStderr strings.Builder

	for i, cmd := range commands {
		vp.MarkCommandRunning(i + 1)
		writeProgress(vp)
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "running")

		result := pipeline.RunValidationCommand(context.Background(), repoPath, cmd, pipeline.DefaultValidationCommandTimeout)
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

		vp.MarkCommandResult(i+1, result)
		writeProgress(vp)
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "running")
	}

	// Write final run JSON artifact
	aggregate := struct {
		Status   string                      `json:"status"`
		RepoPath string                      `json:"repo_path"`
		Commands []pipeline.CommandRunResult `json:"commands"`
	}{
		Status:   "fail",
		RepoPath: repoPath,
		Commands: results,
	}
	if allPassed {
		aggregate.Status = "pass"
	}

	aggregateJSON, _ := json.MarshalIndent(aggregate, "", "  ")

	h.store.DeleteArtifactsByRunKind(runID, "validation_run_json")
	h.store.DeleteArtifactsByRunKind(runID, "validation_stdout")
	h.store.DeleteArtifactsByRunKind(runID, "validation_stderr")

	jsonPath, err := artifacts.Write(runID, "validation_run_json", pipeline.ArtifactFilename("validation_run_json"), aggregateJSON)
	if err != nil {
		h.log.Error("write validation run json", "error", err)
		vp.MarkError("failed to write validation_run_json: " + err.Error())
		writeProgress(vp)
		h.store.FinishValidationExecution(validationExecutionID, "error", "failed to write validation_run_json: "+err.Error())
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
		vp.MarkFinished("pass")
		h.store.FinishValidationExecution(validationExecutionID, "pass", "")
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "pass")
	} else {
		h.store.UpdateRunStatus(runID, "validation_failed")
		h.store.CreateEvent(runID, "info", "Validation commands failed")
		vp.MarkFinished("fail")
		h.store.FinishValidationExecution(validationExecutionID, "fail", "")
		h.publishRunEvent(runID, events.KindStepValidation, "validation", "fail")
	}

	writeProgress(vp)

	h.log.Info("validation commands executed", "run_id", runID, "status", aggregate.Status, "commands", len(commands))
}

func isValidationExecutionStale(exec *store.ValidationExecution) bool {
	if exec.Status != "starting" && exec.Status != "running" {
		return false
	}
	updated, err := time.Parse("2006-01-02 15:04:05", exec.UpdatedAt)
	if err != nil {
		return true
	}
	return time.Since(updated) > 30*time.Minute
}

func (h *RunsHandler) generateAuditHandoff(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	// Block normal audit handoff generation when mixed committed+uncommitted evidence is present
	if evidenceJSON := readArtifactPreview(runID, "git_change_evidence_json"); evidenceJSON != "" {
		var ev struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &ev); err == nil && ev.Mode == repos.EvidenceModeMixedCommittedUncommitted {
			h.store.CreateEvent(runID, "warn", "Audit handoff blocked: mixed committed and uncommitted changes detected. Resolve uncommitted changes and rerun Inspect Git Diff.")
			setHXPushURL(w, runID, "audit")
			http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
			return
		}
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

	// Load git diff evidence
	gitStatusText := readArtifactPreview(runID, "git_status_text")
	gitDiffStat := readArtifactPreview(runID, "git_diff_stat")
	gitDiffNumstat := readArtifactPreview(runID, "git_diff_numstat")
	gitDiffNameStatus := readArtifactPreview(runID, "git_diff_name_status")
	gitDiffPatch := readArtifactPreview(runID, "git_diff_patch")

	// Load change evidence JSON
	evidenceMode := ""
	evidenceBaseline := ""
	evidenceHead := ""
	evidenceBranch := ""
	evidenceCommitCnt := 0
	evidenceCommits := ""
	evidenceWarning := ""
	if evidenceJSON := readArtifactPreview(runID, "git_change_evidence_json"); evidenceJSON != "" {
		var ev repos.GitChangeEvidence
		if err := json.Unmarshal([]byte(evidenceJSON), &ev); err == nil {
			evidenceMode = ev.Mode
			evidenceBaseline = ev.BaselineSHA
			evidenceHead = ev.CurrentHeadSHA
			evidenceBranch = ev.Branch
			evidenceCommitCnt = ev.CommitCount
			if len(ev.Commits) > 0 {
				var commitLines []string
				for _, c := range ev.Commits {
					commitLines = append(commitLines, "- "+c.ShortSHA+" "+c.Subject)
				}
				evidenceCommits = strings.Join(commitLines, "\n")
			}
			if ev.Mode == repos.EvidenceModeMixedCommittedUncommitted && ev.StatusPorcelain != "" {
				evidenceWarning = "Uncommitted status:\n" + ev.StatusPorcelain
			}
		}
	}

	input := pipeline.AuditHandoffInput{
		RunID:              runID,
		Title:              run.Title,
		RepoName:           repoName,
		BranchName:         run.BranchName,
		Status:             run.Status,
		SelectedModel:      run.SelectedModel,
		RecommendedModel:   run.RecommendedModel,
		OriginalHandoff:    originalHandoff,
		AgentResultStatus:  agentResultStatus,
		BuildStatus:        buildStatus,
		TestStatus:         testStatus,
		LOCChanged:         locChanged,
		ResultRaw:          agentResultRaw,
		ValidationStatus:   validationStatus,
		ValidationRepoPath: validationRepoPath,
		ValidationCommands: validationCommands,
		GitStatusText:      gitStatusText,
		GitDiffStat:        gitDiffStat,
		GitDiffNumstat:     gitDiffNumstat,
		GitDiffNameStatus:  gitDiffNameStatus,
		GitDiffPatch:       gitDiffPatch,
		EvidenceMode:       evidenceMode,
		BaselineSHA:        evidenceBaseline,
		CurrentHeadSHA:     evidenceHead,
		EvidenceBranch:     evidenceBranch,
		CommitCount:        evidenceCommitCnt,
		Commits:            evidenceCommits,
		EvidenceWarning:    evidenceWarning,
	}

	content := pipeline.BuildAuditHandoff(input)

	// Clear stale audit clearance before replacing the handoff packet.
	h.deleteRunArtifactKind(runID, "audit_clearance_json")

	// Delete stale commit artifacts that depend on the audit handoff
	h.store.DeleteArtifactsByRunKind(runID, "commit_message_text")
	h.store.DeleteArtifactsByRunKind(runID, "commit_suggestion_json")

	artifactPath, err := artifacts.Write(runID, "audit_handoff", pipeline.ArtifactFilename("audit_handoff"), []byte(content))
	if err != nil {
		h.log.Error("write audit handoff", "error", err)
		http.Error(w, "failed to save audit handoff", http.StatusInternalServerError)
		return
	}
	// Delete existing audit handoff rows so the regenerated handoff replaces stale artifacts
	if err := h.store.DeleteArtifactsByRunKind(runID, "audit_handoff"); err != nil {
		h.log.Error("delete previous audit handoff artifact rows", "error", err)
	}
	h.store.CreateArtifact(runID, "audit_handoff", artifactPath, "text/markdown")
	h.store.CreateEvent(runID, "info", "Audit handoff generated")
	h.publishRunEvent(runID, events.KindStepAudit, "audit", "generated")
	h.log.Info("audit handoff generated", "run_id", runID)

	setHXPushURL(w, runID, "audit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
}

func (h *RunsHandler) inspectDiff(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		h.log.Error("get run for inspect-diff", "error", err)
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, err := h.store.GetRepo(run.RepoID)
	if err != nil {
		h.log.Error("get repo for inspect-diff", "error", err)
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if repo.Path == "" {
		h.store.CreateEvent(runID, "warn", "No repo path configured for git diff inspection")
		setHXPushURL(w, runID, "audit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
		return
	}

	info, err := os.Stat(repo.Path)
	if err != nil || !info.IsDir() {
		h.store.CreateEvent(runID, "warn", "Repo path does not exist or is not a directory: "+repo.Path)
		setHXPushURL(w, runID, "audit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
		return
	}

	// Clear existing diff artifacts and downstream stale audit/commit artifacts
	// before collecting new evidence, so stale data is removed even if the
	// git command fails.
	for _, kind := range []string{
		"git_status_text",
		"git_diff_stat",
		"git_diff_numstat",
		"git_diff_name_status",
		"git_diff_patch",
		"git_change_evidence_json",
		"audit_clearance_json",
		"audit_handoff",
		"commit_message_text",
		"commit_suggestion_json",
	} {
		h.store.DeleteArtifactsByRunKind(runID, kind)
	}
	h.deleteRunArtifactKind(runID, "audit_clearance_json")

	// Resolve authoritative baseline
	baselineSHA := h.resolveRunGitBaseline(runID, run)

	// Capture change evidence using the new repos-level function
	evidence := repos.CaptureGitChangeEvidence(repo.Path, baselineSHA)

	// Write git_change_evidence_json artifact
	if evidenceJSON, err := json.MarshalIndent(evidence, "", "  "); err == nil {
		if p, err := artifacts.Write(runID, "git_change_evidence_json", pipeline.ArtifactFilename("git_change_evidence_json"), evidenceJSON); err == nil {
			h.store.CreateArtifact(runID, "git_change_evidence_json", p, "application/json")
		}
	}

	// Write artifacts using selected evidence source
	writeGitArtifact := func(kind, content string, mimeType string) {
		if content == "" {
			return
		}
		path, err := artifacts.Write(runID, kind, pipeline.ArtifactFilename(kind), []byte(content))
		if err != nil {
			h.log.Error("write git diff artifact", "kind", kind, "error", err)
			return
		}
		h.store.CreateArtifact(runID, kind, path, mimeType)
	}

	writeGitArtifact("git_status_text", evidence.StatusPorcelain, "text/plain")
	writeGitArtifact("git_diff_stat", evidence.Stat, "text/plain")
	writeGitArtifact("git_diff_numstat", evidence.Numstat, "text/plain")
	writeGitArtifact("git_diff_name_status", evidence.NameStatus, "text/plain")
	writeGitArtifact("git_diff_patch", evidence.Patch, "text/plain")

	// For committed-range and mixed mode, update runs.head_commit to current HEAD
	if evidence.CurrentHeadSHA != "" && (evidence.Mode == repos.EvidenceModeCommittedRange || evidence.Mode == repos.EvidenceModeMixedCommittedUncommitted) {
		if _, err := h.store.UpdateRunBranch(runID, evidence.Branch, run.BaseCommit, evidence.CurrentHeadSHA); err != nil {
			h.log.Warn("update run head_commit for evidence mode", "run_id", runID, "mode", evidence.Mode, "error", err)
		}
	}

	eventMsg := "Git diff inspection completed"
	switch evidence.Mode {
	case repos.EvidenceModeNoChanges:
		eventMsg += ": no changes detected"
	case repos.EvidenceModeUncommittedWorktree, repos.EvidenceModeBaselineUnavailableDirty:
		eventMsg += ": uncommitted changes"
	case repos.EvidenceModeCommittedRange:
		eventMsg += ": " + strconv.Itoa(evidence.CommitCount) + " commit(s) detected in range"
	case repos.EvidenceModeMixedCommittedUncommitted:
		eventMsg += ": mixed committed + uncommitted changes detected (caution)"
	case repos.EvidenceModeBaselineUnavailableClean:
		eventMsg += ": baseline unavailable, no working tree changes"
	}
	h.store.CreateEvent(runID, "info", eventMsg)
	h.publishRunEvent(runID, events.KindStepAudit, "audit", evidence.Mode)

	h.log.Info("git diff inspection completed", "run_id", runID, "mode", evidence.Mode)

	setHXPushURL(w, runID, "audit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
}

// resolveRunGitBaseline resolves the authoritative baseline SHA from artifacts and DB.
// Resolution order:
// 1. git_baseline_json.authoritative_baseline_sha
// 2. git_baseline_json.agent_start.head_sha
// 3. git_baseline_json.run_created.head_sha
// 4. runs.base_commit
func (h *RunsHandler) resolveRunGitBaseline(runID int64, run *store.Run) string {
	data, err := artifacts.Read(runID, "git_baseline_json", pipeline.ArtifactFilename("git_baseline_json"))
	if err != nil {
		// Fallback to DB field
		return run.BaseCommit
	}

	var baseline repos.GitBaselineArtifact
	if err := json.Unmarshal(data, &baseline); err != nil {
		return run.BaseCommit
	}

	// Check authoritative baseline first
	if baseline.AuthoritativeBaselineSHA != "" {
		return baseline.AuthoritativeBaselineSHA
	}

	// Check agent_start head_sha
	if baseline.AgentStart != nil && baseline.AgentStart.HeadSHA != "" {
		return baseline.AgentStart.HeadSHA
	}

	// Check run_created head_sha
	if baseline.RunCreated != nil && baseline.RunCreated.HeadSHA != "" {
		return baseline.RunCreated.HeadSHA
	}

	// Fallback to DB field
	return run.BaseCommit
}

func (h *RunsHandler) replaceOriginalHandoff(w http.ResponseWriter, r *http.Request, runID int64) {
	rawText := r.FormValue("handoff_text")
	handoffText := strings.TrimSpace(rawText)
	if handoffText == "" {
		h.store.CreateEvent(runID, "warn", "Replace handoff skipped: handoff text is empty")
		setHXPushURL(w, runID, "intake")
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
		"validation_progress_json",
		"validation_run_json",
		"validation_stdout",
		"validation_stderr",
		"git_status_text",
		"git_diff_stat",
		"git_diff_numstat",
		"git_diff_name_status",
		"git_diff_patch",
		"audit_handoff",
		"commit_message_text",
		"commit_suggestion_json",
		"opencode_stdout",
		"opencode_stderr",
		"opencode_combined_log",
		"agent_result_raw",
		"agent_result_json",
	}
	for _, kind := range staleKinds {
		h.store.DeleteArtifactsByRunKind(runID, kind)
	}

	// Clear stale checks
	h.store.DeleteChecksByRunKind(runID, "validation")
	h.store.DeleteChecksByRunKind(runID, "validation_run")
	h.store.DeleteChecksByRunKind(runID, "agent_result")

	// Reset run status to draft so validation can re-run
	h.store.UpdateRunStatus(runID, "draft")

	h.store.CreateEvent(runID, "info", "Original handoff replaced; re-running Intake Review")
	h.publishRunEvent(runID, events.KindRunSummary, "intake", "updated")

	h.log.Info("original handoff replaced", "run_id", runID)

	// Re-run Intake Review using the new handoff text
	h.validateHandoff(w, r, runID)
}

func (h *RunsHandler) deleteRunArtifactKind(runID int64, kind string) {
	_ = h.store.DeleteArtifactsByRunKind(runID, kind)
	if err := artifacts.Delete(runID, kind, pipeline.ArtifactFilename(kind)); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.log.Warn("delete stale artifact file", "run_id", runID, "kind", kind, "error", err)
	}
}

func (h *RunsHandler) updateSelectedModel(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	selectedModelOption := r.FormValue("selected_model_option")
	selectedModelCustom := r.FormValue("selected_model_custom")

	newSelectedModel, _ := pipeline.ResolveSelectedModel(selectedModelOption, selectedModelCustom, run.RecommendedModel)

	_, err = h.store.UpdateRunModel(runID, run.RecommendedModel, newSelectedModel)
	if err != nil {
		h.log.Error("update run model", "error", err)
		http.Error(w, "failed to update model", http.StatusInternalServerError)
		return
	}

	h.store.CreateEvent(runID, "info", "Selected model updated to "+newSelectedModel)
	h.publishRunEvent(runID, events.KindRunSummary, "handoff", "model_updated")

	// Delete stale OpenCode artifacts (DB rows + disk files) that encode the old selected model
	for _, kind := range []string{"opencode_handoff_packet", "opencode_cli_check_json", "opencode_dry_run_json"} {
		h.deleteRunArtifactKind(runID, kind)
	}

	// Regenerate the OpenCode packet if agent_prompt exists
	artifacts, artErr := h.store.ListArtifactsByRun(runID)
	if artErr == nil {
		for _, a := range artifacts {
			if a.Kind == "agent_prompt" {
				h.generateOpenCodePacket(w, r, runID)
				return
			}
		}
	}

	setHXPushURL(w, runID, "handoff")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=handoff", http.StatusSeeOther)
}

func (h *RunsHandler) acceptAuditClearance(w http.ResponseWriter, r *http.Request, runID int64) {
	artList, err := h.store.ListArtifactsByRun(runID)
	if err != nil {
		h.store.CreateEvent(runID, "warn", "Accept audit clearance failed: cannot list artifacts.")
		h.publishRunEvent(runID, events.KindStepAudit, "audit", "blocked")
		setHXPushURL(w, runID, "audit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
		return
	}
	auditHandoffArtifactID := int64(0)
	for _, a := range artList {
		if a.Kind == "audit_handoff" {
			auditHandoffArtifactID = a.ID
			break
		}
	}
	if auditHandoffArtifactID == 0 {
		h.store.CreateEvent(runID, "warn", "Accept audit clearance failed: no audit handoff found.")
		h.publishRunEvent(runID, events.KindStepAudit, "audit", "blocked")
		setHXPushURL(w, runID, "audit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
		return
	}

	clearance := repos.AuditClearance{
		Status:                 "accepted",
		AcceptedAt:             time.Now().UTC().Format(time.RFC3339),
		Source:                 "manual_ui",
		AuditHandoffArtifactID: auditHandoffArtifactID,
	}
	clearanceJSON, _ := json.MarshalIndent(clearance, "", "  ")

	h.deleteRunArtifactKind(runID, "audit_clearance_json")
	p, err := artifacts.Write(runID, "audit_clearance_json", pipeline.ArtifactFilename("audit_clearance_json"), clearanceJSON)
	if err != nil {
		h.log.Error("write audit clearance", "error", err)
		http.Error(w, "failed to save audit clearance", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "audit_clearance_json", p, "application/json")
	h.store.CreateEvent(runID, "info", "Audit clearance accepted")
	h.publishRunEvent(runID, events.KindStepAudit, "audit", "accepted")

	setHXPushURL(w, runID, "audit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
}

func (h *RunsHandler) revokeAuditClearance(w http.ResponseWriter, r *http.Request, runID int64) {
	h.deleteRunArtifactKind(runID, "audit_clearance_json")
	h.store.CreateEvent(runID, "info", "Audit clearance revoked")
	h.publishRunEvent(runID, events.KindStepAudit, "audit", "revoked")

	setHXPushURL(w, runID, "audit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
}

// refreshGitChangeEvidenceArtifacts re-resolves the git baseline, re-captures change evidence,
// and rewrites git evidence artifacts. It returns the fresh evidence or nil on failure.
func (h *RunsHandler) refreshGitChangeEvidenceArtifacts(runID int64, run *store.Run, repoPath string) *repos.GitChangeEvidence {
	baselineSHA := h.resolveRunGitBaseline(runID, run)
	evidence := repos.CaptureGitChangeEvidence(repoPath, baselineSHA)
	if evidence.Error != "" {
		h.log.Warn("refresh git change evidence failed", "run_id", runID, "error", evidence.Error)
		return evidence
	}

	// Write git_change_evidence_json artifact
	if evidenceJSON, err := json.MarshalIndent(evidence, "", "  "); err == nil {
		h.store.DeleteArtifactsByRunKind(runID, "git_change_evidence_json")
		if p, err := artifacts.Write(runID, "git_change_evidence_json", pipeline.ArtifactFilename("git_change_evidence_json"), evidenceJSON); err == nil {
			h.store.CreateArtifact(runID, "git_change_evidence_json", p, "application/json")
		}
	}

	// Write artifact files
	writeGitArtifact := func(kind, content string, mimeType string) {
		if content == "" {
			return
		}
		h.store.DeleteArtifactsByRunKind(runID, kind)
		path, err := artifacts.Write(runID, kind, pipeline.ArtifactFilename(kind), []byte(content))
		if err != nil {
			h.log.Error("refresh: write git artifact", "kind", kind, "error", err)
			return
		}
		h.store.CreateArtifact(runID, kind, path, mimeType)
	}

	writeGitArtifact("git_status_text", evidence.StatusPorcelain, "text/plain")
	writeGitArtifact("git_diff_stat", evidence.Stat, "text/plain")
	writeGitArtifact("git_diff_numstat", evidence.Numstat, "text/plain")
	writeGitArtifact("git_diff_name_status", evidence.NameStatus, "text/plain")
	writeGitArtifact("git_diff_patch", evidence.Patch, "text/plain")

	// Update runs.head_commit when current HEAD is available
	if evidence.CurrentHeadSHA != "" {
		if _, err := h.store.UpdateRunBranch(runID, evidence.Branch, run.BaseCommit, evidence.CurrentHeadSHA); err != nil {
			h.log.Warn("refresh: update run branch after evidence refresh", "run_id", runID, "error", err)
		}
	}

	return evidence
}

func (h *RunsHandler) createGitCommit(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, _ := h.store.GetRepo(run.RepoID)
	if repo == nil || repo.Path == "" {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: no repo path configured.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Check validation
	validationJSON := readArtifactPreview(runID, "validation_run_json")
	if validationJSON == "" {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: run validation first.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}
	var v struct{ Status string }
	json.Unmarshal([]byte(validationJSON), &v)
	validationPassed := v.Status == "pass"
	validationAccepted := run.Status == "validation_failed_accepted"
	if !validationPassed && !validationAccepted {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: validation did not pass.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Check audit clearance
	clearanceData := readArtifactPreview(runID, "audit_clearance_json")
	if clearanceData == "" {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: audit clearance not accepted.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}
	var clearance repos.AuditClearance
	if err := json.Unmarshal([]byte(clearanceData), &clearance); err != nil || clearance.Status != "accepted" {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: audit clearance not accepted.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Load evidence mode
	evidenceJSON := readArtifactPreview(runID, "git_change_evidence_json")
	if evidenceJSON == "" {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: inspect git diff first.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}
	var ev struct {
		Mode string `json:"mode"`
	}
	json.Unmarshal([]byte(evidenceJSON), &ev)
	if ev.Mode != repos.EvidenceModeUncommittedWorktree && ev.Mode != repos.EvidenceModeBaselineUnavailableDirty {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: evidence mode is "+ev.Mode+". Only uncommitted worktree can be committed via UI.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Get commit message
	suggestionJSON := readArtifactPreview(runID, "commit_suggestion_json")
	var message string
	if suggestionJSON != "" {
		var sugg struct {
			Selected string `json:"selected"`
		}
		if err := json.Unmarshal([]byte(suggestionJSON), &sugg); err == nil && sugg.Selected != "" {
			message = sugg.Selected
		}
	}
	if message == "" {
		h.store.CreateEvent(runID, "warn", "Create commit blocked: no commit message. Prepare Git Commit first.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Execute commit
	result := repos.CreateGitCommit(repo.Path, message)

	// Store result artifact
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	h.store.DeleteArtifactsByRunKind(runID, "git_commit_result_json")
	p, err := artifacts.Write(runID, "git_commit_result_json", pipeline.ArtifactFilename("git_commit_result_json"), resultJSON)
	if err != nil {
		h.log.Error("write git commit result", "error", err)
	} else {
		h.store.CreateArtifact(runID, "git_commit_result_json", p, "application/json")
	}

	// Also store readable text artifact
	if result.Success {
		textContent := "Commit created successfully.\nSHA: " + result.SHA + "\nSubject: " + result.Subject + "\nBranch: " + result.Branch + "\nTimestamp: " + result.Timestamp
		textPath, _ := artifacts.Write(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text"), []byte(textContent))
		if textPath != "" {
			h.store.CreateArtifact(runID, "commit_message_text", textPath, "text/plain")
		}

		// Update run head_commit if a commit was created
		if result.SHA != "" {
			h.store.UpdateRunBranch(runID, result.Branch, run.BaseCommit, result.SHA)
		}

		// Refresh git change evidence so the downstream state reflects the committed state
		h.refreshGitChangeEvidenceArtifacts(runID, run, repo.Path)
	}

	if result.Success {
		h.store.CreateEvent(runID, "info", "Git commit created: "+result.ShortSHA+" "+result.Subject)
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "created")
		h.log.Info("git commit created", "run_id", runID, "sha", result.SHA, "subject", result.Subject)
	} else {
		h.store.CreateEvent(runID, "warn", "Git commit failed: "+result.Error)
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "failed")
	}

	setHXPushURL(w, runID, "commit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
}

func (h *RunsHandler) pushGitCommit(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, _ := h.store.GetRepo(run.RepoID)
	if repo == nil || repo.Path == "" {
		h.store.CreateEvent(runID, "warn", "Push blocked: no repo path configured.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Check validation
	validationJSON := readArtifactPreview(runID, "validation_run_json")
	if validationJSON == "" {
		h.store.CreateEvent(runID, "warn", "Push blocked: run validation first.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}
	var v struct{ Status string }
	json.Unmarshal([]byte(validationJSON), &v)
	validationPassed := v.Status == "pass"
	validationAccepted := run.Status == "validation_failed_accepted"
	if !validationPassed && !validationAccepted {
		h.store.CreateEvent(runID, "warn", "Push blocked: validation did not pass.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Check audit clearance
	clearanceData := readArtifactPreview(runID, "audit_clearance_json")
	if clearanceData == "" {
		h.store.CreateEvent(runID, "warn", "Push blocked: audit clearance not accepted.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}
	var clearance repos.AuditClearance
	if err := json.Unmarshal([]byte(clearanceData), &clearance); err != nil || clearance.Status != "accepted" {
		h.store.CreateEvent(runID, "warn", "Push blocked: audit clearance not accepted.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Allow push when:
	// - evidence mode is committed_range, OR
	// - a successful commit result exists and HEAD matches that commit SHA
	evidenceJSON := readArtifactPreview(runID, "git_change_evidence_json")
	if evidenceJSON == "" {
		h.store.CreateEvent(runID, "warn", "Push blocked: inspect git diff first.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}
	var ev struct{ Mode string }
	json.Unmarshal([]byte(evidenceJSON), &ev)

	// Check commit result as an alternative to committed_range evidence
	commitResultData := readArtifactPreview(runID, "git_commit_result_json")
	hasCommitResult := false
	commitResultSuccess := false
	commitResultSHA := ""
	if commitResultData != "" {
		var cr repos.GitCommitResult
		if err := json.Unmarshal([]byte(commitResultData), &cr); err == nil {
			hasCommitResult = true
			commitResultSuccess = cr.Success
			commitResultSHA = cr.SHA
		}
	}

	evidenceOK := ev.Mode == repos.EvidenceModeCommittedRange
	if !evidenceOK && hasCommitResult && commitResultSuccess {
		// Also accept if commit result SHA matches current HEAD
		snap := repos.CaptureGitSnapshot(repo.Path, "push_preflight_commit_check")
		if snap.Error == "" && snap.HeadSHA == commitResultSHA {
			evidenceOK = true
		}
	}

	if !evidenceOK {
		h.store.CreateEvent(runID, "warn", "Push blocked: evidence mode is "+ev.Mode+". Only committed range can be pushed.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Worktree must be clean
	snap := repos.CaptureGitSnapshot(repo.Path, "push_preflight")
	if snap.Error != "" || snap.Dirty {
		h.store.CreateEvent(runID, "warn", "Push blocked: working tree is dirty. Commit or stash changes first.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Check upstream exists
	upstream, err := repos.GetUpstreamInfo(repo.Path)
	if err != nil {
		h.store.CreateEvent(runID, "warn", "Push blocked: no upstream configured. "+err.Error())
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}
	_ = upstream

	// Dry run first
	dryRunResult := repos.DryRunPush(repo.Path)
	dryRunJSON, _ := json.MarshalIndent(dryRunResult, "", "  ")
	h.store.DeleteArtifactsByRunKind(runID, "git_push_dry_run_json")
	dryRunPath, err := artifacts.Write(runID, "git_push_dry_run_json", pipeline.ArtifactFilename("git_push_dry_run_json"), dryRunJSON)
	if err == nil {
		h.store.CreateArtifact(runID, "git_push_dry_run_json", dryRunPath, "application/json")
	}
	h.publishRunEvent(runID, events.KindStepCommit, "commit", "dry_run")

	if !dryRunResult.DryRunPass {
		h.store.CreateEvent(runID, "warn", "Push dry run failed. Check git_push_dry_run_json artifact for details.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "blocked")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Execute push
	result := repos.PushGitCommit(repo.Path)
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	h.store.DeleteArtifactsByRunKind(runID, "git_push_result_json")
	p, err := artifacts.Write(runID, "git_push_result_json", pipeline.ArtifactFilename("git_push_result_json"), resultJSON)
	if err != nil {
		h.log.Error("write push result", "error", err)
	} else {
		h.store.CreateArtifact(runID, "git_push_result_json", p, "application/json")
	}

	if result.Success {
		h.store.CreateEvent(runID, "info", "Git push succeeded.")
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "pushed")
		h.log.Info("git push succeeded", "run_id", runID)
	} else {
		h.store.CreateEvent(runID, "warn", "Git push failed: "+result.Error)
		h.publishRunEvent(runID, events.KindStepCommit, "commit", "failed")
	}

	setHXPushURL(w, runID, "commit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
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
	h.publishRunEvent(runID, events.KindStepArtifacts, "opencode", "packet")

	h.log.Info("opencode handoff packet generated", "run_id", runID)

	setHXPushURL(w, runID, "handoff")
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
		setHXPushURL(w, runID, "intake")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
		return
	}

	metadata := pipeline.ParseHandoffMetadata(string(handoffData), repoDefaults)
	review := pipeline.BuildIntakeReview(metadata, repoPath)

	if len(review.Warnings) == 0 && len(review.Blockers) == 0 {
		h.store.CreateEvent(runID, "info", "No intake review warnings or blockers found; no fix handoff needed.")
		setHXPushURL(w, runID, "intake")
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

	setHXPushURL(w, runID, "intake")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=intake", http.StatusSeeOther)
}

// buildExecutionPreviews populates transcript and parsed result previews from the latest execution.
// It also auto-reconciles a running OpenCode execution when safe to do so and returns whether
// that reconciliation changed persisted state.
func (h *RunsHandler) buildExecutionPreviews(runID int64, run *store.Run, artifactsList []store.Artifact) (views.RunPreviews, bool) {
	var previews views.RunPreviews
	changed := false
	now := time.Now()

	exec, err := h.store.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		return previews, changed
	}

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
	if exec.Error.Valid {
		previews.OpenCodeExecutionError = exec.Error.String
	}
	previews.OpenCodeCommandPreview = exec.CommandPreview

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

	stdoutPreview := readArtifactPreview(runID, "opencode_stdout")
	stderrPreview := readArtifactPreview(runID, "opencode_stderr")
	previews.HasOpenCodeOutput = stdoutPreview != "" || stderrPreview != ""
	previews.OpenCodePermissionWarning = pipeline.OpenCodePermissionWarning(stderrPreview)
	if lastOutputAt, ok := latestOpenCodeOutputTime(runID); ok {
		previews.HasOpenCodeOutput = true
		previews.OpenCodeLastOutputAt = lastOutputAt.Local().Format("2006-01-02 15:04:05")
		previews.OpenCodeLastOutputAge = formatDurationCompact(now.Sub(lastOutputAt)) + " ago"
	}

	if progressData := readArtifactPreview(runID, "opencode_stream_progress_json"); progressData != "" {
		var sp pipeline.StreamProgress
		if err := json.Unmarshal([]byte(progressData), &sp); err == nil {
			previews.OpenCodeStreamStdoutChunks = sp.StdoutChunks
			previews.OpenCodeStreamStderrChunks = sp.StderrChunks
			previews.OpenCodeStreamStdoutBytes = sp.StdoutBytes
			previews.OpenCodeStreamStderrBytes = sp.StderrBytes
			previews.OpenCodeStreamLastChunkAt = sp.LastChunkAt
			if sp.LastChunkAt != "" {
				if t, ok := parseExecutionTimestamp(sp.LastChunkAt); ok {
					previews.OpenCodeStreamLastChunkAge = formatDurationCompact(now.Sub(t)) + " ago"
				}
			}
			previews.HasOpenCodeStreamActivity = sp.StdoutChunks > 0 || sp.StderrChunks > 0 || sp.LastChunkAt != ""
		}
	}

	events := pipeline.BuildOpenCodeTranscript(stdoutPreview, stderrPreview, 200)
	for _, ev := range events {
		previews.OpenCodeTranscript = append(previews.OpenCodeTranscript, views.OpenCodeTranscriptEventView{
			Kind: ev.Kind,
			Text: ev.Text,
		})
	}

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

		if isOpenCodeExecutionRunning(previews.OpenCodeExecutionStatus) && (parsed.Status == pipeline.AgentResultDone || parsed.Status == pipeline.AgentResultBlocked) {
			result, reconcileErr := h.reconcileOpenCodeExecution(runID)
			if reconcileErr == nil {
				changed = result.Changed
				h.log.Info("auto-reconciled stale opencode execution from GET path", "run_id", runID)
				previews.HasOpenCodeRunning = false
				previews.HasOpenCodeStaleRunning = false
				previews.OpenCodeStaleReason = ""
				previews.OpenCodeCanRecover = false
				previews.OpenCodeRecoveryActionLabel = ""
				if result.FinalStatus != "" {
					previews.OpenCodeExecutionStatus = result.FinalStatus
					switch result.FinalStatus {
					case "completed":
						previews.OpenCodeLifecycleState = "completed"
					case "failed":
						previews.OpenCodeLifecycleState = "failed"
					default:
						previews.OpenCodeLifecycleState = "none"
					}
				}
				if refreshedExec, err2 := h.store.GetLatestAgentExecutionByRun(runID); err2 == nil {
					exec = refreshedExec
					previews.OpenCodeExecutionStatus = refreshedExec.Status
					if refreshedExec.ExitCode.Valid {
						previews.OpenCodeExecutionExitCode = strconv.FormatInt(refreshedExec.ExitCode.Int64, 10)
					}
					if refreshedExec.StartedAt.Valid {
						previews.OpenCodeExecutionStarted = refreshedExec.StartedAt.String
					}
					if refreshedExec.FinishedAt.Valid {
						previews.OpenCodeExecutionFinished = refreshedExec.FinishedAt.String
					}
					previews.OpenCodeRuntime = formatOpenCodeRuntime(previews.OpenCodeExecutionStarted, previews.OpenCodeExecutionFinished, now)
					if refreshedExec.Status == "completed" {
						previews.OpenCodeLifecycleState = "completed"
					} else if refreshedExec.Status == "failed" {
						previews.OpenCodeLifecycleState = "failed"
					} else {
						previews.OpenCodeLifecycleState = "none"
					}
					previews.HasOpenCodeRunning = false
					previews.HasOpenCodeStaleRunning = false
					previews.OpenCodeStaleReason = ""
					previews.OpenCodeCanRecover = false
					previews.OpenCodeRecoveryActionLabel = ""
				}
			} else {
				h.log.Warn("auto-reconcile opencode execution failed", "run_id", runID, "error", reconcileErr)
			}
		}
	}

	if isOpenCodeExecutionRunning(previews.OpenCodeExecutionStatus) {
		liveness := evaluateOpenCodeExecutionLiveness(runID, exec, now)
		applyOpenCodeExecutionLiveness(&previews, liveness)

		// For stale states without a real DONE/BLOCKED result, clear the
		// parsed result fields so the template does not show confusing
		// "UNKNOWN" status alongside the stale recovery warning.
		if (previews.OpenCodeLifecycleState == "stale_output" || previews.OpenCodeLifecycleState == "stale_timeout") &&
			previews.OpenCodeParsedResultStatus != "" &&
			previews.OpenCodeParsedResultStatus != "DONE" && previews.OpenCodeParsedResultStatus != "BLOCKED" {
			previews.OpenCodeParsedResultStatus = ""
			previews.OpenCodeParsedBuildStatus = ""
			previews.OpenCodeParsedTestStatus = ""
			previews.OpenCodeParsedLOCChanged = ""
			previews.OpenCodeParsedResultRaw = ""
		}
	} else if previews.OpenCodeLifecycleState == "" {
		switch previews.OpenCodeExecutionStatus {
		case "completed":
			previews.OpenCodeLifecycleState = "completed"
		case "failed":
			previews.OpenCodeLifecycleState = "failed"
		default:
			previews.OpenCodeLifecycleState = "none"
		}
	}

	return previews, changed
}

func formatPromptEstimate(est pipeline.PromptEstimate) string {
	kb := float64(est.Bytes) / 1024.0
	return fmt.Sprintf("%.1f KB (~%d tokens, approximate)", kb, est.ApproxTokens)
}

func truncatePreviewText(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "\n..."
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
	case "intake", "prompt", "packet", "handoff", "run", "validation", "audit", "commit":
		return step
	default:
		return "intake"
	}
}

func parseValidationProgressPreview(jsonData string) views.ValidationProgressPreview {
	if jsonData == "" {
		return views.ValidationProgressPreview{}
	}
	var vp pipeline.ValidationProgress
	if err := json.Unmarshal([]byte(jsonData), &vp); err != nil {
		return views.ValidationProgressPreview{}
	}
	preview := views.ValidationProgressPreview{
		Status:         vp.Status,
		RepoPath:       vp.RepoPath,
		StartedAt:      vp.StartedAt,
		UpdatedAt:      vp.UpdatedAt,
		FinishedAt:     vp.FinishedAt,
		CurrentIndex:   vp.CurrentIndex,
		CurrentCommand: vp.CurrentCommand,
		TotalCommands:  vp.TotalCommands,
		Error:          vp.Error,
	}
	for i, pc := range vp.Commands {
		if pc.Index == 0 {
			pc.Index = i + 1
		}
		preview.Commands = append(preview.Commands, views.ValidationProgressCommandView{
			Index:       pc.Index,
			Label:       pc.Label,
			Command:     pc.Command,
			Source:      pc.Source,
			Status:      pc.Status,
			StartedAt:   pc.StartedAt,
			CompletedAt: pc.CompletedAt,
			ExitCode:    pc.ExitCode,
			TimedOut:    pc.TimedOut,
			DurationMs:  pc.DurationMs,
			HasStdout:   pc.HasStdout,
			HasStderr:   pc.HasStderr,
		})
		switch pc.Status {
		case "pass":
			preview.PassedCount++
			preview.CompletedCount++
		case "fail":
			preview.FailedCount++
			preview.CompletedCount++
		case "timed_out":
			preview.TimedOutCount++
			preview.CompletedCount++
		case "skipped":
			preview.SkippedCount++
			preview.CompletedCount++
		case "running":
			preview.RunningCount++
		case "error":
			preview.ErrorCount++
			preview.CompletedCount++
		default:
			preview.PendingCount++
		}
	}
	if preview.TotalCommands == 0 {
		preview.TotalCommands = len(preview.Commands)
	}
	observed := preview.CompletedCount + preview.RunningCount + preview.PendingCount
	if preview.TotalCommands > observed {
		preview.PendingCount += preview.TotalCommands - observed
	}
	return preview
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

func (h *RunsHandler) acceptValidationFailure(w http.ResponseWriter, r *http.Request, runID int64) {
	checks, err := h.store.ListChecksByRun(runID)
	if err != nil {
		h.log.Error("list checks for accept-validation-failure", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hasFailedCheck := false
	for _, c := range checks {
		if c.Kind == "validation_run" && c.Status == "fail" {
			hasFailedCheck = true
			break
		}
	}

	if !hasFailedCheck {
		h.store.CreateEvent(runID, "warn", "Cannot accept validation failure: no failed validation run found.")
		setHXPushURL(w, runID, "validation")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
		return
	}

	h.store.UpdateRunStatus(runID, "validation_failed_accepted")
	h.store.CreateEvent(runID, "info", "Validation failure accepted; continuing to diff/audit.")
	setHXPushURL(w, runID, "audit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
}

func (h *RunsHandler) prepareGitCommit(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := h.store.GetRun(runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	repo, _ := h.store.GetRepo(run.RepoID)
	if repo == nil {
		h.store.CreateEvent(runID, "warn", "Cannot prepare commit: no repo configured for this run.")
		setHXPushURL(w, runID, "audit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
		return
	}

	if repo.Path == "" {
		h.store.CreateEvent(runID, "warn", "Cannot prepare commit: repo path is empty.")
		setHXPushURL(w, runID, "audit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=audit", http.StatusSeeOther)
		return
	}

	// Check validation status from artifact
	validationJSON := readArtifactPreview(runID, "validation_run_json")
	if validationJSON == "" {
		h.store.CreateEvent(runID, "warn", "Prepare Git Commit blocked: run validation first.")
		setHXPushURL(w, runID, "validation")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
		return
	}
	var validationRaw struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(validationJSON), &validationRaw)
	validationStatus := validationRaw.Status

	validationAcceptedWithFailure := run.Status == "validation_failed_accepted"
	if validationStatus == "fail" && !validationAcceptedWithFailure {
		h.store.CreateEvent(runID, "warn", "Prepare Git Commit blocked: validation failed and has not been accepted.")
		setHXPushURL(w, runID, "validation")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
		return
	}

	// Check artifact evidence lists for comprehensive checks
	artifactsList, _ := h.store.ListArtifactsByRun(runID)

	gitStatusText := readArtifactPreview(runID, "git_status_text")
	gitDiffStat := readArtifactPreview(runID, "git_diff_stat")
	gitDiffNameStatus := readArtifactPreview(runID, "git_diff_name_status")
	gitDiffPatch := readArtifactPreview(runID, "git_diff_patch")
	hasGitStatus := gitStatusText != ""
	hasGitDiffStat := gitDiffStat != ""
	hasGitDiffPatch := gitDiffPatch != ""
	hasGitDiffNameStatus := hasArtifactKind(artifactsList, "git_diff_name_status") || gitDiffNameStatus != ""
	hasGitDiffEvidence := hasGitStatus || hasGitDiffStat || hasGitDiffPatch || hasGitDiffNameStatus

	if !hasGitDiffEvidence {
		h.store.CreateEvent(runID, "warn", "Prepare Git Commit blocked: inspect git diff first.")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	auditHandoffPreview := readArtifactPreview(runID, "audit_handoff")
	hasAuditHandoff := auditHandoffPreview != "" || hasArtifactKind(artifactsList, "audit_handoff")
	if !hasAuditHandoff {
		h.store.CreateEvent(runID, "warn", "Prepare Git Commit blocked: generate audit handoff first.")
		setHXPushURL(w, runID, "commit")
		http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
		return
	}

	// Read input data for commit suggestion
	originalHandoff := readArtifactPreview(runID, "original_handoff")

	// Count changed files from diff stat
	changedFileCount := int64(0)
	if gitDiffStat != "" {
		for _, line := range strings.Split(gitDiffStat, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, " ") {
				changedFileCount++
			}
		}
	}

	// Parse agent result for status info
	agentResultStatus := ""
	agentBuildStatus := ""
	agentTestStatus := ""
	agentLOCChanged := ""
	if agentRaw := readArtifactPreview(runID, "agent_result_raw"); agentRaw != "" {
		parsed := pipeline.ParseAgentResult(agentRaw)
		agentResultStatus = string(parsed.Status)
		agentBuildStatus = parsed.BuildStatus
		agentTestStatus = parsed.TestStatus
		agentLOCChanged = parsed.LOCChanged
	}

	// Load evidence mode and commits for better commit message
	evidenceMode := ""
	var evidenceCommits []string
	if evidenceJSON := readArtifactPreview(runID, "git_change_evidence_json"); evidenceJSON != "" {
		var ev struct {
			Mode    string                   `json:"mode"`
			Commits []repos.GitCommitSummary `json:"commits"`
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &ev); err == nil {
			evidenceMode = ev.Mode
			for _, c := range ev.Commits {
				evidenceCommits = append(evidenceCommits, c.Subject)
			}
		}
	}

	input := pipeline.CommitSuggestionInput{
		OriginalHandoff:          originalHandoff,
		AuditHandoff:             auditHandoffPreview,
		GitDiffStat:              gitDiffStat,
		GitDiffNameStatus:        gitDiffNameStatus,
		GitChangeEvidenceJSON:    readArtifactPreview(runID, "git_change_evidence_json"),
		AgentResultStatus:        agentResultStatus,
		AgentBuildStatus:         agentBuildStatus,
		AgentTestStatus:          agentTestStatus,
		AgentLOCChanged:          agentLOCChanged,
		RepoPath:                 repo.Path,
		ValidationStatus:         validationStatus,
		ValidationFailedAccepted: validationAcceptedWithFailure,
		DiffInspected:            hasGitDiffEvidence,
		AuditHandoffPresent:      hasAuditHandoff,
		ChangedFileCount:         changedFileCount,
		EvidenceMode:             evidenceMode,
		EvidenceCommits:          evidenceCommits,
	}

	suggestion := pipeline.BuildCommitSuggestion(input)

	// Delete stale commit suggestion artifacts
	h.store.DeleteArtifactsByRunKind(runID, "commit_message_text")
	h.store.DeleteArtifactsByRunKind(runID, "commit_suggestion_json")

	// Write commit message text artifact
	msgPath, err := artifacts.Write(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text"), []byte(suggestion.Selected))
	if err != nil {
		h.log.Error("write commit message artifact", "error", err)
		http.Error(w, "failed to save commit message", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "commit_message_text", msgPath, "text/plain")

	// Write commit suggestion JSON artifact
	suggestionJSON, _ := json.MarshalIndent(suggestion, "", "  ")
	jsonPath, err := artifacts.Write(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json"), suggestionJSON)
	if err != nil {
		h.log.Error("write commit suggestion json", "error", err)
		http.Error(w, "failed to save commit suggestion", http.StatusInternalServerError)
		return
	}
	h.store.CreateArtifact(runID, "commit_suggestion_json", jsonPath, "application/json")

	h.store.CreateEvent(runID, "info", "Git commit suggestion prepared")

	h.log.Info("git commit suggestion prepared", "run_id", runID, "message", suggestion.Selected)

	setHXPushURL(w, runID, "commit")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=commit", http.StatusSeeOther)
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
	setHXPushURL(w, runID, "validation")
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(runID, 10)+"?step=validation", http.StatusSeeOther)
}

func setHXPushURL(w http.ResponseWriter, runID int64, step string) {
	w.Header().Set("HX-Push-Url", "/runs/"+strconv.FormatInt(runID, 10)+"?step="+step)
}

func (h *RunsHandler) captureAgentStartBaseline(runID int64, repoPath string) {
	snap := repos.CaptureGitSnapshot(repoPath, "agent_start")
	if !snap.IsGitRepo || snap.HeadSHA == "" {
		h.store.CreateEvent(runID, "warn", "Git baseline unavailable at agent start: "+snap.Error)
		return
	}

	// Update DB fields with authoritative baseline
	if _, err := h.store.UpdateRunBranch(runID, snap.Branch, snap.HeadSHA, snap.HeadSHA); err != nil {
		h.log.Warn("update run branch with agent-start baseline", "run_id", runID, "error", err)
	}

	// Read existing baseline artifact and merge or create new one
	existingJSON, err := artifacts.Read(runID, "git_baseline_json", pipeline.ArtifactFilename("git_baseline_json"))
	baseline := repos.GitBaselineArtifact{
		AuthoritativeBaselineStage: "agent_start",
		AuthoritativeBaselineSHA:   snap.HeadSHA,
	}
	if err == nil {
		json.Unmarshal(existingJSON, &baseline)
	}
	baseline.AgentStart = snap
	baseline.AuthoritativeBaselineStage = "agent_start"
	baseline.AuthoritativeBaselineSHA = snap.HeadSHA

	baselineJSON, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		h.log.Warn("marshal git baseline artifact", "run_id", runID, "error", err)
		return
	}

	// Delete stale artifact and write new one
	h.store.DeleteArtifactsByRunKind(runID, "git_baseline_json")
	if bp, err := artifacts.Write(runID, "git_baseline_json", pipeline.ArtifactFilename("git_baseline_json"), baselineJSON); err == nil {
		h.store.CreateArtifact(runID, "git_baseline_json", bp, "application/json")
	} else {
		h.log.Warn("write git baseline artifact", "run_id", runID, "error", err)
	}

	h.store.CreateEvent(runID, "info", "Git baseline refreshed at agent start: "+shortSHA(snap.HeadSHA)+" on "+snap.Branch)
}
