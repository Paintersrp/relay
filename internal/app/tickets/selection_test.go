package tickets

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestSelectRecordsCompatibleBundleWithoutMutatingTickets(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	first := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 50, 0, "first")
	second := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-B", 40, 0, "second")

	result, err := service.Select(ctx, SelectInput{
		WorkspaceID: workspaceID,
		Members: []SelectionMemberInput{
			{TicketID: second.Ticket.TicketID, RevisionRowID: second.Revision.ID},
			{TicketID: first.Ticket.TicketID, RevisionRowID: first.Revision.ID},
		},
		Rationale: "The two current tickets form the smallest cohesive delivery bound.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection.State != "active" || result.Selection.Rationale != "The two current tickets form the smallest cohesive delivery bound." ||
		!result.Selection.SourceClosureRowID.Valid || result.Selection.SourceClosureRowID.Int64 != closure.ID {
		t.Fatalf("selection = %#v", result.Selection)
	}
	if len(result.Members) != 2 || result.Members[0].TicketID != first.Ticket.TicketID || result.Members[1].TicketID != second.Ticket.TicketID ||
		result.Members[0].RevisionRowID != first.Revision.ID || result.Members[1].RevisionRowID != second.Revision.ID {
		t.Fatalf("selection members = %#v", result.Members)
	}

	for _, published := range []PublishedRevision{first, second} {
		detail, err := service.Read(ctx, published.Ticket.TicketID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Canonical.SHA256 != published.Canonical.SHA256 || detail.Ticket.ExternalPriority != published.Ticket.ExternalPriority || !detail.Readiness.Selected {
			t.Fatalf("ticket changed by selection = %#v", detail)
		}
	}
	frontier, err := service.ListFrontier(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Entries) != 0 {
		t.Fatalf("selected tickets remained on frontier = %#v", frontier.Entries)
	}
	var runCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 {
		t.Fatalf("selection created %d Runs", runCount)
	}
}

func TestSelectRollsBackStaleRevisionAuthorityAndSource(t *testing.T) {
	t.Run("replacement revision", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, authorityID := ticketFixture(t)
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		original := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 50, 0, "original")
		if _, err := service.Publish(ctx, publishInput(workspaceID, original.Ticket.TicketID, 50, 1, closure, "replacement", "")); err != nil {
			t.Fatal(err)
		}
		_, err = service.Select(ctx, SelectInput{WorkspaceID: workspaceID, Members: []SelectionMemberInput{{TicketID: original.Ticket.TicketID, RevisionRowID: original.Revision.ID}}, Rationale: "must reject the replaced revision"})
		if !errors.Is(err, ErrSelectionMemberStale) {
			t.Fatalf("selection error = %v", err)
		}
		assertNoDeliveryTicketSelection(t, ctx, store)
	})

	t.Run("authority", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, authorityID := ticketFixture(t)
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 50, 0, "authority")
		setCurrentAuthority(t, ctx, store, workspaceID, closure.ID, "authority-ticket-2")
		_, err = service.Select(ctx, SelectInput{WorkspaceID: workspaceID, Members: []SelectionMemberInput{{TicketID: published.Ticket.TicketID, RevisionRowID: published.Revision.ID}}, Rationale: "must reject stale authority"})
		if !errors.Is(err, ErrSelectionAuthorityStale) {
			t.Fatalf("selection error = %v", err)
		}
		assertNoDeliveryTicketSelection(t, ctx, store)
	})

	t.Run("dependency", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, authorityID := ticketFixture(t)
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		dependency := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 50, 0, "dependency")
		dependentInput := publishInput(workspaceID, "P4-B", 40, 0, closure, "dependent", "")
		dependentInput.Revision.Dependencies = []DependencyInput{{RevisionRowID: dependency.Revision.ID, Outcome: "satisfied"}}
		dependent, err := service.Publish(ctx, dependentInput)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Approve(ctx, ApproveInput{TicketID: dependent.Ticket.TicketID, RevisionRowID: dependent.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "dependent approved"}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Publish(ctx, publishInput(workspaceID, dependency.Ticket.TicketID, 50, 1, closure, "dependency replacement", "")); err != nil {
			t.Fatal(err)
		}
		_, err = service.Select(ctx, SelectInput{WorkspaceID: workspaceID, Members: []SelectionMemberInput{{TicketID: dependent.Ticket.TicketID, RevisionRowID: dependent.Revision.ID}}, Rationale: "must reject stale dependency"})
		if !errors.Is(err, ErrSelectionDependenciesInvalid) {
			t.Fatalf("selection error = %v", err)
		}
		assertNoDeliveryTicketSelection(t, ctx, store)
	})

	t.Run("source", func(t *testing.T) {
		ctx := context.Background()
		store, workspaceID, closure, authorityID := ticketFixture(t)
		service, err := NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 50, 0, "source")
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, err := tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
				ClosureID: closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateReady,
				NextState:     workflowstore.SourceVaultClosureStateUnavailable,
				FailureReason: sql.NullString{String: workflowstore.SourceVaultFailureVaultMissing, Valid: true},
				TransitionAt:  "2026-07-18T00:00:02.000000000Z",
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		_, err = service.Select(ctx, SelectInput{WorkspaceID: workspaceID, Members: []SelectionMemberInput{{TicketID: published.Ticket.TicketID, RevisionRowID: published.Revision.ID}}, Rationale: "must reject stale source"})
		if !errors.Is(err, ErrSelectionSourceStale) {
			t.Fatalf("selection error = %v", err)
		}
		assertNoDeliveryTicketSelection(t, ctx, store)
	})
}

func TestSelectAllowsOneConcurrentWinner(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 50, 0, "race")
	input := SelectInput{
		WorkspaceID: workspaceID,
		Members:     []SelectionMemberInput{{TicketID: published.Ticket.TicketID, RevisionRowID: published.Revision.ID}},
		Rationale:   "select one current ticket exactly once",
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := service.Select(ctx, input)
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if errors.Is(err, ErrSelectionConflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent selection error: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results successes=%d conflicts=%d", successes, conflicts)
	}
	var selectionCount, memberCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_ticket_selections WHERE state = 'active'`).Scan(&selectionCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_ticket_selection_members`).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if selectionCount != 1 || memberCount != 1 {
		t.Fatalf("concurrent reservation persisted selections=%d members=%d", selectionCount, memberCount)
	}
}

func TestValidateSelectionBundleRequiresOneRepositoryBranchAndSourceBasis(t *testing.T) {
	base := selectionCandidate{revision: workflowstore.DeliveryTicketRevision{
		RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40), SourceClosureRowID: 1,
	}}
	for _, other := range []workflowstore.DeliveryTicketRevision{
		{RepoTarget: "relay-specs", Branch: "main", BaseCommit: strings.Repeat("a", 40), SourceClosureRowID: 1},
		{RepoTarget: "relay", Branch: "release", BaseCommit: strings.Repeat("a", 40), SourceClosureRowID: 1},
		{RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40), SourceClosureRowID: 2},
		{RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("b", 40), SourceClosureRowID: 1},
	} {
		if err := validateSelectionBundle([]selectionCandidate{base, {revision: other}}); !errors.Is(err, ErrIncompatibleSelection) {
			t.Fatalf("incompatible bundle error = %v", err)
		}
	}
	if err := validateSelectionBundle([]selectionCandidate{base, {revision: base.revision}}); err != nil {
		t.Fatalf("compatible bundle error = %v", err)
	}
}

func assertNoDeliveryTicketSelection(t *testing.T, ctx context.Context, store *workflowstore.Store) {
	t.Helper()
	var selections, members int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_ticket_selections`).Scan(&selections); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_ticket_selection_members`).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if selections != 0 || members != 0 {
		t.Fatalf("failed selection persisted selections=%d members=%d", selections, members)
	}
}
