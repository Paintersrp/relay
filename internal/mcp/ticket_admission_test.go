package mcp

import (
	"context"
	"encoding/json"
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

func TestTicketFrontierIdentityAcceptsOnlyPlannerFrontierRead(t *testing.T) {
	identity := TicketFrontierOperationIdentity{
		ExpectedPacketID: "packet-1", OperationID: string(registry.PlannerTicketFrontierOperationID),
		Action: string(registry.TicketActionReadFrontier), FeatureSlug: "checkout", RequestedUnitID: "P4-T1",
	}
	if err := identity.Validate(); err != nil {
		t.Fatalf("valid frontier identity rejected: %v", err)
	}
	sha, err := identity.SemanticRequestSHA256()
	if err != nil || sha == "" {
		t.Fatalf("frontier SHA256 = %q, %v", sha, err)
	}
	identity.Action = string(registry.TicketActionPublish)
	if err := identity.Validate(); err == nil {
		t.Fatal("mutation action on frontier identity was accepted")
	}
}

func TestTicketFrontierIdentityStrictDecode(t *testing.T) {
	raw := json.RawMessage(`{"expected_packet_id":"packet-1","operation_id":"planner.ticket_frontier","action":"read_ticket_frontier","feature_slug":"checkout","requested_unit_id":"P4-T1"}`)
	identity, err := DecodeTicketFrontierOperationIdentity(raw)
	if err != nil {
		t.Fatal(err)
	}
	if identity.FeatureSlug != "checkout" || identity.RequestedUnitID != "P4-T1" {
		t.Fatalf("frontier identity = %#v", identity)
	}
	if _, err := DecodeTicketFrontierOperationIdentity(json.RawMessage(`{"expected_packet_id":"packet-1","operation_id":"planner.ticket_frontier","action":"read_ticket_frontier","ticket_id":"ticket-1"}`)); err == nil {
		t.Fatal("legacy ticket_id field was accepted")
	}
	if _, err := DecodeTicketFrontierOperationIdentity(json.RawMessage(`{"expected_packet_id":"packet-1","operation_id":"planner.ticket_frontier","action":"read_ticket_frontier","feature_slug":"Checkout"}`)); err == nil {
		t.Fatal("invalid feature_slug was accepted")
	}
	if _, err := DecodeTicketFrontierOperationIdentity(json.RawMessage(`{"expected_packet_id":"packet-1","operation_id":"planner.ticket_frontier","action":"read_ticket_frontier","feature_slug":"checkout","requested_unit_id":"ticket-1"}`)); err == nil {
		t.Fatal("invalid requested_unit_id was accepted")
	}
	if _, err := DecodeTicketFrontierOperationIdentity(json.RawMessage(`{"expected_packet_id":"packet-1","operation_id":"planner.ticket_frontier","action":"read_ticket_frontier","feature_slug":"checkout","unknown":true}`)); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestTicketFrontierIdentityAcceptsAbsentFilter(t *testing.T) {
	raw := json.RawMessage(`{"expected_packet_id":"packet-1","operation_id":"planner.ticket_frontier","action":"read_ticket_frontier","feature_slug":"checkout","requested_unit_id":null}`)
	identity, err := DecodeTicketFrontierOperationIdentity(raw)
	if err != nil {
		t.Fatal(err)
	}
	if identity.RequestedUnitID != "" {
		t.Fatalf("null requested_unit_id = %q", identity.RequestedUnitID)
	}
}

func TestTicketFrontierAdmitterForwardsExactPlannerRoute(t *testing.T) {
	packet := &fakeMCPTicketPacketAuthorizer{}
	service, err := appoperations.NewTicketFrontierAdmissionService(packet)
	if err != nil {
		t.Fatal(err)
	}
	admitter, err := NewTicketFrontierAdmitter(service)
	if err != nil {
		t.Fatal(err)
	}
	identity := TicketFrontierOperationIdentity{ExpectedPacketID: "packet-1", OperationID: string(registry.PlannerTicketFrontierOperationID), Action: string(registry.TicketActionReadFrontier), FeatureSlug: "checkout", RequestedUnitID: "P4-T1"}
	authorization, fingerprint, err := admitter.Admit(context.Background(), identity)
	if err != nil || !authorization.Allowed || fingerprint == "" {
		t.Fatalf("admission = %#v, %q, %v", authorization, fingerprint, err)
	}
	if packet.request.SurfaceContract != registry.PlannerTicketFrontierSurface || packet.request.OperationID != registry.PlannerTicketFrontierOperationID || packet.request.Action != registry.TicketActionReadFrontier {
		t.Fatalf("packet request = %#v", packet.request)
	}
}

func TestTicketRoleSurfacesExposeOnlyPlannerFrontier(t *testing.T) {
	surfaces := TicketRoleSurfaces()
	if len(surfaces) != 1 || surfaces[0].Role != registry.Role("planner") || len(surfaces[0].Operations) != 1 || surfaces[0].Operations[0] != registry.PlannerTicketFrontierOperationID {
		t.Fatalf("surfaces = %#v", surfaces)
	}
}
