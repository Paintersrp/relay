package operations

import (
	"context"
	"path/filepath"
	"testing"

	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func TestLegacyLifecycleMethodsRequireCompleteCoordinator(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, CreateInput{Document: packet.Document{}}); ErrorCode(err) != CodeCompleteLifecycleRequired {
		t.Fatalf("create error = %v code=%q", err, ErrorCode(err))
	}
	if _, err := service.Refresh(ctx, RefreshInput{PriorPacketID: "opkt-prior"}); ErrorCode(err) != CodeCompleteLifecycleRequired {
		t.Fatalf("refresh error = %v code=%q", err, ErrorCode(err))
	}
	if _, err := service.Close(ctx, CloseInput{PacketID: "opkt-prior"}); ErrorCode(err) != CodeCompleteLifecycleRequired {
		t.Fatalf("close error = %v code=%q", err, ErrorCode(err))
	}
}

func TestReadAndAuthorizationRemainAvailableForCoordinatedPackets(t *testing.T) {
	ctx := context.Background()
	publication, store := openAuthorityPublicationRemediationService(t, ctx)
	input := authorityPublicationCreateInput(t, "mutation-read", "opkt-read", "artifact-read", "planner.plan", "planner-plan.v1")
	created, err := publication.Publish(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, created.Packet.PacketID)
	if err != nil || view.Summary.PacketID != created.Packet.PacketID {
		t.Fatalf("view = %#v err=%v", view, err)
	}
	mutation, err := service.AuthorizeMutation(ctx, MutationRequest{PacketID: created.Packet.PacketID, SurfaceContract: "planner-plan.v1", OperationID: "planner.plan", Action: "submit_plan"})
	if err != nil || !mutation.Allowed {
		t.Fatalf("mutation authorization = %#v err=%v", mutation, err)
	}
	read, err := service.AuthorizeRead(ctx, ReadRequest{PacketID: created.Packet.PacketID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: input.PacketArtifactID})
	if err != nil || read.OwnerIdentity != input.PacketArtifactID || read.Summary.OperationID != registry.OperationID("planner.plan") {
		t.Fatalf("read authorization = %#v err=%v", read, err)
	}
}
