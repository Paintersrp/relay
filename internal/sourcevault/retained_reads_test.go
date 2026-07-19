package sourcevault

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestRetainedTreeAndBlobRangeUseExactVaultAuthority(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	first := commitFile(t, repo, "payload.bin", []byte{0, 1, 2, 3, 4, 5}, "payload")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), first.commit, first.tree)})
	if err != nil {
		t.Fatal(err)
	}
	retention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerOperationPacket, OwnerIdentity: "packet-source-reader"})
	if err != nil {
		t.Fatal(err)
	}
	relationship := workflowstore.OperationPacketVaultRelationship{ID: 1, PublicationID: "publication-source-reader", PacketRowID: 1, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:relay:primary", OwnerIdentity: retention.OwnerIdentity, RetentionRowID: retention.ID, ClosureRowID: imported.Closure.ID, VaultRowID: imported.Vault.ID, CommitOID: imported.CommitOID, TreeOID: imported.TreeOID}
	tree, err := manager.ReadRetainedTree(ctx, ReadRetainedTreeRequest{Relationship: relationship, TreeOID: first.tree})
	if err != nil || len(tree.Entries) != 1 || !bytes.Equal(tree.Entries[0].Name, []byte("payload.bin")) || tree.Entries[0].Mode != "100644" || tree.Entries[0].ObjectType != "blob" || tree.Entries[0].ObjectOID != first.blob {
		t.Fatalf("tree = %#v err=%v", tree, err)
	}
	page, err := manager.ReadRetainedBlobRange(ctx, ReadRetainedBlobRangeRequest{Relationship: relationship, BlobOID: first.blob, Offset: 2, Limit: 3})
	if err != nil || page.Offset != 2 || page.TotalSize != 6 || !bytes.Equal(page.Bytes, []byte{2, 3, 4}) {
		t.Fatalf("page = %#v err=%v", page, err)
	}
	if _, err := manager.ReleaseRetention(ctx, retention.RetentionID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReadRetainedTree(ctx, ReadRetainedTreeRequest{Relationship: relationship, TreeOID: first.tree}); ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("released retention error = %v", err)
	}
}

func TestCommandGitTreeParserPreservesInvalidUTF8AndDeterministicOrder(t *testing.T) {
	invalid := rawTreeEntry("100755", []byte{0xff, 'x'}, strings.Repeat("3", 40))
	raw := append(append(rawTreeEntry("100644", []byte("a.txt"), strings.Repeat("1", 40)), rawTreeEntry("40000", []byte("a"), strings.Repeat("2", 40))...), invalid...)
	entries, err := parseRawTree(bytes.NewReader(raw))
	if err != nil || len(entries) != 3 {
		t.Fatalf("entries = %#v err=%v", entries, err)
	}
	if !bytes.Equal(entries[0].Name, []byte("a")) || !bytes.Equal(entries[1].Name, []byte("a.txt")) || !bytes.Equal(entries[2].Name, []byte{0xff, 'x'}) {
		t.Fatalf("order = %x %x %x", entries[0].Name, entries[1].Name, entries[2].Name)
	}
	if entries[0].Mode != "040000" || entries[0].ObjectType != "tree" || entries[2].Mode != "100755" {
		t.Fatalf("entries = %#v", entries)
	}
}
func rawTreeEntry(mode string, name []byte, oid string) []byte {
	value := append([]byte(mode+" "), name...)
	value = append(value, 0)
	decoded, err := hex.DecodeString(oid)
	if err != nil {
		panic(err)
	}
	return append(value, decoded...)
}
