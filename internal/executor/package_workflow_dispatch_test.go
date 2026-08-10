package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestPackageWorkflowDispatchAdaptiveModesUsePreparedAttemptID(t *testing.T) {
	for _, mode := range []ExecutionMode{
		ExecutionModeAbsent,
		ExecutionModePreflightFailed,
	} {
		t.Run(string(mode), func(t *testing.T) {
			service, prepared := syntheticPackageWorkflowDispatch(t, mode)
			var input PreparedAdaptiveLaunchInput
			withPackageWorkflowDispatchSeams(t, func(_ context.Context, _ *Execution, got PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
				input = got
				return validAdaptiveDispatchLaunch(prepared, true), nil
			}, nil)

			if _, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared); err != nil {
				t.Fatal(err)
			}
			if input.RunID != prepared.Run.RunID || input.AttemptID != prepared.Adaptive.Attempt.AttemptID {
				t.Fatalf("launch input=%#v prepared attempt=%q", input, prepared.Adaptive.Attempt.AttemptID)
			}
		})
	}
}

func TestPackageWorkflowDispatchPartialRequiresRetainedLease(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModePartialApplied)
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return validAdaptiveDispatchLaunch(prepared, true), nil
	}, nil)
	result, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Launch.Lease.LeaseID != prepared.Deterministic.ActiveLease.LeaseID {
		t.Fatalf("lease=%q prepared=%q", result.Launch.Lease.LeaseID, prepared.Deterministic.ActiveLease.LeaseID)
	}

	replacement := validAdaptiveDispatchLaunch(prepared, true)
	replacement.Lease = cloneLease(*prepared.Deterministic.ActiveLease)
	replacement.Lease.LeaseID = "replacement-lease"
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return replacement, nil
	}, nil)
	_, err = service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if !errors.Is(err, ErrPackageWorkflowDispatchConflict) {
		t.Fatalf("err=%v, want dispatch conflict", err)
	}
}

func TestPackageWorkflowDispatchAdaptiveReadbackAndIdentity(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModeAbsent)
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return validAdaptiveDispatchLaunch(prepared, false), nil
	}, nil)
	result, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if err != nil || result.Launch.NewlyAdmitted || result.Launch.NewlyLaunched {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	mismatch := validAdaptiveDispatchLaunch(prepared, true)
	mismatch.Attempt = cloneAttempt(*prepared.Adaptive.Attempt)
	mismatch.Attempt.AttemptID = "other-attempt"
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return mismatch, nil
	}, nil)
	_, err = service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if !errors.Is(err, ErrPackageWorkflowDispatchConflict) {
		t.Fatalf("err=%v, want dispatch conflict", err)
	}
}

func TestPackageWorkflowDispatchAdaptiveLaunchErrorDoesNotFinalize(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModeAbsent)
	launchErr := errors.New("launch failed")
	finalizeCalls := 0
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return validAdaptiveDispatchLaunch(prepared, true), launchErr
	}, func(context.Context, *workflowruns.Service, string) (workflowstore.Run, error) {
		finalizeCalls++
		return workflowstore.Run{}, nil
	})
	result, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if !errors.Is(err, launchErr) || finalizeCalls != 0 || result.Launch.Run == nil {
		t.Fatalf("result=%#v err=%v finalize calls=%d", result, err, finalizeCalls)
	}
}

func TestPackageWorkflowDispatchDeterministicCompleteOrderingAndResult(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModeCompleteApplied)
	var launchInput PreparedAdaptiveLaunchInput
	finalizeCalls := 0
	withPackageWorkflowDispatchSeams(t, func(_ context.Context, _ *Execution, input PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		launchInput = input
		if finalizeCalls != 0 {
			return PreparedAdaptiveLaunchResult{}, errors.New("finalized too early")
		}
		return PreparedAdaptiveLaunchResult{Mode: ExecutionModeCompleteApplied}, nil
	}, func(context.Context, *workflowruns.Service, string) (workflowstore.Run, error) {
		finalizeCalls++
		finalized := prepared.Run
		finalized.Status = workflowstore.RunStatusValidating
		return finalized, nil
	})
	result, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if err != nil || launchInput.RunID != prepared.Run.RunID || launchInput.AttemptID != "" || finalizeCalls != 1 || result.FinalizedRun == nil || result.FinalizedRun.ID != prepared.Run.ID || result.FinalizedRun.RunID != prepared.Run.RunID || result.FinalizedRun.Status != workflowstore.RunStatusValidating {
		t.Fatalf("result=%#v input=%#v err=%v finalize calls=%d", result, launchInput, err, finalizeCalls)
	}
}

func TestPackageWorkflowDispatchCompleteRejectsMalformedLaunchBeforeFinalization(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModeCompleteApplied)
	finalizeCalls := 0
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return PreparedAdaptiveLaunchResult{Mode: ExecutionModeCompleteApplied, NewlyAdmitted: true}, nil
	}, func(context.Context, *workflowruns.Service, string) (workflowstore.Run, error) {
		finalizeCalls++
		return workflowstore.Run{}, nil
	})
	_, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if !errors.Is(err, ErrPackageWorkflowDispatchConflict) || finalizeCalls != 0 {
		t.Fatalf("err=%v finalize calls=%d", err, finalizeCalls)
	}
}

func TestPackageWorkflowDispatchCompleteFinalizationFailurePreservesLaunch(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModeCompleteApplied)
	finalizeErr := errors.New("finalization failed")
	launch := PreparedAdaptiveLaunchResult{Mode: ExecutionModeCompleteApplied}
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return launch, nil
	}, func(context.Context, *workflowruns.Service, string) (workflowstore.Run, error) {
		return workflowstore.Run{}, finalizeErr
	})
	result, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if !errors.Is(err, finalizeErr) || result.Launch != launch || result.FinalizedRun != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestPackageWorkflowDispatchCompleteIsIdempotent(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModeCompleteApplied)
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return PreparedAdaptiveLaunchResult{Mode: ExecutionModeCompleteApplied}, nil
	}, nil)
	first, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if err != nil || first.FinalizedRun == nil || second.FinalizedRun == nil || first.FinalizedRun.ID != second.FinalizedRun.ID || first.FinalizedRun.RunID != second.FinalizedRun.RunID || second.FinalizedRun.Status != workflowstore.RunStatusValidating {
		t.Fatalf("first=%#v second=%#v err=%v", first, second, err)
	}
}

func TestPackageWorkflowDispatchMalformedPreparationRejectedBeforeLaunch(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModeAbsent)
	prepared.Run.ExecutionPackageRowID.Valid = false
	launchCalls := 0
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		launchCalls++
		return PreparedAdaptiveLaunchResult{}, nil
	}, nil)
	_, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared)
	if !errors.Is(err, ErrPackageWorkflowDispatchConflict) || launchCalls != 0 {
		t.Fatalf("err=%v launch calls=%d", err, launchCalls)
	}
}

func TestPackageWorkflowDispatchDoesNotModifyAttemptOrLease(t *testing.T) {
	service, prepared := syntheticPackageWorkflowDispatch(t, ExecutionModePartialApplied)
	withPackageWorkflowDispatchSeams(t, func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error) {
		return validAdaptiveDispatchLaunch(prepared, true), nil
	}, nil)
	beforeAttempts, err := service.store.ListExecutionAttemptsByRun(context.Background(), prepared.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeLeases, err := service.store.ListRepositoryBranchMutationLeases(context.Background(), prepared.Run.RepoTarget, prepared.Run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DispatchPreparedPackageWorkflow(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	afterAttempts, err := service.store.ListExecutionAttemptsByRun(context.Background(), prepared.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterLeases, err := service.store.ListRepositoryBranchMutationLeases(context.Background(), prepared.Run.RepoTarget, prepared.Run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(beforeAttempts) != fmt.Sprint(afterAttempts) || fmt.Sprint(beforeLeases) != fmt.Sprint(afterLeases) {
		t.Fatalf("attempts before=%#v after=%#v leases before=%#v after=%#v", beforeAttempts, afterAttempts, beforeLeases, afterLeases)
	}
}

func syntheticPackageWorkflowDispatch(t *testing.T, mode ExecutionMode) (*Execution, PackagePreparationResult) {
	t.Helper()
	fixture := newExecutionAssignmentFixture(t, mode == ExecutionModePartialApplied, "partial")
	prepared := PackagePreparationResult{Run: fixture.run}
	prepared.Deterministic.Outcome.Outcome.Outcome.Status = "applied"
	prepared.Deterministic.Outcome.Outcome.Outcome.Coverage = "complete"
	if mode == ExecutionModeAbsent {
		prepared.Deterministic.Outcome.Outcome.Outcome.Status = string(DeterministicPreflightNotPresent)
		prepared.Deterministic.Outcome.Outcome.Outcome.Coverage = ""
	}
	if mode == ExecutionModePreflightFailed {
		prepared.Deterministic.Outcome.Outcome.Outcome.Status = string(DeterministicPreflightFailed)
		prepared.Deterministic.Outcome.Outcome.Outcome.Coverage = "complete"
		prepared.Deterministic.Outcome.Outcome.PreflightFailure = &DeterministicOutcomePreflightFailure{}
	}
	if mode == ExecutionModePartialApplied {
		lease := workflowstore.RepositoryBranchMutationLease{ID: 21, LeaseID: "retained-lease", RepoTarget: fixture.run.RepoTarget, Branch: fixture.run.Branch, OwnerKind: "run_execution", OwnerIdentity: fixture.run.RunID, State: workflowstore.RepositoryBranchMutationLeaseStateActive, UncertaintyState: workflowstore.RepositoryBranchMutationLeaseCertaintyCertain, ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired}
		prepared.Deterministic.ActiveLease = &lease
		prepared.Deterministic.Outcome.Outcome.Outcome.Coverage = "partial"
	}
	if mode != ExecutionModeCompleteApplied {
		attempt := workflowstore.ExecutionAttempt{ID: 31, AttemptID: "prepared-attempt", RunRowID: fixture.run.ID, AttemptNumber: 1, Adapter: "codex", Model: "model", Status: workflowstore.AttemptStatusPending}
		prepared.Adaptive = AdaptiveExecutionAttemptResult{Mode: mode, AdaptiveDispatchRequired: true, Attempt: &attempt, InputArtifact: &workflowstore.Artifact{ID: 41, OwnerType: workflowstore.ArtifactOwnerExecutionAttempt, ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true}}, InputBytes: []byte("prepared")}
	} else {
		prepared.Adaptive.Mode = mode
	}
	exec, err := NewExecution(fixture.store, nil, "package-dispatch-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	return exec, prepared
}

func validAdaptiveDispatchLaunch(prepared PackagePreparationResult, newlyAdmitted bool) PreparedAdaptiveLaunchResult {
	run := prepared.Run
	attempt := *prepared.Adaptive.Attempt
	lease := workflowstore.RepositoryBranchMutationLease{ID: 51, LeaseID: "fresh-lease", RepoTarget: run.RepoTarget, Branch: run.Branch, OwnerIdentity: run.RunID, State: workflowstore.RepositoryBranchMutationLeaseStateActive, UncertaintyState: workflowstore.RepositoryBranchMutationLeaseCertaintyCertain, ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired}
	if prepared.Adaptive.Mode == ExecutionModePartialApplied {
		lease = *prepared.Deterministic.ActiveLease
	}
	return PreparedAdaptiveLaunchResult{Mode: prepared.Adaptive.Mode, AdaptiveDispatchRequired: true, NewlyAdmitted: newlyAdmitted, NewlyLaunched: newlyAdmitted, Run: &run, Attempt: &attempt, Lease: &lease}
}

func withPackageWorkflowDispatchSeams(t *testing.T, launch func(context.Context, *Execution, PreparedAdaptiveLaunchInput) (PreparedAdaptiveLaunchResult, error), finalize func(context.Context, *workflowruns.Service, string) (workflowstore.Run, error)) {
	t.Helper()
	previousLaunch, previousFinalize := packageWorkflowDispatchLaunch, packageWorkflowDispatchFinalize
	if launch != nil {
		packageWorkflowDispatchLaunch = launch
	}
	if finalize != nil {
		packageWorkflowDispatchFinalize = finalize
	}
	t.Cleanup(func() {
		packageWorkflowDispatchLaunch = previousLaunch
		packageWorkflowDispatchFinalize = previousFinalize
	})
}

func cloneLease(lease workflowstore.RepositoryBranchMutationLease) *workflowstore.RepositoryBranchMutationLease {
	return &lease
}

func cloneAttempt(attempt workflowstore.ExecutionAttempt) *workflowstore.ExecutionAttempt {
	return &attempt
}
