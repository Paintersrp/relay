package sourcevault

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestManagerStaleConfiguredAuthorityBlocksEveryAcquisitionPath(t *testing.T) {
	for _, tc := range []struct {
		name                string
		initialState        string
		wantDisposition     string
		wantGeneration      int64
		wantPreExplicitRows int
	}{
		{
			name:                "first generation",
			wantDisposition:     workflowstore.SourceVaultClosureAcquisitionCreated,
			wantGeneration:      1,
			wantPreExplicitRows: 0,
		},
		{
			name:                "unavailable retry",
			initialState:        workflowstore.SourceVaultClosureStateUnavailable,
			wantDisposition:     workflowstore.SourceVaultClosureAcquisitionRetry,
			wantGeneration:      1,
			wantPreExplicitRows: 1,
		},
		{
			name:                "next generation after release",
			initialState:        workflowstore.SourceVaultClosureStateReleased,
			wantDisposition:     workflowstore.SourceVaultClosureAcquisitionCreated,
			wantGeneration:      2,
			wantPreExplicitRows: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := newGitRepository(t)
			commit := commitFile(t, repo, "authority.txt", []byte("authority\n"), "authority")
			primary, independent := openIndependentSourceVaultStores(t)
			registerSourceVaultRepository(t, ctx, primary, "relay", repo, "refs/heads/main")
			captured := storeTarget(t, ctx, primary, "relay")
			configured := configuredRevision(captured, commit.commit, commit.tree)

			var seeded workflowstore.SourceVaultClosure
			if tc.initialState != "" {
				_, seeded = seedAuthorityClosure(t, ctx, primary, configured, tc.initialState)
			}
			manager, err := newManager(primary, newFakeGit())
			if err != nil {
				t.Fatal(err)
			}
			reconfigureAuthorityTarget(t, ctx, independent, captured)

			staleClosureID := workflowstore.NewSourceVaultClosureID()
			_, _, err = manager.tryAcquireImportAuthority(
				ctx,
				configured,
				canonicalTime(testTime().Add(time.Second)),
				workflowstore.NewSourceVaultID(),
				"repositories/stale-candidate.git",
				staleClosureID,
				"refs/relay/closures/"+staleClosureID,
			)
			if ErrorCode(err) != CodeStaleConfiguredAuthority {
				t.Fatalf("stale configured acquisition error = %v code=%q", err, ErrorCode(err))
			}
			assertTableCount(t, primary, "source_vault_closures", int64(tc.wantPreExplicitRows))
			if seeded.ClosureID != "" {
				unchanged, readErr := primary.GetSourceVaultClosureByClosureID(ctx, seeded.ClosureID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if unchanged.State != tc.initialState || unchanged.Generation != 1 {
					t.Fatalf("stale acquisition changed seeded closure: %#v", unchanged)
				}
			}

			explicit := explicitRevision(captured, commit.commit, commit.tree)
			candidateClosureID := workflowstore.NewSourceVaultClosureID()
			_, acquisition, err := manager.tryAcquireImportAuthority(
				ctx,
				explicit,
				canonicalTime(testTime().Add(2*time.Second)),
				workflowstore.NewSourceVaultID(),
				"repositories/explicit-candidate.git",
				candidateClosureID,
				"refs/relay/closures/"+candidateClosureID,
			)
			if err != nil {
				t.Fatal(err)
			}
			if acquisition.Disposition != tc.wantDisposition || acquisition.Closure.Generation != tc.wantGeneration {
				t.Fatalf("explicit acquisition = %#v", acquisition)
			}
		})
	}
}

func TestManagerWinnerLookupRevalidatesConfiguredAuthorityAcrossStates(t *testing.T) {
	for _, tc := range []struct {
		state           string
		wantDisposition string
	}{
		{state: workflowstore.SourceVaultClosureStateImporting, wantDisposition: workflowstore.SourceVaultClosureAcquisitionImporting},
		{state: workflowstore.SourceVaultClosureStateReady, wantDisposition: workflowstore.SourceVaultClosureAcquisitionReady},
		{state: workflowstore.SourceVaultClosureStateReleasing, wantDisposition: workflowstore.SourceVaultClosureAcquisitionReleasing},
	} {
		t.Run(tc.state, func(t *testing.T) {
			ctx := context.Background()
			repo := newGitRepository(t)
			commit := commitFile(t, repo, "winner.txt", []byte("winner\n"), "winner")
			primary, independent := openIndependentSourceVaultStores(t)
			registerSourceVaultRepository(t, ctx, primary, "relay", repo, "refs/heads/main")
			captured := storeTarget(t, ctx, primary, "relay")
			configured := configuredRevision(captured, commit.commit, commit.tree)
			_, seeded := seedAuthorityClosure(t, ctx, primary, configured, tc.state)
			manager, err := newManager(primary, newFakeGit())
			if err != nil {
				t.Fatal(err)
			}
			reconfigureAuthorityTarget(t, ctx, independent, captured)

			_, _, err = manager.currentImportAuthority(ctx, configured)
			if ErrorCode(err) != CodeStaleConfiguredAuthority {
				t.Fatalf("stale winner lookup error = %v code=%q", err, ErrorCode(err))
			}
			winner, acquisition, err := manager.currentImportAuthority(ctx, explicitRevision(captured, commit.commit, commit.tree))
			if err != nil {
				t.Fatal(err)
			}
			if winner.RepoTarget != "relay" || acquisition.Closure.ClosureID != seeded.ClosureID || acquisition.Disposition != tc.wantDisposition {
				t.Fatalf("explicit winner = %#v %#v", winner, acquisition)
			}
		})
	}
}

func TestManagerSourceMissingWinnerLookupRevalidatesAfterIndependentReconfiguration(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	commit := commitFile(t, repo, "missing-source.txt", []byte("retained\n"), "retained")
	primary, independent := openIndependentSourceVaultStores(t)
	registerSourceVaultRepository(t, ctx, primary, "relay", repo, "refs/heads/main")
	captured := storeTarget(t, ctx, primary, "relay")
	configured := configuredRevision(captured, commit.commit, commit.tree)
	_, closure := seedAuthorityClosure(t, ctx, primary, configured, workflowstore.SourceVaultClosureStateReady)

	barrier := newMissingSourceAuthorityBarrier()
	barrier.refs[closure.RefName] = commit.commit
	barrier.trees[commit.commit] = commit.tree
	manager, err := newManager(primary, barrier)
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, importErr := manager.ImportClosure(ctx, ImportRequest{Revision: configured})
		errCh <- importErr
	}()
	<-barrier.entered
	reconfigureAuthorityTarget(t, ctx, independent, captured)
	close(barrier.release)
	if err := <-errCh; ErrorCode(err) != CodeStaleConfiguredAuthority {
		t.Fatalf("source-missing stale winner error = %v code=%q", err, ErrorCode(err))
	}
	unchanged, err := primary.GetSourceVaultClosureByClosureID(ctx, closure.ClosureID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.State != workflowstore.SourceVaultClosureStateReady || unchanged.Generation != 1 {
		t.Fatalf("stale source-missing lookup changed closure: %#v", unchanged)
	}

	result, err := manager.ImportClosure(ctx, ImportRequest{
		Revision: explicitRevision(captured, commit.commit, commit.tree),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ready || result.Closure.ClosureID != closure.ClosureID {
		t.Fatalf("explicit source-missing lookup = %#v", result)
	}
}

func openIndependentSourceVaultStores(t *testing.T) (*workflowstore.Store, *workflowstore.Store) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "workflow.sqlite")
	primary := openSourceVaultTestStoreAt(t, dbPath, filepath.Join(root, "artifacts-primary"))
	independent := openSourceVaultTestStoreAt(t, dbPath, filepath.Join(root, "artifacts-independent"))
	return primary, independent
}

func reconfigureAuthorityTarget(
	t *testing.T,
	ctx context.Context,
	store *workflowstore.Store,
	captured workflowstore.RepositoryTarget,
) {
	t.Helper()
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.ConfigureRepositoryTarget(ctx, workflowstore.ConfigureRepositoryTargetParams{
			RepoTarget:                   captured.RepoTarget,
			ExpectedConfigurationVersion: captured.ConfigurationVersion,
			ConfiguredBranchRef:          "refs/heads/reconfigured",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func seedAuthorityClosure(
	t *testing.T,
	ctx context.Context,
	store *workflowstore.Store,
	revision workflowrepos.ResolvedRevision,
	state string,
) (workflowstore.SourceVault, workflowstore.SourceVaultClosure) {
	t.Helper()
	var vault workflowstore.SourceVault
	var closure workflowstore.SourceVaultClosure
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		vaultID := workflowstore.NewSourceVaultID()
		var err error
		vault, err = tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{
			VaultID:      vaultID,
			RepoTarget:   revision.RepositoryTarget.RepoTarget,
			RelativePath: filepath.ToSlash(filepath.Join("repositories", vaultID+".git")),
		})
		if err != nil {
			return err
		}
		closureID := workflowstore.NewSourceVaultClosureID()
		acquisition, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID,
			ClosureID:  closureID,
			CommitOID:  revision.CommitOID,
			TreeOID:    revision.TreeOID,
			RefName:    "refs/relay/closures/" + closureID,
			StartedAt:  canonicalTime(testTime()),
		})
		if err != nil {
			return err
		}
		closure = acquisition.Closure
		switch state {
		case workflowstore.SourceVaultClosureStateImporting:
			return nil
		case workflowstore.SourceVaultClosureStateReady:
			closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
				ClosureID:     closure.ClosureID,
				ExpectedState: workflowstore.SourceVaultClosureStateImporting,
				NextState:     workflowstore.SourceVaultClosureStateReady,
				TransitionAt:  canonicalTime(testTime()),
			})
			return err
		case workflowstore.SourceVaultClosureStateUnavailable:
			closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
				ClosureID:     closure.ClosureID,
				ExpectedState: workflowstore.SourceVaultClosureStateImporting,
				NextState:     workflowstore.SourceVaultClosureStateUnavailable,
				FailureReason: sql.NullString{String: workflowstore.SourceVaultFailureInterruptedImport, Valid: true},
				TransitionAt:  canonicalTime(testTime()),
			})
			return err
		case workflowstore.SourceVaultClosureStateReleasing, workflowstore.SourceVaultClosureStateReleased:
			closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
				ClosureID:     closure.ClosureID,
				ExpectedState: workflowstore.SourceVaultClosureStateImporting,
				NextState:     workflowstore.SourceVaultClosureStateReady,
				TransitionAt:  canonicalTime(testTime()),
			})
			if err != nil {
				return err
			}
			closure, err = tx.BeginSourceVaultClosureRelease(ctx, closure.ClosureID, canonicalTime(testTime()))
			if err != nil || state == workflowstore.SourceVaultClosureStateReleasing {
				return err
			}
			closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
				ClosureID:     closure.ClosureID,
				ExpectedState: workflowstore.SourceVaultClosureStateReleasing,
				NextState:     workflowstore.SourceVaultClosureStateReleased,
				TransitionAt:  canonicalTime(testTime()),
			})
			return err
		default:
			t.Fatalf("unsupported source vault state %q", state)
			return nil
		}
	}); err != nil {
		t.Fatal(err)
	}
	return vault, closure
}

type missingSourceAuthorityBarrier struct {
	*fakeGit
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newMissingSourceAuthorityBarrier() *missingSourceAuthorityBarrier {
	return &missingSourceAuthorityBarrier{
		fakeGit: newFakeGit(),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *missingSourceAuthorityBarrier) ValidateRepositorySeparation(ctx context.Context, _ string) (bool, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.release:
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
