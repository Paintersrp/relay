package packages

import (
	"context"
	"errors"
	"strings"
	"testing"

	"relay/internal/guidedapp"
)

func TestPrepareCurrentSelectionResolvesApprovedTicketServerSide(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)

	// Preparation resolves the current active selection and the selected
	// approved Delivery Ticket server-side; the caller supplied no selection
	// ID, Ticket digest, or Brief identity.
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

func TestPrepareCurrentSelectionUsesOnlySelectedApprovedTicket(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)
	// Mutate the selected Ticket source bytes: the server-resolved preparation
	// must refuse because the approved Ticket exact bytes are the sole basis.
	mutated := []byte(strings.Replace(string(fixture.ticketDocument), `"goal":"Package the selected ticket."`, `"goal":"Different goal."`, 1))
	fixture.service.setSourceVaults(newPackageSourceVaultReader(fixture.sourcePath, mutated))
	if _, err := fixture.service.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: "workspace-package"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("prepare with mutated Ticket source error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, fixture.store.DB(), "execution_packages", 0)
}
