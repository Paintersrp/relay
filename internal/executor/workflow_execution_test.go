package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/pipeline"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

type captureAdapter struct {
	id    AdapterID
	mu    sync.Mutex
	brief string
	model string
}

func (a *captureAdapter) ID() AdapterID { return a.id }

func (a *captureAdapter) BuildInvocation(request ExecutorAdapterRequest) (ExecutorInvocation, error) {
	a.mu.Lock()
	a.brief = request.BriefContent
	a.model = request.SelectedModel
	a.mu.Unlock()
	return ExecutorInvocation{
		Adapter:     a.id,
		Binary:      "fake-agent",
		WorkDir:     request.RepoPath,
		Stdin:       request.BriefContent,
		StdinSource: request.BriefPath,
		Model:       request.SelectedModel,
		Agent:       string(a.id),
		Preview:     "fake-agent < " + request.BriefPath,
	}, nil
}

func (a *captureAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	if strings.Contains(raw, "STATUS: DONE") {
		return NormalizedExecutorResult{Status: pipeline.AgentResultDone, ExecutorResultText: raw}
	}
	return NormalizedExecutorResult{Status: pipeline.AgentResultBlocked, ExecutorResultText: raw, BlockerText: "blocked"}
}

type workflowFixture struct {
	store   *workflowstore.Store
	runs    *workflowruns.Service
	service *WorkflowExecutionService
	run     workflowstore.Run
	brief   []byte
	adapter *captureAdapter
}

func newWorkflowFixture(t *testing.T) *workflowFixture {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(context.Background(), "relay", repoPath); err != nil {
		t.Fatal(err)
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	brief := []byte("# Executor Brief\n\nUse the exact approved task.\n")
	created, err := runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      "workflow-execution-test",
		RepoTarget:       "relay",
		Branch:           "feat/simplification",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    []byte(`{"test":true}`),
		RenderedMarkdown: brief,
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &captureAdapter{id: AdapterOpenCodeGo}
	service := NewWorkflowExecutionService(store, nil, "relay-test")
	service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		return workflowrepos.ExecutionPreflightResult{OK: true}
	}
	service.adapterFactory = func(string) (ExecutorAdapter, error) { return adapter, nil }
	service.invocationPreflight = func(ExecutorInvocation) ExecutorPreflightResult {
		return ExecutorPreflightResult{OK: true}
	}
	service.launch = func(fn func()) { fn() }
	return &workflowFixture{store: store, runs: runs, service: service, run: created.Run, brief: brief, adapter: adapter}
}

func successfulRunner(_ context.Context, _ string, _ string, _ []string, stdin string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
	identity := pipeline.ProcessIdentity{PID: 101, StartedAt: "1", Platform: "linux"}
	if callbacks.OnProcessStarted != nil {
		_ = callbacks.OnProcessStarted(identity)
	}
	if callbacks.OnStdout != nil {
		callbacks.OnStdout([]byte("STATUS: DONE\n"))
	}
	return pipeline.AgentCommandRunResult{
		ExitCode:            0,
		Stdout:              "STATUS: DONE\n",
		StartedAt:           time.Now(),
		FinishedAt:          time.Now(),
		LaunchDisposition:   pipeline.AgentLaunchOwned,
		ProcessIdentity:     identity,
		IdentityAvailable:   true,
		TerminationVerified: true,
	}
}

func TestWorkflowStartBlocksBeforeAttemptCreation(t *testing.T) {
	fixture := newWorkflowFixture(t)
	artifactsBefore, err := fixture.store.ListArtifactsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		return workflowrepos.ExecutionPreflightResult{OK: false, BlockerCode: "repository_dirty", BlockerText: "repository is dirty"}
	}
	_, err = fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID:   fixture.run.RunID,
		Adapter: "opencode_go",
		Model:   "test-model",
	})
	if err == nil {
		t.Fatal("expected preflight blocker")
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("attempts = %d, want 0", len(attempts))
	}
	artifactsAfter, err := fixture.store.ListArtifactsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifactsAfter) != len(artifactsBefore) {
		t.Fatalf("run artifacts changed from %d to %d", len(artifactsBefore), len(artifactsAfter))
	}
	current, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != workflowstore.RunStatusSetupReady {
		t.Fatalf("Run status = %q", current.Status)
	}
}

func TestWorkflowStartUsesExactBriefAndPersistsAttemptEvidence(t *testing.T) {
	fixture := newWorkflowFixture(t)
	fixture.service.runner = successfulRunner
	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID:   fixture.run.RunID,
		Adapter: "opencode_go",
		Model:   "attempt-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.Adapter != "opencode_go" || result.Attempt.Model != "attempt-model" {
		t.Fatalf("attempt identity = %+v", result.Attempt)
	}
	if fixture.adapter.brief != string(fixture.brief) || fixture.adapter.model != "attempt-model" {
		t.Fatal("adapter did not receive the exact rendered brief and attempt-local model")
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), result.Attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != workflowstore.AttemptStatusSucceeded {
		t.Fatalf("attempt status = %q", attempt.Status)
	}
	current, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != workflowstore.RunStatusValidating {
		t.Fatalf("Run status = %q, want validating", current.Status)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) < 3 {
		t.Fatalf("attempt artifacts = %d, want at least 3", len(artifacts))
	}
}

func TestWorkflowOperationalRetryStaysOnSameRun(t *testing.T) {
	fixture := newWorkflowFixture(t)
	fail := true
	fixture.service.runner = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, controller pipeline.ProcessController) pipeline.AgentCommandRunResult {
		if fail {
			if callbacks.OnProcessStarted != nil {
				_ = callbacks.OnProcessStarted(pipeline.ProcessIdentity{PID: 102, StartedAt: "1", Platform: "linux"})
			}
			if callbacks.OnStdout != nil {
				callbacks.OnStdout([]byte("STATUS: BLOCKED\n"))
			}
			return pipeline.AgentCommandRunResult{ExitCode: 1, Stdout: "STATUS: BLOCKED\n", StartedAt: time.Now(), FinishedAt: time.Now(), TerminationVerified: true}
		}
		return successfulRunner(ctx, workDir, binary, args, stdin, timeout, callbacks, controller)
	}
	first, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "first-model"})
	if err != nil {
		t.Fatal(err)
	}
	fail = false
	second, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "kiro_cli", Model: "second-model"})
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].AttemptID != first.Attempt.AttemptID || attempts[1].AttemptID != second.Attempt.AttemptID {
		t.Fatalf("attempt history = %+v", attempts)
	}
	if attempts[0].Status != workflowstore.AttemptStatusFailed || attempts[1].Status != workflowstore.AttemptStatusSucceeded {
		t.Fatalf("attempt statuses = %q, %q", attempts[0].Status, attempts[1].Status)
	}
	if attempts[0].Adapter != "codex" || attempts[1].Adapter != "kiro_cli" {
		t.Fatalf("attempt adapters were not preserved: %+v", attempts)
	}
}

func TestWorkflowTimeoutAndCancellationAreTerminal(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		fixture.service.runner = func(_ context.Context, _ string, _ string, _ []string, _ string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
			if callbacks.OnProcessStarted != nil {
				_ = callbacks.OnProcessStarted(pipeline.ProcessIdentity{PID: 103, StartedAt: "1", Platform: "linux"})
			}
			return pipeline.AgentCommandRunResult{ExitCode: -2, TimedOut: true, StartedAt: time.Now(), FinishedAt: time.Now(), TerminationVerified: true}
		}
		started, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "antigravity", Model: "timeout-model"})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), started.Attempt.AttemptID)
		if err != nil {
			t.Fatal(err)
		}
		if attempt.Status != workflowstore.AttemptStatusTimedOut {
			t.Fatalf("status = %q", attempt.Status)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		startedSignal := make(chan struct{})
		startFailed := make(chan error, 1)
		executionComplete := make(chan struct{})
		fixture.service.launch = func(fn func()) {
			go func() {
				defer close(executionComplete)
				fn()
			}()
		}
		fixture.service.runner = func(ctx context.Context, _ string, _ string, _ []string, _ string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
			if err := callbacks.OnProcessStarted(pipeline.ProcessIdentity{PID: 104, StartedAt: "1", Platform: "linux"}); err != nil {
				startFailed <- err
				now := time.Now()
				return pipeline.AgentCommandRunResult{ExitCode: -1, Error: err.Error(), StartedAt: now, FinishedAt: now, TerminationVerified: true}
			}
			close(startedSignal)
			<-ctx.Done()
			return pipeline.AgentCommandRunResult{ExitCode: -1, Error: ctx.Err().Error(), StartedAt: time.Now(), FinishedAt: time.Now(), TerminationVerified: true}
		}
		started, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "cancel-model"})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-startedSignal:
		case startErr := <-startFailed:
			t.Fatalf("persist executor process start: %v", startErr)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for executor process start")
		}
		if _, err := fixture.service.Cancel(context.Background(), fixture.run.RunID, started.Attempt.AttemptID); err != nil {
			t.Fatal(err)
		}
		select {
		case <-executionComplete:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for cancelled execution to finish")
		}
		attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), started.Attempt.AttemptID)
		if err != nil {
			t.Fatal(err)
		}
		if attempt.Status != workflowstore.AttemptStatusCancelled {
			t.Fatalf("attempt status = %q, want cancelled", attempt.Status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(attempt.ResultJSON), &result); err != nil {
			t.Fatal(err)
		}
		current, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != workflowstore.RunStatusCancelled {
			t.Fatalf("Run status = %q, want cancelled", current.Status)
		}
	})
}
