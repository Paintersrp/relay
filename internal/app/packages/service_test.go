package packages

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

type packageFixture struct {
	store     *workflowstore.Store
	selection workflowstore.DeliveryTicketSelection
	closure   workflowstore.SourceVaultClosure
}

func TestPrepareAndApproveCreatesUnqualifiedSetupReadyRun(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageFixture(t, ctx)
	service, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	input := fixtureInput(t, fixture)

	prepared, err := service.Prepare(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Package.PackageSha256 == "" || len(prepared.Members) != 1 || len(prepared.Briefs) != 1 {
		t.Fatalf("unexpected prepared package: %#v", prepared)
	}
	selection, err := fixture.store.GetDeliveryTicketSelectionByRowID(ctx, fixture.selection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selection.State != "active" {
		t.Fatalf("prepared selection state = %q, want active", selection.State)
	}

	approved, err := service.Approve(ctx, ApproveInput{PackageID: prepared.Package.PackageID})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Run.Status != workflowstore.RunStatusSetupReady || approved.Run.PlanRowID.Valid || approved.Run.PlanPassRowID.Valid {
		t.Fatalf("unexpected ticket-oriented Run: %#v", approved.Run)
	}
	if !approved.Run.ExecutionPackageRowID.Valid || approved.Run.ExecutionPackageRowID.Int64 != prepared.Package.ID {
		t.Fatalf("Run package link = %#v", approved.Run.ExecutionPackageRowID)
	}
	if len(approved.RunArtifacts) != 2 {
		t.Fatalf("Run artifacts = %#v", approved.RunArtifacts)
	}
	selection, err = fixture.store.GetDeliveryTicketSelectionByRowID(ctx, fixture.selection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selection.State != "consumed" {
		t.Fatalf("approved selection state = %q, want consumed", selection.State)
	}
	bindings, err := fixture.store.ListExecutionPackageApprovalBindings(ctx, prepared.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 {
		t.Fatalf("approval bindings = %#v", bindings)
	}
}

func TestApprovalRevalidatesPackageBytesAndRollsBackRunAndConsumption(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageFixture(t, ctx)
	service, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	input := fixtureInput(t, fixture)
	prepared, err := service.Prepare(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(prepared.Briefs[0].RelativePath))
	if err := os.WriteFile(path, append(input.TicketDesignBriefs[0].Bytes, []byte("\nchanged")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{PackageID: prepared.Package.PackageID}); !errors.Is(err, ErrInvalidPackageInput) && !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("Approve error = %v, want package byte rejection", err)
	}
	selection, err := fixture.store.GetDeliveryTicketSelectionByRowID(ctx, fixture.selection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selection.State != "active" {
		t.Fatalf("failed approval consumed selection: %q", selection.State)
	}
	if _, err := fixture.store.GetRunByExecutionPackageRowID(ctx, prepared.Package.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("failed approval Run lookup = %v", err)
	}
	bindings, err := fixture.store.ListExecutionPackageApprovalBindings(ctx, prepared.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 {
		t.Fatalf("failed approval bindings = %#v", bindings)
	}
}

func TestPrepareRejectsExpectedHashMismatchWithoutPublishingPackage(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageFixture(t, ctx)
	service, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	input := fixtureInput(t, fixture)
	input.ExecutionSpec.ExpectedSHA256 = strings.Repeat("0", 64)
	if _, err := service.Prepare(ctx, input); !errors.Is(err, ErrInvalidPackageInput) {
		t.Fatalf("Prepare error = %v, want exact hash rejection", err)
	}
	var count int
	if err := fixture.store.DB().QueryRow(`SELECT COUNT(*) FROM execution_packages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("execution package rows after rejected Prepare = %d", count)
	}
}

func newPackageFixture(t *testing.T, ctx context.Context) packageFixture {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture := packageFixture{store: store}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
			RepoTarget: "relay", LocalPath: "/repo", ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		}); err != nil {
			return err
		}
		vault, err := tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{VaultID: workflowstore.NewSourceVaultID(), RepoTarget: "relay", RelativePath: "repositories/relay.git"})
		if err != nil {
			return err
		}
		closureID := workflowstore.NewSourceVaultClosureID()
		acquired, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{VaultRowID: vault.ID, ClosureID: closureID, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40), RefName: "refs/relay/closures/" + closureID, StartedAt: "2026-07-18T00:00:00.000000000Z"})
		if err != nil {
			return err
		}
		fixture.closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{ClosureID: acquired.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting, NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: "2026-07-18T00:00:00.000000000Z"})
		if err != nil {
			return err
		}
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: "project-package", Name: "Package tests"})
		if err != nil {
			return err
		}
		workspace, err := tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: "workspace-package", ProjectRowID: project.ID, FeatureSlug: "package-feature"})
		if err != nil {
			return err
		}
		authority, err := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: "authority-package", WorkspaceRowID: workspace.ID, RevisionNumber: 1, SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true}})
		if err != nil {
			return err
		}
		if _, err := tx.SetFeatureWorkspaceAuthorityRevision(ctx, authority.ID, workspace.WorkspaceID, workspace.Version); err != nil {
			return err
		}
		ticket, err := tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "P5-T2", WorkspaceRowID: workspace.ID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		revision, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "relay", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID, SourcePath: "tickets/p5-t2.delivery-ticket.json", Goal: "Prepare the package.", Context: "The exact selected ticket is the package input.", TransitionApplicability: "not_required"})
		if err != nil {
			return err
		}
		if _, err := tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revision.ID); err != nil {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{ApprovalID: "approval-package", RevisionRowID: revision.ID, ApprovalKind: "delivery", ApprovalState: "approved", Rationale: "Approved exact ticket.", SourceClosureRowID: fixture.closure.ID, AuthorityRevisionRowID: sql.NullInt64{Int64: authority.ID, Valid: true}})
		if err != nil {
			return err
		}
		fixture.selection, err = tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{SelectionID: "selection-package", WorkspaceRowID: workspace.ID, State: "active", Rationale: "Select the exact package input.", SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true}})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{SelectionRowID: fixture.selection.ID, Sequence: 1, RevisionRowID: revision.ID, ApprovalRowID: approval.ID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func fixtureInput(t *testing.T, fixture packageFixture) PrepareInput {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join("..", "..", "speccompiler", "testdata", "valid-v2.execution-spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	bytes = []byte(strings.ReplaceAll(string(bytes), "compiler-v2-fixture", "package-feature"))
	brief := []byte("# Ticket Design Brief\n\n## Ticket Identity\n\n## Context\n\n## Design\n\n## Implementation Notes\n\n## Validation\n")
	return PrepareInput{
		SelectionID:        "selection-package",
		TicketDesignBriefs: []ArtifactInput{{DisplayName: "package-feature.ticket-P5-T2.r1.design-brief.md", ExpectedSHA256: sha256Hex(brief), Bytes: brief}},
		ExecutionSpec:      ArtifactInput{DisplayName: "package-feature.execution-spec.json", ExpectedSHA256: sha256Hex(bytes), Bytes: bytes},
	}
}
