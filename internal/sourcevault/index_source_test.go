package sourcevault

import (
	"context"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	workflowstore "relay/internal/store/workflow"
)

func TestAcquireSourceIndexLeaseUsesExactRetainedClosureAndProcessLocalLock(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	commit := commitFile(t, repo, "indexed.txt", []byte("indexed\n"), "indexed")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "source-index"}); err != nil {
		t.Fatal(err)
	}
	options, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity(imported.Vault.VaultID, commit.commit, commit.tree, options)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.AcquireSourceIndexLease(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	if lease.RepositoryPath() == "" {
		t.Fatal("lease did not expose retained repository")
	}

	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AcquireSourceIndexLease(ctx, sourceindex.GenerationIdentity{}); err == nil {
		t.Fatalf("invalid identity lease error = %v", err)
	}
}

func TestResolveSourceIndexIdentityRequiresExactRetainedClosure(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	commit := commitFile(t, repo, "indexed.txt", []byte("indexed\n"), "indexed")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	retention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerOperationPacket, OwnerIdentity: "source-index-resolver"})
	if err != nil {
		t.Fatal(err)
	}
	options, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity(imported.Vault.VaultID, commit.commit, commit.tree, options)
	if err != nil {
		t.Fatal(err)
	}
	relationship := workflowstore.OperationPacketVaultRelationship{RetentionRowID: retention.ID, ClosureRowID: imported.Closure.ID, VaultRowID: imported.Vault.ID, OwnerIdentity: retention.OwnerIdentity, CommitOID: commit.commit, TreeOID: commit.tree}
	resolved, err := manager.ResolveSourceIndexIdentity(ctx, relationship)
	if err != nil || resolved != identity {
		t.Fatalf("resolved = %#v, err=%v", resolved, err)
	}
	wrongTree := relationship
	wrongTree.TreeOID = strings.Repeat("f", 40)
	if _, err := manager.ResolveSourceIndexIdentity(ctx, wrongTree); ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("wrong tree error = %v code=%q", err, ErrorCode(err))
	}
	if _, err := manager.ReleaseRetention(ctx, retention.RetentionID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResolveSourceIndexIdentity(ctx, relationship); ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("released closure error = %v code=%q", err, ErrorCode(err))
	}
}
