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
}

func (f *fakeWorkflowAuditToolService) GetCurrentPacket(context.Context, string) (appaudits.GetWorkflowAuditPacketResult, error) {
	return f.packet, f.packetErr
}
func (f *fakeWorkflowAuditToolService) GetCurrentArtifact(_ context.Context, input appaudits.GetWorkflowAuditArtifactInput) (appaudits.GetWorkflowAuditArtifactResult, error) {
	f.artifactInput = input
	return f.artifact, f.artifactErr
}
func (f *fakeWorkflowAuditToolService) RecordDecision(context.Context, appaudits.RecordWorkflowAuditDecisionInput) (appaudits.RecordWorkflowAuditDecisionResult, error) {
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
	result := server.HandleGetRunArtifact(json.RawMessage(`{"run_id":"run-test","artifact_reference":"unified_diff","max_bytes":128}`))
	if result.IsError || !strings.Contains(result.Content[0].Text, `"artifact_reference": "unified_diff"`) {
		t.Fatalf("result = %+v", result)
	}
	if service.artifactInput.ArtifactReference != "unified_diff" || service.artifactInput.MaxBytes != 128 {
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
		PacketBytes: []byte(`{"schema_version":"1.0","audit_packet_id":"packet-test"}`),
	}}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleGetWorkflowAuditPacket(json.RawMessage(`{"run_id":"run-test"}`))
	if result.IsError || !strings.Contains(result.Content[0].Text, `"audit_packet_id": "packet-test"`) {
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
	}}
	server := NewServer(nil, &MCPDeps{ToolProfile: ToolProfileAuditor, WorkflowAuditService: service})
	result := server.HandleRecordWorkflowAuditDecision(json.RawMessage(`{
		"run_id":"run-test",
		"audit_packet_id":"packet-test",
		"packet_sha256":"` + strings.Repeat("c", 64) + `",
		"audited_commit":"` + strings.Repeat("b", 40) + `",
		"decision":"accepted",
		"rationale":"accepted after review",
		"operator_confirmed":true
	}`))
	if result.IsError || !strings.Contains(result.Content[0].Text, `"run_status": "completed"`) {
		t.Fatalf("result = %+v", result)
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
