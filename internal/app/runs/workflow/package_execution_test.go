package workflowruns

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

type packageAdmissionFixture struct {
	store      *workflowstore.Store
	service    *Service
	packageRun workflowstore.Run
	plainRun   workflowstore.Run
}

func newPackageAdmissionFixture(t *testing.T, withCutover bool) *packageAdmissionFixture {
	t.Helper()
	ctx := context.Background()
	store, _ := openRunTestStore(t)
	registerRunTestRepo(t, ctx, store, "relay")
	baseCommit := strings.Repeat("a", 40)

	// This fixture only needs the package/run identity and the cutover table;
	// the package composition contract is owned by the package service tests.
	// Disable those insert guards while creating the minimal durable fixture.
	db := store.DB()
	for _, query := range []string{
		"PRAGMA foreign_keys = OFF",
		"DROP TRIGGER IF EXISTS execution_package_input_guard",
		"DROP TRIGGER IF EXISTS run_initial_status_guard",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO execution_packages (
        id, package_id, selection_row_id, workspace_row_id, repo_target, branch,
        base_commit, source_closure_row_id, authority_revision_row_id,
        package_sha256, authority_sha256, source_sha256, design_brief_sha256
    ) VALUES (1, 'package-admission', 1, 1, 'relay', 'main', ?, 1, 1, ?, ?, ?, ?)`,
		baseCommit, strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO execution_package_approvals (
        id, approval_id, package_row_id, package_sha256, operator_confirmation_evidence
    ) VALUES (1, 'pkg-approval-admission', 1, ?, 'package admission test approval')`, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (
        run_id, feature_slug, repo_target, status, branch, base_commit,
        execution_package_row_id, package_approval_row_id
    ) VALUES ('run-package-admission', 'admission', 'relay', 'setup_ready', 'main', ?, 1, 1)`, baseCommit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (
        run_id, feature_slug, repo_target, status, branch, base_commit
    ) VALUES ('run-plain-admission', 'admission', 'relay', 'setup_ready', 'main', ?)`, baseCommit); err != nil {
		t.Fatal(err)
	}
	if withCutover {
		for _, query := range []string{
			"DROP TRIGGER IF EXISTS cutover_activation_insert_guard",
			"DROP TRIGGER IF EXISTS cutover_activation_state_guard",
		} {
			if _, err := db.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`INSERT INTO cutover_activations (
            id, cutover_activation_id, workspace_row_id,
            transition_plan_ticket_revision_row_id, transition_plan_ticket_id,
            transition_plan_ticket_revision, transition_plan_authority_layer_row_id,
            transition_plan_sha256, authority_revision_row_id, authority_revision_id,
            authority_revision_number, authority_sha256, rollback_eligibility,
            activation_status, activated_at, execution_boundary_status,
            rollback_status, roll_forward_status
        ) VALUES (1, 'cutover-admission', 1, 1, 'ticket-admission', 1, 1, ?, 1,
                  'authority-admission', 1, ?, 'eligible', 'active',
                  '2000-01-01T00:00:00Z', 'open', 'available', 'pending')`,
			strings.Repeat("f", 64), strings.Repeat("1", 64)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cutover_current_states (singleton_id, activation_row_id) VALUES (1, 1)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cutover_gateway_configurations (
            activation_row_id, configuration_sha256, relay_repository, relay_commit_oid,
            standing_repository, standing_commit_oid
        ) VALUES (1, ?, 'relay', ?, 'standing', ?)`, strings.Repeat("2", 64), strings.Repeat("3", 40), strings.Repeat("4", 40)); err != nil {
			t.Fatal(err)
		}
		for sequence := 1; sequence <= 7; sequence++ {
			routePath := fmt.Sprintf("/mcp/v1/route-%d", sequence)
			if _, err := db.Exec(`INSERT INTO cutover_gateway_routes (
                activation_row_id, sequence, route_path, role, surface_contract_id,
                manifest_sha256, authority_commit_oid, authority_blob_oid
            ) VALUES (1, ?, ?, 'planner', ?, ?, ?, ?)`, sequence, routePath,
				fmt.Sprintf("surface-%d", sequence), strings.Repeat("5", 64), strings.Repeat("6", 40), strings.Repeat("7", 40)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO cutover_gateway_mappings (
                activation_row_id, sequence, mapping_id, route_path, listener_identity,
                upstream_identity, health_evidence_sha256, trace_evidence_sha256
            ) VALUES (1, ?, ?, ?, ?, ?, ?, ?)`, sequence, fmt.Sprintf("mapping-%d", sequence), routePath,
				fmt.Sprintf("listener-%d", sequence), fmt.Sprintf("upstream-%d", sequence), strings.Repeat("8", 64), strings.Repeat("9", 64)); err != nil {
				t.Fatal(err)
			}
		}
		for _, role := range []string{"wayfinder", "planner", "auditor"} {
			if _, err := db.Exec(`INSERT INTO cutover_gateway_standing_authorities (
                activation_row_id, role, repository, commit_oid, path, blob_oid, content_sha256
            ) VALUES (1, ?, 'standing', ?, ?, ?, ?)`, role, strings.Repeat("a", 40), "/authority/"+role, strings.Repeat("b", 40), strings.Repeat("c", 64)); err != nil {
				t.Fatal(err)
			}
		}
		for sequence := 1; sequence <= 3; sequence++ {
			if _, err := db.Exec(`INSERT INTO cutover_gateway_dependency_outcomes (
                activation_row_id, sequence, ticket_id, ticket_revision, outcome, evidence_sha256
            ) VALUES (1, ?, ?, 1, 'completed_accepted', ?)`, sequence, fmt.Sprintf("ticket-%d", sequence), strings.Repeat("d", 64)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}

	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	packageRun, err := store.GetRunByRunID(ctx, "run-package-admission")
	if err != nil {
		t.Fatal(err)
	}
	plainRun, err := store.GetRunByRunID(ctx, "run-plain-admission")
	if err != nil {
		t.Fatal(err)
	}
	return &packageAdmissionFixture{store: store, service: service, packageRun: packageRun, plainRun: plainRun}
}

func TestAdmitPackageExecution(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)

	before := fixture.packageRun
	admitted, err := fixture.service.AdmitPackageExecution(ctx, before.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.ID != before.ID || admitted.RunID != before.RunID || admitted.Status != workflowstore.RunStatusSetupReady {
		t.Fatalf("admitted Run = %#v, before = %#v", admitted, before)
	}
	current, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		t.Fatalf("current cutover activation: found=%v err=%v", found, err)
	}
	if current.ExecutionBoundaryStatus != "crossed" || !current.FirstNewExecutionRunRowID.Valid || current.FirstNewExecutionRunRowID.Int64 != before.ID {
		t.Fatalf("cutover evidence = %#v", current)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, before)

	repeated, err := fixture.service.AdmitPackageExecution(ctx, before.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != admitted.ID || repeated.RunID != admitted.RunID || repeated.Status != admitted.Status {
		t.Fatalf("repeated admission = %#v, first = %#v", repeated, admitted)
	}
	currentAgain, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found || currentAgain.FirstNewExecutionRunRowID.Int64 != current.FirstNewExecutionRunRowID.Int64 {
		t.Fatalf("repeated cutover evidence = found=%v activation=%#v err=%v", found, currentAgain, err)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, before)
}

func TestAdmitPackageExecutionConcurrent(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	start := make(chan struct{})
	results := make(chan struct {
		run workflowstore.Run
		err error
	}, 2)
	for range 2 {
		go func() {
			<-start
			run, err := fixture.service.AdmitPackageExecution(ctx, fixture.packageRun.RunID)
			results <- struct {
				run workflowstore.Run
				err error
			}{run: run, err: err}
		}()
	}
	close(start)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.run.ID != fixture.packageRun.ID || result.run.Status != workflowstore.RunStatusSetupReady {
			t.Fatalf("concurrent admission = %#v", result.run)
		}
	}
	current, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found || current.ExecutionBoundaryStatus != "crossed" || current.FirstNewExecutionRunRowID.Int64 != fixture.packageRun.ID {
		t.Fatalf("concurrent cutover evidence = found=%v activation=%#v err=%v", found, current, err)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, fixture.packageRun)
}

func TestAdmitPackageExecutionAcceptsExecuting(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	if _, err := fixture.store.DB().Exec("UPDATE runs SET status = 'executing' WHERE run_id = ?", fixture.packageRun.RunID); err != nil {
		t.Fatal(err)
	}

	admitted, err := fixture.service.AdmitPackageExecution(ctx, fixture.packageRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.ID != fixture.packageRun.ID || admitted.Status != workflowstore.RunStatusExecuting {
		t.Fatalf("executing readback = %#v", admitted)
	}
}

func TestAdmitPackageExecutionRejectsInvalidInputAndStates(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, false)
	for _, runID := range []string{"", " ", " run-package-admission", "run-package-admission "} {
		if _, err := fixture.service.AdmitPackageExecution(ctx, runID); err == nil {
			t.Fatalf("invalid Run ID %q was accepted", runID)
		}
	}
	if _, err := fixture.service.AdmitPackageExecution(ctx, fixture.plainRun.RunID); err == nil {
		t.Fatal("non-package Run was accepted")
	}
	if _, err := fixture.store.DB().Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}

	for index, status := range []string{
		workflowstore.RunStatusCreated,
		workflowstore.RunStatusExecutionFailed,
		workflowstore.RunStatusCancelled,
		workflowstore.RunStatusValidating,
		workflowstore.RunStatusValidationFailed,
		workflowstore.RunStatusAuditReady,
		workflowstore.RunStatusNeedsRevision,
		workflowstore.RunStatusCompleted,
	} {
		runID := "run-package-" + strings.ReplaceAll(status, "_", "-")
		completedAt := ""
		if status == workflowstore.RunStatusCompleted {
			completedAt = "2000-01-01T00:00:00Z"
		}
		packageRowID := index + 2
		if _, err := fixture.store.DB().Exec(`INSERT INTO execution_packages (
			id, package_id, selection_row_id, workspace_row_id, repo_target, branch,
			base_commit, source_closure_row_id, authority_revision_row_id,
			package_sha256, authority_sha256, source_sha256, design_brief_sha256
		) VALUES (?, ?, ?, 1, 'relay', 'main', ?, 1, 1, ?, ?, ?, ?)`,
			packageRowID, fmt.Sprintf("package-admission-%d", packageRowID), packageRowID, strings.Repeat("a", 40),
			strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64)); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.DB().Exec(`INSERT INTO runs (
            run_id, feature_slug, repo_target, status, branch, base_commit,
            completed_at, execution_package_row_id
		) VALUES (?, 'admission', 'relay', ?, 'main', ?, NULLIF(?, ''), ?)`, runID, status, strings.Repeat("a", 40), completedAt, packageRowID); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.AdmitPackageExecution(ctx, runID); err == nil {
			t.Fatalf("unsupported status %q was accepted", status)
		}
	}
	if _, err := fixture.store.DB().Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
}

func TestAdmitPackageExecutionRollsBackBoundaryFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_package_admission_boundary
        BEFORE UPDATE OF execution_boundary_status ON cutover_activations
        BEGIN SELECT RAISE(ABORT, 'forced package admission boundary failure'); END`); err != nil {
		t.Fatal(err)
	}
	before, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		t.Fatalf("current cutover before failure: found=%v err=%v", found, err)
	}

	if _, err := fixture.service.AdmitPackageExecution(ctx, fixture.packageRun.RunID); err == nil {
		t.Fatal("boundary failure was ignored")
	}
	after, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		t.Fatalf("current cutover after failure: found=%v err=%v", found, err)
	}
	if after.ExecutionBoundaryStatus != before.ExecutionBoundaryStatus || after.RollbackStatus != before.RollbackStatus || after.FirstNewExecutionRunRowID.Valid {
		t.Fatalf("boundary failure changed cutover state: before=%#v after=%#v", before, after)
	}
	run, err := fixture.store.GetRunByRunID(ctx, fixture.packageRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != workflowstore.RunStatusSetupReady {
		t.Fatalf("boundary failure changed Run status to %q", run.Status)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, fixture.packageRun)
}

func assertPackageAdmissionSideEffects(t *testing.T, store *workflowstore.Store, run workflowstore.Run) {
	t.Helper()
	attempts, err := store.ListExecutionAttemptsByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := store.ListArtifactsByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := store.ListRepositoryBranchMutationLeases(context.Background(), run.RepoTarget, run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 || len(artifacts) != 0 || len(leases) != 0 {
		t.Fatalf("package admission side effects: attempts=%d artifacts=%d leases=%d", len(attempts), len(artifacts), len(leases))
	}
}

func TestAdmitPackageExecutionRequiresAvailableService(t *testing.T) {
	if _, err := (*Service)(nil).AdmitPackageExecution(context.Background(), "run-package-admission"); err == nil {
		t.Fatal("nil workflow service was accepted")
	}
	if _, err := (&Service{}).AdmitPackageExecution(context.Background(), "run-package-admission"); err == nil {
		t.Fatal("workflow service without a store was accepted")
	}
}

func TestCompletePackageDeterministicExecution(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)

	completed, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.ID != fixture.packageRun.ID || completed.RunID != fixture.packageRun.RunID || completed.Status != workflowstore.RunStatusValidating {
		t.Fatalf("completed Run = %#v, before = %#v", completed, fixture.packageRun)
	}
	current, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		t.Fatalf("current cutover activation: found=%v err=%v", found, err)
	}
	if current.ExecutionBoundaryStatus != "crossed" || !current.FirstNewExecutionRunRowID.Valid || current.FirstNewExecutionRunRowID.Int64 != fixture.packageRun.ID {
		t.Fatalf("cutover evidence = %#v", current)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, fixture.packageRun)
}

func TestCompletePackageDeterministicExecutionIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)

	first, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.RunID != first.RunID || second.Status != workflowstore.RunStatusValidating {
		t.Fatalf("repeated finalization = %#v, first = %#v", second, first)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, fixture.packageRun)
}

func TestCompletePackageDeterministicExecutionConcurrentIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	start := make(chan struct{})
	results := make(chan struct {
		run workflowstore.Run
		err error
	}, 2)
	for range 2 {
		go func() {
			<-start
			run, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID)
			results <- struct {
				run workflowstore.Run
				err error
			}{run: run, err: err}
		}()
	}
	close(start)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.run.ID != fixture.packageRun.ID || result.run.RunID != fixture.packageRun.RunID || result.run.Status != workflowstore.RunStatusValidating {
			t.Fatalf("concurrent finalization = %#v", result.run)
		}
	}
	assertPackageAdmissionSideEffects(t, fixture.store, fixture.packageRun)
}

func TestCompletePackageDeterministicExecutionAcceptsValidating(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	if _, err := fixture.store.DB().Exec("DROP TRIGGER IF EXISTS run_status_transition_guard"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec("UPDATE runs SET status = 'validating' WHERE run_id = ?", fixture.packageRun.RunID); err != nil {
		t.Fatal(err)
	}

	completed, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.ID != fixture.packageRun.ID || completed.Status != workflowstore.RunStatusValidating {
		t.Fatalf("validating readback = %#v", completed)
	}
	current, found, err := fixture.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found || current.ExecutionBoundaryStatus != "crossed" {
		t.Fatalf("validating cutover evidence: found=%v activation=%#v err=%v", found, current, err)
	}
}

func TestCompletePackageDeterministicExecutionRejectsInvalidInputAndStates(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, false)
	if _, err := fixture.store.DB().Exec("DROP TRIGGER IF EXISTS run_status_transition_guard"); err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{"", " ", " run-package-admission", "run-package-admission "} {
		if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, runID); err == nil {
			t.Fatalf("invalid Run ID %q was accepted", runID)
		}
	}
	if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.plainRun.RunID); err == nil {
		t.Fatal("non-package Run was accepted")
	}

	for _, status := range []string{
		workflowstore.RunStatusCreated,
		workflowstore.RunStatusExecuting,
		workflowstore.RunStatusExecutionFailed,
		workflowstore.RunStatusCancelled,
		workflowstore.RunStatusValidationFailed,
		workflowstore.RunStatusNeedsRevision,
		workflowstore.RunStatusAuditReady,
		workflowstore.RunStatusCompleted,
	} {
		if _, err := fixture.store.DB().Exec("UPDATE runs SET status = ?, completed_at = CASE WHEN ? = 'completed' THEN '2000-01-01T00:00:00Z' ELSE NULL END WHERE run_id = ?", status, status, fixture.packageRun.RunID); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
			t.Fatalf("unsupported status %q was accepted", status)
		}
		run, err := fixture.store.GetRunByRunID(ctx, fixture.packageRun.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != status {
			t.Fatalf("rejected status changed from %q to %q", status, run.Status)
		}
	}
}

func TestCompletePackageDeterministicExecutionRequiresAvailableService(t *testing.T) {
	if _, err := (*Service)(nil).CompletePackageDeterministicExecution(context.Background(), "run-package-admission"); err == nil {
		t.Fatal("nil workflow service was accepted")
	}
	if _, err := (&Service{}).CompletePackageDeterministicExecution(context.Background(), "run-package-admission"); err == nil {
		t.Fatal("workflow service without a store was accepted")
	}
}

func TestCompletePackageDeterministicExecutionRejectsExecutionAttempts(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "terminal"}[terminal], func(t *testing.T) {
			ctx := context.Background()
			fixture := newPackageAdmissionFixture(t, false)
			createPackageExecutionAttempt(t, ctx, fixture.store, fixture.packageRun, terminal)

			if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
				t.Fatal("Run with an execution attempt was accepted")
			}
			run, err := fixture.store.GetRunByRunID(ctx, fixture.packageRun.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != workflowstore.RunStatusSetupReady {
				t.Fatalf("attempt rejection changed Run status to %q", run.Status)
			}
		})
	}
}

func TestCompletePackageDeterministicExecutionRejectsActiveLeasesUnchanged(t *testing.T) {
	for _, certainty := range []string{workflowstore.RepositoryBranchMutationLeaseCertaintyCertain, workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain} {
		t.Run(certainty, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPackageAdmissionFixture(t, false)
			lease, err := fixture.service.AcquireRunMutationLease(ctx, fixture.packageRun.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if certainty == workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain {
				lease, err = fixture.service.MarkRunMutationLeaseUncertain(ctx, fixture.packageRun.RunID, lease.LeaseID, "test uncertainty")
				if err != nil {
					t.Fatal(err)
				}
			}
			before := lease

			if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
				t.Fatal("Run with an active mutation lease was accepted")
			}
			after, err := fixture.store.GetRepositoryBranchMutationLeaseByLeaseID(ctx, lease.LeaseID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected lease changed: before=%#v after=%#v", before, after)
			}
			run, err := fixture.store.GetRunByRunID(ctx, fixture.packageRun.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != workflowstore.RunStatusSetupReady {
				t.Fatalf("lease rejection changed Run status to %q", run.Status)
			}
		})
	}
}

func TestCompletePackageDeterministicExecutionRollsBackCutoverFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_package_finalization_boundary
        BEFORE UPDATE OF execution_boundary_status ON cutover_activations
        BEGIN SELECT RAISE(ABORT, 'forced package finalization boundary failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
		t.Fatal("cutover failure was ignored")
	}
	assertPackageFinalizationRunStatus(t, fixture.store, fixture.packageRun.RunID, workflowstore.RunStatusSetupReady)
	assertPackageFinalizationCutoverOpen(t, fixture.store)
}

func TestCompletePackageDeterministicExecutionRollsBackFirstTransitionFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_package_finalization_first_transition
        BEFORE UPDATE OF status ON runs WHEN OLD.status = 'setup_ready' AND NEW.status = 'executing'
        BEGIN SELECT RAISE(ABORT, 'forced package finalization first transition failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
		t.Fatal("first transition failure was ignored")
	}
	assertPackageFinalizationRunStatus(t, fixture.store, fixture.packageRun.RunID, workflowstore.RunStatusSetupReady)
	assertPackageFinalizationCutoverOpen(t, fixture.store)
}

func TestCompletePackageDeterministicExecutionRollsBackSecondTransitionFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t, true)
	if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_package_finalization_second_transition
        BEFORE UPDATE OF status ON runs WHEN OLD.status = 'executing' AND NEW.status = 'validating'
        BEGIN SELECT RAISE(ABORT, 'forced package finalization second transition failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
		t.Fatal("second transition failure was ignored")
	}
	assertPackageFinalizationRunStatus(t, fixture.store, fixture.packageRun.RunID, workflowstore.RunStatusSetupReady)
	assertPackageFinalizationCutoverOpen(t, fixture.store)
}

func createPackageExecutionAttempt(t *testing.T, ctx context.Context, store *workflowstore.Store, run workflowstore.Run, terminal bool) {
	t.Helper()
	if _, err := store.DB().Exec("DROP TRIGGER IF EXISTS run_status_transition_guard"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec("UPDATE runs SET status = 'executing' WHERE run_id = ?", run.RunID); err != nil {
		t.Fatal(err)
	}
	err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		attempt, err := tx.CreateExecutionAttempt(ctx, workflowstore.CreateExecutionAttemptParams{
			AttemptID:     "attempt-package-finalization",
			RunRowID:      run.ID,
			AttemptNumber: 1,
			Adapter:       "test",
			Model:         "test",
		})
		if err != nil {
			return err
		}
		if !terminal {
			return nil
		}
		if _, err = tx.TransitionExecutionAttempt(ctx, attempt.AttemptID, workflowstore.AttemptStatusPending, workflowstore.AttemptStatusRunning, "{}"); err != nil {
			return err
		}
		_, err = tx.TransitionExecutionAttempt(ctx, attempt.AttemptID, workflowstore.AttemptStatusRunning, workflowstore.AttemptStatusSucceeded, "{}")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec("UPDATE runs SET status = 'setup_ready' WHERE run_id = ?", run.RunID); err != nil {
		t.Fatal(err)
	}
}

func assertPackageFinalizationRunStatus(t *testing.T, store *workflowstore.Store, runID, want string) {
	t.Helper()
	run, err := store.GetRunByRunID(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != want {
		t.Fatalf("Run status = %q, want %q", run.Status, want)
	}
}

func assertPackageFinalizationCutoverOpen(t *testing.T, store *workflowstore.Store) {
	t.Helper()
	current, found, err := store.GetCurrentCutoverActivation(context.Background())
	if err != nil || !found {
		t.Fatalf("current cutover activation: found=%v err=%v", found, err)
	}
	if current.ExecutionBoundaryStatus != "open" || current.FirstNewExecutionRunRowID.Valid {
		t.Fatalf("cutover changed after rollback: %#v", current)
	}
}
