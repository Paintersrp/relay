package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"relay/internal/applier"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowStartPackageConstructorInitializesPreparation(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	if service.packagePreparation == nil {
		t.Fatal("package preparation service is nil")
	}
}

func TestWorkflowStartPackageRoutesPackageRun(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	prepared := PackageWorkflowPreparationResult{Run: fixture.run}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
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
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	var got PackageWorkflowPreparationInput
	prepared := PackageWorkflowPreparationResult{Run: fixture.run}
	withPackageWorkflowStartSeams(t,
		func(_ context.Context, _ *PackageWorkflowPreparationService, input PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			got = input
			return prepared, nil
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
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
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	attempt := packageStartAttempt(fixture.run, "prepared-attempt")
	prepared := PackageWorkflowPreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
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
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
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
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	attempt := packageStartAttempt(fixture.run, "readback-attempt")
	prepared := PackageWorkflowPreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
	readbackRun := fixture.run
	readbackRun.Status = workflowstore.RunStatusExecuting
	dispatched := PackageWorkflowDispatchResult{
		Preparation: prepared,
		Launch:      PreparedAdaptiveLaunchResult{Run: &readbackRun, Attempt: &attempt, NewlyAdmitted: false, NewlyLaunched: false},
	}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
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
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	prepared := PackageWorkflowPreparationResult{Run: fixture.run}
	finalized := fixture.run
	finalized.Status = workflowstore.RunStatusValidating
	dispatched := PackageWorkflowDispatchResult{Preparation: prepared, FinalizedRun: &finalized}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
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
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	attempt := packageStartAttempt(fixture.run, "preparation-failure-attempt")
	prepared := PackageWorkflowPreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
	prepareErr := errors.New("preparation failed")
	dispatchCalls := 0
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			return prepared, prepareErr
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
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
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	attempt := packageStartAttempt(fixture.run, "dispatch-failure-attempt")
	prepared := PackageWorkflowPreparationResult{Run: fixture.run, Adaptive: AdaptiveExecutionAttemptResult{Attempt: &attempt}}
	launchRun := fixture.run
	launchRun.Status = workflowstore.RunStatusExecuting
	launch := PreparedAdaptiveLaunchResult{Run: &launchRun}
	dispatched := PackageWorkflowDispatchResult{Preparation: prepared, Launch: launch}
	dispatchErr := errors.New("dispatch failed")
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
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
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
	prepared := PackageWorkflowPreparationResult{Run: fixture.run}
	service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		t.Fatal("package Start called legacy repository preflight")
		return workflowrepos.ExecutionPreflightResult{}
	}
	service.applier = func(context.Context, applier.Input) (applier.Result, error) {
		t.Fatal("package Start called legacy applier")
		return applier.Result{}, nil
	}
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			return prepared, nil
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
			return PackageWorkflowDispatchResult{Preparation: prepared}, nil
		},
	)

	if _, err := service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowStartPackageUnavailableFailsClosed(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service := NewWorkflowExecutionService(fixture.store, nil, "package-start-test")
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

func TestWorkflowStartPackageIsNilForLegacyRunAndPreflightBehaviorRemains(t *testing.T) {
	fixture := newWorkflowFixture(t)
	fixture.service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		return workflowrepos.ExecutionPreflightResult{OK: false, BlockerCode: "blocked"}
	}

	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"})
	var preflightErr *WorkflowPreflightError
	if !errors.As(err, &preflightErr) || result.Run != fixture.run || result.Package != nil {
		t.Fatalf("result=%#v err=%v", result, err)
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
	prepare func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error),
	dispatch func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error),
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
