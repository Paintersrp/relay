package audits

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowPackageAuditRecordDecisionAccepted(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	attachPackageRunToEligiblePass(t, fixture)
	ctx := context.Background()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RecordDecision(ctx, RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: packet.AuditPacketID, PacketSHA256: packet.PacketSHA256,
		AuditedCommit: packet.AuditedCommit, Decision: workflowstore.AuditDecisionAccepted,
		Rationale: "The exact approved package satisfies its obligations.", OperatorConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != workflowstore.RunStatusCompleted || result.Pass == nil || result.Pass.Status != workflowstore.PassStatusCompleted || result.Plan == nil || result.Plan.Status != workflowstore.PlanStatusCompleted || len(result.TicketRevisionDecisions) != 1 || len(result.TicketSatisfactions) != 1 || len(result.RemediationSeeds) != 0 {
		t.Fatalf("accepted package decision = %#v", result)
	}
	assertExactPackageDecisionBindings(t, fixture, packet, result)
}

func TestWorkflowPackageAuditRecordDecisionRejectsCoherentlyAlteredPacket(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	ctx := context.Background()
	var document WorkflowPackageAuditPacket
	if err := json.Unmarshal(currentPackagePacketBytes(t, fixture), &document); err != nil {
		t.Fatal(err)
	}
	document.Run.UserIntent = "coherently altered packet"
	altered, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	replaceCurrentPackagePacketBytes(t, fixture, append(altered, '\n'))
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RecordDecision(ctx, RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: packet.AuditPacketID, PacketSHA256: packet.PacketSHA256,
		AuditedCommit: packet.AuditedCommit, Decision: workflowstore.AuditDecisionAccepted,
		Rationale: "The altered packet must not be decided.", OperatorConfirmed: true,
	})
	if !errors.Is(err, ErrWorkflowAuditPacketStale) {
		t.Fatalf("error = %v, want ErrWorkflowAuditPacketStale", err)
	}
}

func TestWorkflowPackageAuditRecordDecisionNeedsRevision(t *testing.T) {
	for _, source := range []string{"implementation", "governing_package", "both"} {
		t.Run(source, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
			attachPackageRunToEligiblePass(t, fixture)
			ctx := context.Background()
			packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.RecordDecision(ctx, RecordWorkflowAuditDecisionInput{
				RunID: fixture.run.RunID, AuditPacketID: packet.AuditPacketID, PacketSHA256: packet.PacketSHA256,
				AuditedCommit: packet.AuditedCommit, Decision: workflowstore.AuditDecisionNeedsRevision,
				Rationale: "The package needs a revision.", OperatorConfirmed: true,
				MaterialFindings: []WorkflowAuditMaterialFinding{{Source: source, Summary: "Missing proof", Evidence: "The packet lacks the required proof.", RequiredRemediation: "Add the proof."}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Run.Status != workflowstore.RunStatusNeedsRevision || result.Pass != nil || result.Plan != nil || len(result.TicketRevisionDecisions) != 1 || len(result.TicketSatisfactions) != 0 || len(result.RemediationSeeds) != 1 {
				t.Fatalf("needs-revision package decision = %#v", result)
			}
			findings, err := fixture.store.ListAuditRemediationSeedFindings(ctx, result.RemediationSeeds[0].ID)
			if err != nil || len(findings) != 1 || findings[0].UpstreamClassification != source || findings[0].Sequence != 1 {
				t.Fatalf("remediation seed findings = %#v, %v", findings, err)
			}
			seed := result.RemediationSeeds[0]
			if seed.AuditTicketRevisionDecisionRowID != result.TicketRevisionDecisions[0].ID || seed.AuditPacketRowID != packet.ID || seed.ExecutionPackageRowID != fixture.run.ExecutionPackageRowID.Int64 || seed.AuditedCommit != result.Decision.AuditedCommit || seed.DecisionRationale != result.Decision.Rationale {
				t.Fatalf("remediation seed basis = %#v", seed)
			}
			effectsValue, err := service.GetAuditEffects(ctx, result.Decision.AuditDecisionID)
			if err != nil {
				t.Fatal(err)
			}
			effects := effectsValue.(AuditEffects)
			seedValue, err := service.GetRemediationSeed(ctx, seed.RemediationSeedID)
			if err != nil {
				t.Fatal(err)
			}
			seedDetail := seedValue.(RemediationSeedDetail)
			if len(effects.RemediationSeeds) != 1 || effects.RemediationSeeds[0] != seed || seedDetail.RemediationSeed != seed || len(seedDetail.MaterialFindings) != 1 || seedDetail.MaterialFindings[0] != findings[0] {
				t.Fatalf("durable remediation seed readback = %#v %#v", effects, seedDetail)
			}
			pass, err := fixture.store.GetPlanPassByRowID(ctx, fixture.run.PlanPassRowID.Int64)
			if err != nil || pass.Status != workflowstore.PassStatusInProgress {
				t.Fatalf("needs-revision pass = %#v, err=%v", pass, err)
			}
			plan, err := fixture.store.GetPlanByRowID(ctx, fixture.run.PlanRowID.Int64)
			if err != nil || plan.Status != workflowstore.PlanStatusActive {
				t.Fatalf("needs-revision plan = %#v, err=%v", plan, err)
			}
			assertExactPackageDecisionBindings(t, fixture, packet, result)
		})
	}
}

func TestWorkflowPackageAuditRecordDecisionInputValidation(t *testing.T) {
	tests := []struct {
		name  string
		input func(string, workflowstore.AuditPacket) RecordWorkflowAuditDecisionInput
	}{
		{
			name: "accepted_rejects_material_finding",
			input: func(runID string, packet workflowstore.AuditPacket) RecordWorkflowAuditDecisionInput {
				return packageDecisionInput(runID, packet, workflowstore.AuditDecisionAccepted, []WorkflowAuditMaterialFinding{{Source: "implementation", Summary: "finding", Evidence: "evidence", RequiredRemediation: "remediate"}})
			},
		},
		{
			name: "needs_revision_rejects_zero_findings",
			input: func(runID string, packet workflowstore.AuditPacket) RecordWorkflowAuditDecisionInput {
				return packageDecisionInput(runID, packet, workflowstore.AuditDecisionNeedsRevision, nil)
			},
		},
		{
			name: "needs_revision_rejects_missing_summary",
			input: func(runID string, packet workflowstore.AuditPacket) RecordWorkflowAuditDecisionInput {
				return packageDecisionInput(runID, packet, workflowstore.AuditDecisionNeedsRevision, []WorkflowAuditMaterialFinding{{Source: "implementation", Evidence: "evidence", RequiredRemediation: "remediate"}})
			},
		},
		{
			name: "needs_revision_rejects_missing_evidence",
			input: func(runID string, packet workflowstore.AuditPacket) RecordWorkflowAuditDecisionInput {
				return packageDecisionInput(runID, packet, workflowstore.AuditDecisionNeedsRevision, []WorkflowAuditMaterialFinding{{Source: "implementation", Summary: "finding", RequiredRemediation: "remediate"}})
			},
		},
		{
			name: "needs_revision_rejects_missing_required_remediation",
			input: func(runID string, packet workflowstore.AuditPacket) RecordWorkflowAuditDecisionInput {
				return packageDecisionInput(runID, packet, workflowstore.AuditDecisionNeedsRevision, []WorkflowAuditMaterialFinding{{Source: "implementation", Summary: "finding", Evidence: "evidence"}})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
			packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.RecordDecision(context.Background(), test.input(fixture.run.RunID, packet)); !errors.Is(err, ErrWorkflowAuditDecisionInput) {
				t.Fatalf("error = %v, want ErrWorkflowAuditDecisionInput", err)
			}
			assertNoPackageDecisionEffects(t, fixture)
		})
	}
}

func TestWorkflowPackageAuditRecordDecisionUsesPackageRouteOnly(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	service.packetValidator = func([]byte) (bool, error) {
		t.Fatal("package decision invoked the legacy packet validator")
		return false, nil
	}
	packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordDecision(context.Background(), packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionAccepted, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowPackageAuditRecordDecisionWithoutEvidenceLoader(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	service.loadPackageEvidence = nil
	packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordDecision(context.Background(), packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionAccepted, nil)); !errors.Is(err, ErrWorkflowAuditPackageUnavailable) {
		t.Fatalf("error = %v, want ErrWorkflowAuditPackageUnavailable", err)
	}
}

func TestWorkflowPackageAuditRecordDecisionPreservesLoaderAndInspectorErrors(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*WorkflowAuditService, error)
		wantStale    bool
		wantSentinel bool
	}{
		{
			name: "package_evidence_loader_infrastructure_failure", wantSentinel: true,
			setup: func(service *WorkflowAuditService, sentinel error) {
				service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
					return WorkflowPackageExecutionEvidence{}, sentinel
				}
			},
		},
		{
			name: "semantic_package_evidence_conflict", wantStale: true,
			setup: func(service *WorkflowAuditService, _ error) {
				service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
					return WorkflowPackageExecutionEvidence{}, ErrWorkflowPackageExecutionEvidenceConflict
				}
			},
		},
		{
			name: "repository_inspector_infrastructure_failure", wantSentinel: true,
			setup: func(service *WorkflowAuditService, sentinel error) {
				service.inspector = func(context.Context, string, string, string, string) (workflowrepos.AuditCommitEvidence, error) {
					return workflowrepos.AuditCommitEvidence{}, sentinel
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
			packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New(test.name)
			test.setup(service, sentinel)
			_, decisionErr := service.RecordDecision(context.Background(), packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionAccepted, nil))
			if test.wantStale && !errors.Is(decisionErr, ErrWorkflowAuditPacketStale) {
				t.Fatalf("error = %v, want stale", decisionErr)
			}
			if test.wantSentinel && (!errors.Is(decisionErr, sentinel) || errors.Is(decisionErr, ErrWorkflowAuditPacketStale)) {
				t.Fatalf("error = %v, want preserved sentinel without bare stale", decisionErr)
			}
			assertNoPackageDecisionEffects(t, fixture)
		})
	}
}

func TestWorkflowPackageAuditRecordDecisionRejectsSecondAttemptWithoutEffects(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionAccepted, nil)
	if _, err := service.RecordDecision(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	before := capturePackageDecisionState(t, fixture)
	if _, err := service.RecordDecision(context.Background(), input); !errors.Is(err, ErrWorkflowAuditDecisionRecorded) {
		t.Fatalf("second decision error = %v, want duplicate decision", err)
	}
	assertPackageDecisionState(t, fixture, before)
}

func TestWorkflowPackageAuditRecordDecisionAuthorityDriftRollsBack(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*packageEvidenceFixture, WorkflowPackageExecutionEvidence) error
	}{
		{name: "execution_package_row_identity", mutate: mutatePackageRowIdentity},
		{name: "execution_package_digest", mutate: mutatePackageDigest},
		{name: "package_approval_row_identity", mutate: mutatePackageApprovalIdentity},
		{name: "approved_package_digest", mutate: mutateApprovedPackageDigest},
		{name: "execution_package_member_identity", mutate: mutateExecutionPackageMemberIdentity},
		{name: "current_delivery_ticket_revision", mutate: mutateCurrentDeliveryTicketRevision},
		{name: "ticket_approval_identity", mutate: mutateTicketApprovalIdentity},
		{name: "ticket_approval_state", mutate: mutateTicketApprovalState},
		{name: "current_workspace_authority_revision", mutate: mutateCurrentWorkspaceAuthorityRevision},
		{name: "source_closure_identity", mutate: mutateSourceClosureIdentity},
		{name: "source_closure_ready_state", mutate: mutateSourceClosureReadyState},
		{name: "source_commit", mutate: mutateSourceCommit},
		{name: "obligation_package_approval", mutate: mutateObligationPackageApproval},
		{name: "obligation_approved_digest", mutate: mutateObligationApprovedDigest},
		{name: "obligation_execution_package_member", mutate: mutateObligationMember},
		{name: "packet_artifact_digest", mutate: mutatePacketArtifactDigest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
			packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := service.loadPackageEvidence(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			state := capturePackageDecisionState(t, fixture)
			calls := 0
			baseInspector := packagePrepareTestInspector()
			service.inspector = func(ctx context.Context, localPath, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
				calls++
				if calls == 1 {
					if err := test.mutate(fixture, evidence); err != nil {
						t.Fatal(err)
					}
				}
				return baseInspector(ctx, localPath, branch, baseCommit, auditedCommit)
			}
			_, err = service.RecordDecision(context.Background(), packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionAccepted, nil))
			if !errors.Is(err, ErrWorkflowAuditPacketStale) && !errors.Is(err, ErrWorkflowAuditTicketIneligible) {
				t.Fatalf("error = %v, want stale or ineligible", err)
			}
			assertPackageDecisionState(t, fixture, state)
		})
	}
}

type packageDecisionState struct {
	run                              workflowstore.Run
	pass                             workflowstore.PlanPass
	plan                             workflowstore.Plan
	passStatus, planStatus           string
	decisionRows, decisionArtifacts  int
	ticketDecisions, satisfactions   int
	remediationSeeds, seedFindings   int
	decisionDirectories, stagingDirs []string
}

func packageDecisionInput(runID string, packet workflowstore.AuditPacket, decision string, findings []WorkflowAuditMaterialFinding) RecordWorkflowAuditDecisionInput {
	return RecordWorkflowAuditDecisionInput{RunID: runID, AuditPacketID: packet.AuditPacketID, PacketSHA256: packet.PacketSHA256, AuditedCommit: packet.AuditedCommit, Decision: decision, Rationale: "test decision rationale", OperatorConfirmed: true, MaterialFindings: findings}
}

func assertExactPackageDecisionBindings(t *testing.T, fixture *packageEvidenceFixture, packet workflowstore.AuditPacket, result RecordWorkflowAuditDecisionResult) {
	t.Helper()
	obligations, err := fixture.store.ListAuditPacketTicketObligations(context.Background(), packet.ID)
	if err != nil || len(obligations) != len(result.TicketRevisionDecisions) {
		t.Fatalf("obligations=%d decisions=%d err=%v", len(obligations), len(result.TicketRevisionDecisions), err)
	}
	approval, err := fixture.store.GetRunExecutionPackageApproval(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, decision := range result.TicketRevisionDecisions {
		obligation := obligations[i]
		if decision.AuditDecisionRowID != result.Decision.ID || decision.AuditPacketTicketObligationRowID != obligation.ID || !decision.PackageApprovalRowID.Valid || decision.PackageApprovalRowID.Int64 != approval.ID || !decision.ApprovedPackageSha256.Valid || decision.ApprovedPackageSha256.String != approval.PackageSha256 {
			t.Fatalf("decision binding = %#v, obligation=%#v, approval=%#v", decision, obligation, approval)
		}
	}
	if len(result.TicketSatisfactions) == 0 {
		return
	}
	for i, satisfaction := range result.TicketSatisfactions {
		if satisfaction.DeliveryTicketRevisionRowID != obligations[i].DeliveryTicketRevisionRowID || satisfaction.AuditTicketRevisionDecisionRowID != result.TicketRevisionDecisions[i].ID {
			t.Fatalf("satisfaction binding = %#v, obligation=%#v, decision=%#v", satisfaction, obligations[i], result.TicketRevisionDecisions[i])
		}
	}
}

func assertNoPackageDecisionEffects(t *testing.T, fixture *packageEvidenceFixture) {
	t.Helper()
	state := capturePackageDecisionState(t, fixture)
	if state.decisionRows != 0 || state.decisionArtifacts != 0 || state.ticketDecisions != 0 || state.satisfactions != 0 || state.remediationSeeds != 0 || state.seedFindings != 0 {
		t.Fatalf("unexpected decision effects = %#v", state)
	}
}

func capturePackageDecisionState(t *testing.T, fixture *packageEvidenceFixture) packageDecisionState {
	t.Helper()
	ctx := context.Background()
	state := packageDecisionState{}
	var err error
	state.run, err = fixture.store.GetRunByRunID(ctx, fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.run.PlanPassRowID.Valid {
		state.pass, err = fixture.store.GetPlanPassByRowID(ctx, state.run.PlanPassRowID.Int64)
		if err != nil {
			t.Fatal(err)
		}
		state.passStatus = state.pass.Status
	}
	if state.run.PlanRowID.Valid {
		state.plan, err = fixture.store.GetPlanByRowID(ctx, state.run.PlanRowID.Int64)
		if err != nil {
			t.Fatal(err)
		}
		state.planStatus = state.plan.Status
	}
	queries := []struct {
		query string
		into  *int
	}{
		{`SELECT COUNT(*) FROM audit_decisions WHERE run_row_id = ?`, &state.decisionRows},
		{`SELECT COUNT(*) FROM artifacts WHERE run_row_id = ? AND kind = 'audit_decision'`, &state.decisionArtifacts},
		{`SELECT COUNT(*) FROM audit_ticket_revision_decisions WHERE audit_decision_row_id IN (SELECT id FROM audit_decisions WHERE run_row_id = ?)`, &state.ticketDecisions},
		{`SELECT COUNT(*) FROM delivery_ticket_revision_satisfactions WHERE audit_ticket_revision_decision_row_id IN (SELECT id FROM audit_ticket_revision_decisions WHERE audit_decision_row_id IN (SELECT id FROM audit_decisions WHERE run_row_id = ?))`, &state.satisfactions},
		{`SELECT COUNT(*) FROM audit_remediation_seeds WHERE audit_ticket_revision_decision_row_id IN (SELECT id FROM audit_ticket_revision_decisions WHERE audit_decision_row_id IN (SELECT id FROM audit_decisions WHERE run_row_id = ?))`, &state.remediationSeeds},
		{`SELECT COUNT(*) FROM audit_remediation_seed_findings WHERE remediation_seed_row_id IN (SELECT id FROM audit_remediation_seeds WHERE audit_ticket_revision_decision_row_id IN (SELECT id FROM audit_ticket_revision_decisions WHERE audit_decision_row_id IN (SELECT id FROM audit_decisions WHERE run_row_id = ?)))`, &state.seedFindings},
	}
	for _, item := range queries {
		if err := fixture.store.DB().QueryRowContext(ctx, item.query, state.run.ID).Scan(item.into); err != nil {
			t.Fatal(err)
		}
	}
	state.decisionDirectories = packageDecisionDirectories(t, fixture, "audit-decisions")
	state.stagingDirs = packageDecisionDirectories(t, fixture, filepath.Join(".staging"))
	return state
}

func assertPackageDecisionState(t *testing.T, fixture *packageEvidenceFixture, want packageDecisionState) {
	t.Helper()
	got := capturePackageDecisionState(t, fixture)
	if got.run.Status != want.run.Status || got.passStatus != want.passStatus || got.planStatus != want.planStatus || got.decisionRows != want.decisionRows || got.decisionArtifacts != want.decisionArtifacts || got.ticketDecisions != want.ticketDecisions || got.satisfactions != want.satisfactions || got.remediationSeeds != want.remediationSeeds || got.seedFindings != want.seedFindings || !reflect.DeepEqual(got.decisionDirectories, want.decisionDirectories) || !reflect.DeepEqual(got.stagingDirs, want.stagingDirs) {
		t.Fatalf("decision state after failure = %#v, want %#v", got, want)
	}
}

func packageDecisionDirectories(t *testing.T, fixture *packageEvidenceFixture, relative string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixture.store.ArtifactStore().Root(), relative))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry.Name())
		}
	}
	return result
}

func attachPackageRunToEligiblePass(t *testing.T, fixture *packageEvidenceFixture) {
	t.Helper()
	ctx := context.Background()
	var planID int64
	if err := fixture.store.DB().QueryRow(`SELECT id FROM plans WHERE plan_id = 'plan-package'`).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec(`INSERT INTO plan_repository_targets (plan_row_id, sequence, repo_target, branch, planning_base_commit) VALUES (?, 1, 'relay', 'main', ?)`, planID, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	var passID int64
	if err := fixture.store.DB().QueryRow(`INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target) VALUES ('pass-package-audit', ?, 1, 'Package audit pass', 'relay') RETURNING id`, planID).Scan(&passID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionPlanPass(ctx, "pass-package-audit", workflowstore.PassStatusPlanned, workflowstore.PassStatusInProgress)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec(`UPDATE runs SET plan_row_id = ?, plan_pass_row_id = ? WHERE id = ?`, planID, passID, fixture.run.ID); err != nil {
		t.Fatal(err)
	}
	fixture.run, _ = fixture.store.GetRunByRunID(ctx, fixture.run.RunID)
}

func mutatePackageRowIdentity(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS execution_package_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE execution_packages SET package_id = 'package-drifted' WHERE id = ?`, e.Authority.Package.ID)
	return err
}

func mutatePackageDigest(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS execution_package_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE execution_packages SET package_sha256 = ? WHERE id = ?`, strings.Repeat("d", 64), e.Authority.Package.ID)
	return err
}

func mutatePackageApprovalIdentity(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS execution_package_approval_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE execution_package_approvals SET approval_id = 'pkg-approval-drifted' WHERE id = ?`, e.Authority.PackageApproval.ID)
	return err
}

func mutateApprovedPackageDigest(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS execution_package_approval_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE execution_package_approvals SET package_sha256 = ? WHERE id = ?`, strings.Repeat("d", 64), e.Authority.PackageApproval.ID)
	return err
}

func mutateExecutionPackageMemberIdentity(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS execution_package_member_update_immutable`); err != nil {
		return err
	}
	var memberID int64
	if err := f.store.DB().QueryRow(`SELECT id FROM execution_package_members WHERE package_row_id = ? ORDER BY id LIMIT 1`, e.Authority.Package.ID).Scan(&memberID); err != nil {
		return err
	}
	newRevisionID, err := createReplacementTicketRevision(f, e)
	if err != nil {
		return err
	}
	_, err = f.store.DB().Exec(`UPDATE execution_package_members SET revision_row_id = ? WHERE id = ?`, newRevisionID, memberID)
	return err
}

func mutateCurrentDeliveryTicketRevision(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	newRevisionID, err := createReplacementTicketRevision(f, e)
	if err != nil {
		return err
	}
	_, err = f.store.DB().Exec(`UPDATE delivery_tickets SET current_revision_row_id = ? WHERE id = ?`, newRevisionID, e.Authority.Ticket.ID)
	return err
}

func mutateTicketApprovalIdentity(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS delivery_ticket_approval_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE delivery_ticket_revision_approvals SET approval_id = 'approval-drifted' WHERE id = ?`, e.Authority.TicketApproval.ID)
	return err
}

func mutateTicketApprovalState(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS delivery_ticket_approval_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE delivery_ticket_revision_approvals SET approval_state = 'rejected' WHERE id = ?`, e.Authority.TicketApproval.ID)
	return err
}

func mutateCurrentWorkspaceAuthorityRevision(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	var authorityID int64
	if err := f.store.DB().QueryRow(`INSERT INTO feature_workspace_authority_revisions (authority_revision_id, workspace_row_id, revision_number, source_closure_row_id) VALUES ('authority-drifted', ?, ?, ?) RETURNING id`, e.Authority.Workspace.ID, e.Authority.Authority.RevisionNumber+1, e.Authority.Source.ID).Scan(&authorityID); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE feature_workspaces SET current_authority_revision_row_id = ?, version = version + 1 WHERE id = ?`, authorityID, e.Authority.Workspace.ID)
	return err
}

func mutateSourceClosureIdentity(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS source_vault_closure_identity_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE source_vault_closures SET closure_id = 'closure-drifted' WHERE id = ?`, e.Authority.Source.ID)
	return err
}

func mutateSourceClosureReadyState(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	_, err := f.store.DB().Exec(`UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'source_commit_missing', verified_at = NULL WHERE id = ?`, e.Authority.Source.ID)
	return err
}

func mutateSourceCommit(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS source_vault_closure_identity_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE source_vault_closures SET commit_oid = ? WHERE id = ?`, strings.Repeat("d", 40), e.Authority.Source.ID)
	return err
}

func mutateObligationPackageApproval(f *packageEvidenceFixture, _ WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS audit_packet_ticket_obligation_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE audit_packet_ticket_obligations SET package_approval_row_id = NULL WHERE audit_packet_row_id = (SELECT id FROM audit_packets WHERE run_row_id = ? AND status = 'current')`, f.run.ID)
	return err
}

func mutateObligationApprovedDigest(f *packageEvidenceFixture, _ WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS audit_packet_ticket_obligation_update_immutable`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE audit_packet_ticket_obligations SET approved_package_sha256 = ? WHERE audit_packet_row_id = (SELECT id FROM audit_packets WHERE run_row_id = ? AND status = 'current')`, strings.Repeat("d", 64), f.run.ID)
	return err
}

func mutateObligationMember(f *packageEvidenceFixture, _ WorkflowPackageExecutionEvidence) error {
	if _, err := f.store.DB().Exec(`DROP TRIGGER IF EXISTS audit_packet_ticket_obligation_update_immutable`); err != nil {
		return err
	}
	if _, err := f.store.DB().Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	_, err := f.store.DB().Exec(`UPDATE audit_packet_ticket_obligations SET execution_package_member_row_id = execution_package_member_row_id + 1 WHERE audit_packet_row_id = (SELECT id FROM audit_packets WHERE run_row_id = ? AND status = 'current')`, f.run.ID)
	return err
}

func mutatePacketArtifactDigest(f *packageEvidenceFixture, _ WorkflowPackageExecutionEvidence) error {
	_, err := f.store.DB().Exec(`UPDATE artifacts SET sha256 = ? WHERE id = (SELECT artifact_row_id FROM audit_packets WHERE run_row_id = ? AND status = 'current')`, strings.Repeat("d", 64), f.run.ID)
	return err
}

func createReplacementTicketRevision(f *packageEvidenceFixture, e WorkflowPackageExecutionEvidence) (int64, error) {
	r := e.Authority.TicketRevision
	var id int64
	err := f.store.DB().QueryRow(`INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, replaces_revision_row_id, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, r.DeliveryTicketRowID, r.RevisionNumber+1, r.ID, r.RepoTarget, r.Branch, r.BaseCommit, r.SourceClosureRowID, r.SourcePath, r.Goal, r.Context, r.TransitionApplicability).Scan(&id)
	return id, err
}

func TestWorkflowPackageAuditRecordDecisionPersistsImmutableArtifact(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	ctx := context.Background()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := service.loadPackageEvidence(ctx, fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	loadCount := 0
	service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
		loadCount++
		return evidence, nil
	}
	input := RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: packet.AuditPacketID, PacketSHA256: packet.PacketSHA256,
		AuditedCommit: packet.AuditedCommit, Decision: workflowstore.AuditDecisionNeedsRevision,
		Rationale: "The package needs one precise revision.", OperatorConfirmed: true,
		MaterialFindings: []WorkflowAuditMaterialFinding{{Source: "governing_package", Summary: "Missing proof", Evidence: "The packet lacks the required proof.", RequiredRemediation: "Add the required proof."}},
		Observations:     []string{"The persisted packet was reconstructed exactly."},
	}
	result, err := service.RecordDecision(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if loadCount != 1 {
		t.Fatalf("package evidence load count = %d, want 1", loadCount)
	}

	data, err := readWorkflowArtifact(fixture.store, result.Artifact, MaxWorkflowAuditPacketBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' || sha256HexBytes(data) != result.Artifact.SHA256 || result.Artifact.SizeBytes != int64(len(data)) {
		t.Fatalf("decision artifact integrity = size %d digest %q", len(data), result.Artifact.SHA256)
	}
	var document workflowPackageDecisionDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.AuditDecisionID != result.Decision.AuditDecisionID || document.RunID != fixture.run.RunID || document.RunRowID != fixture.run.ID ||
		document.Decision != string(input.Decision) || document.Rationale != input.Rationale || len(document.MaterialFindings) != 1 || document.MaterialFindings[0] != input.MaterialFindings[0] ||
		len(document.Observations) != 1 || document.Observations[0] != input.Observations[0] || document.AuditPacketID != packet.AuditPacketID || document.AuditPacketRowID != packet.ID ||
		document.AuditPacketArtifactRowID != packet.ArtifactRowID || document.PacketSHA256 != packet.PacketSHA256 || document.AuditedCommit != packet.AuditedCommit ||
		document.ExecutionPackageID != evidence.Authority.Package.PackageID || document.ExecutionPackageRowID != evidence.Authority.Package.ID || document.PackageSHA256 != evidence.Authority.Package.PackageSha256 ||
		document.PackageApprovalID != evidence.Authority.PackageApproval.ApprovalID || document.PackageApprovalRowID != evidence.Authority.PackageApproval.ID || document.ApprovedPackageSHA256 != evidence.Authority.PackageApproval.PackageSha256 ||
		document.DeliveryTicketID != evidence.Authority.Ticket.TicketID || document.DeliveryTicketRowID != evidence.Authority.Ticket.ID || document.DeliveryTicketRevisionRowID != evidence.Authority.TicketRevision.ID ||
		document.DeliveryTicketRevisionNumber != evidence.Authority.TicketRevision.RevisionNumber || document.DeliveryTicketApprovalID != evidence.Authority.TicketApproval.ApprovalID || document.DeliveryTicketApprovalRowID != evidence.Authority.TicketApproval.ID ||
		document.AuthorityRevisionID != evidence.Authority.Authority.AuthorityRevisionID || document.AuthorityRevisionRowID != evidence.Authority.Authority.ID || document.SourceClosureID != evidence.Authority.Source.ClosureID ||
		document.SourceClosureRowID != evidence.Authority.Source.ID || document.SourceCommit != evidence.Authority.Source.CommitOID {
		t.Fatalf("immutable package decision document = %#v", document)
	}
}

func TestWorkflowPackageAuditRecordDecisionRejectsLegacyFindingSource(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	ctx := context.Background()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RecordDecision(ctx, RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: packet.AuditPacketID, PacketSHA256: packet.PacketSHA256,
		AuditedCommit: packet.AuditedCommit, Decision: workflowstore.AuditDecisionNeedsRevision,
		Rationale: "Legacy source must not be translated.", OperatorConfirmed: true,
		MaterialFindings: []WorkflowAuditMaterialFinding{{Source: "executor_implementation", Summary: "Missing proof", Evidence: "Missing", RequiredRemediation: "Add proof"}},
	})
	if !errors.Is(err, ErrWorkflowAuditDecisionInput) {
		t.Fatalf("error = %v, want invalid input", err)
	}
	if strings.Contains(err.Error(), "stale") {
		t.Fatalf("legacy attribution was translated into a stale conflict: %v", err)
	}
}
