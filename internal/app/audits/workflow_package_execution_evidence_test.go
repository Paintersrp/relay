package audits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	workflowpackages "relay/internal/app/packages"
	"relay/internal/executor"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

const workflowPackageEvidenceOperations = `{"schema_version":"1.0","feature_slug":"checkout","repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","coverage":"complete","operations":[{"path":"internal/example.go","operation":"create","implementation":{"content":"package example\n"}}]}`

func packageEvidenceExecutionSpec(featureSlug, branch, baseCommit string) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":"2.0","feature_slug":%q,"repo_target":"relay","branch":%q,"base_commit":%q,"goal":"Package evidence test.","context":"Package evidence test.","scope":{"in_scope":["Package evidence."],"out_of_scope":[]},"steps":[],"validation":{"commands":[]},"completion_criteria":["Done."]}`, featureSlug, branch, baseCommit))
}

type packageEvidenceFixture struct {
	store             *workflowstore.Store
	run               workflowstore.Run
	packageID         string
	assignment        executor.ExecutionAssignmentResult
	outcome           executor.DeterministicOutcomeResult
	mode              executor.ExecutionMode
	sourceVaultReader *evidenceSourceVaultReader
}

// buildPackageEvidence constructs a committed package-linked Run whose runtime
// evidence resolves to the requested execution mode, using the real production
// package, assignment, outcome, and mode-derivation services.
func buildPackageEvidence(t *testing.T, mode executor.ExecutionMode) *packageEvidenceFixture {
	t.Helper()
	withOperations := mode != executor.ExecutionModeAbsent
	coverage := "complete"
	if mode == executor.ExecutionModePartialApplied {
		coverage = "partial"
	}
	fixture := newPackageEvidenceFixture(t, withOperations, coverage)

	assignments, err := executor.NewExecutionAssignmentService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.assignment = assignment

	outcomes, err := executor.NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := outcomes.Persist(context.Background(), packageEvidenceOutcomeInput(fixture.run.RunID, mode, coverage))
	if err != nil {
		t.Fatal(err)
	}
	fixture.outcome = outcome
	fixture.mode = mode

	if mode != executor.ExecutionModeCompleteApplied {
		valResults := make([]workflowAuditValidationEvidence, 0, len(assignment.Assignment.ValidationCommands))
		for _, cmd := range assignment.Assignment.ValidationCommands {
			valResults = append(valResults, workflowAuditValidationEvidence{
				Command:          cmd.Command,
				WorkingDirectory: cmd.WorkingDirectory,
				Expected:         cmd.Expected,
				Status:           "passed",
				ConciseResult:    "ok",
			})
		}
		addTestAttemptAndEvidence(t, fixture, valResults)
	}

	return fixture
}

func packageEvidenceOutcomeInput(runID string, mode executor.ExecutionMode, coverage string) executor.DeterministicOutcomeInput {
	switch mode {
	case executor.ExecutionModeAbsent:
		return executor.DeterministicOutcomeInput{RunID: runID, Preflight: executor.DeterministicPreflightResult{Status: executor.DeterministicPreflightNotPresent}}
	case executor.ExecutionModePreflightFailed:
		return executor.DeterministicOutcomeInput{RunID: runID, Preflight: executor.DeterministicPreflightResult{
			Status:   executor.DeterministicPreflightFailed,
			Coverage: coverage,
			Failure:  &executor.DeterministicPreflightFailure{Code: "source_missing", OperationIndex: 1, Path: "internal/example.go", Expected: "exists=true", Observed: "exists=false"},
		}}
	default:
		content := []byte("package example\n")
		sum := sha256.Sum256(content)
		digest := hex.EncodeToString(sum[:])
		size := int64(len(content))
		plan := &executor.DeterministicMutationPlan{Coverage: coverage, Operations: []executor.PreparedDeterministicOperation{{
			Index: 1, Operation: "create", SourcePath: "internal/example.go", After: executor.FileState{Exists: true, SHA256: digest, Size: size, Bytes: content},
		}}}
		application := executor.DeterministicApplicationResult{
			Coverage:     coverage,
			Operations:   []executor.AppliedDeterministicOperation{{Index: 1, Operation: "create", SourcePath: "internal/example.go", SourceAfter: executor.AppliedFileState{Exists: true, SHA256: digest, Size: size}}},
			ChangedPaths: []string{"internal/example.go"},
		}
		return executor.DeterministicOutcomeInput{RunID: runID, Preflight: executor.DeterministicPreflightResult{Status: executor.DeterministicPreflightReady, Coverage: coverage, Plan: plan}, Application: &application}
	}
}

func newPackageEvidenceFixture(t *testing.T, withOperations bool, coverage string) *packageEvidenceFixture {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	ctx := context.Background()
	baseCommit := strings.Repeat("a", 40)
	treeOID := strings.Repeat("b", 40)
	sourcePath := "tickets/checkout.ticket-P2-T2.r1.delivery-ticket.json"
	reader := &evidenceSourceVaultReader{path: sourcePath, bytes: packageEvidenceDeliveryTicketBytes(baseCommit)}
	packageService, err := workflowpackages.NewServiceWithSourceVaults(store, reader)
	if err != nil {
		t.Fatal(err)
	}
	authorityBytes := []byte(`{"requirements":"authority"}`)
	authoritySHA := packageEvidenceSHA(authorityBytes)
	authorityPath := filepath.Join(store.ArtifactStore().Root(), "plans", "checkout", "requirements.json")
	if err := os.MkdirAll(filepath.Dir(authorityPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorityPath, authorityBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	operationsName := "checkout.ticket-P2-T2.r1.deterministic-operations.json"
	operationsBytes := []byte(workflowPackageEvidenceOperations)
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
	input := workflowpackages.PrepareInput{
		SelectionID: "selection-package",
	}
	if withOperations {
		input.DeterministicOperations = &workflowpackages.ArtifactInput{DisplayName: operationsName, Bytes: operationsBytes, ExpectedSHA256: packageEvidenceSHA(operationsBytes)}
	}
	prepared, err := packageService.Prepare(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := packageService.Approve(ctx, workflowpackages.ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	return &packageEvidenceFixture{store: store, run: approved.Run, packageID: prepared.Package.PackageID, sourceVaultReader: reader}
}

func packageEvidenceSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func packageEvidenceDeliveryTicketBytes(baseCommit string) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"%s","goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"required_invariants":["Packages must bind the exact approved Ticket."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":"internal/app/packages","obligation":"Preserve the selected package basis.","prerequisites":[]}],"proof_obligations":["Prove package preparation binds the approved Ticket."],"validation_commands":[{"working_directory":"","command":"go test ./internal/app/packages","expected":"all tests pass"}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["All tests pass."]}`, baseCommit))
}

type evidenceSourceVaultReader struct {
	path  string
	bytes []byte
	err   error
}

func (r *evidenceSourceVaultReader) ReadPath(ctx context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	if r.err != nil {
		return sourcevault.ReadPathResult{}, r.err
	}
	if request.Path != r.path {
		return sourcevault.ReadPathResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
	}
	return sourcevault.ReadPathResult{ObjectOID: strings.Repeat("d", 40), Bytes: append([]byte(nil), r.bytes...)}, nil
}

func (r *evidenceSourceVaultReader) WithErr(err error) *evidenceSourceVaultReader {
	return &evidenceSourceVaultReader{path: r.path, bytes: r.bytes, err: err}
}

// packageEvidenceMutatePreflightCoverage changes the deterministic outcome summary
// coverage and re-derives the artifact digest so the mutated result still
// passes artifact verification. It is used to exercise the coverage decision
// through the outcome seam without constructing new database fixtures.
func packageEvidenceMutatePreflightCoverage(t *testing.T, outcome executor.DeterministicOutcomeResult, coverage string) executor.DeterministicOutcomeResult {
	t.Helper()
	outcome.Outcome.Outcome.Coverage = coverage
	bytes, err := json.Marshal(outcome.Outcome)
	if err != nil {
		t.Fatal(err)
	}
	outcome.Bytes = bytes
	outcome.Artifact.SizeBytes = int64(len(bytes))
	sum := sha256.Sum256(bytes)
	outcome.Artifact.SHA256 = hex.EncodeToString(sum[:])
	return outcome
}

func packageEvidenceModes() []executor.ExecutionMode {
	return []executor.ExecutionMode{
		executor.ExecutionModeAbsent,
		executor.ExecutionModePreflightFailed,
		executor.ExecutionModePartialApplied,
		executor.ExecutionModeCompleteApplied,
	}
}

func TestWorkflowPackageExecutionEvidenceConstructorInitializesDependencies(t *testing.T) {
	fixture := newPackageEvidenceFixture(t, false, "")
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if service.store == nil || service.packages == nil || service.assignments == nil || service.outcomes == nil {
		t.Fatalf("service dependencies are incomplete: %#v", service)
	}
	if service.loadRun == nil || service.loadAuthority == nil || service.loadAssignment == nil || service.loadOutcome == nil {
		t.Fatal("service read seams are not initialized")
	}
	if _, err := NewWorkflowPackageExecutionEvidenceService(nil, fixture.sourceVaultReader); err == nil {
		t.Fatal("nil store was accepted")
	}
	if _, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, nil); err == nil {
		t.Fatal("nil source-vault reader was accepted, expected error")
	}
}

func TestWorkflowExecutionEvidencePlaceholderFilesAbsent(t *testing.T) {
	files := []string{
		"workflow_execution_evidence.go",
		"workflow_execution_evidence_test.go",
	}
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("placeholder file %s should be absent, but stat returned: %v", f, err)
		}
	}
}

func TestWorkflowPackageExecutionEvidenceRejectsInvalidRunIDWithoutReads(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	fail := func(string) { t.Fatal("Load performed a read for an invalid Run ID") }
	service.loadRun = func(context.Context, string) (workflowstore.Run, error) { fail(""); return workflowstore.Run{}, nil }
	service.loadAuthority = func(context.Context, string) (workflowpackages.ApprovedAuthority, error) {
		fail("")
		return workflowpackages.ApprovedAuthority{}, nil
	}
	service.loadAssignment = func(context.Context, string) (executor.ExecutionAssignmentResult, error) {
		fail("")
		return executor.ExecutionAssignmentResult{}, nil
	}
	service.loadOutcome = func(context.Context, string) (executor.DeterministicOutcomeResult, error) {
		fail("")
		return executor.DeterministicOutcomeResult{}, nil
	}
	for _, runID := range []string{"", " ", " run-1", "run-1 "} {
		if _, err := service.Load(context.Background(), runID); err == nil {
			t.Fatalf("Run ID %q was accepted", runID)
		}
	}
}

func TestWorkflowPackageExecutionEvidenceRejectsNonPackageRun(t *testing.T) {
	fixture := newPackageEvidenceFixture(t, false, "")
	var created workflowstore.Run
	err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		var createErr error
		created, createErr = tx.CreateRun(context.Background(), workflowstore.CreateRunParams{RunID: "run-audit-non-package", FeatureSlug: "audit-test", RepoTarget: "relay", Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: strings.Repeat("a", 40)})
		return createErr
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Load(context.Background(), created.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("non-package Run error = %v", err)
	}
}

func TestWorkflowPackageExecutionEvidenceResolvesEveryMode(t *testing.T) {
	for _, mode := range packageEvidenceModes() {
		t.Run(string(mode), func(t *testing.T) {
			fixture := buildPackageEvidence(t, mode)
			service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := service.Load(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if evidence.Mode != mode {
				t.Fatalf("mode = %q, want %q", evidence.Mode, mode)
			}
			wantAdaptive := mode != executor.ExecutionModeCompleteApplied
			if (evidence.Attempt != nil) != wantAdaptive {
				t.Fatalf("attempt presence = %v, want adaptive dispatch %v", evidence.Attempt != nil, wantAdaptive)
			}
			if evidence.Assignment.Artifact.ID != fixture.assignment.Artifact.ID || evidence.Assignment.Artifact.SHA256 != fixture.assignment.Artifact.SHA256 {
				t.Fatalf("assignment artifact = %#v", evidence.Assignment.Artifact)
			}
			if evidence.Deterministic.Artifact.ID != fixture.outcome.Artifact.ID || evidence.Deterministic.Artifact.SHA256 != fixture.outcome.Artifact.SHA256 {
				t.Fatalf("outcome artifact = %#v", evidence.Deterministic.Artifact)
			}
		})
	}
}

func TestWorkflowPackageExecutionEvidenceCompleteHasNoAdaptiveDispatch(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeCompleteApplied)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := service.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Mode != executor.ExecutionModeCompleteApplied {
		t.Fatalf("mode = %q, want complete_applied", evidence.Mode)
	}
	if evidence.Attempt != nil {
		t.Fatal("complete mode required adaptive dispatch")
	}
}

func TestWorkflowPackageExecutionEvidenceRejectsCrossServiceMismatches(t *testing.T) {
	tests := []struct {
		name    string
		mode    executor.ExecutionMode
		corrupt func(t *testing.T, service *WorkflowPackageExecutionEvidenceService)
	}{
		{
			name: "run package identity mismatch",
			mode: executor.ExecutionModeAbsent,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadAuthority
				service.loadAuthority = func(ctx context.Context, runID string) (workflowpackages.ApprovedAuthority, error) {
					authority, err := real(ctx, runID)
					if err != nil {
						return authority, err
					}
					authority.Package.ID++
					return authority, nil
				}
			},
		},
		{
			name: "assignment authority mismatch",
			mode: executor.ExecutionModeAbsent,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadAssignment
				service.loadAssignment = func(ctx context.Context, runID string) (executor.ExecutionAssignmentResult, error) {
					assignment, err := real(ctx, runID)
					if err != nil {
						return assignment, err
					}
					assignment.Assignment.Package.PackageID += "-tampered"
					return assignment, nil
				}
			},
		},
		{
			name: "assignment source identity mismatch",
			mode: executor.ExecutionModeAbsent,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadAssignment
				service.loadAssignment = func(ctx context.Context, runID string) (executor.ExecutionAssignmentResult, error) {
					assignment, err := real(ctx, runID)
					if err != nil {
						return assignment, err
					}
					assignment.Assignment.Source.CommitOID = strings.Repeat("0", 40)
					return assignment, nil
				}
			},
		},
		{
			name: "assignment completed dependency mismatch",
			mode: executor.ExecutionModeAbsent,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadAssignment
				service.loadAssignment = func(ctx context.Context, runID string) (executor.ExecutionAssignmentResult, error) {
					assignment, err := real(ctx, runID)
					if err != nil {
						return assignment, err
					}
					assignment.Assignment.Dependencies = append(assignment.Assignment.Dependencies, executor.ExecutionAssignmentDependency{Sequence: 1, TicketID: "P2-T1", Revision: 1, Outcome: "satisfied"})
					return assignment, nil
				}
			},
		},
		{
			name: "outcome assignment mismatch",
			mode: executor.ExecutionModeAbsent,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadOutcome
				service.loadOutcome = func(ctx context.Context, runID string) (executor.DeterministicOutcomeResult, error) {
					outcome, err := real(ctx, runID)
					if err != nil {
						return outcome, err
					}
					outcome.Outcome.ExecutionAssignment.SHA256 = strings.Repeat("0", 64)
					return outcome, nil
				}
			},
		},
		{
			name: "outcome status tampering changes mode",
			mode: executor.ExecutionModeAbsent,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadOutcome
				service.loadOutcome = func(ctx context.Context, runID string) (executor.DeterministicOutcomeResult, error) {
					outcome, err := real(ctx, runID)
					if err != nil {
						return outcome, err
					}
					outcome.Outcome.Outcome.Status = "applied"
					outcome.Outcome.Outcome.Coverage = "complete"
					return outcome, nil
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildPackageEvidence(t, test.mode)
			service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			test.corrupt(t, service)
			if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
				t.Fatalf("error = %v, want conflict", err)
			}
		})
	}
}

func TestWorkflowPackageExecutionEvidenceMissingEvidenceFailsClosed(t *testing.T) {
	t.Run("missing assignment", func(t *testing.T) {
		fixture := newPackageEvidenceFixture(t, false, "")
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Load(context.Background(), fixture.run.RunID)
		if err == nil {
			t.Fatal("missing assignment was accepted")
		}
		if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("missing assignment error = %v", err)
		}
	})
	t.Run("missing outcome", func(t *testing.T) {
		fixture := newPackageEvidenceFixture(t, false, "")
		assignments, err := executor.NewExecutionAssignmentService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID); err != nil {
			t.Fatal(err)
		}
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Load(context.Background(), fixture.run.RunID)
		if err == nil {
			t.Fatal("missing outcome was accepted")
		}
		if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("missing outcome error = %v", err)
		}
	})
	t.Run("missing execution attempt", func(t *testing.T) {
		fixture := newPackageEvidenceFixture(t, false, "")
		assignments, err := executor.NewExecutionAssignmentService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := assignments.PrepareExecutionAssignment(context.Background(), fixture.run.RunID); err != nil {
			t.Fatal(err)
		}
		outcomes, err := executor.NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := outcomes.Persist(context.Background(), packageEvidenceOutcomeInput(fixture.run.RunID, executor.ExecutionModeAbsent, "")); err != nil {
			t.Fatal(err)
		}
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Load(context.Background(), fixture.run.RunID)
		if err == nil {
			t.Fatal("missing execution attempt was accepted")
		}
		if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("missing execution attempt error = %v", err)
		}
	})
}

func TestWorkflowPackageExecutionEvidenceDuplicateFromLoaderIsConflict(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.loadAssignment = func(context.Context, string) (executor.ExecutionAssignmentResult, error) {
		return executor.ExecutionAssignmentResult{}, executor.ErrExecutionAssignmentConflict
	}
	_, err = service.Load(context.Background(), fixture.run.RunID)
	if !errors.Is(err, executor.ErrExecutionAssignmentConflict) {
		t.Fatalf("duplicate assignment error = %v", err)
	}
	if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("duplicate assignment not classified as audit conflict: %v", err)
	}
}

func TestWorkflowPackageExecutionEvidenceDeterministicOutcomeConflictIsClassified(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.loadOutcome = func(context.Context, string) (executor.DeterministicOutcomeResult, error) {
		return executor.DeterministicOutcomeResult{}, executor.ErrDeterministicOutcomeConflict
	}
	_, err = service.Load(context.Background(), fixture.run.RunID)
	if !errors.Is(err, executor.ErrDeterministicOutcomeConflict) {
		t.Fatalf("deterministic outcome conflict error = %v", err)
	}
	if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("deterministic outcome conflict not classified as audit conflict: %v", err)
	}
}

func TestWorkflowPackageExecutionEvidenceApprovedAuthorityInvalidIsClassified(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.loadAuthority = func(context.Context, string) (workflowpackages.ApprovedAuthority, error) {
		return workflowpackages.ApprovedAuthority{}, workflowpackages.ErrApprovedAuthorityInvalid
	}
	_, err = service.Load(context.Background(), fixture.run.RunID)
	if !errors.Is(err, workflowpackages.ErrApprovedAuthorityInvalid) {
		t.Fatalf("approved authority invalid error = %v", err)
	}
	if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("approved authority invalid not classified as audit conflict: %v", err)
	}
}

func TestWorkflowPackageExecutionEvidenceInfrastructureErrorIsNotClassified(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	infrastructure := errors.New("simulated infrastructure failure")
	service.loadAssignment = func(context.Context, string) (executor.ExecutionAssignmentResult, error) {
		return executor.ExecutionAssignmentResult{}, infrastructure
	}
	_, err = service.Load(context.Background(), fixture.run.RunID)
	if !errors.Is(err, infrastructure) {
		t.Fatalf("infrastructure error = %v", err)
	}
	if errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("infrastructure error misclassified as audit conflict: %v", err)
	}
}

func TestWorkflowPackageExecutionEvidencePreflightFailedCoverage(t *testing.T) {
	tests := []struct {
		name     string
		coverage string
		wantErr  bool
	}{
		{"partial", "partial", false},
		{"complete", "complete", false},
		{"empty", "", true},
		{"unknown", "unknown", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildPackageEvidence(t, executor.ExecutionModePreflightFailed)
			service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			realOutcome := service.loadOutcome
			service.loadOutcome = func(ctx context.Context, runID string) (executor.DeterministicOutcomeResult, error) {
				outcome, err := realOutcome(ctx, runID)
				if err != nil {
					return outcome, err
				}
				return packageEvidenceMutatePreflightCoverage(t, outcome, test.coverage), nil
			}
			_, err = service.Load(context.Background(), fixture.run.RunID)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected conflict")
				}
				if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkflowPackageExecutionEvidenceRepeatedLoadIsIdentical(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeCompleteApplied)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Assignment, second.Assignment) || !reflect.DeepEqual(first.Deterministic, second.Deterministic) || first.Mode != second.Mode {
		t.Fatal("repeated Load returned different evidence")
	}
	if first.Run.ID != second.Run.ID || first.Authority.Package.PackageID != second.Authority.Package.PackageID {
		t.Fatal("repeated Load returned different run or authority identity")
	}
}

func TestWorkflowPackageExecutionEvidenceLoadPerformsNoWrites(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
		return append([]byte(nil), data...), nil
	})
	tables := []string{
		"runs", "artifacts", "execution_attempts", "repository_branch_mutation_leases",
		"execution_packages", "execution_package_approvals", "delivery_ticket_selections",
	}
	before := packageEvidenceCounts(t, fixture.store, tables)
	if _, err := service.Load(context.Background(), fixture.run.RunID); err != nil {
		t.Fatal(err)
	}
	after := packageEvidenceCounts(t, fixture.store, tables)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("row counts changed: before=%v after=%v", before, after)
	}
}

func packageEvidenceCounts(t *testing.T, store *workflowstore.Store, tables []string) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var count int64
		if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

func installExecutionEvidenceMutation(
	t *testing.T,
	service *WorkflowPackageExecutionEvidenceService,
	mutate func([]byte) ([]byte, error),
) {
	t.Helper()
	if service == nil || service.readArtifactBytes == nil || service.loadAttemptArtifacts == nil {
		t.Fatal("execution evidence mutation requires initialized read seams")
	}
	if mutate == nil {
		t.Fatal("execution evidence mutation function is required")
	}

	realRead := service.readArtifactBytes
	realLoad := service.loadAttemptArtifacts
	var target workflowstore.Artifact
	var mutatedBytes []byte
	initialized := false

	service.loadAttemptArtifacts = func(ctx context.Context, attemptRowID int64) ([]workflowstore.Artifact, error) {
		artifacts, err := realLoad(ctx, attemptRowID)
		if err != nil {
			return nil, err
		}
		artifacts = append([]workflowstore.Artifact(nil), artifacts...)

		targetIndex := -1
		for index, artifact := range artifacts {
			if artifact.Kind != "execution_evidence" {
				continue
			}
			if targetIndex != -1 {
				t.Fatalf("mutation helper found multiple execution_evidence artifacts")
				return nil, fmt.Errorf("multiple execution_evidence artifacts")
			}
			targetIndex = index
		}
		if targetIndex == -1 {
			t.Fatalf("mutation helper found no execution_evidence artifact")
			return nil, fmt.Errorf("missing execution_evidence artifact")
		}

		if !initialized {
			target = artifacts[targetIndex]
			originalBytes, err := realRead(ctx, target, MaxWorkflowAuditEvidenceBytes)
			if err != nil {
				t.Fatalf("read original execution_evidence artifact: %v", err)
				return nil, err
			}
			first, err := mutate(append([]byte(nil), originalBytes...))
			if err != nil {
				t.Fatalf("mutate execution_evidence artifact: %v", err)
				return nil, err
			}
			second, err := mutate(append([]byte(nil), originalBytes...))
			if err != nil {
				t.Fatalf("repeat mutation of execution_evidence artifact: %v", err)
				return nil, err
			}
			if !bytes.Equal(first, second) {
				err := fmt.Errorf("mutation of execution_evidence artifact is nondeterministic")
				t.Fatal(err)
				return nil, err
			}
			mutatedBytes = append([]byte(nil), first...)
			initialized = true
		}

		artifacts[targetIndex].SizeBytes = int64(len(mutatedBytes))
		artifacts[targetIndex].SHA256 = packageEvidenceSHA(mutatedBytes)
		return artifacts, nil
	}

	service.readArtifactBytes = func(ctx context.Context, artifact workflowstore.Artifact, max int) ([]byte, error) {
		if initialized && artifact.ID == target.ID && artifact.ArtifactID == target.ArtifactID && artifact.RelativePath == target.RelativePath {
			return append([]byte(nil), mutatedBytes...), nil
		}
		return realRead(ctx, artifact, max)
	}
}

func addTestAttemptAndEvidence(
	t *testing.T,
	fixture *packageEvidenceFixture,
	validationResults []workflowAuditValidationEvidence,
) (workflowstore.ExecutionAttempt, workflowstore.Artifact, []byte) {
	t.Helper()
	ctx := context.Background()
	db := fixture.store.DB()

	var attemptRowID int64
	attemptID := workflowstore.NewExecutionAttemptID()
	wireMode, wireModeValid := workflowPackageWireMode(fixture.mode)
	if !wireModeValid {
		// Deterministic-complete runs never dispatch an adaptive attempt, so
		// Load rejects any attempt before reading its payload identity. The
		// placeholder keeps the attempt fixture record well-formed.
		wireMode = "adaptive_no_operations"
	}
	resultStruct := map[string]any{
		"exit_code":                        0,
		"execution_assignment_artifact_id": fixture.assignment.Artifact.ArtifactID,
		"execution_assignment_sha256":      fixture.assignment.Artifact.SHA256,
		"execution_assignment_mode":        wireMode,
	}
	resultJSON, err := json.Marshal(resultStruct)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO execution_attempts (attempt_id, run_row_id, attempt_number, adapter, model, status, result_json)
		VALUES (?, ?, 1, 'codex', 'm1', 'pending', '{}') RETURNING id`,
		attemptID, fixture.run.ID,
	).Scan(&attemptRowID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE execution_attempts SET status = 'running', started_at = '2026-07-18T00:00:00.000000000Z' WHERE id = ?`,
		attemptRowID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE execution_attempts SET status = 'succeeded', result_json = ?, finished_at = '2026-07-18T00:00:01.000000000Z' WHERE id = ?`,
		string(resultJSON), attemptRowID,
	); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"execution_assignment_artifact_id": fixture.assignment.Artifact.ArtifactID,
		"execution_assignment_sha256":      fixture.assignment.Artifact.SHA256,
		"execution_assignment_mode":        wireMode,
	}
	if validationResults != nil {
		payload["validation_results"] = validationResults
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(payloadBytes)
	sha := hex.EncodeToString(sum[:])
	relPath := fmt.Sprintf("attempts/%s/execution-evidence.json", attemptID)

	var artifactRowID int64
	artifactID := workflowstore.NewArtifactID()
	if err := db.QueryRowContext(ctx, `
		INSERT INTO artifacts (artifact_id, owner_type, execution_attempt_row_id, kind, relative_path, media_type, sha256, size_bytes)
		VALUES (?, 'execution_attempt', ?, 'execution_evidence', ?, 'application/json', ?, ?) RETURNING id`,
		artifactID, attemptRowID, relPath, sha, len(payloadBytes),
	).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}

	fullPath := filepath.Join(fixture.store.ArtifactStore().Root(), relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, payloadBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	attempt := workflowstore.ExecutionAttempt{
		ID:            attemptRowID,
		RunRowID:      fixture.run.ID,
		AttemptID:     attemptID,
		AttemptNumber: 1,
		Status:        string(workflowstore.AttemptStatusSucceeded),
		ResultJSON:    string(resultJSON),
	}
	art := workflowstore.Artifact{
		ID:                    artifactRowID,
		ArtifactID:            artifactID,
		OwnerType:             workflowstore.ArtifactOwnerExecutionAttempt,
		ExecutionAttemptRowID: sql.NullInt64{Int64: attemptRowID, Valid: true},
		Kind:                  "execution_evidence",
		RelativePath:          relPath,
		MediaType:             "application/json",
		SHA256:                sha,
		SizeBytes:             int64(len(payloadBytes)),
	}

	return attempt, art, payloadBytes
}

func TestWorkflowPackageExecutionEvidenceDeterministicCompleteWithAttemptFails(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeCompleteApplied)
	addTestAttemptAndEvidence(t, fixture, nil)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("error = %v, want conflict", err)
	}
}

func TestWorkflowPackageExecutionEvidenceAdaptiveExecutionAttemptValidation(t *testing.T) {
	t.Run("zero attempts fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
			return nil, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("multiple attempts fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realAttempts := service.loadAttempts
		service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
			attempts, err := realAttempts(ctx, runRowID)
			if err != nil {
				return nil, err
			}
			return append(attempts, attempts[0]), nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("nonsucceeded attempt status fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realAttempts := service.loadAttempts
		service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
			attempts, err := realAttempts(ctx, runRowID)
			if err != nil {
				return nil, err
			}
			attempts[0].Status = "failed"
			return attempts, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("attempt number not 1 fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realAttempts := service.loadAttempts
		service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
			attempts, err := realAttempts(ctx, runRowID)
			if err != nil {
				return nil, err
			}
			attempts[0].AttemptNumber = 2
			return attempts, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("noncanonical attempt ID fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realAttempts := service.loadAttempts
		service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
			attempts, err := realAttempts(ctx, runRowID)
			if err != nil {
				return nil, err
			}
			attempts[0].AttemptID = "   "
			return attempts, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})
}

func TestWorkflowPackageExecutionEvidenceArtifactValidation(t *testing.T) {
	t.Run("missing execution_evidence artifact fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		service.loadAttemptArtifacts = func(ctx context.Context, attemptRowID int64) ([]workflowstore.Artifact, error) {
			return nil, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("duplicate execution_evidence artifact fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realArtifacts := service.loadAttemptArtifacts
		service.loadAttemptArtifacts = func(ctx context.Context, attemptRowID int64) ([]workflowstore.Artifact, error) {
			arts, err := realArtifacts(ctx, attemptRowID)
			if err != nil {
				return nil, err
			}
			return append(arts, arts[0]), nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("wrongly owned artifact fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realArtifacts := service.loadAttemptArtifacts
		service.loadAttemptArtifacts = func(ctx context.Context, attemptRowID int64) ([]workflowstore.Artifact, error) {
			arts, err := realArtifacts(ctx, attemptRowID)
			if err != nil {
				return nil, err
			}
			arts[0].OwnerType = workflowstore.ArtifactOwnerRun
			return arts, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("wrongly typed artifact fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realArtifacts := service.loadAttemptArtifacts
		service.loadAttemptArtifacts = func(ctx context.Context, attemptRowID int64) ([]workflowstore.Artifact, error) {
			arts, err := realArtifacts(ctx, attemptRowID)
			if err != nil {
				return nil, err
			}
			arts[0].MediaType = "text/plain"
			return arts, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("wrongly pathed artifact fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realArtifacts := service.loadAttemptArtifacts
		service.loadAttemptArtifacts = func(ctx context.Context, attemptRowID int64) ([]workflowstore.Artifact, error) {
			arts, err := realArtifacts(ctx, attemptRowID)
			if err != nil {
				return nil, err
			}
			arts[0].RelativePath = "attempts/wrong/execution-evidence.json"
			return arts, nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})

	t.Run("tampered artifact digest fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		realRead := service.readArtifactBytes
		service.readArtifactBytes = func(ctx context.Context, art workflowstore.Artifact, max int) ([]byte, error) {
			bytes, err := realRead(ctx, art, max)
			if err != nil {
				return nil, err
			}
			return append(bytes, []byte("tampered")...), nil
		}
		if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	})
}

func TestWorkflowPackageExecutionEvidenceAssignmentBindingMismatchFails(t *testing.T) {
	t.Run("execution assignment artifact ID mismatch fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, err
			}
			m["execution_assignment_artifact_id"] = "wrong-id"
			return json.Marshal(m)
		})
		if _, err := service.Load(context.Background(), fixture.run.RunID); err == nil || !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) || !strings.Contains(err.Error(), "execution evidence payload execution assignment identity disagrees with assignment") {
			t.Fatalf("error = %v, want execution-assignment identity conflict", err)
		}
	})

}

func TestWorkflowPackageExecutionEvidenceResultJSONBindingMismatchFails(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	realAttempts := service.loadAttempts
	service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
		attempts, err := realAttempts(ctx, runRowID)
		if err != nil {
			return nil, err
		}
		attempts[0].ResultJSON = `{"execution_assignment_artifact_id":"wrong-id"}`
		return attempts, nil
	}
	if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("error = %v, want conflict", err)
	}
}

// TestWorkflowPackageExecutionEvidenceRejectsObsoleteExecutionModeKey pins the
// canonical execution_assignment_mode wire key that the real executor now
// persists. A runtime or evidence payload carrying the retired execution_mode
// key (or omitting the canonical key) must be rejected as an execution
// assignment identity conflict.
func TestWorkflowPackageExecutionEvidenceRejectsObsoleteExecutionModeKey(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	realAttempts := service.loadAttempts
	service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
		attempts, err := realAttempts(ctx, runRowID)
		if err != nil {
			return nil, err
		}
		var runtime map[string]any
		if err := json.Unmarshal([]byte(attempts[0].ResultJSON), &runtime); err != nil {
			t.Fatalf("decode attempt ResultJSON: %v", err)
			return nil, err
		}
		delete(runtime, "execution_assignment_mode")
		runtime["execution_mode"] = "absent"
		rewritten, err := json.Marshal(runtime)
		if err != nil {
			return nil, err
		}
		attempts[0].ResultJSON = string(rewritten)
		return attempts, nil
	}
	installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, err
		}
		delete(payload, "execution_assignment_mode")
		payload["execution_mode"] = "absent"
		return json.Marshal(payload)
	})
	if _, err := service.Load(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("error = %v, want execution-assignment identity conflict", err)
	}
}

func TestWorkflowPackageExecutionEvidenceValidationMapping(t *testing.T) {
	t.Run("missing declared results become not_run", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, err
			}
			delete(m, "validation_results") // missing results for declared commands
			return json.Marshal(m)
		})
		evidence, err := service.Load(context.Background(), fixture.run.RunID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(evidence.Validation) != len(fixture.assignment.Assignment.ValidationCommands) {
			t.Fatalf("validation count = %d, want %d", len(evidence.Validation), len(fixture.assignment.Assignment.ValidationCommands))
		}
		for _, val := range evidence.Validation {
			if val.Status != "not_run" {
				t.Fatalf("status = %q, want not_run", val.Status)
			}
			if val.ConciseResult != "No trustworthy structured result was available for this approved validation command." {
				t.Fatalf("concise_result = %q", val.ConciseResult)
			}
		}
		if evidence.Attempt == nil || evidence.Attempt.Artifact.SizeBytes != int64(len(evidence.Attempt.Bytes)) || evidence.Attempt.Artifact.SHA256 != packageEvidenceSHA(evidence.Attempt.Bytes) {
			t.Fatalf("mutated artifact metadata = %#v, want size and digest for returned bytes", evidence.Attempt)
		}
	})

	t.Run("deterministic-complete commands become not_run", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeCompleteApplied)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := service.Load(context.Background(), fixture.run.RunID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(evidence.Validation) != len(fixture.assignment.Assignment.ValidationCommands) {
			t.Fatalf("validation count = %d, want %d", len(evidence.Validation), len(fixture.assignment.Assignment.ValidationCommands))
		}
		for _, val := range evidence.Validation {
			if val.Status != "not_run" {
				t.Fatalf("status = %q, want not_run", val.Status)
			}
			if val.ConciseResult != "No adaptive Executor attempt was dispatched for deterministic-complete execution." {
				t.Fatalf("concise_result = %q", val.ConciseResult)
			}
		}
	})
}

func TestWorkflowPackageExecutionEvidenceValidationMappingRejectsInvalid(t *testing.T) {
	t.Run("unknown command fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, err
			}
			m["validation_results"] = []any{
				map[string]any{"command": "unknown-command", "expected": "pass", "status": "passed", "concise_result": "ok"},
			}
			return json.Marshal(m)
		})
		if _, err := service.Load(context.Background(), fixture.run.RunID); err == nil || !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) || !strings.Contains(err.Error(), "validation mapping") || !strings.Contains(err.Error(), "unknown validation command") {
			t.Fatalf("error = %v, want unknown validation command mapping conflict", err)
		}
	})
}

func TestWorkflowPackageExecutionEvidenceZeroValidationCommands(t *testing.T) {
	setupZeroCommands := func(service *WorkflowPackageExecutionEvidenceService) {
		realAuth := service.loadAuthority
		service.loadAuthority = func(ctx context.Context, runID string) (workflowpackages.ApprovedAuthority, error) {
			auth, err := realAuth(ctx, runID)
			if err != nil {
				return auth, err
			}
			auth.TicketProjection.ValidationCommands = nil
			return auth, nil
		}
		realAssign := service.loadAssignment
		service.loadAssignment = func(ctx context.Context, runID string) (executor.ExecutionAssignmentResult, error) {
			asgn, err := realAssign(ctx, runID)
			if err != nil {
				return asgn, err
			}
			asgn.Assignment.ValidationCommands = nil
			return asgn, nil
		}
	}

	t.Run("absent validation_results succeeds", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		setupZeroCommands(service)
		installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, err
			}
			delete(m, "validation_results")
			return json.Marshal(m)
		})
		evidence, err := service.Load(context.Background(), fixture.run.RunID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(evidence.Validation) != 0 {
			t.Fatalf("validation count = %d, want 0", len(evidence.Validation))
		}
	})

	t.Run("empty validation_results array fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		setupZeroCommands(service)
		installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, err
			}
			m["validation_results"] = []any{}
			return json.Marshal(m)
		})
		_, err = service.Load(context.Background(), fixture.run.RunID)
		if err == nil {
			t.Fatal("expected error for empty validation_results array")
		}
		if err == nil || !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) || !strings.Contains(err.Error(), "decode execution_evidence artifact") || strings.Contains(err.Error(), "artifact size does not match") || strings.Contains(err.Error(), "artifact digest does not match") {
			t.Fatalf("error = %v, want typed-decoder conflict without artifact integrity mismatch", err)
		}
	})

	t.Run("null validation_results fails", func(t *testing.T) {
		fixture := buildPackageEvidence(t, executor.ExecutionModeAbsent)
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		setupZeroCommands(service)
		installExecutionEvidenceMutation(t, service, func(data []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, err
			}
			m["validation_results"] = nil
			return json.Marshal(m)
		})
		_, err = service.Load(context.Background(), fixture.run.RunID)
		if err == nil {
			t.Fatal("expected error for null validation_results")
		}
		if err == nil || !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) || !strings.Contains(err.Error(), "decode execution_evidence artifact") || strings.Contains(err.Error(), "artifact size does not match") || strings.Contains(err.Error(), "artifact digest does not match") {
			t.Fatalf("error = %v, want typed-decoder conflict without artifact integrity mismatch", err)
		}
	})
}
