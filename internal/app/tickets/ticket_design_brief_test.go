package tickets

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
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

	// The narrow review-completion fact records its bounded disposition, never
	// findings or prose.
	reviewed, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Review.BriefRowID != admitted.Brief.ID || reviewed.Review.ReviewerIdentity != "auditor" || reviewed.Review.Disposition != string(TicketDesignBriefReviewReadyForApproval) {
		t.Fatalf("completed review = %#v", reviewed.Review)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "reviewed" || state.ReviewDisposition != string(TicketDesignBriefReviewReadyForApproval) {
		t.Fatalf("brief state after review = %+v, %v", state, err)
	}
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval}); !errors.Is(err, ErrTicketDesignBriefConflict) {
		t.Fatalf("duplicate review completion error = %v, want ErrTicketDesignBriefConflict", err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); !errors.Is(err, ErrTicketDesignBriefConflict) {
		t.Fatalf("ready brief replacement error = %v, want ErrTicketDesignBriefConflict", err)
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
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); !errors.Is(err, ErrTicketDesignBriefConflict) {
		t.Fatalf("approved brief replacement error = %v, want ErrTicketDesignBriefConflict", err)
	}
	lineage, err := service.ReadWorkspaceBriefIntegrity(ctx, workspaceID)
	if err != nil || len(lineage.Briefs) != 1 || len(lineage.Diagnostics) != 0 {
		t.Fatalf("brief lineage = %+v, %v", lineage, err)
	}
	entry := lineage.Briefs[0]
	if entry.BriefID != admitted.Brief.BriefID || entry.SelectionID != selection.Selection.SelectionID || entry.SelectionState != "active" || entry.TicketID != "P4-BR1" || entry.RevisionNumber != 1 || entry.Filename != admitted.Filename || entry.SHA256 != admitted.Brief.ArtifactSha256 || entry.SizeBytes != admitted.Brief.ArtifactSizeBytes || entry.Status != "approved" || entry.ReviewState != "completed" || entry.ReviewDisposition != string(TicketDesignBriefReviewReadyForApproval) || entry.ReviewID != reviewed.Review.ReviewID || entry.ApprovalID != approved.Approval.ApprovalID || entry.Historical {
		t.Fatalf("brief lineage entry = %+v", entry)
	}
}

func TestTicketDesignBriefApprovalPropagatesUnreadableReview(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR-READ-ERROR", 59, 0, "brief review read error")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR-READ-ERROR", RevisionRowID: published.Revision.ID, Rationale: "select for unreadable review"}); err != nil {
		t.Fatal(err)
	}
	admitted, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `ALTER TABLE ticket_design_brief_reviews RENAME COLUMN disposition TO unreadable_disposition`); err != nil {
		t.Fatal(err)
	}
	_, readErr := store.GetTicketDesignBriefReviewByBriefRowID(ctx, admitted.Brief.ID)
	if readErr == nil || errors.Is(readErr, sql.ErrNoRows) {
		t.Fatalf("review read error = %v, want non-no-row error", readErr)
	}
	_, err = service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID, ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "review read must propagate", CreatedIdentity: "auditor",
	})
	if err == nil || errors.Is(err, ErrBriefReviewIncomplete) || err.Error() != readErr.Error() {
		t.Fatalf("approval error = %v, want propagated review read error %v", err, readErr)
	}
}

func TestTicketDesignBriefNeedsRevisionAndReplacementCannotAuthorizeApproval(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR-DISPOSITION", 57, 0, "brief disposition")
	selection, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR-DISPOSITION", RevisionRowID: published.Revision.ID, Rationale: "select first brief basis"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	review, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewNeedsRevision})
	if err != nil || review.Review.BriefRowID != first.Brief.ID || review.Review.Disposition != string(TicketDesignBriefReviewNeedsRevision) {
		t.Fatalf("needs-revision review = %#v, %v", review, err)
	}
	state, err := service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "reviewed" || state.ReviewDisposition != string(TicketDesignBriefReviewNeedsRevision) {
		t.Fatalf("needs-revision brief state = %+v, %v", state, err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "needs revision cannot approve", CreatedIdentity: "operator"}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("needs-revision approval error = %v, want ErrBriefReviewIncomplete", err)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "reviewed" || state.ReviewDisposition != string(TicketDesignBriefReviewNeedsRevision) {
		t.Fatalf("rejected needs-revision approval changed state = %+v, %v", state, err)
	}
	second, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Brief.SelectionRowID == selection.Selection.ID {
		t.Fatalf("replacement brief reused original selection %d", selection.Selection.ID)
	}
	oldSelection, err := store.GetDeliveryTicketSelectionBySelectionID(ctx, selection.Selection.SelectionID)
	if err != nil || oldSelection.State != "superseded" {
		t.Fatalf("original selection after replacement = %#v, %v", oldSelection, err)
	}
	lineage, err := service.ReadWorkspaceBriefIntegrity(ctx, workspaceID)
	if err != nil || len(lineage.Briefs) != 2 || len(lineage.Diagnostics) != 0 {
		t.Fatalf("replacement lineage = %+v, %v", lineage, err)
	}
	lineageByBriefID := make(map[string]TicketDesignBriefIntegrity, len(lineage.Briefs))
	for _, entry := range lineage.Briefs {
		lineageByBriefID[entry.BriefID] = entry
	}
	if old := lineageByBriefID[first.Brief.BriefID]; !old.Historical || old.SelectionState != "superseded" || old.ReviewDisposition != string(TicketDesignBriefReviewNeedsRevision) || old.ApprovalID != "" {
		t.Fatalf("historical replacement lineage = %+v", old)
	}
	if current := lineageByBriefID[second.Brief.BriefID]; current.Historical || current.SelectionState != "active" || current.ReviewState != "none" || current.ApprovalID != "" {
		t.Fatalf("current replacement lineage = %+v", current)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "replacement cannot inherit review", CreatedIdentity: "operator"}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("replacement inherited review error = %v, want ErrBriefReviewIncomplete", err)
	}
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "ready replacement approved", CreatedIdentity: "operator"})
	if err != nil || approved.Brief.ID != second.Brief.ID || approved.Approval.BriefRowID != second.Brief.ID {
		t.Fatalf("ready replacement approval = %#v, %v", approved, err)
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
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval}); err != nil {
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
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval}); err != nil {
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
