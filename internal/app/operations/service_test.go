package operations

import (
	"context"
	"testing"

	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

func TestLegacyLifecycleMethodsRequireCompleteCoordinator(t *testing.T) {
	ctx := context.Background()
	store := workflowfixture.Open(t, workflowstore.Open)
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
	input := authorityPublicationCreateInput(t, "mutation-read", "opkt-read", "artifact-read", "wayfinder.workspace", "wayfinder-workspace.v1")
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
	operation, ok := registry.Lookup("wayfinder.workspace")
	if !ok || len(operation.AllowedNonSourceActions) == 0 {
		t.Fatal("workspace has no allowed action")
	}
	mutation, err := service.AuthorizeMutation(ctx, MutationRequest{PacketID: created.Packet.PacketID, SurfaceContract: "wayfinder-workspace.v1", OperationID: "wayfinder.workspace", Action: operation.AllowedNonSourceActions[0]})
	if err != nil || !mutation.Allowed {
		t.Fatalf("mutation authorization = %#v err=%v", mutation, err)
	}
	read, err := service.AuthorizeRead(ctx, ReadRequest{PacketID: created.Packet.PacketID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: input.PacketArtifactID})
	if err != nil || read.OwnerIdentity != input.PacketArtifactID || read.Summary.OperationID != registry.OperationID("wayfinder.workspace") {
		t.Fatalf("read authorization = %#v err=%v", read, err)
	}
}
