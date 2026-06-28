package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"relay/internal/app/plans"
	"relay/internal/artifacts"
	"relay/internal/events"
	"relay/internal/pipeline"
	"relay/internal/store"
)

const (
	StatusExecutorDispatched = "executor_dispatched"
	StatusExecutorDone       = "executor_done"
	StatusExecutorBlocked    = "executor_blocked"

	DefaultExecutorTimeout = 30 * time.Minute

	ArtifactKindExecutorStdout        = "executor_stdout"
	ArtifactKindExecutorStderr        = "executor_stderr"
	ArtifactKindCommandLog            = "command_log"
	ArtifactKindExecutorResult        = "executor_result"
	ArtifactKindCodexLastMessage      = "codex_last_message"
	ArtifactKindExecutorUsage         = "executor_usage_json"
	ArtifactKindKiroParseFixtureJSON = "kiro_parse_fixture_json" // temporary opt-in diagnostic fixture
)

var knownSecrets = []string{
	"RELAY_OPENCODE_BIN",
	"RELAY_OPENCODE_AGENT",
	"RELAY_OPENCODE_VARIANT",
	"RELAY_CODEX_BIN",
	"RELAY_CODEX_MODEL",
	"RELAY_CODEX_PROFILE",
	"OPENAI_API_KEY",
	"RELAY_ANTIGRAVITY_BIN",
	"RELAY_ANTIGRAVITY_MODEL",
	"RELAY_ANTIGRAVITY_APPROVE_FLAG",
	"ANTIGRAVITY_API_KEY",
	"RELAY_KIRO_BIN",
	"RELAY_KIRO_MODEL",
	"RELAY_KIRO_EFFORT",
	"RELAY_KIRO_TRUST_TOOLS",
	"RELAY_KIRO_AGENT",
	"RELAY_KIRO_AGENT_ENGINE",
	"KIRO_API_KEY",
}

type DispatchParams struct {
	Store       *store.Store
	Log         *slog.Logger
	EventHub    *events.Hub
	RunID       int64
	Adapter     ExecutorAdapter
	Preflight   func(ExecutorInvocation) ExecutorPreflightResult
	RunAgentCmd func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult
	LaunchAsync func(func())
}

type DispatchResult struct {
	Dispatched bool
	ExecID     int64
	EventMsg   string

	Cancel context.CancelFunc
}

func (p *DispatchParams) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func (p *DispatchParams) preflight() func(ExecutorInvocation) ExecutorPreflightResult {
	if p.Preflight != nil {
		return p.Preflight
	}
	return defaultExecutorPreflight
}

func (p *DispatchParams) runner() func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
	if p.RunAgentCmd != nil {
		return p.RunAgentCmd
	}
	return pipeline.RunLocalAgentCommandArgsStreaming
}

func (p *DispatchParams) launcher() func(func()) {
	if p.LaunchAsync != nil {
		return p.LaunchAsync
	}
	return func(fn func()) { go fn() }
}

func publishRunEvent(hub *events.Hub, runID int64, kind, source, status string) {
	if hub == nil {
		return
	}
	hub.Publish(events.RunEvent{
		RunID:  runID,
		Kind:   kind,
		Source: source,
		Status: status,
	})
}

func createEvent(store *store.Store, runID int64, level, message string) {
	if store == nil {
		return
	}
	store.CreateEvent(runID, level, message)
}

func executionTimestampNow() string {
	return time.Now().Format(time.RFC3339Nano)
}

func redactSensitive(input string) string {
	result := input
	for _, key := range knownSecrets {
		val := strings.TrimSpace(os.Getenv(key))
		if val == "" {
			continue
		}
		result = strings.ReplaceAll(result, val, "[REDACTED]")
	}
	return result
}

func redactSensitiveBytes(input []byte) []byte {
	return []byte(redactSensitive(string(input)))
}

func parseDispatchResult(raw string) (pipeline.AgentResultStatus, string) {
	r := pipeline.ParseAgentResult(raw)
	return r.Status, r.Raw
}

func readExecutorBrief(store *store.Store, runID int64) ([]byte, error) {
	data, err := artifacts.Read(runID, "executor_brief", "executor_brief.md")
	if err != nil {
		return nil, fmt.Errorf("executor_brief.md not found: %w", err)
	}
	return data, nil
}

type executorOutputPaths struct {
	stdoutPath   string
	stderrPath   string
	combinedPath string
	resultPath   string
}

func writeExecutorArtifact(runID int64, kind string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	redacted := redactSensitiveBytes(data)
	return artifacts.Write(runID, kind, pipeline.ArtifactFilename(kind), redacted)
}

func recordExecutorArtifact(store *store.Store, runID int64, kind, path, mimeType string) {
	if store == nil || path == "" {
		return
	}
	store.CreateArtifact(runID, kind, path, mimeType)
}

func deleteExecutorArtifacts(store *store.Store, runID int64) {
	for _, kind := range []string{
		ArtifactKindExecutorStdout,
		ArtifactKindExecutorStderr,
		ArtifactKindCommandLog,
		ArtifactKindExecutorResult,
		ArtifactKindCodexLastMessage,
		ArtifactKindExecutorUsage,
		ArtifactKindKiroParseFixtureJSON,
	} {
		store.DeleteArtifactsByRunKind(runID, kind)
		artifacts.Delete(runID, kind, pipeline.ArtifactFilename(kind))
	}
}

func updateRunStatus(st *store.Store, runID int64, status string) {
	if st == nil {
		return
	}
	updatedRun, err := st.UpdateRunStatus(runID, status)
	if err != nil {
		return
	}
	if err := plans.NewRunLifecycleService(st).SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		_, _ = st.CreateEvent(runID, "warn", "Associated pass status sync failed: "+err.Error())
	}
}

func defaultExecutorPreflight(inv ExecutorInvocation) ExecutorPreflightResult {
	res := ExecutorPreflightResult{
		OK:             true,
		Adapter:        inv.Adapter,
		Binary:         inv.Binary,
		WorkDir:        inv.WorkDir,
		CommandPreview: inv.Preview,
		Checks:         []ExecutorPreflightCheck{},
	}

	addCheck := func(name string, ok bool, detail string) {
		res.Checks = append(res.Checks, ExecutorPreflightCheck{Name: name, OK: ok, Detail: detail})
		if !ok && res.OK {
			res.OK = false
			res.BlockerText = detail
		}
	}

	if inv.Binary == "" {
		addCheck("binary_configured", false, "executor binary is not configured")
	} else {
		if filepath.IsAbs(inv.Binary) || strings.ContainsAny(inv.Binary, `/\`) {
			info, err := os.Stat(inv.Binary)
			if err != nil {
				addCheck("binary_available", false, fmt.Sprintf("executor binary not found at %s", inv.Binary))
			} else if info.IsDir() {
				addCheck("binary_available", false, fmt.Sprintf("executor binary is a directory at %s", inv.Binary))
			} else {
				addCheck("binary_available", true, "binary found")
			}
		} else {
			_, err := exec.LookPath(inv.Binary)
			if err != nil {
				addCheck("binary_available", false, fmt.Sprintf("executor binary %s not found in PATH", inv.Binary))
			} else {
				addCheck("binary_available", true, "binary found in PATH")
			}
		}
	}

	if inv.WorkDir == "" {
		addCheck("workdir_configured", false, "workdir is not configured")
	} else {
		info, err := os.Stat(inv.WorkDir)
		if err != nil {
			addCheck("workdir_available", false, fmt.Sprintf("workdir not found: %s", inv.WorkDir))
		} else if !info.IsDir() {
			addCheck("workdir_available", false, fmt.Sprintf("workdir is not a directory: %s", inv.WorkDir))
		} else {
			addCheck("workdir_available", true, "workdir exists")
		}
	}

	if inv.StdinSource == "" || inv.StdinSource == "/dev/null" {
		addCheck("stdin_source", true, "no stdin source required")
	} else {
		info, err := os.Stat(inv.StdinSource)
		if err != nil {
			addCheck("stdin_source", false, fmt.Sprintf("stdin source not found: %s", inv.StdinSource))
		} else if info.IsDir() {
			addCheck("stdin_source", false, fmt.Sprintf("stdin source is a directory: %s", inv.StdinSource))
		} else {
			addCheck("stdin_source", true, "stdin source found")
		}
	}

	if inv.Preview == "" {
		addCheck("command_preview", false, "command preview is empty")
	} else {
		addCheck("command_preview", true, "command preview present")
	}

	return res
}

func blockExecutorPreflight(p *DispatchParams, s *store.Store, runID int64, inv ExecutorInvocation, res ExecutorPreflightResult) {
	deleteExecutorArtifacts(s, runID)

	jsonBytes, _ := json.MarshalIndent(res, "", "  ")

	logText := fmt.Sprintf("Preflight: BLOCKED\nCommand: %s\nWorkDir: %s\nModel: %s\nAgent: %s\n\n--- PREFLIGHT DETAILS ---\n%s\n", inv.Preview, inv.WorkDir, inv.Model, inv.Agent, string(jsonBytes))
	logPath, _ := writeExecutorArtifact(runID, ArtifactKindCommandLog, []byte(logText))
	if logPath != "" {
		recordExecutorArtifact(s, runID, ArtifactKindCommandLog, logPath, "text/plain")
	}

	resText := fmt.Sprintf("STATUS: BLOCKED\n\nBlocker/error only if blocked: executor preflight failed: %s\n", res.BlockerText)
	resPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte(resText))
	if resPath != "" {
		recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resPath, "text/plain")
	}

	createEvent(s, runID, "warn", "Executor preflight blocked: "+res.BlockerText)
	publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")

	updateRunStatus(s, runID, StatusExecutorBlocked)
	publishRunEvent(p.EventHub, runID, events.KindRunSummary, "executor", "blocked")
}

type streamingState struct {
	mu                sync.Mutex
	streamedStdout    strings.Builder
	streamedStderr    strings.Builder
	stdoutChunkCount  int64
	stderrChunkCount  int64
	stdoutByteCount   int64
	stderrByteCount   int64
	lastStdoutChunkAt string
	lastStderrChunkAt string
	lastAnyChunkAt    string
	writeErrors       map[string]string
	progressParser    *progressParser
	lastEventMsg      string
	lastEventKind     string
	lastTextAt        time.Time
	lastTextMsg       string
}

func (s *streamingState) recordWriteError(key string, err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErrors == nil {
		s.writeErrors = make(map[string]string)
	}
	if _, exists := s.writeErrors[key]; !exists {
		s.writeErrors[key] = err.Error()
	}
}

func (s *streamingState) writeErrorSummary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.writeErrors) == 0 {
		return ""
	}
	errs := make([]string, 0, len(s.writeErrors))
	for k, v := range s.writeErrors {
		errs = append(errs, k+": "+v)
	}
	return strings.Join(errs, "; ")
}

func (s *streamingState) emitProgressEvent(st *store.Store, runID int64, ev ExecutorProgressEvent) {
	if st == nil || ev.Message == "" {
		return
	}
	s.mu.Lock()
	shouldEmit := s.checkAndRecordEvent(ev)
	s.mu.Unlock()

	if shouldEmit {
		createEvent(st, runID, ev.Level, ev.Message)
	}
}

func (s *streamingState) checkAndRecordEvent(ev ExecutorProgressEvent) bool {
	rateKind := ev.Kind == "assistant_text" || ev.Kind == "text" || ev.Kind == "executor_text"

	if ev.Message == s.lastEventMsg && ev.Kind == s.lastEventKind {
		return false
	}

	if rateKind {
		now := time.Now()
		if !s.lastTextAt.IsZero() && now.Sub(s.lastTextAt) < time.Second {
			if ev.Message == s.lastTextMsg {
				return false
			}
		}
		s.lastTextAt = now
		s.lastTextMsg = ev.Message
	}

	s.lastEventMsg = ev.Message
	s.lastEventKind = ev.Kind
	return true
}

func runBackgroundDispatch(
	ctx context.Context,
	p *DispatchParams,
	runID int64,
	execID int64,
	invocation ExecutorInvocation,
	adapter ExecutorAdapter,
	repo *store.Repo,
) {
	l := p.log()
	s := p.Store
	hub := p.EventHub
	runner := p.runner()

	startedAt := executionTimestampNow()
	s.UpdateAgentExecutionStatus(execID, "running", nil, &startedAt, nil, nil, nil, nil, nil, nil)
	publishRunEvent(hub, runID, events.KindStepAgent, "executor", "running")
	createEvent(s, runID, "info", "Executor dispatched: "+invocation.Preview)

	deleteExecutorArtifacts(s, runID)

	stream := &streamingState{}
	stream.progressParser = newProgressParser()
	if err := artifacts.EnsureDir(runID); err != nil {
		l.Error("executor: ensure artifact dir", "error", err)
	}

	combinedLogText := func(stdout, stderr string) string {
		var b strings.Builder
		b.WriteString("Command: ")
		b.WriteString(invocation.Preview)
		b.WriteString("\nWorkDir: ")
		b.WriteString(invocation.WorkDir)
		b.WriteString("\nModel: ")
		b.WriteString(invocation.Model)
		b.WriteString("\nAgent: ")
		b.WriteString(invocation.Agent)
		b.WriteString("\nStartedAt: ")
		b.WriteString(startedAt)
		b.WriteString("\n\n--- STDOUT ---\n")
		b.WriteString(stdout)
		if stderr != "" {
			b.WriteString("\n--- STDERR ---\n")
			b.WriteString(stderr)
		}
		return b.String()
	}

	runResult := runner(
		ctx,
		invocation.WorkDir,
		invocation.Binary,
		invocation.Args,
		invocation.Stdin,
		DefaultExecutorTimeout,
		pipeline.AgentCommandStreamCallbacks{
			OnStdout: func(chunk []byte) {
				if len(chunk) == 0 {
					return
				}
				stream.mu.Lock()
				stream.streamedStdout.Write(chunk)
				stream.stdoutChunkCount++
				stream.stdoutByteCount += int64(len(chunk))
				nowText := executionTimestampNow()
				stream.lastStdoutChunkAt = nowText
				stream.lastAnyChunkAt = nowText
				stream.mu.Unlock()

				appendPath, err := writeExecutorArtifact(runID, ArtifactKindExecutorStdout, chunk)
				if err != nil {
					stream.recordWriteError("append_stdout", err)
				} else if appendPath != "" {
					recordExecutorArtifact(s, runID, ArtifactKindExecutorStdout, appendPath, "text/plain")
				}

				parseEvents := stream.progressParser.feed(chunk)
				for _, ev := range parseEvents {
					stream.emitProgressEvent(s, runID, ev)
				}
			},
			OnStderr: func(chunk []byte) {
				if len(chunk) == 0 {
					return
				}
				stream.mu.Lock()
				stream.streamedStderr.Write(chunk)
				stream.stderrChunkCount++
				stream.stderrByteCount += int64(len(chunk))
				nowText := executionTimestampNow()
				stream.lastStderrChunkAt = nowText
				stream.lastAnyChunkAt = nowText
				stream.mu.Unlock()

				appendPath, err := writeExecutorArtifact(runID, ArtifactKindExecutorStderr, chunk)
				if err != nil {
					stream.recordWriteError("append_stderr", err)
				} else if appendPath != "" {
					recordExecutorArtifact(s, runID, ArtifactKindExecutorStderr, appendPath, "text/plain")
				}

				parseEvents := stream.progressParser.feed(chunk)
				for _, ev := range parseEvents {
					stream.emitProgressEvent(s, runID, ev)
				}
			},
		},
	)

	flushEvents := stream.progressParser.flush()
	for _, ev := range flushEvents {
		stream.emitProgressEvent(s, runID, ev)
	}

	stream.mu.Lock()
	finalStdout := stream.streamedStdout.String()
	finalStderr := stream.streamedStderr.String()
	stream.mu.Unlock()

	stdoutPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorStdout, []byte(finalStdout))
	if stdoutPath != "" {
		recordExecutorArtifact(s, runID, ArtifactKindExecutorStdout, stdoutPath, "text/plain")
	}
	stderrPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorStderr, []byte(finalStderr))
	if stderrPath != "" {
		recordExecutorArtifact(s, runID, ArtifactKindExecutorStderr, stderrPath, "text/plain")
	}

	combinedLog := combinedLogText(finalStdout, finalStderr)
	combinedPath, _ := writeExecutorArtifact(runID, ArtifactKindCommandLog, []byte(combinedLog))
	if combinedPath != "" {
		recordExecutorArtifact(s, runID, ArtifactKindCommandLog, combinedPath, "text/plain")
	}

	if ctx.Err() != nil {
		l.Info("executor: context canceled before finalization", "run_id", runID, "exec_id", execID)
		createEvent(s, runID, "warn", "Executor execution canceled")
		publishRunEvent(hub, runID, events.KindStepAgent, "executor", "canceled")
		return
	}

	if runResult.TimedOut {
		ec := int64(-2)
		finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)
		errMsg := "executor timed out"
		s.UpdateAgentExecutionStatus(execID, "failed", &ec, &startedAt, &finishedStr, &stdoutPath, &stderrPath, &combinedPath, nil, &errMsg)
		createEvent(s, runID, "warn", "Executor timed out")
		publishRunEvent(hub, runID, events.KindStepAgent, "executor", "timed_out")
		updateRunStatus(s, runID, StatusExecutorBlocked)
		publishRunEvent(hub, runID, events.KindRunSummary, "executor", "blocked")
		return
	}

	finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)
	ec := int64(runResult.ExitCode)
	execStatus := "completed"
	if runResult.ExitCode != 0 {
		execStatus = "failed"
	}

	errMsg := runResult.Error
	writeErrSummary := stream.writeErrorSummary()
	if writeErrSummary != "" {
		if errMsg != "" {
			errMsg += "; " + writeErrSummary
		} else {
			errMsg = writeErrSummary
		}
	}
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}

	s.UpdateAgentExecutionStatus(execID, execStatus, &ec, &startedAt, &finishedStr, &stdoutPath, &stderrPath, &combinedPath, nil, errPtr)

	if invocation.RequireZeroExit && runResult.ExitCode != 0 {
		blocker := strings.TrimSpace(runResult.Error)
		if blocker == "" {
			blocker = fmt.Sprintf("executor exited with code %d", runResult.ExitCode)
		}
		resultText := fmt.Sprintf("STATUS: BLOCKED\n\nBlocker/error only if blocked: %s\n", blocker)
		resultPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte(resultText))
		if resultPath != "" {
			recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resultPath, "text/plain")
		}
		createEvent(s, runID, "warn", "Executor blocked: "+blocker)
		publishRunEvent(hub, runID, events.KindStepAgent, "executor", "blocked")
		updateRunStatus(s, runID, StatusExecutorBlocked)
		publishRunEvent(hub, runID, events.KindRunSummary, "executor", "blocked")
		return
	}

	collectAndPersistGitEvidence(s, runID, repo.Path)

	// Extract and persist Kiro usage telemetry (Kiro-only)
	if invocation.Adapter == AdapterKiroCLI {
		if usageTel, ok := extractKiroUsageTelemetry(finalStdout, finalStderr, invocation.Model); ok {
			usageBytes, err := json.MarshalIndent(usageTel, "", "  ")
			if err == nil {
				usagePath, wErr := writeExecutorArtifact(runID, ArtifactKindExecutorUsage, usageBytes)
				if wErr == nil && usagePath != "" {
					recordExecutorArtifact(s, runID, ArtifactKindExecutorUsage, usagePath, "application/json")
					l.Info("executor: captured usage telemetry", "run_id", runID, "credits", usageTel.CreditsText)
				} else if wErr != nil {
					l.Warn("executor: failed to write usage telemetry artifact", "error", wErr)
				}
			} else {
				l.Warn("executor: failed to marshal usage telemetry", "error", err)
			}
		}
	}

	// Emit Kiro parse fixture (opt-in, Kiro-only) before status normalization
	if invocation.Adapter == AdapterKiroCLI && isKiroParseFixtureEnabled() {
		captureKiroParseFixture(l, s, runID, execID, invocation, runResult, finalStdout, finalStderr)
	}

	if runResult.Stdout != "" || invocation.ResultFile != "" {
		normalizationInput := runResult.Stdout

		if invocation.ResultFile != "" {
			if content, err := os.ReadFile(invocation.ResultFile); err == nil {
				trimmed := strings.TrimSpace(string(content))
				if trimmed != "" {
					appendPath, wErr := writeExecutorArtifact(runID, ArtifactKindCodexLastMessage, content)
					if wErr == nil && appendPath != "" {
						recordExecutorArtifact(s, runID, ArtifactKindCodexLastMessage, appendPath, "text/plain")
					}
					normalizationInput = string(content)
				}
			}
		}

		if normalizationInput != "" {
			res := adapter.NormalizeResult(normalizationInput)

			if res.ExecutorResultText != "" {
				resultPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte(res.ExecutorResultText))
				if resultPath != "" {
					recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resultPath, "text/plain")
				}
			}

			switch res.Status {
			case pipeline.AgentResultDone:
				createEvent(s, runID, "info", "Executor completed: DONE")
				publishRunEvent(hub, runID, events.KindStepAgent, "executor", "done")
				updateRunStatus(s, runID, StatusExecutorDone)
				publishRunEvent(hub, runID, events.KindRunSummary, "executor", "done")
			case pipeline.AgentResultBlocked:
				createEvent(s, runID, "warn", "Executor blocked: "+res.BlockerText)
				publishRunEvent(hub, runID, events.KindStepAgent, "executor", "blocked")
				updateRunStatus(s, runID, StatusExecutorBlocked)
				publishRunEvent(hub, runID, events.KindRunSummary, "executor", "blocked")
			default:
				createEvent(s, runID, "warn", res.ParseError)
				publishRunEvent(hub, runID, events.KindStepAgent, "executor", "parse_failed")
				updateRunStatus(s, runID, StatusExecutorBlocked)
				publishRunEvent(hub, runID, events.KindRunSummary, "executor", "blocked")
			}
			return
		}
	}

	resultPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte("STATUS: UNKNOWN\nNo stdout captured from executor.\n"))
	if resultPath != "" {
		recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resultPath, "text/plain")
	}
	createEvent(s, runID, "warn", "Executor completed with no stdout")
	publishRunEvent(hub, runID, events.KindStepAgent, "executor", "no_output")
	updateRunStatus(s, runID, StatusExecutorBlocked)
	publishRunEvent(hub, runID, events.KindRunSummary, "executor", "blocked")
}

func getRepoWorkspacePath(s *store.Store, runID int64) (*store.Repo, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("run not found: %w", err)
	}
	repo, err := s.GetRepo(run.RepoID)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	if repo.Path == "" {
		return nil, fmt.Errorf("repo path is empty")
	}
	info, err := os.Stat(repo.Path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("repo path does not exist or is not a directory: %s", repo.Path)
	}
	return repo, nil
}

func getRunModel(s *store.Store, runID int64) (string, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return "", fmt.Errorf("run not found: %w", err)
	}
	if run.SelectedModel == "" {
		return "", fmt.Errorf("selected model is empty")
	}
	return run.SelectedModel, nil
}

func DispatchBrief(p *DispatchParams) (DispatchResult, error) {
	l := p.log()
	s := p.Store
	runID := p.RunID

	run, err := s.GetRun(runID)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("run not found: %w", err)
	}
	if run.Status != "approved_for_executor" {
		return DispatchResult{}, fmt.Errorf("run status is %q, must be approved_for_executor to dispatch", run.Status)
	}

	repo, err := getRepoWorkspacePath(s, runID)
	if err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("workspace prerequisite failed: %w", err)
	}

	selectedModel, err := getRunModel(s, runID)
	if err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("model prerequisite failed: %w", err)
	}

	briefData, err := readExecutorBrief(s, runID)
	if err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("executor brief prerequisite failed: %w", err)
	}

	adapter := p.Adapter
	if adapter == nil {
		var err error
		adapter, err = NewAdapterFromID(run.ExecutorAdapter)
		if err != nil {
			createEvent(s, runID, "warn", "Executor dispatch blocked: "+err.Error())
			publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
			updateRunStatus(s, runID, StatusExecutorBlocked)
			publishRunEvent(p.EventHub, runID, events.KindRunSummary, "executor", "blocked")
			return DispatchResult{}, fmt.Errorf("adapter error: %w", err)
		}
	}

	briefPath := filepath.Join(artifacts.Dir(runID), "executor_brief.md")
	req := ExecutorAdapterRequest{
		RunID:         runID,
		RepoPath:      repo.Path,
		BriefContent:  string(briefData),
		BriefPath:     briefPath,
		SelectedModel: selectedModel,
		Timeout:       DefaultExecutorTimeout,
	}

	invocation, err := adapter.BuildInvocation(req)
	if err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: invocation build failed: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("invocation build failed: %w", err)
	}

	existingExec, err := s.GetLatestAgentExecutionByRun(runID)
	if err == nil && existingExec != nil && (existingExec.Status == "starting" || existingExec.Status == "running") {
		createEvent(s, runID, "warn", "Executor dispatch blocked: an execution is already running")
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("an execution is already running for this run")
	}

	preflightRes := p.preflight()(invocation)
	if !preflightRes.OK {
		blockExecutorPreflight(p, s, runID, invocation, preflightRes)
		return DispatchResult{Dispatched: false}, fmt.Errorf("executor preflight failed: %s", preflightRes.BlockerText)
	}

	exec, err := s.CreateAgentExecution(runID, string(adapter.ID()), "starting", invocation.Preview)
	if err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: failed to create execution record: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("create execution record: %w", err)
	}

	l.Info("executor: dispatching from executor_brief.md", "run_id", runID, "exec_id", exec.ID, "model", invocation.Model)

	commandCtx, cancel := context.WithCancel(context.Background())

	createEvent(s, runID, "info", "Executor dispatched from executor_brief.md")
	publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "starting")

	updateRunStatus(s, runID, StatusExecutorDispatched)
	publishRunEvent(p.EventHub, runID, events.KindRunSummary, "executor", "dispatched")

	p.launcher()(func() {
		runBackgroundDispatch(commandCtx, p, runID, exec.ID, invocation, adapter, repo)
	})

	return DispatchResult{
		Dispatched: true,
		ExecID:     exec.ID,
		EventMsg:   "Executor dispatched from executor_brief.md",
		Cancel:     cancel,
	}, nil
}

func WriteArtifactFromReader(runID int64, kind string, r io.Reader) (string, error) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return "", err
	}
	return writeExecutorArtifact(runID, kind, buf.Bytes())
}

func ParseStrictStatus(raw string) (pipeline.AgentResultStatus, string) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "STATUS:") {
			val := strings.TrimSpace(line[7:])
			upper := strings.ToUpper(val)
			if upper == "DONE" {
				return pipeline.AgentResultDone, "STATUS: DONE"
			}
			if upper == "BLOCKED" {
				return pipeline.AgentResultBlocked, "STATUS: BLOCKED" + extractBlocker(raw)
			}
			return pipeline.AgentResultUnknown, fmt.Sprintf("STATUS: %s (unrecognized)", val)
		}
	}
	return pipeline.AgentResultUnknown, "STATUS: missing"
}

func extractBlocker(raw string) string {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "BLOCKER:") {
			val := strings.TrimSpace(line[8:])
			if val != "" {
				return "\n" + val
			}
		}
		if strings.HasPrefix(line, "BLOCKER/ERROR:") {
			val := strings.TrimSpace(line[14:])
			if val != "" {
				return "\n" + val
			}
		}
	}
	return ""
}

func collectAndPersistGitEvidence(s *store.Store, runID int64, repoPath string) {
	if repoPath == "" {
		return
	}
	ev, err := pipeline.CollectGitDiffEvidence(context.Background(), repoPath, 30*time.Second)
	if err != nil {
		s.CreateEvent(runID, "warn", fmt.Sprintf("Failed to collect git evidence: %v", err))
		return
	}

	persistGitArtifact(s, runID, "git_status_text", ev.StatusText)
	persistGitArtifact(s, runID, "git_diff_stat", ev.DiffStat)
	persistGitArtifact(s, runID, "git_diff_numstat", ev.DiffNumstat)
	persistGitArtifact(s, runID, "git_diff_name_status", ev.NameStatus)
	persistGitArtifact(s, runID, "git_diff_patch", ev.DiffPatch)
}

func persistGitArtifact(s *store.Store, runID int64, kind, content string) {
	if content == "" {
		return
	}
	filename := pipeline.ArtifactFilename(kind)
	path, err := artifacts.Write(runID, kind, filename, []byte(content))
	if err == nil && path != "" {
		s.CreateArtifact(runID, kind, path, "text/plain")
	}
}
func persistGitArtifact(s *store.Store, runID int64, kind, content string) {
	if content == "" {
		return
	}
	filename := pipeline.ArtifactFilename(kind)
	path, err := artifacts.Write(runID, kind, filename, []byte(content))
	if err == nil && path != "" {
		s.CreateArtifact(runID, kind, path, "text/plain")
	}
}

// isKiroParseFixtureEnabled returns true iff RELAY_KIRO_CAPTURE_PARSE_FIXTURE=true (case-insensitive).
func isKiroParseFixtureEnabled() bool {
	v := strings.TrimSpace(os.Getenv("RELAY_KIRO_CAPTURE_PARSE_FIXTURE"))
	return strings.EqualFold(v, "true")
}

// kiroParseFixtureJSON is the diagnostic fixture schema.
type kiroParseFixtureJSON struct {
	SchemaVersion                string                              `json:"schemaVersion"`
	Temporary                    bool                                `json:"temporary"`
	CapturedAt                   string                              `json:"capturedAt"`
	RunID                        int64                               `json:"runId"`
	ExecID                       int64                               `json:"execId"`
	Adapter                      string                              `json:"adapter"`
	Model                        string                              `json:"model"`
	Agent                        string                              `json:"agent"`
	CommandPreview               string                              `json:"commandPreview"`
	ExitCode                     int                                 `json:"exitCode"`
	TimedOut                     bool                                `json:"timedOut"`
	ErrorText                    string                              `json:"errorText,omitempty"`
	RunResultStdoutBytes         int                                 `json:"runResultStdoutBytes"`
	RunResultStderrBytes         int                                 `json:"runResultStderrBytes"`
	FinalStdoutBytes             int                                 `json:"finalStdoutBytes"`
	FinalStderrBytes             int                                 `json:"finalStderrBytes"`
	RunResultStdoutQ             string                              `json:"runResultStdoutQ,omitempty"`
	RunResultStderrQ             string                              `json:"runResultStderrQ,omitempty"`
	FinalStdoutQ                 string                              `json:"finalStdoutQ,omitempty"`
	FinalStderrQ                 string                              `json:"finalStderrQ,omitempty"`
	CombinedFinalStreamsQ        string                              `json:"combinedFinalStreamsQ,omitempty"`
	SelectedNormalizationInput   string                              `json:"selectedNormalizationInput"`
	SelectedNormalizationInputQ  string                              `json:"selectedNormalizationInputQ,omitempty"`
	SelectedParsedStatus         string                              `json:"selectedParsedStatus"`
	CandidateResults             []kiroParseFixtureCandidate         `json:"candidateResults"`
}

type kiroParseFixtureCandidate struct {
	Name               string `json:"name"`
	ByteCount          int    `json:"byteCount"`
	HasStatusToken     bool   `json:"hasStatusToken"`
	NormalizedQ        string `json:"normalizedQ,omitempty"`
	ParsedStatus       string `json:"parsedStatus"`
	BuildStatus        string `json:"buildStatus"`
	TestStatus         string `json:"testStatus"`
	LOCChanged         string `json:"locChanged"`
	BlockerError       string `json:"blockerError,omitempty"`
}

const kiroFixtureMaxLen = 8192

func boundAndQuote(s string) string {
	if len(s) > kiroFixtureMaxLen {
		s = s[:kiroFixtureMaxLen]
	}
	return fmt.Sprintf("%q", s)
}

func hasStatusToken(s string) bool {
	return strings.Contains(s, "STATUS:") ||
		strings.Contains(s, "DONE") ||
		strings.Contains(s, "BLOCKED")
}

func captureKiroParseFixture(
	l *slog.Logger,
	s *store.Store,
	runID int64,
	execID int64,
	invocation ExecutorInvocation,
	runResult pipeline.AgentCommandRunResult,
	finalStdout string,
	finalStderr string,
) {
	now := time.Now().Format(time.RFC3339Nano)

	// Determine selected normalization input (matches runBackgroundDispatch logic)
	selectedInput := runResult.Stdout
	if invocation.ResultFile != "" {
		if content, err := os.ReadFile(invocation.ResultFile); err == nil {
			trimmed := strings.TrimSpace(string(content))
			if trimmed != "" {
				selectedInput = string(content)
			}
		}
	}

	// Run normalization on selected input
	selectedNormalized := normalizeKiroHeadlessOutput(selectedInput)
	selectedParsed := pipeline.ParseAgentResult(selectedNormalized)

	fixture := kiroParseFixtureJSON{
		SchemaVersion:              "1",
		Temporary:                  true,
		CapturedAt:                 now,
		RunID:                      runID,
		ExecID:                     execID,
		Adapter:                    string(invocation.Adapter),
		Model:                      invocation.Model,
		Agent:                      invocation.Agent,
		CommandPreview:             invocation.Preview,
		ExitCode:                   runResult.ExitCode,
		TimedOut:                   runResult.TimedOut,
		ErrorText:                  runResult.Error,
		RunResultStdoutBytes:       len(runResult.Stdout),
		RunResultStderrBytes:       len(runResult.Stderr),
		FinalStdoutBytes:           len(finalStdout),
		FinalStderrBytes:           len(finalStderr),
		RunResultStdoutQ:           boundAndQuote(redactSensitive(runResult.Stdout)),
		RunResultStderrQ:           boundAndQuote(redactSensitive(runResult.Stderr)),
		FinalStdoutQ:               boundAndQuote(redactSensitive(finalStdout)),
		FinalStderrQ:               boundAndQuote(redactSensitive(finalStderr)),
		CombinedFinalStreamsQ:      boundAndQuote(redactSensitive(finalStdout + "\n" + finalStderr)),
		SelectedNormalizationInput: "run_result_stdout",
		SelectedNormalizationInputQ: boundAndQuote(redactSensitive(selectedInput)),
		SelectedParsedStatus:        string(selectedParsed.Status),
	}

	// Build candidate results
	candidates := []kiroParseFixtureCandidate{
		makeKiroCandidate("run_result_stdout", runResult.Stdout),
		makeKiroCandidate("run_result_stderr", runResult.Stderr),
		makeKiroCandidate("final_stdout", finalStdout),
		makeKiroCandidate("final_stderr", finalStderr),
		makeKiroCandidate("combined_final_streams", finalStdout+"\n"+finalStderr),
	}
	fixture.CandidateResults = candidates

	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		l.Warn("executor: failed to marshal kiro parse fixture", "error", err)
		return
	}

	path, err := writeExecutorArtifact(runID, ArtifactKindKiroParseFixtureJSON, data)
	if err != nil {
		l.Warn("executor: failed to write kiro parse fixture", "error", err)
		return
	}
	if path != "" {
		recordExecutorArtifact(s, runID, ArtifactKindKiroParseFixtureJSON, path, "application/json")
		l.Info("executor: captured kiro parse fixture", "run_id", runID)
	}
}

func makeKiroCandidate(name, raw string) kiroParseFixtureCandidate {
	redacted := redactSensitive(raw)
	normalized := normalizeKiroHeadlessOutput(redacted)
	parsed := pipeline.ParseAgentResult(normalized)

	return kiroParseFixtureCandidate{
		Name:           name,
		ByteCount:      len(raw),
		HasStatusToken: hasStatusToken(raw),
		NormalizedQ:    boundAndQuote(normalized),
		ParsedStatus:   string(parsed.Status),
		BuildStatus:    parsed.BuildStatus,
		TestStatus:     parsed.TestStatus,
		LOCChanged:     parsed.LOCChanged,
		BlockerError:   parsed.BlockerError,
	}
}
