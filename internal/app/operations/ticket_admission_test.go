package operations

import (
	"context"
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
func (f *fakeTicketWorkflowOwner) AdmitTicketDesignBrief(_ context.Context, _ tickets.TicketDesignBriefAdmissionInput) (tickets.TicketDesignBriefAdmissionResult, error) {
	f.calls = append(f.calls, "admit-brief")
	return tickets.TicketDesignBriefAdmissionResult{}, nil
}
func (f *fakeTicketWorkflowOwner) CompleteTicketDesignBriefReview(_ context.Context, _ tickets.CompleteBriefReviewInput) (tickets.TicketDesignBriefReviewResult, error) {
	f.calls = append(f.calls, "complete-brief-review")
	return tickets.TicketDesignBriefReviewResult{}, nil
}
func (f *fakeTicketWorkflowOwner) ApproveTicketDesignBrief(_ context.Context, _ tickets.TicketDesignBriefApprovalInput) (tickets.TicketDesignBriefApprovalResult, error) {
	f.calls = append(f.calls, "approve-brief")
	return tickets.TicketDesignBriefApprovalResult{}, nil
}

func TestTicketWorkflowMutationsDelegateDirectly(t *testing.T) {
	owner := &fakeTicketWorkflowOwner{}
	service, err := NewTicketWorkflowService(nil, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdatePriority(context.Background(), "TICKET-1", 70); err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 1 || owner.calls[0] != "priority" {
		t.Fatalf("owner calls = %#v", owner.calls)
	}
	if _, err := service.ListFrontier(context.Background(), "workspace-1"); err != nil {
		t.Fatal(err)
	}
	if owner.calls[1] != "frontier:workspace-1" {
		t.Fatalf("owner calls = %#v", owner.calls)
	}
}

func TestTicketWorkflowDistinctBriefApprovalDelegatesToOwner(t *testing.T) {
	owner := &fakeTicketWorkflowOwner{}
	service, err := NewTicketWorkflowService(nil, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveTicketDesignBrief(context.Background(), tickets.TicketDesignBriefApprovalInput{
		WorkspaceID: "workspace-1", ExpectedVersion: 8,
		OperatorConfirmationEvidence: "distinct approval", CreatedIdentity: "operator",
	}); err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 1 || owner.calls[0] != "approve-brief" {
		t.Fatalf("owner calls = %#v", owner.calls)
	}
	if _, err := service.ApproveTicketDesignBrief(context.Background(), tickets.TicketDesignBriefApprovalInput{}); err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 2 || owner.calls[1] != "approve-brief" {
		t.Fatalf("owner calls = %#v", owner.calls)
	}
}

func TestTicketFrontierReadRequiresOnlyPlannerPacketAdmission(t *testing.T) {
	packet := &fakeTicketPacketAuthorizer{}
	service, err := NewTicketFrontierAdmissionService(packet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Admit(context.Background(), TicketFrontierReadRequest{PacketID: "packet-1", TicketID: "ticket-1"}); err != nil {
		t.Fatal(err)
	}
	if packet.request.SurfaceContract != registry.PlannerTicketFrontierSurface ||
		packet.request.OperationID != registry.PlannerTicketFrontierOperationID ||
		packet.request.Action != registry.TicketActionReadFrontier || len(packet.request.RequiredDependencies) != 0 {
		t.Fatalf("packet request = %#v", packet.request)
	}
}
