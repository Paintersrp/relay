package operations

import (
	"context"
	"errors"
	"testing"

	"relay/internal/app/tickets"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type fakeTicketPacketAuthorizer struct {
	request MutationRequest
	err     error
}

func (f *fakeTicketPacketAuthorizer) AuthorizeMutation(_ context.Context, request MutationRequest) (MutationAuthorization, error) {
	f.request = request
	return MutationAuthorization{Allowed: true}, f.err
}

type fakeTicketWorkflowOwner struct {
	calls      []string
	readDetail tickets.TicketDetail
}

func (f *fakeTicketWorkflowOwner) Publish(_ context.Context, _ tickets.PublishInput) (tickets.PublishedRevision, error) {
	f.calls = append(f.calls, "publish")
	return tickets.PublishedRevision{}, nil
}

func (f *fakeTicketWorkflowOwner) UpdateExternalPriority(_ context.Context, _ string, _ int64) (workflowstore.DeliveryTicket, error) {
	f.calls = append(f.calls, "priority")
	return workflowstore.DeliveryTicket{}, nil
}

func (f *fakeTicketWorkflowOwner) Approve(_ context.Context, _ tickets.ApproveInput) (workflowstore.DeliveryTicketRevisionApproval, error) {
	f.calls = append(f.calls, "approve")
	return workflowstore.DeliveryTicketRevisionApproval{}, nil
}

func (f *fakeTicketWorkflowOwner) Read(_ context.Context, _ string) (tickets.TicketDetail, error) {
	f.calls = append(f.calls, "read")
	return f.readDetail, nil
}

func (f *fakeTicketWorkflowOwner) ListFrontier(_ context.Context, workspaceID string) (tickets.Frontier, error) {
	f.calls = append(f.calls, "frontier:"+workspaceID)
	return tickets.Frontier{WorkspaceID: workspaceID}, nil
}

func (f *fakeTicketWorkflowOwner) Select(_ context.Context, _ tickets.SelectInput) (tickets.SelectionResult, error) {
	f.calls = append(f.calls, "select")
	return tickets.SelectionResult{}, nil
}

func TestTicketWorkflowAdmitsBeforeCallingSharedOwner(t *testing.T) {
	packet := &fakeTicketPacketAuthorizer{}
	owner := &fakeTicketWorkflowOwner{}
	service, err := NewTicketWorkflowService(packet, owner)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := TicketPriorityPayloadSHA256("TICKET-1", 70)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdatePriority(context.Background(), TicketOperationRequest{
		PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionUpdatePriority,
		WorkspaceID: "workspace-1", TicketID: "TICKET-1", ExternalPriority: 70, PayloadSHA256: payload,
		RequiredDependencies: []DependencyRequirement{{Class: "workflow_snapshot", Key: "ticket:TICKET-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.request.SurfaceContract != registry.LocalOperatorTicketWorkflowSurface || packet.request.OperationID != registry.LocalOperatorTicketWorkflowOperationID || packet.request.Action != registry.TicketActionUpdatePriority {
		t.Fatalf("packet request = %#v", packet.request)
	}
	if len(owner.calls) != 1 || owner.calls[0] != "priority" {
		t.Fatalf("owner calls = %#v", owner.calls)
	}
}

func TestTicketWorkflowFailsClosedBeforeCallingSharedOwner(t *testing.T) {
	packet := &fakeTicketPacketAuthorizer{err: errors.New("stale packet")}
	owner := &fakeTicketWorkflowOwner{}
	service, err := NewTicketWorkflowService(packet, owner)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := TicketPriorityPayloadSHA256("TICKET-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdatePriority(context.Background(), TicketOperationRequest{
		PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionUpdatePriority,
		WorkspaceID: "workspace-1", TicketID: "TICKET-1", ExternalPriority: 1, PayloadSHA256: payload,
	})
	if err == nil {
		t.Fatal("stale packet was admitted")
	}
	if len(owner.calls) != 0 {
		t.Fatalf("owner was called after failed admission: %#v", owner.calls)
	}
}

func TestTicketWorkflowReadUsesPlannerSurfaceWithoutMutation(t *testing.T) {
	packet := &fakeTicketPacketAuthorizer{}
	owner := &fakeTicketWorkflowOwner{}
	service, err := NewTicketWorkflowService(packet, owner)
	if err != nil {
		t.Fatal(err)
	}
	frontier, err := service.ListFrontier(context.Background(), TicketOperationRequest{
		PacketID: "packet-1", OperationID: registry.PlannerTicketFrontierOperationID, Action: registry.TicketActionReadFrontier,
		WorkspaceID: "workspace-1",
	})
	if err != nil || frontier.WorkspaceID != "workspace-1" {
		t.Fatalf("frontier = %#v, %v", frontier, err)
	}
	if packet.request.SurfaceContract != registry.PlannerTicketFrontierSurface || packet.request.Action != registry.TicketActionReadFrontier {
		t.Fatalf("packet request = %#v", packet.request)
	}
	if len(owner.calls) != 1 || owner.calls[0] != "frontier:workspace-1" {
		t.Fatalf("owner calls = %#v", owner.calls)
	}
}

func TestTicketWorkflowApprovalBindsCurrentRevisionAndSource(t *testing.T) {
	packet := &fakeTicketPacketAuthorizer{}
	owner := &fakeTicketWorkflowOwner{readDetail: tickets.TicketDetail{Revision: workflowstore.DeliveryTicketRevision{ID: 9, SourceClosureRowID: 12}}}
	service, err := NewTicketWorkflowService(packet, owner)
	if err != nil {
		t.Fatal(err)
	}
	approve := tickets.ApproveInput{TicketID: "TICKET-1", RevisionRowID: 9, AuthorityRevisionID: "authority-1", Rationale: "approved"}
	payload, err := TicketApprovalPayloadSHA256(approve)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Approve(context.Background(), TicketApprovalOperationInput{Admission: TicketOperationRequest{
		PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionApprove,
		WorkspaceID: "workspace-1", TicketID: "TICKET-1", RevisionRowID: 9, AuthorityRevisionID: "authority-1", SourceClosureRowID: 12,
		PayloadSHA256: payload,
	}, Approve: approve})
	if err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 2 || owner.calls[0] != "read" || owner.calls[1] != "approve" {
		t.Fatalf("owner calls = %#v", owner.calls)
	}

	owner.calls = nil
	owner.readDetail.Revision.SourceClosureRowID = 13
	_, err = service.Approve(context.Background(), TicketApprovalOperationInput{Admission: TicketOperationRequest{
		PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionApprove,
		WorkspaceID: "workspace-1", TicketID: "TICKET-1", RevisionRowID: 9, AuthorityRevisionID: "authority-1", SourceClosureRowID: 12,
		PayloadSHA256: payload,
	}, Approve: approve})
	if !errors.Is(err, ErrTicketAdmission) {
		t.Fatalf("stale source error = %v", err)
	}
	if len(owner.calls) != 1 || owner.calls[0] != "read" {
		t.Fatalf("approval reached owner after source drift: %#v", owner.calls)
	}
}

func TestTicketWorkflowDependencyReplacementPublishesWholeRevision(t *testing.T) {
	packet := &fakeTicketPacketAuthorizer{}
	owner := &fakeTicketWorkflowOwner{}
	service, err := NewTicketWorkflowService(packet, owner)
	if err != nil {
		t.Fatal(err)
	}
	publish := tickets.PublishInput{
		WorkspaceID: "workspace-1", TicketID: "TICKET-1", ExternalPriority: 3, ExpectedRevisionNumber: 1,
		Revision: tickets.RevisionInput{SourceClosureRowID: 12},
	}
	payload, err := TicketPublishPayloadSHA256(publish)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ReplaceDependencies(context.Background(), TicketPublishOperationInput{Admission: TicketOperationRequest{
		PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionReplaceDependencies,
		WorkspaceID: "workspace-1", TicketID: "TICKET-1", ExpectedRevisionNumber: 1, SourceClosureRowID: 12, ExternalPriority: 3,
		PayloadSHA256: payload,
	}, Publish: publish})
	if err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 1 || owner.calls[0] != "publish" {
		t.Fatalf("dependency replacement did not use publication owner: %#v", owner.calls)
	}
}
