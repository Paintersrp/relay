package tickets

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/approvals"
	workflowstore "relay/internal/store/workflow"
)

func TestPublishApprovalPriorityReplacementAndCancellation(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.Publish(ctx, publishInput(workspaceID, "P4-T2", 69, 0, closure, "first", ""))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.Approve(ctx, ApproveInput{TicketID: first.Ticket.TicketID, RevisionRowID: first.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "approved against current authority"})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.Read(ctx, first.Ticket.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Readiness.Ready || detail.Readiness.Selected {
		t.Fatalf("readiness before selection = %#v", detail.Readiness)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		selection, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{SelectionID: "selection-p4-t2", WorkspaceRowID: first.Ticket.WorkspaceRowID, State: "active", Rationale: "select current ready ticket"})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: first.Revision.ID, ApprovalRowID: approval.ID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateExternalPriority(ctx, first.Ticket.TicketID, 99); err != nil {
		t.Fatal(err)
	}
	detail, err = service.Read(ctx, first.Ticket.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Canonical.SHA256 != first.Canonical.SHA256 || detail.Ticket.ExternalPriority != 99 || !detail.Readiness.Selected {
		t.Fatalf("priority changed ticket identity or selection = %#v", detail)
	}

	cancelled, err := service.Publish(ctx, publishInput(workspaceID, first.Ticket.TicketID, 99, 1, closure, "cancelled", "operator cancelled this work"))
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled.Revision.ReplacesRevisionRowID.Valid || cancelled.Revision.ReplacesRevisionRowID.Int64 != first.Revision.ID {
		t.Fatalf("replacement lineage = %#v", cancelled.Revision)
	}
	detail, err = service.Read(ctx, first.Ticket.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Readiness.Ready || !hasReason(detail.Readiness, "cancelled") {
		t.Fatalf("cancelled ticket readiness = %#v", detail.Readiness)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE delivery_ticket_revisions SET goal = 'mutated' WHERE id = ?`, first.Revision.ID); err == nil {
		t.Fatal("published revision was mutable")
	}
}

func TestReadinessRejectsStaleAuthorityAndDependencyRevision(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := service.Publish(ctx, publishInput(workspaceID, "P4-T1", 70, 0, closure, "dependency", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{TicketID: dependency.Ticket.TicketID, RevisionRowID: dependency.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "dependency approved"}); err != nil {
		t.Fatal(err)
	}
	dependentInput := publishInput(workspaceID, "P4-T2", 69, 0, closure, "dependent", "")
	dependentInput.Revision.Dependencies = []DependencyInput{{RevisionRowID: dependency.Revision.ID, Outcome: "satisfied"}}
	dependent, err := service.Publish(ctx, dependentInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{TicketID: dependent.Ticket.TicketID, RevisionRowID: dependent.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "dependent approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, publishInput(workspaceID, dependency.Ticket.TicketID, 70, 1, closure, "dependency replacement", "")); err != nil {
		t.Fatal(err)
	}
	detail, err := service.Read(ctx, dependent.Ticket.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Readiness.Ready || !hasReason(detail.Readiness, "dependency_revision_stale") {
		t.Fatalf("stale dependency readiness = %#v", detail.Readiness)
	}

	newAuthority := setCurrentAuthority(t, ctx, store, workspaceID, closure.ID, "authority-ticket-2")
	if _, err := service.Approve(ctx, ApproveInput{TicketID: dependent.Ticket.TicketID, RevisionRowID: dependent.Revision.ID, AuthorityRevisionID: newAuthority, Rationale: "approval must reject stale revision"}); !errors.Is(err, approvals.ErrStaleAuthority) {
		t.Fatalf("approval under changed authority error = %v", err)
	}
	detail, err = service.Read(ctx, dependent.Ticket.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasReason(detail.Readiness, "approval_missing_or_stale") {
		t.Fatalf("same-source authority replacement readiness = %#v", detail.Readiness)
	}
}

func ticketFixture(t *testing.T) (*workflowstore.Store, string, workflowstore.SourceVaultClosure, string) {
	t.Helper()
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.sqlite"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-ticket', 'Ticket')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-ticket', 'relay', 'vaults/ticket') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	closure := addClosureWithVault(t, ctx, store, vaultID, strings.Repeat("a", 40), "closure-ticket-1")
	var workspaceID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-ticket', (SELECT id FROM projects WHERE project_id = 'project-ticket'), 'ticket') RETURNING id`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	authorityID := setCurrentAuthority(t, ctx, store, "workspace-ticket", closure.ID, "authority-ticket-1")
	if workspaceID < 1 {
		t.Fatal("workspace was not created")
	}
	return store, "workspace-ticket", closure, authorityID
}

func addClosure(t *testing.T, ctx context.Context, store *workflowstore.Store, commit, closureID string) workflowstore.SourceVaultClosure {
	t.Helper()
	var vaultID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM source_vaults WHERE vault_id = 'vault-ticket'`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	return addClosureWithVault(t, ctx, store, vaultID, commit, closureID)
}

func addClosureWithVault(t *testing.T, ctx context.Context, store *workflowstore.Store, vaultID int64, commit, closureID string) workflowstore.SourceVaultClosure {
	t.Helper()
	var closure workflowstore.SourceVaultClosure
	err := store.DB().QueryRowContext(ctx, `
INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at)
VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(generation), 0) + 1 FROM source_vault_closures WHERE vault_row_id = ?), ?, 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z')
RETURNING id, closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, failure_reason, import_started_at, verified_at, released_at, created_at, updated_at`,
		closureID, vaultID, commit, strings.Repeat("c", 40), vaultID, "refs/relay/closures/"+closureID).Scan(
		&closure.ID, &closure.ClosureID, &closure.VaultRowID, &closure.CommitOID, &closure.TreeOID, &closure.Generation, &closure.RefName, &closure.State, &closure.FailureReason, &closure.ImportStartedAt, &closure.VerifiedAt, &closure.ReleasedAt, &closure.CreatedAt, &closure.UpdatedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	return closure
}

func setCurrentAuthority(t *testing.T, ctx context.Context, store *workflowstore.Store, workspaceID string, closureRowID int64, authorityID string) string {
	t.Helper()
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var authority workflowstore.FeatureWorkspaceAuthorityRevision
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		prior, err := tx.ListFeatureWorkspaceAuthorityRevisions(ctx, workspace.ID)
		if err != nil {
			return err
		}
		authority, err = tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: authorityID, WorkspaceRowID: workspace.ID, RevisionNumber: int64(len(prior) + 1), SourceClosureRowID: sql.NullInt64{Int64: closureRowID, Valid: true}})
		if err != nil {
			return err
		}
		_, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, authority.ID, workspace.WorkspaceID, workspace.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return authority.AuthorityRevisionID
}

func publishInput(workspaceID, ticketID string, priority, expectedRevision int64, closure workflowstore.SourceVaultClosure, name, cancellationReason string) PublishInput {
	return PublishInput{WorkspaceID: workspaceID, TicketID: ticketID, ExternalPriority: priority, ExpectedRevisionNumber: expectedRevision, Revision: RevisionInput{
		RepoTarget: "relay", Branch: "main", BaseCommit: closure.CommitOID, SourceClosureRowID: closure.ID, SourcePath: "tickets/" + strings.ToLower(ticketID) + ".json",
		Goal: "Publish " + name + " revision.", Context: "Retain exact canonical and rendered ticket artifacts.", TransitionApplicability: "not_required", CancellationReason: cancellationReason,
		CanonicalJSON: []byte(`{"ticket":"` + ticketID + `","revision":"` + name + `"}`), RenderedMarkdown: []byte("# " + ticketID + "\n\n" + name + "\n"),
		Members: []RevisionMemberInput{{Kind: "implementation_obligation", Path: "internal/app/tickets", Text: "Derive readiness from current facts."}},
	}}
}

func hasReason(readiness Readiness, want string) bool {
	for _, reason := range readiness.Reasons {
		if reason == want {
			return true
		}
	}
	return false
}
