package audits

import (
	"context"
	"errors"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowPackageAuditRecordDecisionAccepted(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, true)
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
	if result.Run.Status != workflowstore.RunStatusCompleted || len(result.TicketRevisionDecisions) != 1 || len(result.TicketSatisfactions) != 1 || len(result.RemediationSeeds) != 0 {
		t.Fatalf("accepted package decision = %#v", result)
	}
}

func TestWorkflowPackageAuditRecordDecisionNeedsRevision(t *testing.T) {
	for _, source := range []string{"implementation", "governing_package", "both"} {
		t.Run(source, func(t *testing.T) {
			fixture, service := newPackageAuditPrepareFixture(t, true)
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
			if result.Run.Status != workflowstore.RunStatusNeedsRevision || len(result.TicketRevisionDecisions) != 1 || len(result.TicketSatisfactions) != 0 || len(result.RemediationSeeds) != 0 {
				t.Fatalf("needs-revision package decision = %#v", result)
			}
		})
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
