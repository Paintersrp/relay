package executor

import (
	"bytes"
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
}

type WorkflowCancelResult struct {
	Run     workflowstore.Run
	Attempt workflowstore.ExecutionAttempt
}

type WorkflowAttemptView struct {
	Attempt    workflowstore.ExecutionAttempt
	Artifacts  []workflowstore.Artifact
	LiveStdout string
	LiveStderr string
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
	stdout   bytes.Buffer
	stderr   bytes.Buffer
	identity pipeline.ProcessIdentity
}

func (r *workflowRuntime) appendStdout(chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.stdout.Write(chunk)
}

func (r *workflowRuntime) appendStderr(chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.stderr.Write(chunk)
}

func (r *workflowRuntime) snapshot() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stdout.String(), r.stderr.String()
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
	brief, briefArtifact, briefPath, err := s.loadVerifiedBrief(ctx, run)
	if err != nil {
		return WorkflowStartResult{}, err
	}
	preflight := s.preflight(ctx, repository.LocalPath, run.Branch, run.BaseCommit)
	if !preflight.OK {
		return WorkflowStartResult{Run: run, Preflight: preflight}, &WorkflowPreflightError{Result: preflight}
	}

	adapter, err := s.adapterFactory(normalizedAdapter)
	if err != nil {
		return WorkflowStartResult{}, err
	}
	runtimeResultPath := filepath.Join(s.store.ArtifactStore().Root(), ".runtime", run.RunID, "executor-result.tmp")
	invocation, err := adapter.BuildInvocation(ExecutorAdapterRequest{
		RunID:         run.ID,
		RepoPath:      repository.LocalPath,
		BriefContent:  string(brief),
		BriefPath:     briefPath,
		ResultPath:    runtimeResultPath,
		SelectedModel: input.Model,
		Timeout:       s.timeout,
	})
	if err != nil {
		return WorkflowStartResult{}, fmt.Errorf("build executor invocation: %w", err)
	}
	if err := verifyInvocationUsesBrief(invocation, brief, briefPath); err != nil {
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
		s.execute(runtimeCtx, begun.Run, begun.Attempt, repository, briefArtifact, invocation, adapter, runtime)
	})
	return WorkflowStartResult{Run: begun.Run, Attempt: begun.Attempt, Preflight: preflight}, nil
}

func (s *WorkflowExecutionService) Cancel(ctx context.Context, runID, attemptID string) (WorkflowCancelResult, error) {
	attempt, err := s.runs.RequestExecutionAttemptCancellation(ctx, attemptID)
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	if strings.TrimSpace(runID) != "" {
		run, err := s.store.GetRunByRunID(ctx, runID)
		if err != nil {
			return WorkflowCancelResult{}, err
		}
		if run.ID != attempt.RunRowID {
			return WorkflowCancelResult{}, fmt.Errorf("execution attempt does not belong to Run")
		}
	}
	if terminalAttemptStatus(attempt.Status) {
		run, err := s.store.GetRunByRowID(ctx, attempt.RunRowID)
		return WorkflowCancelResult{Run: run, Attempt: attempt}, err
	}

	if runtime := s.getRuntime(attempt.AttemptID); runtime != nil {
		runtime.cancel()
	} else {
		var runtimeState workflowAttemptRuntime
		_ = json.Unmarshal([]byte(attempt.ResultJSON), &runtimeState)
		if runtimeState.ProcessIdentity != "" {
			identity, decodeErr := pipeline.DecodeProcessIdentity(runtimeState.ProcessIdentity)
			if decodeErr != nil {
				return WorkflowCancelResult{}, fmt.Errorf("decode durable process identity: %w", decodeErr)
			}
			owned, openErr := s.controller.OpenOwned(identity)
			if openErr != nil {
				return WorkflowCancelResult{}, fmt.Errorf("open owned process: %w", openErr)
			}
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
			runtimeState.TerminationVerified = true
			runtimeState.Error = appendWorkflowError(runtimeState.Error, "operator cancellation requested")
			resultJSON, _ := json.Marshal(runtimeState)
			finished, finishErr := s.runs.FinishExecutionAttempt(ctx, workflowruns.FinishExecutionAttemptInput{
				AttemptID:  attempt.AttemptID,
				Status:     workflowstore.AttemptStatusCancelled,
				ResultJSON: string(resultJSON),
			})
			if finishErr != nil {
				return WorkflowCancelResult{}, finishErr
			}
			return WorkflowCancelResult{Run: finished.Run, Attempt: finished.Attempt}, nil
		}
		if attempt.Status == workflowstore.AttemptStatusPending {
			finished, finishErr := s.runs.FinishExecutionAttempt(ctx, workflowruns.FinishExecutionAttemptInput{
				AttemptID:  attempt.AttemptID,
				Status:     workflowstore.AttemptStatusCancelled,
				ResultJSON: `{"error":"operator cancellation requested before process start"}`,
			})
			if finishErr != nil {
				return WorkflowCancelResult{}, finishErr
			}
			return WorkflowCancelResult{Run: finished.Run, Attempt: finished.Attempt}, nil
		}
		return WorkflowCancelResult{}, fmt.Errorf("running execution attempt has no durable process identity")
	}

	refreshed, err := s.store.GetExecutionAttemptByAttemptID(ctx, attemptID)
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	run, err := s.store.GetRunByRowID(ctx, refreshed.RunRowID)
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	return WorkflowCancelResult{Run: run, Attempt: refreshed}, nil
}

func (s *WorkflowExecutionService) ListAttempts(ctx context.Context, runID string) ([]WorkflowAttemptView, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	attempts, err := s.store.ListExecutionAttemptsByRun(ctx, run.ID)
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
		view.LiveStdout, view.LiveStderr = runtime.snapshot()
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
	OwnerInstanceID     string `json:"owner_instance_id,omitempty"`
	CommandPreview      string `json:"command_preview,omitempty"`
	ProcessIdentity     string `json:"process_identity,omitempty"`
	LaunchDisposition   string `json:"launch_disposition,omitempty"`
	ExitCode            int    `json:"exit_code"`
	TimedOut            bool   `json:"timed_out"`
	TerminationVerified bool   `json:"termination_verified"`
	Error               string `json:"error,omitempty"`
	NormalizedStatus    string `json:"normalized_status,omitempty"`
	BlockerText         string `json:"blocker_text,omitempty"`
	BriefArtifactID     string `json:"brief_artifact_id,omitempty"`
	BriefSHA256         string `json:"brief_sha256,omitempty"`
}

func (s *WorkflowExecutionService) execute(
	ctx context.Context,
	run workflowstore.Run,
	attempt workflowstore.ExecutionAttempt,
	repository workflowstore.RepositoryTarget,
	briefArtifact workflowstore.Artifact,
	invocation ExecutorInvocation,
	adapter ExecutorAdapter,
	runtime *workflowRuntime,
) {
	state := workflowAttemptRuntime{
		OwnerInstanceID: s.ownerInstanceID,
		CommandPreview:  invocation.Preview,
		BriefArtifactID: briefArtifact.ArtifactID,
		BriefSHA256:     briefArtifact.SHA256,
	}
	updateState := func() {
		data, _ := json.Marshal(state)
		_, _ = s.runs.UpdateExecutionAttemptResult(context.Background(), attempt.AttemptID, string(data))
	}
	updateState()

	if invocation.ResultFile != "" {
		if err := os.MkdirAll(filepath.Dir(invocation.ResultFile), 0o700); err != nil {
			failedJSON, _ := json.Marshal(workflowAttemptRuntime{Error: "prepare result path: " + err.Error()})
			_, _ = s.runs.FinishExecutionAttempt(context.Background(), workflowruns.FinishExecutionAttemptInput{
				AttemptID:  attempt.AttemptID,
				Status:     workflowstore.AttemptStatusFailed,
				ResultJSON: string(failedJSON),
			})
			return
		}
		defer func() {
			_ = os.Remove(invocation.ResultFile)
			_ = os.Remove(filepath.Dir(invocation.ResultFile))
		}()
	}

	result := s.runner(
		ctx,
		invocation.WorkDir,
		invocation.Binary,
		invocation.Args,
		invocation.Stdin,
		s.timeout,
		pipeline.AgentCommandStreamCallbacks{
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
	stdout, stderr := runtime.snapshot()
	resultFileContent := []byte(nil)
	if invocation.ResultFile != "" {
		resultFileContent, _ = os.ReadFile(invocation.ResultFile)
	}
	normalizedInput := stdout
	if len(resultFileContent) > 0 {
		normalizedInput = string(resultFileContent)
	} else if normalizedInput == "" {
		normalizedInput = stderr
	}
	normalized := adapter.NormalizeResult(normalizedInput)
	state.ExitCode = result.ExitCode
	state.TimedOut = result.TimedOut
	state.TerminationVerified = result.TerminationVerified
	state.Error = strings.TrimSpace(result.Error)
	state.NormalizedStatus = string(normalized.Status)
	state.BlockerText = normalized.BlockerText
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
		state.Error = appendWorkflowError(state.Error, normalized.ParseError)
	}
	if result.TerminationError != "" {
		state.Error = appendWorkflowError(state.Error, result.TerminationError)
	}
	if result.ReleaseError != "" {
		state.Error = appendWorkflowError(state.Error, "release process: "+result.ReleaseError)
	}

	resultJSON, _ := json.Marshal(state)
	if err := s.persistAttemptEvidence(
		attempt,
		invocation,
		stdout,
		stderr,
		normalized.ExecutorResultText,
		resultFileContent,
		resultJSON,
	); err != nil {
		status = workflowstore.AttemptStatusFailed
		state.Error = appendWorkflowError(state.Error, "persist attempt evidence: "+err.Error())
		resultJSON, _ = json.Marshal(state)
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

func (s *WorkflowExecutionService) persistAttemptEvidence(
	attempt workflowstore.ExecutionAttempt,
	invocation ExecutorInvocation,
	stdout, stderr, normalized string,
	resultFileContent, resultJSON []byte,
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
	commandLog := []byte(fmt.Sprintf("Command: %s\nWorkDir: %s\nModel: %s\nAdapter: %s\n", invocation.Preview, invocation.WorkDir, invocation.Model, invocation.Adapter))
	if err := stage("executor_stdout", "stdout.log", "text/plain", []byte(stdout)); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stage("executor_stderr", "stderr.log", "text/plain", []byte(stderr)); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stage("command_log", "command.log", "text/plain", commandLog); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stage("executor_result", "executor-result.txt", "text/plain", []byte(normalized)); err != nil {
		_ = batch.Rollback()
		return err
	}
	if err := stage("codex_last_message", "codex-last-message.txt", "text/plain", resultFileContent); err != nil {
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
	if invocation.Stdin != "" {
		if !bytes.Equal([]byte(invocation.Stdin), brief) {
			return fmt.Errorf("executor invocation changed the rendered Executor Brief bytes")
		}
		return nil
	}
	for _, arg := range invocation.Args {
		if arg == briefPath {
			return nil
		}
	}
	return fmt.Errorf("executor invocation does not reference the rendered Executor Brief")
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
