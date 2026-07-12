package executor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	workflowruns "relay/internal/app/runs/workflow"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/pipeline"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

const DefaultWorkflowExecutionTimeout = 30 * time.Minute

type WorkflowStartInput struct {
	RunID   string
	Adapter string
	Model   string
}

type WorkflowStartResult struct {
	Run       workflowstore.Run
	Attempt   workflowstore.ExecutionAttempt
	Preflight workflowrepos.ExecutionPreflightResult
	Applier   *WorkflowApplierResult
}

type WorkflowCancelResult struct {
	Run     workflowstore.Run
	Attempt workflowstore.ExecutionAttempt
}

type WorkflowAttemptView struct {
	Attempt             workflowstore.ExecutionAttempt
	Artifacts           []workflowstore.Artifact
	LiveStdout          string
	LiveStderr          string
	LiveStdoutTruncated bool
	LiveStderrTruncated bool
	LiveStdoutBytes     int64
	LiveStderrBytes     int64
}

type WorkflowCommandRunner func(
	ctx context.Context,
	workDir, binary string,
	args []string,
	stdin string,
	timeout time.Duration,
	callbacks pipeline.AgentCommandStreamCallbacks,
	controller pipeline.ProcessController,
) pipeline.AgentCommandRunResult

type workflowRuntime struct {
	cancel   context.CancelFunc
	mu       sync.Mutex
	stdout   *workflowOutputCapture
	stderr   *workflowOutputCapture
	identity pipeline.ProcessIdentity
}

func (r *workflowRuntime) setOutputCaptures(stdout, stderr *workflowOutputCapture) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdout = stdout
	r.stderr = stderr
}

func (r *workflowRuntime) appendStdout(chunk []byte) {
	r.mu.Lock()
	capture := r.stdout
	r.mu.Unlock()
	if capture != nil {
		capture.Write(chunk)
	}
}

func (r *workflowRuntime) appendStderr(chunk []byte) {
	r.mu.Lock()
	capture := r.stderr
	r.mu.Unlock()
	if capture != nil {
		capture.Write(chunk)
	}
}

func (r *workflowRuntime) snapshot() (workflowOutputSnapshot, workflowOutputSnapshot) {
	r.mu.Lock()
	stdout := r.stdout
	stderr := r.stderr
	r.mu.Unlock()
	var stdoutSnapshot workflowOutputSnapshot
	var stderrSnapshot workflowOutputSnapshot
	if stdout != nil {
		stdoutSnapshot = stdout.Snapshot()
	}
	if stderr != nil {
		stderrSnapshot = stderr.Snapshot()
	}
	return stdoutSnapshot, stderrSnapshot
}

func (r *workflowRuntime) closeOutputs() (workflowOutputSnapshot, workflowOutputSnapshot, error) {
	r.mu.Lock()
	stdout := r.stdout
	stderr := r.stderr
	r.mu.Unlock()
	var joined error
	if stdout != nil {
		joined = errors.Join(joined, stdout.Close())
	}
	if stderr != nil {
		joined = errors.Join(joined, stderr.Close())
	}
	stdoutSnapshot, stderrSnapshot := r.snapshot()
	return stdoutSnapshot, stderrSnapshot, joined
}

type WorkflowExecutionService struct {
	store               *workflowstore.Store
	runs                *workflowruns.Service
	log                 *slog.Logger
	ownerInstanceID     string
	controller          pipeline.ProcessController
	timeout             time.Duration
	preflight           func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult
	invocationPreflight func(ExecutorInvocation) ExecutorPreflightResult
	adapterFactory      func(string) (ExecutorAdapter, error)
	applier             workflowApplierFunc
	runner              WorkflowCommandRunner
	launch              func(func())
	mu                  sync.Mutex
	active              map[string]*workflowRuntime
}

func NewWorkflowExecutionService(store *workflowstore.Store, log *slog.Logger, ownerInstanceID string) *WorkflowExecutionService {
	runService, _ := workflowruns.NewService(store)
	return &WorkflowExecutionService{
		store:               store,
		runs:                runService,
		log:                 log,
		ownerInstanceID:     ownerInstanceID,
		controller:          pipeline.DefaultProcessController(),
		timeout:             DefaultWorkflowExecutionTimeout,
		preflight:           workflowrepos.VerifyExecutionPreflight,
		invocationPreflight: ValidateInvocationPreflight,
		adapterFactory:      NewAdapterFromID,
		applier:             defaultWorkflowApplier(),
		runner: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, controller pipeline.ProcessController) pipeline.AgentCommandRunResult {
			return pipeline.RunLocalAgentCommandArgsStreamingWithController(ctx, workDir, binary, args, stdin, timeout, callbacks, controller)
		},
		launch: func(fn func()) { go fn() },
		active: map[string]*workflowRuntime{},
	}
}

func (s *WorkflowExecutionService) Start(ctx context.Context, input WorkflowStartInput) (WorkflowStartResult, error) {
	if s == nil || s.store == nil || s.runs == nil {
		return WorkflowStartResult{}, fmt.Errorf("workflow execution service is unavailable")
	}
	input.RunID = strings.TrimSpace(input.RunID)
	input.Adapter = strings.TrimSpace(input.Adapter)
	input.Model = strings.TrimSpace(input.Model)
	if input.RunID == "" || input.Adapter == "" || input.Model == "" {
		return WorkflowStartResult{}, fmt.Errorf("run_id, adapter, and model are required")
	}
	normalizedAdapter, err := NormalizeKnownAdapterID(input.Adapter)
	if err != nil {
		return WorkflowStartResult{}, err
	}

	run, err := s.store.GetRunByRunID(ctx, input.RunID)
	if err != nil {
		return WorkflowStartResult{}, fmt.Errorf("load Run: %w", err)
	}
	switch run.Status {
	case workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecutionFailed, workflowstore.RunStatusCancelled:
	default:
		return WorkflowStartResult{}, fmt.Errorf("Run %q cannot start an execution attempt from status %q", run.RunID, run.Status)
	}
	repository, err := s.store.GetRepositoryTarget(ctx, run.RepoTarget)
	if err != nil {
		return WorkflowStartResult{}, fmt.Errorf("resolve repository target: %w", err)
	}
	executionSpec, executionSpecArtifact, err := s.loadVerifiedExecutionSpec(ctx, run)
	if err != nil {
		return WorkflowStartResult{}, err
	}
	brief, briefArtifact, briefPath, err := s.loadVerifiedBrief(ctx, run)
	if err != nil {
		return WorkflowStartResult{}, err
	}
	preflight := s.preflight(ctx, repository.LocalPath, run.Branch, run.BaseCommit)
	if !preflight.OK {
		return WorkflowStartResult{Run: run, Preflight: preflight}, &WorkflowPreflightError{Result: preflight}
	}

	applierResult, err := s.applyDeterministicFirst(ctx, run, repository.LocalPath, executionSpec, executionSpecArtifact)
	if err != nil {
		return WorkflowStartResult{}, err
	}
	if applierResult != nil {
		switch applierResult.Outcome {
		case "completed":
			updated, err := s.runs.RecordApplierCompleted(ctx, run.RunID)
			if err != nil {
				return WorkflowStartResult{}, err
			}
			return WorkflowStartResult{Run: updated, Preflight: preflight, Applier: applierResult}, nil
		case "blocked":
			updated, err := s.runs.RecordApplierBlocked(ctx, run.RunID)
			if err != nil {
				return WorkflowStartResult{}, err
			}
			return WorkflowStartResult{Run: updated, Preflight: preflight, Applier: applierResult}, nil
		case "partial", "not_attempted":
		default:
			return WorkflowStartResult{}, fmt.Errorf("unsupported deterministic applier outcome %q", applierResult.Outcome)
		}
	}

	if applierResult != nil && applierResult.Outcome == "partial" {
		begun, err := s.runs.BeginExecutionAttempt(ctx, workflowruns.BeginExecutionAttemptInput{
			RunID:   run.RunID,
			Adapter: normalizedAdapter,
			Model:   input.Model,
		})
		if err != nil {
			return WorkflowStartResult{}, err
		}
		selected, err := s.prepareResidualEffectiveBrief(ctx, begun.Attempt, applierResult)
		if err != nil {
			return s.failPrelaunchAttempt(ctx, begun, preflight, applierResult, nil, err)
		}
		if err := s.recordEffectiveBriefIdentity(ctx, begun.Attempt, selected); err != nil {
			return s.failPrelaunchAttempt(ctx, begun, preflight, applierResult, &selected, err)
		}
		adapter, err := s.adapterFactory(normalizedAdapter)
		if err != nil {
			return s.failPrelaunchAttempt(ctx, begun, preflight, applierResult, &selected, err)
		}
		runtimeResultPath := filepath.Join(s.store.ArtifactStore().Root(), ".runtime", run.RunID, "executor-result.tmp")
		invocation, err := adapter.BuildInvocation(ExecutorAdapterRequest{
			RunID:         run.ID,
			RepoPath:      repository.LocalPath,
			BriefContent:  string(selected.Content),
			BriefPath:     selected.Path,
			ResultPath:    runtimeResultPath,
			SelectedModel: input.Model,
			Timeout:       s.timeout,
		})
		if err != nil {
			return s.failPrelaunchAttempt(ctx, begun, preflight, applierResult, &selected, fmt.Errorf("build executor invocation: %w", err))
		}
		if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err != nil {
			return s.failPrelaunchAttempt(ctx, begun, preflight, applierResult, &selected, err)
		}
		invocationPreflight := s.invocationPreflight(invocation)
		if !invocationPreflight.OK {
			return s.failPrelaunchAttempt(ctx, begun, preflight, applierResult, &selected, fmt.Errorf("adapter preflight failed: %s", invocationPreflight.BlockerText))
		}
		runtimeCtx, cancel := context.WithCancel(context.Background())
		runtime := &workflowRuntime{cancel: cancel}
		s.putRuntime(begun.Attempt.AttemptID, runtime)
		s.launch(func() {
			defer s.deleteRuntime(begun.Attempt.AttemptID)
			s.execute(runtimeCtx, begun.Run, begun.Attempt, repository, selected, invocation, adapter, runtime)
		})
		return WorkflowStartResult{Run: begun.Run, Attempt: begun.Attempt, Preflight: preflight, Applier: applierResult}, nil
	}

	selected := fullEffectiveBriefInput(brief, briefArtifact, briefPath)
	adapter, err := s.adapterFactory(normalizedAdapter)
	if err != nil {
		return WorkflowStartResult{}, err
	}
	runtimeResultPath := filepath.Join(s.store.ArtifactStore().Root(), ".runtime", run.RunID, "executor-result.tmp")
	invocation, err := adapter.BuildInvocation(ExecutorAdapterRequest{
		RunID:         run.ID,
		RepoPath:      repository.LocalPath,
		BriefContent:  string(selected.Content),
		BriefPath:     selected.Path,
		ResultPath:    runtimeResultPath,
		SelectedModel: input.Model,
		Timeout:       s.timeout,
	})
	if err != nil {
		return WorkflowStartResult{}, fmt.Errorf("build executor invocation: %w", err)
	}
	if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err != nil {
		return WorkflowStartResult{}, err
	}
	invocationPreflight := s.invocationPreflight(invocation)
	if !invocationPreflight.OK {
		return WorkflowStartResult{}, fmt.Errorf("adapter preflight failed: %s", invocationPreflight.BlockerText)
	}
	begun, err := s.runs.BeginExecutionAttempt(ctx, workflowruns.BeginExecutionAttemptInput{
		RunID:   run.RunID,
		Adapter: normalizedAdapter,
		Model:   invocation.Model,
	})
	if err != nil {
		return WorkflowStartResult{}, err
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	runtime := &workflowRuntime{cancel: cancel}
	s.putRuntime(begun.Attempt.AttemptID, runtime)
	s.launch(func() {
		defer s.deleteRuntime(begun.Attempt.AttemptID)
		s.execute(runtimeCtx, begun.Run, begun.Attempt, repository, selected, invocation, adapter, runtime)
	})
	return WorkflowStartResult{Run: begun.Run, Attempt: begun.Attempt, Preflight: preflight, Applier: applierResult}, nil
}

func (s *WorkflowExecutionService) Cancel(ctx context.Context, runID, attemptID string) (WorkflowCancelResult, error) {
	attempt, err := s.runs.RequestExecutionAttemptCancellation(ctx, strings.TrimSpace(runID), strings.TrimSpace(attemptID))
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	if terminalAttemptStatus(attempt.Status) {
		run, err := s.store.GetRunByRowID(ctx, attempt.RunRowID)
		return WorkflowCancelResult{Run: run, Attempt: attempt}, err
	}
	if runtime := s.getRuntime(attempt.AttemptID); runtime != nil {
		runtime.cancel()
		refreshed, err := s.store.GetExecutionAttemptByAttemptID(ctx, attempt.AttemptID)
		if err != nil {
			return WorkflowCancelResult{}, err
		}
		run, err := s.store.GetRunByRowID(ctx, refreshed.RunRowID)
		return WorkflowCancelResult{Run: run, Attempt: refreshed}, err
	}
	return s.reconcileAttempt(ctx, runID, attempt, true)
}

func (s *WorkflowExecutionService) Reconcile(ctx context.Context, runID, attemptID string) (WorkflowCancelResult, error) {
	run, attempt, err := s.loadAttemptForRun(ctx, strings.TrimSpace(runID), strings.TrimSpace(attemptID))
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	if terminalAttemptStatus(attempt.Status) {
		return WorkflowCancelResult{Run: run, Attempt: attempt}, nil
	}
	return s.reconcileAttempt(ctx, run.RunID, attempt, false)
}

func (s *WorkflowExecutionService) reconcileAttempt(ctx context.Context, runID string, attempt workflowstore.ExecutionAttempt, forceCancel bool) (WorkflowCancelResult, error) {
	var state workflowAttemptRuntime
	if err := json.Unmarshal([]byte(attempt.ResultJSON), &state); err != nil {
		return WorkflowCancelResult{}, fmt.Errorf("decode execution attempt runtime: %w", err)
	}
	if attempt.Status == workflowstore.AttemptStatusPending && state.ProcessIdentity == "" {
		if !forceCancel {
			return WorkflowCancelResult{}, fmt.Errorf("pending execution attempt has no process identity to reconcile")
		}
		state.TerminationVerified = true
		state.Error = appendWorkflowError(state.Error, "operator cancellation requested before process start")
		return s.finishReconciledAttempt(ctx, attempt, state, workflowstore.AttemptStatusCancelled)
	}
	if !state.CleanupPending {
		return WorkflowCancelResult{}, fmt.Errorf("execution attempt is not awaiting process cleanup")
	}
	if state.ProcessIdentity == "" {
		return WorkflowCancelResult{}, fmt.Errorf("cleanup-pending execution attempt has no durable process identity")
	}
	identity, err := pipeline.DecodeProcessIdentity(state.ProcessIdentity)
	if err != nil {
		return WorkflowCancelResult{}, fmt.Errorf("decode durable process identity: %w", err)
	}
	owned, err := s.controller.OpenOwned(identity)
	if err != nil {
		if errors.Is(err, pipeline.ErrProcessNotRunning) {
			return s.finishReconciledAttempt(ctx, attempt, state, reconciledTerminalStatus(attempt, state, forceCancel))
		}
		return WorkflowCancelResult{}, fmt.Errorf("open owned process: %w", err)
	}
	running, treeErr := owned.TreeRunning()
	if treeErr != nil {
		_ = owned.Release()
		return WorkflowCancelResult{}, fmt.Errorf("inspect owned process tree: %w", treeErr)
	}
	if running && !forceCancel && !attempt.CancellationRequestedAt.Valid {
		if err := owned.Release(); err != nil {
			return WorkflowCancelResult{}, fmt.Errorf("release owned process: %w", err)
		}
		run, err := s.store.GetRunByRunID(ctx, runID)
		return WorkflowCancelResult{Run: run, Attempt: attempt}, err
	}
	if running {
		termination, terminateErr := owned.Terminate(2 * time.Second)
		releaseErr := owned.Release()
		if terminateErr != nil && !errors.Is(terminateErr, pipeline.ErrProcessNotRunning) {
			return WorkflowCancelResult{}, fmt.Errorf("terminate owned process: %w", terminateErr)
		}
		if releaseErr != nil {
			return WorkflowCancelResult{}, fmt.Errorf("release owned process: %w", releaseErr)
		}
		if !termination.VerifiedAbsent {
			return WorkflowCancelResult{}, fmt.Errorf("process absence was not verified")
		}
	} else if err := owned.Release(); err != nil {
		return WorkflowCancelResult{}, fmt.Errorf("release owned process: %w", err)
	}
	return s.finishReconciledAttempt(ctx, attempt, state, reconciledTerminalStatus(attempt, state, forceCancel))
}

func (s *WorkflowExecutionService) finishReconciledAttempt(ctx context.Context, attempt workflowstore.ExecutionAttempt, state workflowAttemptRuntime, status string) (WorkflowCancelResult, error) {
	state.CleanupPending = false
	state.PendingTerminalStatus = ""
	state.TerminationVerified = true
	if status == workflowstore.AttemptStatusCancelled {
		state.Error = appendWorkflowError(state.Error, "operator cancellation requested")
	}
	resultJSON, _ := json.Marshal(state)
	finished, err := s.runs.FinishExecutionAttempt(ctx, workflowruns.FinishExecutionAttemptInput{
		AttemptID:  attempt.AttemptID,
		Status:     status,
		ResultJSON: string(resultJSON),
	})
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	return WorkflowCancelResult{Run: finished.Run, Attempt: finished.Attempt}, nil
}

func reconciledTerminalStatus(attempt workflowstore.ExecutionAttempt, state workflowAttemptRuntime, forceCancel bool) string {
	if forceCancel || attempt.CancellationRequestedAt.Valid {
		return workflowstore.AttemptStatusCancelled
	}
	switch state.PendingTerminalStatus {
	case workflowstore.AttemptStatusSucceeded,
		workflowstore.AttemptStatusFailed,
		workflowstore.AttemptStatusCancelled,
		workflowstore.AttemptStatusTimedOut:
		return state.PendingTerminalStatus
	default:
		return workflowstore.AttemptStatusFailed
	}
}

func (s *WorkflowExecutionService) loadAttemptForRun(ctx context.Context, runID, attemptID string) (workflowstore.Run, workflowstore.ExecutionAttempt, error) {
	run, err := s.store.GetRunByRunID(ctx, runID)
	if err != nil {
		return workflowstore.Run{}, workflowstore.ExecutionAttempt{}, err
	}
	attempt, err := s.store.GetExecutionAttemptByAttemptID(ctx, attemptID)
	if err != nil {
		return workflowstore.Run{}, workflowstore.ExecutionAttempt{}, err
	}
	if attempt.RunRowID != run.ID {
		return workflowstore.Run{}, workflowstore.ExecutionAttempt{}, fmt.Errorf("execution attempt does not belong to Run")
	}
	return run, attempt, nil
}

func (s *WorkflowExecutionService) ListAttempts(ctx context.Context, runID string) ([]WorkflowAttemptView, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	attempts, err := s.store.ListRecentExecutionAttemptsByRun(
		ctx,
		run.ID,
		workflowstore.MaxWorkflowAttemptLimit,
	)
	if err != nil {
		return nil, err
	}
	views := make([]WorkflowAttemptView, 0, len(attempts))
	for _, attempt := range attempts {
		view, err := s.attemptView(ctx, attempt)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *WorkflowExecutionService) GetAttempt(ctx context.Context, runID, attemptID string) (WorkflowAttemptView, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return WorkflowAttemptView{}, err
	}
	attempt, err := s.store.GetExecutionAttemptByAttemptID(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return WorkflowAttemptView{}, err
	}
	if attempt.RunRowID != run.ID {
		return WorkflowAttemptView{}, sql.ErrNoRows
	}
	return s.attemptView(ctx, attempt)
}

func (s *WorkflowExecutionService) attemptView(ctx context.Context, attempt workflowstore.ExecutionAttempt) (WorkflowAttemptView, error) {
	artifacts, err := s.store.ListArtifactsByExecutionAttempt(ctx, attempt.ID)
	if err != nil {
		return WorkflowAttemptView{}, err
	}
	view := WorkflowAttemptView{Attempt: attempt, Artifacts: artifacts}
	if runtime := s.getRuntime(attempt.AttemptID); runtime != nil {
		stdout, stderr := runtime.snapshot()
		view.LiveStdout = stdout.Text
		view.LiveStderr = stderr.Text
		view.LiveStdoutTruncated = stdout.Truncated
		view.LiveStderrTruncated = stderr.Truncated
		view.LiveStdoutBytes = stdout.TotalBytes
		view.LiveStderrBytes = stderr.TotalBytes
	}
	return view, nil
}

type WorkflowPreflightError struct {
	Result workflowrepos.ExecutionPreflightResult
}

func (e *WorkflowPreflightError) Error() string {
	return e.Result.BlockerText
}

type workflowAttemptRuntime struct {
	OwnerInstanceID          string `json:"owner_instance_id,omitempty"`
	CommandPreview           string `json:"command_preview,omitempty"`
	ProcessIdentity          string `json:"process_identity,omitempty"`
	LaunchDisposition        string `json:"launch_disposition,omitempty"`
	ExitCode                 int    `json:"exit_code"`
	TimedOut                 bool   `json:"timed_out"`
	TerminationVerified      bool   `json:"termination_verified"`
	CleanupPending           bool   `json:"cleanup_pending,omitempty"`
	PendingTerminalStatus    string `json:"pending_terminal_status,omitempty"`
	Error                    string `json:"error,omitempty"`
	NormalizedStatus         string `json:"normalized_status,omitempty"`
	BlockerText              string `json:"blocker_text,omitempty"`
	EffectiveBriefArtifactID string `json:"effective_brief_artifact_id,omitempty"`
	EffectiveBriefSHA256     string `json:"effective_brief_sha256,omitempty"`
	EffectiveBriefMode       string `json:"effective_brief_mode,omitempty"`
	StdoutTruncated          bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated          bool   `json:"stderr_truncated,omitempty"`
	StdoutBytes              int64  `json:"stdout_bytes,omitempty"`
	StderrBytes              int64  `json:"stderr_bytes,omitempty"`
}

func (s *WorkflowExecutionService) recordEffectiveBriefIdentity(ctx context.Context, attempt workflowstore.ExecutionAttempt, selected effectiveBriefInput) error {
	state := workflowAttemptRuntime{
		EffectiveBriefArtifactID: selected.Artifact.ArtifactID,
		EffectiveBriefSHA256:     selected.Artifact.SHA256,
		EffectiveBriefMode:       string(selected.Mode),
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode effective brief identity: %w", err)
	}
	if _, err := s.runs.UpdateExecutionAttemptResult(ctx, attempt.AttemptID, string(data)); err != nil {
		return fmt.Errorf("record effective brief identity: %w", err)
	}
	return nil
}

func (s *WorkflowExecutionService) execute(
	ctx context.Context,
	run workflowstore.Run,
	attempt workflowstore.ExecutionAttempt,
	repository workflowstore.RepositoryTarget,
	selected effectiveBriefInput,
	invocation ExecutorInvocation,
	adapter ExecutorAdapter,
	runtime *workflowRuntime,
) {
	state := workflowAttemptRuntime{
		OwnerInstanceID:          s.ownerInstanceID,
		CommandPreview:           redactSensitive(invocation.Preview),
		EffectiveBriefArtifactID: selected.Artifact.ArtifactID,
		EffectiveBriefSHA256:     selected.Artifact.SHA256,
		EffectiveBriefMode:       string(selected.Mode),
	}
	updateState := func() {
		data, _ := json.Marshal(state)
		_, _ = s.runs.UpdateExecutionAttemptResult(context.Background(), attempt.AttemptID, string(data))
	}
	updateState()

	runtimeDir := filepath.Join(s.store.ArtifactStore().Root(), ".runtime", run.RunID, attempt.AttemptID)
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		s.finishPrelaunchFailure(attempt, &selected, "prepare output spool: "+err.Error())
		return
	}
	defer func() {
		_ = os.RemoveAll(runtimeDir)
		if invocation.ResultFile != "" {
			_ = os.Remove(invocation.ResultFile)
			_ = os.Remove(filepath.Dir(invocation.ResultFile))
		}
	}()
	stdoutCapture, err := newWorkflowOutputCapture(filepath.Join(runtimeDir, "stdout.log"), WorkflowLiveOutputLimitBytes)
	if err != nil {
		s.finishPrelaunchFailure(attempt, &selected, err.Error())
		return
	}
	stderrCapture, err := newWorkflowOutputCapture(filepath.Join(runtimeDir, "stderr.log"), WorkflowLiveOutputLimitBytes)
	if err != nil {
		_ = stdoutCapture.Close()
		s.finishPrelaunchFailure(attempt, &selected, err.Error())
		return
	}
	runtime.setOutputCaptures(stdoutCapture, stderrCapture)

	if invocation.ResultFile != "" {
		if err := os.MkdirAll(filepath.Dir(invocation.ResultFile), 0o700); err != nil {
			_, _, _ = runtime.closeOutputs()
			s.finishPrelaunchFailure(attempt, &selected, "prepare result path: "+err.Error())
			return
		}
	}

	result := s.runner(
		ctx,
		invocation.WorkDir,
		invocation.Binary,
		invocation.Args,
		invocation.Stdin,
		s.timeout,
		pipeline.AgentCommandStreamCallbacks{
			CaptureLimitBytes: WorkflowRunnerCaptureLimitBytes,
			OnProcessStarted: func(identity pipeline.ProcessIdentity) error {
				runtime.mu.Lock()
				runtime.identity = identity
				runtime.mu.Unlock()
				state.ProcessIdentity = identity.Encode()
				data, _ := json.Marshal(state)
				if _, err := s.runs.MarkExecutionAttemptRunning(context.Background(), attempt.AttemptID, string(data)); err != nil {
					return err
				}
				return nil
			},
			OnLaunchSettled: func(disposition pipeline.AgentLaunchDisposition) {
				state.LaunchDisposition = string(disposition)
				updateState()
			},
			OnStdout: runtime.appendStdout,
			OnStderr: runtime.appendStderr,
		},
		s.controller,
	)
	stdoutSnapshot, stderrSnapshot, outputErr := runtime.closeOutputs()
	state.StdoutTruncated = stdoutSnapshot.Truncated
	state.StderrTruncated = stderrSnapshot.Truncated
	state.StdoutBytes = stdoutSnapshot.TotalBytes
	state.StderrBytes = stderrSnapshot.TotalBytes

	redactedResultPath := ""
	resultFileContent := []byte(nil)
	if invocation.ResultFile != "" {
		redactedResultPath = filepath.Join(runtimeDir, "executor-result-redacted.log")
		if err := redactFileToPath(invocation.ResultFile, redactedResultPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			outputErr = errors.Join(outputErr, err)
		} else if err == nil {
			resultFileContent, _, _ = readFileTail(redactedResultPath, WorkflowRunnerCaptureLimitBytes)
		}
	}
	normalizedInput := result.Stdout
	if len(resultFileContent) > 0 {
		normalizedInput = string(resultFileContent)
	} else if normalizedInput == "" {
		normalizedInput = result.Stderr
	}
	normalized := adapter.NormalizeResult(normalizedInput)
	state.ExitCode = result.ExitCode
	state.TimedOut = result.TimedOut
	state.TerminationVerified = result.TerminationVerified
	state.Error = redactSensitive(strings.TrimSpace(result.Error))
	state.NormalizedStatus = string(normalized.Status)
	state.BlockerText = redactSensitive(normalized.BlockerText)
	state.LaunchDisposition = string(result.LaunchDisposition)

	status := workflowstore.AttemptStatusFailed
	switch {
	case cancellationRequested(s.store, attempt.AttemptID) || errors.Is(ctx.Err(), context.Canceled):
		status = workflowstore.AttemptStatusCancelled
	case result.TimedOut:
		status = workflowstore.AttemptStatusTimedOut
	case result.ExitCode == 0 && normalized.Status == pipeline.AgentResultDone:
		status = workflowstore.AttemptStatusSucceeded
	}
	if normalized.ParseError != "" {
		state.Error = appendWorkflowError(state.Error, redactSensitive(normalized.ParseError))
	}
	if result.TerminationError != "" {
		state.Error = appendWorkflowError(state.Error, redactSensitive(result.TerminationError))
	}
	if result.ReleaseError != "" {
		state.Error = appendWorkflowError(state.Error, "release process: "+redactSensitive(result.ReleaseError))
	}
	if outputErr != nil {
		status = workflowstore.AttemptStatusFailed
		state.Error = appendWorkflowError(state.Error, "capture executor output: "+redactSensitive(outputErr.Error()))
	}

	if result.LaunchStarted && !result.TerminationVerified {
		state.CleanupPending = true
		state.PendingTerminalStatus = status
	}
	resultJSON, _ := json.Marshal(state)
	if err := s.persistAttemptEvidence(
		attempt,
		invocation,
		stdoutCapture.Path(),
		stderrCapture.Path(),
		redactSensitive(normalized.ExecutorResultText),
		redactedResultPath,
		resultJSON,
	); err != nil {
		status = workflowstore.AttemptStatusFailed
		state.Error = appendWorkflowError(state.Error, "persist attempt evidence: "+redactSensitive(err.Error()))
		resultJSON, _ = json.Marshal(state)
	}
	if state.CleanupPending {
		state.PendingTerminalStatus = status
		resultJSON, _ = json.Marshal(state)
		if _, err := s.runs.UpdateExecutionAttemptResult(context.Background(), attempt.AttemptID, string(resultJSON)); err != nil && s.log != nil {
			s.log.Error("record workflow execution cleanup pending", "run_id", run.RunID, "attempt_id", attempt.AttemptID, "error", err)
		}
		return
	}
	if _, err := s.runs.FinishExecutionAttempt(context.Background(), workflowruns.FinishExecutionAttemptInput{
		AttemptID:  attempt.AttemptID,
		Status:     status,
		ResultJSON: string(resultJSON),
	}); err != nil && s.log != nil {
		s.log.Error("finish workflow execution attempt", "run_id", run.RunID, "attempt_id", attempt.AttemptID, "error", err)
	}
	_ = repository
}

func (s *WorkflowExecutionService) finishPrelaunchFailure(attempt workflowstore.ExecutionAttempt, selected *effectiveBriefInput, message string) {
	state := workflowAttemptRuntime{}
	if current, err := s.store.GetExecutionAttemptByAttemptID(context.Background(), attempt.AttemptID); err == nil && strings.TrimSpace(current.ResultJSON) != "" {
		_ = json.Unmarshal([]byte(current.ResultJSON), &state)
	}
	if selected != nil {
		state.EffectiveBriefArtifactID = selected.Artifact.ArtifactID
		state.EffectiveBriefSHA256 = selected.Artifact.SHA256
		state.EffectiveBriefMode = string(selected.Mode)
	}
	state.TerminationVerified = true
	state.Error = redactSensitive(message)
	resultJSON, _ := json.Marshal(state)
	if _, err := s.runs.FinishExecutionAttempt(context.Background(), workflowruns.FinishExecutionAttemptInput{
		AttemptID:  attempt.AttemptID,
		Status:     workflowstore.AttemptStatusFailed,
		ResultJSON: string(resultJSON),
	}); err != nil && s.log != nil {
		s.log.Error("finish prelaunch workflow execution failure", "attempt_id", attempt.AttemptID, "error", err)
	}
}

func (s *WorkflowExecutionService) persistAttemptEvidence(
	attempt workflowstore.ExecutionAttempt,
	invocation ExecutorInvocation,
	stdoutPath, stderrPath, normalized, resultFilePath string,
	resultJSON []byte,
) error {
	batch, err := s.store.ArtifactStore().Begin("attempts/" + attempt.AttemptID)
	if err != nil {
		return err
	}
	type pendingArtifact struct {
		file workflowartifacts.File
	}
	var staged []pendingArtifact
	stage := func(kind, filename, mediaType string, data []byte) error {
		if len(data) == 0 {
			return nil
		}
		file, err := batch.Stage(kind, filename, mediaType, data)
		if err != nil {
			return err
		}
		staged = append(staged, pendingArtifact{file: file})
		return nil
	}
	stageFile := func(kind, filename, mediaType, sourcePath string) error {
		if strings.TrimSpace(sourcePath) == "" {
			return nil
		}
		info, err := os.Stat(sourcePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Size() == 0 {
			return nil
		}
		file, err := batch.StageFile(kind, filename, mediaType, sourcePath)
		if err != nil {
			return err
		}
		staged = append(staged, pendingArtifact{file: file})
		return nil
	}
	commandLog := []byte(fmt.Sprintf("Command: %s\nWorkDir: %s\nModel: %s\nAdapter: %s\n", invocation.Preview, invocation.WorkDir, invocation.Model, invocation.Adapter))
	if err := stageFile("executor_stdout", "stdout.log", "text/plain", stdoutPath); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stageFile("executor_stderr", "stderr.log", "text/plain", stderrPath); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stage("command_log", "command.log", "text/plain", redactSensitiveBytes(commandLog)); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stage("executor_result", "executor-result.txt", "text/plain", []byte(redactSensitive(normalized))); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stageFile("codex_last_message", "codex-last-message.txt", "text/plain", resultFilePath); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stage("execution_evidence", "execution-evidence.json", "application/json", resultJSON); err != nil {
		_ = batch.Rollback()
		return err
	}
	return s.store.CommitArtifactBatch(context.Background(), batch, func(tx *workflowstore.Tx) error {
		for _, pending := range staged {
			if _, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
				ArtifactID:            workflowstore.NewArtifactID(),
				OwnerType:             workflowstore.ArtifactOwnerExecutionAttempt,
				ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true},
				Kind:                  pending.file.Kind,
				RelativePath:          pending.file.RelativePath,
				MediaType:             pending.file.MediaType,
				SHA256:                pending.file.SHA256,
				SizeBytes:             pending.file.SizeBytes,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *WorkflowExecutionService) loadVerifiedBrief(ctx context.Context, run workflowstore.Run) ([]byte, workflowstore.Artifact, string, error) {
	artifacts, err := s.store.ListArtifactsByRun(ctx, run.ID)
	if err != nil {
		return nil, workflowstore.Artifact{}, "", err
	}
	var briefArtifact workflowstore.Artifact
	found := false
	for _, artifact := range artifacts {
		if artifact.Kind == "executor_brief" {
			if found {
				return nil, workflowstore.Artifact{}, "", fmt.Errorf("Run has multiple executor_brief artifacts")
			}
			briefArtifact = artifact
			found = true
		}
	}
	if !found {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("Run executor_brief artifact is missing")
	}
	root := s.store.ArtifactStore().Root()
	absolute := filepath.Clean(filepath.Join(root, filepath.FromSlash(briefArtifact.RelativePath)))
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("Run executor_brief artifact path is invalid")
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("read Run executor_brief artifact: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != briefArtifact.SHA256 || int64(len(data)) != briefArtifact.SizeBytes {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("Run executor_brief artifact integrity check failed")
	}
	return data, briefArtifact, absolute, nil
}

func verifyInvocationUsesBrief(invocation ExecutorInvocation, brief []byte, briefPath string) error {
	digest := sha256.Sum256(brief)
	return verifyInvocationUsesEffectiveBrief(invocation, effectiveBriefInput{
		Mode:    "full",
		Content: append([]byte(nil), brief...),
		Artifact: workflowstore.Artifact{
			ArtifactID: "verification",
			SHA256:     hex.EncodeToString(digest[:]),
			SizeBytes:  int64(len(brief)),
		},
		Path: briefPath,
	})
}

func cancellationRequested(store *workflowstore.Store, attemptID string) bool {
	attempt, err := store.GetExecutionAttemptByAttemptID(context.Background(), attemptID)
	return err == nil && attempt.CancellationRequestedAt.Valid
}

func terminalAttemptStatus(status string) bool {
	switch status {
	case workflowstore.AttemptStatusSucceeded, workflowstore.AttemptStatusFailed, workflowstore.AttemptStatusCancelled, workflowstore.AttemptStatusTimedOut:
		return true
	default:
		return false
	}
}

func appendWorkflowError(base, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return extra
	}
	return base + "; " + extra
}

func (s *WorkflowExecutionService) putRuntime(attemptID string, runtime *workflowRuntime) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[attemptID] = runtime
}

func (s *WorkflowExecutionService) getRuntime(attemptID string) *workflowRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[attemptID]
}

func (s *WorkflowExecutionService) deleteRuntime(attemptID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, attemptID)
}
