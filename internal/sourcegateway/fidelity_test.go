package sourcegateway

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	"relay/internal/app/operations"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type fidelityAuthorityFake struct {
	values map[string]operations.SourceReadAuthority
}

func (f fidelityAuthorityFake) ResolveSourceReadAuthority(_ context.Context, request operations.ResolveSourceReadAuthorityRequest) (operations.SourceReadAuthority, error) {
	value, ok := f.values[request.AnchorName]
	if !ok {
		return operations.SourceReadAuthority{}, &operations.Error{Code: operations.CodeRepositoryAuthorityUnavailable}
	}
	return value, nil
}

type fidelityVaultFake struct {
	trees      map[string][]sourcevault.RetainedTreeEntry
	blobs      map[string][]byte
	nodes      map[string]sourcevault.RetainedCommitNode
	comparison sourcevault.ReadRetainedComparisonResult
	diff       []byte
}

func (f *fidelityVaultFake) ReadRetainedTree(_ context.Context, request sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error) {
	entries, ok := f.trees[request.TreeOID]
	if !ok {
		return sourcevault.ReadRetainedTreeResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
	}
	return sourcevault.ReadRetainedTreeResult{TreeOID: request.TreeOID, Entries: append([]sourcevault.RetainedTreeEntry(nil), entries...)}, nil
}

func (f *fidelityVaultFake) ReadRetainedBlobRange(_ context.Context, request sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
	value, ok := f.blobs[request.BlobOID]
	if !ok || request.Offset < 0 || request.Offset > int64(len(value)) || request.Limit <= 0 {
		return sourcevault.ReadRetainedBlobRangeResult{}, &sourcevault.Error{Code: sourcevault.CodeInvalidRequest}
	}
	end := request.Offset + request.Limit
	if end < request.Offset || end > int64(len(value)) {
		end = int64(len(value))
	}
	return sourcevault.ReadRetainedBlobRangeResult{BlobOID: request.BlobOID, Offset: request.Offset, TotalSize: int64(len(value)), Bytes: append([]byte(nil), value[request.Offset:end]...)}, nil
}

func (f *fidelityVaultFake) ReadRetainedCommitNode(_ context.Context, request sourcevault.ReadRetainedCommitNodeRequest) (sourcevault.RetainedCommitNode, error) {
	value, ok := f.nodes[request.CommitOID]
	if !ok {
		return sourcevault.RetainedCommitNode{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
	}
	return value, nil
}

func (f *fidelityVaultFake) ReadRetainedCommitRange(_ context.Context, request sourcevault.ReadRetainedCommitRangeRequest) (sourcevault.ReadRetainedCommitRangeResult, error) {
	node, ok := f.nodes[request.CommitOID]
	if !ok || request.Offset < 0 || request.Offset > node.RawSize || request.Limit <= 0 {
		return sourcevault.ReadRetainedCommitRangeResult{}, &sourcevault.Error{Code: sourcevault.CodeInvalidRequest}
	}
	raw := make([]byte, node.RawSize)
	end := request.Offset + request.Limit
	if end > node.RawSize {
		end = node.RawSize
	}
	return sourcevault.ReadRetainedCommitRangeResult{CommitOID: request.CommitOID, Offset: request.Offset, TotalSize: node.RawSize, Bytes: append([]byte(nil), raw[request.Offset:end]...)}, nil
}

func (f *fidelityVaultFake) ReadRetainedComparison(context.Context, sourcevault.ReadRetainedComparisonRequest) (sourcevault.ReadRetainedComparisonResult, error) {
	return f.comparison, nil
}

func (f *fidelityVaultFake) ReadRetainedDiffRange(_ context.Context, request sourcevault.ReadRetainedDiffRangeRequest) (sourcevault.ReadRetainedDiffRangeResult, error) {
	if request.Offset < 0 || request.Offset > int64(len(f.diff)) || request.Limit <= 0 {
		return sourcevault.ReadRetainedDiffRangeResult{}, &sourcevault.Error{Code: sourcevault.CodeInvalidRequest}
	}
	end := request.Offset + request.Limit
	if end > int64(len(f.diff)) {
		end = int64(len(f.diff))
	}
	return sourcevault.ReadRetainedDiffRangeResult{BeforeCommitOID: request.Before.CommitOID, AfterCommitOID: request.After.CommitOID, Offset: request.Offset, TotalSize: int64(len(f.diff)), Bytes: append([]byte(nil), f.diff[request.Offset:end]...)}, nil
}

type fidelitySelectorFake struct {
	values map[string]workflowstore.SourcePathSelector
}

func (f *fidelitySelectorFake) CreateOrGetSourcePathSelector(_ context.Context, params workflowstore.CreateOrGetSourcePathSelectorParams) (workflowstore.SourcePathSelector, error) {
	if value, ok := f.values[params.SelectorID]; ok {
		return value, nil
	}
	value := workflowstore.SourcePathSelector{ID: int64(len(f.values) + 1), SelectorID: params.SelectorID, PacketRowID: params.PacketRowID, PacketID: params.PacketID, SurfaceContractID: params.SurfaceContractID, OperationID: params.OperationID, ProjectID: params.ProjectID, RepositoryKey: params.RepositoryKey, PublicationID: params.PublicationID, VaultRelationshipRowID: params.VaultRelationshipRowID, CommitOID: params.CommitOID, TreeOID: params.TreeOID, PathID: params.PathID, PathByteLength: int64(len(params.PathBytes)), PathBytes: append([]byte(nil), params.PathBytes...)}
	f.values[params.SelectorID] = value
	return value, nil
}

func (f *fidelitySelectorFake) GetSourcePathSelector(_ context.Context, selectorID string) (workflowstore.SourcePathSelector, error) {
	value, ok := f.values[selectorID]
	if !ok {
		return workflowstore.SourcePathSelector{}, sql.ErrNoRows
	}
	return value, nil
}

func fidelityAuthority(commitOID, treeOID, anchorName string, relationshipID int64) operations.SourceReadAuthority {
	dependencyKey := "repository:relay:primary"
	if anchorName != "" {
		dependencyKey = "repository:relay:anchor:" + anchorName
	}
	return operations.SourceReadAuthority{
		Summary:       operations.PacketSummary{PacketID: "opkt-fidelity", PacketSHA256: strings.Repeat("a", 64), Role: "planner", OperationID: "planner.requirements", SurfaceContract: "planner-authoring.v1", ProjectID: "project-fidelity", ReadinessState: workflowstore.OperationPacketReadinessReady, LifecycleState: workflowstore.OperationPacketLifecycleActive},
		PacketRowID:   11,
		PublicationID: "publication-fidelity",
		RepositoryKey: "relay",
		DependencyKey: dependencyKey,
		AnchorName:    anchorName,
		Relationship:  workflowstore.OperationPacketVaultRelationship{ID: relationshipID, PublicationID: "publication-fidelity", PacketRowID: 11, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: dependencyKey, OwnerIdentity: "owner-" + dependencyKey, RetentionRowID: relationshipID + 10, ClosureRowID: relationshipID + 20, VaultRowID: 9, CommitOID: commitOID, TreeOID: treeOID},
	}
}

func newFidelityService(t *testing.T, vault interface {
	VaultReader
	FidelityVaultReader
}, authorities ...operations.SourceReadAuthority) *Service {
	t.Helper()
	values := make(map[string]operations.SourceReadAuthority, len(authorities))
	for _, authority := range authorities {
		values[authority.AnchorName] = authority
	}
	codec, err := NewHMACCursorCodec([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(fidelityAuthorityFake{values: values}, vault, &fidelitySelectorFake{values: map[string]workflowstore.SourcePathSelector{}}, codec)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestReadTextPreservesExactBytesAcrossMixedTerminatorsAndOversizedLines(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	treeOID := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	data := []byte("a\r\nb\rc\n" + strings.Repeat("é", 12))
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{treeOID: {{Name: []byte("text.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: data}, nodes: map[string]sourcevault.RetainedCommitNode{commitOID: {CommitOID: commitOID, TreeOID: treeOID, RawSize: 10, MessageOffset: 5, MessageSize: 5}}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, treeOID, "", 1))
	request := ReadTextRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Path: PathReference{PathID: pathID([]byte("text.txt")), InlineBase64: canonicalInline([]byte("text.txt"))}, Limit: MinTextPageBytes}
	var reconstructed []byte
	for {
		result, err := service.ReadText(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		for _, segment := range result.Segments {
			reconstructed = append(reconstructed, segment.Bytes...)
			reconstructed = append(reconstructed, segment.Terminator...)
		}
		if result.Complete {
			break
		}
		if result.Cursor == "" {
			t.Fatal("incomplete text page omitted cursor")
		}
		request.Cursor = result.Cursor
	}
	if !bytes.Equal(reconstructed, data) {
		t.Fatalf("reconstructed = %q want %q", reconstructed, data)
	}
}

func TestReadTextRejectsInvalidUTF8WithoutChangingBlobAuthority(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	treeOID := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{treeOID: {{Name: []byte("bad.bin"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: {'a', 0xff}}, nodes: map[string]sourcevault.RetainedCommitNode{commitOID: {CommitOID: commitOID, TreeOID: treeOID, RawSize: 10, MessageOffset: 5, MessageSize: 5}}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, treeOID, "", 1))
	_, err := service.ReadText(context.Background(), ReadTextRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Path: PathReference{PathID: pathID([]byte("bad.bin")), InlineBase64: canonicalInline([]byte("bad.bin"))}, Limit: MinTextPageBytes})
	if ErrorCode(err) != CodeInvalidTextProjection {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
}

func TestCommitHistoryIsDistanceFirstAndPathHistoryContinuesAfterAbsentStart(t *testing.T) {
	rootCommit := strings.Repeat("1", 40)
	leftCommit := strings.Repeat("2", 40)
	rightCommit := strings.Repeat("3", 40)
	startCommit := strings.Repeat("4", 40)
	rootTree := strings.Repeat("a", 40)
	leftTree := strings.Repeat("b", 40)
	rightTree := strings.Repeat("c", 40)
	startTree := strings.Repeat("d", 40)
	blobOID := strings.Repeat("e", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			startTree: {},
			leftTree:  {{Name: []byte("x"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}},
			rightTree: {},
			rootTree:  {{Name: []byte("x"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}},
		},
		nodes: map[string]sourcevault.RetainedCommitNode{
			startCommit: {CommitOID: startCommit, TreeOID: startTree, ParentOIDs: []string{rightCommit, leftCommit}, RawSize: 10, MessageOffset: 5, MessageSize: 5},
			leftCommit:  {CommitOID: leftCommit, TreeOID: leftTree, ParentOIDs: []string{rootCommit}, RawSize: 10, MessageOffset: 5, MessageSize: 5},
			rightCommit: {CommitOID: rightCommit, TreeOID: rightTree, ParentOIDs: []string{rootCommit}, RawSize: 10, MessageOffset: 5, MessageSize: 5},
			rootCommit:  {CommitOID: rootCommit, TreeOID: rootTree, RawSize: 10, MessageOffset: 5, MessageSize: 5},
		},
	}
	service := newFidelityService(t, vault, fidelityAuthority(startCommit, startTree, "", 1))
	history, err := service.CommitHistory(context.Background(), CommitHistoryRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{startCommit, leftCommit, rightCommit, rootCommit}
	for index, want := range wantOrder {
		if history.Entries[index].CommitOID != want {
			t.Fatalf("history[%d] = %q want %q", index, history.Entries[index].CommitOID, want)
		}
	}
	pathHistory, err := service.PathHistory(context.Background(), PathHistoryRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", PathSeed: []byte("x"), Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(pathHistory.Entries) != 3 || pathHistory.Entries[0].CommitOID != startCommit || pathHistory.Entries[0].State.Present || pathHistory.Entries[1].CommitOID != rightCommit || pathHistory.Entries[1].State.Present || pathHistory.Entries[2].CommitOID != rootCommit || !pathHistory.Entries[2].State.Present {
		t.Fatalf("path history = %#v", pathHistory.Entries)
	}
}

func TestComparisonAndDiffUseIndependentContinuations(t *testing.T) {
	beforeCommit := strings.Repeat("1", 40)
	afterCommit := strings.Repeat("2", 40)
	beforeTree := strings.Repeat("a", 40)
	afterTree := strings.Repeat("b", 40)
	objectOID := strings.Repeat("f", 40)
	before := fidelityAuthority(beforeCommit, beforeTree, "base", 2)
	after := fidelityAuthority(afterCommit, afterTree, "", 1)
	vault := &fidelityVaultFake{
		blobs: map[string][]byte{objectOID: []byte("same")},
		comparison: sourcevault.ReadRetainedComparisonResult{
			BeforeCommitOID: beforeCommit,
			BeforeTreeOID:   beforeTree,
			AfterCommitOID:  afterCommit,
			AfterTreeOID:    afterTree,
			BeforeEntries:   []sourcevault.RetainedPathEntry{{Path: []byte("old"), Mode: "100644", ObjectType: "blob", ObjectOID: objectOID}},
			AfterEntries:    []sourcevault.RetainedPathEntry{{Path: []byte("new"), Mode: "100644", ObjectType: "blob", ObjectOID: objectOID}},
		},
		diff: []byte("diff --git a/old b/new\n"),
	}
	service := newFidelityService(t, vault, after, before)
	comparison, err := service.Compare(context.Background(), CompareRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Before: RevisionReference{AnchorName: "base"}, Limit: 1})
	if err != nil || len(comparison.Entries) != 1 || comparison.Entries[0].Kind != ChangeRename || !comparison.Complete {
		t.Fatalf("comparison = %#v err=%v", comparison, err)
	}
	diffRequest := ReadDiffRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Before: RevisionReference{AnchorName: "base"}, Offset: 0, Limit: 5}
	var diff []byte
	for {
		page, readErr := service.ReadDiff(context.Background(), diffRequest)
		if readErr != nil {
			t.Fatal(readErr)
		}
		diff = append(diff, page.Bytes...)
		if page.Complete {
			break
		}
		diffRequest.Cursor = page.Cursor
	}
	if !bytes.Equal(diff, vault.diff) {
		t.Fatalf("diff = %q want %q", diff, vault.diff)
	}
}
