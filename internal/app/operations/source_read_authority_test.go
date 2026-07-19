package operations

import (
	"testing"

	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func sourceReadCloseIdentity(packetID string) semanticidentity.CloseOperationPacket {
	return semanticidentity.CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: packetID}
}

func TestResolveSourceReadAuthorityPreservesActiveSupersededAndClosedPacketIdentity(t *testing.T) {
	fixture := openLifecycleFixture(t)
	prior := createLifecycleRequirementsPacket(t, fixture, "create-source-authority")
	service, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	request := ResolveSourceReadAuthorityRequest{PacketID: prior.Packet.Summary.PacketID, SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "project"}
	active, err := service.ResolveSourceReadAuthority(fixture.ctx, request)
	if err != nil || active.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || active.Relationship.DependencyKey != "repository:project:primary" {
		t.Fatalf("active = %#v err=%v", active, err)
	}
	refreshed, err := fixture.service.Refresh(fixture.ctx, lifecycleRefreshRequest(fixture, request.PacketID, "refresh-source-authority"))
	if err != nil {
		t.Fatal(err)
	}
	superseded, err := service.ResolveSourceReadAuthority(fixture.ctx, request)
	if err != nil || superseded.Summary.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded || superseded.Summary.PacketID != request.PacketID {
		t.Fatalf("superseded = %#v err=%v", superseded, err)
	}
	closeResult, err := fixture.service.Close(fixture.ctx, CloseLifecycleInput{MutationID: "close-source-authority", Identity: sourceReadCloseIdentity(refreshed.Packet.Summary.PacketID)})
	if err != nil {
		t.Fatal(err)
	}
	closedRequest := request
	closedRequest.PacketID = closeResult.Packet.PacketID
	closed, err := service.ResolveSourceReadAuthority(fixture.ctx, closedRequest)
	if err != nil || closed.Summary.LifecycleState != workflowstore.OperationPacketLifecycleClosed {
		t.Fatalf("closed = %#v err=%v", closed, err)
	}
	wrong := request
	wrong.OperationID = registry.OperationID("planner.design")
	if _, err := service.ResolveSourceReadAuthority(fixture.ctx, wrong); ErrorCode(err) != CodePacketRouteMismatch {
		t.Fatalf("wrong route error = %v", err)
	}
}
