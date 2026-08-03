package operations

import (
	"bytes"
	"context"
	"testing"

	"relay/internal/mcp/semanticidentity"
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
	fixture := openLifecycleFixture(t)
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{
		MutationID: "create-wayfinder-workspace-read",
		Identity: semanticidentity.CreateOperationPacket{
			SurfaceContract: "wayfinder-workspace.v1",
			OperationID:     "wayfinder.workspace",
			ProjectID:       fixture.projectID,
		},
	})
	if err != nil || created.Replay || created.Packet.Summary.PacketID == "" {
		t.Fatalf("created = %#v err=%v", created, err)
	}
	if created.Packet.Summary.Role != "wayfinder" || created.Packet.Summary.OperationID != "wayfinder.workspace" || created.Packet.Summary.SurfaceContract != "wayfinder-workspace.v1" || created.Packet.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive {
		t.Fatalf("created summary = %#v", created.Packet.Summary)
	}
	service, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(fixture.ctx, created.Packet.Summary.PacketID)
	if err != nil || view.Summary.PacketID != created.Packet.Summary.PacketID || !bytes.Equal(view.DocumentBytes, created.Packet.DocumentBytes) {
		t.Fatalf("view = %#v err=%v", view, err)
	}
	mutation, err := service.AuthorizeMutation(fixture.ctx, MutationRequest{
		PacketID:        created.Packet.Summary.PacketID,
		SurfaceContract: "wayfinder-workspace.v1",
		OperationID:     "wayfinder.workspace",
		Action:          registry.AllowedAction("create_workspace"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.Allowed {
		t.Fatalf("mutation authorization = %#v", mutation)
	}
	packetRow, err := fixture.store.GetOperationPacketByPacketID(fixture.ctx, created.Packet.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	packetArtifact, err := fixture.store.GetOperationPacketArtifact(fixture.ctx, packetRow.PacketArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := service.AuthorizeRead(fixture.ctx, ReadRequest{
		PacketID:        created.Packet.Summary.PacketID,
		DependencyClass: workflowstore.OperationPacketDependencyPacketDocument,
		DependencyKey:   packetArtifact.ArtifactID,
	})
	if err != nil || read.OwnerIdentity != packetArtifact.ArtifactID || read.Summary.OperationID != "wayfinder.workspace" || read.Summary.SurfaceContract != "wayfinder-workspace.v1" || read.Summary.Role != "wayfinder" {
		t.Fatalf("read authorization = %#v err=%v", read, err)
	}
}
