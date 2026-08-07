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

func newPackageAdmissionFixture(t *testing.T) *packageAdmissionFixture {
	t.Helper()
	ctx := context.Background()
	store, _ := openRunTestStore(t)
	registerRunTestRepo(t, ctx, store, "relay")
	baseCommit := strings.Repeat("a", 40)

	// The package/run identity remains intentionally compact, but its referenced
	// Feature basis is current so owner-boundary rechecks exercise normal behavior.
	db := store.DB()
	if _, err := db.Exec(`UPDATE repository_targets SET configured_branch_ref = 'refs/heads/main', configuration_version = 2 WHERE repo_target = 'relay'`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"PRAGMA foreign_keys = OFF",
		"DROP TRIGGER IF EXISTS execution_package_input_guard",
		"DROP TRIGGER IF EXISTS run_initial_status_guard",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO projects (id, project_id, name) VALUES (1, 'project-package-admission', 'Package admission')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO source_vaults (id, vault_id, repo_target, relative_path) VALUES (1, 'vault-package-admission', 'relay', 'vaults/package-admission')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO source_vault_closures (id, closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES (1, 'closure-package-admission', 1, ?, ?, 1, 'refs/relay/closures/package-admission', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z')`, baseCommit, strings.Repeat("b", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspaces (id, workspace_id, project_row_id, feature_slug) VALUES (1, 'workspace-package-admission', 1, 'admission')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_authority_revisions (id, authority_revision_id, workspace_row_id, revision_number, source_closure_row_id) VALUES (1, 'authority-package-admission', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET current_authority_revision_row_id = 1, version = 2 WHERE id = 1 AND version = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_discovery_adoptions (workspace_row_id, adoption_id, operator_identity, adopted_workspace_version) VALUES (1, 'discovery-adoption-package-admission', 'run-fixture', 2)`); err != nil {
		t.Fatal(err)
	}
	manifestSHA := strings.Repeat("f", 64)
	if _, err := db.Exec(`INSERT INTO feature_workspace_discovery_artifacts (id, discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES (1, 'discovery-artifact-package-admission', 1, 'feature-discovery/admission/closure/manifest.json', ?, 'application/vnd.relay.feature-discovery-closure+json', 1)`, manifestSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_integrated_discovery_revisions (id, discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity, settled_destination, continuation_json) VALUES (1, 'discovery-revision-package-admission', 1, 1, 1, 'run-fixture', 'requirements', '{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_discovery_closure_packets (id, closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES (1, 'discovery-packet-package-admission', 1, 1, 'requirements', 1, ?, 1, 'application/vnd.relay.feature-discovery-closure+json')`, manifestSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET current_discovery_revision_row_id = 1, current_discovery_closure_packet_row_id = 1, version = 3 WHERE id = 1 AND version = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO delivery_tickets (id, ticket_id, workspace_row_id, external_priority) VALUES (1, 'P2-T2', 1, 10)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO delivery_ticket_revisions (id, delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (1, 1, 1, 'relay', 'main', ?, 1, 'tickets/admission.json', 'Package admission', 'Package admission fixture', 'not_required')`, baseCommit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE delivery_tickets SET current_revision_row_id = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO delivery_ticket_revision_approvals (id, approval_id, revision_row_id, approval_kind, approval_state, rationale, source_closure_row_id, authority_revision_row_id) VALUES (1, 'approval-package-admission', 1, 'delivery', 'approved', 'Run fixture approval', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO delivery_ticket_selections (id, selection_id, workspace_row_id, state, rationale, source_closure_row_id) VALUES (1, 'selection-package-admission', 1, 'consumed', 'Run fixture selection', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO delivery_ticket_selection_members (id, selection_row_id, sequence, revision_row_id, approval_row_id) VALUES (1, 1, 1, 1, 1)`); err != nil {
		t.Fatal(err)
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
	if _, err := db.Exec(`INSERT INTO execution_package_members (id, package_row_id, selection_member_row_id, sequence, revision_row_id, member_sha256) VALUES (1, 1, 1, 1, 1, ?)`, strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO execution_package_approval_bindings (package_row_id, package_member_row_id, approval_row_id, authority_revision_row_id, source_closure_row_id, approval_basis_sha256) VALUES (1, 1, 1, 1, 1, ?)`, strings.Repeat("f", 64)); err != nil {
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
	fixture := newPackageAdmissionFixture(t)

	before := fixture.packageRun
	admitted, err := fixture.service.AdmitPackageExecution(ctx, before.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.ID != before.ID || admitted.RunID != before.RunID || admitted.Status != workflowstore.RunStatusSetupReady {
		t.Fatalf("admitted Run = %#v, before = %#v", admitted, before)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, before)

	repeated, err := fixture.service.AdmitPackageExecution(ctx, before.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != admitted.ID || repeated.RunID != admitted.RunID || repeated.Status != admitted.Status {
		t.Fatalf("repeated admission = %#v, first = %#v", repeated, admitted)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, before)
}

func TestAdmitPackageExecutionConcurrent(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t)
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
	assertPackageAdmissionSideEffects(t, fixture.store, fixture.packageRun)
}

func TestAdmitPackageExecutionAcceptsExecuting(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t)
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
	fixture := newPackageAdmissionFixture(t)
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
	fixture := newPackageAdmissionFixture(t)

	completed, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.ID != fixture.packageRun.ID || completed.RunID != fixture.packageRun.RunID || completed.Status != workflowstore.RunStatusValidating {
		t.Fatalf("completed Run = %#v, before = %#v", completed, fixture.packageRun)
	}
	assertPackageAdmissionSideEffects(t, fixture.store, fixture.packageRun)
}

func TestCompletePackageDeterministicExecutionIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t)

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
	fixture := newPackageAdmissionFixture(t)
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
	fixture := newPackageAdmissionFixture(t)
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
}

func TestCompletePackageDeterministicExecutionRejectsInvalidInputAndStates(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t)
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
			fixture := newPackageAdmissionFixture(t)
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
			fixture := newPackageAdmissionFixture(t)
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

func TestCompletePackageDeterministicExecutionRollsBackFirstTransitionFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t)
	if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_package_finalization_first_transition
        BEFORE UPDATE OF status ON runs WHEN OLD.status = 'setup_ready' AND NEW.status = 'executing'
        BEGIN SELECT RAISE(ABORT, 'forced package finalization first transition failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
		t.Fatal("first transition failure was ignored")
	}
	assertPackageFinalizationRunStatus(t, fixture.store, fixture.packageRun.RunID, workflowstore.RunStatusSetupReady)
}

func TestCompletePackageDeterministicExecutionRollsBackSecondTransitionFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t)
	if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_package_finalization_second_transition
        BEFORE UPDATE OF status ON runs WHEN OLD.status = 'executing' AND NEW.status = 'validating'
        BEGIN SELECT RAISE(ABORT, 'forced package finalization second transition failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.service.CompletePackageDeterministicExecution(ctx, fixture.packageRun.RunID); err == nil {
		t.Fatal("second transition failure was ignored")
	}
	assertPackageFinalizationRunStatus(t, fixture.store, fixture.packageRun.RunID, workflowstore.RunStatusSetupReady)
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

func TestAdmitPackageExecutionRejectsStaleSourceWithoutEffects(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageAdmissionFixture(t)
	if _, err := fixture.store.DB().Exec(`UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'source_commit_missing', verified_at = NULL WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.AdmitPackageExecution(ctx, fixture.packageRun.RunID); err == nil {
		t.Fatal("admission from unavailable source closure succeeded")
	}
	var attempts int
	if err := fixture.store.DB().QueryRow(`SELECT COUNT(*) FROM execution_attempts WHERE run_row_id = ?`, fixture.packageRun.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("execution attempts after stale admission = %d, want 0", attempts)
	}
}
