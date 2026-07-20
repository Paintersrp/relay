package sourcevault

import (
	"bytes"
	"context"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestCommitNodeParserPreservesOrderedHeadersParentsAndMessageCoordinates(t *testing.T) {
	commitOID := strings.Repeat("a", 40)
	treeOID := strings.Repeat("b", 40)
	firstParent := strings.Repeat("c", 40)
	secondParent := strings.Repeat("d", 40)
	raw := []byte("tree " + treeOID + "\nparent " + firstParent + "\nparent " + secondParent + "\nauthor Example <example@example.com> 1 +0000\ncommitter Example <example@example.com> 2 +0000\ngpgsig -----BEGIN\n continuation\n\nmessage\x00bytes\n")
	node, err := parseCommitNode(commitOID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if node.CommitOID != commitOID || node.TreeOID != treeOID || len(node.ParentOIDs) != 2 || node.ParentOIDs[0] != firstParent || node.ParentOIDs[1] != secondParent {
		t.Fatalf("node identity = %#v", node)
	}
	if node.RawSize != int64(len(raw)) || node.MessageOffset <= 0 || node.MessageOffset+node.MessageSize != node.RawSize || !bytes.Equal(raw[node.MessageOffset:], []byte("message\x00bytes\n")) {
		t.Fatalf("message coordinates = %#v", node)
	}
	if len(node.Headers) != 6 || string(node.Headers[5].Name) != "gpgsig" || !bytes.Contains(node.Headers[5].Raw, []byte(" continuation\n")) {
		t.Fatalf("headers = %#v", node.Headers)
	}
}

func TestRetainedFidelityReadsUseExactVaultAuthorityForCommitComparisonAndDiff(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	beforeCommit := commitFile(t, repo, "value.txt", []byte("before\n"), "before")
	afterCommit := commitFile(t, repo, "value.txt", []byte("after\n"), "after")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	target := storeTarget(t, ctx, store, "relay")
	beforeImport, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(target, beforeCommit.commit, beforeCommit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	afterImport, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(target, afterCommit.commit, afterCommit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	beforeRetention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: beforeImport.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerOperationPacket, OwnerIdentity: "packet-before"})
	if err != nil {
		t.Fatal(err)
	}
	afterRetention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: afterImport.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerOperationPacket, OwnerIdentity: "packet-after"})
	if err != nil {
		t.Fatal(err)
	}
	before := workflowstore.OperationPacketVaultRelationship{ID: 1, PublicationID: "publication", PacketRowID: 1, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:relay:anchor:before", OwnerIdentity: beforeRetention.OwnerIdentity, RetentionRowID: beforeRetention.ID, ClosureRowID: beforeImport.Closure.ID, VaultRowID: beforeImport.Vault.ID, CommitOID: beforeImport.CommitOID, TreeOID: beforeImport.TreeOID}
	after := workflowstore.OperationPacketVaultRelationship{ID: 2, PublicationID: "publication", PacketRowID: 1, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:relay:primary", OwnerIdentity: afterRetention.OwnerIdentity, RetentionRowID: afterRetention.ID, ClosureRowID: afterImport.Closure.ID, VaultRowID: afterImport.Vault.ID, CommitOID: afterImport.CommitOID, TreeOID: afterImport.TreeOID}

	node, err := manager.ReadRetainedCommitNode(ctx, ReadRetainedCommitNodeRequest{Relationship: after, CommitOID: after.CommitOID})
	if err != nil || node.CommitOID != after.CommitOID || node.TreeOID != after.TreeOID || len(node.ParentOIDs) != 1 || node.ParentOIDs[0] != before.CommitOID {
		t.Fatalf("commit node = %#v err=%v", node, err)
	}
	var raw []byte
	var offset int64
	for {
		page, readErr := manager.ReadRetainedCommitRange(ctx, ReadRetainedCommitRangeRequest{Relationship: after, CommitOID: after.CommitOID, Offset: offset, Limit: 7})
		if readErr != nil {
			t.Fatal(readErr)
		}
		raw = append(raw, page.Bytes...)
		offset += int64(len(page.Bytes))
		if offset == page.TotalSize {
			break
		}
	}
	if int64(len(raw)) != node.RawSize || !bytes.Contains(raw, []byte("after")) {
		t.Fatalf("raw commit bytes = %q", raw)
	}
	comparison, err := manager.ReadRetainedComparison(ctx, ReadRetainedComparisonRequest{Before: before, After: after})
	if err != nil || comparison.BeforeCommitOID != before.CommitOID || comparison.AfterCommitOID != after.CommitOID || len(comparison.BeforeEntries) != 1 || len(comparison.AfterEntries) != 1 || comparison.BeforeEntries[0].ObjectOID == comparison.AfterEntries[0].ObjectOID {
		t.Fatalf("comparison = %#v err=%v", comparison, err)
	}
	var diff []byte
	offset = 0
	for {
		page, readErr := manager.ReadRetainedDiffRange(ctx, ReadRetainedDiffRangeRequest{Before: before, After: after, Offset: offset, Limit: 9})
		if readErr != nil {
			t.Fatal(readErr)
		}
		diff = append(diff, page.Bytes...)
		offset += int64(len(page.Bytes))
		if offset == page.TotalSize {
			break
		}
	}
	if !bytes.Contains(diff, []byte("-before")) || !bytes.Contains(diff, []byte("+after")) {
		t.Fatalf("diff = %q", diff)
	}
	if _, err := manager.ReleaseRetention(ctx, beforeRetention.RetentionID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReadRetainedComparison(ctx, ReadRetainedComparisonRequest{Before: before, After: after}); ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("released before edge error = %v", err)
	}
}
