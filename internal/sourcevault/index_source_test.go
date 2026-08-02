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

func TestResolveSourceIndexIdentityRejectsEveryRelationshipMismatch(t *testing.T) {
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
	base := workflowstore.OperationPacketVaultRelationship{RetentionRowID: retention.ID, ClosureRowID: imported.Closure.ID, VaultRowID: imported.Vault.ID, OwnerIdentity: retention.OwnerIdentity, CommitOID: commit.commit, TreeOID: commit.tree}
	resolved, err := manager.ResolveSourceIndexIdentity(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.VaultID != imported.Vault.VaultID || resolved.CommitOID != commit.commit || resolved.TreeOID != commit.tree || resolved.BuildOptionsSHA256 != digest {
		t.Fatalf("resolved identity = %#v", resolved)
	}
	for _, tc := range []struct {
		name string
		edit func(*workflowstore.OperationPacketVaultRelationship)
	}{
		{"owner", func(r *workflowstore.OperationPacketVaultRelationship) { r.OwnerIdentity = "other" }},
		{"closure", func(r *workflowstore.OperationPacketVaultRelationship) { r.ClosureRowID++ }},
		{"vault", func(r *workflowstore.OperationPacketVaultRelationship) { r.VaultRowID++ }},
		{"commit", func(r *workflowstore.OperationPacketVaultRelationship) { r.CommitOID = strings.Repeat("0", 40) }},
		{"tree", func(r *workflowstore.OperationPacketVaultRelationship) { r.TreeOID = strings.Repeat("0", 40) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relationship := base
			tc.edit(&relationship)
			if _, err := manager.ResolveSourceIndexIdentity(ctx, relationship); ErrorCode(err) != CodeVaultUnavailable {
				t.Fatalf("error = %v, code = %q", err, ErrorCode(err))
			}
		})
	}
}
