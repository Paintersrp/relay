package executor

import (
	"context"
	"errors"
	"testing"

	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestPackageWorkflowPreparationModeMatrix(t *testing.T) {
	for _, test := range []struct {
		name       string
		operations bool
		coverage   string
		preflight  DeterministicPreflightStatus
		mode       ExecutionMode
		adaptive   bool
	}{
		{name: "no operations", mode: ExecutionModeAbsent, adaptive: true},
		{name: "preflight failed", operations: true, coverage: "complete", preflight: DeterministicPreflightFailed, mode: ExecutionModePreflightFailed, adaptive: true},
		{name: "partial application", operations: true, coverage: "partial", preflight: DeterministicPreflightReady, mode: ExecutionModePartialApplied, adaptive: true},
		{name: "complete application", operations: true, coverage: "complete", preflight: DeterministicPreflightReady, mode: ExecutionModeCompleteApplied},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, test.operations, test.coverage)
			previousPreflight, previousApply := packageDeterministicPreflight, packageDeterministicApply
			t.Cleanup(func() {
				packageDeterministicPreflight = previousPreflight
				packageDeterministicApply = previousApply
			})
			if test.operations {
				if test.preflight == DeterministicPreflightFailed {
					packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
						return DeterministicPreflightResult{Status: DeterministicPreflightFailed, Coverage: test.coverage, Failure: &DeterministicPreflightFailure{Code: "source_missing", OperationIndex: 1, Path: "internal/example.go", Expected: "exists=true", Observed: "exists=false"}}, nil
					}
				} else {
					packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
						return readyPackageDeterministicPreflight(test.coverage), nil
					}
					packageDeterministicApply = func(input DeterministicApplyInput) (DeterministicApplicationResult, error) {
						model, err := validateDeterministicPlan(input.Plan)
						if err != nil {
							return DeterministicApplicationResult{}, err
						}
						return applicationResult(model), nil
					}
				}
			}

			service, err := NewPackagePreparation(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Deterministic.Outcome.Outcome.Outcome.Status == "" || result.Adaptive.Mode != test.mode || result.Adaptive.AdaptiveDispatchRequired != test.adaptive {
				t.Fatalf("preparation result = %#v", result)
			}
			if test.adaptive {
				if result.Adaptive.Attempt == nil || result.Adaptive.InputArtifact == nil || len(result.Adaptive.InputBytes) == 0 {
					t.Fatalf("adaptive result = %#v", result.Adaptive)
				}
			} else if result.Adaptive.Attempt != nil || result.Adaptive.InputArtifact != nil || len(result.Adaptive.InputBytes) != 0 {
				t.Fatalf("complete adaptive result = %#v", result.Adaptive)
			}
			if result.Run.Status != workflowstore.RunStatusSetupReady {
				t.Fatalf("Run status = %q", result.Run.Status)
			}
		})
	}
}

func TestPackageWorkflowPreparationPartialLeasePreservedOnAdaptiveFailure(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, true, "partial")
	previousPreflight, previousApply, previousAdaptive := packageDeterministicPreflight, packageDeterministicApply, packageWorkflowPrepareAdaptive
	t.Cleanup(func() {
		packageDeterministicPreflight = previousPreflight
		packageDeterministicApply = previousApply
		packageWorkflowPrepareAdaptive = previousAdaptive
	})
	packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
		return readyPackageDeterministicPreflight("partial"), nil
	}
	packageDeterministicApply = func(input DeterministicApplyInput) (DeterministicApplicationResult, error) {
		model, err := validateDeterministicPlan(input.Plan)
		if err != nil {
			return DeterministicApplicationResult{}, err
		}
		return applicationResult(model), nil
	}
	prepareErr := errors.New("adaptive preparation failed")
	packageWorkflowPrepareAdaptive = func(context.Context, *AdaptiveExecutionAttemptService, AdaptiveExecutionAttemptInput) (AdaptiveExecutionAttemptResult, error) {
		return AdaptiveExecutionAttemptResult{}, prepareErr
	}

	result, err := mustPreparePackageWorkflow(t, fixture)
	if !errors.Is(err, prepareErr) {
		t.Fatalf("error = %v, want %v", err, prepareErr)
	}
	if result.Deterministic.ActiveLease == nil {
		t.Fatal("deterministic result lost the active lease")
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0] != *result.Deterministic.ActiveLease || leases[0].State != workflowstore.RepositoryBranchMutationLeaseStateActive || leases[0].UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain || leases[0].ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired {
		t.Fatalf("leases = %#v, result lease = %#v", leases, result.Deterministic.ActiveLease)
	}
	if result.Run.Status != workflowstore.RunStatusSetupReady {
		t.Fatalf("Run status = %q", result.Run.Status)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), fixture.run.ID)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
}

func TestPackageWorkflowPreparationNoMutationFailureLeavesNoLease(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	previous := packageWorkflowPrepareAdaptive
	t.Cleanup(func() { packageWorkflowPrepareAdaptive = previous })
	prepareErr := errors.New("adaptive preparation failed")
	packageWorkflowPrepareAdaptive = func(context.Context, *AdaptiveExecutionAttemptService, AdaptiveExecutionAttemptInput) (AdaptiveExecutionAttemptResult, error) {
		return AdaptiveExecutionAttemptResult{}, prepareErr
	}
	_, err := mustPreparePackageWorkflow(t, fixture)
	if !errors.Is(err, prepareErr) {
		t.Fatalf("error = %v, want %v", err, prepareErr)
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	for _, lease := range leases {
		if lease.State == workflowstore.RepositoryBranchMutationLeaseStateActive {
			t.Fatalf("active lease = %#v", lease)
		}
	}
}

func TestPackageWorkflowPreparationIdempotencyAndCompleteBehavior(t *testing.T) {
	adaptiveFixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewPackagePreparation(adaptiveFixture.store, adaptiveFixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	input := PackagePreparationInput{RunID: adaptiveFixture.run.RunID, Adapter: "codex", Model: "model"}
	first, err := service.Prepare(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Prepare(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Deterministic.Outcome.Artifact.ArtifactID != second.Deterministic.Outcome.Artifact.ArtifactID || first.Adaptive.Attempt.ID != second.Adaptive.Attempt.ID || first.Adaptive.InputArtifact.ID != second.Adaptive.InputArtifact.ID {
		t.Fatalf("repeated preparation differs: %#v %#v", first, second)
	}
	attempts, err := adaptiveFixture.store.ListExecutionAttemptsByRun(context.Background(), adaptiveFixture.run.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
	artifacts, err := adaptiveFixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempts[0].ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("adaptive artifacts = %#v err=%v", artifacts, err)
	}

	completeFixture := newExecutionAssignmentFixture(t, true, "complete")
	previousPreflight, previousApply := packageDeterministicPreflight, packageDeterministicApply
	t.Cleanup(func() {
		packageDeterministicPreflight = previousPreflight
		packageDeterministicApply = previousApply
	})
	packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
		return readyPackageDeterministicPreflight("complete"), nil
	}
	packageDeterministicApply = func(input DeterministicApplyInput) (DeterministicApplicationResult, error) {
		model, err := validateDeterministicPlan(input.Plan)
		if err != nil {
			return DeterministicApplicationResult{}, err
		}
		return applicationResult(model), nil
	}
	complete, err := NewPackagePreparation(completeFixture.store, completeFixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	completeResult, err := complete.Prepare(context.Background(), PackagePreparationInput{RunID: completeFixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if completeResult.Adaptive.Attempt != nil || completeResult.Adaptive.InputArtifact != nil || len(completeResult.Adaptive.InputBytes) != 0 || completeResult.Run.Status != workflowstore.RunStatusSetupReady {
		t.Fatalf("complete result = %#v", completeResult)
	}
	attempts, err = completeFixture.store.ListExecutionAttemptsByRun(context.Background(), completeFixture.run.ID)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("complete attempts = %#v err=%v", attempts, err)
	}
}

func TestPackageWorkflowPreparationFailureShortCircuiting(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	previousAdmit, previousDeterministic, previousAdaptive := packageWorkflowAdmit, packageWorkflowExecuteDeterministic, packageWorkflowPrepareAdaptive
	t.Cleanup(func() {
		packageWorkflowAdmit = previousAdmit
		packageWorkflowExecuteDeterministic = previousDeterministic
		packageWorkflowPrepareAdaptive = previousAdaptive
	})
	admissionErr := errors.New("admission failed")
	deterministicCalls, adaptiveCalls := 0, 0
	packageWorkflowAdmit = func(context.Context, *workflowruns.Service, string) (workflowstore.Run, error) {
		return workflowstore.Run{}, admissionErr
	}
	packageWorkflowExecuteDeterministic = func(context.Context, *PackageDeterministicExecutionService, string) (PackageDeterministicExecutionResult, error) {
		deterministicCalls++
		return PackageDeterministicExecutionResult{}, nil
	}
	packageWorkflowPrepareAdaptive = func(context.Context, *AdaptiveExecutionAttemptService, AdaptiveExecutionAttemptInput) (AdaptiveExecutionAttemptResult, error) {
		adaptiveCalls++
		return AdaptiveExecutionAttemptResult{}, nil
	}
	service, err := NewPackagePreparation(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"}); !errors.Is(err, admissionErr) {
		t.Fatalf("admission error = %v", err)
	}
	if deterministicCalls != 0 || adaptiveCalls != 0 {
		t.Fatalf("calls after admission failure = deterministic %d adaptive %d", deterministicCalls, adaptiveCalls)
	}

	packageWorkflowAdmit = func(context.Context, *workflowruns.Service, string) (workflowstore.Run, error) {
		return fixture.run, nil
	}
	deterministicErr := errors.New("deterministic failed")
	packageWorkflowExecuteDeterministic = func(context.Context, *PackageDeterministicExecutionService, string) (PackageDeterministicExecutionResult, error) {
		deterministicCalls++
		return PackageDeterministicExecutionResult{}, deterministicErr
	}
	if _, err := service.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"}); !errors.Is(err, deterministicErr) {
		t.Fatalf("deterministic error = %v", err)
	}
	if adaptiveCalls != 0 {
		t.Fatalf("adaptive calls after deterministic failure = %d", adaptiveCalls)
	}

	packageWorkflowExecuteDeterministic = func(context.Context, *PackageDeterministicExecutionService, string) (PackageDeterministicExecutionResult, error) {
		return PackageDeterministicExecutionResult{Outcome: DeterministicOutcomeResult{Outcome: DeterministicOutcome{Outcome: DeterministicOutcomeSummary{Status: "unsupported"}}}}, nil
	}
	if _, err := service.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"}); !errors.Is(err, ErrPackagePreparationConflict) {
		t.Fatalf("mode disagreement error = %v", err)
	}
}

func mustPreparePackageWorkflow(t *testing.T, fixture *executionAssignmentFixture) (PackagePreparationResult, error) {
	t.Helper()
	service, err := NewPackagePreparation(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	return service.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
}
