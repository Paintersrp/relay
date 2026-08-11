package packages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/sourcevault"
	"relay/internal/speccompiler"
	workflow "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

const packageOperations = `{"schema_version":"1.0","feature_slug":"checkout","repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","coverage":"complete","operations":[{"path":"internal/example.go","operation":"create","implementation":{"content":"package example\n"}}]}`

// packageTicketDocument is a valid Delivery Ticket v2 source document bound to
// the package fixture's selected revision. Its exact bytes are the sole ticket
// authority; no Brief participates.
func packageTicketDocument(baseCommit string) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"%s","goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"required_invariants":["Packages must bind the exact approved Ticket."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":"internal/app/packages","obligation":"Preserve the selected package basis.","prerequisites":[]}],"proof_obligations":["Prove package preparation binds the approved Ticket."],"validation_commands":[{"working_directory":"","command":"go test ./internal/app/packages","expected":"all tests pass"}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["All tests pass."]}`, baseCommit))
}

// packageTicketDocumentWithDependency returns the canonical approved Delivery
// Ticket bytes with one completed depends-on entry (P2-T1 revision 1), matching
// the dependency rows seeded by the completed-dependency authority test.
func packageTicketDocumentWithDependency(baseCommit string) []byte {
	return []byte(strings.Replace(string(packageTicketDocument(baseCommit)), `"depends_on":[]`, `"depends_on":[{"ticket_id":"P2-T1","revision":1}]`, 1))
}

type packageServiceFixture struct {
	store          *workflow.Store
	service        *Service
	reader         *packageSourceVaultReader
	selectionID    string
	operations     ArtifactInput
	ticketDocument []byte
	ticketSHA256   string
	sourcePath     string
	baseCommit     string
	closureID      string
	workspaceID    string
	ticketID       int64
	revisionID     int64
	approvalID     int64
	selectionRowID int64
	authorityRowID int64
}

func TestSelectedPackageOperationsDigestPartsBindTheOptionalIdentity(t *testing.T) {
	absent := selectedPackageOperationsDigestParts(nil)
	if !reflect.DeepEqual(absent, []string{"operations absent"}) {
		t.Fatalf("absent operations digest parts = %#v", absent)
	}

	operations := &validatedOperations{
		input:    ArtifactInput{DisplayName: "checkout.ticket-P2-T2.r1.deterministic-operations.json"},
		sha256:   strings.Repeat("a", 64),
		document: &speccompiler.DeterministicOperationsDocument{Coverage: "complete"},
	}
	present := selectedPackageOperationsDigestParts(operations)
	want := []string{"operations present", operations.input.DisplayName, operations.sha256, "complete"}
	if !reflect.DeepEqual(present, want) {
		t.Fatalf("present operations digest parts = %#v, want %#v", present, want)
	}
	if compoundSHA256(append([]string{"selected-package-v4", "basis"}, absent...)...) == compoundSHA256(append([]string{"selected-package-v4", "basis"}, present...)...) {
		t.Fatal("absent and present operations package digests matched")
	}

	changedName := *operations
	changedName.input.DisplayName = "checkout.ticket-P2-T2.r1.other-operations.json"
	if compoundSHA256(append([]string{"selected-package-v4", "basis"}, present...)...) == compoundSHA256(append([]string{"selected-package-v4", "basis"}, selectedPackageOperationsDigestParts(&changedName)...)...) {
		t.Fatal("changing the operations filename did not change the package digest")
	}
}

func TestServicePrepareAndGetTicketOnlyPackage(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)

	if prepared.Package.DeterministicOperationsSha256.Valid || prepared.Package.DeterministicOperationsCoverage.Valid {
		t.Fatalf("ticket-only package unexpectedly populated operations fields")
	}
	members, err := fixture.store.ListExecutionPackageMembers(context.Background(), prepared.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Fatalf("package members = %d, want 1", len(members))
	}
	if members[0].MemberSha256 != fixture.ticketSHA256 {
		t.Fatalf("package member digest = %q, want exact Ticket source digest %q", members[0].MemberSha256, fixture.ticketSHA256)
	}
	if prepared.Ticket.TicketID != "P2-T2" || prepared.TicketRevision.RevisionNumber != 1 || prepared.TicketDocument.SHA256 != fixture.ticketSHA256 {
		t.Fatalf("prepared Ticket basis = %#v", prepared)
	}
	if len(prepared.TicketProjection.ValidationCommands) != 1 || prepared.TicketProjection.ValidationCommands[0].Command != "go test ./internal/app/packages" {
		t.Fatalf("prepared Ticket projection = %#v", prepared.TicketProjection)
	}

	detail, err := fixture.service.Get(context.Background(), prepared.Package.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TicketDocument.SHA256 != fixture.ticketSHA256 || detail.DeterministicOperations != nil || detail.Run != nil {
		t.Fatalf("ticket-only package detail = %#v", detail)
	}
	assertCount(t, fixture.store.DB(), "execution_package_approvals", 0)
	assertCount(t, fixture.store.DB(), "runs", 0)
}

func TestServicePrepareAndGetPackageWithOperations(t *testing.T) {
	ticketOnly := newPackageServiceFixture(t)
	ticketPackage := preparePackage(t, ticketOnly, false)

	withOperations := newPackageServiceFixture(t)
	prepared := preparePackage(t, withOperations, true)
	if prepared.Operations == nil {
		t.Fatal("operations artifact was not returned from Prepare")
	}
	if prepared.Operations.DisplayName != withOperations.operations.DisplayName || prepared.Operations.SHA256 != withOperations.operations.ExpectedSHA256 {
		t.Fatalf("prepared operations artifact = %#v", prepared.Operations)
	}
	assertPackageFile(t, withOperations, *prepared.Operations, withOperations.operations.Bytes)
	if prepared.Package.DeterministicOperationsCoverage.String != "complete" || !prepared.Package.DeterministicOperationsCoverage.Valid {
		t.Fatalf("operations coverage = %#v", prepared.Package.DeterministicOperationsCoverage)
	}
	if prepared.Package.PackageSha256 == ticketPackage.Package.PackageSha256 {
		t.Fatal("operations package reused the Ticket-only package digest")
	}

	detail, err := withOperations.service.Get(context.Background(), prepared.Package.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.DeterministicOperations == nil || detail.DeterministicOperations.DisplayName != withOperations.operations.DisplayName || detail.DeterministicOperations.SHA256 != withOperations.operations.ExpectedSHA256 {
		t.Fatalf("read-back operations artifact = %#v", detail.DeterministicOperations)
	}
	assertCount(t, withOperations.store.DB(), "runs", 0)

	validated, err := validateInput(withOperations.input(true))
	if err != nil {
		t.Fatal(err)
	}
	changed := *validated.operations
	changed.input.DisplayName = "checkout.ticket-P2-T2.r1.changed-operations.json"
	if compoundSHA256(append([]string{"selected-package-v4", "basis"}, selectedPackageOperationsDigestParts(validated.operations)...)...) == compoundSHA256(append([]string{"selected-package-v4", "basis"}, selectedPackageOperationsDigestParts(&changed)...)...) {
		t.Fatal("changing the operations filename component did not change the digest")
	}
}

func TestServicePrepareAndApproveRejectBasisDrift(t *testing.T) {
	ctx := context.Background()

	// Ticket source drift: mutated selected Ticket bytes must reject approval
	// revalidation with ErrPackageBasisChanged.
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	mutated := []byte(strings.Replace(string(fixture.ticketDocument), `"goal":"Package the selected ticket."`, `"goal":"Different goal."`, 1))
	fixture.service.setSourceVaults(newPackageSourceVaultReader(fixture.sourcePath, mutated))
	if _, err := fixture.service.Approve(ctx, ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve stale basis"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("approval after Ticket source drift error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, fixture.store.DB(), "execution_package_approvals", 0)
	assertCount(t, fixture.store.DB(), "runs", 0)

	// Repository target drift: a changed configured branch must reject
	// preparation on a fresh fixture.
	fixture2 := newPackageServiceFixture(t)
	if _, err := fixture2.store.DB().Exec(`UPDATE repository_targets SET configured_branch_ref = 'refs/heads/other' WHERE repo_target = 'relay'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture2.service.Prepare(ctx, fixture2.input(false)); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("prepare after repository target drift error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, fixture2.store.DB(), "execution_packages", 0)

	// Approval of an already-prepared package whose repository target drifts
	// must fail with ErrPackageBasisChanged and write nothing.
	fixture3 := newPackageServiceFixture(t)
	prepared3 := preparePackage(t, fixture3, false)
	if _, err := fixture3.store.DB().Exec(`UPDATE repository_targets SET configured_branch_ref = 'refs/heads/other' WHERE repo_target = 'relay'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture3.service.Approve(ctx, ApproveInput{PackageID: prepared3.Package.PackageID, ExpectedPackageSha256: prepared3.Package.PackageSha256, OperatorConfirmationEvidence: "approve stale basis"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("approval after basis drift error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, fixture3.store.DB(), "execution_package_approvals", 0)
	assertCount(t, fixture3.store.DB(), "runs", 0)
}

func TestServicePackageReadbackRejectsChangedOrMissingArtifacts(t *testing.T) {
	tests := []struct {
		name       string
		operations bool
		mutate     func(t *testing.T, fixture *packageServiceFixture, packageID string)
	}{
		{
			name: "changed operations bytes",
			mutate: func(t *testing.T, fixture *packageServiceFixture, packageID string) {
				path := filepath.Join(fixture.store.ArtifactStore().Root(), "packages", packageID, fixture.operations.DisplayName)
				if err := os.WriteFile(path, []byte("mutated operations"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "missing operations",
			mutate: func(t *testing.T, fixture *packageServiceFixture, packageID string) {
				if err := os.Remove(filepath.Join(fixture.store.ArtifactStore().Root(), "packages", packageID, fixture.operations.DisplayName)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "empty operations is not absence",
			operations: true,
			mutate: func(t *testing.T, fixture *packageServiceFixture, packageID string) {
				path := filepath.Join(fixture.store.ArtifactStore().Root(), "packages", packageID, fixture.operations.DisplayName)
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPackageServiceFixture(t)
			prepared := preparePackage(t, fixture, true)
			test.mutate(t, fixture, prepared.Package.PackageID)
			if _, err := fixture.service.Get(context.Background(), prepared.Package.PackageID); !errors.Is(err, ErrPackageBasisChanged) {
				t.Fatalf("read-back error = %v, want ErrPackageBasisChanged", err)
			}
		})
	}
}

func TestServiceGetRejectsChangedTicketSource(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	// Swap the source vault reader to serve mutated Ticket bytes; Get must
	// reject the package because its member digest no longer matches.
	mutated := packageTicketDocument(fixture.baseCommit)
	mutated = append([]byte(" "), mutated...)
	fixture.service.setSourceVaults(newPackageSourceVaultReader(fixture.sourcePath, mutated))
	if _, err := fixture.service.Get(ctx, prepared.Package.PackageID); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("read-back with changed Ticket source error = %v, want ErrPackageBasisChanged", err)
	}
}

func TestServiceApproveAtomicallyConsumesSelectionAndCreatesLinkedSetupReadyRun(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	result, err := fixture.service.Approve(context.Background(), ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunArtifacts == nil || len(result.RunArtifacts) != 0 {
		t.Fatalf("package Run artifacts = %#v, want a concrete empty list", result.RunArtifacts)
	}
	if result.Run.Status != workflow.RunStatusSetupReady || result.Run.RepoTarget != prepared.Package.RepoTarget || result.Run.Branch != prepared.Package.Branch || result.Run.BaseCommit != prepared.Package.BaseCommit || result.Run.PlanRowID.Valid || result.Run.PlanPassRowID.Valid || result.Run.CanonicalSHA256.Valid || !result.Run.ExecutionPackageRowID.Valid || !result.Run.PackageApprovalRowID.Valid {
		t.Fatalf("linked package Run = %#v", result.Run)
	}
	if result.Run.ExecutionPackageRowID.Int64 != prepared.Package.ID || result.PackageApproval.ID != result.Run.PackageApprovalRowID.Int64 {
		t.Fatalf("package Run links = package %d / approval %d / run %#v", prepared.Package.ID, result.PackageApproval.ID, result.Run)
	}
	if artifacts, err := fixture.store.ListArtifactsByRun(context.Background(), result.Run.ID); err != nil {
		t.Fatal(err)
	} else if len(artifacts) != 0 {
		t.Fatalf("stored package Run artifacts = %d, want 0", len(artifacts))
	}
	selection, err := fixture.store.GetDeliveryTicketSelectionBySelectionID(context.Background(), fixture.selectionID)
	if err != nil {
		t.Fatal(err)
	}
	if selection.State != "consumed" {
		t.Fatalf("selection state = %q, want consumed", selection.State)
	}
	assertCount(t, fixture.store.DB(), "execution_package_approvals", 1)
	assertCount(t, fixture.store.DB(), "execution_package_approval_bindings", 1)
	assertCount(t, fixture.store.DB(), "runs", 1)

	if _, err := fixture.service.Approve(context.Background(), ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "duplicate"}); err == nil {
		t.Fatal("second package approval succeeded")
	}
	assertCount(t, fixture.store.DB(), "execution_package_approvals", 1)
	assertCount(t, fixture.store.DB(), "runs", 1)
}

func TestServiceLoadApprovedAuthorityForRunTicketOnly(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.service.LoadApprovedAuthorityForRun(context.Background(), approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Run.ID != approved.Run.ID || loaded.Package.ID != prepared.Package.ID || loaded.PackageApproval.ID != approved.PackageApproval.ID {
		t.Fatalf("loaded package linkage = %#v", loaded)
	}
	if loaded.TicketRevision.ID == 0 || len(loaded.TicketMembers) != 1 || loaded.TicketMembers[0].Sequence != 1 || loaded.TicketApproval.ApprovalID != "approval-package-1" {
		t.Fatalf("loaded Ticket projection = %#v", loaded)
	}
	if len(loaded.AuthorityLayers) != 1 || string(loaded.AuthorityLayers[0].Bytes) != "authority" || loaded.AuthorityLayers[0].Kind != "requirements" {
		t.Fatalf("loaded authority layers = %#v", loaded.AuthorityLayers)
	}
	if loaded.DeliveryTicket.SHA256 != fixture.ticketSHA256 || len(loaded.TicketProjection.ValidationCommands) != 1 || loaded.TicketProjection.ValidationCommands[0].Command != "go test ./internal/app/packages" {
		t.Fatalf("loaded Ticket bytes or projection = %#v", loaded)
	}
	if string(loaded.DeliveryTicket.Bytes) != string(fixture.ticketDocument) {
		t.Fatalf("loaded Ticket exact bytes were not preserved")
	}
	if loaded.DeterministicOperations != nil {
		t.Fatalf("ticket-only authority unexpectedly has operations")
	}
}

func TestServiceLoadApprovedAuthorityForRunWithOperations(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, true)
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.service.LoadApprovedAuthorityForRun(context.Background(), approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeterministicOperations == nil || loaded.DeterministicOperations.SHA256 != fixture.operations.ExpectedSHA256 || loaded.DeterministicOperations.Coverage != "complete" || loaded.DeterministicOperations.Document == nil {
		t.Fatalf("loaded operations = %#v", loaded.DeterministicOperations)
	}
	if string(loaded.DeterministicOperations.Bytes) != string(fixture.operations.Bytes) || loaded.DeliveryTicket.SHA256 != fixture.ticketSHA256 {
		t.Fatal("loader did not preserve exact package bytes")
	}
}

func TestServiceApproveWrongPackageSHAIsAtomic(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	wrongSHA := strings.Repeat("f", 64)
	if wrongSHA == prepared.Package.PackageSha256 {
		wrongSHA = strings.Repeat("e", 64)
	}
	if _, err := fixture.service.Approve(context.Background(), ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: wrongSHA, OperatorConfirmationEvidence: "wrong digest"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("wrong expected package SHA error = %v, want ErrPackageBasisChanged", err)
	}
	selection, err := fixture.store.GetDeliveryTicketSelectionBySelectionID(context.Background(), fixture.selectionID)
	if err != nil {
		t.Fatal(err)
	}
	if selection.State != "active" {
		t.Fatalf("selection state after rejected approval = %q, want active", selection.State)
	}
	assertCount(t, fixture.store.DB(), "execution_package_approvals", 0)
	assertCount(t, fixture.store.DB(), "execution_package_approval_bindings", 0)
	assertCount(t, fixture.store.DB(), "runs", 0)
}

func newPackageServiceFixture(t *testing.T) *packageServiceFixture {
	t.Helper()
	store := workflowfixture.Open(t, workflow.Open)
	ctx := context.Background()

	baseCommit := strings.Repeat("a", 40)
	treeOID := strings.Repeat("b", 40)
	sourcePath := "tickets/checkout.ticket-P2-T2.r1.delivery-ticket.json"
	authorityBytes := []byte("authority")
	authoritySHA := sha256Hex(authorityBytes)
	authorityPath := filepath.Join(store.ArtifactStore().Root(), "plans", "checkout", "requirements.json")
	if err := os.MkdirAll(filepath.Dir(authorityPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorityPath, authorityBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	ticketDocument := packageTicketDocument(baseCommit)
	ticketSHA := sha256Hex(ticketDocument)
	operationsName := "checkout.ticket-P2-T2.r1.deterministic-operations.json"
	operationsBytes := packageOperationsWithBaseCommit(baseCommit)
	var projectID, workspaceID, vaultID, closureID, authorityID, planID, ticketID, revisionID, approvalID, selectionRowID int64
	db := store.DB()
	if err := db.QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-package', 'Package') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-package', 'relay', 'vaults/package') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-package', ?, ?, ?, 1, 'refs/relay/closures/closure-package', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z') RETURNING id`, vaultID, baseCommit, treeOID).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-package', ?, 'checkout') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-package', 'checkout', ?) RETURNING id`, projectID, strings.Repeat("c", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-package-authority', 'plan', ?, 'requirements', 'plans/checkout/requirements.json', 'application/json', ?, ?)`, planID, authoritySHA, len(authorityBytes)); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO feature_workspace_authority_revisions (authority_revision_id, workspace_row_id, revision_number, source_closure_row_id) VALUES ('authority-package-1', ?, 1, ?) RETURNING id`, workspaceID, closureID).Scan(&authorityID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO feature_workspace_authority_layers (authority_revision_row_id, layer_kind, sequence, artifact_row_id, artifact_sha256, source_closure_row_id) VALUES (?, 'requirements', 1, (SELECT id FROM artifacts WHERE artifact_id = 'artifact-package-authority'), ?, ?)`, authorityID, authoritySHA, closureID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE feature_workspaces SET current_authority_revision_row_id = ?, version = 2 WHERE id = ? AND version = 1`, authorityID, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO feature_workspace_discovery_adoptions (workspace_row_id, adoption_id, operator_identity, adopted_workspace_version) VALUES (?, 'discovery-adoption-package', 'package-fixture', 2)`, workspaceID); err != nil {
		t.Fatal(err)
	}
	manifestSHA := strings.Repeat("f", 64)
	var discoveryArtifactID, discoveryRevisionID, discoveryPacketID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-package', ?, 'feature-discovery/checkout/closure/manifest.json', ?, 'application/vnd.relay.feature-discovery-closure+json', 1) RETURNING id`, workspaceID, manifestSHA).Scan(&discoveryArtifactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity, settled_destination, continuation_json) VALUES ('discovery-revision-package', ?, 1, ?, 'package-fixture', 'requirements', '{}') RETURNING id`, workspaceID, discoveryArtifactID).Scan(&discoveryRevisionID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES ('discovery-packet-package', ?, ?, 'requirements', ?, ?, 1, 'application/vnd.relay.feature-discovery-closure+json') RETURNING id`, workspaceID, discoveryRevisionID, discoveryArtifactID, manifestSHA).Scan(&discoveryPacketID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = ?, version = 3 WHERE id = ? AND version = 2`, discoveryRevisionID, discoveryPacketID, workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority) VALUES ('P2-T2', ?, 10) RETURNING id`, workspaceID).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, 1, 'relay', 'main', ?, ?, ?, 'Package the selected ticket.', 'Package basis context.', 'not_required') RETURNING id`, ticketID, baseCommit, closureID, sourcePath).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE delivery_tickets SET current_revision_row_id = ? WHERE id = ?`, revisionID, ticketID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO delivery_ticket_revision_members (revision_row_id, sequence, member_kind, member_path, member_text) VALUES (?, 1, 'implementation_obligation', 'internal/app/packages', 'Preserve the selected package basis.')`, revisionID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_ticket_revision_approvals (approval_id, revision_row_id, approval_kind, approval_state, rationale, source_closure_row_id, authority_revision_row_id) VALUES ('approval-package-1', ?, 'delivery', 'approved', 'Approved package basis.', ?, ?) RETURNING id`, revisionID, closureID, authorityID).Scan(&approvalID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_ticket_selections (selection_id, workspace_row_id, state, rationale, source_closure_row_id) VALUES ('selection-package', ?, 'active', 'Select the package ticket.', ?) RETURNING id`, workspaceID, closureID).Scan(&selectionRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO delivery_ticket_selection_members (selection_row_id, sequence, revision_row_id, approval_row_id) VALUES (?, 1, ?, ?)`, selectionRowID, revisionID, approvalID); err != nil {
		t.Fatal(err)
	}

	reader := newPackageSourceVaultReader(sourcePath, ticketDocument)
	service, err := NewServiceWithSourceVaults(store, reader)
	if err != nil {
		t.Fatal(err)
	}

	operations := ArtifactInput{DisplayName: operationsName, Bytes: operationsBytes, ExpectedSHA256: sha256Hex(operationsBytes)}
	return &packageServiceFixture{store: store, service: service, reader: reader, selectionID: "selection-package", operations: operations, ticketDocument: ticketDocument, ticketSHA256: ticketSHA, sourcePath: sourcePath, baseCommit: baseCommit, closureID: "closure-package", workspaceID: "workspace-package", ticketID: ticketID, revisionID: revisionID, approvalID: approvalID, selectionRowID: selectionRowID, authorityRowID: authorityID}
}

func packageOperationsWithBaseCommit(baseCommit string) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","repo_target":"relay","branch":"main","base_commit":"%s","coverage":"complete","operations":[{"path":"internal/example.go","operation":"create","implementation":{"content":"package example\n"}}]}`, baseCommit))
}

type packageSourceVaultReader struct {
	path  string
	bytes []byte
	paths map[string][]byte
	err   error
}

func newPackageSourceVaultReader(path string, bytes []byte) *packageSourceVaultReader {
	return &packageSourceVaultReader{path: path, bytes: bytes}
}

func (r *packageSourceVaultReader) ReadPath(ctx context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	if r.err != nil {
		return sourcevault.ReadPathResult{}, r.err
	}
	if request.Path == r.path {
		return sourcevault.ReadPathResult{ObjectOID: strings.Repeat("d", 40), Bytes: append([]byte(nil), r.bytes...)}, nil
	}
	if bytes, ok := r.paths[request.Path]; ok {
		return sourcevault.ReadPathResult{ObjectOID: strings.Repeat("e", 40), Bytes: append([]byte(nil), bytes...)}, nil
	}
	return sourcevault.ReadPathResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
}

func (r *packageSourceVaultReader) WithErr(err error) *packageSourceVaultReader {
	return &packageSourceVaultReader{path: r.path, bytes: r.bytes, err: err}
}

// setSourceVaults is an unexported test helper that swaps the source-vault
// reader on the service. It is intentionally not an exported API surface.
func (s *Service) setSourceVaults(reader SourceVaultReader) {
	s.sourceVaults = reader
}

func (f *packageServiceFixture) input(withOperations bool) PrepareInput {
	input := PrepareInput{SelectionID: f.selectionID}
	if withOperations {
		operations := f.operations
		input.DeterministicOperations = &operations
	}
	return input
}

func preparePackage(t *testing.T, fixture *packageServiceFixture, withOperations bool) PrepareResult {
	t.Helper()
	prepared, err := fixture.service.Prepare(context.Background(), fixture.input(withOperations))
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func assertPackageFile(t *testing.T, fixture *packageServiceFixture, artifact PackageArtifact, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || artifact.SHA256 != sha256Hex(want) || artifact.SizeBytes != int64(len(want)) {
		t.Fatalf("package artifact = bytes %q, metadata %#v", got, artifact)
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s count = %d, want %d", table, got, want)
	}
}

func TestServicePrepareRejectsStaleFeatureClosure(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	if _, err := fixture.store.DB().Exec(`UPDATE feature_workspaces SET current_discovery_closure_packet_row_id = NULL, version = version + 1 WHERE id = ?`, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Prepare(context.Background(), fixture.input(false)); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("stale closure prepare error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, fixture.store.DB(), "execution_packages", 0)
}
