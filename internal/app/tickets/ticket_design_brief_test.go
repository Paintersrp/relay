package tickets

import (
	"context"
	"errors"
	"strings"
	"testing"

	"relay/internal/testfixtures"
	workflowstore "relay/internal/store/workflow"
)

func TestTicketDesignBriefAdmissionApprovalAndReadback(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR1", 50, 0, "brief")
	selection, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR1", RevisionRowID: published.Revision.ID, Rationale: "select for brief lifecycle"})
	if err != nil {
		t.Fatal(err)
	}

	state, err := service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "none" || state.TicketID != "P4-BR1" || state.RevisionNumber != 1 {
		t.Fatalf("brief state before admission = %+v, %v", state, err)
	}

	// Authoring admits the exact admissible brief bytes; the canonical filename
	// is derived server-side from the selected Ticket revision.
	admitted, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "ticket.ticket-P4-BR1.r1.design-brief.md"; admitted.Filename != want {
		t.Fatalf("admitted brief filename = %q, want %q", admitted.Filename, want)
	}
	if admitted.Brief.SelectionRowID != selection.Selection.ID || admitted.Brief.RevisionRowID != published.Revision.ID || admitted.Brief.ArtifactSha256 != digestCandidate([]byte(testfixtures.TicketDesignBrief)) {
		t.Fatalf("admitted brief = %#v", admitted.Brief)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "authored" {
		t.Fatalf("brief state after admission = %+v, %v", state, err)
	}

	// A second admission for the same active selection conflicts.
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); !errors.Is(err, ErrTicketDesignBriefConflict) {
		t.Fatalf("duplicate admission error = %v, want ErrTicketDesignBriefConflict", err)
	}

	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	// Approval is an explicit confirmed owner mutation that follows the
	// completed read-only review; it must never bypass the review fact.
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID, ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "approval without review", CreatedIdentity: "auditor",
	}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("approval without review error = %v, want ErrBriefReviewIncomplete", err)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "authored" {
		t.Fatalf("brief state after rejected approval = %+v, %v", state, err)
	}

	// The narrow review-completion fact records only that the read-only review
	// handoff finished; no review outcome is persisted.
	reviewed, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor"})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Review.BriefRowID != admitted.Brief.ID || reviewed.Review.ReviewerIdentity != "auditor" {
		t.Fatalf("completed review = %#v", reviewed.Review)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "reviewed" {
		t.Fatalf("brief state after review = %+v, %v", state, err)
	}
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor"}); !errors.Is(err, ErrTicketDesignBriefConflict) {
		t.Fatalf("duplicate review completion error = %v, want ErrTicketDesignBriefConflict", err)
	}

	approved, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID, ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "reviewed and approved", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Approval.BriefRowID != admitted.Brief.ID || approved.Approval.BriefSha256 != admitted.Brief.ArtifactSha256 {
		t.Fatalf("approved approval = %#v", approved.Approval)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "approved" {
		t.Fatalf("brief state after approval = %+v, %v", state, err)
	}
	// Approval is an explicit single mutation: a second approval conflicts.
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID, ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "second approval", CreatedIdentity: "auditor",
	}); !errors.Is(err, ErrTicketDesignBriefConflict) {
		t.Fatalf("duplicate approval error = %v, want ErrTicketDesignBriefConflict", err)
	}
}

func TestTicketDesignBriefAdmissionRejectsInvalidBytesAndMissingSelection(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR2", 51, 0, "brief reject")
	selection, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR2", RevisionRowID: published.Revision.ID, Rationale: "select for rejection"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte("# Not a complete brief\n"), CreatedIdentity: "planner"}); !errors.Is(err, ErrInvalidTicketDesignBrief) {
		t.Fatalf("invalid brief admission error = %v, want ErrInvalidTicketDesignBrief", err)
	}

	// A superseded selection has no active basis and cannot admit a brief.
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionDeliveryTicketSelection(ctx, selection.Selection.SelectionID, "superseded")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); !errors.Is(err, ErrNoActiveSelection) {
		t.Fatalf("admission without active selection error = %v, want ErrNoActiveSelection", err)
	}
	state, err := service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "none" {
		t.Fatalf("brief state after consumption = %+v, %v", state, err)
	}
}

func TestTicketDesignBriefApprovalRejectsMissingBriefAndStaleVersion(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR3", 52, 0, "brief approval reject")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR3", RevisionRowID: published.Revision.ID, Rationale: "select for approval rejection"}); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID, ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "no brief authored", CreatedIdentity: "auditor",
	}); !errors.Is(err, ErrTicketDesignBriefNotFound) {
		t.Fatalf("approval without brief error = %v, want ErrTicketDesignBriefNotFound", err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID, ExpectedVersion: workspace.Version + 99,
		OperatorConfirmationEvidence: "stale version", CreatedIdentity: "auditor",
	}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale version approval error = %v, want ErrRevisionConflict", err)
	}
}

func TestTicketDesignBriefRowsAreImmutableHistory(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR4", 53, 0, "brief immutable")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR4", RevisionRowID: published.Revision.ID, Rationale: "select for immutability"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE ticket_design_briefs SET filename = 'mutated.md' WHERE selection_row_id = ?`, selectionRowIDForTicket(t, ctx, store, "P4-BR4")); err == nil {
		t.Fatal("ticket design brief row was mutable")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM ticket_design_briefs`); err == nil {
		t.Fatal("ticket design brief rows are deletable")
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE ticket_design_brief_reviews SET reviewer_identity = 'mutated'`); err == nil {
		t.Fatal("ticket design brief review row was mutable")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM ticket_design_brief_reviews`); err == nil {
		t.Fatal("ticket design brief review rows are deletable")
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID, ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "approved immutable", CreatedIdentity: "auditor",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE ticket_design_brief_approvals SET operator_confirmation_evidence = 'mutated'`); err == nil {
		t.Fatal("ticket design brief approval row was mutable")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM ticket_design_brief_approvals`); err == nil {
		t.Fatal("ticket design brief approval rows are deletable")
	}
	if strings.TrimSpace(workspace.WorkspaceID) == "" {
		t.Fatal("workspace identity missing")
	}
}

func TestApproveCurrentTicketDesignBriefResolvesBriefServerSide(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR5", 54, 0, "brief approve current")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR5", RevisionRowID: published.Revision.ID, Rationale: "select for server-side approval"}); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	// Approve-current without an authored brief fails server-side.
	if _, err := service.ApproveCurrentTicketDesignBrief(ctx, ApproveCurrentBriefInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, Evidence: "guided-operator-approval"}); !errors.Is(err, ErrTicketDesignBriefNotFound) {
		t.Fatalf("approve current without brief error = %v, want ErrTicketDesignBriefNotFound", err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	// Review completion is a mandatory separate fact before approval.
	if _, err := service.ApproveCurrentTicketDesignBrief(ctx, ApproveCurrentBriefInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, Evidence: "guided-operator-approval"}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("approve current without review error = %v, want ErrBriefReviewIncomplete", err)
	}
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor"}); err != nil {
		t.Fatal(err)
	}
	// The guided approve resolves the current brief identity server-side; no
	// brief ID or digest is supplied by the caller.
	approved, err := service.ApproveCurrentTicketDesignBrief(ctx, ApproveCurrentBriefInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, Evidence: "guided-operator-approval"})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Approval.BriefRowID != approved.Brief.ID || approved.Approval.OperatorConfirmationEvidence != "guided-operator-approval" {
		t.Fatalf("server-side approved approval = %#v", approved.Approval)
	}
	state, err := service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "approved" {
		t.Fatalf("brief state after server-side approval = %+v, %v", state, err)
	}
}

func selectionRowIDForTicket(t *testing.T, ctx context.Context, store *workflowstore.Store, ticketID string) int64 {
	t.Helper()
	ticket, err := store.GetDeliveryTicketByTicketID(ctx, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	var rowID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM delivery_ticket_selections WHERE workspace_row_id = ? ORDER BY id DESC LIMIT 1`, ticket.WorkspaceRowID).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	return rowID
}
