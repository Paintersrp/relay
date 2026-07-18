package workflowstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	relaydb "relay/internal/db"
	workflowgenerated "relay/internal/store/workflowgenerated"

	"github.com/pressly/goose/v3"
)

func TestDeliveryTicketRevisionsSelectionsAndGuards(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	_, closure := seedReadySourceVaultClosure(t, ctx, store)
	workspace := seedDeliveryTicketWorkspace(t, ctx, store)

	var ticket DeliveryTicket
	var firstRevision DeliveryTicketRevision
	var approval DeliveryTicketRevisionApproval
	var selection DeliveryTicketSelection
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		ticket, err = tx.CreateDeliveryTicket(ctx, CreateDeliveryTicketParams{
			TicketID: "P4-T1", WorkspaceRowID: workspace.ID, ExternalPriority: 70,
		})
		if err != nil {
			return err
		}
		firstRevision, err = tx.CreateDeliveryTicketRevision(ctx, CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "relay", Branch: "main",
			BaseCommit: closure.CommitOID, SourceClosureRowID: closure.ID, SourcePath: "tickets/p4-t1.delivery-ticket.json",
			Goal: "Persist the delivery ticket.", Context: "Ticket source is retained.", TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		if _, err := tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, firstRevision.ID); err != nil {
			return err
		}
		if _, err := tx.CreateDeliveryTicketRevisionMember(ctx, CreateDeliveryTicketRevisionMemberParams{
			RevisionRowID: firstRevision.ID, Sequence: 1, MemberKind: "implementation_obligation",
			MemberPath: sql.NullString{String: "internal/store/workflow", Valid: true}, MemberText: "Persist ordered ticket members.",
		}); err != nil {
			return err
		}
		approval, err = tx.CreateDeliveryTicketRevisionApproval(ctx, CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID: NewDeliveryTicketApprovalID(), RevisionRowID: firstRevision.ID, ApprovalKind: "delivery",
			ApprovalState: "approved", Rationale: "Exact revision approved.", SourceClosureRowID: closure.ID,
		})
		if err != nil {
			return err
		}
		selection, err = tx.CreateDeliveryTicketSelection(ctx, CreateDeliveryTicketSelectionParams{
			SelectionID: NewDeliveryTicketSelectionID(), WorkspaceRowID: workspace.ID, State: "active",
			Rationale: "Prioritize the approved ticket.", SourceClosureRowID: sql.NullInt64{Int64: closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelectionMember(ctx, CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: firstRevision.ID, ApprovalRowID: approval.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DB().Exec(`UPDATE delivery_ticket_revisions SET goal = 'mutated' WHERE id = ?`, firstRevision.ID); err == nil {
		t.Fatal("delivery ticket revision was mutable")
	}
	if _, err := store.DB().Exec(`UPDATE delivery_ticket_selection_members SET sequence = 2 WHERE selection_row_id = ?`, selection.ID); err == nil {
		t.Fatal("delivery ticket selection member was mutable")
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		updated, err := tx.UpdateDeliveryTicketExternalPriority(ctx, ticket.TicketID, 90)
		if err != nil {
			return err
		}
		if updated.ExternalPriority != 90 || updated.CurrentRevisionRowID.Int64 != firstRevision.ID {
			t.Fatalf("priority metadata update = %#v", updated)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		rolledBack, err := tx.CreateDeliveryTicketSelection(ctx, CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-rolled-back", WorkspaceRowID: workspace.ID, State: "superseded",
			Rationale: "This selection must roll back.",
		})
		if err != nil {
			return err
		}
		if _, err := tx.CreateDeliveryTicketSelectionMember(ctx, CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: rolledBack.ID, Sequence: 1, RevisionRowID: firstRevision.ID, ApprovalRowID: approval.ID,
		}); err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelection(ctx, CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-conflict", WorkspaceRowID: workspace.ID, State: "active",
			Rationale: "A second active selection must fail.",
		})
		return err
	}); err == nil {
		t.Fatal("second active selection succeeded")
	}
	var rolledBackCount, rolledBackMemberCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM delivery_ticket_selections WHERE selection_id = 'selection-rolled-back'`).Scan(&rolledBackCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM delivery_ticket_selection_members WHERE selection_row_id NOT IN (?)`, selection.ID).Scan(&rolledBackMemberCount); err != nil {
		t.Fatal(err)
	}
	if rolledBackCount != 0 || rolledBackMemberCount != 0 {
		t.Fatalf("failed selection transaction persisted selection=%d members=%d", rolledBackCount, rolledBackMemberCount)
	}
}

func TestDeliveryTicketMigrationUpgradesAndRollsBack(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "workflow.sqlite")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	goose.SetBaseFS(relaydb.WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, "workflow_migrations", 11); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO projects (project_id, name) VALUES ('project-delivery-ticket-upgrade', 'Upgrade')`); err != nil {
		t.Fatal(err)
	}
	if err := relaydb.AutoMigrateWorkflow(database); err != nil {
		t.Fatal(err)
	}
	var projects int
	if err := database.QueryRow(`SELECT COUNT(*) FROM projects WHERE project_id = 'project-delivery-ticket-upgrade'`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if projects != 1 {
		t.Fatalf("upgraded project rows = %d, want 1", projects)
	}
	if err := goose.DownTo(database, "workflow_migrations", 11); err != nil {
		t.Fatal(err)
	}
	var tables int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'delivery_tickets'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("delivery ticket table survived rollback")
	}
}

func seedDeliveryTicketWorkspace(t *testing.T, ctx context.Context, store *Store) FeatureWorkspace {
	t.Helper()
	var projectID int64
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO projects (project_id, name)
VALUES ('project-delivery-ticket', 'Delivery Ticket')
RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	workspace, err := workflowgenerated.New(store.DB()).CreateFeatureWorkspace(ctx, workflowgenerated.CreateFeatureWorkspaceParams{
		WorkspaceID: "workspace-delivery-ticket", ProjectRowID: projectID, FeatureSlug: "delivery-ticket",
	})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}
