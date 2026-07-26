package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"relay/internal/pipeline"
	workflowstore "relay/internal/store/workflow"
)

type preparedLaunchAdapter struct {
	id       AdapterID
	buildErr error
	mutate   func(*ExecutorInvocation, ExecutorAdapterRequest)
	mu       sync.Mutex
	requests []ExecutorAdapterRequest
}

func (a *preparedLaunchAdapter) ID() AdapterID { return a.id }

func (a *preparedLaunchAdapter) BuildInvocation(request ExecutorAdapterRequest) (ExecutorInvocation, error) {
	a.mu.Lock()
	a.requests = append(a.requests, request)
	a.mu.Unlock()
	if a.buildErr != nil {
		return ExecutorInvocation{}, a.buildErr
	}
	invocation := ExecutorInvocation{
		Adapter:     a.id,
		Binary:      "fake-agent",
		WorkDir:     request.RepoPath,
		Stdin:       request.BriefContent,
		StdinSource: request.BriefPath,
		StdinBytes:  len([]byte(request.BriefContent)),
		Model:       request.SelectedModel,
		Preview:     "fake-agent < " + request.BriefPath,
	}
	if a.mutate != nil {
		a.mutate(&invocation, request)
	}
	return invocation, nil
}

func (a *preparedLaunchAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	if strings.Contains(raw, "STATUS: DONE") {
		return NormalizedExecutorResult{Status: pipeline.AgentResultDone, ExecutorResultText: raw}
	}
	return NormalizedExecutorResult{Status: pipeline.AgentResultBlocked, ExecutorResultText: raw, BlockerText: "blocked"}
}

func newPreparedLaunchService(t *testing.T, fixture *executionAssignmentFixture, adapter *preparedLaunchAdapter) *WorkflowExecutionService {
	t.Helper()
	service := NewWorkflowExecutionService(fixture.store, nil, "prepared-launch-test")
	service.adapterFactory = func(string) (ExecutorAdapter, error) { return adapter, nil }
	service.invocationPreflight = func(ExecutorInvocation) ExecutorPreflightResult { return ExecutorPreflightResult{OK: true} }
	service.launch = func(fn func()) { fn() }
	service.runner = successfulRunner
	return service
}

func prepareLaunchFixture(t *testing.T, outcome DeterministicOutcomeInput) (*executionAssignmentFixture, *AdaptiveExecutionAttemptResult) {
	t.Helper()
	fixture := adaptiveAttemptFixture(t, outcome.Application != nil || outcome.Preflight.Coverage != "", outcome.Preflight.Coverage, outcome)
	prepared, err := newAdaptiveAttemptService(t, fixture).Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "prepared-model"})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, &prepared
}

func TestLaunchPreparedAdaptiveModesPreservePackageMode(t *testing.T) {
	cases := []struct {
		name    string
		outcome DeterministicOutcomeInput
		mode    EffectiveExecutorBriefMode
	}{
		{name: "operations absent", outcome: DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}}, mode: EffectiveExecutorBriefAdaptiveNoOperations},
		{name: "partial preflight failure", outcome: failedOutcomeInput("partial"), mode: EffectiveExecutorBriefAdaptivePreflightFailed},
		{name: "complete preflight failure", outcome: failedOutcomeInput("complete"), mode: EffectiveExecutorBriefAdaptivePreflightFailed},
		{name: "partial deterministic application", outcome: appliedOutcomeInput("partial"), mode: EffectiveExecutorBriefAdaptiveAfterPartialApplication},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture, prepared := prepareLaunchFixture(t, tc.outcome)
			adapter := &preparedLaunchAdapter{id: AdapterCodex}
			service := newPreparedLaunchService(t, fixture, adapter)
			result, err := service.LaunchPreparedAdaptive(context.Background(), PreparedAdaptiveLaunchInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
			if err != nil || !result.NewlyAdmitted || !result.NewlyLaunched || result.Mode != tc.mode {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), prepared.Attempt.AttemptID)
			if err != nil || attempt.Status != workflowstore.AttemptStatusSucceeded {
				t.Fatalf("attempt=%#v err=%v", attempt, err)
			}
			var runtime workflowAttemptRuntime
			if err := json.Unmarshal([]byte(attempt.ResultJSON), &runtime); err != nil {
				t.Fatal(err)
			}
			if runtime.EffectiveBriefMode != string(tc.mode) || runtime.SourceMutationStarted != true || runtime.EffectiveBriefArtifactID == "" || runtime.EffectiveBriefSHA256 == "" {
				t.Fatalf("runtime=%#v", runtime)
			}
			brief, err := NewEffectiveExecutorBriefService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			effective, err := brief.Load(context.Background(), fixture.run.RunID)
			if err != nil || effective.Artifact == nil {
				t.Fatalf("effective brief=%#v err=%v", effective, err)
			}
			adapter.mu.Lock()
			request := adapter.requests[0]
			adapter.mu.Unlock()
			if request.BriefContent != string(effective.Bytes) || request.SelectedModel != "prepared-model" || request.BriefPath != filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(effective.Artifact.RelativePath)) || request.RepoPath != "C:/relay" || !strings.HasSuffix(request.ResultPath, filepath.Join(fixture.run.RunID, prepared.Attempt.AttemptID, "executor-result.tmp")) {
				t.Fatalf("adapter request=%#v", request)
			}
			artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(artifacts) < 1 {
				t.Fatal("successful package execution did not persist evidence")
			}
		})
	}
}

func TestLaunchPreparedAdaptiveCompleteDoesNoLaunchWork(t *testing.T) {
	fixture := adaptiveAttemptFixture(t, true, "complete", appliedOutcomeInput("complete"))
	adapterCalls, launchCalls := 0, 0
	service := NewWorkflowExecutionService(fixture.store, nil, "prepared-launch-test")
	service.adapterFactory = func(string) (ExecutorAdapter, error) {
		adapterCalls++
		return &preparedLaunchAdapter{id: AdapterCodex}, nil
	}
	service.launch = func(func()) { launchCalls++ }
	result, err := service.LaunchPreparedAdaptive(context.Background(), PreparedAdaptiveLaunchInput{RunID: fixture.run.RunID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != EffectiveExecutorBriefDeterministicComplete || result.AdaptiveDispatchRequired || result.NewlyAdmitted || result.NewlyLaunched || result.Run != nil || result.Attempt != nil || result.Lease != nil {
		t.Fatalf("result=%#v", result)
	}
	if adapterCalls != 0 || launchCalls != 0 {
		t.Fatalf("adapter calls=%d launch calls=%d", adapterCalls, launchCalls)
	}
}

func TestLaunchPreparedAdaptiveReadbackDoesNotDuplicateProcess(t *testing.T) {
	fixture, prepared := prepareLaunchFixture(t, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	adapter := &preparedLaunchAdapter{id: AdapterCodex}
	service := newPreparedLaunchService(t, fixture, adapter)
	launchEntered := make(chan struct{})
	continueLaunch := make(chan struct{})
	service.launch = func(fn func()) {
		close(launchEntered)
		<-continueLaunch
		fn()
	}
	firstResult := make(chan PreparedAdaptiveLaunchResult, 1)
	firstErr := make(chan error, 1)
	go func() {
		result, err := service.LaunchPreparedAdaptive(context.Background(), PreparedAdaptiveLaunchInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
		firstResult <- result
		firstErr <- err
	}()
	<-launchEntered
	second, err := service.LaunchPreparedAdaptive(context.Background(), PreparedAdaptiveLaunchInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
	if err != nil || second.NewlyAdmitted || second.NewlyLaunched || second.Attempt == nil || second.Lease == nil {
		t.Fatalf("readback=%#v err=%v", second, err)
	}
	close(continueLaunch)
	first := <-firstResult
	if err := <-firstErr; err != nil || !first.NewlyAdmitted || !first.NewlyLaunched {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	if first.Attempt.ID != second.Attempt.ID || first.Lease.LeaseID != second.Lease.LeaseID {
		t.Fatalf("identity mismatch first=%#v second=%#v", first, second)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%#v err=%v", attempts, err)
	}
	adapter.mu.Lock()
	requestCount := len(adapter.requests)
	adapter.mu.Unlock()
	if requestCount != 1 {
		t.Fatalf("adapter request count=%d", requestCount)
	}
}

func TestLaunchPreparedAdaptivePreflightFailureSettlesAttemptAndLease(t *testing.T) {
	fixture, prepared := prepareLaunchFixture(t, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	adapter := &preparedLaunchAdapter{id: AdapterCodex}
	service := newPreparedLaunchService(t, fixture, adapter)
	preflightCalls, launchCalls := 0, 0
	service.invocationPreflight = func(ExecutorInvocation) ExecutorPreflightResult {
		preflightCalls++
		return ExecutorPreflightResult{BlockerText: "blocked"}
	}
	service.launch = func(func()) { launchCalls++ }
	result, err := service.LaunchPreparedAdaptive(context.Background(), PreparedAdaptiveLaunchInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
	if err == nil || result.NewlyLaunched || preflightCalls != 1 || launchCalls != 0 {
		t.Fatalf("result=%#v err=%v preflight=%d launch=%d", result, err, preflightCalls, launchCalls)
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), prepared.Attempt.AttemptID)
	if err != nil || attempt.Status != workflowstore.AttemptStatusFailed {
		t.Fatalf("attempt=%#v err=%v", attempt, err)
	}
	var state workflowAttemptRuntime
	if err := json.Unmarshal([]byte(attempt.ResultJSON), &state); err != nil {
		t.Fatal(err)
	}
	if state.SourceMutationStarted || !state.TerminationVerified || state.MutationLeaseID == "" || state.EffectiveBriefMode != string(EffectiveExecutorBriefAdaptiveNoOperations) {
		t.Fatalf("settled state=%#v", state)
	}
	run, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
	if err != nil || run.Status != workflowstore.RunStatusExecutionFailed {
		t.Fatalf("run=%#v err=%v", run, err)
	}
	if _, err := service.runs.GetActiveRunMutationLease(context.Background(), fixture.run.RunID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("active lease err=%v", err)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.Kind != adaptiveExecutionInputKind {
			t.Fatalf("unexpected prelaunch output artifact=%#v", artifact)
		}
	}
}

func TestLaunchPreparedAdaptiveRejectsChangedInvocationIdentity(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ExecutorInvocation, ExecutorAdapterRequest)
	}{
		{name: "adapter", mutate: func(invocation *ExecutorInvocation, _ ExecutorAdapterRequest) { invocation.Adapter = AdapterOpenCodeGo }},
		{name: "model", mutate: func(invocation *ExecutorInvocation, _ ExecutorAdapterRequest) { invocation.Model = "other-model" }},
		{name: "brief bytes", mutate: func(invocation *ExecutorInvocation, _ ExecutorAdapterRequest) {
			invocation.Stdin += "tampered"
			invocation.StdinBytes = len([]byte(invocation.Stdin))
		}},
		{name: "brief path", mutate: func(invocation *ExecutorInvocation, _ ExecutorAdapterRequest) {
			invocation.StdinSource = "alternate-brief.md"
		}},
		{name: "workdir", mutate: func(invocation *ExecutorInvocation, _ ExecutorAdapterRequest) { invocation.WorkDir = "C:/alternate" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture, prepared := prepareLaunchFixture(t, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
			adapter := &preparedLaunchAdapter{id: AdapterCodex, mutate: tc.mutate}
			service := newPreparedLaunchService(t, fixture, adapter)
			preflightCalls, launchCalls := 0, 0
			service.invocationPreflight = func(ExecutorInvocation) ExecutorPreflightResult {
				preflightCalls++
				return ExecutorPreflightResult{OK: true}
			}
			service.launch = func(func()) { launchCalls++ }
			_, err := service.LaunchPreparedAdaptive(context.Background(), PreparedAdaptiveLaunchInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
			if err == nil || preflightCalls != 0 || launchCalls != 0 {
				t.Fatalf("err=%v preflight=%d launch=%d", err, preflightCalls, launchCalls)
			}
			attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), prepared.Attempt.AttemptID)
			if err != nil || attempt.Status != workflowstore.AttemptStatusFailed {
				t.Fatalf("attempt=%#v err=%v", attempt, err)
			}
		})
	}
}

func TestPreparedAdaptiveProcessStart(t *testing.T) {
	fixture, prepared := prepareLaunchFixture(t, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	service := NewWorkflowExecutionService(fixture.store, nil, "prepared-launch-test")
	admission, err := service.adaptiveAdmission.Begin(context.Background(), AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
	if err != nil {
		t.Fatal(err)
	}
	data := `{"process_identity":"pid:7"}`
	updated, err := service.recordProcessStart(context.Background(), admission.Attempt.AttemptID, data)
	if err != nil || updated.Status != workflowstore.AttemptStatusRunning || updated.ResultJSON != data {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
}

func TestLaunchPreparedAdaptiveTerminalizationFailureRetainsLease(t *testing.T) {
	fixture, prepared := prepareLaunchFixture(t, DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_prepared_adaptive_terminalization BEFORE UPDATE OF status ON execution_attempts WHEN NEW.status = 'failed' BEGIN SELECT RAISE(ABORT, 'injected terminalization failure'); END`); err != nil {
		t.Fatal(err)
	}
	service := NewWorkflowExecutionService(fixture.store, nil, "prepared-launch-test")
	service.adapterFactory = func(string) (ExecutorAdapter, error) { return nil, fmt.Errorf("factory failure") }
	result, err := service.LaunchPreparedAdaptive(context.Background(), PreparedAdaptiveLaunchInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
	if err == nil || !strings.Contains(err.Error(), "terminalize admitted attempt") || result.NewlyLaunched {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := service.runs.GetActiveRunMutationLease(context.Background(), fixture.run.RunID); err != nil {
		t.Fatalf("lease was not retained after terminalization failure: %v", err)
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), prepared.Attempt.AttemptID)
	if err != nil || attempt.Status != workflowstore.AttemptStatusRunning {
		t.Fatalf("attempt=%#v err=%v", attempt, err)
	}
}
