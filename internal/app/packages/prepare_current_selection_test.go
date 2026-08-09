package packages

import (
	"context"
	"errors"
	"testing"

	"relay/internal/app/tickets"
	"relay/internal/guidedapp"
	"relay/internal/testfixtures"
)

func TestPrepareCurrentSelectionResolvesApprovedBriefServerSide(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)
	ticketsService, err := tickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}

	// Without an authored brief the package owner refuses preparation.
	if _, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: "workspace-package"}); !errors.Is(err, ErrApprovedBriefMissing) {
		t.Fatalf("prepare without brief error = %v, want ErrApprovedBriefMissing", err)
	}

	// Authoring admits the brief through the delivery owner; an authored but
	// unapproved brief must not authorize package preparation.
	admitted, err := ticketsService.AdmitTicketDesignBrief(ctx, tickets.TicketDesignBriefAdmissionInput{
		WorkspaceID: "workspace-package", Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if admitted.Filename != fixture.brief.DisplayName {
		t.Fatalf("admitted brief filename = %q, want %q", admitted.Filename, fixture.brief.DisplayName)
	}
	if _, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: "workspace-package"}); !errors.Is(err, ErrBriefNotApproved) {
		t.Fatalf("prepare without approval error = %v, want ErrBriefNotApproved", err)
	}

	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(ctx, "workspace-package")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketsService.CompleteAndApproveTicketDesignBrief(ctx, tickets.CompleteBriefReviewInput{WorkspaceID: "workspace-package", ReviewerIdentity: "auditor", Disposition: tickets.TicketDesignBriefReviewReadyForApproval}, tickets.TicketDesignBriefApprovalInput{
		WorkspaceID: "workspace-package", ExpectedVersion: workspace.Version,
		OperatorConfirmationEvidence: "reviewed and approved", CreatedIdentity: "auditor",
	}); err != nil {
		t.Fatal(err)
	}

	// Preparation resolves the current active selection and approved Brief
	// server-side; the caller supplied no selection ID, brief ID, or digest.
	result, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: "workspace-package"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "prepared" || result.PackageID == "" {
		t.Fatalf("prepare result = %+v", result)
	}
	state, err := fixture.service.ReadWorkspacePackageState(ctx, "workspace-package")
	if err != nil || state.State != "prepared" || state.PackageID != result.PackageID {
		t.Fatalf("package state after prepared = %+v, %v", state, err)
	}
	// A second prepare must not duplicate package state for the same selection.
	second, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: "workspace-package"})
	if err == nil {
		t.Fatalf("second prepare unexpectedly succeeded: %+v", second)
	}
}

func TestPrepareCurrentSelectionRejectsInvalidWorkspaceInput(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)
	if _, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: " workspace-package"}); !errors.Is(err, ErrInvalidPackageInput) {
		t.Fatalf("invalid workspace prepare error = %v, want ErrInvalidPackageInput", err)
	}
	if _, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: ""}); !errors.Is(err, ErrInvalidPackageInput) {
		t.Fatalf("empty workspace prepare error = %v, want ErrInvalidPackageInput", err)
	}
}

func TestPrepareCurrentSelectionDoesNotUseNeedsRevisionBriefAfterReplacement(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)
	ticketService, err := tickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ticketService.AdmitTicketDesignBrief(ctx, tickets.TicketDesignBriefAdmissionInput{
		WorkspaceID: "workspace-package", Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.CompleteTicketDesignBriefReview(ctx, tickets.CompleteBriefReviewInput{
		WorkspaceID: "workspace-package", ReviewerIdentity: "auditor", Disposition: tickets.TicketDesignBriefReviewNeedsRevision,
	}); err != nil {
		t.Fatal(err)
	}
	replacement, err := ticketService.AdmitTicketDesignBrief(ctx, tickets.TicketDesignBriefAdmissionInput{
		WorkspaceID: "workspace-package", Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Brief.SelectionRowID != first.Brief.SelectionRowID || replacement.Brief.AttemptNumber != first.Brief.AttemptNumber+1 {
		t.Fatalf("replacement brief did not advance the current selection attempt: first=%#v replacement=%#v", first.Brief, replacement.Brief)
	}
	if _, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: "workspace-package"}); !errors.Is(err, ErrBriefNotApproved) {
		t.Fatalf("needs-revision brief authorized replacement package: %v", err)
	}
}
