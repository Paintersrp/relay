package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appoperations "relay/internal/app/operations"
	"relay/internal/operations/registry"
)

type fakeMCPTicketPacketAuthorizer struct {
	request appoperations.MutationRequest
}

func (f *fakeMCPTicketPacketAuthorizer) AuthorizeMutation(_ context.Context, request appoperations.MutationRequest) (appoperations.MutationAuthorization, error) {
	f.request = request
	return appoperations.MutationAuthorization{Allowed: true}, nil
}

func TestTicketOperationIdentityCanonicalizesDependenciesAndSelectionMembers(t *testing.T) {
	identity := TicketOperationIdentity{
		MutationID: "mutation-1", ExpectedPacketID: "packet-1", OperationID: string(registry.LocalOperatorTicketWorkflowOperationID),
		Action: string(registry.TicketActionSelect), WorkspaceID: "workspace-1", PayloadSHA256: strings.Repeat("a", 64),
		SelectionMembers:     []TicketSelectionMemberIdentity{{TicketID: "TICKET-2", RevisionRowID: 2}, {TicketID: "TICKET-1", RevisionRowID: 1}},
		RequiredDependencies: []TicketPacketDependency{{Class: "workflow_snapshot", Key: "ticket:TICKET-2"}, {Class: "workflow_snapshot", Key: "ticket:TICKET-1"}},
	}
	first, err := identity.SemanticRequestSHA256()
	if err != nil || first == "" {
		t.Fatalf("first fingerprint = %q, %v", first, err)
	}
	identity.SelectionMembers[0], identity.SelectionMembers[1] = identity.SelectionMembers[1], identity.SelectionMembers[0]
	identity.RequiredDependencies[0], identity.RequiredDependencies[1] = identity.RequiredDependencies[1], identity.RequiredDependencies[0]
	second, err := identity.SemanticRequestSHA256()
	if err != nil || second != first {
		t.Fatalf("reordered fingerprint = %q, %v; want %q", second, err, first)
	}
	identity.SelectionMembers[0].RevisionRowID = 3
	changed, err := identity.SemanticRequestSHA256()
	if err != nil || changed == first {
		t.Fatalf("changed selection fingerprint = %q, %v", changed, err)
	}
	identity.RequiredDependencies = append(identity.RequiredDependencies, identity.RequiredDependencies[0])
	if _, err := identity.SemanticRequestSHA256(); err == nil {
		t.Fatal("duplicate retained dependency was accepted")
	}
}

func TestTicketMutationIdentityBindsAuthoritySourceAndPayload(t *testing.T) {
	identity := TicketOperationIdentity{
		MutationID: "mutation-1", ExpectedPacketID: "packet-1", OperationID: string(registry.LocalOperatorTicketWorkflowOperationID),
		Action: string(registry.TicketActionApprove), WorkspaceID: "workspace-1", TicketID: "TICKET-1", RevisionRowID: 7,
		AuthorityRevisionID: "authority-1", SourceClosureRowID: 9, PayloadSHA256: strings.Repeat("a", 64),
	}
	first, err := identity.SemanticRequestSHA256()
	if err != nil {
		t.Fatal(err)
	}
	identity.AuthorityRevisionID = "authority-2"
	authorityChanged, err := identity.SemanticRequestSHA256()
	if err != nil || authorityChanged == first {
		t.Fatalf("authority fingerprint = %q, %v", authorityChanged, err)
	}
	identity.AuthorityRevisionID = "authority-1"
	identity.SourceClosureRowID = 10
	sourceChanged, err := identity.SemanticRequestSHA256()
	if err != nil || sourceChanged == first {
		t.Fatalf("source fingerprint = %q, %v", sourceChanged, err)
	}
	identity.SourceClosureRowID = 9
	identity.PayloadSHA256 = strings.Repeat("b", 64)
	payloadChanged, err := identity.SemanticRequestSHA256()
	if err != nil || payloadChanged == first {
		t.Fatalf("payload fingerprint = %q, %v", payloadChanged, err)
	}
}

func TestTicketOperationIdentityStrictDecodeAndRouteValidation(t *testing.T) {
	valid := TicketOperationIdentity{
		ExpectedPacketID: "packet-1", OperationID: string(registry.PlannerTicketFrontierOperationID),
		Action: string(registry.TicketActionReadFrontier), WorkspaceID: "workspace-1",
	}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTicketOperationIdentity(raw)
	if err != nil || decoded.Action != string(registry.TicketActionReadFrontier) {
		t.Fatalf("decoded = %#v, %v", decoded, err)
	}
	if _, err := DecodeTicketOperationIdentity(append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...)); err == nil {
		t.Fatal("unknown field was accepted")
	}
	valid.MutationID = "mutation-1"
	if _, err := valid.SemanticRequestSHA256(); err == nil {
		t.Fatal("frontier read with mutation id was accepted")
	}
	valid.MutationID = ""
	valid.OperationID = string(registry.LocalOperatorTicketWorkflowOperationID)
	if _, err := valid.SemanticRequestSHA256(); err == nil {
		t.Fatal("planner action on local-operator operation was accepted")
	}
}

func TestTicketPacketAdmitterForwardsExactRegisteredRoute(t *testing.T) {
	packet := &fakeMCPTicketPacketAuthorizer{}
	service, err := appoperations.NewTicketAdmissionService(packet)
	if err != nil {
		t.Fatal(err)
	}
	admitter, err := NewTicketPacketAdmitter(service)
	if err != nil {
		t.Fatal(err)
	}
	identity := TicketOperationIdentity{
		MutationID: "mutation-1", ExpectedPacketID: "packet-1", OperationID: string(registry.LocalOperatorTicketWorkflowOperationID),
		Action: string(registry.TicketActionUpdatePriority), WorkspaceID: "workspace-1", TicketID: "TICKET-1", ExternalPriority: 8,
		PayloadSHA256: strings.Repeat("a", 64), RequiredDependencies: []TicketPacketDependency{{Class: "workflow_snapshot", Key: "ticket:TICKET-1"}},
	}
	authorization, fingerprint, err := admitter.Admit(context.Background(), identity)
	if err != nil || !authorization.Allowed || fingerprint == "" {
		t.Fatalf("admission = %#v, %q, %v", authorization, fingerprint, err)
	}
	if packet.request.SurfaceContract != registry.LocalOperatorTicketWorkflowSurface || packet.request.OperationID != registry.LocalOperatorTicketWorkflowOperationID || packet.request.Action != registry.TicketActionUpdatePriority {
		t.Fatalf("packet request = %#v", packet.request)
	}
}

func TestTicketRoleSurfacesExcludePackagesAndRuns(t *testing.T) {
	surfaces := TicketRoleSurfaces()
	if len(surfaces) != 2 || surfaces[0].Role != "planner" || surfaces[1].Role != "local_operator" {
		t.Fatalf("surfaces = %#v", surfaces)
	}
	for _, surface := range surfaces {
		if surface.SurfaceContract == "" || surface.ManifestSHA256 == "" || len(surface.Operations) != 1 {
			t.Fatalf("invalid surface = %#v", surface)
		}
	}
}
