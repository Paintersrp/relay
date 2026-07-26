package audits

import (
	"context"
	"crypto/sha256"
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
	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/executor"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
)

const workflowPackageEvidenceOperations = `{"schema_version":"1.0","feature_slug":"checkout","repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","coverage":"complete","operations":[{"path":"internal/example.go","operation":"create","implementation":{"content":"package example\n"}}]}`

type packageEvidenceFixture struct {
	store             *workflowstore.Store
	run               workflowstore.Run
	packageID         string
	assignment        executor.ExecutionAssignmentResult
	outcome           executor.DeterministicOutcomeResult
	brief             executor.EffectiveExecutorBriefResult
	sourceVaultReader *evidenceSourceVaultReader
}

// buildPackageEvidence constructs a committed package-linked Run whose runtime
// evidence resolves to the requested effective mode, using the real production
// package, assignment, outcome, and effective-Brief services.
func buildPackageEvidence(t *testing.T, mode executor.EffectiveExecutorBriefMode) *packageEvidenceFixture {
	t.Helper()
	withOperations := mode != executor.EffectiveExecutorBriefAdaptiveNoOperations
	coverage := "complete"
	if mode == executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication {
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

	briefs, err := executor.NewEffectiveExecutorBriefService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	brief, err := briefs.Prepare(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Mode != mode {
		t.Fatalf("prepared mode = %q, want %q", brief.Mode, mode)
	}
	fixture.brief = brief
	return fixture
}

func packageEvidenceOutcomeInput(runID string, mode executor.EffectiveExecutorBriefMode, coverage string) executor.DeterministicOutcomeInput {
	switch mode {
	case executor.EffectiveExecutorBriefAdaptiveNoOperations:
		return executor.DeterministicOutcomeInput{RunID: runID, Preflight: executor.DeterministicPreflightResult{Status: executor.DeterministicPreflightNotPresent}}
	case executor.EffectiveExecutorBriefAdaptivePreflightFailed:
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
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	baseCommit := strings.Repeat("a", 40)
	treeOID := strings.Repeat("b", 40)
	sourcePath := "tickets/p2-t2.delivery-ticket.json"
	reader := &evidenceSourceVaultReader{path: sourcePath, bytes: packageEvidenceDeliveryTicketBytes(baseCommit)}
	packageService, err := workflowpackages.NewServiceWithSourceVaults(store, reader)
	if err != nil {
		t.Fatal(err)
	}
	authorityBytes := []byte("authority")
	authoritySHA := packageEvidenceSHA(authorityBytes)
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
		SelectionID:       "selection-package",
		TicketDesignBrief: workflowpackages.ArtifactInput{DisplayName: briefName, Bytes: briefBytes, ExpectedSHA256: packageEvidenceSHA(briefBytes)},
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
	return []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"%s","goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":[],"out_of_scope":[]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":[],"transition_applicability":"not_required","completion_criteria":[]}`, baseCommit))
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

func packageEvidenceModes() []executor.EffectiveExecutorBriefMode {
	return []executor.EffectiveExecutorBriefMode{
		executor.EffectiveExecutorBriefAdaptiveNoOperations,
		executor.EffectiveExecutorBriefAdaptivePreflightFailed,
		executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication,
		executor.EffectiveExecutorBriefDeterministicComplete,
	}
}

func TestWorkflowPackageExecutionEvidenceConstructorInitializesDependencies(t *testing.T) {
	fixture := newPackageEvidenceFixture(t, false, "")
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if service.store == nil || service.packages == nil || service.assignments == nil || service.outcomes == nil || service.briefs == nil {
		t.Fatalf("service dependencies are incomplete: %#v", service)
	}
	if service.loadRun == nil || service.loadAuthority == nil || service.loadAssignment == nil || service.loadOutcome == nil || service.loadBrief == nil {
		t.Fatal("service read seams are not initialized")
	}
	if _, err := NewWorkflowPackageExecutionEvidenceService(nil, nil); err == nil {
		t.Fatal("nil store was accepted")
	}
}

func TestWorkflowPackageExecutionEvidenceRejectsInvalidRunIDWithoutReads(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
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
	service.loadBrief = func(context.Context, string) (executor.EffectiveExecutorBriefResult, error) {
		fail("")
		return executor.EffectiveExecutorBriefResult{}, nil
	}
	for _, runID := range []string{"", " ", " run-1", "run-1 "} {
		if _, err := service.Load(context.Background(), runID); err == nil {
			t.Fatalf("Run ID %q was accepted", runID)
		}
	}
}

func TestWorkflowPackageExecutionEvidenceRejectsNonPackageRun(t *testing.T) {
	fixture := newPackageEvidenceFixture(t, false, "")
	runs, err := workflowruns.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      "audit-test",
		RepoTarget:       "relay",
		Branch:           "main",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    auditFixtureExecutionSpec("audit-test", "main", strings.Repeat("a", 40)),
		RenderedMarkdown: []byte("# Executor Brief\n\nExact task.\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Load(context.Background(), created.Run.RunID); !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
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
			if evidence.EffectiveBrief.Mode != mode {
				t.Fatalf("mode = %q, want %q", evidence.EffectiveBrief.Mode, mode)
			}
			wantAdaptive := mode != executor.EffectiveExecutorBriefDeterministicComplete
			if evidence.EffectiveBrief.AdaptiveDispatchRequired != wantAdaptive {
				t.Fatalf("adaptive dispatch = %v, want %v", evidence.EffectiveBrief.AdaptiveDispatchRequired, wantAdaptive)
			}
			if evidence.Assignment.Artifact.ID != fixture.assignment.Artifact.ID || evidence.Assignment.Artifact.SHA256 != fixture.assignment.Artifact.SHA256 {
				t.Fatalf("assignment artifact = %#v", evidence.Assignment.Artifact)
			}
			if evidence.Deterministic.Artifact.ID != fixture.outcome.Artifact.ID || evidence.Deterministic.Artifact.SHA256 != fixture.outcome.Artifact.SHA256 {
				t.Fatalf("outcome artifact = %#v", evidence.Deterministic.Artifact)
			}
			if evidence.EffectiveBrief.Artifact == nil || fixture.brief.Artifact == nil || evidence.EffectiveBrief.Artifact.ID != fixture.brief.Artifact.ID || evidence.EffectiveBrief.Artifact.SHA256 != fixture.brief.Artifact.SHA256 {
				t.Fatalf("effective brief artifact = %#v", evidence.EffectiveBrief.Artifact)
			}
		})
	}
}

func TestWorkflowPackageExecutionEvidenceCompleteHasBriefWithoutAdaptiveDispatch(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefDeterministicComplete)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := service.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.EffectiveBrief.Artifact == nil || len(evidence.EffectiveBrief.Bytes) == 0 {
		t.Fatalf("effective brief = %#v", evidence.EffectiveBrief)
	}
	if evidence.EffectiveBrief.AdaptiveDispatchRequired {
		t.Fatal("complete mode required adaptive dispatch")
	}
}

func TestWorkflowPackageExecutionEvidenceRejectsCrossServiceMismatches(t *testing.T) {
	tests := []struct {
		name    string
		mode    executor.EffectiveExecutorBriefMode
		corrupt func(t *testing.T, service *WorkflowPackageExecutionEvidenceService)
	}{
		{
			name: "mode mismatch",
			mode: executor.EffectiveExecutorBriefDeterministicComplete,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadBrief
				service.loadBrief = func(ctx context.Context, runID string) (executor.EffectiveExecutorBriefResult, error) {
					brief, err := real(ctx, runID)
					if err != nil {
						return brief, err
					}
					brief.Mode = executor.EffectiveExecutorBriefAdaptiveNoOperations
					return brief, nil
				}
			},
		},
		{
			name: "dispatch requirement mismatch",
			mode: executor.EffectiveExecutorBriefDeterministicComplete,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadBrief
				service.loadBrief = func(ctx context.Context, runID string) (executor.EffectiveExecutorBriefResult, error) {
					brief, err := real(ctx, runID)
					if err != nil {
						return brief, err
					}
					brief.AdaptiveDispatchRequired = !brief.AdaptiveDispatchRequired
					return brief, nil
				}
			},
		},
		{
			name: "run package identity mismatch",
			mode: executor.EffectiveExecutorBriefAdaptiveNoOperations,
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
			mode: executor.EffectiveExecutorBriefAdaptiveNoOperations,
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
			name: "outcome assignment mismatch",
			mode: executor.EffectiveExecutorBriefAdaptiveNoOperations,
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
			name: "effective brief ownership mismatch",
			mode: executor.EffectiveExecutorBriefAdaptiveNoOperations,
			corrupt: func(t *testing.T, service *WorkflowPackageExecutionEvidenceService) {
				real := service.loadBrief
				service.loadBrief = func(ctx context.Context, runID string) (executor.EffectiveExecutorBriefResult, error) {
					brief, err := real(ctx, runID)
					if err != nil {
						return brief, err
					}
					artifact := *brief.Artifact
					artifact.RunRowID.Int64++
					brief.Artifact = &artifact
					return brief, nil
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

func TestWorkflowPackageExecutionEvidenceMalformedArtifactFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(a *workflowstore.Artifact)
	}{
		{name: "path", mutate: func(a *workflowstore.Artifact) { a.RelativePath = "" }},
		{name: "media type", mutate: func(a *workflowstore.Artifact) { a.MediaType = "" }},
		{name: "digest", mutate: func(a *workflowstore.Artifact) { a.SHA256 = "not-a-real-digest" }},
		{name: "size", mutate: func(a *workflowstore.Artifact) { a.SizeBytes++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
			service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			real := service.loadBrief
			service.loadBrief = func(ctx context.Context, runID string) (executor.EffectiveExecutorBriefResult, error) {
				brief, err := real(ctx, runID)
				if err != nil {
					return brief, err
				}
				artifact := *brief.Artifact
				test.mutate(&artifact)
				brief.Artifact = &artifact
				return brief, nil
			}
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
	t.Run("missing effective brief", func(t *testing.T) {
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
		if _, err := outcomes.Persist(context.Background(), packageEvidenceOutcomeInput(fixture.run.RunID, executor.EffectiveExecutorBriefAdaptiveNoOperations, "")); err != nil {
			t.Fatal(err)
		}
		service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Load(context.Background(), fixture.run.RunID)
		if err == nil {
			t.Fatal("missing effective brief was accepted")
		}
		if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			t.Fatalf("missing effective brief error = %v", err)
		}
	})
}

func TestWorkflowPackageExecutionEvidenceDuplicateFromLoaderIsConflict(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
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
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
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

func TestWorkflowPackageExecutionEvidenceEffectiveBriefConflictIsClassified(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.loadBrief = func(context.Context, string) (executor.EffectiveExecutorBriefResult, error) {
		return executor.EffectiveExecutorBriefResult{}, executor.ErrEffectiveExecutorBriefConflict
	}
	_, err = service.Load(context.Background(), fixture.run.RunID)
	if !errors.Is(err, executor.ErrEffectiveExecutorBriefConflict) {
		t.Fatalf("effective brief conflict error = %v", err)
	}
	if !errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
		t.Fatalf("effective brief conflict not classified as audit conflict: %v", err)
	}
}

func TestWorkflowPackageExecutionEvidenceApprovedAuthorityInvalidIsClassified(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
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
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
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
			fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptivePreflightFailed)
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
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefDeterministicComplete)
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
	if !reflect.DeepEqual(first.Assignment, second.Assignment) || !reflect.DeepEqual(first.Deterministic, second.Deterministic) || !reflect.DeepEqual(first.EffectiveBrief, second.EffectiveBrief) {
		t.Fatal("repeated Load returned different evidence")
	}
	if first.Run.ID != second.Run.ID || first.Authority.Package.PackageID != second.Authority.Package.PackageID {
		t.Fatal("repeated Load returned different run or authority identity")
	}
}

func TestWorkflowPackageExecutionEvidenceLoadPerformsNoWrites(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefDeterministicComplete)
	service, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
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
