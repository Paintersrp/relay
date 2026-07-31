package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"relay/internal/applier"
	"relay/internal/pipeline"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowStartPackageConstructorInitializesPreparation(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if service.packagePreparation == nil {
		t.Fatal("package preparation service is nil")
	}
}

func TestWorkflowStartPackageRoutesPackageRun(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	prepared := PackagePreparationResult{Run: fixture.run}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			return PackageWorkflowDispatchResult{Preparation: prepared}, nil
		},
	)

	result, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Package == nil {
		t.Fatal("package result is nil")
	}
}

func TestWorkflowStartPackagePassesNormalizedInput(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	var got PackagePreparationInput
	prepared := PackagePreparationResult{Run: fixture.run}
	withPackageWorkflowStartSeams(t,
		func(_ context.Context, _ *PackagePreparation, input PackagePreparationInput) (PackagePreparationResult, error) {
			got = input
			return prepared, nil
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			return PackageWorkflowDispatchResult{Preparation: prepared}, nil
		},
	)

	if _, err := service.Start(context.Background(), WorkflowStartInput{
		RunID:   " " + fixture.run.RunID + " ",
		Adapter: " OPENCODE ",
		Model:   " model ",
	}); err != nil {
		t.Fatal(err)
	}
	if got.RunID != fixture.run.RunID || got.Adapter != string(AdapterOpenCodeGo) || got.Model != "model" {
		t.Fatalf("preparation input = %#v", got)
	}
}

func TestWorkflowStartPackageAdaptiveSuccessDispatchesOnceAndProjectsIdentity(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	attempt := packageStartAttempt(fixture.run, "prepared-attempt")
	prepared := PackagePreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
	launchRun := fixture.run
	launchRun.Status = workflowstore.RunStatusExecuting
	launchAttempt := attempt
	launchAttempt.AttemptID = "launched-attempt"
	dispatched := PackageWorkflowDispatchResult{
		Preparation: prepared,
		Launch:      PreparedAdaptiveLaunchResult{Run: &launchRun, Attempt: &launchAttempt},
	}
	dispatchCalls := 0
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			dispatchCalls++
			return dispatched, nil
		},
	)

	result, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if err != nil || dispatchCalls != 1 {
		t.Fatalf("err=%v dispatch calls=%d", err, dispatchCalls)
	}
	if result.Run != launchRun || result.Attempt != launchAttempt || result.Package == nil || !reflect.DeepEqual(*result.Package, dispatched) {
		t.Fatalf("result=%#v dispatched=%#v", result, dispatched)
	}
	if !reflect.DeepEqual(result.Preflight, workflowrepos.ExecutionPreflightResult{}) || result.Applier != nil {
		t.Fatalf("legacy fields were populated: preflight=%#v applier=%#v", result.Preflight, result.Applier)
	}
}

func TestWorkflowStartPackageAdaptiveReadbackRemainsSuccessful(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	attempt := packageStartAttempt(fixture.run, "readback-attempt")
	prepared := PackagePreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
	readbackRun := fixture.run
	readbackRun.Status = workflowstore.RunStatusExecuting
	dispatched := PackageWorkflowDispatchResult{
		Preparation: prepared,
		Launch:      PreparedAdaptiveLaunchResult{Run: &readbackRun, Attempt: &attempt, NewlyAdmitted: false, NewlyLaunched: false},
	}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			return dispatched, nil
		},
	)

	result, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if err != nil || result.Run != readbackRun || result.Attempt != attempt {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWorkflowStartPackageDeterministicCompleteProjectsFinalizedRunWithoutAttempt(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	prepared := PackagePreparationResult{Run: fixture.run}
	finalized := fixture.run
	finalized.Status = workflowstore.RunStatusValidating
	dispatched := PackageWorkflowDispatchResult{Preparation: prepared, FinalizedRun: &finalized}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			return dispatched, nil
		},
	)

	result, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if err != nil || result.Run != finalized || result.Attempt != (workflowstore.ExecutionAttempt{}) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWorkflowStartPackagePreparationFailurePreservesEvidenceAndPreventsDispatch(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	attempt := packageStartAttempt(fixture.run, "preparation-failure-attempt")
	prepared := PackagePreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
	prepareErr := errors.New("preparation failed")
	dispatchCalls := 0
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			return prepared, prepareErr
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			dispatchCalls++
			return PackageWorkflowDispatchResult{}, nil
		},
	)

	result, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if !errors.Is(err, prepareErr) || dispatchCalls != 0 || result.Package == nil || !reflect.DeepEqual(result.Package.Preparation, prepared) || result.Run != fixture.run || result.Attempt != attempt {
		t.Fatalf("result=%#v err=%v dispatch calls=%d", result, err, dispatchCalls)
	}
}

func TestWorkflowStartPackageDispatchFailurePreservesEvidence(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	attempt := packageStartAttempt(fixture.run, "dispatch-failure-attempt")
	prepared := PackagePreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
	launchRun := fixture.run
	launchRun.Status = workflowstore.RunStatusExecuting
	launch := PreparedAdaptiveLaunchResult{Run: &launchRun}
	dispatched := PackageWorkflowDispatchResult{Preparation: prepared, Launch: launch}
	dispatchErr := errors.New("dispatch failed")
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			return dispatched, dispatchErr
		},
	)

	result, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if !errors.Is(err, dispatchErr) || result.Package == nil || !reflect.DeepEqual(*result.Package, dispatched) || result.Run != launchRun || result.Attempt != attempt {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWorkflowStartPackageNeverEntersLegacyExecution(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	prepared := PackagePreparationResult{Run: fixture.run}
	service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		t.Fatal("package Start called legacy repository preflight")
		return workflowrepos.ExecutionPreflightResult{}
	}
	service.applier = func(context.Context, applier.Input) (applier.Result, error) {
		t.Fatal("package Start called legacy applier")
		return applier.Result{}, nil
	}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			return PackageWorkflowDispatchResult{Preparation: prepared}, nil
		},
	)

	if _, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowStartPackageUnavailableFailsClosed(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewExecution(fixture.store, nil, "package-start-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.packagePreparation = nil
	service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		t.Fatal("unavailable package preparation entered legacy execution")
		return workflowrepos.ExecutionPreflightResult{}
	}

	result, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if err == nil || result.Run != fixture.run || result.Package != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWorkflowStartLegacyRunRetiredBeforeExecutionSideEffects(t *testing.T) {
	fixture := newLegacyWorkflowFixture(t)
	if _, err := fixture.store.DB().ExecContext(context.Background(), "PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().ExecContext(context.Background(), "DELETE FROM repository_targets WHERE repo_target = ?", fixture.run.RepoTarget); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	beforeAttempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeArtifacts, err := fixture.store.ListArtifactsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeLeases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		t.Fatal("legacy Run entered repository preflight")
		return workflowrepos.ExecutionPreflightResult{}
	}
	fixture.service.applier = func(context.Context, applier.Input) (applier.Result, error) {
		t.Fatal("legacy Run invoked deterministic applier")
		return applier.Result{}, nil
	}
	fixture.service.adapterFactory = func(string) (ExecutorAdapter, error) {
		t.Fatal("legacy Run built an executor adapter")
		return nil, nil
	}
	fixture.service.runner = func(context.Context, string, string, []string, string, time.Duration, pipeline.AgentCommandStreamCallbacks, pipeline.ProcessController) pipeline.AgentCommandRunResult {
		t.Fatal("legacy Run launched a model process")
		return pipeline.AgentCommandRunResult{}
	}

	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	if !errors.Is(err, ErrLegacyExecutionRetired) || result.Run != fixture.run || result.Package != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	afterAttempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterArtifacts, err := fixture.store.ListArtifactsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterLeases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAttempts) != len(beforeAttempts) || len(afterArtifacts) != len(beforeArtifacts) || len(afterLeases) != len(beforeLeases) {
		t.Fatalf("legacy Run side effects: attempts %d->%d artifacts %d->%d leases %d->%d", len(beforeAttempts), len(afterAttempts), len(beforeArtifacts), len(afterArtifacts), len(beforeLeases), len(afterLeases))
	}
}

func packageStartAttempt(run workflowstore.Run, attemptID string) workflowstore.ExecutionAttempt {
	return workflowstore.ExecutionAttempt{
		ID:            77,
		AttemptID:     attemptID,
		RunRowID:      run.ID,
		AttemptNumber: 1,
		Adapter:       string(AdapterOpenCodeGo),
		Model:         "model",
		Status:        workflowstore.AttemptStatusPending,
	}
}

func withPackageWorkflowStartSeams(
	t *testing.T,
	prepare func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error),
	dispatch func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error),
) {
	t.Helper()
	previousPrepare, previousDispatch := packageWorkflowStartPrepare, packageWorkflowStartDispatch
	packageWorkflowStartPrepare = prepare
	packageWorkflowStartDispatch = dispatch
	t.Cleanup(func() {
		packageWorkflowStartPrepare = previousPrepare
		packageWorkflowStartDispatch = previousDispatch
	})
}
