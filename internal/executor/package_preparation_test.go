package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestPackageWorkflowPreparationAdmissionPrecedesDeterministicCoordination(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	seedPackagePreparationCutover(t, fixture.store)
	previousDeterministic, previousAdaptive := packageWorkflowExecuteDeterministic, packageWorkflowPrepareAdaptive
	t.Cleanup(func() {
		packageWorkflowExecuteDeterministic = previousDeterministic
		packageWorkflowPrepareAdaptive = previousAdaptive
	})
	boundaryObserved := false
	packageWorkflowExecuteDeterministic = func(ctx context.Context, _ *PackageDeterministicExecutionService, _ string) (PackageDeterministicExecutionResult, error) {
		current, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
		if err != nil {
			return PackageDeterministicExecutionResult{}, err
		}
		boundaryObserved = found && current.ExecutionBoundaryStatus == "crossed" && current.FirstNewExecutionRunRowID.Valid && current.FirstNewExecutionRunRowID.Int64 == fixture.run.ID
		return PackageDeterministicExecutionResult{Outcome: DeterministicOutcomeResult{Outcome: DeterministicOutcome{Outcome: DeterministicOutcomeSummary{Status: string(DeterministicPreflightNotPresent)}}}}, nil
	}
	packageWorkflowPrepareAdaptive = func(context.Context, *AdaptiveExecutionAttemptService, AdaptiveExecutionAttemptInput) (AdaptiveExecutionAttemptResult, error) {
		return AdaptiveExecutionAttemptResult{
			Mode: EffectiveExecutorBriefAdaptiveNoOperations, AdaptiveDispatchRequired: true,
			Attempt:       &workflowstore.ExecutionAttempt{ID: 1, RunRowID: fixture.run.ID, AttemptNumber: 1, Adapter: "codex", Model: "model", Status: workflowstore.AttemptStatusPending},
			InputArtifact: &workflowstore.Artifact{OwnerType: workflowstore.ArtifactOwnerExecutionAttempt, ExecutionAttemptRowID: sql.NullInt64{Int64: 1, Valid: true}},
			InputBytes:    []byte("input"),
		}, nil
	}

	if _, err := mustPreparePackageWorkflow(t, fixture); err != nil {
		t.Fatal(err)
	}
	if !boundaryObserved {
		t.Fatal("deterministic coordination began before the package execution boundary was crossed")
	}
}

func seedPackagePreparationCutover(t *testing.T, store *workflowstore.Store) {
	t.Helper()
	db := store.DB()
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS cutover_activation_insert_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cutover_activations (cutover_activation_id, workspace_row_id, transition_plan_ticket_revision_row_id, transition_plan_ticket_id, transition_plan_ticket_revision, transition_plan_authority_layer_row_id, transition_plan_sha256, authority_revision_row_id, authority_revision_id, authority_revision_number, authority_sha256, rollback_eligibility, activation_status, activated_at, execution_boundary_status, rollback_status, roll_forward_status) VALUES ('cutover-package-preparation', 1, 1, 'CUTOVER', 1, 1, ?, 1, 'authority-package-preparation', 1, ?, 'eligible', 'active', '2000-01-01T00:00:00Z', 'open', 'available', 'pending')`, strings.Repeat("a", 64), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cutover_current_states (singleton_id, activation_row_id) SELECT 1, id FROM cutover_activations WHERE cutover_activation_id = 'cutover-package-preparation'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cutover_gateway_configurations (activation_row_id, configuration_sha256, relay_repository, relay_commit_oid, standing_repository, standing_commit_oid) VALUES (1, ?, 'relay', ?, 'standing', ?)`, strings.Repeat("c", 64), strings.Repeat("d", 40), strings.Repeat("e", 40)); err != nil {
		t.Fatal(err)
	}
	for sequence := 1; sequence <= 7; sequence++ {
		routePath := fmt.Sprintf("/mcp/v1/package-preparation-%d", sequence)
		if _, err := db.Exec(`INSERT INTO cutover_gateway_routes (activation_row_id, sequence, route_path, role, surface_contract_id, manifest_sha256, authority_commit_oid, authority_blob_oid) VALUES (1, ?, ?, 'planner', ?, ?, ?, ?)`, sequence, routePath, fmt.Sprintf("surface-%d", sequence), strings.Repeat("f", 64), strings.Repeat("1", 40), strings.Repeat("2", 40)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cutover_gateway_mappings (activation_row_id, sequence, mapping_id, route_path, listener_identity, upstream_identity, health_evidence_sha256, trace_evidence_sha256) VALUES (1, ?, ?, ?, ?, ?, ?, ?)`, sequence, fmt.Sprintf("mapping-%d", sequence), routePath, fmt.Sprintf("listener-%d", sequence), fmt.Sprintf("upstream-%d", sequence), strings.Repeat("3", 64), strings.Repeat("4", 64)); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"wayfinder", "planner", "auditor"} {
		if _, err := db.Exec(`INSERT INTO cutover_gateway_standing_authorities (activation_row_id, role, repository, commit_oid, path, blob_oid, content_sha256) VALUES (1, ?, 'standing', ?, ?, ?, ?)`, role, strings.Repeat("5", 40), "/authority/"+role, strings.Repeat("6", 40), strings.Repeat("7", 64)); err != nil {
			t.Fatal(err)
		}
	}
	for sequence := 1; sequence <= 3; sequence++ {
		if _, err := db.Exec(`INSERT INTO cutover_gateway_dependency_outcomes (activation_row_id, sequence, ticket_id, ticket_revision, outcome, evidence_sha256) VALUES (1, ?, ?, 1, 'completed_accepted', ?)`, sequence, fmt.Sprintf("ticket-%d", sequence), strings.Repeat("8", 64)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
}

func TestPackageWorkflowPreparationModeMatrix(t *testing.T) {
	for _, test := range []struct {
		name       string
		operations bool
		coverage   string
		preflight  DeterministicPreflightStatus
		mode       EffectiveExecutorBriefMode
		adaptive   bool
	}{
		{name: "no operations", mode: EffectiveExecutorBriefAdaptiveNoOperations, adaptive: true},
		{name: "preflight failed", operations: true, coverage: "complete", preflight: DeterministicPreflightFailed, mode: EffectiveExecutorBriefAdaptivePreflightFailed, adaptive: true},
		{name: "partial application", operations: true, coverage: "partial", preflight: DeterministicPreflightReady, mode: EffectiveExecutorBriefAdaptiveAfterPartialApplication, adaptive: true},
		{name: "complete application", operations: true, coverage: "complete", preflight: DeterministicPreflightReady, mode: EffectiveExecutorBriefDeterministicComplete},
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
