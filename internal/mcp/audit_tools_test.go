package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appaudits "relay/internal/app/audits"
	workflowstore "relay/internal/store/workflow"
)

type fakeWorkflowAuditToolService struct {
	packet        appaudits.GetWorkflowAuditPacketResult
	packetErr     error
	artifact      appaudits.GetWorkflowAuditArtifactResult
	artifactErr   error
	artifactInput appaudits.GetWorkflowAuditArtifactInput
	decision      appaudits.RecordWorkflowAuditDecisionResult
	decisionErr   error
	decisionInput appaudits.RecordWorkflowAuditDecisionInput
}

func (f *fakeWorkflowAuditToolService) GetCurrentPacket(context.Context, string) (appaudits.GetWorkflowAuditPacketResult, error) {
	return f.packet, f.packetErr
}
func (f *fakeWorkflowAuditToolService) GetCurrentArtifact(_ context.Context, input appaudits.GetWorkflowAuditArtifactInput) (appaudits.GetWorkflowAuditArtifactResult, error) {
	f.artifactInput = input
	return f.artifact, f.artifactErr
}
func (f *fakeWorkflowAuditToolService) RecordDecision(_ context.Context, input appaudits.RecordWorkflowAuditDecisionInput) (appaudits.RecordWorkflowAuditDecisionResult, error) {
	f.decisionInput = input
	return f.decision, f.decisionErr
}

func TestGetRunArtifactUsesPacketReference(t *testing.T) {
	service := &fakeWorkflowAuditToolService{artifact: appaudits.GetWorkflowAuditArtifactResult{
		Run:      workflowstore.Run{RunID: "run-test", Status: workflowstore.RunStatusAuditReady},
		Packet:   workflowstore.AuditPacket{AuditPacketID: "packet-test", PacketSHA256: strings.Repeat("c", 64)},
		Artifact: workflowstore.Artifact{ArtifactID: "artifact-row", Kind: "unified_diff", MediaType: "text/x-diff; charset=utf-8", SHA256: strings.Repeat("d", 64), SizeBytes: 12},
		Content:  []byte("diff --git\n"),
	}}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleGetRunArtifact(json.RawMessage(`{"run_id":"run-test","artifact_reference":"art_test_unified_diff","max_bytes":128}`))
	if result.IsError || !strings.Contains(result.Content[0].Text, `"artifact_reference": "art_test_unified_diff"`) {
		t.Fatalf("result = %+v", result)
	}
	if service.artifactInput.ArtifactReference != "art_test_unified_diff" || service.artifactInput.MaxBytes != 128 {
		t.Fatalf("artifact input = %+v", service.artifactInput)
	}
}

func TestGetRunArtifactMapsUndeclaredReferenceError(t *testing.T) {
	service := &fakeWorkflowAuditToolService{artifactErr: appaudits.ErrWorkflowAuditArtifactReference}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleGetRunArtifact(json.RawMessage(`{"run_id":"run-test","artifact_reference":"../secret"}`))
	if !result.IsError || !strings.Contains(result.Content[0].Text, "artifact_reference_not_declared") {
		t.Fatalf("result = %+v", result)
	}
}

func TestGetAuditPacketReturnsAuthoritativeBody(t *testing.T) {
	service := &fakeWorkflowAuditToolService{packet: appaudits.GetWorkflowAuditPacketResult{
		Run: workflowstore.Run{RunID: "run-test", Status: workflowstore.RunStatusAuditReady},
		Packet: workflowstore.AuditPacket{
			AuditPacketID: "packet-test",
			PacketSHA256:  strings.Repeat("c", 64),
			AuditedCommit: strings.Repeat("b", 40),
		},
		PacketBytes: []byte(`{"schema_version":"1.0","run":{"run_id":1}}`),
	}}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleGetWorkflowAuditPacket(json.RawMessage(`{"run_id":"run-test"}`))
	if result.IsError || !strings.Contains(result.Content[0].Text, `"run_id": 1`) {
		t.Fatalf("result = %+v", result)
	}
}

func TestRecordAuditDecisionRequiresConfirmationAndReturnsLifecycle(t *testing.T) {
	service := &fakeWorkflowAuditToolService{decision: appaudits.RecordWorkflowAuditDecisionResult{
		Run:    workflowstore.Run{RunID: "run-test", Status: workflowstore.RunStatusCompleted},
		Packet: workflowstore.AuditPacket{AuditPacketID: "packet-test", PacketSHA256: strings.Repeat("c", 64)},
		Decision: workflowstore.AuditDecision{
			AuditDecisionID: "audit-test",
			AuditedCommit:   strings.Repeat("b", 40),
			Decision:        workflowstore.AuditDecisionAccepted,
		},
		RemediationSeeds: []workflowstore.AuditRemediationSeed{{RemediationSeedID: "seed-test", AuditPacketRowID: 1, ExecutionPackageRowID: 2, AuditedCommit: strings.Repeat("b", 40)}},
	}}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleRecordWorkflowAuditDecision(json.RawMessage(`{
		"run_id":"run-test",
		"audit_packet_id":"packet-test",
		"packet_sha256":"` + strings.Repeat("c", 64) + `",
		"audited_commit":"` + strings.Repeat("b", 40) + `",
		"decision":"accepted",
		"rationale":"accepted after review",
		"observations":["non-blocking"],
		"operator_confirmed":true
	}`))
	if result.IsError || !strings.Contains(result.Content[0].Text, `"run_status": "completed"`) || !strings.Contains(result.Content[0].Text, `"ticket_effects"`) || len(service.decisionInput.Observations) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRecordAuditDecisionForwardsBoundedMaterialFindings(t *testing.T) {
	service := &fakeWorkflowAuditToolService{decision: appaudits.RecordWorkflowAuditDecisionResult{Run: workflowstore.Run{RunID: "run-test"}, Packet: workflowstore.AuditPacket{AuditPacketID: "packet-test"}, Decision: workflowstore.AuditDecision{AuditDecisionID: "audit-test", Decision: workflowstore.AuditDecisionNeedsRevision}}}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleRecordWorkflowAuditDecision(json.RawMessage(`{"run_id":"run-test","audit_packet_id":"packet-test","packet_sha256":"` + strings.Repeat("c", 64) + `","audited_commit":"` + strings.Repeat("b", 40) + `","decision":"needs_revision","rationale":"revision required","material_findings":[{"source":"both","summary":"missing proof","evidence":"packet","required_remediation":"supply proof"}],"operator_confirmed":true}`))
	if result.IsError || len(service.decisionInput.MaterialFindings) != 1 || service.decisionInput.MaterialFindings[0].RequiredRemediation != "supply proof" {
		t.Fatalf("result = %+v input = %#v", result, service.decisionInput)
	}
}

func TestRecordAuditDecisionMapsStalePacket(t *testing.T) {
	service := &fakeWorkflowAuditToolService{decisionErr: appaudits.ErrWorkflowAuditPacketStale}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleRecordWorkflowAuditDecision(json.RawMessage(`{
		"run_id":"run-test",
		"audit_packet_id":"packet-test",
		"packet_sha256":"` + strings.Repeat("c", 64) + `",
		"audited_commit":"` + strings.Repeat("b", 40) + `",
		"decision":"needs_revision",
		"rationale":"revision required",
		"operator_confirmed":true
	}`))
	if !result.IsError || !strings.Contains(result.Content[0].Text, "audit_packet_stale") {
		t.Fatalf("result = %+v", result)
	}
}
