package audits

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

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

func TestWorkflowAuditEffectsPackageRemediationSeedFailureMatrix(t *testing.T) {
	tests := []struct {
		name        string
		triggerName string
		findings    []WorkflowAuditMaterialFinding
		triggerSQL  func(string, int64) string
	}{
		{
			name:        "first_seed_insertion",
			triggerName: "test_fail_first_seed",
			findings:    []WorkflowAuditMaterialFinding{{Source: "implementation", Summary: "gap", Evidence: "evidence", RequiredRemediation: "remediate"}},
			triggerSQL: func(triggerName string, packetID int64) string {
				return fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON audit_remediation_seeds FOR EACH ROW WHEN NEW.audit_packet_row_id = %d AND (SELECT COUNT(*) FROM audit_remediation_seeds WHERE audit_packet_row_id = NEW.audit_packet_row_id) = 0 BEGIN SELECT RAISE(ABORT, 'test sentinel first seed insertion'); END`, triggerName, packetID)
			},
		},
		{
			name:        "first_finding_insertion",
			triggerName: "test_fail_first_finding",
			findings: []WorkflowAuditMaterialFinding{
				{Source: "implementation", Summary: "first gap", Evidence: "first evidence", RequiredRemediation: "first remediation"},
				{Source: "both", Summary: "second gap", Evidence: "second evidence", RequiredRemediation: "second remediation"},
			},
			triggerSQL: func(triggerName string, packetID int64) string {
				return fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON audit_remediation_seed_findings FOR EACH ROW WHEN NEW.sequence = 1 AND EXISTS (SELECT 1 FROM audit_remediation_seeds WHERE id = NEW.remediation_seed_row_id AND audit_packet_row_id = %d) BEGIN SELECT RAISE(ABORT, 'test sentinel first finding insertion'); END`, triggerName, packetID)
			},
		},
		{
			name:        "later_finding_insertion",
			triggerName: "test_fail_later_finding",
			findings: []WorkflowAuditMaterialFinding{
				{Source: "implementation", Summary: "first gap", Evidence: "first evidence", RequiredRemediation: "first remediation"},
				{Source: "both", Summary: "second gap", Evidence: "second evidence", RequiredRemediation: "second remediation"},
			},
			triggerSQL: func(triggerName string, packetID int64) string {
				return fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON audit_remediation_seed_findings FOR EACH ROW WHEN NEW.sequence = 2 AND EXISTS (SELECT 1 FROM audit_remediation_seeds WHERE id = NEW.remediation_seed_row_id AND audit_packet_row_id = %d) BEGIN SELECT RAISE(ABORT, 'test sentinel later finding insertion'); END`, triggerName, packetID)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
			attachPackageRunToEligiblePass(t, fixture)
			ctx := context.Background()
			packet, err := fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			triggerSQL := test.triggerSQL(test.triggerName, packet.ID)
			if _, err := fixture.store.DB().Exec(triggerSQL); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = fixture.store.DB().Exec("DROP TRIGGER IF EXISTS " + test.triggerName) })
			before := capturePackageDecisionState(t, fixture)
			_, err = service.RecordDecision(ctx, packageDecisionInput(fixture.run.RunID, packet, workflowstore.AuditDecisionNeedsRevision, test.findings))
			if err == nil || !strings.Contains(err.Error(), "test sentinel") {
				t.Fatalf("failure = %v, want injected SQLite sentinel", err)
			}
			assertPackageDecisionState(t, fixture, before)
		})
	}
}
