package approvals

import (
	"context"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

type approvalTestIDs struct{}

func (approvalTestIDs) ApprovalID() string { return "approval-test-produced" }

func TestApproveDeliveryTicketRevisionInTxCreatesApprovalWithoutAdvancingCurrentness(t *testing.T) {
	ctx := context.Background()
	store := workflowfixture.Open(t, workflowstore.Open)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-approval-hook', 'Approval Hook')`); err != nil {
		t.Fatal(err)
	}
	var projectID, workspaceID, vaultID, closureID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM projects WHERE project_id = 'project-approval-hook'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-approval-hook', ?, 'approval-hook') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('approval-hook-repo', 'C:/approval-hook-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-approval-hook', 'approval-hook-repo', 'vaults/approval-hook') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-approval-hook', ?, ?, ?, 1, 'refs/relay/closures/approval-hook', 'ready', '2026-08-01T00:00:00.000000000Z', '2026-08-01T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("a", 40), strings.Repeat("b", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithIDs(store, approvalTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	var ticket workflowstore.DeliveryTicket
	var revision workflowstore.DeliveryTicketRevision
	var approval workflowstore.DeliveryTicketRevisionApproval
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "P3-APPROVAL", WorkspaceRowID: workspaceID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		revision, err = tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "approval-hook-repo", Branch: "main",
			BaseCommit: strings.Repeat("a", 40), SourceClosureRowID: closureID, SourcePath: "ticket.json",
			Goal: "Test approval ownership.", Context: "Approval is transaction-aware.", TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		approval, err = service.ApproveDeliveryTicketRevisionInTx(ctx, tx, DeliveryTicketRevisionApprovalInput{
			Ticket: ticket, Revision: revision, Rationale: "produced revision approval", RequireCurrentRevision: false, RequireAuthority: false,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if approval.ApprovalID != "approval-test-produced" || approval.RevisionRowID != revision.ID || approval.ApprovalState != "approved" || approval.AuthorityRevisionRowID.Valid {
		t.Fatalf("transaction-aware approval = %#v", approval)
	}
	current, err := store.GetDeliveryTicketByTicketID(ctx, ticket.TicketID)
	if err != nil || current.CurrentRevisionRowID.Valid {
		t.Fatalf("approval hook advanced currentness = %#v, %v", current, err)
	}
	approvals, err := store.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil || len(approvals) != 1 || approvals[0].ID != approval.ID {
		t.Fatalf("persisted approval = %#v, %v", approvals, err)
	}
}
