package audits

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowPackageAuditRecordDecisionMultipleObligations(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	addMultiplePackageAuditObligations(t, fixture, service)
	ctx := context.Background()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	findings := []WorkflowAuditMaterialFinding{
		{Source: "implementation", Summary: "implementation gap", Evidence: "implementation evidence", RequiredRemediation: "repair implementation"},
		{Source: "both", Summary: "package gap", Evidence: "package evidence", RequiredRemediation: "repair package basis"},
	}
	before := capturePackageDecisionState(t, fixture)
	result, err := service.RecordDecision(ctx, packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionNeedsRevision, findings))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TicketRevisionDecisions) != 2 || len(result.TicketSatisfactions) != 0 || len(result.RemediationSeeds) != 2 {
		t.Fatalf("multiple-obligation result = %#v", result)
	}
	obligations, err := fixture.store.ListAuditPacketTicketObligations(ctx, packet.ID)
	if err != nil || len(obligations) != 2 {
		t.Fatalf("packet obligations = %#v, err=%v", obligations, err)
	}
	for index, decision := range result.TicketRevisionDecisions {
		if decision.AuditPacketTicketObligationRowID != obligations[index].ID {
			t.Fatalf("Ticket decision %d = %#v, obligation=%#v", index, decision, obligations[index])
		}
		seed := result.RemediationSeeds[index]
		if seed.AuditTicketRevisionDecisionRowID != decision.ID || seed.AuditPacketRowID != packet.ID || seed.ExecutionPackageRowID != fixture.run.ExecutionPackageRowID.Int64 || seed.AuditedCommit != result.Decision.AuditedCommit || seed.DecisionRationale != result.Decision.Rationale {
			t.Fatalf("seed %d = %#v, decision=%#v", index, seed, decision)
		}
		seedValue, err := service.GetRemediationSeed(ctx, seed.RemediationSeedID)
		if err != nil {
			t.Fatal(err)
		}
		seedDetail := seedValue.(RemediationSeedDetail)
		if len(seedDetail.MaterialFindings) != len(findings) {
			t.Fatalf("seed %d findings = %#v", index, seedDetail.MaterialFindings)
		}
		for findingIndex, finding := range seedDetail.MaterialFindings {
			want := findings[findingIndex]
			if finding.Sequence != int64(findingIndex+1) || finding.UpstreamClassification != want.Source || finding.Summary != want.Summary || finding.Evidence != want.Evidence || finding.RequiredRemediation != want.RequiredRemediation {
				t.Fatalf("seed %d finding %d = %#v, want %#v", index, findingIndex, finding, want)
			}
		}
	}
	if before.deliveryTickets != capturePackageDecisionState(t, fixture).deliveryTickets || before.deliveryRevisions != capturePackageDecisionState(t, fixture).deliveryRevisions {
		t.Fatal("seed persistence created Delivery Tickets or revisions")
	}
}

func TestWorkflowPackageAuditRecordDecisionNeedsRevisionDuplicateWithoutEffects(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	ctx := context.Background()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionNeedsRevision, []WorkflowAuditMaterialFinding{
		{Source: "implementation", Summary: "gap", Evidence: "evidence", RequiredRemediation: "remediate"},
	})
	if _, err := service.RecordDecision(ctx, input); err != nil {
		t.Fatal(err)
	}
	before := capturePackageDecisionState(t, fixture)
	if _, err := service.RecordDecision(ctx, input); !errors.Is(err, ErrWorkflowAuditDecisionRecorded) {
		t.Fatalf("second decision error = %v, want duplicate decision", err)
	}
	assertPackageDecisionState(t, fixture, before)
}

func TestWorkflowPackageAuditRecordDecisionKeepsExistingCompletionCurrent(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
	ctx := context.Background()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	completion := createCurrentPackageCompletion(t, fixture, packet)
	before := capturePackageDecisionState(t, fixture)
	result, err := service.RecordDecision(ctx, packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionNeedsRevision, []WorkflowAuditMaterialFinding{
		{Source: "governing_package", Summary: "gap", Evidence: "evidence", RequiredRemediation: "remediate"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemediationSeeds) != 1 || before.completionReopenings != 0 || before.seedReopenings != 0 {
		t.Fatalf("unexpected pre-decision completion state = %#v", before)
	}
	after := capturePackageDecisionState(t, fixture)
	if after.completionReopenings != 0 || after.seedReopenings != 0 || after.deliveryTickets != before.deliveryTickets || after.deliveryRevisions != before.deliveryRevisions || after.plans != before.plans || after.passes != before.passes || after.runs != before.runs || after.attempts != before.attempts || after.leases != before.leases {
		t.Fatalf("runtime state changed during seed persistence: before=%#v after=%#v", before, after)
	}
	current, err := fixture.store.GetCurrentFeatureWorkspaceCompletionDecision(ctx, completion.WorkspaceRowID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, completion) {
		t.Fatalf("current completion = %#v, want %#v", current, completion)
	}
}

func TestWorkflowAuditEffectsPackageRemediationSeedFailureMatrix(t *testing.T) {
	tests := []struct {
		name       string
		multiple   bool
		findings   []WorkflowAuditMaterialFinding
		triggerSQL func(int64) string
	}{
		{
			name:     "first_seed_insertion",
			findings: []WorkflowAuditMaterialFinding{{Source: "implementation", Summary: "gap", Evidence: "evidence", RequiredRemediation: "remediate"}},
			triggerSQL: func(packetID int64) string {
				return fmt.Sprintf(`CREATE TRIGGER test_fail_first_seed BEFORE INSERT ON audit_remediation_seeds FOR EACH ROW WHEN NEW.audit_packet_row_id = %d AND (SELECT COUNT(*) FROM audit_remediation_seeds WHERE audit_packet_row_id = NEW.audit_packet_row_id) = 0 BEGIN SELECT RAISE(ABORT, 'test sentinel first seed insertion'); END`, packetID)
			},
		},
		{
			name:     "later_seed_insertion",
			multiple: true,
			findings: []WorkflowAuditMaterialFinding{{Source: "implementation", Summary: "gap", Evidence: "evidence", RequiredRemediation: "remediate"}},
			triggerSQL: func(packetID int64) string {
				return fmt.Sprintf(`CREATE TRIGGER test_fail_later_seed BEFORE INSERT ON audit_remediation_seeds FOR EACH ROW WHEN NEW.audit_packet_row_id = %d AND (SELECT COUNT(*) FROM audit_remediation_seeds WHERE audit_packet_row_id = NEW.audit_packet_row_id) = 1 BEGIN SELECT RAISE(ABORT, 'test sentinel later seed insertion'); END`, packetID)
			},
		},
		{
			name: "first_finding_insertion",
			findings: []WorkflowAuditMaterialFinding{
				{Source: "implementation", Summary: "first gap", Evidence: "first evidence", RequiredRemediation: "first remediation"},
				{Source: "both", Summary: "second gap", Evidence: "second evidence", RequiredRemediation: "second remediation"},
			},
			triggerSQL: func(packetID int64) string {
				return fmt.Sprintf(`CREATE TRIGGER test_fail_first_finding BEFORE INSERT ON audit_remediation_seed_findings FOR EACH ROW WHEN NEW.sequence = 1 AND EXISTS (SELECT 1 FROM audit_remediation_seeds WHERE id = NEW.remediation_seed_row_id AND audit_packet_row_id = %d) BEGIN SELECT RAISE(ABORT, 'test sentinel first finding insertion'); END`, packetID)
			},
		},
		{
			name: "later_finding_insertion",
			findings: []WorkflowAuditMaterialFinding{
				{Source: "implementation", Summary: "first gap", Evidence: "first evidence", RequiredRemediation: "first remediation"},
				{Source: "both", Summary: "second gap", Evidence: "second evidence", RequiredRemediation: "second remediation"},
			},
			triggerSQL: func(packetID int64) string {
				return fmt.Sprintf(`CREATE TRIGGER test_fail_later_finding BEFORE INSERT ON audit_remediation_seed_findings FOR EACH ROW WHEN NEW.sequence = 2 AND EXISTS (SELECT 1 FROM audit_remediation_seeds WHERE id = NEW.remediation_seed_row_id AND audit_packet_row_id = %d) BEGIN SELECT RAISE(ABORT, 'test sentinel later finding insertion'); END`, packetID)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
			if test.multiple {
				addMultiplePackageAuditObligations(t, fixture, service)
			}
			ctx := context.Background()
			packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			triggerName := "test_fail_" + strings.ReplaceAll(test.name, "-", "_")
			triggerSQL := strings.Replace(test.triggerSQL(packet.ID), "test_fail_", triggerName+"_", 1)
			if _, err := fixture.store.DB().Exec(triggerSQL); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = fixture.store.DB().Exec("DROP TRIGGER IF EXISTS " + triggerName) })
			before := capturePackageDecisionState(t, fixture)
			_, err = service.RecordDecision(ctx, packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionNeedsRevision, test.findings))
			if err == nil || !strings.Contains(err.Error(), "test sentinel") {
				t.Fatalf("failure = %v, want injected SQLite sentinel", err)
			}
			assertPackageDecisionState(t, fixture, before)
		})
	}
}

func addMultiplePackageAuditObligations(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) {
	t.Helper()
	ctx := context.Background()
	evidence, err := service.loadPackageEvidence(ctx, fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
		return evidence, nil
	}
	db := fixture.store.DB()
	for _, trigger := range []string{
		"execution_package_member_guard", "execution_package_member_update_immutable", "execution_package_member_delete_guard",
		"execution_package_approval_binding_guard", "delivery_ticket_selection_consumption_guard",
		"audit_packet_ticket_obligation_guard", "audit_packet_ticket_obligation_update_immutable", "audit_packet_ticket_obligation_delete_guard",
		"audit_packet_ticket_obligation_approval_guard", "audit_ticket_revision_decision_approval_guard",
		"audit_ticket_revision_decision_guard", "delivery_ticket_revision_satisfaction_guard", "audit_remediation_seed_guard",
		"audit_remediation_seed_reopening_guard", "feature_workspace_completion_decision_guard", "feature_workspace_completion_reopening_guard",
	} {
		if _, err := db.Exec("DROP TRIGGER IF EXISTS " + trigger); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_execution_package_members_package`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE execution_package_members RENAME TO execution_package_members_original`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE execution_package_members (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        package_row_id INTEGER NOT NULL REFERENCES execution_packages(id) ON DELETE RESTRICT,
        selection_member_row_id INTEGER NOT NULL REFERENCES delivery_ticket_selection_members(id) ON DELETE RESTRICT,
        sequence INTEGER NOT NULL CHECK (sequence >= 1),
        revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
        member_sha256 TEXT NOT NULL CHECK (length(member_sha256) = 64 AND member_sha256 NOT GLOB '*[^0-9a-f]*'),
        created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
    )`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO execution_package_members (id, package_row_id, selection_member_row_id, sequence, revision_row_id, member_sha256, created_at) SELECT id, package_row_id, selection_member_row_id, sequence, revision_row_id, member_sha256, created_at FROM execution_package_members_original`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE execution_package_members_original`); err != nil {
		t.Fatal(err)
	}
	var memberID int64
	if err := db.QueryRow(`INSERT INTO execution_package_members (package_row_id, selection_member_row_id, sequence, revision_row_id, member_sha256) SELECT package_row_id, selection_member_row_id, 2, revision_row_id, ? FROM execution_package_members WHERE package_row_id = ? RETURNING id`, strings.Repeat("e", 64), fixture.run.ExecutionPackageRowID.Int64).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_audit_packet_ticket_obligations_packet`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE audit_packet_ticket_obligations RENAME TO audit_packet_ticket_obligations_original`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE audit_packet_ticket_obligations (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        audit_packet_row_id INTEGER NOT NULL REFERENCES audit_packets(id) ON DELETE RESTRICT,
        execution_package_row_id INTEGER NOT NULL REFERENCES execution_packages(id) ON DELETE RESTRICT,
        execution_package_member_row_id INTEGER NOT NULL REFERENCES execution_package_members(id) ON DELETE RESTRICT,
        delivery_ticket_row_id INTEGER NOT NULL REFERENCES delivery_tickets(id) ON DELETE RESTRICT,
        delivery_ticket_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
        authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
        source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
        package_approval_row_id INTEGER REFERENCES execution_package_approvals(id) ON DELETE RESTRICT,
        approved_package_sha256 TEXT CHECK (approved_package_sha256 IS NULL OR (length(approved_package_sha256) = 64 AND approved_package_sha256 NOT GLOB '*[^0-9a-f]*')),
        created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
    )`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO audit_packet_ticket_obligations (id, audit_packet_row_id, execution_package_row_id, execution_package_member_row_id, delivery_ticket_row_id, delivery_ticket_revision_row_id, authority_revision_row_id, source_closure_row_id, package_approval_row_id, approved_package_sha256, created_at)
        SELECT id, audit_packet_row_id, execution_package_row_id, execution_package_member_row_id, delivery_ticket_row_id, delivery_ticket_revision_row_id, authority_revision_row_id, source_closure_row_id, package_approval_row_id,
               CASE WHEN package_approval_row_id IS NULL THEN NULL ELSE (SELECT package_sha256 FROM execution_package_approvals WHERE id = package_approval_row_id) END,
               created_at
        FROM audit_packet_ticket_obligations_original`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE audit_packet_ticket_obligations_original`); err != nil {
		t.Fatal(err)
	}
	packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO audit_packet_ticket_obligations (audit_packet_row_id, execution_package_row_id, execution_package_member_row_id, delivery_ticket_row_id, delivery_ticket_revision_row_id, authority_revision_row_id, source_closure_row_id, package_approval_row_id, approved_package_sha256) SELECT audit_packet_row_id, execution_package_row_id, ?, delivery_ticket_row_id, delivery_ticket_revision_row_id, authority_revision_row_id, source_closure_row_id, package_approval_row_id, approved_package_sha256 FROM audit_packet_ticket_obligations WHERE audit_packet_row_id = ?`, memberID, packet.ID); err != nil {
		t.Fatal(err)
	}
}

func createCurrentPackageCompletion(t *testing.T, fixture *packageEvidenceFixture, packet workflowstore.AuditPacket) workflowstore.FeatureWorkspaceCompletionDecision {
	t.Helper()
	ctx := context.Background()
	allowDuplicatePackageAuditDecision(t, fixture)
	var completion workflowstore.FeatureWorkspaceCompletionDecision
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, fixture.run.RunID)
		if err != nil {
			return err
		}
		pkg, err := tx.GetExecutionPackageByRowID(ctx, run.ExecutionPackageRowID.Int64)
		if err != nil {
			return err
		}
		obligations, err := tx.ListAuditPacketTicketObligations(ctx, packet.ID)
		if err != nil {
			return err
		}
		approval, err := tx.GetRunExecutionPackageApproval(ctx, run.ID)
		if err != nil {
			return err
		}
		decision, err := tx.CreateAuditDecision(ctx, workflowstore.CreateAuditDecisionParams{
			AuditDecisionID: workflowstore.NewAuditDecisionID(), RunRowID: run.ID, AuditPacketArtifactRowID: packet.ArtifactRowID,
			AuditedCommit: packet.AuditedCommit, PacketSHA256: packet.PacketSHA256, Decision: workflowstore.AuditDecisionAccepted,
			Rationale: "The existing completion is based on the exact package ticket.",
		})
		if err != nil {
			return fmt.Errorf("create completion audit decision: %w", err)
		}
		revisionDecision, err := tx.CreateAuditTicketRevisionDecision(ctx, workflowstore.CreateAuditTicketRevisionDecisionParams{
			AuditDecisionRowID: decision.ID, AuditPacketTicketObligationRowID: obligations[0].ID,
			PackageApprovalRowID: sql.NullInt64{Int64: approval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: approval.PackageSha256, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("create completion Ticket decision: %w", err)
		}
		if _, err := tx.CreateDeliveryTicketRevisionSatisfaction(ctx, workflowstore.CreateDeliveryTicketRevisionSatisfactionParams{DeliveryTicketRevisionRowID: obligations[0].DeliveryTicketRevisionRowID, AuditTicketRevisionDecisionRowID: revisionDecision.ID}); err != nil {
			return fmt.Errorf("create completion satisfaction: %w", err)
		}
		completion, err = tx.CreateFeatureWorkspaceCompletionDecision(ctx, workflowstore.CreateFeatureWorkspaceCompletionDecisionParams{
			CompletionDecisionID: workflowstore.NewFeatureWorkspaceCompletionDecisionID(), WorkspaceRowID: pkg.WorkspaceRowID,
			AuthorityRevisionRowID: pkg.AuthorityRevisionRowID, SourceClosureRowID: pkg.SourceClosureRowID, Decision: "completed",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return completion
}

func allowDuplicatePackageAuditDecision(t *testing.T, fixture *packageEvidenceFixture) {
	t.Helper()
	db := fixture.store.DB()
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"audit_decision_guard", "audit_decision_update_immutable", "audit_decision_delete_guard", "audit_ticket_revision_decision_guard", "audit_ticket_revision_decision_approval_guard", "delivery_ticket_revision_satisfaction_guard", "audit_remediation_seed_guard", "audit_remediation_seed_reopening_guard", "feature_workspace_completion_decision_guard", "feature_workspace_completion_reopening_guard"} {
		if _, err := db.Exec("DROP TRIGGER IF EXISTS " + trigger); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_audit_decisions_run`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE audit_decisions RENAME TO audit_decisions_original`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE audit_decisions (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        audit_decision_id TEXT NOT NULL UNIQUE,
        run_row_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
        audit_packet_artifact_row_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
        audited_commit TEXT NOT NULL,
        packet_sha256 TEXT NOT NULL,
        decision TEXT NOT NULL CHECK (decision IN ('accepted', 'needs_revision')),
        rationale TEXT NOT NULL DEFAULT '',
        created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
        CHECK (audit_decision_id GLOB 'audit-*' AND trim(audit_decision_id) = audit_decision_id),
        CHECK (length(audited_commit) = 40 AND audited_commit NOT GLOB '*[^0-9a-f]*'),
        CHECK (length(packet_sha256) = 64 AND packet_sha256 NOT GLOB '*[^0-9a-f]*')
    )`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO audit_decisions SELECT * FROM audit_decisions_original`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE audit_decisions_original`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE INDEX idx_audit_decisions_run ON audit_decisions(run_row_id, created_at)`); err != nil {
		t.Fatal(err)
	}
}
