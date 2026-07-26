package workflowruns

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

type preparedAdaptiveFixture struct {
	store   *workflowstore.Store
	service *Service
	run     workflowstore.Run
	attempt workflowstore.ExecutionAttempt
	input   BeginPreparedAdaptiveExecutionInput
}

func TestBeginPreparedAdaptiveExecutionAdmitsAndReadsExisting(t *testing.T) {
	ctx := context.Background()
	fixture := newPreparedAdaptiveFixture(t)
	first, err := fixture.service.BeginPreparedAdaptiveExecution(ctx, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.NewlyAdmitted || first.Run.Status != workflowstore.RunStatusExecuting || first.Attempt.Status != workflowstore.AttemptStatusRunning || first.Lease.State != workflowstore.RepositoryBranchMutationLeaseStateActive {
		t.Fatalf("admission = %#v", first)
	}
	if !first.Run.ExecutionPackageRowID.Valid {
		t.Fatal("package-linked Run lost its package link")
	}
	var runtime preparedAdaptiveRuntime
	if err := json.Unmarshal([]byte(first.Attempt.ResultJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.MutationLeaseID != first.Lease.LeaseID || runtime.EffectiveBriefArtifactID != fixture.input.EffectiveBriefArtifactID || runtime.EffectiveBriefSHA256 != fixture.input.EffectiveBriefSHA256 || runtime.SourceMutationStarted {
		t.Fatalf("runtime = %#v", runtime)
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(ctx, fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil || len(leases) != 1 {
		t.Fatalf("leases = %#v, %v", leases, err)
	}

	secondInput := fixture.input
	secondInput.ProposedLeaseID = first.Lease.LeaseID
	secondInput.RunningResultJSON = first.Attempt.ResultJSON
	second, err := fixture.service.BeginPreparedAdaptiveExecution(ctx, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if second.NewlyAdmitted || second.Run.ID != first.Run.ID || second.Attempt.ID != first.Attempt.ID || second.Lease.LeaseID != first.Lease.LeaseID {
		t.Fatalf("existing admission = %#v", second)
	}
	leases, err = fixture.store.ListRepositoryBranchMutationLeases(ctx, fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil || len(leases) != 1 {
		t.Fatalf("existing admission created a lease: %#v, %v", leases, err)
	}
}

func TestBeginPreparedAdaptiveExecutionAdoptsPartialLease(t *testing.T) {
	ctx := context.Background()
	fixture := newPreparedAdaptiveFixture(t)
	fixture.input.EffectiveBriefMode = preparedAdaptiveModeAfterPartialApplication
	fixture.input.ProposedLeaseID = "lease-prepared-partial"
	fixture.input.RunningResultJSON = preparedRuntimeJSON(t, fixture.input)
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryBranchMutationLease(ctx, workflowstore.CreateRepositoryBranchMutationLeaseParams{
			LeaseID: fixture.input.ProposedLeaseID, RepoTarget: fixture.run.RepoTarget, Branch: fixture.run.Branch,
			OwnerKind: runMutationLeaseOwnerKind, OwnerIdentity: fixture.run.RunID,
			UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
			ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	existing, err := fixture.store.GetRepositoryBranchMutationLeaseByLeaseID(ctx, fixture.input.ProposedLeaseID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := fixture.service.BeginPreparedAdaptiveExecution(ctx, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.NewlyAdmitted || first.Lease.LeaseID != existing.LeaseID || first.Lease.CreatedAt != existing.CreatedAt {
		t.Fatalf("admission = %#v, existing = %#v", first, existing)
	}
	var runtime preparedAdaptiveRuntime
	if err := json.Unmarshal([]byte(first.Attempt.ResultJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if !runtime.SourceMutationStarted || runtime.MutationLeaseID != existing.LeaseID {
		t.Fatalf("runtime = %#v", runtime)
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(ctx, fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil || len(leases) != 1 {
		t.Fatalf("leases = %#v, %v", leases, err)
	}

	repeat := fixture.input
	repeat.RunningResultJSON = first.Attempt.ResultJSON
	repeat.ProposedLeaseID = first.Lease.LeaseID
	second, err := fixture.service.BeginPreparedAdaptiveExecution(ctx, repeat)
	if err != nil || second.NewlyAdmitted || second.Lease.LeaseID != existing.LeaseID {
		t.Fatalf("repeat = %#v, %v", second, err)
	}
}

func TestBeginPreparedAdaptiveExecutionRejectsInvalidPartialLeaseHandoff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *preparedAdaptiveFixture)
		want   error
	}{
		{name: "missing active lease", mutate: func(*testing.T, *preparedAdaptiveFixture) {}},
		{name: "different proposed lease ID", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			seedPreparedPartialLease(t, f)
			f.input.ProposedLeaseID = "lease-not-the-active-one"
			f.input.RunningResultJSON = preparedRuntimeJSON(t, f.input)
		}},
		{name: "other owner", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			if err := f.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
				_, err := tx.CreateRepositoryBranchMutationLease(context.Background(), workflowstore.CreateRepositoryBranchMutationLeaseParams{
					LeaseID: "lease-partial-other-owner", RepoTarget: f.run.RepoTarget, Branch: f.run.Branch,
					OwnerKind: "other", OwnerIdentity: "run-other",
					UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
					ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired,
				})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "uncertain lease", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			seedPreparedPartialLease(t, f)
			markPreparedLeaseUncertain(t, f)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPreparedAdaptiveFixture(t)
			fixture.input.EffectiveBriefMode = preparedAdaptiveModeAfterPartialApplication
			fixture.input.ProposedLeaseID = "lease-prepared-partial"
			fixture.input.RunningResultJSON = preparedRuntimeJSON(t, fixture.input)
			tc.mutate(t, fixture)
			_, err := fixture.service.BeginPreparedAdaptiveExecution(context.Background(), fixture.input)
			want := tc.want
			if want == nil {
				want = ErrPreparedAdaptiveExecutionConflict
				if tc.name == "other owner" {
					want = ErrMutationLeaseConflict
				}
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			expectedLeases := 0
			if tc.name != "missing active lease" {
				expectedLeases = 1
			}
			assertPreparedAdmissionUnchanged(t, fixture, expectedLeases)
		})
	}
}

func TestBeginPreparedAdaptiveExecutionPartialLeaseRollbackPreservesLease(t *testing.T) {
	fixture := newPreparedAdaptiveFixture(t)
	fixture.input.EffectiveBriefMode = preparedAdaptiveModeAfterPartialApplication
	fixture.input.ProposedLeaseID = "lease-prepared-partial"
	fixture.input.RunningResultJSON = preparedRuntimeJSON(t, fixture.input)
	seedPreparedPartialLease(t, fixture)
	before, err := fixture.store.GetRepositoryBranchMutationLeaseByLeaseID(context.Background(), fixture.input.ProposedLeaseID)
	if err != nil {
		t.Fatal(err)
	}
	createAdmissionFailureTrigger(t, fixture.store, "runs", "BEFORE UPDATE OF status", "NEW.run_id = 'run-prepared-adaptive'", "fail Run transition")
	if _, err := fixture.service.BeginPreparedAdaptiveExecution(context.Background(), fixture.input); err == nil {
		t.Fatal("expected transaction failure")
	}
	after, err := fixture.store.GetRepositoryBranchMutationLeaseByLeaseID(context.Background(), fixture.input.ProposedLeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("lease changed across rollback: before=%#v after=%#v", before, after)
	}
	assertPreparedAdmissionUnchanged(t, fixture, 1)
}

func TestBeginPreparedAdaptiveExecutionRollsBackEachStage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, fixture *preparedAdaptiveFixture)
		want    error
	}{
		{
			name: "repository lease conflict",
			prepare: func(t *testing.T, fixture *preparedAdaptiveFixture) {
				t.Helper()
				if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
					_, err := tx.CreateRepositoryBranchMutationLease(context.Background(), workflowstore.CreateRepositoryBranchMutationLeaseParams{
						LeaseID: "lease-other-owner", RepoTarget: fixture.run.RepoTarget, Branch: fixture.run.Branch,
						OwnerKind: "other", OwnerIdentity: "run-other-owner",
						UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
						ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired,
					})
					return err
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrMutationLeaseConflict,
		},
		{
			name: "Run transition",
			prepare: func(t *testing.T, fixture *preparedAdaptiveFixture) {
				t.Helper()
				createAdmissionFailureTrigger(t, fixture.store, "runs", "BEFORE UPDATE OF status", "NEW.run_id = 'run-prepared-adaptive'", "fail Run transition")
			},
		},
		{
			name: "cutover crossing",
			prepare: func(t *testing.T, fixture *preparedAdaptiveFixture) {
				t.Helper()
				seedPreparedAdaptiveCutover(t, fixture.store)
				createAdmissionFailureTrigger(t, fixture.store, "cutover_activations", "BEFORE UPDATE OF execution_boundary_status", "NEW.cutover_activation_id = 'cutover-prepared-adaptive'", "fail cutover crossing")
			},
		},
		{
			name: "attempt transition",
			prepare: func(t *testing.T, fixture *preparedAdaptiveFixture) {
				t.Helper()
				createAdmissionFailureTrigger(t, fixture.store, "execution_attempts", "BEFORE UPDATE OF status", "NEW.attempt_id = 'attempt-prepared-adaptive'", "fail attempt transition")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPreparedAdaptiveFixture(t)
			tc.prepare(t, fixture)
			_, err := fixture.service.BeginPreparedAdaptiveExecution(context.Background(), fixture.input)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("admission error = %v", err)
			}
			if tc.want == ErrMutationLeaseConflict {
				assertPreparedAdmissionUnchanged(t, fixture, 1)
			} else {
				assertPreparedAdmissionUnchanged(t, fixture, 0)
			}
		})
	}
}

func TestBeginPreparedAdaptiveExecutionRejectsInvalidDurableState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, fixture *preparedAdaptiveFixture)
		want   error
	}{
		{name: "malformed runtime JSON", mutate: func(_ *testing.T, f *preparedAdaptiveFixture) { f.input.RunningResultJSON = "{" }},
		{name: "wrong Run identity", mutate: func(_ *testing.T, f *preparedAdaptiveFixture) { f.input.RunRowID++ }},
		{name: "wrong attempt identity", mutate: func(_ *testing.T, f *preparedAdaptiveFixture) { f.input.AttemptRowID++ }},
		{name: "wrong adapter", mutate: func(_ *testing.T, f *preparedAdaptiveFixture) { f.input.Adapter = "other" }},
		{name: "wrong model", mutate: func(_ *testing.T, f *preparedAdaptiveFixture) { f.input.Model = "other" }},
		{name: "wrong artifact owner", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			t.Helper()
			_, err := f.store.DB().Exec(`UPDATE artifacts SET owner_type = 'run', run_row_id = ?, execution_attempt_row_id = NULL WHERE id = ?`, f.run.ID, f.input.InputArtifactRowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong artifact digest", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			t.Helper()
			_, err := f.store.DB().Exec(`UPDATE artifacts SET sha256 = ? WHERE id = ?`, strings.Repeat("f", 64), f.input.EffectiveBriefArtifactRowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "cancelled pending attempt", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			t.Helper()
			if _, err := f.store.DB().Exec(`UPDATE execution_attempts SET cancellation_requested_at = '2000-01-01T00:00:00Z' WHERE id = ?`, f.attempt.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "running attempt without active lease", mutate: func(t *testing.T, f *preparedAdaptiveFixture) { admitPreparedFixture(t, f); releasePreparedLease(t, f) }},
		{name: "running attempt has different lease ID", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			admitPreparedFixture(t, f)
			wrong := f.input
			wrong.ProposedLeaseID = "lease-different"
			if _, err := f.store.DB().Exec(`UPDATE execution_attempts SET result_json = ? WHERE id = ?`, preparedRuntimeJSON(t, wrong), f.attempt.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "active lease owned by another Run", mutate: func(t *testing.T, f *preparedAdaptiveFixture) { createOtherActiveLease(t, f) }, want: ErrMutationLeaseConflict},
		{name: "uncertain lease", mutate: func(t *testing.T, f *preparedAdaptiveFixture) {
			admitPreparedFixture(t, f)
			markPreparedLeaseUncertain(t, f)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPreparedAdaptiveFixture(t)
			tc.mutate(t, fixture)
			_, err := fixture.service.BeginPreparedAdaptiveExecution(context.Background(), fixture.input)
			want := tc.want
			if want == nil {
				want = ErrPreparedAdaptiveExecutionConflict
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func newPreparedAdaptiveFixture(t *testing.T) *preparedAdaptiveFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.DB()
	if _, err := db.Exec(`INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER execution_package_input_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	var packageID int64
	if err := db.QueryRow(`INSERT INTO execution_packages (package_id, selection_row_id, workspace_row_id, repo_target, branch, base_commit, source_closure_row_id, authority_revision_row_id, package_sha256, authority_sha256, source_sha256, design_brief_sha256) VALUES ('package-prepared-adaptive', 1, 1, 'relay', 'main', ?, 1, 1, ?, ?, ?, ?) RETURNING id`, strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64)).Scan(&packageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	var run workflowstore.Run
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var createErr error
		run, createErr = tx.CreateRun(ctx, workflowstore.CreateRunParams{RunID: "run-prepared-adaptive", FeatureSlug: "prepared", RepoTarget: "relay", Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: strings.Repeat("a", 40)})
		if createErr != nil {
			return createErr
		}
		run, createErr = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady)
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE runs SET execution_package_row_id = ? WHERE id = ?`, packageID, run.ID); err != nil {
		t.Fatal(err)
	}
	run, err = store.GetRunByRunID(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var attempt workflowstore.ExecutionAttempt
	var inputArtifact, briefArtifact workflowstore.Artifact
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var createErr error
		attempt, createErr = tx.CreateExecutionAttempt(ctx, workflowstore.CreateExecutionAttemptParams{AttemptID: "attempt-prepared-adaptive", RunRowID: run.ID, AttemptNumber: 1, Adapter: "codex", Model: "model"})
		if createErr != nil {
			return createErr
		}
		inputArtifact, createErr = tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{ArtifactID: "artifact-prepared-input", OwnerType: workflowstore.ArtifactOwnerExecutionAttempt, ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true}, Kind: "adaptive_input", RelativePath: "runs/prepared/input.json", MediaType: "application/json", SHA256: preparedDigest("input"), SizeBytes: 5})
		if createErr != nil {
			return createErr
		}
		briefArtifact, createErr = tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{ArtifactID: "artifact-prepared-brief", OwnerType: workflowstore.ArtifactOwnerRun, RunRowID: sql.NullInt64{Int64: run.ID, Valid: true}, Kind: "effective_brief", RelativePath: "runs/prepared/brief.md", MediaType: "text/markdown", SHA256: preparedDigest("brief"), SizeBytes: 5})
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	input := BeginPreparedAdaptiveExecutionInput{RunID: run.RunID, RunRowID: run.ID, AttemptID: attempt.AttemptID, AttemptRowID: attempt.ID, AttemptNumber: attempt.AttemptNumber, Adapter: attempt.Adapter, Model: attempt.Model, InputArtifactRowID: inputArtifact.ID, InputArtifactSHA256: inputArtifact.SHA256, EffectiveBriefArtifactRowID: briefArtifact.ID, EffectiveBriefArtifactID: briefArtifact.ArtifactID, EffectiveBriefSHA256: briefArtifact.SHA256, EffectiveBriefMode: "adaptive_no_operations", ProposedLeaseID: "lease-prepared-adaptive"}
	input.RunningResultJSON = preparedRuntimeJSON(t, input)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return &preparedAdaptiveFixture{store: store, service: service, run: run, attempt: attempt, input: input}
}

func preparedDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func preparedRuntimeJSON(t *testing.T, input BeginPreparedAdaptiveExecutionInput) string {
	t.Helper()
	sourceMutationStarted, valid := preparedAdaptiveSourceMutationStarted(input.EffectiveBriefMode)
	if !valid {
		t.Fatal("invalid prepared adaptive mode")
	}
	content, err := json.Marshal(preparedAdaptiveRuntime{MutationLeaseID: input.ProposedLeaseID, SourceMutationStarted: sourceMutationStarted, EffectiveBriefArtifactID: input.EffectiveBriefArtifactID, EffectiveBriefSHA256: input.EffectiveBriefSHA256, EffectiveBriefMode: input.EffectiveBriefMode})
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func seedPreparedPartialLease(t *testing.T, f *preparedAdaptiveFixture) {
	t.Helper()
	if err := f.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryBranchMutationLease(context.Background(), workflowstore.CreateRepositoryBranchMutationLeaseParams{
			LeaseID: f.input.ProposedLeaseID, RepoTarget: f.run.RepoTarget, Branch: f.run.Branch,
			OwnerKind: runMutationLeaseOwnerKind, OwnerIdentity: f.run.RunID,
			UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
			ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func assertPreparedAdmissionUnchanged(t *testing.T, f *preparedAdaptiveFixture, expectedLeases int) {
	t.Helper()
	run, err := f.store.GetRunByRunID(context.Background(), f.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.store.GetExecutionAttemptByAttemptID(context.Background(), f.attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != workflowstore.RunStatusSetupReady || attempt.Status != workflowstore.AttemptStatusPending || attempt.ResultJSON != "{}" {
		t.Fatalf("rollback left run=%s attempt=%s result=%s", run.Status, attempt.Status, attempt.ResultJSON)
	}
	leases, err := f.store.ListRepositoryBranchMutationLeases(context.Background(), f.run.RepoTarget, f.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != expectedLeases {
		t.Fatalf("rollback left leases %#v", leases)
	}
}

func createAdmissionFailureTrigger(t *testing.T, store *workflowstore.Store, table, timing, condition, message string) {
	t.Helper()
	name := "fail_prepared_admission_" + strings.ReplaceAll(strings.ReplaceAll(table, "_", ""), "-", "")
	query := fmt.Sprintf("CREATE TRIGGER %s %s ON %s WHEN %s BEGIN SELECT RAISE(ABORT, '%s'); END", name, timing, table, condition, message)
	if _, err := store.DB().Exec(query); err != nil {
		t.Fatal(err)
	}
}

func seedPreparedAdaptiveCutover(t *testing.T, store *workflowstore.Store) {
	t.Helper()
	db := store.DB()
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS cutover_activation_insert_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cutover_activations (cutover_activation_id, workspace_row_id, transition_plan_ticket_revision_row_id, transition_plan_ticket_id, transition_plan_ticket_revision, transition_plan_authority_layer_row_id, transition_plan_sha256, authority_revision_row_id, authority_revision_id, authority_revision_number, authority_sha256, rollback_eligibility, activation_status, activated_at, execution_boundary_status, rollback_status, roll_forward_status) VALUES ('cutover-prepared-adaptive', 1, 1, 'CUTOVER', 1, 1, ?, 1, 'authority-prepared-adaptive', 1, ?, 'eligible', 'active', '2000-01-01T00:00:00Z', 'open', 'available', 'pending')`, strings.Repeat("a", 64), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cutover_current_states (singleton_id, activation_row_id) SELECT 1, id FROM cutover_activations WHERE cutover_activation_id = 'cutover-prepared-adaptive'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
}

func admitPreparedFixture(t *testing.T, f *preparedAdaptiveFixture) {
	t.Helper()
	result, err := f.service.BeginPreparedAdaptiveExecution(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.input.ProposedLeaseID, f.input.RunningResultJSON = result.Lease.LeaseID, result.Attempt.ResultJSON
}

func releasePreparedLease(t *testing.T, f *preparedAdaptiveFixture) {
	t.Helper()
	if _, err := f.store.DB().Exec(`UPDATE repository_branch_mutation_leases SET state = 'released', released_at = '2000-01-01T00:00:00Z' WHERE repo_target = ? AND branch = ?`, f.run.RepoTarget, f.run.Branch); err != nil {
		t.Fatal(err)
	}
}
func markPreparedLeaseUncertain(t *testing.T, f *preparedAdaptiveFixture) {
	t.Helper()
	if _, err := f.store.DB().Exec(`UPDATE repository_branch_mutation_leases SET uncertainty_state = 'uncertain', uncertainty_reason = 'test', reconciliation_state = 'required' WHERE repo_target = ? AND branch = ?`, f.run.RepoTarget, f.run.Branch); err != nil {
		t.Fatal(err)
	}
}
func createOtherActiveLease(t *testing.T, f *preparedAdaptiveFixture) {
	t.Helper()
	if err := f.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryBranchMutationLease(context.Background(), workflowstore.CreateRepositoryBranchMutationLeaseParams{LeaseID: "lease-other-run", RepoTarget: f.run.RepoTarget, Branch: f.run.Branch, OwnerKind: "run", OwnerIdentity: "run-other", UncertaintyState: workflowstore.RepositoryBranchMutationLeaseCertaintyCertain, ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
