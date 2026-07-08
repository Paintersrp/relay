package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appaudits "relay/internal/app/audits"
	workflowstore "relay/internal/store/workflow"
)

type artifactReadbackAuditService struct {
	result appaudits.GetWorkflowAuditArtifactResult
	err    error
}

func (f *artifactReadbackAuditService) GetCurrentPacket(context.Context, string) (appaudits.GetWorkflowAuditPacketResult, error) {
	return appaudits.GetWorkflowAuditPacketResult{}, errors.New("not used")
}
func (f *artifactReadbackAuditService) GetCurrentArtifact(context.Context, appaudits.GetWorkflowAuditArtifactInput) (appaudits.GetWorkflowAuditArtifactResult, error) {
	return f.result, f.err
}
func (f *artifactReadbackAuditService) RecordDecision(context.Context, appaudits.RecordWorkflowAuditDecisionInput) (appaudits.RecordWorkflowAuditDecisionResult, error) {
	return appaudits.RecordWorkflowAuditDecisionResult{}, errors.New("not used")
}

func TestGetRunArtifactUsesPacketReferenceOnly(t *testing.T) {
	fake := &artifactReadbackAuditService{result: appaudits.GetWorkflowAuditArtifactResult{
		Run:      workflowstore.Run{RunID: "run-1"},
		Packet:   workflowstore.AuditPacket{AuditPacketID: "audit-packet-1", PacketSHA256: strings.Repeat("a", 64)},
		Artifact: workflowstore.Artifact{ArtifactID: "artifact-1", Kind: "execution_evidence", MediaType: "application/json", SHA256: strings.Repeat("b", 64), SizeBytes: 11},
		Content:  []byte(`{"ok":true}`),
	}}
	server := &Server{deps: &MCPDeps{WorkflowAuditService: fake}}
	result := server.HandleGetRunArtifact(json.RawMessage(`{"run_id":"run-1","artifact_reference":"artifact-1","max_bytes":20}`))
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	output, ok := result.StructuredContent.(map[string]any)
	if !ok || output["artifact_reference"] != "artifact-1" || output["content"] != `{"ok":true}` {
		t.Fatalf("output = %#v", result.StructuredContent)
	}
}

func TestGetRunArtifactRejectsPathAndKindArguments(t *testing.T) {
	server := &Server{deps: &MCPDeps{WorkflowAuditService: &artifactReadbackAuditService{}}}
	for _, raw := range []string{
		`{"run_id":"run-1","artifact_reference":"artifact-1","path":"/tmp/secret"}`,
		`{"run_id":"run-1","artifact_reference":"artifact-1","artifact_kind":"executor_stdout"}`,
	} {
		result := server.HandleGetRunArtifact(json.RawMessage(raw))
		if !result.IsError {
			t.Fatalf("unsafe payload accepted: %s", raw)
		}
	}
}

func TestGetRunArtifactMapsUndeclaredReference(t *testing.T) {
	server := &Server{deps: &MCPDeps{WorkflowAuditService: &artifactReadbackAuditService{err: appaudits.ErrWorkflowAuditArtifactReference}}}
	result := server.HandleGetRunArtifact(json.RawMessage(`{"run_id":"run-1","artifact_reference":"artifact-other"}`))
	if !result.IsError {
		t.Fatal("undeclared reference was accepted")
	}
	blocked, ok := result.StructuredContent.(MCPBlockedResponse)
	if !ok || len(blocked.Blockers) != 1 || blocked.Blockers[0].Code != "artifact_reference_not_declared" {
		t.Fatalf("blocked = %#v", result.StructuredContent)
	}
}
