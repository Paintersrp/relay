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

	ArtifactKindExecutorStdout   = "executor_stdout"
	ArtifactKindExecutorStderr   = "executor_stderr"
	ArtifactKindCommandLog       = "command_log"
	ArtifactKindExecutorResult   = "executor_result"
	ArtifactKindCodexLastMessage = "codex_last_message"
	ArtifactKindExecutorUsage    = "executor_usage_json"
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
	Store           *store.Store
	Log             *slog.Logger
	EventHub        *events.Hub
	RunID           int64
	OwnerInstanceID string
	ProcessControl  pipeline.ProcessController
	Adapter         ExecutorAdapter
	Preflight       func(ExecutorInvocation) ExecutorPreflightResult
	RunAgentCmd     func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult
	LaunchAsync     func(func())
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

func (p *DispatchParams) ownerInstanceID() string {
	if p.OwnerInstanceID != "" {
		return p.OwnerInstanceID
	}
	return NewOwnerInstanceID()
}

func (p *DispatchParams) processController() pipeline.ProcessController {
	if p.ProcessControl != nil {
		return p.ProcessControl
	}
	return pipeline.DefaultProcessController()
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
	ownershipToken string,
	invocation ExecutorInvocation,
	adapter ExecutorAdapter,
	repo *store.Repo,
) {
	l := p.log()
	s := p.Store
	hub := p.EventHub
	runner := p.runner()

	startedAt := executionTimestampNow()
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
			OnProcessStarted: func(identity pipeline.ProcessIdentity) {
				startedAt = executionTimestampNow()
				if _, won, err := s.RegisterAgentExecutionProcess(execID, store.AgentProcessIdentityUpdate{
					ProcessID:        int64(identity.PID),
					ProcessGroupID:   int64(identity.GroupID),
					ProcessIdentity:  identity.Encode(),
					ProcessStartedAt: identity.StartedAt,
					StartedAt:        startedAt,
					OwnershipToken:   ownershipToken,
				}); err != nil {
					stream.recordWriteError("register_process_identity", err)
				} else if won {
					publishRunEvent(hub, runID, events.KindStepAgent, "executor", "running")
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

	currentExec, _ := s.GetAgentExecution(execID)
	if currentExec != nil && currentExec.CancellationRequestedAt.Valid {
		collectAndPersistGitEvidence(s, runID, repo.Path)
		finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)
		ec := int64(runResult.ExitCode)
		if runResult.TimedOut {
			ec = -2
		}
		errMsg := strings.TrimSpace(runResult.Error)
		if errMsg == "" && ctx.Err() != nil {
			errMsg = "executor cancellation requested"
		}
		_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
			Status:                  ExecutionStatusCanceled,
			Reason:                  TerminalReasonCanceled,
			ExitCode:                &ec,
			StartedAt:               startedAt,
			FinishedAt:              finishedStr,
			StdoutPath:              stdoutPath,
			StderrPath:              stderrPath,
			CombinedPath:            combinedPath,
			Error:                   errMsg,
			CancellationCompletedAt: finishedStr,
			EventLevel:              "warn",
			EventMessage:            "Executor canceled by operator request",
			StepEventStatus:         "canceled",
			RunStatus:               StatusExecutorBlocked,
			RunEventStatus:          "blocked",
		})
		if err != nil {
			l.Error("executor: terminalize canceled execution", "run_id", runID, "exec_id", execID, "error", err)
		}
		return
	}

	if runResult.TimedOut {
		ec := int64(-2)
		finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)
		errMsg := "executor timed out"
		_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
			Status:          ExecutionStatusTimedOut,
			Reason:          TerminalReasonTimedOut,
			ExitCode:        &ec,
			StartedAt:       startedAt,
			FinishedAt:      finishedStr,
			StdoutPath:      stdoutPath,
			StderrPath:      stderrPath,
			CombinedPath:    combinedPath,
			Error:           errMsg,
			EventLevel:      "warn",
			EventMessage:    "Executor timed out",
			StepEventStatus: "timed_out",
			RunStatus:       StatusExecutorBlocked,
			RunEventStatus:  "blocked",
		})
		if err != nil {
			l.Error("executor: terminalize timed out execution", "run_id", runID, "exec_id", execID, "error", err)
		}
		return
	}

	finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)
	ec := int64(runResult.ExitCode)
	execStatus := ExecutionStatusSucceeded
	if runResult.ExitCode != 0 {
		execStatus = ExecutionStatusFailed
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
		_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
			Status:          execStatus,
			Reason:          TerminalReasonFailed,
			ExitCode:        &ec,
			StartedAt:       startedAt,
			FinishedAt:      finishedStr,
			StdoutPath:      stdoutPath,
			StderrPath:      stderrPath,
			CombinedPath:    combinedPath,
			ResultPath:      resultPath,
			Error:           nonEmpty(errPtr),
			EventLevel:      "warn",
			EventMessage:    "Executor blocked: " + blocker,
			StepEventStatus: "blocked",
			RunStatus:       StatusExecutorBlocked,
			RunEventStatus:  "blocked",
		})
		if err != nil {
			l.Error("executor: terminalize failed execution", "run_id", runID, "exec_id", execID, "error", err)
		}
		return
	}

	collectAndPersistGitEvidence(s, runID, repo.Path)

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

			resultPath := ""
			if res.ExecutorResultText != "" {
				resultPath, _ = writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte(res.ExecutorResultText))
				if resultPath != "" {
					recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resultPath, "text/plain")
				}
			}

			switch res.Status {
			case pipeline.AgentResultDone:
				_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
					Status:          execStatus,
					Reason:          TerminalReasonSucceeded,
					ExitCode:        &ec,
					StartedAt:       startedAt,
					FinishedAt:      finishedStr,
					StdoutPath:      stdoutPath,
					StderrPath:      stderrPath,
					CombinedPath:    combinedPath,
					ResultPath:      resultPath,
					Error:           nonEmpty(errPtr),
					EventMessage:    "Executor completed: DONE",
					StepEventStatus: "done",
					RunStatus:       StatusExecutorDone,
					RunEventStatus:  "done",
				})
				if err != nil {
					l.Error("executor: terminalize succeeded execution", "run_id", runID, "exec_id", execID, "error", err)
				}
			case pipeline.AgentResultBlocked:
				_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
					Status:          execStatus,
					Reason:          TerminalReasonFailed,
					ExitCode:        &ec,
					StartedAt:       startedAt,
					FinishedAt:      finishedStr,
					StdoutPath:      stdoutPath,
					StderrPath:      stderrPath,
					CombinedPath:    combinedPath,
					ResultPath:      resultPath,
					Error:           nonEmpty(errPtr),
					EventLevel:      "warn",
					EventMessage:    "Executor blocked: " + res.BlockerText,
					StepEventStatus: "blocked",
					RunStatus:       StatusExecutorBlocked,
					RunEventStatus:  "blocked",
				})
				if err != nil {
					l.Error("executor: terminalize blocked execution", "run_id", runID, "exec_id", execID, "error", err)
				}
			default:
				_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
					Status:          ExecutionStatusFailed,
					Reason:          TerminalReasonFailed,
					ExitCode:        &ec,
					StartedAt:       startedAt,
					FinishedAt:      finishedStr,
					StdoutPath:      stdoutPath,
					StderrPath:      stderrPath,
					CombinedPath:    combinedPath,
					ResultPath:      resultPath,
					Error:           res.ParseError,
					EventLevel:      "warn",
					EventMessage:    res.ParseError,
					StepEventStatus: "parse_failed",
					RunStatus:       StatusExecutorBlocked,
					RunEventStatus:  "blocked",
				})
				if err != nil {
					l.Error("executor: terminalize parse failed execution", "run_id", runID, "exec_id", execID, "error", err)
				}
			}
			return
		}
	}

	resultPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte("STATUS: UNKNOWN\nNo stdout captured from executor.\n"))
	if resultPath != "" {
		recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resultPath, "text/plain")
	}
	_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
		Status:          ExecutionStatusFailed,
		Reason:          TerminalReasonFailed,
		ExitCode:        &ec,
		StartedAt:       startedAt,
		FinishedAt:      finishedStr,
		StdoutPath:      stdoutPath,
		StderrPath:      stderrPath,
		CombinedPath:    combinedPath,
		ResultPath:      resultPath,
		Error:           "no stdout captured from executor",
		EventLevel:      "warn",
		EventMessage:    "Executor completed with no stdout",
		StepEventStatus: "no_output",
		RunStatus:       StatusExecutorBlocked,
		RunEventStatus:  "blocked",
	})
	if err != nil {
		l.Error("executor: terminalize no output execution", "run_id", runID, "exec_id", execID, "error", err)
	}
}

func nonEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
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

	existingExec, err := s.GetActiveAgentExecutionByRun(runID)
	if err == nil && existingExec != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: an execution is already running")
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("an execution is already running for this run")
	}

	preflightRes := p.preflight()(invocation)
	if !preflightRes.OK {
		blockExecutorPreflight(p, s, runID, invocation, preflightRes)
		return DispatchResult{Dispatched: false}, fmt.Errorf("executor preflight failed: %s", preflightRes.BlockerText)
	}

	ownerID := p.ownerInstanceID()
	ownershipToken := newOwnershipToken()
	exec, err := s.CreateOwnedAgentExecution(runID, string(adapter.ID()), ExecutionStatusStarting, invocation.Preview, "local_process", ownerID, ownershipToken)
	if err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: failed to create execution record: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("create execution record: %w", err)
	}

	l.Info("executor: dispatching from executor_brief.md", "run_id", runID, "exec_id", exec.ID, "model", invocation.Model)

	commandCtx, cancel := context.WithCancel(context.Background())
	globalRuntimeRegistry.put(runtimeHandle{
		execID:         exec.ID,
		runID:          runID,
		ownershipToken: ownershipToken,
		cancel:         cancel,
		controller:     p.processController(),
	})

	createEvent(s, runID, "info", "Executor dispatched from executor_brief.md")
	publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "starting")

	updateRunStatus(s, runID, StatusExecutorDispatched)
	publishRunEvent(p.EventHub, runID, events.KindRunSummary, "executor", "dispatched")

	p.launcher()(func() {
		defer globalRuntimeRegistry.delete(exec.ID)
		runBackgroundDispatch(commandCtx, p, runID, exec.ID, ownershipToken, invocation, adapter, repo)
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
