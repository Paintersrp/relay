package executor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	executionpackages "relay/internal/app/packages"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
)

const executionAssignmentOperations = `{"schema_version":"1.0","feature_slug":"checkout","repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","coverage":"complete","operations":[{"path":"internal/example.go","operation":"create","implementation":{"content":"package example\n"}}]}`

type executionAssignmentFixture struct {
	store              *workflowstore.Store
	packages           *executionpackages.Service
	assignments        *ExecutionAssignmentService
	selectionID        string
	packageID          string
	run                workflowstore.Run
	brief              executionpackages.ArtifactInput
	operations         executionpackages.ArtifactInput
	assignmentFilename string
}

func TestPrepareExecutionAssignmentPersistsBriefOnlyArtifact(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	result := prepareExecutionAssignment(t, fixture)

	artifacts := listRunArtifacts(t, fixture)
	if len(artifacts) != 1 {
		t.Fatalf("Run artifacts = %d, want 1", len(artifacts))
	}
	artifact := artifacts[0]
	if artifact.Kind != executionAssignmentKind || artifact.MediaType != executionAssignmentMediaType {
		t.Fatalf("assignment artifact identity = %#v", artifact)
	}
	if artifact.RelativePath != filepath.ToSlash(filepath.Join("runs", fixture.run.RunID, fixture.assignmentFilename)) {
		t.Fatalf("assignment artifact path = %q", artifact.RelativePath)
	}
	managed := readManagedAssignment(t, fixture)
	if !reflect.DeepEqual(managed, result.Bytes) {
		t.Fatal("managed assignment bytes differ from returned bytes")
	}
	if artifact.SHA256 != sha256Hex(result.Bytes) || artifact.SizeBytes != int64(len(result.Bytes)) {
		t.Fatalf("assignment metadata = %#v", artifact)
	}
	var decoded map[string]any
	if err := json.Unmarshal(managed, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded["deterministic_operations"]; !reflect.DeepEqual(got, map[string]any{"presence": "absent"}) {
		t.Fatalf("operations = %#v, want explicit absence", got)
	}
}

func TestLoadExecutionAssignmentRequiresAndVerifiesExistingArtifact(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	if _, err := fixture.assignments.LoadExecutionAssignment(context.Background(), fixture.run.RunID); err == nil {
		t.Fatal("missing assignment was accepted")
	}
	prepared := prepareExecutionAssignment(t, fixture)
	loaded, err := fixture.assignments.LoadExecutionAssignment(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Artifact.ID != prepared.Artifact.ID || !reflect.DeepEqual(loaded.Bytes, prepared.Bytes) {
		t.Fatalf("loaded assignment = %#v, want %#v", loaded, prepared)
	}
}

func TestPrepareExecutionAssignmentPersistsOperationsIdentity(t *testing.T) {
	for _, coverage := range []string{"partial", "complete"} {
		t.Run(coverage, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, true, coverage)
			result := prepareExecutionAssignment(t, fixture)
			var decoded ExecutionAssignment
			if err := json.Unmarshal(result.Bytes, &decoded); err != nil {
				t.Fatal(err)
			}
			operations := decoded.DeterministicOperations
			if operations.Presence != "present" || operations.DisplayName != fixture.operations.DisplayName || operations.RelativePath != "packages/"+fixture.packageID+"/"+fixture.operations.DisplayName || operations.MediaType != "application/json" || operations.SHA256 != fixture.operations.ExpectedSHA256 || operations.Coverage != coverage {
				t.Fatalf("stored operations = %#v", operations)
			}
		})
	}
}

func TestPrepareExecutionAssignmentIsIdempotent(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	first := prepareExecutionAssignment(t, fixture)
	second, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Artifact.ID != first.Artifact.ID || second.Artifact.ArtifactID != first.Artifact.ArtifactID {
		t.Fatalf("second artifact = %#v, first = %#v", second.Artifact, first.Artifact)
	}
	if !reflect.DeepEqual(second.Bytes, first.Bytes) {
		t.Fatal("repeated preparation changed assignment bytes")
	}
	if got := len(listRunArtifacts(t, fixture)); got != 1 {
		t.Fatalf("Run artifacts = %d, want 1", got)
	}
	entries, err := os.ReadDir(filepath.Dir(assignmentPath(fixture)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != fixture.assignmentFilename {
		t.Fatalf("managed assignment directory entries = %#v", entries)
	}
}

func TestPrepareExecutionAssignmentRejectsTamperedOrMissingBytes(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{name: "tampered", mutate: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("tampered assignment\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, false, "")
			first := prepareExecutionAssignment(t, fixture)
			path := assignmentPath(fixture)
			test.mutate(t, path)
			_, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID)
			if !errors.Is(err, ErrExecutionAssignmentConflict) {
				t.Fatalf("repeated preparation error = %v, want conflict", err)
			}
			if got := len(listRunArtifacts(t, fixture)); got != 1 {
				t.Fatalf("Run artifacts = %d, want 1", got)
			}
			if _, statErr := os.Stat(path); test.name == "missing" && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("missing assignment was recreated: %v", statErr)
			}
			if test.name == "tampered" {
				managed, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if reflect.DeepEqual(managed, first.Bytes) {
					t.Fatal("tampered assignment was repaired")
				}
			}
		})
	}
}

func TestPrepareExecutionAssignmentRejectsConflictingMetadata(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
		value any
	}{
		{name: "media type", query: "UPDATE artifacts SET media_type = ? WHERE artifact_id = ?", value: "text/plain"},
		{name: "SHA", query: "UPDATE artifacts SET sha256 = ? WHERE artifact_id = ?", value: strings.Repeat("0", 64)},
		{name: "relative path", query: "UPDATE artifacts SET relative_path = ? WHERE artifact_id = ?", value: "runs/" + "run-package" + "/other-assignment.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, false, "")
			result := prepareExecutionAssignment(t, fixture)
			if _, err := fixture.store.DB().Exec(test.query, test.value, result.Artifact.ArtifactID); err != nil {
				t.Fatal(err)
			}
			_, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID)
			if !errors.Is(err, ErrExecutionAssignmentConflict) {
				t.Fatalf("repeated preparation error = %v, want conflict", err)
			}
			if got := len(listRunArtifacts(t, fixture)); got != 1 {
				t.Fatalf("Run artifacts = %d, want 1", got)
			}
		})
	}
}

func TestPrepareExecutionAssignmentRequiresSetupReadyForFirstGeneration(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	transitionRun(t, fixture, workflowstore.RunStatusExecuting)
	if _, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID); err == nil {
		t.Fatal("assignment generation succeeded for a non-setup_ready Run")
	}
	assertNoAssignment(t, fixture)
}

func TestPrepareExecutionAssignmentReturnsExistingAfterRunAdvancement(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	first := prepareExecutionAssignment(t, fixture)
	transitionRun(t, fixture, workflowstore.RunStatusExecuting)
	second, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Artifact.ID != first.Artifact.ID || !reflect.DeepEqual(second.Bytes, first.Bytes) {
		t.Fatalf("advanced-Run assignment = %#v, want %#v", second, first)
	}
	if current, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID); err != nil {
		t.Fatal(err)
	} else if current.Status != workflowstore.RunStatusExecuting {
		t.Fatalf("Run status = %q, want executing", current.Status)
	}
}

func TestPrepareExecutionAssignmentInvalidAuthorityCreatesNothing(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, fixture *executionAssignmentFixture)
	}{
		{name: "changed Brief bytes", mutate: func(t *testing.T, fixture *executionAssignmentFixture) {
			path := filepath.Join(fixture.store.ArtifactStore().Root(), "packages", fixture.packageID, fixture.brief.DisplayName)
			if err := os.WriteFile(path, []byte("changed Brief"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "changed authority-layer bytes", mutate: func(t *testing.T, fixture *executionAssignmentFixture) {
			path := filepath.Join(fixture.store.ArtifactStore().Root(), "plans", "checkout", "requirements.json")
			if err := os.WriteFile(path, []byte("changed authority"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionAssignmentFixture(t, false, "")
			test.mutate(t, fixture)
			if _, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID); err == nil {
				t.Fatal("invalid approved authority was accepted")
			}
			assertNoAssignment(t, fixture)
		})
	}
}

func TestPrepareExecutionAssignmentHasNoExecutionOrLifecycleSideEffects(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	before := mustRun(t, fixture)
	if _, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID); err != nil {
		t.Fatal(err)
	}
	if after := mustRun(t, fixture); after.Status != before.Status || after.UpdatedAt == "" {
		t.Fatalf("Run changed during assignment generation: before=%#v after=%#v", before, after)
	}
	for _, table := range []string{"execution_attempts", "repository_branch_mutation_leases", "audit_packets", "audit_decisions"} {
		if got := countSideEffectRows(t, fixture, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
	artifacts := listRunArtifacts(t, fixture)
	if len(artifacts) != 1 || artifacts[0].Kind != executionAssignmentKind {
		t.Fatalf("Run side-effect artifacts = %#v", artifacts)
	}
}

func TestExecutionAssignmentUniquenessAllowsUnrelatedRunArtifacts(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	first := prepareExecutionAssignment(t, fixture)
	duplicateErr := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
			ArtifactID:   "artifact-duplicate-assignment",
			OwnerType:    workflowstore.ArtifactOwnerRun,
			RunRowID:     sql.NullInt64{Int64: fixture.run.ID, Valid: true},
			Kind:         executionAssignmentKind,
			RelativePath: "runs/" + fixture.run.RunID + "/duplicate-assignment.json",
			MediaType:    executionAssignmentMediaType,
			SHA256:       strings.Repeat("0", 64),
			SizeBytes:    1,
		})
		return err
	})
	if duplicateErr == nil {
		t.Fatal("database accepted a second execution assignment for one Run")
	}
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
			ArtifactID:   "artifact-unrelated-run-kind",
			OwnerType:    workflowstore.ArtifactOwnerRun,
			RunRowID:     sql.NullInt64{Int64: fixture.run.ID, Valid: true},
			Kind:         "execution_evidence",
			RelativePath: "runs/" + fixture.run.RunID + "/execution-evidence.json",
			MediaType:    executionAssignmentMediaType,
			SHA256:       strings.Repeat("0", 64),
			SizeBytes:    1,
		})
		return err
	}); err != nil {
		t.Fatalf("unrelated Run artifact kind was rejected: %v", err)
	}
	if got := len(listRunArtifacts(t, fixture)); got != 2 {
		t.Fatalf("Run artifacts = %d, want assignment plus unrelated artifact", got)
	}
	if first.Artifact.Kind != executionAssignmentKind {
		t.Fatalf("first artifact kind = %q", first.Artifact.Kind)
	}
}

func TestPrepareExecutionAssignmentRollsBackStagedArtifactOnMetadataFailure(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	if _, err := fixture.store.DB().Exec(`
CREATE TRIGGER fail_execution_assignment_insert
BEFORE INSERT ON artifacts
WHEN NEW.kind = 'execution_assignment'
BEGIN
    SELECT RAISE(ABORT, 'injected assignment metadata failure');
END`); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID)
	if err == nil {
		t.Fatal("assignment generation succeeded despite metadata failure")
	}
	assertNoAssignment(t, fixture)
	if _, statErr := os.Stat(assignmentPath(fixture)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("promoted assignment survived rollback: %v", statErr)
	}
	if _, err := fixture.store.DB().Exec("DROP TRIGGER fail_execution_assignment_insert"); err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID); err != nil {
		t.Fatal(err)
	} else if len(result.Bytes) == 0 {
		t.Fatal("normal preparation returned empty assignment")
	}
}

func newExecutionAssignmentFixture(t *testing.T, withOperations bool, coverage string) *executionAssignmentFixture {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	packageService, err := executionpackages.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	assignmentService, err := NewExecutionAssignmentService(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	baseCommit := strings.Repeat("a", 40)
	treeOID := strings.Repeat("b", 40)
	authorityBytes := []byte("authority")
	authoritySHA := sha256Hex(authorityBytes)
	authorityPath := filepath.Join(store.ArtifactStore().Root(), "plans", "checkout", "requirements.json")
	if err := os.MkdirAll(filepath.Dir(authorityPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorityPath, authorityBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	briefName := "checkout.ticket-P2-T2.r1.design-brief.md"
	operationsName := "checkout.ticket-P2-T2.r1.deterministic-operations.json"
	briefBytes := []byte(testfixtures.TicketDesignBrief)
	operationsBytes := []byte(executionAssignmentOperations)
	if coverage == "partial" {
		operationsBytes = []byte(strings.Replace(string(operationsBytes), `"coverage":"complete"`, `"coverage":"partial"`, 1))
	}
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
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority) VALUES ('P2-T2', ?, 10) RETURNING id`, workspaceID).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, 1, 'relay', 'main', ?, ?, 'tickets/p2-t2.delivery-ticket.json', 'Package the selected ticket.', 'Package basis context.', 'not_required') RETURNING id`, ticketID, baseCommit, closureID).Scan(&revisionID); err != nil {
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

	fixture := &executionAssignmentFixture{
		store: store, packages: packageService, assignments: assignmentService, selectionID: "selection-package",
		brief:              executionpackages.ArtifactInput{DisplayName: briefName, Bytes: briefBytes, ExpectedSHA256: sha256Hex(briefBytes)},
		operations:         executionpackages.ArtifactInput{DisplayName: operationsName, Bytes: operationsBytes, ExpectedSHA256: sha256Hex(operationsBytes)},
		assignmentFilename: "checkout.ticket-P2-T2.r1.execution-assignment.json",
	}
	input := executionpackages.PrepareInput{SelectionID: fixture.selectionID, TicketDesignBrief: fixture.brief}
	if withOperations {
		operations := fixture.operations
		input.DeterministicOperations = &operations
	}
	prepared, err := packageService.Prepare(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := packageService.Approve(ctx, executionpackages.ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	fixture.run = approved.Run
	fixture.packageID = prepared.Package.PackageID
	return fixture
}

func prepareExecutionAssignment(t *testing.T, fixture *executionAssignmentFixture) ExecutionAssignmentResult {
	t.Helper()
	result, err := fixture.assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func transitionRun(t *testing.T, fixture *executionAssignmentFixture, status string) {
	t.Helper()
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionRun(context.Background(), fixture.run.RunID, workflowstore.RunStatusSetupReady, status)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func listRunArtifacts(t *testing.T, fixture *executionAssignmentFixture) []workflowstore.Artifact {
	t.Helper()
	artifacts, err := fixture.store.ListArtifactsByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return artifacts
}

func assertNoAssignment(t *testing.T, fixture *executionAssignmentFixture) {
	t.Helper()
	for _, artifact := range listRunArtifacts(t, fixture) {
		if artifact.Kind == executionAssignmentKind {
			t.Fatalf("unexpected assignment artifact = %#v", artifact)
		}
	}
	if _, err := os.Stat(assignmentPath(fixture)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected assignment file: %v", err)
	}
}

func assignmentPath(fixture *executionAssignmentFixture) string {
	return filepath.Join(fixture.store.ArtifactStore().Root(), "runs", fixture.run.RunID, fixture.assignmentFilename)
}

func readManagedAssignment(t *testing.T, fixture *executionAssignmentFixture) []byte {
	t.Helper()
	data, err := os.ReadFile(assignmentPath(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustRun(t *testing.T, fixture *executionAssignmentFixture) workflowstore.Run {
	t.Helper()
	run, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func countByRun(t *testing.T, fixture *executionAssignmentFixture, table string) int64 {
	t.Helper()
	var count int64
	if err := fixture.store.DB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE run_row_id = ?", fixture.run.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func countSideEffectRows(t *testing.T, fixture *executionAssignmentFixture, table string) int64 {
	t.Helper()
	if table == "repository_branch_mutation_leases" {
		var count int64
		if err := fixture.store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	return countByRun(t, fixture, table)
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
