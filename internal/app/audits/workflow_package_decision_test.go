package audits

import (
	"context"
	"encoding/json"
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
