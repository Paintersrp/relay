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
	EvidenceSink    ExecutionEvidenceSink
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
	return func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
		return pipeline.RunLocalAgentCommandArgsStreamingWithController(ctx, workDir, binary, args, stdin, timeout, callbacks, p.processController())
	}
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

func (p *DispatchParams) evidenceSink() ExecutionEvidenceSink {
	if p.EvidenceSink != nil {
		return p.EvidenceSink
	}
	return storeExecutionEvidenceSink{store: p.Store}
}

type ExecutionEvidenceSink interface {
	Write(runID int64, kind, filename string, data []byte) (string, error)
	Register(runID int64, kind, path, mimeType string) error
	Delete(runID int64, kind, filename string) error
}

type storeExecutionEvidenceSink struct {
	store *store.Store
}

func (s storeExecutionEvidenceSink) Write(runID int64, kind, filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	return artifacts.Write(runID, kind, filename, redactSensitiveBytes(data))
}

func (s storeExecutionEvidenceSink) Register(runID int64, kind, path, mimeType string) error {
	return recordExecutorArtifact(s.store, runID, kind, path, mimeType)
}

func (s storeExecutionEvidenceSink) Delete(runID int64, kind, filename string) error {
	var errs []string
	if s.store != nil {
		if err := s.store.DeleteArtifactsByRunKind(runID, kind); err != nil {
			errs = append(errs, "delete artifact rows: "+err.Error())
		}
	}
	if err := artifacts.Delete(runID, kind, filename); err != nil && !os.IsNotExist(err) && !strings.Contains(err.Error(), "unknown artifact kind") {
		errs = append(errs, "delete artifact file: "+err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
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

func createEvent(store *store.Store, runID int64, level, message string) error {
	if store == nil {
		return fmt.Errorf("store is unavailable")
	}
	_, err := store.CreateEvent(runID, level, message)
	return err
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

func RedactSensitiveText(value string) string {
	return redactSensitive(value)
}

func RedactSensitiveBytes(data []byte) []byte {
	return redactSensitiveBytes(data)
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

func writeExecutorArtifact(sink ExecutionEvidenceSink, runID int64, kind string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	if sink == nil {
		sink = storeExecutionEvidenceSink{}
	}
	return sink.Write(runID, kind, pipeline.ArtifactFilename(kind), data)
}

func recordExecutorArtifact(store *store.Store, runID int64, kind, path, mimeType string) error {
	if store == nil || path == "" {
		return nil
	}
	_, err := store.CreateArtifact(runID, kind, path, mimeType)
	return err
}

func deleteExecutorArtifacts(sink ExecutionEvidenceSink, runID int64) error {
	if sink == nil {
		sink = storeExecutionEvidenceSink{}
	}
	var errs []string
	for _, kind := range []string{
		ArtifactKindExecutorStdout,
		ArtifactKindExecutorStderr,
		ArtifactKindCommandLog,
		ArtifactKindExecutorResult,
		ArtifactKindCodexLastMessage,
		ArtifactKindExecutorUsage,
	} {
		if err := sink.Delete(runID, kind, pipeline.ArtifactFilename(kind)); err != nil {
			errs = append(errs, kind+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func updateRunStatus(st *store.Store, runID int64, status string) (*store.Run, error) {
	if st == nil {
		return nil, nil
	}
	updatedRun, err := st.UpdateRunStatus(runID, status)
	if err != nil {
		return nil, err
	}
	if err := plans.NewRunLifecycleService(st).SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		_, _ = st.CreateEvent(runID, "warn", "Associated pass status sync failed: "+err.Error())
		return updatedRun, err
	}
	return updatedRun, nil
}

func recordDispatchBlocker(st *store.Store, runID int64, message string, terminalRunStatus string) error {
	if st == nil {
		return fmt.Errorf("store is unavailable")
	}
	var updatedRun *store.Run
	var err error
	if terminalRunStatus != "" {
		updatedRun, err = st.RecordRunStatusEvent(runID, terminalRunStatus, "warn", message)
		if err != nil {
			return err
		}
	} else if _, err := st.CreateEvent(runID, "warn", message); err != nil {
		return err
	}
	if updatedRun != nil {
		if err := plans.NewRunLifecycleService(st).SyncAssociatedPassForRunStatus(updatedRun); err != nil {
			return fmt.Errorf("dispatch blocker transaction committed but associated pass status sync failed: %w", err)
		}
	}
	return nil
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

func blockExecutorPreflight(p *DispatchParams, s *store.Store, runID int64, inv ExecutorInvocation, res ExecutorPreflightResult) error {
	sink := p.evidenceSink()

	// Stage A: evidence — fail before any durable state mutation
	if err := deleteExecutorArtifacts(sink, runID); err != nil {
		return fmt.Errorf("executor preflight persistence failed: %s", err.Error())
	}

	jsonBytes, _ := json.MarshalIndent(res, "", "  ")

	logText := fmt.Sprintf("Preflight: BLOCKED\nCommand: %s\nWorkDir: %s\nModel: %s\nAgent: %s\n\n--- PREFLIGHT DETAILS ---\n%s\n", inv.Preview, inv.WorkDir, inv.Model, inv.Agent, string(jsonBytes))
	logPath, logErr := writeExecutorArtifact(sink, runID, ArtifactKindCommandLog, []byte(logText))
	if logErr != nil {
		return fmt.Errorf("executor preflight persistence failed: %s", logErr.Error())
	}
	if logPath != "" {
		if err := sink.Register(runID, ArtifactKindCommandLog, logPath, "text/plain"); err != nil {
			return fmt.Errorf("executor preflight persistence failed: %s", err.Error())
		}
	}

	resText := fmt.Sprintf("STATUS: BLOCKED\n\nBlocker/error only if blocked: executor preflight failed: %s\n", res.BlockerText)
	resPath, resErr := writeExecutorArtifact(sink, runID, ArtifactKindExecutorResult, []byte(resText))
	if resErr != nil {
		return fmt.Errorf("executor preflight persistence failed: %s", resErr.Error())
	}
	if resPath != "" {
		if err := sink.Register(runID, ArtifactKindExecutorResult, resPath, "text/plain"); err != nil {
			return fmt.Errorf("executor preflight persistence failed: %s", err.Error())
		}
	}

	// Stage B: durable blocker state — transactional
	updatedRun, err := s.RecordExecutorPreflightBlocked(runID, StatusExecutorBlocked, "warn", "Executor preflight blocked: "+res.BlockerText)
	if err != nil {
		return fmt.Errorf("executor preflight persistence failed: %s", err.Error())
	}
	if err := plans.NewRunLifecycleService(s).SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		return fmt.Errorf("executor preflight blocked run/event transaction committed but associated pass status sync failed: %w", err)
	}

	publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
	publishRunEvent(p.EventHub, runID, events.KindRunSummary, "executor", "blocked")
	return nil
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
	launchDone chan<- struct{},
	invocation ExecutorInvocation,
	adapter ExecutorAdapter,
	repo *store.Repo,
) {
	var launchDoneOnce sync.Once
	closeLaunchDone := func() {
		launchDoneOnce.Do(func() {
			if launchDone != nil {
				close(launchDone)
			}
		})
	}
	l := p.log()
	s := p.Store
	hub := p.EventHub
	runner := p.runner()
	sink := p.evidenceSink()

	startedAt := executionTimestampNow()
	createEvent(s, runID, "info", "Executor dispatched: "+invocation.Preview)

	stream := &streamingState{}
	stream.progressParser = newProgressParser()
	if err := deleteExecutorArtifacts(sink, runID); err != nil {
		stream.recordWriteError("delete_previous_evidence", err)
	}
	if err := artifacts.EnsureDir(runID); err != nil {
		l.Error("executor: ensure artifact dir", "error", err)
	}

	if claimed, won, err := s.ClaimAgentExecutionLaunch(execID, ownershipToken); err != nil {
		l.Error("executor: claim launch", "run_id", runID, "exec_id", execID, "error", err)
		markTerminationFailed(s, execID, "claim launch failed: "+err.Error())
		closeLaunchDone()
		return
	} else if !won {
		if claimed != nil && claimed.CancellationRequestedAt.Valid {
			if prevented, preventedWon, preventErr := s.RecordAgentExecutionStartPrevented(execID, ownershipToken); preventErr == nil && preventedWon {
				finished := executionTimestampNow()
				_, _, _ = terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
					Status:                  ExecutionStatusCanceled,
					Reason:                  TerminalReasonCanceled,
					FinishedAt:              finished,
					CancellationCompletedAt: finished,
					EventLevel:              "warn",
					EventMessage:            "Executor canceled before process start",
					StepEventStatus:         "canceled",
					RunStatus:               StatusExecutorBlocked,
					RunEventStatus:          "blocked",
				})
				_ = prevented
				closeLaunchDone()
				return
			}
		}
		markTerminationFailed(s, execID, "launch ownership could not be claimed and start was not durably prevented")
		closeLaunchDone()
		return
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

				appendPath, err := writeExecutorArtifact(sink, runID, ArtifactKindExecutorStdout, chunk)
				if err != nil {
					stream.recordWriteError("append_stdout", err)
				} else if appendPath != "" {
					stream.recordWriteError("register_append_stdout", sink.Register(runID, ArtifactKindExecutorStdout, appendPath, "text/plain"))
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

				appendPath, err := writeExecutorArtifact(sink, runID, ArtifactKindExecutorStderr, chunk)
				if err != nil {
					stream.recordWriteError("append_stderr", err)
				} else if appendPath != "" {
					stream.recordWriteError("register_append_stderr", sink.Register(runID, ArtifactKindExecutorStderr, appendPath, "text/plain"))
				}

				parseEvents := stream.progressParser.feed(chunk)
				for _, ev := range parseEvents {
					stream.emitProgressEvent(s, runID, ev)
				}
			},
			OnStartError: func(err error) {
			},
			OnProcessStarted: func(identity pipeline.ProcessIdentity) error {
				startedAt = executionTimestampNow()
				if _, won, err := s.RegisterAgentExecutionProcess(execID, store.AgentProcessIdentityUpdate{
					ProcessID:           int64(identity.PID),
					ProcessGroupID:      int64(identity.GroupID),
					ProcessIdentity:     identity.Encode(),
					ProcessStartedAt:    identity.StartedAt,
					StartedAt:           startedAt,
					PlatformOwnershipID: identity.Nonce,
					OwnershipToken:      ownershipToken,
				}); err != nil {
					stream.recordWriteError("register_process_identity", err)
					return fmt.Errorf("register process identity: %w", err)
				} else if won {
					publishRunEvent(hub, runID, events.KindStepAgent, "executor", "running")
					return nil
				}
				err := fmt.Errorf("process identity registration lost ownership")
				stream.recordWriteError("register_process_identity", err)
				return err
			},
			OnLaunchSettled: func(disp pipeline.AgentLaunchDisposition) {
				closeLaunchDone()
			},
		},
	)
	closeLaunchDone()

	if runResult.TerminationError == "" {
		if current, err := s.GetAgentExecution(execID); err == nil && current != nil &&
			current.LaunchState == "start_in_progress" && !current.ProcessIdentity.Valid {
			switch runResult.LaunchDisposition {
			case pipeline.AgentLaunchNotStarted:
				_, _, _ = s.RecordAgentExecutionStartPrevented(execID, ownershipToken)
			case pipeline.AgentLaunchCleanupVerified:
				markTerminationVerified(s, execID)
			case pipeline.AgentLaunchCleanupPending:
				markTerminationFailed(s, execID, "cleanup pending after launch")
			case pipeline.AgentLaunchOwned:
				markTerminationFailed(s, execID, "launch completed but process identity was not durably registered")
			case pipeline.AgentLaunchUnresolved:
				markTerminationFailed(s, execID, "launch disposition unresolved after start attempt")
			default:
				markTerminationFailed(s, execID, "unknown launch disposition: "+string(runResult.LaunchDisposition))
			}
		}
	}

	flushEvents := stream.progressParser.flush()
	for _, ev := range flushEvents {
		stream.emitProgressEvent(s, runID, ev)
	}

	stream.mu.Lock()
	finalStdout := stream.streamedStdout.String()
	finalStderr := stream.streamedStderr.String()
	stream.mu.Unlock()

	stdoutPath, err := writeExecutorArtifact(sink, runID, ArtifactKindExecutorStdout, []byte(finalStdout))
	stream.recordWriteError("final_stdout", err)
	if stdoutPath != "" {
		stream.recordWriteError("register_final_stdout", sink.Register(runID, ArtifactKindExecutorStdout, stdoutPath, "text/plain"))
	}
	stderrPath, err := writeExecutorArtifact(sink, runID, ArtifactKindExecutorStderr, []byte(finalStderr))
	stream.recordWriteError("final_stderr", err)
	if stderrPath != "" {
		stream.recordWriteError("register_final_stderr", sink.Register(runID, ArtifactKindExecutorStderr, stderrPath, "text/plain"))
	}

	combinedLog := combinedLogText(finalStdout, finalStderr)
	combinedPath, err := writeExecutorArtifact(sink, runID, ArtifactKindCommandLog, []byte(combinedLog))
	stream.recordWriteError("combined_log", err)
	if combinedPath != "" {
		stream.recordWriteError("register_combined_log", sink.Register(runID, ArtifactKindCommandLog, combinedPath, "text/plain"))
	}

	currentExec, _ := s.GetAgentExecution(execID)
	if currentExec != nil && currentExec.CancellationRequestedAt.Valid {
		stream.recordWriteError("git_evidence", collectAndPersistGitEvidence(sink, s, runID, repo.Path))
		finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)
		ec := int64(runResult.ExitCode)
		if runResult.TimedOut {
			ec = -2
		}
		errMsg := strings.TrimSpace(runResult.Error)
		if errMsg == "" && ctx.Err() != nil {
			errMsg = "executor cancellation requested"
		}
		if runResult.TerminationError != "" {
			errMsg = appendError(errMsg, runResult.TerminationError)
		}
		if runResult.ReleaseError != "" {
			errMsg = appendError(errMsg, "release owned process: "+runResult.ReleaseError)
		}
		writeErrSummary := stream.writeErrorSummary()
		if writeErrSummary != "" {
			errMsg = appendError(errMsg, writeErrSummary)
		}
		if runResult.TerminationVerified {
			markTerminationVerified(s, execID)
			if runResult.ReleaseError != "" {
				if _, err := appendLifecycleError(s, execID, "release owned process: "+runResult.ReleaseError); err != nil {
					errMsg = appendError(errMsg, "persist lifecycle error: "+err.Error())
				}
			}
		} else {
			markTerminationFailed(s, execID, errMsg)
			createEvent(s, runID, "warn", "Executor cancellation cleanup is still pending")
			return
		}
		status := ExecutionStatusCanceled
		reason := TerminalReasonCanceled
		stepStatus := "canceled"
		eventMessage := "Executor canceled by operator request"
		if writeErrSummary != "" || runResult.ReleaseError != "" {
			status = ExecutionStatusFailed
			reason = TerminalReasonFailed
			stepStatus = "blocked"
			eventMessage = "Executor cancellation failed"
		}
		_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
			Status:                  status,
			Reason:                  reason,
			ExitCode:                &ec,
			StartedAt:               startedAt,
			FinishedAt:              finishedStr,
			StdoutPath:              stdoutPath,
			StderrPath:              stderrPath,
			CombinedPath:            combinedPath,
			Error:                   errMsg,
			CancellationCompletedAt: finishedStr,
			EventLevel:              "warn",
			EventMessage:            eventMessage,
			StepEventStatus:         stepStatus,
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
		if runResult.TerminationError != "" {
			errMsg = appendError(errMsg, runResult.TerminationError)
		}
		if runResult.ReleaseError != "" {
			errMsg = appendError(errMsg, "release owned process: "+runResult.ReleaseError)
		}
		writeErrSummary := stream.writeErrorSummary()
		if writeErrSummary != "" {
			errMsg = appendError(errMsg, writeErrSummary)
		}
		if runResult.TerminationVerified {
			markTerminationVerified(s, execID)
			if runResult.ReleaseError != "" {
				if _, err := appendLifecycleError(s, execID, "release owned process: "+runResult.ReleaseError); err != nil {
					errMsg = appendError(errMsg, "persist lifecycle error: "+err.Error())
				}
			}
		} else {
			markTerminationFailed(s, execID, errMsg)
			createEvent(s, runID, "warn", "Executor timeout cleanup is still pending")
			return
		}
		status := ExecutionStatusTimedOut
		reason := TerminalReasonTimedOut
		stepStatus := "timed_out"
		eventMessage := "Executor timed out"
		if writeErrSummary != "" || runResult.ReleaseError != "" {
			status = ExecutionStatusFailed
			reason = TerminalReasonFailed
			stepStatus = "blocked"
			eventMessage = "Executor timeout cleanup failed"
		}
		_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
			Status:          status,
			Reason:          reason,
			ExitCode:        &ec,
			StartedAt:       startedAt,
			FinishedAt:      finishedStr,
			StdoutPath:      stdoutPath,
			StderrPath:      stderrPath,
			CombinedPath:    combinedPath,
			Error:           errMsg,
			EventLevel:      "warn",
			EventMessage:    eventMessage,
			StepEventStatus: stepStatus,
			RunStatus:       StatusExecutorBlocked,
			RunEventStatus:  "blocked",
		})
		if err != nil {
			l.Error("executor: terminalize timed out execution", "run_id", runID, "exec_id", execID, "error", err)
		}
		return
	}

	if runResult.TerminationError != "" && !runResult.TerminationVerified {
		markTerminationFailed(s, execID, runResult.TerminationError)
		createEvent(s, runID, "warn", "Executor cleanup is still pending: "+runResult.TerminationError)
		return
	}

	errMsg := runResult.Error
	if runResult.ReleaseError != "" {
		errMsg = appendError(errMsg, "release owned process: "+runResult.ReleaseError)
	}

	terminalReleaseError := ""
	treeEvidence := ensureExecutionTreeAbsentForTerminal(s, p.processController(), execID)
	if !treeEvidence.VerifiedAbsent {
		blocker := terminalTreeEvidenceError(treeEvidence)
		markTerminationFailed(s, execID, blocker)
		createEvent(s, runID, "warn", "Executor completion cleanup is still pending: "+blocker)
		return
	} else if treeEvidence.ReleaseError != nil {
		terminalReleaseError = "release owned process during terminal verification: " + treeEvidence.ReleaseError.Error()
		errMsg = appendError(errMsg, terminalReleaseError)
		if _, err := appendLifecycleError(s, execID, terminalReleaseError); err != nil {
			errMsg = appendError(errMsg, "persist lifecycle error: "+err.Error())
		}
	}

	finishedStr := runResult.FinishedAt.Format(time.RFC3339Nano)
	ec := int64(runResult.ExitCode)
	execStatus := ExecutionStatusSucceeded
	if runResult.ExitCode != 0 {
		execStatus = ExecutionStatusFailed
	}
	if runResult.ReleaseError != "" || terminalReleaseError != "" {
		execStatus = ExecutionStatusFailed
	}

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
		resultPath, resultErr := writeExecutorArtifact(sink, runID, ArtifactKindExecutorResult, []byte(resultText))
		stream.recordWriteError("exit_result", resultErr)
		if resultPath != "" {
			stream.recordWriteError("register_exit_result", sink.Register(runID, ArtifactKindExecutorResult, resultPath, "text/plain"))
		}
		writeErrSummary = stream.writeErrorSummary()
		if writeErrSummary != "" {
			errMsg = appendError(errMsg, writeErrSummary)
			errPtr = &errMsg
			execStatus = ExecutionStatusFailed
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

	stream.recordWriteError("git_evidence", collectAndPersistGitEvidence(sink, s, runID, repo.Path))
	writeErrSummary = stream.writeErrorSummary()
	if writeErrSummary != "" {
		errMsg = appendError(errMsg, writeErrSummary)
		errPtr = &errMsg
		execStatus = ExecutionStatusFailed
	}

	if runResult.Stdout != "" || invocation.ResultFile != "" {
		normalizationInput := runResult.Stdout

		if invocation.ResultFile != "" {
			if content, err := os.ReadFile(invocation.ResultFile); err == nil {
				trimmed := strings.TrimSpace(string(content))
				if trimmed != "" {
					appendPath, wErr := writeExecutorArtifact(sink, runID, ArtifactKindCodexLastMessage, content)
					if wErr == nil && appendPath != "" {
						stream.recordWriteError("register_last_message", sink.Register(runID, ArtifactKindCodexLastMessage, appendPath, "text/plain"))
					} else if wErr != nil {
						stream.recordWriteError("last_message", wErr)
					}
					normalizationInput = string(content)
				}
			} else {
				stream.recordWriteError("read_result_file", err)
				errMsg = appendError(errMsg, "read configured result file: "+err.Error())
				errPtr = &errMsg
				execStatus = ExecutionStatusFailed
			}
		}

		if normalizationInput != "" {
			res := adapter.NormalizeResult(normalizationInput)

			resultPath := ""
			if res.ExecutorResultText != "" {
				var resultErr error
				resultPath, resultErr = writeExecutorArtifact(sink, runID, ArtifactKindExecutorResult, []byte(res.ExecutorResultText))
				stream.recordWriteError("executor_result", resultErr)
				if resultPath != "" {
					stream.recordWriteError("register_executor_result", sink.Register(runID, ArtifactKindExecutorResult, resultPath, "text/plain"))
				}
			}
			writeErrSummary = stream.writeErrorSummary()
			if writeErrSummary != "" {
				errMsg = appendError(errMsg, writeErrSummary)
				errPtr = &errMsg
				execStatus = ExecutionStatusFailed
			}

			switch res.Status {
			case pipeline.AgentResultDone:
				_, _, err := terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
					Status:          execStatus,
					Reason:          terminalReasonForStatus(execStatus),
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

	resultPath, err := writeExecutorArtifact(sink, runID, ArtifactKindExecutorResult, []byte("STATUS: UNKNOWN\nNo stdout captured from executor.\n"))
	stream.recordWriteError("executor_result", err)
	if resultPath != "" {
		stream.recordWriteError("register_no_output_result", sink.Register(runID, ArtifactKindExecutorResult, resultPath, "text/plain"))
	}
	writeErrSummary = stream.writeErrorSummary()
	if writeErrSummary != "" {
		errMsg = appendError(errMsg, writeErrSummary)
	}
	noOutputError := "no stdout captured from executor"
	if errMsg != "" {
		noOutputError = errMsg + "; " + noOutputError
	}
	_, _, err = terminalizeExecution(s, hub, l, runID, execID, terminalExecutionInput{
		Status:          ExecutionStatusFailed,
		Reason:          TerminalReasonFailed,
		ExitCode:        &ec,
		StartedAt:       startedAt,
		FinishedAt:      finishedStr,
		StdoutPath:      stdoutPath,
		StderrPath:      stderrPath,
		CombinedPath:    combinedPath,
		ResultPath:      resultPath,
		Error:           noOutputError,
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

func appendError(base, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return extra
	}
	return base + "; " + extra
}

type terminalTreeEvidence struct {
	VerifiedAbsent    bool
	VerificationError error
	ReleaseError      error
}

func terminalTreeEvidenceError(e terminalTreeEvidence) string {
	msg := ""
	if e.VerificationError != nil {
		msg = appendError(msg, e.VerificationError.Error())
	}
	if e.ReleaseError != nil {
		msg = appendError(msg, "release owned process during terminal verification: "+e.ReleaseError.Error())
	}
	if msg == "" {
		msg = "process tree absence was not verified during terminal cleanup verification"
	}
	return msg
}

func ensureExecutionTreeAbsentForTerminal(st *store.Store, controller pipeline.ProcessController, execID int64) terminalTreeEvidence {
	if st == nil {
		return terminalTreeEvidence{VerificationError: fmt.Errorf("store is unavailable for terminal cleanup verification")}
	}
	exec, err := st.GetAgentExecution(execID)
	if err != nil {
		return terminalTreeEvidence{VerificationError: fmt.Errorf("load execution for terminal cleanup verification: %w", err)}
	}
	if exec == nil {
		return terminalTreeEvidence{VerificationError: fmt.Errorf("execution missing during terminal cleanup verification")}
	}
	if exec.LaunchState == "start_prevented" || exec.TerminationState == "verified_absent" {
		return terminalTreeEvidence{VerifiedAbsent: true}
	}
	identity, err := processIdentityFromExecution(exec)
	if err != nil {
		return terminalTreeEvidence{VerificationError: fmt.Errorf("process identity unavailable during terminal cleanup verification: %w", err)}
	}
	if controller == nil {
		controller = pipeline.DefaultProcessController()
	}
	owned, err := controller.OpenOwned(identity)
	if err != nil {
		return terminalTreeEvidence{VerificationError: fmt.Errorf("process tree ownership unavailable during terminal cleanup verification: %w", err)}
	}
	running, err := owned.TreeRunning()
	releaseErr := owned.Release()
	if err != nil {
		return terminalTreeEvidence{
			VerificationError: fmt.Errorf("process tree presence unverifiable during terminal cleanup verification: %w", err),
			ReleaseError:      releaseErr,
		}
	}
	if running {
		return terminalTreeEvidence{
			VerificationError: fmt.Errorf("process tree still running after executor completion"),
			ReleaseError:      releaseErr,
		}
	}
	markTerminationVerified(st, execID)
	return terminalTreeEvidence{VerifiedAbsent: true, ReleaseError: releaseErr}
}

func terminalReasonForStatus(status string) string {
	if status == ExecutionStatusSucceeded {
		return TerminalReasonSucceeded
	}
	return TerminalReasonFailed
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
		if perr := recordDispatchBlocker(s, runID, "Executor dispatch blocked: "+err.Error(), ""); perr != nil {
			return DispatchResult{}, fmt.Errorf("workspace prerequisite failed: %w; %w", err, perr)
		}
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("workspace prerequisite failed: %w", err)
	}

	selectedModel, err := getRunModel(s, runID)
	if err != nil {
		if perr := recordDispatchBlocker(s, runID, "Executor dispatch blocked: "+err.Error(), ""); perr != nil {
			return DispatchResult{}, fmt.Errorf("model prerequisite failed: %w; %w", err, perr)
		}
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("model prerequisite failed: %w", err)
	}

	briefData, err := readExecutorBrief(s, runID)
	if err != nil {
		if perr := recordDispatchBlocker(s, runID, "Executor dispatch blocked: "+err.Error(), ""); perr != nil {
			return DispatchResult{}, fmt.Errorf("executor brief prerequisite failed: %w; %w", err, perr)
		}
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("executor brief prerequisite failed: %w", err)
	}

	adapter := p.Adapter
	if adapter == nil {
		var err error
		adapter, err = NewAdapterFromID(run.ExecutorAdapter)
		if err != nil {
			blockMsg := "Executor dispatch blocked: " + err.Error()
			if perr := recordDispatchBlocker(s, runID, blockMsg, StatusExecutorBlocked); perr != nil {
				return DispatchResult{}, fmt.Errorf("adapter error: %w; %w", err, perr)
			}
			publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
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
		if perr := recordDispatchBlocker(s, runID, "Executor dispatch blocked: invocation build failed: "+err.Error(), ""); perr != nil {
			return DispatchResult{}, fmt.Errorf("invocation build failed: %w; %w", err, perr)
		}
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("invocation build failed: %w", err)
	}

	existingExec, err := s.GetActiveAgentExecutionByRun(runID)
	if err == nil && existingExec != nil {
		if perr := recordDispatchBlocker(s, runID, "Executor dispatch blocked: an execution is already running", ""); perr != nil {
			return DispatchResult{}, fmt.Errorf("an execution is already running for this run; %w", perr)
		}
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("an execution is already running for this run")
	}

	preflightRes := p.preflight()(invocation)
	if !preflightRes.OK {
		if err := blockExecutorPreflight(p, s, runID, invocation, preflightRes); err != nil {
			return DispatchResult{Dispatched: false}, fmt.Errorf("executor preflight failed: %s; %w", preflightRes.BlockerText, err)
		}
		return DispatchResult{Dispatched: false}, fmt.Errorf("executor preflight failed: %s", preflightRes.BlockerText)
	}

	ownerID := p.ownerInstanceID()
	ownershipToken := newOwnershipToken()
	exec, err := s.CreateOwnedAgentExecution(runID, string(adapter.ID()), ExecutionStatusStarting, invocation.Preview, "local_process", ownerID, ownershipToken)
	if err != nil {
		if perr := recordDispatchBlocker(s, runID, "Executor dispatch blocked: failed to create execution record: "+err.Error(), ""); perr != nil {
			return DispatchResult{}, fmt.Errorf("create execution record: %w; %w", err, perr)
		}
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("create execution record: %w", err)
	}

	l.Info("executor: dispatching from executor_brief.md", "run_id", runID, "exec_id", exec.ID, "model", invocation.Model)

	commandCtx, cancel := context.WithCancel(context.Background())
	launchDone := make(chan struct{})
	globalRuntimeRegistry.put(runtimeHandle{
		execID:         exec.ID,
		runID:          runID,
		ownershipToken: ownershipToken,
		cancel:         cancel,
		controller:     p.processController(),
		launchDone:     launchDone,
	})

	createEvent(s, runID, "info", "Executor dispatched from executor_brief.md")
	publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "starting")

	updateRunStatus(s, runID, StatusExecutorDispatched)
	publishRunEvent(p.EventHub, runID, events.KindRunSummary, "executor", "dispatched")

	p.launcher()(func() {
		defer globalRuntimeRegistry.delete(exec.ID)
		runBackgroundDispatch(commandCtx, p, runID, exec.ID, ownershipToken, launchDone, invocation, adapter, repo)
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
	return writeExecutorArtifact(storeExecutionEvidenceSink{}, runID, kind, buf.Bytes())
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

func collectAndPersistGitEvidence(sink ExecutionEvidenceSink, s *store.Store, runID int64, repoPath string) error {
	if repoPath == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect git metadata: %w", err)
	}
	ev, err := pipeline.CollectGitDiffEvidence(context.Background(), repoPath, 30*time.Second)
	if err != nil {
		if s != nil {
			_, _ = s.CreateEvent(runID, "warn", fmt.Sprintf("Failed to collect git evidence: %v", err))
		}
		return fmt.Errorf("collect git evidence: %w", err)
	}

	var errs []string
	for _, item := range []struct {
		kind    string
		content string
	}{
		{"git_status_text", ev.StatusText},
		{"git_diff_stat", ev.DiffStat},
		{"git_diff_numstat", ev.DiffNumstat},
		{"git_diff_name_status", ev.NameStatus},
		{"git_diff_patch", ev.DiffPatch},
	} {
		if err := persistGitArtifact(sink, runID, item.kind, item.content); err != nil {
			errs = append(errs, item.kind+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func persistGitArtifact(sink ExecutionEvidenceSink, runID int64, kind, content string) error {
	if content == "" {
		return nil
	}
	filename := pipeline.ArtifactFilename(kind)
	if sink == nil {
		sink = storeExecutionEvidenceSink{}
	}
	path, err := sink.Write(runID, kind, filename, []byte(content))
	if err != nil {
		return err
	}
	if path != "" {
		if err := sink.Register(runID, kind, path, "text/plain"); err != nil {
			return err
		}
	}
	return nil
}
