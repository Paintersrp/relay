package operations

import (
	"context"
	"testing"

	"relay/internal/operations/registry"
)

type fakeWayfinderPacketAuthorizer struct {
	request MutationRequest
	err     error
}

func (f *fakeWayfinderPacketAuthorizer) AuthorizeMutation(_ context.Context, request MutationRequest) (MutationAuthorization, error) {
	f.request = request
	return MutationAuthorization{Allowed: true}, f.err
}

func TestWayfinderAdmissionRequiresRegisteredPacketOperation(t *testing.T) {
	fake := &fakeWayfinderPacketAuthorizer{}
	service, err := NewWayfinderAdmissionService(fake)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AdmitWayfinderMutation(context.Background(), WayfinderMutationRequest{PacketID: "packet-1", OperationID: "wayfinder.workspace", Action: "create_workspace", RequiredDependencies: []DependencyRequirement{{Class: "manifest_member", Key: "workspace-manifest"}}})
	if err != nil {
		t.Fatal(err)
	}
	if fake.request.SurfaceContract != "wayfinder-workspace.v1" || fake.request.OperationID != "wayfinder.workspace" || fake.request.Action != "create_workspace" {
		t.Fatalf("packet request = %#v", fake.request)
	}
	if _, err := service.AdmitWayfinderMutation(context.Background(), WayfinderMutationRequest{PacketID: "packet-1", OperationID: registry.OperationID("wayfinder.unregistered"), Action: "create_workspace"}); err == nil {
		t.Fatal("unregistered mutation was admitted")
	}
}
