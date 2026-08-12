package audits

import (
	"context"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

// attachProgramDispatchLineage binds the fixture Run to one immutable Program
// dispatch through the exact prepared-member lineage and records the exact
// dispatch result facts. A nil branch leaves the dispatch result unrecorded.
func attachProgramDispatchLineage(t *testing.T, fixture *packageEvidenceFixture, pushedBranch, headSHA string) {
	t.Helper()
	ctx := context.Background()
	db := fixture.store.DB()
	var workspaceRow int64
	if err := db.QueryRowContext(ctx, `SELECT workspace_row_id FROM execution_packages WHERE id=?`, fixture.run.ExecutionPackageRowID.Int64).Scan(&workspaceRow); err != nil {
		t.Fatal(err)
	}
	var revisionRow int64
	if err := db.QueryRowContext(ctx, `SELECT delivery_ticket_revision_row_id FROM audit_packet_ticket_obligations WHERE audit_packet_row_id=(SELECT id FROM audit_packets WHERE run_row_id=?)`, fixture.run.ID).Scan(&revisionRow); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO program_prepared_members(prepared_member_id,workspace_row_id,execution_package_row_id,run_row_id,ticket_revision_row_id,assignment_artifact_row_id,repo_target,branch,base_commit,state) VALUES(?,?,?,?,?,?,?,?,?,'dispatched')`, "program-member-"+fixture.run.RunID, workspaceRow, fixture.run.ExecutionPackageRowID.Int64, fixture.run.ID, revisionRow, fixture.assignment.Artifact.ID, fixture.run.RepoTarget, fixture.run.Branch, fixture.run.BaseCommit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO program_dispatches(dispatch_id,workspace_row_id,repo_target,branch,base_commit) VALUES('dispatch-audit',?,?,?,?)`, workspaceRow, fixture.run.RepoTarget, fixture.run.Branch, fixture.run.BaseCommit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE program_dispatches SET status='reported' WHERE dispatch_id='dispatch-audit' AND status='dispatched'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO program_dispatch_members(dispatch_row_id,prepared_member_row_id,sequence) SELECT d.id,m.id,1 FROM program_dispatches d JOIN program_prepared_members m ON m.prepared_member_id=? WHERE d.dispatch_id='dispatch-audit'`, "program-member-"+fixture.run.RunID); err != nil {
		t.Fatal(err)
	}
	if pushedBranch == "" {
		return
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO program_dispatch_results(dispatch_member_row_id,outcome,branch,branch_head_sha) SELECT dm.id,'done',?,? FROM program_dispatch_members dm WHERE dm.sequence=1 AND dm.dispatch_row_id=(SELECT id FROM program_dispatches WHERE dispatch_id='dispatch-audit')`, pushedBranch, headSHA); err != nil {
		t.Fatal(err)
	}
}

func recordProgramAcceptedDecision(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) RecordWorkflowAuditDecisionResult {
	t.Helper()
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
	return result
}

func programIntegrationEligibilityCount(t *testing.T, fixture *packageEvidenceFixture) int {
	t.Helper()
	var count int
	if err := fixture.store.DB().QueryRow(`SELECT count(*) FROM program_integration_eligibilities`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAcceptedStandaloneAuditRemainsOrdinaryCompletionWithoutEligibility(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	result := recordProgramAcceptedDecision(t, fixture, service)
	if len(result.TicketSatisfactions) != 1 || len(result.IntegrationEligibilities) != 0 {
		t.Fatalf("standalone accepted disposition = %#v", result)
	}
	if programIntegrationEligibilityCount(t, fixture) != 0 {
		t.Fatal("standalone accepted audit recorded integration eligibility")
	}
	if result.Run.Status != workflowstore.RunStatusCompleted {
		t.Fatalf("standalone accepted run status = %q", result.Run.Status)
	}
}

func TestAcceptedProgramBoundAuditRecordsDurableEligibilityOnly(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	auditedCommit := strings.Repeat("c", 40)
	attachProgramDispatchLineage(t, fixture, "feature/program", auditedCommit)
	result := recordProgramAcceptedDecision(t, fixture, service)
	if len(result.TicketRevisionDecisions) != 1 || len(result.TicketSatisfactions) != 0 || len(result.RemediationSeeds) != 0 || len(result.IntegrationEligibilities) != 1 {
		t.Fatalf("program-bound accepted disposition = %#v", result)
	}
	if result.Run.Status != workflowstore.RunStatusCompleted {
		t.Fatalf("program-bound accepted run status = %q", result.Run.Status)
	}
	eligibility := result.IntegrationEligibilities[0]
	if eligibility.AuditedCommit != auditedCommit || eligibility.PushedBranch != "feature/program" ||
		eligibility.AuditTicketRevisionDecisionRowID != result.TicketRevisionDecisions[0].ID ||
		eligibility.ExecutionPackageRowID != fixture.run.ExecutionPackageRowID.Int64 ||
		eligibility.AssignmentArtifactRowID != fixture.assignment.Artifact.ID {
		t.Fatalf("eligibility exact facts = %#v", eligibility)
	}
	var stored EligibilityRowFacts
	err := fixture.store.DB().QueryRow(`SELECT eligibility_id, dispatch_member_row_id, audit_ticket_revision_decision_row_id, delivery_ticket_revision_row_id, audited_commit, pushed_branch, execution_package_row_id, assignment_artifact_row_id, authority_revision_row_id, source_closure_row_id FROM program_integration_eligibilities`).Scan(&stored.EligibilityID, &stored.DispatchMemberRowID, &stored.AuditTicketRevisionDecisionRowID, &stored.DeliveryTicketRevisionRowID, &stored.AuditedCommit, &stored.PushedBranch, &stored.ExecutionPackageRowID, &stored.AssignmentArtifactRowID, &stored.AuthorityRevisionRowID, &stored.SourceClosureRowID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EligibilityID != eligibility.EligibilityID || stored.DispatchMemberRowID != eligibility.DispatchMemberRowID || stored.DeliveryTicketRevisionRowID != eligibility.DeliveryTicketRevisionRowID || stored.AuthorityRevisionRowID != eligibility.AuthorityRevisionRowID || stored.SourceClosureRowID != eligibility.SourceClosureRowID {
		t.Fatalf("stored eligibility = %#v, want %#v", stored, eligibility)
	}
	// The eligibility never records completion: no satisfaction, no dependency
	// outcome change, and no workspace completion decision.
	var satisfactions, completionDecisions int
	if err := fixture.store.DB().QueryRow(`SELECT count(*) FROM delivery_ticket_revision_satisfactions`).Scan(&satisfactions); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_completion_decisions`).Scan(&completionDecisions); err != nil {
		t.Fatal(err)
	}
	if satisfactions != 0 || completionDecisions != 0 {
		t.Fatalf("program-bound accepted audit advanced completion: satisfactions=%d completions=%d", satisfactions, completionDecisions)
	}
}

type EligibilityRowFacts struct {
	EligibilityID                    string
	DispatchMemberRowID              int64
	AuditTicketRevisionDecisionRowID int64
	DeliveryTicketRevisionRowID      int64
	AuditedCommit                    string
	PushedBranch                     string
	ExecutionPackageRowID            int64
	AssignmentArtifactRowID          int64
	AuthorityRevisionRowID           int64
	SourceClosureRowID               int64
}

func TestAcceptedProgramBoundAuditBlocksEligibilityOnMissingOrMismatchedFacts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lineage func(*testing.T, *packageEvidenceFixture)
		reason  string
	}{
		{"missing dispatch result", func(t *testing.T, fixture *packageEvidenceFixture) {
			attachProgramDispatchLineage(t, fixture, "", "")
		}, "no recorded accepted commit and pushed branch"},
		{"accepted commit mismatch", func(t *testing.T, fixture *packageEvidenceFixture) {
			attachProgramDispatchLineage(t, fixture, "feature/program", strings.Repeat("d", 40))
		}, "recorded pushed head differs from the audited commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
			tc.lineage(t, fixture)
			result := recordProgramAcceptedDecision(t, fixture, service)
			if len(result.TicketRevisionDecisions) != 1 || len(result.TicketSatisfactions) != 0 || len(result.IntegrationEligibilities) != 0 {
				t.Fatalf("blocked eligibility disposition = %#v (%s)", result, tc.reason)
			}
			if programIntegrationEligibilityCount(t, fixture) != 0 {
				t.Fatal("blocked eligibility recorded an eligibility row")
			}
			if result.Run.Status != workflowstore.RunStatusCompleted {
				t.Fatalf("decision still recorded, run status = %q", result.Run.Status)
			}
		})
	}
}

func TestProgramBoundNeedsRevisionRemainsOrdinaryRemediation(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	attachProgramDispatchLineage(t, fixture, "feature/program", strings.Repeat("c", 40))
	ctx := context.Background()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	findings := []WorkflowAuditMaterialFinding{{Source: "implementation", Summary: "summary", Evidence: "evidence", RequiredRemediation: "remediation"}}
	result, err := service.RecordDecision(ctx, RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: packet.AuditPacketID, PacketSHA256: packet.PacketSHA256,
		AuditedCommit: packet.AuditedCommit, Decision: workflowstore.AuditDecisionNeedsRevision,
		Rationale: "The package needs a revision.", OperatorConfirmed: true, MaterialFindings: findings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TicketRevisionDecisions) != 1 || len(result.TicketSatisfactions) != 0 || len(result.RemediationSeeds) != 1 || len(result.IntegrationEligibilities) != 0 {
		t.Fatalf("program-bound needs-revision disposition = %#v", result)
	}
	if programIntegrationEligibilityCount(t, fixture) != 0 {
		t.Fatal("needs-revision recorded integration eligibility")
	}
	if result.Run.Status != workflowstore.RunStatusNeedsRevision {
		t.Fatalf("needs-revision run status = %q", result.Run.Status)
	}
}
