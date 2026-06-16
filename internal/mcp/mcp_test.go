package mcp

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"relay/internal/artifacts"
)

// setupTestArtifactDir points artifact storage at a temp dir so tests don't
// write to the real data/artifacts directory.
func setupTestArtifactDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	artifacts.SetBaseDir(dir)
	t.Cleanup(func() { artifacts.SetBaseDir("data/artifacts") })
	return dir
}

// TestHandleSubmitTestAuditPacket_DocumentedPayload verifies that the standard
// mcp-test smoke payload succeeds and writes a durable artifact.
func TestHandleSubmitTestAuditPacket_DocumentedPayload(t *testing.T) {
	dir := setupTestArtifactDir(t)

	args := json.RawMessage(`{
		"run_id": "mcp-test",
		"audit_packet_markdown": "# Test Packet\n\nThis is a feasibility test.",
		"decision": "accepted"
	}`)

	result := HandleSubmitTestAuditPacket(args)

	if result.IsError {
		t.Fatalf("expected success, got tool error: %s", result.Content[0].Text)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}

	var out submitTestAuditPacketOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if !out.OK {
		t.Errorf("expected ok=true")
	}
	if out.Tool != "submit_test_audit_packet" {
		t.Errorf("expected tool=submit_test_audit_packet, got %q", out.Tool)
	}
	if out.RunID != "mcp-test" {
		t.Errorf("expected run_id=mcp-test, got %q", out.RunID)
	}
	if out.Decision != "accepted" {
		t.Errorf("expected decision=accepted, got %q", out.Decision)
	}
	if out.ArtifactPath == "" {
		t.Error("expected non-empty artifact_path")
	}

	// Verify the artifact was actually written to disk.
	expectedPath := filepath.Join(dir, "0", "mcp_test_audit_packet.md")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("expected artifact file at %s: %v", expectedPath, err)
	}

	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	content := string(data)
	if content == "" {
		t.Error("artifact file is empty")
	}
	if !contains(content, "mcp-test") {
		t.Errorf("artifact does not mention run_id; got:\n%s", content)
	}
	if !contains(content, "accepted") {
		t.Errorf("artifact does not mention decision; got:\n%s", content)
	}
}

// TestHandleSubmitTestAuditPacket_MissingRunID verifies that an empty run_id is rejected.
func TestHandleSubmitTestAuditPacket_MissingRunID(t *testing.T) {
	setupTestArtifactDir(t)

	args := json.RawMessage(`{
		"run_id": "",
		"audit_packet_markdown": "# Test",
		"decision": "accepted"
	}`)

	result := HandleSubmitTestAuditPacket(args)

	if !result.IsError {
		t.Fatal("expected tool error for empty run_id")
	}
	if !contains(result.Content[0].Text, "run_id") {
		t.Errorf("error message should mention run_id, got: %s", result.Content[0].Text)
	}
}

// TestHandleSubmitTestAuditPacket_MissingMarkdown verifies that empty markdown is rejected.
func TestHandleSubmitTestAuditPacket_MissingMarkdown(t *testing.T) {
	setupTestArtifactDir(t)

	args := json.RawMessage(`{
		"run_id": "mcp-test",
		"audit_packet_markdown": "",
		"decision": "accepted"
	}`)

	result := HandleSubmitTestAuditPacket(args)

	if !result.IsError {
		t.Fatal("expected tool error for empty audit_packet_markdown")
	}
	if !contains(result.Content[0].Text, "audit_packet_markdown") {
		t.Errorf("error message should mention audit_packet_markdown, got: %s", result.Content[0].Text)
	}
}

// TestHandleSubmitTestAuditPacket_UnsupportedDecision verifies that an invalid
// decision value is rejected with a structured error.
func TestHandleSubmitTestAuditPacket_UnsupportedDecision(t *testing.T) {
	setupTestArtifactDir(t)

	args := json.RawMessage(`{
		"run_id": "mcp-test",
		"audit_packet_markdown": "# Test",
		"decision": "auto_approve"
	}`)

	result := HandleSubmitTestAuditPacket(args)

	if !result.IsError {
		t.Fatal("expected tool error for unsupported decision")
	}
	if !contains(result.Content[0].Text, "VALIDATION_ERROR") {
		t.Errorf("error message should contain VALIDATION_ERROR, got: %s", result.Content[0].Text)
	}
}

// TestHandleSubmitTestAuditPacket_AllValidDecisions verifies that each supported
// decision value is accepted.
func TestHandleSubmitTestAuditPacket_AllValidDecisions(t *testing.T) {
	decisions := []string{
		"accepted",
		"accepted_with_warnings",
		"revision_required",
		"blocked",
		"manual_review_required",
	}
	for _, d := range decisions {
		t.Run(d, func(t *testing.T) {
			setupTestArtifactDir(t)
			args, _ := json.Marshal(map[string]string{
				"run_id":                "mcp-test",
				"audit_packet_markdown": "# Test for " + d,
				"decision":              d,
			})
			result := HandleSubmitTestAuditPacket(args)
			if result.IsError {
				t.Errorf("expected success for decision %q, got error: %s", d, result.Content[0].Text)
			}
		})
	}
}

// TestHandleSubmitTestAuditPacket_NoGitOrShellMutation verifies that the tool
// does not expose any shell execution, arbitrary file writes, or git operations.
// This test is documentary: the handler has no exec or os.Create outside artifacts.Write.
func TestHandleSubmitTestAuditPacket_NoGitOrShellMutation(t *testing.T) {
	// The handler only calls artifacts.Write which is scoped to the artifact base dir.
	// We confirm the artifact lives inside the expected directory and does not escape.
	dir := setupTestArtifactDir(t)

	args := json.RawMessage(`{
		"run_id": "mcp-test",
		"audit_packet_markdown": "# Test",
		"decision": "accepted"
	}`)

	result := HandleSubmitTestAuditPacket(args)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	var out submitTestAuditPacketOutput
	_ = json.Unmarshal([]byte(result.Content[0].Text), &out)

	// The path must be inside the temp dir.
	rel, err := filepath.Rel(dir, out.ArtifactPath)
	if err != nil || rel[:2] == ".." {
		t.Errorf("artifact path %q escapes artifact base dir %q", out.ArtifactPath, dir)
	}
}

// TestServerToolsList verifies that the MCP server advertises exactly the 13A tools.
func TestServerToolsList(t *testing.T) {
	srv := NewServer(nil_slog(t))
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

	if len(list.Tools) != 1 {
		t.Errorf("expected exactly 1 tool (13A only), got %d", len(list.Tools))
	}
	if list.Tools[0].Name != "submit_test_audit_packet" {
		t.Errorf("expected submit_test_audit_packet, got %q", list.Tools[0].Name)
	}
}

// TestServerUnknownTool verifies that calling an unknown tool returns an error.
func TestServerUnknownTool(t *testing.T) {
	setupTestArtifactDir(t)
	srv := NewServer(nil_slog(t))

	params, _ := json.Marshal(ToolCallParams{Name: "nonexistent_tool", Arguments: json.RawMessage(`{}`)})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`2`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error for unknown tool")
	}
}

// TestServerInitialize verifies the MCP initialize handshake.
func TestServerInitialize(t *testing.T) {
	srv := NewServer(nil_slog(t))
	params, _ := json.Marshal(InitializeParams{ProtocolVersion: MCPProtocolVersion})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`3`),
		Method:  "initialize",
		Params:  params,
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

// TestServerMethodNotFound verifies that unknown methods return -32601.
func TestServerMethodNotFound(t *testing.T) {
	srv := NewServer(nil_slog(t))
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`4`),
		Method:  "exec/shell",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected MethodNotFound (-32601), got %+v", resp.Error)
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || search(s, substr))
}

func search(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// nil_slog returns a slog.Logger that discards all output. Used in tests that
// do not need logging.
func nil_slog(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
