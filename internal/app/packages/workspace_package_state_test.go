package packages

import (
	"context"
	"errors"
	"testing"

	"relay/internal/guidedapp"
)

func TestReadWorkspacePackageStateTracksNonePreparedAndApproved(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)

	none, err := fixture.service.ReadWorkspacePackageState(ctx, "workspace-package")
	if err != nil || none.State != "none" || none.PackageID != "" || none.RunID != "" {
		t.Fatalf("package state before prepare = %+v, %v", none, err)
	}

	prepared := preparePackage(t, fixture, false)
	state, err := fixture.service.ReadWorkspacePackageState(ctx, "workspace-package")
	if err != nil || state.State != "prepared" || state.PackageID != prepared.Package.PackageID || state.RunID != "" || state.RunStatus != "" {
		t.Fatalf("package state after prepare = %+v, %v", state, err)
	}

	if err := fixture.service.ApproveCurrentPackage(ctx, guidedapp.ApprovePackageInput{WorkspaceID: "workspace-package", Evidence: "guided-operator-approval"}); err != nil {
		t.Fatal(err)
	}
	approved, err := fixture.service.ReadWorkspacePackageState(ctx, "workspace-package")
	if err != nil || approved.State != "approved" || approved.PackageID != prepared.Package.PackageID || approved.RunID == "" || approved.RunStatus == "" || approved.RunBaseCommit != fixture.baseCommit {
		t.Fatalf("package state after approve = %+v, %v", approved, err)
	}
	selection, err := fixture.store.GetDeliveryTicketSelectionBySelectionID(ctx, fixture.selectionID)
	if err != nil || selection.State != "consumed" {
		t.Fatalf("selection after guided approve = %+v, %v", selection, err)
	}
}

func TestApproveCurrentPackageRejectsMissingOrApprovedPackage(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)

	if err := fixture.service.ApproveCurrentPackage(ctx, guidedapp.ApprovePackageInput{WorkspaceID: "workspace-package", Evidence: "guided-operator-approval"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("approve without prepared package error = %v, want ErrPackageBasisChanged", err)
	}

	preparePackage(t, fixture, false)
	if err := fixture.service.ApproveCurrentPackage(ctx, guidedapp.ApprovePackageInput{WorkspaceID: "workspace-package", Evidence: "guided-operator-approval"}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.ApproveCurrentPackage(ctx, guidedapp.ApprovePackageInput{WorkspaceID: "workspace-package", Evidence: "guided-operator-approval"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("second approve error = %v, want ErrPackageBasisChanged", err)
	}
}
