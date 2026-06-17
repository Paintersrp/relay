package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
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

// setupTestStore opens an in-memory SQLite store for tests that need real DB operations.
func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	s, err := store.Open(dbPath, discardLogger())
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// setupTestDeps returns a test MCPDeps with real store and temp artifact dir.
func setupTestDeps(t *testing.T) *MCPDeps {
	t.Helper()
	setupTestArtifactDir(t)
	s := setupTestStore(t)
	return &MCPDeps{Store: s, Log: discardLogger()}
}

// --- submit_test_audit_packet tests (Pass 13A, preserved) ---

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

func TestHandleSubmitTestAuditPacket_MissingRunID(t *testing.T) {
	setupTestArtifactDir(t)
	args := json.RawMessage(`{"run_id": "", "audit_packet_markdown": "# Test", "decision": "accepted"}`)
	result := HandleSubmitTestAuditPacket(args)
	if !result.IsError {
		t.Fatal("expected tool error for empty run_id")
	}
	if !contains(result.Content[0].Text, "run_id") {
		t.Errorf("error message should mention run_id, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitTestAuditPacket_MissingMarkdown(t *testing.T) {
	setupTestArtifactDir(t)
	args := json.RawMessage(`{"run_id": "mcp-test", "audit_packet_markdown": "", "decision": "accepted"}`)
	result := HandleSubmitTestAuditPacket(args)
	if !result.IsError {
		t.Fatal("expected tool error for empty audit_packet_markdown")
	}
	if !contains(result.Content[0].Text, "audit_packet_markdown") {
		t.Errorf("error message should mention audit_packet_markdown, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitTestAuditPacket_UnsupportedDecision(t *testing.T) {
	setupTestArtifactDir(t)
	args := json.RawMessage(`{"run_id": "mcp-test", "audit_packet_markdown": "# Test", "decision": "auto_approve"}`)
	result := HandleSubmitTestAuditPacket(args)
	if !result.IsError {
		t.Fatal("expected tool error for unsupported decision")
	}
	if !contains(result.Content[0].Text, "VALIDATION_ERROR") {
		t.Errorf("error message should contain VALIDATION_ERROR, got: %s", result.Content[0].Text)
	}
}

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

func TestHandleSubmitTestAuditPacket_NoGitOrShellMutation(t *testing.T) {
	dir := setupTestArtifactDir(t)
	args := json.RawMessage(`{"run_id": "mcp-test", "audit_packet_markdown": "# Test", "decision": "accepted"}`)
	result := HandleSubmitTestAuditPacket(args)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	var out submitTestAuditPacketOutput
	_ = json.Unmarshal([]byte(result.Content[0].Text), &out)
	rel, err := filepath.Rel(dir, out.ArtifactPath)
	if err != nil || rel[:2] == ".." {
		t.Errorf("artifact path %q escapes artifact base dir %q", out.ArtifactPath, dir)
	}
}

// --- Server-level tests ---

// TestServerToolsList_Pass16 verifies that the MCP server advertises exactly 5 tools.
func TestServerToolsList_Pass16(t *testing.T) {
	srv := NewServer(discardLogger())
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

	expectedTools := []string{
		"submit_test_audit_packet",
		"create_run_from_planner_handoff",
		"list_open_runs",
		"get_run_status",
		"submit_audit_packet",
	}

	if len(list.Tools) != len(expectedTools) {
		t.Errorf("expected exactly %d tools, got %d", len(expectedTools), len(list.Tools))
	}

	registeredNames := map[string]bool{}
	for _, tool := range list.Tools {
		registeredNames[tool.Name] = true
	}
	for _, name := range expectedTools {
		if !registeredNames[name] {
			t.Errorf("expected tool %q not found in tools/list", name)
		}
	}
}

func TestServerUnknownTool(t *testing.T) {
	setupTestArtifactDir(t)
	srv := NewServer(discardLogger())

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

func TestServerInitialize(t *testing.T) {
	srv := NewServer(discardLogger())
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

func TestServerMethodNotFound(t *testing.T) {
	srv := NewServer(discardLogger())
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

func TestServerSafety(t *testing.T) {
	srv := NewServer(discardLogger())
	unsafeNames := []string{
		"shell", "exec", "command", "read_file", "write_file",
		"git_commit", "git_push", "checkout", "reset", "merge",
	}
	for _, tool := range srv.tools {
		for _, unsafe := range unsafeNames {
			if contains(strings.ToLower(tool.Name), unsafe) {
				t.Errorf("unsafe tool name registered: %q contains unsafe keyword %q", tool.Name, unsafe)
			}
		}
	}
}

// --- create_run_from_planner_handoff tests ---

func TestHandleCreateRunFromPlannerHandoff_NoDeps(t *testing.T) {
	srv := NewServer(discardLogger()) // no deps
	params, _ := json.Marshal(ToolCallParams{
		Name:      "create_run_from_planner_handoff",
		Arguments: json.RawMessage(`{"planner_handoff_markdown": "# Test\n\nHello"}`),
	})
	req := Request{JSONRPC: JSONRPCVersion, ID: json.RawMessage(`10`), Method: "tools/call", Params: params}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}
	var result ToolCallResult
	b, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(b, &result)
	if !result.IsError {
		t.Error("expected tool-level DEPENDENCY_ERROR when no store is wired")
	}
	if len(result.Content) > 0 && !contains(result.Content[0].Text, "DEPENDENCY_ERROR") {
		t.Errorf("expected DEPENDENCY_ERROR, got: %s", result.Content[0].Text)
	}
}

func TestHandleCreateRunFromPlannerHandoff_MissingMarkdown(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]string{"planner_handoff_markdown": ""})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected tool error for empty planner_handoff_markdown")
	}
	if !contains(result.Content[0].Text, "VALIDATION_ERROR") {
		t.Errorf("expected VALIDATION_ERROR, got: %s", result.Content[0].Text)
	}
}

func TestHandleCreateRunFromPlannerHandoff_NoRepoTarget(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	// Markdown with no frontmatter repo
	args, _ := json.Marshal(map[string]string{
		"planner_handoff_markdown": "# My Handoff\n\nContent here.",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected tool error when no repo_target is provided")
	}
	if !contains(result.Content[0].Text, "INTAKE_ERROR") {
		t.Errorf("expected INTAKE_ERROR, got: %s", result.Content[0].Text)
	}
}

func TestHandleCreateRunFromPlannerHandoff_Success(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	markdown := fmt.Sprintf("---\ntitle: Test Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n# Test Run\n\nHandoff content for testing.")
	args, _ := json.Marshal(map[string]string{
		"planner_handoff_markdown": markdown,
		"repo_target":              "test-repo",
		"source":                   "mcp_test",
	})

	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}

	var out createRunOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}
	if out.RunID <= 0 {
		t.Errorf("expected positive run_id, got %d", out.RunID)
	}
	if out.Status == "" {
		t.Error("expected non-empty status")
	}
	if out.LifecycleState != "intake" {
		t.Errorf("expected lifecycle_state=intake, got %q", out.LifecycleState)
	}
	if !contains(out.ReviewURL, "/runs/") {
		t.Errorf("expected review_url to contain /runs/, got %q", out.ReviewURL)
	}
}

// --- list_open_runs tests ---

func TestHandleListOpenRuns_NoDeps(t *testing.T) {
	srv := NewServer(discardLogger())
	result := srv.HandleListOpenRuns(json.RawMessage(`{}`))
	if !result.IsError {
		t.Error("expected DEPENDENCY_ERROR when no store is wired")
	}
}

func TestHandleListOpenRuns_Empty(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	result := srv.HandleListOpenRuns(json.RawMessage(`{}`))
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}
	var out listRunsOutput
	_ = json.Unmarshal([]byte(result.Content[0].Text), &out)
	if !out.OK {
		t.Error("expected ok=true")
	}
	if out.Count != 0 {
		t.Errorf("expected 0 runs in empty store, got %d", out.Count)
	}
}

func TestHandleListOpenRuns_LimitCap(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	// Limit over 25 should be capped.
	result := srv.HandleListOpenRuns(json.RawMessage(`{"limit": 100}`))
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content[0].Text)
	}
}

// --- get_run_status tests ---

func TestHandleGetRunStatus_NoDeps(t *testing.T) {
	srv := NewServer(discardLogger())
	result := srv.HandleGetRunStatus(json.RawMessage(`{"run_id": "1"}`))
	if !result.IsError {
		t.Error("expected DEPENDENCY_ERROR when no store is wired")
	}
}

func TestHandleGetRunStatus_MissingRunID(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	result := srv.HandleGetRunStatus(json.RawMessage(`{"run_id": ""}`))
	if !result.IsError {
		t.Error("expected VALIDATION_ERROR for empty run_id")
	}
	if !contains(result.Content[0].Text, "VALIDATION_ERROR") {
		t.Errorf("expected VALIDATION_ERROR, got: %s", result.Content[0].Text)
	}
}

func TestHandleGetRunStatus_InvalidRunID(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	result := srv.HandleGetRunStatus(json.RawMessage(`{"run_id": "not-a-number"}`))
	if !result.IsError {
		t.Error("expected VALIDATION_ERROR for non-numeric run_id")
	}
}

func TestHandleGetRunStatus_NotFound(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	result := srv.HandleGetRunStatus(json.RawMessage(`{"run_id": "99999"}`))
	if !result.IsError {
		t.Error("expected NOT_FOUND for non-existent run")
	}
	if !contains(result.Content[0].Text, "NOT_FOUND") {
		t.Errorf("expected NOT_FOUND, got: %s", result.Content[0].Text)
	}
}

// --- submit_audit_packet tests ---

func TestHandleSubmitAuditPacket_NoDeps(t *testing.T) {
	srv := NewServer(discardLogger())
	args, _ := json.Marshal(map[string]string{
		"run_id":                "1",
		"audit_packet_markdown": "# Test",
		"decision":              "accepted",
	})
	result := srv.HandleSubmitAuditPacket(args)
	if !result.IsError {
		t.Error("expected DEPENDENCY_ERROR when no store is wired")
	}
}

func TestHandleSubmitAuditPacket_MissingRunID(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]string{
		"run_id":                "",
		"audit_packet_markdown": "# Test",
		"decision":              "accepted",
	})
	result := srv.HandleSubmitAuditPacket(args)
	if !result.IsError {
		t.Fatal("expected tool error for empty run_id")
	}
	if !contains(result.Content[0].Text, "run_id") {
		t.Errorf("expected run_id mention, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitAuditPacket_MissingMarkdown(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]string{
		"run_id":                "1",
		"audit_packet_markdown": "",
		"decision":              "accepted",
	})
	result := srv.HandleSubmitAuditPacket(args)
	if !result.IsError {
		t.Fatal("expected tool error for empty audit_packet_markdown")
	}
	if !contains(result.Content[0].Text, "audit_packet_markdown") {
		t.Errorf("expected audit_packet_markdown mention, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitAuditPacket_InvalidDecision(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]string{
		"run_id":                "1",
		"audit_packet_markdown": "# Test",
		"decision":              "auto_approve",
	})
	result := srv.HandleSubmitAuditPacket(args)
	if !result.IsError {
		t.Fatal("expected tool error for invalid decision")
	}
	if !contains(result.Content[0].Text, "VALIDATION_ERROR") {
		t.Errorf("expected VALIDATION_ERROR, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitAuditPacket_NotFound(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]string{
		"run_id":                "99999",
		"audit_packet_markdown": "# Test",
		"decision":              "accepted",
	})
	result := srv.HandleSubmitAuditPacket(args)
	if !result.IsError {
		t.Fatal("expected NOT_FOUND for non-existent run")
	}
	if !contains(result.Content[0].Text, "NOT_FOUND") {
		t.Errorf("expected NOT_FOUND, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitAuditPacket_StatusTransitions(t *testing.T) {
	// blocked and manual_review_required must map to revision_required.
	tests := []struct {
		decision       string
		expectedStatus string
	}{
		{"accepted", "accepted"},
		{"accepted_with_warnings", "accepted_with_warnings"},
		{"revision_required", "revision_required"},
		{"blocked", "revision_required"},
		{"manual_review_required", "revision_required"},
	}

	for _, tt := range tests {
		t.Run(tt.decision, func(t *testing.T) {
			status, note := auditDecisionToStatus(tt.decision)
			if status != tt.expectedStatus {
				t.Errorf("decision %q: expected status %q, got %q", tt.decision, tt.expectedStatus, status)
			}
			if (tt.decision == "blocked" || tt.decision == "manual_review_required") && note == "" {
				t.Errorf("decision %q: expected a non-empty note for mapped status, got empty", tt.decision)
			}
		})
	}
}

func TestHandleSubmitAuditPacket_FullFlow(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	// First create a run via create_run tool.
	markdown := "---\ntitle: Audit Test Run\nrepo_target: audit-test-repo\n---\n\n# Audit Test Run\n\nContent."
	createArgs, _ := json.Marshal(map[string]string{
		"planner_handoff_markdown": markdown,
		"repo_target":              "audit-test-repo",
		"source":                   "test",
	})
	createResult := srv.HandleCreateRunFromPlannerHandoff(createArgs)
	if createResult.IsError {
		t.Fatalf("create run failed: %s", createResult.Content[0].Text)
	}

	var createOut createRunOutput
	if err := json.Unmarshal([]byte(createResult.Content[0].Text), &createOut); err != nil {
		t.Fatalf("unmarshal create output: %v", err)
	}

	runIDStr := fmt.Sprintf("%d", createOut.RunID)

	// Now submit an audit packet.
	auditArgs, _ := json.Marshal(map[string]string{
		"run_id":                runIDStr,
		"audit_packet_markdown": "# Audit\n\nAll looks good.",
		"decision":              "accepted",
	})
	auditResult := srv.HandleSubmitAuditPacket(auditArgs)
	if auditResult.IsError {
		t.Fatalf("submit audit failed: %s", auditResult.Content[0].Text)
	}

	var auditOut submitAuditOutput
	if err := json.Unmarshal([]byte(auditResult.Content[0].Text), &auditOut); err != nil {
		t.Fatalf("unmarshal audit output: %v", err)
	}
	if !auditOut.OK {
		t.Error("expected ok=true")
	}
	if auditOut.Status != "accepted" {
		t.Errorf("expected status=accepted, got %q", auditOut.Status)
	}
	if auditOut.Decision != "accepted" {
		t.Errorf("expected decision=accepted, got %q", auditOut.Decision)
	}
}

// TestHandleSubmitAuditPacket_TerminalRunRejected verifies completed runs cannot receive audits.
func TestHandleSubmitAuditPacket_TerminalRunRejected(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	// Create a run.
	markdown := "---\ntitle: Terminal Test\nrepo_target: term-repo\n---\n\n# Terminal Test\n\nContent."
	createArgs, _ := json.Marshal(map[string]string{
		"planner_handoff_markdown": markdown,
		"repo_target":              "term-repo",
	})
	createResult := srv.HandleCreateRunFromPlannerHandoff(createArgs)
	if createResult.IsError {
		t.Fatalf("create run failed: %s", createResult.Content[0].Text)
	}
	var createOut createRunOutput
	_ = json.Unmarshal([]byte(createResult.Content[0].Text), &createOut)

	// Force the run to "completed" status.
	_, err := deps.Store.UpdateRunStatus(createOut.RunID, "completed")
	if err != nil {
		t.Fatalf("update run status: %v", err)
	}

	// Attempt to submit audit — should be rejected.
	auditArgs, _ := json.Marshal(map[string]string{
		"run_id":                fmt.Sprintf("%d", createOut.RunID),
		"audit_packet_markdown": "# Late Audit",
		"decision":              "accepted",
	})
	auditResult := srv.HandleSubmitAuditPacket(auditArgs)
	if !auditResult.IsError {
		t.Error("expected STATE_ERROR for completed run")
	}
	if !contains(auditResult.Content[0].Text, "STATE_ERROR") {
		t.Errorf("expected STATE_ERROR, got: %s", auditResult.Content[0].Text)
	}
}

// --- lifecycle helpers ---

func TestLifecycleStateFromStatus(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{"intake_received", "intake"},
		{"intake_needs_review", "intake"},
		{"approved_for_prepare", "intake"},
		{"brief_ready_for_review", "prepare"},
		{"approved_for_executor", "execute"},
		{"executor_done", "execute"},
		{"audit_ready", "audit"},
		{"accepted", "audit"},
		{"revision_required", "audit"},
		{"completed", "completed"},
		{"blocked", "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := lifecycleStateFromStatus(tt.status)
			if got != tt.expected {
				t.Errorf("lifecycleStateFromStatus(%q) = %q, want %q", tt.status, got, tt.expected)
			}
		})
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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

// nil_slog is kept for backward compat with test helpers that call it by name.
func nil_slog(t *testing.T) *slog.Logger {
	t.Helper()
	return discardLogger()
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
