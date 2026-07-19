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

type gatewayAuthorityFake struct {
	value operations.SourceReadAuthority
	err   error
}

func (f gatewayAuthorityFake) ResolveSourceReadAuthority(context.Context, operations.ResolveSourceReadAuthorityRequest) (operations.SourceReadAuthority, error) {
	return f.value, f.err
}

type gatewayVaultFake struct {
	trees map[string][]sourcevault.RetainedTreeEntry
	blobs map[string][]byte
}

func (f gatewayVaultFake) ReadRetainedTree(_ context.Context, request sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error) {
	values, ok := f.trees[request.TreeOID]
	if !ok {
		return sourcevault.ReadRetainedTreeResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
	}
	return sourcevault.ReadRetainedTreeResult{TreeOID: request.TreeOID, Entries: append([]sourcevault.RetainedTreeEntry(nil), values...)}, nil
}
func (f gatewayVaultFake) ReadRetainedBlobRange(_ context.Context, request sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
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

type gatewaySelectorFake struct {
	values map[string]workflowstore.SourcePathSelector
}

func (f *gatewaySelectorFake) CreateOrGetSourcePathSelector(_ context.Context, params workflowstore.CreateOrGetSourcePathSelectorParams) (workflowstore.SourcePathSelector, error) {
	if value, ok := f.values[params.SelectorID]; ok {
		return value, nil
	}
	value := workflowstore.SourcePathSelector{ID: int64(len(f.values) + 1), SelectorID: params.SelectorID, PacketRowID: params.PacketRowID, PacketID: params.PacketID, SurfaceContractID: params.SurfaceContractID, OperationID: params.OperationID, ProjectID: params.ProjectID, RepositoryKey: params.RepositoryKey, PublicationID: params.PublicationID, VaultRelationshipRowID: params.VaultRelationshipRowID, CommitOID: params.CommitOID, TreeOID: params.TreeOID, PathID: params.PathID, PathByteLength: int64(len(params.PathBytes)), PathBytes: append([]byte(nil), params.PathBytes...)}
	f.values[params.SelectorID] = value
	return value, nil
}
func (f *gatewaySelectorFake) GetSourcePathSelector(_ context.Context, selectorID string) (workflowstore.SourcePathSelector, error) {
	value, ok := f.values[selectorID]
	if !ok {
		return workflowstore.SourcePathSelector{}, sql.ErrNoRows
	}
	return value, nil
}

func gatewayAuthority(lifecycle string) operations.SourceReadAuthority {
	return operations.SourceReadAuthority{Summary: operations.PacketSummary{PacketID: "opkt-gateway", PacketSHA256: strings.Repeat("a", 64), Role: "planner", OperationID: "planner.requirements", SurfaceContract: "planner-authoring.v1", ProjectID: "project-gateway", ReadinessState: workflowstore.OperationPacketReadinessReady, LifecycleState: lifecycle}, PacketRowID: 11, PublicationID: "publication-gateway", RepositoryKey: "relay", Relationship: workflowstore.OperationPacketVaultRelationship{ID: 19, PublicationID: "publication-gateway", PacketRowID: 11, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:relay:primary", OwnerIdentity: "owner-gateway", RetentionRowID: 7, ClosureRowID: 8, VaultRowID: 9, CommitOID: strings.Repeat("b", 40), TreeOID: strings.Repeat("c", 40)}}
}
func newGatewayService(t *testing.T, lifecycle string) *Service {
	t.Helper()
	codec, err := NewHMACCursorCodec([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	vault := gatewayVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{strings.Repeat("c", 40): {{Name: []byte("a"), Mode: "040000", ObjectType: "tree", ObjectOID: strings.Repeat("d", 40)}, {Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("e", 40)}, {Name: []byte{0xff}, Mode: "100755", ObjectType: "blob", ObjectOID: strings.Repeat("f", 40)}}, strings.Repeat("d", 40): {{Name: []byte("x"), Mode: "120000", ObjectType: "blob", ObjectOID: strings.Repeat("1", 40)}}}, blobs: map[string][]byte{strings.Repeat("e", 40): {0, 1, 2, 3, 4}, strings.Repeat("1", 40): []byte("target")}}
	service, err := NewService(gatewayAuthorityFake{value: gatewayAuthority(lifecycle)}, vault, &gatewaySelectorFake{values: map[string]workflowstore.SourcePathSelector{}}, codec)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestListTreeIsPageSizeInvariantAndUsesFullPathByteOrder(t *testing.T) {
	service := newGatewayService(t, workflowstore.OperationPacketLifecycleActive)
	request := ListTreeRequest{PacketID: "opkt-gateway", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Recursive: true, Limit: 2}
	var paths [][]byte
	for {
		result, err := service.ListTree(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range result.Entries {
			value, ok := decodeCanonicalInline(entry.Path.InlineBase64)
			if !ok {
				t.Fatalf("path is not inline: %#v", entry.Path)
			}
			paths = append(paths, value)
		}
		if result.Complete {
			break
		}
		if result.Cursor == "" {
			t.Fatal("incomplete page omitted continuation")
		}
		request.Cursor = result.Cursor
	}
	want := [][]byte{[]byte("a"), []byte("a.txt"), []byte("a/x"), {0xff}}
	if len(paths) != len(want) {
		t.Fatalf("paths = %q", paths)
	}
	for index := range want {
		if !bytes.Equal(paths[index], want[index]) {
			t.Fatalf("path %d = %x want %x", index, paths[index], want[index])
		}
	}
}

func TestReadBlobContinuationReturnsExactBytesAcrossLifecycleStates(t *testing.T) {
	for _, lifecycle := range []string{workflowstore.OperationPacketLifecycleActive, workflowstore.OperationPacketLifecycleSuperseded, workflowstore.OperationPacketLifecycleClosed} {
		service := newGatewayService(t, lifecycle)
		request := ReadBlobRequest{PacketID: "opkt-gateway", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Path: PathReference{PathID: pathID([]byte("a.txt")), InlineBase64: canonicalInline([]byte("a.txt"))}, Offset: 0, Limit: 2}
		var data []byte
		for {
			result, err := service.ReadBlob(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if result.Source.LifecycleState != lifecycle {
				t.Fatalf("lifecycle = %q want %q", result.Source.LifecycleState, lifecycle)
			}
			data = append(data, result.Bytes...)
			if result.Complete {
				break
			}
			request.Cursor = result.Cursor
		}
		if !bytes.Equal(data, []byte{0, 1, 2, 3, 4}) {
			t.Fatalf("%s bytes = %v", lifecycle, data)
		}
	}
}

func TestTreeCursorCannotCrossRequestBounds(t *testing.T) {
	service := newGatewayService(t, workflowstore.OperationPacketLifecycleActive)
	request := ListTreeRequest{PacketID: "opkt-gateway", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Recursive: true, Limit: 1}
	first, err := service.ListTree(context.Background(), request)
	if err != nil || first.Complete || first.Cursor == "" {
		t.Fatalf("first = %#v err=%v", first, err)
	}
	request.Limit = 2
	request.Cursor = first.Cursor
	if _, err := service.ListTree(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("cross-bound cursor error = %v", err)
	}
}
