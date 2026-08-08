package tickets

import (
	"context"
	"testing"
)

func TestReadWorkspaceSelectionTracksNoneActiveAndConsumed(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	first := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-S1", 50, 0, "first")

	none, err := service.ReadWorkspaceSelection(ctx, workspaceID)
	if err != nil || none.State != "none" || none.TicketID != "" {
		t.Fatalf("selection before select = %+v, %v", none, err)
	}

	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: first.Ticket.TicketID, RevisionRowID: first.Revision.ID, Rationale: "Select for guided projection."}); err != nil {
		t.Fatal(err)
	}
	active, err := service.ReadWorkspaceSelection(ctx, workspaceID)
	if err != nil || active.State != "active" || active.TicketID != "P4-S1" || active.RevisionNumber != 1 {
		t.Fatalf("selection after select = %+v, %v", active, err)
	}
}

func TestReadWorkspaceSelectionRejectsUnknownWorkspace(t *testing.T) {
	store, _, _, _ := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadWorkspaceSelection(context.Background(), "workspace-unknown"); err == nil {
		t.Fatal("unknown workspace selection read succeeded")
	}
}
