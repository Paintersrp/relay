package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

	ArtifactKindExecutorStdout = "executor_stdout"
	ArtifactKindExecutorStderr = "executor_stderr"
	ArtifactKindCommandLog     = "command_log"
	ArtifactKindExecutorResult = "executor_result"
)

var knownSecrets = []string{
	"RELAY_OPENCODE_BIN",
	"RELAY_OPENCODE_AGENT",
	"RELAY_OPENCODE_VARIANT",
}

type DispatchParams struct {
	Store       *store.Store
	Log         *slog.Logger
	EventHub    *events.Hub
	RunID       int64
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

func resolveOpenCodeModel(selectedModel string) (string, error) {
	return pipeline.ResolveOpenCodeModel(selectedModel)
}

func openCodeConfigFromEnv() pipeline.OpenCodeRunConfig {
	return pipeline.OpenCodeRunConfigFromEnv()
}

func buildExecutorInvocation(cfg pipeline.OpenCodeRunConfig, repoPath, briefContent, selectedModel, briefPath string) (pipeline.OpenCodeRunInvocation, error) {
	if strings.TrimSpace(cfg.Binary) == "" {
		return pipeline.OpenCodeRunInvocation{}, fmt.Errorf("OpenCode binary is empty; set RELAY_OPENCODE_BIN")
	}
	if strings.TrimSpace(repoPath) == "" {
		return pipeline.OpenCodeRunInvocation{}, fmt.Errorf("repo path is empty")
	}
	if strings.TrimSpace(briefContent) == "" {
		return pipeline.OpenCodeRunInvocation{}, fmt.Errorf("executor brief content is empty")
	}
	if strings.TrimSpace(selectedModel) == "" {
		return pipeline.OpenCodeRunInvocation{}, fmt.Errorf("selected model is empty")
	}

	model, err := resolveOpenCodeModel(selectedModel)
	if err != nil {
		return pipeline.OpenCodeRunInvocation{}, err
	}

	agent := strings.TrimSpace(cfg.Agent)
	if agent == "" {
		agent = "build"
	}

	args := []string{
		"run",
		"--format", "json",
		"--dir", repoPath,
		"--agent", agent,
		"--model", model,
		"--thinking", "max",
	}
	if strings.TrimSpace(cfg.Variant) != "" {
		args = append(args, "--variant", strings.TrimSpace(cfg.Variant))
	}

	preview := pipeline.ShellPreview(cfg.Binary, args)
	preview += " < " + quotePreview(briefPath)

	return pipeline.OpenCodeRunInvocation{
		Binary:          cfg.Binary,
		Args:            args,
		WorkDir:         repoPath,
		Stdin:           briefContent,
		StdinSource:     briefPath,
		StdinBytes:      len([]byte(briefContent)),
		AgentPromptPath: briefPath,
		PacketPath:      "",
		Model:           model,
		Agent:           agent,
		Variant:         strings.TrimSpace(cfg.Variant),
		Preview:         preview,
	}, nil
}

func quotePreview(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\n\"'") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
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
	} {
		store.DeleteArtifactsByRunKind(runID, kind)
		artifacts.Delete(runID, kind, pipeline.ArtifactFilename(kind))
	}
}

func updateRunStatus(store *store.Store, runID int64, status string) {
	if store == nil {
		return
	}
	store.UpdateRunStatus(runID, status)
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

func runBackgroundDispatch(
	ctx context.Context,
	p *DispatchParams,
	runID int64,
	execID int64,
	invocation pipeline.OpenCodeRunInvocation,
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
			},
		},
	)

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

	if runResult.Stdout != "" {
		assistantText := pipeline.ExtractOpenCodeAssistantText(runResult.Stdout)
		parsed := pipeline.ParseAgentResult(assistantText)

		executorResult := fmt.Sprintf("STATUS: %s\n\nBuild status: %s\nTest status: %s\nCount of LOC changed: %s\n",
			string(parsed.Status), parsed.BuildStatus, parsed.TestStatus, parsed.LOCChanged)
		if parsed.BlockerError != "" {
			executorResult += fmt.Sprintf("Blocker/error only if blocked: %s\n", parsed.BlockerError)
		}
		resultPath, _ := writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte(executorResult))
		if resultPath != "" {
			recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resultPath, "text/plain")
		}

		switch parsed.Status {
		case pipeline.AgentResultDone:
			createEvent(s, runID, "info", "Executor completed: DONE")
			publishRunEvent(hub, runID, events.KindStepAgent, "executor", "done")
			updateRunStatus(s, runID, StatusExecutorDone)
			publishRunEvent(hub, runID, events.KindRunSummary, "executor", "done")
		case pipeline.AgentResultBlocked:
			blockerText := parsed.BlockerError
			if blockerText == "" {
				blockerText = "executor reported BLOCKED"
			}
			createEvent(s, runID, "warn", "Executor blocked: "+blockerText)
			publishRunEvent(hub, runID, events.KindStepAgent, "executor", "blocked")
			updateRunStatus(s, runID, StatusExecutorBlocked)
			publishRunEvent(hub, runID, events.KindRunSummary, "executor", "blocked")
		default:
			errMsg := "executor result parse failed: missing or invalid STATUS line"
			executorResultFail := fmt.Sprintf("STATUS: UNKNOWN\n\nRaw output:\n%s\n", assistantText)
			resultPathFail, _ := writeExecutorArtifact(runID, ArtifactKindExecutorResult, []byte(executorResultFail))
			if resultPathFail != "" {
				recordExecutorArtifact(s, runID, ArtifactKindExecutorResult, resultPathFail, "text/plain")
			}
			createEvent(s, runID, "warn", errMsg)
			publishRunEvent(hub, runID, events.KindStepAgent, "executor", "parse_failed")
			updateRunStatus(s, runID, StatusExecutorBlocked)
			publishRunEvent(hub, runID, events.KindRunSummary, "executor", "blocked")
		}
		return
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

	cfg := openCodeConfigFromEnv()
	if cfg.Binary == "" {
		cfg.Binary = "opencode"
	}
	if _, err := resolveOpenCodeModel(selectedModel); err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: model resolution failed: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("model resolution failed: %w", err)
	}

	briefPath := filepath.Join(artifacts.Dir(runID), "executor_brief.md")
	invocation, err := buildExecutorInvocation(cfg, repo.Path, string(briefData), selectedModel, briefPath)
	if err != nil {
		createEvent(s, runID, "warn", "Executor dispatch blocked: "+err.Error())
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("invocation build failed: %w", err)
	}

	existingExec, err := s.GetLatestAgentExecutionByRun(runID)
	if err == nil && existingExec != nil && (existingExec.Status == "starting" || existingExec.Status == "running") {
		createEvent(s, runID, "warn", "Executor dispatch blocked: an execution is already running")
		publishRunEvent(p.EventHub, runID, events.KindStepAgent, "executor", "blocked")
		return DispatchResult{}, fmt.Errorf("an execution is already running for this run")
	}

	exec, err := s.CreateAgentExecution(runID, "opencode_go", "starting", invocation.Preview)
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
		runBackgroundDispatch(commandCtx, p, runID, exec.ID, invocation, repo)
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
