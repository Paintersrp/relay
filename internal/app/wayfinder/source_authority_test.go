package wayfinder

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
	workflowgenerated "relay/internal/store/workflowgenerated"
)

func TestSourceAuthorityRetainsExactClosureAcrossRestartAndDetectsStaleIdentity(t *testing.T) {
	ctx := context.Background()
	fixture := newSourceAuthorityFixture(t, ctx)
	authority, err := newSourceAuthority(fixture.store, fixture.vault)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := authority.CreateInvestigationClosure(ctx, fixture.input("investigation-restart", 1, fixture.first))
	if err != nil {
		t.Fatal(err)
	}
	if identity.CommitOID != fixture.first.CommitOID || identity.TreeOID != fixture.first.TreeOID || identity.RepositoryTarget != "relay" {
		t.Fatalf("identity = %#v", identity)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workflowstore.Open(fixture.databasePath, fixture.artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	authority, err = newSourceAuthority(reopened, fixture.vault)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := authority.ReadInvestigationClosure(ctx, identity); err != nil || got != identity {
		t.Fatalf("restart read = %#v, %v", got, err)
	}
	stale := identity
	stale.CommitOID = strings.Repeat("f", 40)
	if _, err := authority.ReadInvestigationClosure(ctx, stale); !errors.Is(err, ErrStaleSourceBase) {
		t.Fatalf("stale source base error = %v", err)
	}
}

func TestSourceAuthorityReplacementAndFailureRollbackAreAtomic(t *testing.T) {
	ctx := context.Background()
	fixture := newSourceAuthorityFixture(t, ctx)
	authority, err := newSourceAuthority(fixture.store, fixture.vault)
	if err != nil {
		t.Fatal(err)
	}
	first, err := authority.CreateInvestigationClosure(ctx, fixture.input("investigation-first", 1, fixture.first))
	if err != nil {
		t.Fatal(err)
	}
	failing := fixture.input("investigation-failed", 1, fixture.second)
	if _, err := authority.ReplaceInvestigationClosure(ctx, first, failing); err == nil {
		t.Fatal("replacement with duplicate evidence sequence succeeded")
	}
	if _, err := fixture.store.GetActiveSourceVaultRetentionByOwner(ctx, workflowstore.SourceVaultOwnerArtifact, failing.InvestigationID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("failed retention survived rollback: %v", err)
	}
	if got, err := authority.ReadInvestigationClosure(ctx, first); err != nil || got != first {
		t.Fatalf("prior identity after rollback = %#v, %v", got, err)
	}

	replacement, err := authority.ReplaceInvestigationClosure(ctx, first, fixture.input("investigation-second", 2, fixture.second))
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ClosureID != fixture.second.ClosureID || replacement.CommitOID != fixture.second.CommitOID {
		t.Fatalf("replacement = %#v", replacement)
	}
	if _, err := authority.ReadInvestigationClosure(ctx, first); !errors.Is(err, ErrRetainedClosureUnavailable) {
		t.Fatalf("superseded identity read error = %v", err)
	}
	if err := authority.ReleaseInvestigationClosure(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.ReadInvestigationClosure(ctx, replacement); !errors.Is(err, ErrRetainedClosureUnavailable) {
		t.Fatalf("released identity read error = %v", err)
	}
}

type sourceAuthorityFixture struct {
	store        *workflowstore.Store
	vault        *fakeSourceAuthorityVault
	databasePath string
	artifactRoot string
	workspaceID  int64
	artifactID   int64
	first        workflowstore.SourceVaultClosure
	second       workflowstore.SourceVaultClosure
}

func newSourceAuthorityFixture(t *testing.T, ctx context.Context) sourceAuthorityFixture {
	t.Helper()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "workflow.sqlite")
	artifactRoot := filepath.Join(directory, "artifacts")
	store, err := workflowstore.Open(databasePath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var vault workflowstore.SourceVault
	var first, second workflowstore.SourceVaultClosure
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", filepath.Join(directory, "relay")); err != nil {
			return err
		}
		var err error
		vault, err = tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{VaultID: "vault-wayfinder", RepoTarget: "relay", RelativePath: "repositories/vault-wayfinder.git"})
		if err != nil {
			return err
		}
		first, err = createReadyWayfinderClosure(ctx, tx, vault.ID, "closure-wayfinder-first", strings.Repeat("1", 40), strings.Repeat("2", 40), "2026-07-18T00:00:00Z")
		if err != nil {
			return err
		}
		second, err = createReadyWayfinderClosure(ctx, tx, vault.ID, "closure-wayfinder-second", strings.Repeat("3", 40), strings.Repeat("4", 40), "2026-07-18T00:00:02Z")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var projectID, planID, artifactID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-wayfinder', 'Wayfinder') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-wayfinder', 'wayfinder', ?) RETURNING id`, projectID, strings.Repeat("a", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-wayfinder', 'plan', ?, 'evidence', 'plans/wayfinder/evidence.json', 'application/json', ?, 2) RETURNING id`, planID, strings.Repeat("b", 64)).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	workspace, err := workflowgenerated.New(store.DB()).CreateFeatureWorkspace(ctx, workflowgenerated.CreateFeatureWorkspaceParams{WorkspaceID: "workspace-wayfinder", ProjectRowID: projectID, FeatureSlug: "wayfinder"})
	if err != nil {
		t.Fatal(err)
	}
	return sourceAuthorityFixture{
		store: store, vault: &fakeSourceAuthorityVault{store: store, imported: map[string]sourcevault.ImportResult{
			first.CommitOID:  {Vault: vault, Closure: first, CommitOID: first.CommitOID, TreeOID: first.TreeOID, Ready: true},
			second.CommitOID: {Vault: vault, Closure: second, CommitOID: second.CommitOID, TreeOID: second.TreeOID, Ready: true},
		}}, databasePath: databasePath, artifactRoot: artifactRoot, workspaceID: workspace.ID, artifactID: artifactID, first: first, second: second,
	}
}

func createReadyWayfinderClosure(ctx context.Context, tx *workflowstore.Tx, vaultRowID int64, closureID, commitOID, treeOID, at string) (workflowstore.SourceVaultClosure, error) {
	acquired, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{VaultRowID: vaultRowID, ClosureID: closureID, CommitOID: commitOID, TreeOID: treeOID, RefName: "refs/relay/closures/" + closureID, StartedAt: at})
	if err != nil {
		return workflowstore.SourceVaultClosure{}, err
	}
	return tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{ClosureID: acquired.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting, NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: at})
}

func (f sourceAuthorityFixture) input(investigationID string, sequence int64, closure workflowstore.SourceVaultClosure) CreateInvestigationClosureInput {
	return CreateInvestigationClosureInput{
		InvestigationID: investigationID, WorkspaceRowID: f.workspaceID, Sequence: sequence,
		Artifact:   InvestigationArtifactReference{ArtifactRowID: sql.NullInt64{Int64: f.artifactID, Valid: true}, SHA256: strings.Repeat("b", 64)},
		SourceBase: workflowrepos.ResolvedRevision{RepositoryTarget: workflowstore.RepositoryTarget{RepoTarget: "relay"}, RevisionSource: workflowrepos.RevisionSourceExplicitCommit, CommitOID: closure.CommitOID, TreeOID: closure.TreeOID},
	}
}

type fakeSourceAuthorityVault struct {
	store    *workflowstore.Store
	imported map[string]sourcevault.ImportResult
}

func (f *fakeSourceAuthorityVault) ImportClosure(_ context.Context, request sourcevault.ImportRequest) (sourcevault.ImportResult, error) {
	result, ok := f.imported[request.Revision.CommitOID]
	if !ok || result.TreeOID != request.Revision.TreeOID {
		return sourcevault.ImportResult{}, &sourcevault.Error{Code: sourcevault.CodeStaleConfiguredAuthority}
	}
	return result, nil
}

func (f *fakeSourceAuthorityVault) PrepareInvestigationRetention(_ context.Context, closureID, investigationID string) (sourcevault.PreparedInvestigationRetention, error) {
	for _, result := range f.imported {
		if result.Closure.ClosureID == closureID {
			return sourcevault.PreparedInvestigationRetention{OwnerIdentity: investigationID, Vault: result.Vault, Closure: result.Closure}, nil
		}
	}
	return sourcevault.PreparedInvestigationRetention{}, &sourcevault.Error{Code: sourcevault.CodeVaultUnavailable}
}

func (f *fakeSourceAuthorityVault) RetainPreparedInvestigationInTx(ctx context.Context, tx *workflowstore.Tx, prepared sourcevault.PreparedInvestigationRetention) (workflowstore.SourceVaultRetention, error) {
	return tx.CreateOrGetSourceVaultRetention(ctx, workflowstore.CreateSourceVaultRetentionParams{RetentionID: workflowstore.NewSourceVaultRetentionID(), ClosureRowID: prepared.Closure.ID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: prepared.OwnerIdentity})
}
