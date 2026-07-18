package tickets

import (
	"context"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestListFrontierOrdersReadyTicketsAndExplainsAdjacentTies(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}

	for _, fixture := range []struct {
		ticketID  string
		priority  int64
		createdAt string
	}{
		{ticketID: "P4-A", priority: 50, createdAt: "2026-07-18T00:00:00.000000000Z"},
		{ticketID: "P4-B", priority: 50, createdAt: "2026-07-18T00:00:01.000000000Z"},
		{ticketID: "P4-C", priority: 50, createdAt: "2026-07-18T00:00:01.000000000Z"},
		{ticketID: "P4-D", priority: 49, createdAt: "2026-07-18T00:00:00.000000000Z"},
	} {
		insertTicketAt(t, ctx, store, workspaceID, fixture.ticketID, fixture.priority, fixture.createdAt)
		publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, fixture.ticketID, fixture.priority, 0, fixture.ticketID)
	}

	frontier, err := service.ListFrontier(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if frontier.WorkspaceID != workspaceID || len(frontier.Entries) != 4 {
		t.Fatalf("frontier = %#v", frontier)
	}
	for index, want := range []string{"P4-A", "P4-B", "P4-C", "P4-D"} {
		if got := frontier.Entries[index].TicketID; got != want {
			t.Fatalf("frontier entry %d = %q, want %q", index, got, want)
		}
	}
	if frontier.Entries[0].TieWithPrevious != nil || frontier.Entries[3].TieWithPrevious != nil {
		t.Fatalf("unexpected non-tie reason = %#v", frontier.Entries)
	}
	if tie := frontier.Entries[1].TieWithPrevious; tie == nil || tie.PreviousTicketID != "P4-A" || tie.Rule != FrontierTieRuleEarlierCreation {
		t.Fatalf("creation-time tie = %#v", tie)
	}
	if tie := frontier.Entries[2].TieWithPrevious; tie == nil || tie.PreviousTicketID != "P4-B" || tie.Rule != FrontierTieRuleStableTicketID {
		t.Fatalf("ticket-ID tie = %#v", tie)
	}
}

func insertTicketAt(
	t *testing.T,
	ctx context.Context,
	store *workflowstore.Store,
	workspaceID, ticketID string,
	priority int64,
	createdAt string,
) {
	t.Helper()
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)`, ticketID, workspace.ID, priority, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
}

func publishApprovedTicket(
	t *testing.T,
	ctx context.Context,
	service *Service,
	workspaceID string,
	closure workflowstore.SourceVaultClosure,
	authorityID, ticketID string,
	priority, expectedRevision int64,
	name string,
) PublishedRevision {
	t.Helper()
	published, err := service.Publish(ctx, publishInput(workspaceID, ticketID, priority, expectedRevision, closure, name, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{
		TicketID:            ticketID,
		RevisionRowID:       published.Revision.ID,
		AuthorityRevisionID: authorityID,
		Rationale:           "approved for frontier and selection tests",
	}); err != nil {
		t.Fatal(err)
	}
	return published
}
