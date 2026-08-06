package executor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relay/internal/pipeline"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

type cleanupOwnedProcess struct{ running bool }

func (p cleanupOwnedProcess) Identity() pipeline.ProcessIdentity {
	return pipeline.ProcessIdentity{PID: 42, Platform: "linux", StartedAt: "2026-01-01T00:00:00Z"}
}
func (p cleanupOwnedProcess) Stdout() io.ReadCloser {
	return io.NopCloser(strings.NewReader(""))
}
func (p cleanupOwnedProcess) Stderr() io.ReadCloser {
	return io.NopCloser(strings.NewReader(""))
}
func (p cleanupOwnedProcess) Wait() error                { return nil }
func (p cleanupOwnedProcess) TreeRunning() (bool, error) { return p.running, nil }
func (p cleanupOwnedProcess) Terminate(time.Duration) (pipeline.ProcessTerminationResult, error) {
	return pipeline.ProcessTerminationResult{}, errors.New("cleanup tests must not terminate processes")
}
func (p cleanupOwnedProcess) Release() error { return nil }

type cleanupProcessController struct{ running bool }

func (c cleanupProcessController) StartOwned(context.Context, pipeline.CommandSpec) (pipeline.OwnedProcess, error) {
	return nil, errors.New("cleanup reconciliation must not start processes")
}
func (c cleanupProcessController) OpenOwned(pipeline.ProcessIdentity) (pipeline.OwnedProcess, error) {
	return cleanupOwnedProcess{running: c.running}, nil
}

func prepareCleanupReconciliationFixture(t *testing.T, evidence bool) (*PrototypeCleanup, *workflowstore.Store, workflowstore.PrototypeRun, string) {
	t.Helper()
	ctx := context.Background()
	_, store, run, _, _, worktree := prototypeRegressionFixture(t, true, `[]`)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, _, err := tx.SettlePrototypeProcess(ctx, run.PrototypeRunID, run.Version, "cleanup_required", "failed", "cleanup-test-settle"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetPrototypeRun(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := store.GetPrototypeRuntimeByRunID(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence {
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, err := tx.CreatePrototypeEvidenceImportBatch(ctx, workflowstore.PrototypeEvidenceImportBatch{
				EvidenceBatchID: "prototype-evidence-batch-cleanup-test", RunRowID: run.ID, RuntimeRowID: runtime.ID,
				BatchIdentity: "cleanup-evidence-batch", SettlementCause: "runner_failure", ObservationIdentity: "cleanup-observation",
				ProcessOutcome: "failed", EnvelopeStatus: "missing", Completeness: "partial", ArtifactCount: 0, TotalSizeBytes: 0,
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	cleanup, err := NewPrototypeCleanup(store, filepath.Dir(worktree))
	if err != nil {
		t.Fatal(err)
	}
	return cleanup, store, run, worktree
}

func TestPrototypeCleanupReconciliation(t *testing.T) {
	ctx := context.Background()
	cleanup, store, run, worktree := prepareCleanupReconciliationFixture(t, true)
	cleanup.controller = cleanupProcessController{}
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}

	result, err := cleanup.ReconcileCleanup(ctx, prototypeexecution.CleanupRequest{
		WorkspaceID: "workspace-prototype-regression", RunID: run.PrototypeRunID, ExpectedRunVersion: run.Version,
		MutationIdentity: "cleanup-reconcile-test", TriggerKind: "explicit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.LifecycleState != "closed" || result.Reconciliation.ResultingRunState != "closed" {
		t.Fatalf("cleanup result = %#v", result)
	}
	for _, obligation := range []string{"process_ownership", "evidence_settlement", "worktree", "ephemeral_target", "prototype_lease"} {
		if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, obligation); got != "complete" {
			t.Fatalf("%s status = %q, want complete", obligation, got)
		}
	}
	replayed, err := cleanup.ReconcileCleanup(ctx, prototypeexecution.CleanupRequest{
		WorkspaceID: "workspace-prototype-regression", RunID: run.PrototypeRunID, ExpectedRunVersion: run.Version,
		MutationIdentity: "cleanup-reconcile-test", TriggerKind: "explicit",
	})
	if err != nil {
		if errors.Is(err, prototypeexecution.ErrCleanupOwnershipMismatch) {
			t.Fatalf("idempotent replay reported ownership mismatch: %v", err)
		}
		t.Fatalf("idempotent replay = %v", err)
	}
	if replayed.Reconciliation.ID != result.Reconciliation.ID || replayed.Reconciliation.ReconciliationID != result.Reconciliation.ReconciliationID {
		t.Fatalf("replayed reconciliation = %#v, want %#v", replayed.Reconciliation, result.Reconciliation)
	}
	reconciliations, err := store.ListPrototypeCleanupReconciliations(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciliations) != 1 {
		t.Fatalf("reconciliation count = %d, want 1", len(reconciliations))
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree exists after cleanup: %v", err)
	}
}

func TestPrototypeCleanupAttemptsIndependentObligations(t *testing.T) {
	ctx := context.Background()
	cleanup, store, run, worktree := prepareCleanupReconciliationFixture(t, false)
	cleanup.controller = cleanupProcessController{}
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}

	_, err := cleanup.ReconcileCleanup(ctx, prototypeexecution.CleanupRequest{
		WorkspaceID: "workspace-prototype-regression", RunID: run.PrototypeRunID, ExpectedRunVersion: run.Version,
		MutationIdentity: "cleanup-independent-obligations-test", TriggerKind: "explicit",
	})
	if !errors.Is(err, prototypeexecution.ErrReconciliationIncomplete) {
		t.Fatalf("cleanup error = %v, want incomplete", err)
	}
	for obligation, want := range map[string]string{
		"process_ownership":   "complete",
		"evidence_settlement": "pending",
		"worktree":            "complete",
		"ephemeral_target":    "complete",
		"prototype_lease":     "complete",
	} {
		if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, obligation); got != want {
			t.Fatalf("%s status = %q, want %q", obligation, got, want)
		}
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree exists after cleanup: %v", err)
	}
	var targetStatus, leaseStatus string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM feature_workspace_prototype_targets WHERE run_row_id=?`, run.ID).Scan(&targetStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM feature_workspace_prototype_leases WHERE run_row_id=?`, run.ID).Scan(&leaseStatus); err != nil {
		t.Fatal(err)
	}
	if targetStatus != "released" || leaseStatus != "released" {
		t.Fatalf("resource statuses = target %q, lease %q; want released", targetStatus, leaseStatus)
	}
	current, err := store.GetPrototypeRun(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.LifecycleState != "cleanup_required" {
		t.Fatalf("run lifecycle = %q, want cleanup_required", current.LifecycleState)
	}
}

func TestPrototypeStartupReconciliation(t *testing.T) {
	ctx := context.Background()
	cleanup, store, run, _ := prepareCleanupReconciliationFixture(t, false)
	cleanup.controller = cleanupProcessController{running: true}

	results, err := cleanup.ReconcileStartup(ctx, 1)
	if !errors.Is(err, prototypeexecution.ErrReconciliationIncomplete) {
		t.Fatalf("startup error = %v, want incomplete", err)
	}
	if len(results) != 1 || results[0].Reconciliation.TriggerKind != "startup" || results[0].Reconciliation.ProcessOwnershipStatus != "pending" {
		t.Fatalf("startup results = %#v", results)
	}
	if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, "process_ownership"); got != "pending" {
		t.Fatalf("process ownership status = %q, want pending", got)
	}
	if _, err := cleanup.ReconcileStartup(ctx, 0); err == nil {
		t.Fatal("startup accepted zero limit")
	}
}

func TestPrototypeCleanupIsolation(t *testing.T) {
	ctx := context.Background()
	cleanup, store, run, worktree := prepareCleanupReconciliationFixture(t, true)
	cleanup.controller = cleanupProcessController{}
	productionRoot := filepath.Dir(worktree)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target,local_path,configured_branch_ref,configuration_version) VALUES ('production-overlap',?,'refs/heads/main',1)`, productionRoot); err != nil {
		t.Fatal(err)
	}

	_, err := cleanup.ReconcileCleanup(ctx, prototypeexecution.CleanupRequest{
		WorkspaceID: "workspace-prototype-regression", RunID: run.PrototypeRunID, ExpectedRunVersion: run.Version,
		MutationIdentity: "cleanup-isolation-test", TriggerKind: "explicit",
	})
	if !errors.Is(err, prototypeexecution.ErrCleanupWorktree) {
		t.Fatalf("cleanup error = %v, want worktree isolation failure", err)
	}
	if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, "worktree"); got != "failed" {
		t.Fatalf("worktree status = %q, want failed", got)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("production-overlapping worktree changed: %v", err)
	}
	var localPath string
	if err := store.DB().QueryRowContext(ctx, `SELECT local_path FROM repository_targets WHERE repo_target='production-overlap'`).Scan(&localPath); err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(localPath) != filepath.Clean(productionRoot) {
		t.Fatalf("production target path = %q, want %q", localPath, productionRoot)
	}
}
