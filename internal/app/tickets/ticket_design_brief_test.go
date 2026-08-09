package tickets

import (
	"context"
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
	reviewed, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Review.ReviewerIdentity != "auditor" || reviewed.Review.Disposition != string(TicketDesignBriefReviewReadyForApproval) {
		t.Fatalf("completed review = %#v", reviewed.Review)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "authored" {
		t.Fatalf("brief state after review = %+v, %v", state, err)
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
	if entry.BriefID != admitted.Brief.BriefID || entry.SelectionID != selection.Selection.SelectionID || entry.SelectionState != "active" || entry.TicketID != "P4-BR1" || entry.RevisionNumber != 1 || entry.Filename != admitted.Filename || entry.SHA256 != admitted.Brief.ArtifactSha256 || entry.SizeBytes != admitted.Brief.ArtifactSizeBytes || entry.Status != "approved" || entry.ApprovalID != approved.Approval.ApprovalID || entry.Historical {
		t.Fatalf("brief lineage entry = %+v", entry)
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
	review, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewNeedsRevision, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)})
	if err != nil || review.Review.Disposition != string(TicketDesignBriefReviewNeedsRevision) {
		t.Fatalf("needs-revision review = %#v, %v", review, err)
	}
	state, err := service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "authored" {
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
	if err != nil || state.State != "authored" {
		t.Fatalf("rejected needs-revision approval changed state = %+v, %v", state, err)
	}
	replacementBytes := append([]byte(testfixtures.TicketDesignBrief), '\n')
	second, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: replacementBytes, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Brief.SelectionRowID != selection.Selection.ID || first.Brief.AttemptNumber != 1 || second.Brief.AttemptNumber != 2 || first.Brief.BriefID == second.Brief.BriefID || first.Brief.ArtifactSha256 == second.Brief.ArtifactSha256 {
		t.Fatalf("replacement brief did not create a new immutable attempt on the active selection: first=%#v second=%#v", first.Brief, second.Brief)
	}
	oldSelection, err := store.GetDeliveryTicketSelectionBySelectionID(ctx, selection.Selection.SelectionID)
	if err != nil || oldSelection.State != "active" || !oldSelection.CurrentTicketDesignBriefRowID.Valid || oldSelection.CurrentTicketDesignBriefRowID.Int64 != second.Brief.ID {
		t.Fatalf("original selection after replacement = %#v, %v", oldSelection, err)
	}
	oldBytes, err := store.ReadTicketDesignBriefBytes(ctx, first.Brief.BriefID, 1<<20)
	if err != nil || string(oldBytes) != testfixtures.TicketDesignBrief {
		t.Fatalf("historical brief bytes = %q, %v", oldBytes, err)
	}
	lineage, err := service.ReadWorkspaceBriefIntegrity(ctx, workspaceID)
	if err != nil || len(lineage.Briefs) != 2 || len(lineage.Diagnostics) != 0 {
		t.Fatalf("replacement lineage = %+v, %v", lineage, err)
	}
	lineageByBriefID := make(map[string]TicketDesignBriefIntegrity, len(lineage.Briefs))
	for _, entry := range lineage.Briefs {
		lineageByBriefID[entry.BriefID] = entry
	}
	if old := lineageByBriefID[first.Brief.BriefID]; !old.Historical || old.AttemptNumber != 1 || old.SelectionState != "active" || old.ApprovalID != "" {
		t.Fatalf("historical replacement lineage = %+v", old)
	}
	if current := lineageByBriefID[second.Brief.BriefID]; current.Historical || current.AttemptNumber != 2 || current.SelectionState != "active" || current.ApprovalID != "" {
		t.Fatalf("current replacement lineage = %+v", current)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "replacement cannot inherit review", CreatedIdentity: "operator"}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("replacement inherited review error = %v, want ErrBriefReviewIncomplete", err)
	}
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: replacementBytes}); err != nil {
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
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
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
	// The guided approve resolves the current brief identity server-side; no
	// brief ID or digest is supplied by the caller and the fresh
	// process-local ready-review continuation is consumed exactly once.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "guided-operator-approval", CreatedIdentity: "guided-operator"})
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

func TestReadyReviewCreatesNoApprovalUntilDistinctExplicitApproval(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR6", 55, 0, "brief distinct approval")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR6", RevisionRowID: published.Revision.ID, Rationale: "select for distinct approval"}); err != nil {
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
	// Ready review completion alone must not create any approval or planner
	// refresh; it records only the process-local continuation.
	reviewed, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Refresh != nil || reviewed.Disposition != TicketDesignBriefReviewReadyForApproval {
		t.Fatalf("ready review carried planner refresh or wrong disposition = %#v", reviewed)
	}
	state, err := service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "authored" {
		t.Fatalf("ready review created durable approval state = %+v, %v", state, err)
	}
	// The distinct explicit approval consumes the fresh process-local
	// continuation and the exact confirmation evidence.
	approved, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "distinct approval", CreatedIdentity: "operator"})
	if err != nil || approved.Approval.BriefRowID != admitted.Brief.ID {
		t.Fatalf("distinct approval = %#v, %v", approved, err)
	}
	state, err = service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "approved" {
		t.Fatalf("brief state after distinct approval = %+v, %v", state, err)
	}
	// The continuation is consumed exactly once: a second approval cannot
	// reuse it and conflicts on the already-approved brief.
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "second approval", CreatedIdentity: "operator"}); !errors.Is(err, ErrTicketDesignBriefConflict) {
		t.Fatalf("second distinct approval error = %v, want ErrTicketDesignBriefConflict", err)
	}
}

func TestNeedsRevisionClearsReadyContinuationAndReturnsExactPlannerRefresh(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR7", 56, 0, "brief refresh exact")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR7", RevisionRowID: published.Revision.ID, Rationale: "select for refresh exactness"}); err != nil {
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
	// A ready review records a process-local continuation first.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	// needs_revision clears it and returns the exact ordinary planner refresh.
	rejected, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewNeedsRevision, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Disposition != TicketDesignBriefReviewNeedsRevision || rejected.Refresh == nil || rejected.Refresh.OperationID != "planner.ticket_design_brief" || rejected.Refresh.AuditorReviewResult != string(TicketDesignBriefReviewNeedsRevision) || !equalBytes(rejected.Refresh.ReviewedBrief, []byte(testfixtures.TicketDesignBrief)) || rejected.Refresh.ReviewedBrief == nil {
		t.Fatalf("needs-revision refresh = %#v", rejected.Refresh)
	}
	// The cleared continuation cannot authorize approval.
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "needs revision cannot approve", CreatedIdentity: "operator"}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("approval after needs revision error = %v, want ErrBriefReviewIncomplete", err)
	}
	if _, err := service.ReadWorkspaceBriefIntegrity(ctx, workspaceID); err != nil {
		t.Fatal(err)
	}
	if admitted.Brief.ArtifactSha256 != digestCandidate([]byte(testfixtures.TicketDesignBrief)) {
		t.Fatalf("admitted digest changed = %#v", admitted.Brief)
	}
}

func TestReplacementBriefInvalidatesReadyContinuationOnSameActiveSelection(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR8", 58, 0, "brief continuation replacement")
	selection, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR8", RevisionRowID: published.Revision.ID, Rationale: "select for continuation invalidation"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	// A ready review stores the private continuation for the current brief.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	// The replacement is a new immutable attempt on the same active selection
	// and invalidates the pending continuation.
	replacementBytes := append([]byte(testfixtures.TicketDesignBrief), '\n')
	second, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: replacementBytes, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Brief.SelectionRowID != selection.Selection.ID || second.Brief.SelectionRowID != first.Brief.SelectionRowID || first.Brief.AttemptNumber != 1 || second.Brief.AttemptNumber != 2 || first.Brief.BriefID == second.Brief.BriefID || second.Brief.RevisionRowID != first.Brief.RevisionRowID {
		t.Fatalf("replacement did not advance the active selection immutably: first=%#v second=%#v", first.Brief, second.Brief)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "stale continuation cannot approve", CreatedIdentity: "operator"}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("replacement approval with stale continuation error = %v, want ErrBriefReviewIncomplete", err)
	}
	oldBytes, err := store.ReadTicketDesignBriefBytes(ctx, first.Brief.BriefID, 1<<20)
	if err != nil || string(oldBytes) != testfixtures.TicketDesignBrief {
		t.Fatalf("replaced brief bytes = %q, %v", oldBytes, err)
	}
	state, err := service.ReadWorkspaceBriefState(ctx, workspaceID)
	if err != nil || state.State != "authored" {
		t.Fatalf("replacement state = %+v, %v", state, err)
	}
	// A fresh ready review on the replacement enables the distinct approval.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: replacementBytes}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "fresh replacement approval", CreatedIdentity: "operator"})
	if err != nil || approved.Brief.ID != second.Brief.ID {
		t.Fatalf("fresh replacement approval = %#v, %v", approved, err)
	}
	lineage, err := service.ReadWorkspaceBriefIntegrity(ctx, workspaceID)
	if err != nil || len(lineage.Briefs) != 2 || len(lineage.Diagnostics) != 0 {
		t.Fatalf("replacement lineage = %+v, %v", lineage, err)
	}
}

func TestTicketDesignBriefReviewBindingRejectsStaleAttemptAfterReplacement(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR-BIND", 60, 0, "brief exact binding")
	selection, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR-BIND", RevisionRowID: published.Revision.ID, Rationale: "select for exact review binding"})
	if err != nil {
		t.Fatal(err)
	}
	firstBytes := []byte(testfixtures.TicketDesignBrief)
	first, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: firstBytes, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	replacementBytes := append([]byte(testfixtures.TicketDesignBrief), '\n')
	second, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: replacementBytes, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Brief.SelectionRowID != selection.Selection.ID || first.Brief.AttemptNumber != 1 || second.Brief.AttemptNumber != 2 {
		t.Fatalf("replacement did not create attempt 2: first=%#v second=%#v", first.Brief, second.Brief)
	}
	// A stale ready completion carrying attempt-1's reviewed bytes is rejected;
	// no result, continuation, or refresh attaches to attempt 2.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: firstBytes}); !errors.Is(err, ErrTicketDesignBriefBytesMismatch) {
		t.Fatalf("stale ready review error = %v, want ErrTicketDesignBriefBytesMismatch", err)
	}
	// A stale needs-revision completion carrying attempt-1's reviewed bytes is
	// also rejected, so the stale result cannot drive a planner refresh
	// replacement.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewNeedsRevision, ReviewedBytes: firstBytes}); !errors.Is(err, ErrTicketDesignBriefBytesMismatch) {
		t.Fatalf("stale needs-revision review error = %v, want ErrTicketDesignBriefBytesMismatch", err)
	}
	// Neither stale review armed a continuation, so approval of attempt 2 still
	// requires a fresh ready review of attempt 2's exact bytes.
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "stale result must not attach", CreatedIdentity: "operator"}); !errors.Is(err, ErrBriefReviewIncomplete) {
		t.Fatalf("approval after stale reviews error = %v, want ErrBriefReviewIncomplete", err)
	}
	// A fresh ready review of attempt 2's exact bytes arms its continuation and
	// the distinct explicit approval attaches to attempt 2 alone.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: replacementBytes}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "fresh attempt 2 review approval", CreatedIdentity: "operator"})
	if err != nil || approved.Brief.ID != second.Brief.ID {
		t.Fatalf("fresh attempt 2 approval=%#v err=%v", approved, err)
	}
}

func TestHasPendingCurrentBriefApprovalIsTransientAndExact(t *testing.T) {
	ctx := context.Background()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-BR9", 59, 0, "brief transient pending")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceID, TicketID: "P4-BR9", RevisionRowID: published.Revision.ID, Rationale: "select for transient pending"}); err != nil {
		t.Fatal(err)
	}
	// No authored brief: no pending approval.
	pending, err := service.HasPendingCurrentBriefApproval(ctx, workspaceID)
	if err != nil || pending {
		t.Fatalf("pending before admission = %v, %v", pending, err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	// Admission clears any continuation; no review has run yet.
	pending, err = service.HasPendingCurrentBriefApproval(ctx, workspaceID)
	if err != nil || pending {
		t.Fatalf("pending before review = %v, %v", pending, err)
	}
	// A ready review stores the process-local continuation for the exact brief.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	pending, err = service.HasPendingCurrentBriefApproval(ctx, workspaceID)
	if err != nil || !pending {
		t.Fatalf("pending after ready review = %v, %v", pending, err)
	}
	// needs_revision clears the continuation so it can never authorize approval.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewNeedsRevision, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	pending, err = service.HasPendingCurrentBriefApproval(ctx, workspaceID)
	if err != nil || pending {
		t.Fatalf("pending after needs revision = %v, %v", pending, err)
	}
	// A fresh ready review arms it again, and the distinct approval consumes it.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "consumed transient", CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	pending, err = service.HasPendingCurrentBriefApproval(ctx, workspaceID)
	if err != nil || pending {
		t.Fatalf("pending after consumed approval = %v, %v", pending, err)
	}
}

func TestReadyReviewsInIndependentWorkspacesRemainIndependent(t *testing.T) {
	ctx := context.Background()
	store, workspaceAID, closure, authorityAID := ticketFixture(t)
	// A second independent workspace bound to the same ready source-backed
	// closure.
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-ticket-b', (SELECT id FROM projects WHERE project_id = 'project-ticket'), 'ticket-b')`); err != nil {
		t.Fatal(err)
	}
	const workspaceBID = "workspace-ticket-b"
	authorityBID := setCurrentAuthority(t, ctx, store, workspaceBID, closure.ID, "authority-ticket-b-1")
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	// Two independent current selections, each with its own authored brief.
	first := publishApprovedTicket(t, ctx, service, workspaceAID, closure, authorityAID, "P4-BR-A", 61, 0, "brief workspace A")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceAID, TicketID: "P4-BR-A", RevisionRowID: first.Revision.ID, Rationale: "select A for independent briefs"}); err != nil {
		t.Fatal(err)
	}
	second := publishApprovedTicket(t, ctx, service, workspaceBID, closure, authorityBID, "P4-BR-B", 62, 0, "brief workspace B")
	if _, err := service.Select(ctx, SelectInput{WorkspaceID: workspaceBID, TicketID: "P4-BR-B", RevisionRowID: second.Revision.ID, Rationale: "select B for independent briefs"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceAID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdmitTicketDesignBrief(ctx, TicketDesignBriefAdmissionInput{WorkspaceID: workspaceBID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	// Both workspaces receive ready reviews; B's review must not displace A's
	// private continuation even though neither workspace's selection changed.
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceAID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteTicketDesignBriefReview(ctx, CompleteBriefReviewInput{WorkspaceID: workspaceBID, ReviewerIdentity: "auditor", Disposition: TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)}); err != nil {
		t.Fatal(err)
	}
	for _, workspaceID := range []string{workspaceAID, workspaceBID} {
		pending, err := service.HasPendingCurrentBriefApproval(ctx, workspaceID)
		if err != nil || !pending {
			t.Fatalf("pending for %s after both ready reviews = %v, %v", workspaceID, pending, err)
		}
	}
	// Each workspace later explicitly approves its own reviewed brief: A's
	// approval still succeeds even though B's ready review ran afterwards.
	workspaceA, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceAID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceB, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceBID)
	if err != nil {
		t.Fatal(err)
	}
	approvedA, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceAID, ExpectedVersion: workspaceA.Version, OperatorConfirmationEvidence: "A approves its own review", CreatedIdentity: "operator"})
	if err != nil {
		t.Fatalf("workspace A approval after B ready review = %v", err)
	}
	approvedB, err := service.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{WorkspaceID: workspaceBID, ExpectedVersion: workspaceB.Version, OperatorConfirmationEvidence: "B approves its own review", CreatedIdentity: "operator"})
	if err != nil {
		t.Fatalf("workspace B approval after A approval = %v", err)
	}
	if approvedA.Approval.BriefRowID == 0 || approvedB.Approval.BriefRowID == 0 || approvedA.Approval.BriefRowID == approvedB.Approval.BriefRowID {
		t.Fatalf("approvals did not bind distinct briefs: A=%#v B=%#v", approvedA.Approval, approvedB.Approval)
	}
	// Single-use approval is maintained per workspace: each approval consumed
	// exactly its own workspace's continuation.
	for _, workspaceID := range []string{workspaceAID, workspaceBID} {
		pending, err := service.HasPendingCurrentBriefApproval(ctx, workspaceID)
		if err != nil || pending {
			t.Fatalf("pending for %s after its approval = %v, %v", workspaceID, pending, err)
		}
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
