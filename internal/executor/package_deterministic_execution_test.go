package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestPackageDeterministicExecutionNoOperations(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	service, err := NewPackageDeterministicExecutionService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.Execute(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Preflight.Status != DeterministicPreflightNotPresent || first.Outcome.Outcome.Outcome.Status != string(DeterministicPreflightNotPresent) || first.Application != nil || first.ActiveLease != nil {
		t.Fatalf("first result = %#v", first)
	}
	if artifacts := listRunArtifacts(t, fixture); len(artifacts) != 2 {
		t.Fatalf("Run artifacts = %d, want assignment and outcome", len(artifacts))
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("mutation leases = %#v, want none", leases)
	}

	second, err := service.Execute(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome.Artifact.ArtifactID != first.Outcome.Artifact.ArtifactID {
		t.Fatalf("repeated outcome artifact = %#v, want %#v", second.Outcome.Artifact, first.Outcome.Artifact)
	}
	if artifacts := listRunArtifacts(t, fixture); len(artifacts) != 2 {
		t.Fatalf("repeated Run artifacts = %d, want assignment and outcome", len(artifacts))
	}
}

func TestPackageDeterministicExecutionPreflightFailureIsDurableAndIdempotent(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, true, "complete")
	service, err := NewPackageDeterministicExecutionService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	preflightCalls := 0
	previousPreflight := packageDeterministicPreflight
	previousApply := packageDeterministicApply
	t.Cleanup(func() {
		packageDeterministicPreflight = previousPreflight
		packageDeterministicApply = previousApply
	})
	packageDeterministicPreflight = func(input DeterministicPreflightInput) (DeterministicPreflightResult, error) {
		preflightCalls++
		return DeterministicPreflightResult{
			Status:   DeterministicPreflightFailed,
			Coverage: "complete",
			Failure:  &DeterministicPreflightFailure{Code: "source_missing", OperationIndex: 1, DirectiveIndex: 0, Path: "internal/example.go", Expected: "exists=true", Observed: "exists=false"},
		}, nil
	}
	packageDeterministicApply = func(DeterministicApplyInput) (DeterministicApplicationResult, error) {
		return DeterministicApplicationResult{}, errors.New("application must not be called")
	}

	first, err := service.Execute(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Preflight.Status != DeterministicPreflightFailed || first.Outcome.Outcome.Outcome.Status != string(DeterministicPreflightFailed) || first.Application != nil || first.ActiveLease != nil {
		t.Fatalf("first result = %#v", first)
	}
	if preflightCalls != 1 {
		t.Fatalf("preflight calls = %d, want 1", preflightCalls)
	}
	if artifacts := listRunArtifacts(t, fixture); len(artifacts) != 2 {
		t.Fatalf("Run artifacts = %d, want assignment and outcome", len(artifacts))
	}

	second, err := service.Execute(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome.Artifact.ArtifactID != first.Outcome.Artifact.ArtifactID || preflightCalls != 1 {
		t.Fatalf("repeated result = %#v, preflight calls = %d", second, preflightCalls)
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("mutation leases = %#v, want none", leases)
	}
}

func TestPackageDeterministicExecutionRetainsPartialAndReleasesComplete(t *testing.T) {
	for _, coverage := range []string{"partial", "complete"} {
		t.Run(coverage, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, true, coverage)
			service, err := NewPackageDeterministicExecutionService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			preflightCalls, applicationCalls := 0, 0
			previousPreflight, previousApply := packageDeterministicPreflight, packageDeterministicApply
			t.Cleanup(func() {
				packageDeterministicPreflight = previousPreflight
				packageDeterministicApply = previousApply
			})
			packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
				preflightCalls++
				return readyPackageDeterministicPreflight(coverage), nil
			}
			packageDeterministicApply = func(input DeterministicApplyInput) (DeterministicApplicationResult, error) {
				applicationCalls++
				model, err := validateDeterministicPlan(input.Plan)
				if err != nil {
					return DeterministicApplicationResult{}, err
				}
				return applicationResult(model), nil
			}

			first, err := service.Execute(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if first.Outcome.Outcome.Outcome.Status != "applied" || first.Application == nil || preflightCalls != 1 || applicationCalls != 1 {
				t.Fatalf("first result = %#v, preflight calls = %d, application calls = %d", first, preflightCalls, applicationCalls)
			}
			leases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
			if err != nil {
				t.Fatal(err)
			}
			if coverage == "partial" {
				if first.ActiveLease == nil || len(leases) != 1 || leases[0].LeaseID != first.ActiveLease.LeaseID || leases[0].UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain || leases[0].ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired {
					t.Fatalf("partial leases = %#v, result lease = %#v", leases, first.ActiveLease)
				}
			} else if first.ActiveLease != nil || len(leases) != 1 || leases[0].State != workflowstore.RepositoryBranchMutationLeaseStateReleased {
				t.Fatalf("complete leases = %#v, result lease = %#v", leases, first.ActiveLease)
			}

			second, err := service.Execute(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if preflightCalls != 1 || applicationCalls != 1 || second.Outcome.Artifact.ArtifactID != first.Outcome.Artifact.ArtifactID {
				t.Fatalf("readback result = %#v, preflight calls = %d, application calls = %d", second, preflightCalls, applicationCalls)
			}
			if coverage == "partial" && (second.ActiveLease == nil || second.ActiveLease.LeaseID != first.ActiveLease.LeaseID) {
				t.Fatalf("partial readback lease = %#v, want %#v", second.ActiveLease, first.ActiveLease)
			}
		})
	}
}

func TestPackageDeterministicExecutionFailureSettlement(t *testing.T) {
	t.Run("application rollback releases", func(t *testing.T) {
		fixture := newExecutionAssignmentFixture(t, true, "complete")
		service, err := NewPackageDeterministicExecutionService(fixture.store)
		if err != nil {
			t.Fatal(err)
		}
		previousPreflight, previousApply := packageDeterministicPreflight, packageDeterministicApply
		t.Cleanup(func() {
			packageDeterministicPreflight = previousPreflight
			packageDeterministicApply = previousApply
		})
		packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
			return readyPackageDeterministicPreflight("complete"), nil
		}
		packageDeterministicApply = func(DeterministicApplyInput) (DeterministicApplicationResult, error) {
			return DeterministicApplicationResult{}, fmt.Errorf("%w: injected", ErrDeterministicApplicationFailed)
		}
		if _, err := service.Execute(context.Background(), fixture.run.RunID); !errors.Is(err, ErrDeterministicApplicationFailed) {
			t.Fatalf("error = %v", err)
		}
		assertNoActivePackageDeterministicLease(t, fixture)
	})

	t.Run("uncertain application retains lease", func(t *testing.T) {
		fixture := newExecutionAssignmentFixture(t, true, "complete")
		service, err := NewPackageDeterministicExecutionService(fixture.store)
		if err != nil {
			t.Fatal(err)
		}
		previousPreflight, previousApply := packageDeterministicPreflight, packageDeterministicApply
		t.Cleanup(func() {
			packageDeterministicPreflight = previousPreflight
			packageDeterministicApply = previousApply
		})
		packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
			return readyPackageDeterministicPreflight("complete"), nil
		}
		packageDeterministicApply = func(DeterministicApplyInput) (DeterministicApplicationResult, error) {
			return DeterministicApplicationResult{}, fmt.Errorf("%w: injected", ErrDeterministicMutationReconciliation)
		}
		if _, err := service.Execute(context.Background(), fixture.run.RunID); !errors.Is(err, ErrDeterministicMutationReconciliation) {
			t.Fatalf("error = %v", err)
		}
		assertUncertainPackageDeterministicLease(t, fixture)
	})

	t.Run("outcome persistence failure retains lease", func(t *testing.T) {
		fixture := newExecutionAssignmentFixture(t, true, "complete")
		service, err := NewPackageDeterministicExecutionService(fixture.store)
		if err != nil {
			t.Fatal(err)
		}
		previousPreflight, previousApply, previousPersist := packageDeterministicPreflight, packageDeterministicApply, packageDeterministicPersist
		t.Cleanup(func() {
			packageDeterministicPreflight = previousPreflight
			packageDeterministicApply = previousApply
			packageDeterministicPersist = previousPersist
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
		packageDeterministicPersist = func(context.Context, *DeterministicOutcomeService, DeterministicOutcomeInput) (DeterministicOutcomeResult, error) {
			return DeterministicOutcomeResult{}, errors.New("injected outcome persistence failure")
		}
		if _, err := service.Execute(context.Background(), fixture.run.RunID); err == nil || !strings.Contains(err.Error(), "injected outcome persistence failure") {
			t.Fatalf("error = %v", err)
		}
		assertUncertainPackageDeterministicLease(t, fixture)
	})

	t.Run("release failure preserves durable outcome for readback", func(t *testing.T) {
		fixture := newExecutionAssignmentFixture(t, true, "complete")
		service, err := NewPackageDeterministicExecutionService(fixture.store)
		if err != nil {
			t.Fatal(err)
		}
		previousPreflight, previousApply, previousRelease := packageDeterministicPreflight, packageDeterministicApply, packageDeterministicRelease
		t.Cleanup(func() {
			packageDeterministicPreflight = previousPreflight
			packageDeterministicApply = previousApply
			packageDeterministicRelease = previousRelease
		})
		preflightCalls, applicationCalls := 0, 0
		packageDeterministicPreflight = func(DeterministicPreflightInput) (DeterministicPreflightResult, error) {
			preflightCalls++
			return readyPackageDeterministicPreflight("complete"), nil
		}
		packageDeterministicApply = func(input DeterministicApplyInput) (DeterministicApplicationResult, error) {
			applicationCalls++
			model, err := validateDeterministicPlan(input.Plan)
			if err != nil {
				return DeterministicApplicationResult{}, err
			}
			return applicationResult(model), nil
		}
		packageDeterministicRelease = func(context.Context, *workflowruns.Service, string, string) (workflowstore.RepositoryBranchMutationLease, error) {
			return workflowstore.RepositoryBranchMutationLease{}, errors.New("injected lease release failure")
		}
		first, err := service.Execute(context.Background(), fixture.run.RunID)
		if err == nil || !strings.Contains(err.Error(), "injected lease release failure") || first.Outcome.Outcome.Outcome.Status != "applied" {
			t.Fatalf("first result = %#v, error = %v", first, err)
		}
		if preflightCalls != 1 || applicationCalls != 1 || first.ActiveLease == nil {
			t.Fatalf("first calls/lease = %d/%d/%#v", preflightCalls, applicationCalls, first.ActiveLease)
		}
		packageDeterministicRelease = previousRelease
		second, err := service.Execute(context.Background(), fixture.run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if second.ActiveLease != nil || second.Outcome.Artifact.ArtifactID != first.Outcome.Artifact.ArtifactID || preflightCalls != 1 || applicationCalls != 1 {
			t.Fatalf("readback result = %#v, calls = %d/%d", second, preflightCalls, applicationCalls)
		}
		assertNoActivePackageDeterministicLease(t, fixture)
	})
}

func readyPackageDeterministicPreflight(coverage string) DeterministicPreflightResult {
	return DeterministicPreflightResult{
		Status:   DeterministicPreflightReady,
		Coverage: coverage,
		Plan: &DeterministicMutationPlan{
			Coverage: coverage,
			Operations: []PreparedDeterministicOperation{{
				Index: 1, Operation: "create", SourcePath: "internal/example.go", After: newFileState([]byte("package example\n")),
			}},
		},
	}
}

func assertNoActivePackageDeterministicLease(t *testing.T, fixture *executionAssignmentFixture) {
	t.Helper()
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

func assertUncertainPackageDeterministicLease(t *testing.T, fixture *executionAssignmentFixture) {
	t.Helper()
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].State != workflowstore.RepositoryBranchMutationLeaseStateActive || leases[0].UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain || leases[0].ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationRequired {
		t.Fatalf("leases = %#v", leases)
	}
}
