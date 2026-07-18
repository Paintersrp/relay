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
		"audit_packets",
		"execution_attempts",
		"goose_db_version",
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
		"plans",
		"project_notes",
		"project_repository_targets",
		"projects",
		"repository_targets",
		"runs",
		"source_vault_closures",
		"source_vault_retentions",
		"source_vaults",
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
	root := t.TempDir()
	store, err := Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root
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
