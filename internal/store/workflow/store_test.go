package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/testsupport/workflowfixture"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowgenerated "relay/internal/store/workflowgenerated"
)

func TestFreshWorkflowDatabaseContainsOnlyWorkflowTables(t *testing.T) {
	store, _ := openWorkflowTestStore(t)
	rows, err := store.DB().Query(`
SELECT name
FROM sqlite_master
WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"artifacts",
		"audit_decisions",
		"audit_packet_ticket_obligations",
		"audit_packets",
		"audit_remediation_seed_findings",
		"audit_remediation_seed_reopenings",
		"audit_remediation_seeds",
		"audit_ticket_revision_decisions",
		"delivery_ticket_production_links",
		"delivery_ticket_revision_approvals",
		"delivery_ticket_revision_dependencies",
		"delivery_ticket_revision_members",
		"delivery_ticket_revision_satisfactions",
		"delivery_ticket_revisions",
		"delivery_ticket_selection_members",
		"delivery_ticket_selections",
		"delivery_tickets",
		"execution_attempts",
		"execution_package_approval_bindings",
		"execution_package_approvals",
		"execution_package_members",
		"execution_packages",
		"feature_workspace_admitted_inputs",
		"feature_workspace_authority_layers",
		"feature_workspace_authority_revisions",
		"feature_workspace_completion_decisions",
		"feature_workspace_completion_reopenings",
		"feature_workspace_destinations",
		"feature_workspace_discovery_adoptions",
		"feature_workspace_discovery_artifacts",
		"feature_workspace_discovery_closure_packet_members",
		"feature_workspace_discovery_closure_packets",
		"feature_workspace_discovery_destination_assessments",
		"feature_workspace_discovery_integration_consequences",
		"feature_workspace_discovery_reopen_events",
		"feature_workspace_discovery_tickets",
		"feature_workspace_discovery_work_item_metadata",
		"feature_workspace_integrated_discovery_revisions",
		"feature_workspace_investigations",
		"feature_workspace_prototype_approvals",
		"feature_workspace_prototype_authorizations",
		"feature_workspace_prototype_cleanup_obligations",
		"feature_workspace_prototype_cleanup_reconciliations",
		"feature_workspace_prototype_evidence_import_batches",
		"feature_workspace_prototype_evidence_members",
		"feature_workspace_prototype_launch_claims",
		"feature_workspace_prototype_leases",
		"feature_workspace_prototype_lifecycle_transitions",
		"feature_workspace_prototype_proposals",
		"feature_workspace_prototype_qa_admissions",
		"feature_workspace_prototype_qa_evidence",
		"feature_workspace_prototype_qa_packet_members",
		"feature_workspace_prototype_qa_packets",
		"feature_workspace_prototype_result_members",
		"feature_workspace_prototype_results",
		"feature_workspace_prototype_runs",
		"feature_workspace_prototype_runtimes",
		"feature_workspace_prototype_targets",
		"feature_workspace_route_states",
		"feature_workspace_ticket_dependencies",
		"feature_workspace_ticket_resolutions",
		"feature_workspaces",
		"goose_db_version",
		"governing_artifact_approvals",
		"mcp_mutation_results",
		"operation_packet_artifact_bindings",
		"operation_packet_artifacts",
		"operation_packet_publications",
		"operation_packet_retained_artifacts",
		"operation_packet_retention_dependencies",
		"operation_packet_vault_relationships",
		"operation_packets",
		"plan_pass_dependencies",
		"plan_passes",
		"plan_repository_targets",
		"planning_candidate_approvals",
		"planning_candidates",
		"plans",
		"project_notes",
		"project_repository_targets",
		"projects",
		"repository_branch_mutation_leases",
		"repository_targets",
		"runs",
		"source_index_generations",
		"source_path_selectors",
		"source_vault_closures",
		"source_vault_retentions",
		"source_vaults",
		"ticket_design_brief_approvals",
		"ticket_design_briefs",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected fresh workflow tables\ngot:  %v\nwant: %v", got, want)
	}
}

func TestGeneratedCreatePlanPersistsRequiredProjectAssociation(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)

	var projectRowID int64
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO projects (project_id, name, description)
VALUES (?, ?, ?)
RETURNING id`,
		"project-00000000-0000-0000-0000-000000000099",
		"Generated CreatePlan",
		"Project used to verify the generated Plan query.",
	).Scan(&projectRowID); err != nil {
		t.Fatal(err)
	}

	queries := workflowgenerated.New(store.DB())
	canonicalSHA := strings.Repeat("a", 64)

	if _, err := queries.CreatePlan(ctx, workflowgenerated.CreatePlanParams{
		ProjectRowID:    projectRowID + 1000,
		PlanID:          "plan-generated-invalid-project",
		FeatureSlug:     "generated-invalid-project",
		CanonicalSha256: canonicalSHA,
	}); err == nil {
		t.Fatal("generated CreatePlan accepted an unknown Project row")
	}
	var invalidCount int
	if err := store.DB().QueryRowContext(ctx, `
SELECT COUNT(*)
FROM plans
WHERE plan_id = ?`, "plan-generated-invalid-project").Scan(&invalidCount); err != nil {
		t.Fatal(err)
	}
	if invalidCount != 0 {
		t.Fatalf("invalid generated Plan rows = %d, want 0", invalidCount)
	}

	created, err := queries.CreatePlan(ctx, workflowgenerated.CreatePlanParams{
		ProjectRowID:    projectRowID,
		PlanID:          "plan-generated-project-association",
		FeatureSlug:     "generated-project-association",
		CanonicalSha256: canonicalSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ProjectRowID != projectRowID {
		t.Fatalf("created ProjectRowID = %d, want %d", created.ProjectRowID, projectRowID)
	}
	if created.Status != PlanStatusActive {
		t.Fatalf("created status = %q, want %q", created.Status, PlanStatusActive)
	}

	var storedProjectRowID int64
	if err := store.DB().QueryRowContext(ctx, `
SELECT project_row_id
FROM plans
WHERE plan_id = ?`, created.PlanID).Scan(&storedProjectRowID); err != nil {
		t.Fatal(err)
	}
	if storedProjectRowID != projectRowID {
		t.Fatalf("stored ProjectRowID = %d, want %d", storedProjectRowID, projectRowID)
	}
}

func TestDatabaseConstraintsRejectInvalidRelationshipsAndTransitions(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)

	seed := seedConstraintRecords(t, ctx, store)

	if _, err := store.DB().Exec(`
INSERT INTO plan_pass_dependencies (pass_row_id, depends_on_pass_row_id)
VALUES (?, ?)`, seed.secondPlanPass.ID, seed.firstPass.ID); err == nil {
		t.Fatal("cross-plan dependency was accepted")
	}
	if _, err := store.DB().Exec(`
INSERT INTO plan_pass_dependencies (pass_row_id, depends_on_pass_row_id)
VALUES (?, ?)`, seed.secondPass.ID, seed.firstPass.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
UPDATE plan_passes
SET status = 'in_progress', started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?`, seed.secondPass.ID); err == nil {
		t.Fatal("dependent pass started before dependency completion")
	}
	if _, err := store.DB().Exec(`
UPDATE plans
SET status = 'completed', completed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?`, seed.firstPlan.ID); err == nil {
		t.Fatal("Plan completed with incomplete passes")
	}

	if _, err := store.DB().Exec(`
INSERT INTO runs (
    run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id,
    status, branch, base_commit, canonical_sha256
)
VALUES (?, ?, ?, ?, ?, 'created', ?, ?, ?)`,
		"run-mismatch",
		"feature",
		"other",
		seed.firstPlan.ID,
		seed.firstPass.ID,
		"main",
		strings.Repeat("a", 40),
		strings.Repeat("b", 64),
	); err == nil {
		t.Fatal("mismatched Plan/pass/repository association was accepted")
	}

	original := createConstraintRun(t, ctx, store, "run-original", seed.firstPlan.ID, seed.firstPass.ID)
	if _, err := store.DB().Exec(`
INSERT INTO runs (
    run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id,
    remediates_run_row_id, status, branch, base_commit, canonical_sha256
)
VALUES (?, ?, ?, ?, ?, ?, 'created', ?, ?, ?)`,
		"run-invalid-remediation",
		"feature",
		"relay",
		seed.firstPlan.ID,
		seed.firstPass.ID,
		original.ID,
		"main",
		strings.Repeat("a", 40),
		strings.Repeat("c", 64),
	); err == nil {
		t.Fatal("non-needs_revision run was accepted as a remediation source")
	}

	if _, err := store.DB().Exec(`
INSERT INTO execution_attempts (attempt_id, run_row_id, attempt_number, adapter, model)
VALUES (?, ?, 1, 'adapter', 'model')`, "attempt-invalid", original.ID); err == nil {
		t.Fatal("execution attempt was created while run was not executing")
	}

	if _, err := store.DB().Exec(`
INSERT INTO artifacts (
    artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes
)
VALUES (?, 'plan', ?, 'audit_packet', ?, 'application/json', ?, 1)`,
		"artifact-wrong-owner",
		seed.firstPlan.ID,
		"plans/plan-one/audit.json",
		strings.Repeat("d", 64),
	); err != nil {
		t.Fatal(err)
	}
	var artifactRowID int64
	if err := store.DB().QueryRow(`SELECT id FROM artifacts WHERE artifact_id = 'artifact-wrong-owner'`).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO audit_decisions (
    audit_decision_id, run_row_id, audit_packet_artifact_row_id,
    audited_commit, packet_sha256, decision
)
VALUES (?, ?, ?, ?, ?, 'accepted')`,
		"audit-invalid",
		original.ID,
		artifactRowID,
		strings.Repeat("e", 40),
		strings.Repeat("d", 64),
	); err == nil {
		t.Fatal("audit decision accepted a packet not owned by the audited run")
	}
}

func TestCommitArtifactBatchPreparationFailureRollsBackDatabaseAndArtifacts(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	batch, err := store.ArtifactStore().Begin("feature-discovery/workspace-preparation-failure/artifact-1")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("integrated_discovery", "discovery.md", "text/markdown", []byte("# staged\n"))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("injected artifact preparation failure")
	store.artifactBatchHooks = &artifactBatchHooks{prepareCommit: func(*workflowartifacts.Batch) error { return sentinel }}
	err = store.CommitArtifactBatch(ctx, batch, func(tx *Tx) error {
		_, err := tx.CreateRepositoryTarget(ctx, "preparation-failure-target", t.TempDir())
		return err
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("preparation failure = %v", err)
	}
	if _, err := store.GetRepositoryTarget(ctx, "preparation-failure-target"); err == nil {
		t.Fatal("database mutation survived preparation failure")
	}
	if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(file.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("promoted artifact survived preparation failure: %v", err)
	}
}

func TestCommitArtifactBatchRollsBackDatabaseAndFilesystemTogether(t *testing.T) {
	ctx := context.Background()

	t.Run("callback failure", func(t *testing.T) {
		store, root := openWorkflowTestStore(t)
		batch, err := store.ArtifactStore().Begin("plans/plan-callback")
		if err != nil {
			t.Fatal(err)
		}
		file, err := batch.Stage("canonical_plan", "feature.plan.json", "application/json", []byte("{}\n"))
		if err != nil {
			t.Fatal(err)
		}
		err = store.CommitArtifactBatch(ctx, batch, func(tx *Tx) error {
			if _, err := tx.CreateRepositoryTarget(ctx, "relay", t.TempDir()); err != nil {
				return err
			}
			return errors.New("injected callback failure")
		})
		if err == nil {
			t.Fatal("expected callback failure")
		}
		assertWorkflowCount(t, store.DB(), "repository_targets", 0)
		if _, err := os.Stat(filepath.Join(root, "artifacts", filepath.FromSlash(file.RelativePath))); !os.IsNotExist(err) {
			t.Fatalf("artifact survived callback rollback: %v", err)
		}
	})

	t.Run("promotion failure", func(t *testing.T) {
		store, root := openWorkflowTestStore(t)
		batch, err := store.ArtifactStore().Begin("runs/run-promotion")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := batch.Stage("execution_spec", "feature.execution-spec.json", "application/json", []byte("{}\n")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "artifacts", "runs"), []byte("block directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		err = store.CommitArtifactBatch(ctx, batch, func(tx *Tx) error {
			_, err := tx.CreateRepositoryTarget(ctx, "relay", t.TempDir())
			return err
		})
		if err == nil {
			t.Fatal("expected promotion failure")
		}
		assertWorkflowCount(t, store.DB(), "repository_targets", 0)
	})

	t.Run("success", func(t *testing.T) {
		store, root := openWorkflowTestStore(t)
		batch, err := store.ArtifactStore().Begin("plans/plan-success")
		if err != nil {
			t.Fatal(err)
		}
		file, err := batch.Stage("canonical_plan", "feature.plan.json", "application/json", []byte("{}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CommitArtifactBatch(ctx, batch, func(tx *Tx) error {
			_, err := tx.CreateRepositoryTarget(ctx, "relay", t.TempDir())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		assertWorkflowCount(t, store.DB(), "repository_targets", 1)
		if _, err := os.Stat(filepath.Join(root, "artifacts", filepath.FromSlash(file.RelativePath))); err != nil {
			t.Fatal(err)
		}
	})
}

type constraintSeed struct {
	firstPlan      Plan
	firstPass      PlanPass
	secondPass     PlanPass
	secondPlan     Plan
	secondPlanPass PlanPass
}

func seedConstraintRecords(t *testing.T, ctx context.Context, store *Store) constraintSeed {
	t.Helper()
	var seed constraintSeed
	if err := store.WithTx(ctx, func(tx *Tx) error {
		for _, target := range []string{"relay", "other"} {
			if _, err := tx.CreateRepositoryTarget(ctx, target, filepath.Join(t.TempDir(), target)); err != nil {
				return err
			}
		}
		project, err := tx.CreateProject(ctx, CreateProjectParams{
			ProjectID: "project-constraints",
			Name:      "Constraint tests",
		})
		if err != nil {
			return err
		}
		seed.firstPlan, err = tx.CreatePlan(ctx, CreatePlanParams{
			ProjectRowID:    project.ID,
			PlanID:          "plan-one",
			FeatureSlug:     "feature",
			CanonicalSHA256: strings.Repeat("a", 64),
		})
		if err != nil {
			return err
		}
		if _, err := tx.CreatePlanRepositoryTarget(ctx, CreatePlanRepositoryTargetParams{
			PlanRowID:          seed.firstPlan.ID,
			Sequence:           1,
			RepoTarget:         "relay",
			Branch:             "main",
			PlanningBaseCommit: strings.Repeat("a", 40),
		}); err != nil {
			return err
		}
		seed.firstPass, err = tx.CreatePlanPass(ctx, CreatePlanPassParams{
			PassID: "pass-one", PlanRowID: seed.firstPlan.ID, PassNumber: 1, Name: "One", RepoTarget: "relay",
		})
		if err != nil {
			return err
		}
		seed.secondPass, err = tx.CreatePlanPass(ctx, CreatePlanPassParams{
			PassID: "pass-two", PlanRowID: seed.firstPlan.ID, PassNumber: 2, Name: "Two", RepoTarget: "relay",
		})
		if err != nil {
			return err
		}

		seed.secondPlan, err = tx.CreatePlan(ctx, CreatePlanParams{
			ProjectRowID:    project.ID,
			PlanID:          "plan-two",
			FeatureSlug:     "other-feature",
			CanonicalSHA256: strings.Repeat("b", 64),
		})
		if err != nil {
			return err
		}
		if _, err := tx.CreatePlanRepositoryTarget(ctx, CreatePlanRepositoryTargetParams{
			PlanRowID:          seed.secondPlan.ID,
			Sequence:           1,
			RepoTarget:         "relay",
			Branch:             "main",
			PlanningBaseCommit: strings.Repeat("b", 40),
		}); err != nil {
			return err
		}
		seed.secondPlanPass, err = tx.CreatePlanPass(ctx, CreatePlanPassParams{
			PassID: "pass-other-plan", PlanRowID: seed.secondPlan.ID, PassNumber: 1, Name: "Other", RepoTarget: "relay",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return seed
}

func createConstraintRun(t *testing.T, ctx context.Context, store *Store, runID string, planRowID, passRowID int64) Run {
	t.Helper()
	var run Run
	if err := store.WithTx(ctx, func(tx *Tx) error {
		pass, err := tx.GetPlanPassByRowID(ctx, passRowID)
		if err != nil {
			return err
		}
		if pass.Status == PassStatusPlanned {
			if _, err := tx.TransitionPlanPass(ctx, pass.PassID, PassStatusPlanned, PassStatusInProgress); err != nil {
				return err
			}
		}
		run, err = tx.CreateRun(ctx, CreateRunParams{
			RunID:           runID,
			FeatureSlug:     "feature",
			RepoTarget:      "relay",
			PlanRowID:       sql.NullInt64{Int64: planRowID, Valid: true},
			PlanPassRowID:   sql.NullInt64{Int64: passRowID, Valid: true},
			Status:          RunStatusCreated,
			Branch:          "main",
			BaseCommit:      strings.Repeat("a", 40),
			CanonicalSHA256: strings.Repeat("c", 64),
		})
		if err != nil {
			return err
		}
		run, err = tx.TransitionRun(ctx, run.RunID, RunStatusCreated, RunStatusSetupReady)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func openWorkflowTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	store := workflowfixture.Open(t, Open)
	return store, filepath.Dir(store.ArtifactStore().Root())
}

func assertWorkflowCount(t *testing.T, db *sql.DB, table string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s count = %d, want %d", table, got, want)
	}
}

func prototypeRuntimeFixture(t *testing.T) (*Store, PrototypeRun, PrototypeRuntime, PrototypeTarget, PrototypeLease) {
	t.Helper()
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	db := store.DB()
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	triggerRows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'prototype_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for triggerRows.Next() {
		var name string
		if err := triggerRows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, name)
	}
	if err := triggerRows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range triggers {
		if _, err := db.Exec(`DROP TRIGGER ` + name); err != nil {
			t.Fatal(err)
		}
	}
	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const tree = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := db.Exec(`INSERT INTO feature_workspaces(id,workspace_id,project_row_id,feature_slug) VALUES(1,'workspace-runtime',1,'runtime'); INSERT INTO feature_workspace_discovery_tickets(id,discovery_ticket_id,workspace_row_id,ticket_key,subject) VALUES(1,'discovery-runtime',1,'runtime','runtime'); INSERT INTO feature_workspace_prototype_authorizations(id,authorization_id,proposal_row_id,proposed_run_id,workspace_row_id,workspace_version,work_item_row_id,work_item_version,discovery_revision_row_id,proposal_sha256,source_closure_row_id,source_commit,source_tree,repo_target,base_commit,adapter,model,variants_json,evidence_obligations_json,limits_json,invocation_artifact_row_id,invocation_sha256,invocation_size_bytes,invocation_media_type) VALUES(1,'prototype-authorization-runtime',1,'prototype-run-runtime',1,1,1,1,1,?,1,?,?, 'runtime-repo',?,'adapter','model','[]','[]','{}',1,?,1,'application/json'); INSERT INTO feature_workspace_prototype_runs(id,prototype_run_id,authorization_row_id,workspace_row_id,work_item_row_id,lifecycle_state,version) VALUES(1,'prototype-run-runtime',1,1,1,'approved',1)`, strings.Repeat("a", 64), commit, tree, commit, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	runtime := PrototypeRuntime{RuntimeID: "prototype-runtime-runtime", AuthorizedCommit: commit, AuthorizedTree: tree, RuntimeRootPath: "C:/runtime", WorktreePath: "C:/runtime/worktree", EphemeralTargetKey: "prototype:runtime", LeaseToken: "prototype-lease-runtime", BackgroundContextID: "context-runtime", DeadlineAt: "2026-01-01T00:00:00Z"}
	target := PrototypeTarget{TargetID: "prototype-target-runtime", TargetKey: runtime.EphemeralTargetKey, WorktreePath: runtime.WorktreePath, AuthorizedCommit: commit, AuthorizedTree: tree}
	lease := PrototypeLease{LeaseToken: runtime.LeaseToken, EphemeralTargetKey: runtime.EphemeralTargetKey, OwnerInstanceID: "test-owner"}
	var run PrototypeRun
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		run, runtime, target, lease, err = tx.ReservePrototypeRuntime(ctx, "prototype-run-runtime", 1, runtime, target, lease)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return store, run, runtime, target, lease
}

func TestPrototypeRuntimeMigrationAndOwnership(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	db := store.DB()
	for _, table := range []string{"feature_workspace_prototype_runtimes", "feature_workspace_prototype_targets", "feature_workspace_prototype_leases", "feature_workspace_prototype_evidence_import_batches", "feature_workspace_prototype_results", "feature_workspace_prototype_evidence_members"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}
	for _, trigger := range []string{"prototype_runtime_authorization_guard", "prototype_target_binding_guard", "prototype_lease_binding_guard", "prototype_result_binding_guard", "prototype_evidence_binding_guard", "prototype_result_member_binding_guard", "prototype_result_member_immutable"} {
		var sqlText string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, trigger).Scan(&sqlText); err != nil || strings.TrimSpace(sqlText) == "" {
			t.Fatalf("trigger %s missing: %v", trigger, err)
		}
	}
	store, run, runtime, target, lease := prototypeRuntimeFixture(t)
	if run.LifecycleState != "approved" || runtime.RunRowID != run.ID || target.RuntimeRowID != runtime.ID || lease.RuntimeRowID != runtime.ID {
		t.Fatalf("reserved ownership invalid: run=%#v runtime=%#v target=%#v lease=%#v", run, runtime, target, lease)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_prototype_targets SET run_row_id=run_row_id+100 WHERE id=?`, target.ID); err == nil {
		t.Fatal("cross-run target ownership mutation succeeded")
	}
	if lease.LeaseToken != runtime.LeaseToken || target.TargetKey != runtime.EphemeralTargetKey {
		t.Fatalf("runtime binding values diverged: %#v %#v %#v", runtime, target, lease)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_prototype_result_members SET member_kind='changed' WHERE id=-1`); err != nil {
		t.Fatal(err)
	}
	var productionTargets int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM repository_targets`).Scan(&productionTargets); err != nil || productionTargets != 0 {
		t.Fatalf("runtime migration mutated production targets: %d %v", productionTargets, err)
	}
}

func TestPrototypeRuntimeStateMutations(t *testing.T) {
	ctx := context.Background()
	store, run, runtime, target, lease := prototypeRuntimeFixture(t)
	claimID := "prototype-launch-claim-test"
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, _, err := tx.MarkPrototypePreparationReady(ctx, run.PrototypeRunID, run.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetPrototypeRun(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	var claim PrototypeLaunchClaim
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		run, claim, err = tx.ClaimPrototypeLaunch(ctx, run.PrototypeRunID, run.Version, claimID, "test")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run.LifecycleState != "preparing" || claim.LaunchClaimID != claimID {
		t.Fatalf("claim=%#v run=%#v", claim, run)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		got, _, err := tx.ClaimPrototypeLaunch(ctx, run.PrototypeRunID, run.Version, claimID, "test")
		if err != nil || got.Version != run.Version {
			return errors.New("launch claim replay was not idempotent")
		}
		_, _, err = tx.ClaimPrototypeLaunch(ctx, run.PrototypeRunID, run.Version, "other", "test")
		if !errors.Is(err, ErrPrototypeLaunchAlreadyClaimed) {
			return errors.New("conflicting claim was accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	identity := `{"pid":42,"started_at":"2026-01-01T00:00:00Z","platform":"linux"}`
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		run, runtime, err = tx.PersistPrototypeProcessIdentity(ctx, run.PrototypeRunID, run.Version, identity, "2026-01-01T00:00:00Z")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run.LifecycleState != "running" || runtime.LaunchPhase != "identity_persisted" || !runtime.ProcessIdentity.Valid || runtime.ProcessIdentity.String != identity {
		t.Fatalf("identity persistence=%#v %#v", run, runtime)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		if _, _, err := tx.RequestPrototypeCancellation(ctx, run.PrototypeRunID, run.Version, "cancel-one"); err != nil {
			return err
		}
		if _, _, err := tx.RequestPrototypeCancellation(ctx, run.PrototypeRunID, run.Version, "cancel-one"); err != nil {
			return err
		}
		if _, _, err := tx.RequestPrototypeCancellation(ctx, run.PrototypeRunID, run.Version, "cancel-two"); !errors.Is(err, ErrPrototypeMutationConflict) {
			return errors.New("cancel conflict not rejected")
		}
		if _, _, err := tx.ClaimPrototypeTimeout(ctx, run.PrototypeRunID, run.Version, "timeout-one"); err != nil {
			return err
		}
		if _, _, err := tx.ClaimPrototypeTimeout(ctx, run.PrototypeRunID, run.Version, "timeout-two"); !errors.Is(err, ErrPrototypeMutationConflict) {
			return errors.New("timeout conflict not rejected")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		run, runtime, err = tx.SettlePrototypeProcess(ctx, run.PrototypeRunID, run.Version, "cleanup_required", "unknown", "settle-test")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run.LifecycleState != "cleanup_required" || runtime.LaunchPhase != "ownership_unresolved" {
		t.Fatalf("unresolved settlement=%#v %#v", run, runtime)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.ReleasePrototypeTarget(ctx, run.PrototypeRunID, target.TargetKey, "2026-01-01T00:00:00Z")
		if err != nil {
			return err
		}
		_, err = tx.ReleasePrototypeLease(ctx, run.PrototypeRunID, lease.LeaseToken, "2026-01-01T00:00:00Z")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var targetStatus, leaseStatus string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM feature_workspace_prototype_targets WHERE id=?`, target.ID).Scan(&targetStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM feature_workspace_prototype_leases WHERE id=?`, lease.ID).Scan(&leaseStatus); err != nil {
		t.Fatal(err)
	}
	if targetStatus != "released" || leaseStatus != "released" {
		t.Fatalf("resource release not durable: target=%s lease=%s", targetStatus, leaseStatus)
	}
}

func TestPrototypePart3MigrationAndConstraints(t *testing.T) {
	store, _ := openWorkflowTestStore(t)
	ctx := context.Background()
	for _, table := range []string{
		"feature_workspace_prototype_cleanup_reconciliations",
		"feature_workspace_prototype_qa_packets",
		"feature_workspace_prototype_qa_packet_members",
		"feature_workspace_prototype_qa_evidence",
		"feature_workspace_prototype_qa_admissions",
	} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("Part 3 table %s count=%d err=%v", table, count, err)
		}
	}
	var legacy int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='feature_workspace_prototype_qa_associations'`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy != 0 {
		t.Fatal("legacy reconciliation-owned QA association table remains")
	}
	for _, trigger := range []string{"prototype_qa_packet_immutable", "prototype_qa_packet_member_ownership_guard", "prototype_qa_evidence_ownership_guard", "prototype_qa_admission_ownership_guard"} {
		var definition string
		if err := store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, trigger).Scan(&definition); err != nil || strings.TrimSpace(definition) == "" {
			t.Fatalf("Part 3 trigger %s missing: %v", trigger, err)
		}
	}
	var guard string
	if err := store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name='prototype_qa_packet_member_ownership_guard'`).Scan(&guard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(guard, "a.sha256=NEW.sha256") || !strings.Contains(guard, "a.size_bytes=NEW.size_bytes") {
		t.Fatalf("packet member guard does not bind exact artifact facts: %s", guard)
	}
}

func TestPrototypeCleanupStoreMutations(t *testing.T) {
	ctx := context.Background()
	store, run, _, _, _ := prototypeRuntimeFixture(t)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreatePrototypeCleanupReconciliation(ctx, PrototypeCleanupReconciliation{
			RunRowID: run.ID, ExpectedRunVersion: run.Version, MutationIdentity: "store-reconciliation", TriggerKind: "explicit",
			ObservedRunState: "approved", ProcessOwnershipStatus: "pending", EvidenceSettlementStatus: "pending", WorktreeStatus: "pending",
			EphemeralTargetStatus: "pending", PrototypeLeaseStatus: "pending", ResultingRunState: "cleanup_required",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reconciliations, err := store.ListPrototypeCleanupReconciliations(ctx, run.PrototypeRunID)
	if err != nil || len(reconciliations) != 1 {
		t.Fatalf("reconciliations=%#v err=%v", reconciliations, err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.FailPrototypeCleanupObligation(ctx, run.ID, "process_ownership", "ownership mismatch"); err != nil {
			return err
		}
		_, err := tx.CompletePrototypeCleanupObligation(ctx, run.ID, "process_ownership")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	obligations, err := store.ListPrototypeCleanupObligationsByRunID(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, obligation := range obligations {
		if obligation.ObligationKind == "process_ownership" && obligation.Status != "complete" {
			t.Fatalf("process obligation=%#v", obligation)
		}
	}
}

func TestPrototypeQAPersistence(t *testing.T) {
	ctx := context.Background()
	store, run, _, _, _ := prototypeRuntimeFixture(t)
	var artifact, evidenceArtifact DiscoveryArtifact
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		artifact, err = tx.CreateDiscoveryArtifact(ctx, DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-qa-result", WorkspaceRowID: 1, RelativePath: "feature-discovery/workspace-runtime/qa/result.json", SHA256: strings.Repeat("a", 64), MediaType: "application/json", SizeBytes: 12})
		if err != nil {
			return err
		}
		evidenceArtifact, err = tx.CreateDiscoveryArtifact(ctx, DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-qa-evidence", WorkspaceRowID: 1, RelativePath: "feature-discovery/workspace-runtime/qa/evidence.txt", SHA256: strings.Repeat("b", 64), MediaType: "text/plain", SizeBytes: 7})
		if err != nil {
			return err
		}
		packet, err := tx.CreatePrototypeQAPacket(ctx, PrototypeQAPacket{WorkspaceRowID: 1, RunRowID: run.ID, MutationIdentity: "store-qa-packet", ExpectedRunVersion: run.Version, MemberCount: 1, TotalBytes: 12})
		if err != nil {
			return err
		}
		if _, err = tx.CreatePrototypeQAPacketMember(ctx, PrototypeQAPacketMember{QAPacketRowID: packet.ID, Sequence: 1, MemberKind: "prototype_result", ArtifactRowID: artifact.ID, SHA256: artifact.SHA256, MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes}); err != nil {
			return err
		}
		if _, err = tx.CreatePrototypeQAEvidence(ctx, PrototypeQAEvidence{QAPacketRowID: packet.ID, Sequence: 1, SemanticRole: "operator-note", ArtifactRowID: evidenceArtifact.ID, SHA256: evidenceArtifact.SHA256, MediaType: evidenceArtifact.MediaType, SizeBytes: evidenceArtifact.SizeBytes}); err != nil {
			return err
		}
		admission, err := tx.CreatePrototypeQAAdmission(ctx, PrototypeQAAdmission{QAPacketRowID: packet.ID, MutationIdentity: "store-qa-admission", OperatorConfirmationEvidence: "confirmed", AdmittedMemberCount: 1, AdmittedTotalBytes: evidenceArtifact.SizeBytes})
		if err != nil {
			return err
		}
		if admission.QAAdmissionID == "" {
			return errors.New("admission ID was not generated")
		}
		_, err = tx.MarkPrototypeQAPacketAdmitted(ctx, packet.QAPacketID, "store-qa-admission", "2026-08-05T00:00:00Z")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	packet, err := store.GetPrototypeQAPacketByMutationIdentity(ctx, "store-qa-packet")
	if err != nil || packet.Status != "admitted" {
		t.Fatalf("packet=%#v err=%v", packet, err)
	}
	members, err := store.ListPrototypeQAPacketMembers(ctx, packet.QAPacketID)
	if err != nil || len(members) != 1 || members[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("members=%#v err=%v", members, err)
	}
	evidence, err := store.ListPrototypeQAEvidenceByPacketID(ctx, packet.QAPacketID)
	if err != nil || len(evidence) != 1 || evidence[0].SemanticRole != "operator-note" {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
	admission, err := store.GetPrototypeQAAdmissionByPacketID(ctx, packet.QAPacketID)
	if err != nil || admission.MutationIdentity != "store-qa-admission" {
		t.Fatalf("admission=%#v err=%v", admission, err)
	}
}
