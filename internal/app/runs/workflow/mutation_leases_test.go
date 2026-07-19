package workflowruns

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestRunMutationLeaseScopesAndLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openMutationLeaseStore(t)
	seedMutationLeaseRuns(t, ctx, store)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.AcquireRunMutationLease(ctx, "run-lease-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if first.OwnerKind != runMutationLeaseOwnerKind || first.OwnerIdentity != "run-lease-legacy" || first.State != workflowstore.RepositoryBranchMutationLeaseStateActive {
		t.Fatalf("first lease = %#v", first)
	}
	if _, err := service.AcquireRunMutationLease(ctx, "run-lease-same-branch"); !errors.Is(err, ErrMutationLeaseConflict) {
		t.Fatalf("same repository and branch acquisition error = %v", err)
	}
	if _, err := service.AcquireRunMutationLease(ctx, "run-lease-other-branch"); err != nil {
		t.Fatalf("other branch was blocked: %v", err)
	}
	if _, err := service.AcquireRunMutationLease(ctx, "run-lease-other-repository"); err != nil {
		t.Fatalf("other repository was blocked: %v", err)
	}

	uncertain, err := service.MarkRunMutationLeaseUncertain(ctx, "run-lease-legacy", first.LeaseID, "model process outcome is unknown")
	if err != nil {
		t.Fatal(err)
	}
	if uncertain.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain || uncertain.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationRequired {
		t.Fatalf("uncertain lease = %#v", uncertain)
	}
	inProgress, err := service.BeginRunMutationLeaseReconciliation(ctx, "run-lease-legacy", first.LeaseID, "inspect process and repository evidence")
	if err != nil {
		t.Fatal(err)
	}
	if inProgress.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationInProgress || !inProgress.ReconciliationStartedAt.Valid {
		t.Fatalf("in-progress lease = %#v", inProgress)
	}
	failed, err := service.FailRunMutationLeaseReconciliation(ctx, "run-lease-legacy", first.LeaseID, "repository state remains dirty", "repository preflight did not prove a clean outcome")
	if err != nil {
		t.Fatal(err)
	}
	if failed.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationFailed || !failed.ReconciledAt.Valid {
		t.Fatalf("failed lease = %#v", failed)
	}
	if _, err := service.ReleaseRunMutationLease(ctx, "run-lease-legacy", first.LeaseID); !errors.Is(err, ErrMutationLeaseUncertain) {
		t.Fatalf("uncertain lease release error = %v", err)
	}
	reconciled, err := service.CompleteRunMutationLeaseReconciliation(ctx, "run-lease-legacy", first.LeaseID, "repository evidence now proves a clean outcome")
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain || reconciled.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationReconciled {
		t.Fatalf("reconciled lease = %#v", reconciled)
	}
	released, err := service.ReleaseRunMutationLease(ctx, "run-lease-legacy", first.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != workflowstore.RepositoryBranchMutationLeaseStateReleased || !released.ReleasedAt.Valid {
		t.Fatalf("released lease = %#v", released)
	}
	if _, err := service.AcquireRunMutationLease(ctx, "run-lease-same-branch"); err != nil {
		t.Fatalf("replacement same-branch lease failed after release: %v", err)
	}
}

func TestRunMutationLeaseHasOneConcurrentWinner(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "workflow.sqlite")
	seed, err := workflowstore.Open(databasePath, filepath.Join(root, "seed-artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	seedMutationLeaseRuns(t, ctx, seed)
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	firstStore, err := workflowstore.Open(databasePath, filepath.Join(root, "first-artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstStore.Close() })
	secondStore, err := workflowstore.Open(databasePath, filepath.Join(root, "second-artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })
	firstService, err := NewService(firstStore)
	if err != nil {
		t.Fatal(err)
	}
	secondService, err := NewService(secondStore)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for _, candidate := range []struct {
		service *Service
		runID   string
	}{
		{service: firstService, runID: "run-lease-legacy"},
		{service: secondService, runID: "run-lease-same-branch"},
	} {
		candidate := candidate
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := candidate.service.AcquireRunMutationLease(ctx, candidate.runID)
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrMutationLeaseConflict):
			conflicts++
		default:
			t.Fatalf("concurrent acquisition error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent acquisitions: successes=%d conflicts=%d", successes, conflicts)
	}
}

func openMutationLeaseStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedMutationLeaseRuns(t *testing.T, ctx context.Context, store *workflowstore.Store) {
	t.Helper()
	err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		for _, target := range []string{"relay", "other"} {
			if _, err := tx.CreateRepositoryTarget(ctx, target, filepath.Join(t.TempDir(), target)); err != nil {
				return err
			}
		}
		for _, run := range []struct {
			runID      string
			repoTarget string
			branch     string
		}{
			{runID: "run-lease-legacy", repoTarget: "relay", branch: "main"},
			{runID: "run-lease-same-branch", repoTarget: "relay", branch: "main"},
			{runID: "run-lease-other-branch", repoTarget: "relay", branch: "release"},
			{runID: "run-lease-other-repository", repoTarget: "other", branch: "main"},
		} {
			if _, err := tx.CreateRun(ctx, workflowstore.CreateRunParams{
				RunID:           run.runID,
				FeatureSlug:     "lease-test",
				RepoTarget:      run.repoTarget,
				Status:          workflowstore.RunStatusCreated,
				Branch:          run.branch,
				BaseCommit:      strings.Repeat("a", 40),
				CanonicalSHA256: strings.Repeat("b", 64),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
