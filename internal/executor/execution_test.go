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
		StdinBytes:  len([]byte(request.BriefContent)),
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
	store    *workflowstore.Store
	service  *Execution
	run      workflowstore.Run
	adapter  *captureAdapter
	repoPath string
}

func newWorkflowFixture(t *testing.T) *workflowFixture {
	t.Helper()
	packageFixture := newExecutionAssignmentFixture(t, false, "")
	if err := os.WriteFile(filepath.Join(packageFixture.repoPath, "source.txt"), []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := &captureAdapter{id: AdapterOpenCodeGo}
	service, err := NewExecution(packageFixture.store, nil, "relay-test", packageFixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		return workflowrepos.ExecutionPreflightResult{OK: true}
	}
	service.adapterFactory = func(value string) (ExecutorAdapter, error) {
		adapter.id = AdapterID(value)
		return adapter, nil
	}
	service.invocationPreflight = func(ExecutorInvocation) ExecutorPreflightResult {
		return ExecutorPreflightResult{OK: true}
	}
	service.launch = func(fn func()) { fn() }
	return &workflowFixture{store: packageFixture.store, service: service, run: packageFixture.run, adapter: adapter, repoPath: packageFixture.repoPath}
}

// newLegacyWorkflowFixture seeds an unlinked historical Run for retirement checks only.
func newLegacyWorkflowFixture(t *testing.T) *workflowFixture {
	t.Helper()
	fixture := newWorkflowFixture(t)
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		legacy, err := tx.CreateRun(context.Background(), workflowstore.CreateRunParams{
			RunID:       "run-legacy-execution",
			FeatureSlug: "legacy-execution-test",
			RepoTarget:  "relay",
			Status:      workflowstore.RunStatusCreated,
			Branch:      "main",
			BaseCommit:  strings.Repeat("a", 40),
		})
		if err == nil {
			legacy, err = tx.TransitionRun(context.Background(), legacy.RunID, workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady)
			fixture.run = legacy
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func successfulRunner(_ context.Context, _ string, _ string, _ []string, stdin string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
	identity := pipeline.ProcessIdentity{PID: 101, StartedAt: "1", Platform: "linux"}
	if callbacks.OnProcessStarted != nil {
		_ = callbacks.OnProcessStarted(identity)
	}
	output := "STATUS: DONE\n\n## Validation\n\n- `go test ./internal/executor` - passed\n"
	if callbacks.OnStdout != nil {
		callbacks.OnStdout([]byte(output))
	}
	return pipeline.AgentCommandRunResult{
		ExitCode:            0,
		Stdout:              output,
		StartedAt:           time.Now(),
		FinishedAt:          time.Now(),
		LaunchDisposition:   pipeline.AgentLaunchOwned,
		ProcessIdentity:     identity,
		IdentityAvailable:   true,
		TerminationVerified: true,
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
