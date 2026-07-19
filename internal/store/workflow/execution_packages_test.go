package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	relaydb "relay/internal/db"
	workflowgenerated "relay/internal/store/workflowgenerated"

	"github.com/pressly/goose/v3"
)

func TestExecutionPackageConsumptionPersistsExactImmutableBasis(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedExecutionPackageInputs(t, ctx, store)

	var executionPackage workflowgenerated.ExecutionPackage
	var member workflowgenerated.ExecutionPackageMember
	if err := store.WithTx(ctx, func(tx *Tx) error {
		queries := workflowgenerated.New(tx.tx)
		var err error
		executionPackage, err = createExecutionPackage(ctx, queries, seed)
		if err != nil {
			return err
		}
		if _, err := queries.ConsumeDeliveryTicketSelection(ctx, seed.selection.SelectionID); err == nil {
			return errors.New("incomplete package consumed a selection")
		}
		member, err = queries.CreateExecutionPackageMember(ctx, workflowgenerated.CreateExecutionPackageMemberParams{
			PackageRowID:         executionPackage.ID,
			SelectionMemberRowID: seed.selectionMember.ID,
			Sequence:             1,
			RevisionRowID:        seed.revision.ID,
			MemberSha256:         executionPackageHash('4'),
		})
		if err != nil {
			return err
		}
		if _, err := queries.ConsumeDeliveryTicketSelection(ctx, seed.selection.SelectionID); err == nil {
			return errors.New("package without an approval binding consumed a selection")
		}
		if _, err := queries.CreateExecutionPackageApprovalBinding(ctx, workflowgenerated.CreateExecutionPackageApprovalBindingParams{
			PackageRowID:           executionPackage.ID,
			PackageMemberRowID:     member.ID,
			ApprovalRowID:          seed.approval.ID,
			AuthorityRevisionRowID: seed.authority.ID,
			SourceClosureRowID:     seed.closure.ID,
			ApprovalBasisSha256:    executionPackageHash('5'),
		}); err != nil {
			return err
		}
		consumed, err := queries.ConsumeDeliveryTicketSelection(ctx, seed.selection.SelectionID)
		if err != nil {
			return err
		}
		if consumed.State != "consumed" {
			return errors.New("completed execution package did not consume its selection")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	queries := workflowgenerated.New(store.DB())
	stored, err := queries.GetExecutionPackageByPackageID(ctx, executionPackage.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SelectionRowID != seed.selection.ID || stored.AuthorityRevisionRowID != seed.authority.ID ||
		stored.SourceClosureRowID != seed.closure.ID || stored.ExecutionSpecSha256 != executionPackageHash('3') ||
		stored.DesignBriefSha256 != executionPackageHash('2') {
		t.Fatalf("stored execution package basis = %#v", stored)
	}
	bindings, err := queries.ListExecutionPackageApprovalBindings(ctx, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].ApprovalRowID != seed.approval.ID || bindings[0].PackageMemberRowID != member.ID {
		t.Fatalf("stored package approval bindings = %#v", bindings)
	}

	if _, err := store.DB().Exec(`UPDATE execution_packages SET package_sha256 = ? WHERE id = ?`, executionPackageHash('f'), stored.ID); err == nil {
		t.Fatal("execution package was mutable")
	}
	if _, err := store.DB().Exec(`UPDATE execution_package_members SET sequence = 2 WHERE id = ?`, member.ID); err == nil {
		t.Fatal("execution package member was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM execution_package_approval_bindings WHERE id = ?`, bindings[0].ID); err == nil {
		t.Fatal("execution package approval binding was deletable")
	}

	if _, err := store.DB().Exec(`
INSERT INTO runs (
    run_id, feature_slug, repo_target, status, branch, base_commit, canonical_sha256, execution_package_row_id
)
VALUES ('run-orphan-package', 'package-test', 'relay', 'created', 'main', ?, ?, 999999)`, seed.closure.CommitOID, executionPackageHash('6')); err == nil {
		t.Fatal("Run accepted an orphan execution package reference")
	}

	if _, err := store.DB().Exec(`
INSERT INTO runs (
    run_id, feature_slug, repo_target, status, branch, base_commit, canonical_sha256
)
VALUES ('run-package-linked', 'package-test', 'relay', 'created', 'main', ?, ?)`, seed.closure.CommitOID, executionPackageHash('6')); err != nil {
		t.Fatal(err)
	}
	linked, err := queries.LinkRunToExecutionPackage(ctx, workflowgenerated.LinkRunToExecutionPackageParams{
		ExecutionPackageRowID: sql.NullInt64{Int64: stored.ID, Valid: true},
		RunID:                 "run-package-linked",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !linked.ExecutionPackageRowID.Valid || linked.ExecutionPackageRowID.Int64 != stored.ID {
		t.Fatalf("linked Run package row = %#v", linked.ExecutionPackageRowID)
	}
	if _, err := store.DB().Exec(`
INSERT INTO runs (
    run_id, feature_slug, repo_target, status, branch, base_commit, canonical_sha256
)
VALUES ('run-package-duplicate', 'package-test', 'relay', 'created', 'main', ?, ?)`, seed.closure.CommitOID, executionPackageHash('7')); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.LinkRunToExecutionPackage(ctx, workflowgenerated.LinkRunToExecutionPackageParams{
		ExecutionPackageRowID: sql.NullInt64{Int64: stored.ID, Valid: true},
		RunID:                 "run-package-duplicate",
	}); err == nil {
		t.Fatal("multiple Runs linked to one immutable execution package")
	}
}

func TestExecutionPackageConsumptionRollsBackAsOneTransaction(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedExecutionPackageInputs(t, ctx, store)

	errInjected := errors.New("injected package transaction rollback")
	err := store.WithTx(ctx, func(tx *Tx) error {
		queries := workflowgenerated.New(tx.tx)
		executionPackage, err := createExecutionPackage(ctx, queries, seed)
		if err != nil {
			return err
		}
		member, err := queries.CreateExecutionPackageMember(ctx, workflowgenerated.CreateExecutionPackageMemberParams{
			PackageRowID:         executionPackage.ID,
			SelectionMemberRowID: seed.selectionMember.ID,
			Sequence:             1,
			RevisionRowID:        seed.revision.ID,
			MemberSha256:         executionPackageHash('4'),
		})
		if err != nil {
			return err
		}
		if _, err := queries.CreateExecutionPackageApprovalBinding(ctx, workflowgenerated.CreateExecutionPackageApprovalBindingParams{
			PackageRowID:           executionPackage.ID,
			PackageMemberRowID:     member.ID,
			ApprovalRowID:          seed.approval.ID,
			AuthorityRevisionRowID: seed.authority.ID,
			SourceClosureRowID:     seed.closure.ID,
			ApprovalBasisSha256:    executionPackageHash('5'),
		}); err != nil {
			return err
		}
		if _, err := queries.ConsumeDeliveryTicketSelection(ctx, seed.selection.SelectionID); err != nil {
			return err
		}
		return errInjected
	})
	if !errors.Is(err, errInjected) {
		t.Fatalf("package transaction error = %v, want injected rollback", err)
	}
	assertWorkflowCount(t, store.DB(), "execution_packages", 0)
	assertWorkflowCount(t, store.DB(), "execution_package_members", 0)
	assertWorkflowCount(t, store.DB(), "execution_package_approval_bindings", 0)
	var selectionState string
	if err := store.DB().QueryRow(`SELECT state FROM delivery_ticket_selections WHERE id = ?`, seed.selection.ID).Scan(&selectionState); err != nil {
		t.Fatal(err)
	}
	if selectionState != "active" {
		t.Fatalf("rolled back selection state = %q, want active", selectionState)
	}
}

func TestRepositoryBranchMutationLeaseAllowsOneActiveLeaseAcrossStores(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "workflow.sqlite")
	firstStore, err := Open(databasePath, filepath.Join(root, "artifacts-one"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstStore.Close() })
	if _, err := firstStore.DB().Exec(`
INSERT INTO repository_targets (repo_target, local_path)
VALUES ('relay', '/repo')`); err != nil {
		t.Fatal(err)
	}
	secondStore, err := Open(databasePath, filepath.Join(root, "artifacts-two"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })

	start := make(chan struct{})
	results := make(chan error, 2)
	for index, store := range []*Store{firstStore, secondStore} {
		index, store := index, store
		go func() {
			<-start
			_, err := workflowgenerated.New(store.DB()).CreateRepositoryBranchMutationLease(ctx, workflowgenerated.CreateRepositoryBranchMutationLeaseParams{
				LeaseID:             "lease-package-" + string(rune('a'+index)),
				RepoTarget:          "relay",
				Branch:              "main",
				OwnerKind:           "execution_package",
				OwnerIdentity:       "package-concurrency",
				UncertaintyState:    "certain",
				ReconciliationState: "not_required",
			})
			results <- err
		}()
	}
	close(start)
	var successes int
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent active lease successes = %d, want 1", successes)
	}
	assertWorkflowCount(t, firstStore.DB(), "repository_branch_mutation_leases", 1)

	queries := workflowgenerated.New(firstStore.DB())
	active, err := queries.GetActiveRepositoryBranchMutationLease(ctx, workflowgenerated.GetActiveRepositoryBranchMutationLeaseParams{RepoTarget: "relay", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.UpdateRepositoryBranchMutationLeaseFacts(ctx, workflowgenerated.UpdateRepositoryBranchMutationLeaseFactsParams{
		UncertaintyState:        "uncertain",
		UncertaintyReason:       sql.NullString{String: "mutation outcome must be reconciled", Valid: true},
		ReconciliationState:     "in_progress",
		ReconciliationNote:      sql.NullString{String: "checking repository state", Valid: true},
		ReconciliationStartedAt: sql.NullString{String: "2026-07-18T12:00:00.000000000Z", Valid: true},
		LeaseID:                 active.LeaseID,
		State:                   "active",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.ReleaseRepositoryBranchMutationLease(ctx, active.LeaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.CreateRepositoryBranchMutationLease(ctx, workflowgenerated.CreateRepositoryBranchMutationLeaseParams{
		LeaseID:             "lease-package-replacement",
		RepoTarget:          "relay",
		Branch:              "main",
		OwnerKind:           "execution_package",
		OwnerIdentity:       "package-replacement",
		UncertaintyState:    "certain",
		ReconciliationState: "not_required",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionPackageMigrationPreservesHistoricalRuns(t *testing.T) {
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
	if err := goose.UpTo(database, "workflow_migrations", 13); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO repository_targets (repo_target, local_path) VALUES ('relay', '/repo')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO runs (run_id, feature_slug, repo_target, status, branch, base_commit, canonical_sha256)
VALUES ('run-historical', 'historical', 'relay', 'created', 'main', ?, ?)`, strings.Repeat("a", 40), executionPackageHash('9')); err != nil {
		t.Fatal(err)
	}
	if err := relaydb.AutoMigrateWorkflow(database); err != nil {
		t.Fatal(err)
	}
	var runCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM runs WHERE run_id = 'run-historical' AND execution_package_row_id IS NULL`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("historical Run rows after upgrade = %d, want 1", runCount)
	}
	if err := goose.DownTo(database, "workflow_migrations", 13); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM runs WHERE run_id = 'run-historical'`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("historical Run rows after rollback = %d, want 1", runCount)
	}
}

type executionPackageSeed struct {
	closure         SourceVaultClosure
	workspace       FeatureWorkspace
	authority       FeatureWorkspaceAuthorityRevision
	ticket          DeliveryTicket
	revision        DeliveryTicketRevision
	approval        DeliveryTicketRevisionApproval
	selection       DeliveryTicketSelection
	selectionMember DeliveryTicketSelectionMember
}

func seedExecutionPackageInputs(t *testing.T, ctx context.Context, store *Store) executionPackageSeed {
	t.Helper()
	_, closure := seedReadySourceVaultClosure(t, ctx, store)
	workspace := seedDeliveryTicketWorkspace(t, ctx, store)
	seed := executionPackageSeed{closure: closure, workspace: workspace}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		seed.authority, err = tx.CreateFeatureWorkspaceAuthorityRevision(ctx, CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: "authority-package-1",
			WorkspaceRowID:      workspace.ID,
			RevisionNumber:      1,
			SourceClosureRowID:  sql.NullInt64{Int64: closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		seed.workspace, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, seed.authority.ID, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return err
		}
		seed.ticket, err = tx.CreateDeliveryTicket(ctx, CreateDeliveryTicketParams{
			TicketID: "P5-T1", WorkspaceRowID: workspace.ID, ExternalPriority: 60,
		})
		if err != nil {
			return err
		}
		seed.revision, err = tx.CreateDeliveryTicketRevision(ctx, CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID:     seed.ticket.ID,
			RevisionNumber:          1,
			RepoTarget:              "relay",
			Branch:                  "main",
			BaseCommit:              closure.CommitOID,
			SourceClosureRowID:      closure.ID,
			SourcePath:              "tickets/p5-t1.delivery-ticket.json",
			Goal:                    "Persist immutable execution package inputs.",
			Context:                 "Selection, approval, source, and authority are exact facts.",
			TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		if _, err := tx.SetDeliveryTicketCurrentRevision(ctx, seed.ticket.TicketID, seed.revision.ID); err != nil {
			return err
		}
		seed.approval, err = tx.CreateDeliveryTicketRevisionApproval(ctx, CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID:             "approval-package-1",
			RevisionRowID:          seed.revision.ID,
			ApprovalKind:           "delivery",
			ApprovalState:          "approved",
			Rationale:              "Approved against the exact package authority.",
			SourceClosureRowID:     closure.ID,
			AuthorityRevisionRowID: sql.NullInt64{Int64: seed.authority.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		seed.selection, err = tx.CreateDeliveryTicketSelection(ctx, CreateDeliveryTicketSelectionParams{
			SelectionID:        "selection-package-1",
			WorkspaceRowID:     workspace.ID,
			State:              "active",
			Rationale:          "Reserve the exact approved ticket for package composition.",
			SourceClosureRowID: sql.NullInt64{Int64: closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		seed.selectionMember, err = tx.CreateDeliveryTicketSelectionMember(ctx, CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: seed.selection.ID,
			Sequence:       1,
			RevisionRowID:  seed.revision.ID,
			ApprovalRowID:  seed.approval.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return seed
}

func createExecutionPackage(ctx context.Context, queries *workflowgenerated.Queries, seed executionPackageSeed) (workflowgenerated.ExecutionPackage, error) {
	return queries.CreateExecutionPackage(ctx, workflowgenerated.CreateExecutionPackageParams{
		PackageID:              "package-p5-t1",
		SelectionRowID:         seed.selection.ID,
		WorkspaceRowID:         seed.workspace.ID,
		RepoTarget:             "relay",
		Branch:                 "main",
		BaseCommit:             seed.closure.CommitOID,
		SourceClosureRowID:     seed.closure.ID,
		AuthorityRevisionRowID: seed.authority.ID,
		PackageSha256:          executionPackageHash('1'),
		AuthoritySha256:        executionPackageHash('a'),
		SourceSha256:           executionPackageHash('b'),
		DesignBriefSha256:      executionPackageHash('2'),
		ExecutionSpecSha256:    executionPackageHash('3'),
	})
}

func executionPackageHash(char rune) string {
	return strings.Repeat(string(char), 64)
}
