package mcp

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appplans "relay/internal/app/plans"
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
	if _, err := s.CreateProject("relay", "Relay", "Default Test Project", "active", ""); err != nil {
		t.Fatalf("create test project: %v", err)
	}
	return s
}

// setupTestDeps returns a test MCPDeps with real store and temp artifact dir.
func setupTestDeps(t *testing.T) *MCPDeps {
	t.Helper()
	setupTestArtifactDir(t)
	s := setupTestStore(t)
	return &MCPDeps{Store: s, Log: discardLogger()}
}

// seedMCPSourceSnapshot creates a completed source snapshot tied to the plan's project
// so it can satisfy the managed-pass source/context provenance gate.
func seedMCPSourceSnapshot(t *testing.T, s *store.Store, planID, snapshotID string) {
	t.Helper()
	plan, err := s.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("get plan for snapshot seed: %v", err)
	}
	if _, err := s.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: snapshotID,
		ProjectRowID:     plan.ProjectRowID,
		ProjectID:        "relay",
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-23T00:00:00Z",
		SummaryJSON:      "{}",
	}); err != nil {
		t.Fatalf("seed source snapshot: %v", err)
	}
}

func validMCPHandoffMarkdown(title, repo string) string {
	return fmt.Sprintf("---\ntitle: %s\nrepo_target: %s\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# %s\n\nContent.", title, repo, title)
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

// TestServerToolsList_Pass16 verifies that the MCP server advertises the
// required base tools under the restricted profile without a fixed count gate.
func TestServerToolsList_Pass16(t *testing.T) {
	srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileRestricted})
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

	expectedTools := baseToolNamesForTest()

	if len(list.Tools) < len(expectedTools) {
		t.Errorf("expected at least %d tools, got %d", len(expectedTools), len(list.Tools))
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

// --- submit_planner_pass_plan tests ---

func TestHandleSubmitPlannerPassPlan_NoDeps(t *testing.T) {
	srv := NewServer(discardLogger())
	args, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})

	result := srv.HandleSubmitPlannerPassPlan(args)
	if !result.IsError {
		t.Fatal("expected DEPENDENCY_ERROR when no store is wired")
	}
	if !contains(result.Content[0].Text, "DEPENDENCY_ERROR") {
		t.Fatalf("expected DEPENDENCY_ERROR, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitPlannerPassPlan_MissingPlanJSON(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]string{
		"planner_pass_plan_json": "",
	})

	result := srv.HandleSubmitPlannerPassPlan(args)
	if !result.IsError {
		t.Fatal("expected VALIDATION_ERROR for empty planner_pass_plan_json")
	}
	if !contains(result.Content[0].Text, "VALIDATION_ERROR") || !contains(result.Content[0].Text, "planner_pass_plan_json") {
		t.Fatalf("expected planner_pass_plan_json validation error, got: %s", result.Content[0].Text)
	}
}

func TestHandleSubmitPlannerPassPlan_Success(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	rawPlan := mustMarshalPlannerPassPlan(t, validPlannerPassPlan())
	args, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(rawPlan),
		"source_artifact_path":   "handoffs/planner/plan.json",
		"unmanaged_acknowledged": true,
	})

	result := srv.HandleSubmitPlannerPassPlan(args)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	var out submitPlannerPassPlanOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !out.OK {
		t.Fatal("expected ok=true")
	}
	if out.Tool != "submit_planner_pass_plan" {
		t.Fatalf("expected tool submit_planner_pass_plan, got %q", out.Tool)
	}
	if out.PlanID != "plan-123" {
		t.Fatalf("expected plan_id plan-123, got %q", out.PlanID)
	}
	if out.PassCount != 2 {
		t.Fatalf("expected pass_count 2, got %d", out.PassCount)
	}
	if len(out.Passes) != 2 {
		t.Fatalf("expected 2 passes, got %d", len(out.Passes))
	}
	if got := countTableRows(t, deps.Store.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "plan_passes"); got != 2 {
		t.Fatalf("expected 2 plan_passes rows, got %d", got)
	}
}

func TestHandleSubmitPlannerPassPlan_RequiresUnmanagedAcknowledgement(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	rawPlan := mustMarshalPlannerPassPlan(t, validPlannerPassPlan())
	args, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(rawPlan),
		"source":                 "does-not-count-as-acknowledgement",
	})

	result := srv.HandleSubmitPlannerPassPlan(args)
	if !result.IsError {
		t.Fatal("expected PLAN_VALIDATION_FAILED without unmanaged_acknowledged")
	}

	var out submitPlannerPassPlanErrorOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal error output: %v", err)
	}
	if out.Error != "PLAN_VALIDATION_FAILED" {
		t.Fatalf("expected PLAN_VALIDATION_FAILED, got %q", out.Error)
	}
	if out.Validation == nil {
		t.Fatal("expected validation report in error output")
	}
	assertValidationIssueCode(t, *out.Validation, appplans.IssuePlanUnmanagedAcknowledgementRequired)
	if got := countTableRows(t, deps.Store.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestHandleSubmitPlannerPassPlan_InvalidDependency(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	plan := validPlannerPassPlan()
	plan.Passes[1].Dependencies = []string{"PASS-999"}
	args, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, plan)),
		"unmanaged_acknowledged": true,
	})

	result := srv.HandleSubmitPlannerPassPlan(args)
	if !result.IsError {
		t.Fatal("expected PLAN_VALIDATION_FAILED for invalid dependency")
	}

	var out submitPlannerPassPlanErrorOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal error output: %v", err)
	}
	if out.Error != "PLAN_VALIDATION_FAILED" {
		t.Fatalf("expected PLAN_VALIDATION_FAILED, got %q", out.Error)
	}
	if out.Validation == nil {
		t.Fatal("expected validation report in error output")
	}
	assertValidationIssueCode(t, *out.Validation, appplans.IssuePlanDependencyUnknown)
	if got := countTableRows(t, deps.Store.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestHandleSubmitPlannerPassPlan_DuplicatePlanID(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	args, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})

	first := srv.HandleSubmitPlannerPassPlan(args)
	if first.IsError {
		t.Fatalf("expected first submission to succeed, got: %s", first.Content[0].Text)
	}

	second := srv.HandleSubmitPlannerPassPlan(args)
	if !second.IsError {
		t.Fatal("expected duplicate submission to fail")
	}

	var out submitPlannerPassPlanErrorOutput
	if err := json.Unmarshal([]byte(second.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal duplicate error output: %v", err)
	}
	if out.Error != "PLAN_VALIDATION_FAILED" {
		t.Fatalf("expected PLAN_VALIDATION_FAILED, got %q", out.Error)
	}
	if out.Validation == nil {
		t.Fatal("expected validation report in duplicate error output")
	}
	assertValidationIssueCode(t, *out.Validation, appplans.IssuePlanDuplicatePlanID)
	if got := countTableRows(t, deps.Store.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row after duplicate submit, got %d", got)
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
	args, _ := json.Marshal(map[string]any{"planner_handoff_markdown": ""})
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
	args, _ := json.Marshal(map[string]any{
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

	markdown := fmt.Sprintf("---\ntitle: Test Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Test Run\n\nHandoff content for testing.")
	args, _ := json.Marshal(map[string]any{
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
	if out.Provenance.PlannerHandoffSHA256 == "" {
		t.Fatal("expected provenance hash in create run output")
	}
	if out.Provenance.ArtifactKind != "planner_handoff_provenance_json" {
		t.Fatalf("expected provenance artifact kind, got %q", out.Provenance.ArtifactKind)
	}
}

func TestHandleCreateRunFromPlannerHandoffFile_SuccessExactSHA(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	markdownBytes := []byte("---\ntitle: File Run\nrepo_target: test-repo\nbranch_context: main\n---\r\n\r\n<compiler_input>\r\n```yaml\r\ncompiler_input:\r\n  goal: Test.\r\n  scope: Test.\r\n  file_targets:\r\n    - path: test.go\r\n  implementation_steps:\r\n    - id: S1\r\n      title: Step\r\n      action: modify\r\n      instructions: Run.\r\n  code_requirements:\r\n    - id: CR1\r\n      requirement: Test.\r\n  validation_contract:\r\n    mode: commands\r\n    failure_policy: block\r\n  completion_contract:\r\n    done_when:\r\n      - Done.\r\n```\r\n</compiler_input>\r\n\r\n# File Run\r\n\r\nExact bytes.\r\n")
	handoffPath := filepath.Join(t.TempDir(), "reviewed-handoff.md")
	if err := os.WriteFile(handoffPath, markdownBytes, 0644); err != nil {
		t.Fatalf("write handoff fixture: %v", err)
	}
	sum := sha256.Sum256(markdownBytes)
	expectedSHA := hex.EncodeToString(sum[:])

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_file": handoffPath,
		"expected_sha256":      expectedSHA,
		"repo_target":          "test-repo",
		"source":               "mcp_test_file",
	})
	result := srv.HandleCreateRunFromPlannerHandoffFile(args)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	var out createRunOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Tool != "create_run_from_planner_handoff_file" {
		t.Fatalf("unexpected tool: %q", out.Tool)
	}
	if out.SubmittedHandoffSHA256 != expectedSHA {
		t.Fatalf("expected submitted SHA %q, got %q", expectedSHA, out.SubmittedHandoffSHA256)
	}
	if out.ExpectedSHA256 != expectedSHA {
		t.Fatalf("expected echoed SHA %q, got %q", expectedSHA, out.ExpectedSHA256)
	}
	if !out.SHAMatch {
		t.Fatal("expected sha_match=true")
	}
	if out.SourceMode != "file_parameter" {
		t.Fatalf("expected source_mode=file_parameter, got %q", out.SourceMode)
	}
	if out.Provenance.PlannerHandoffSHA256 != expectedSHA {
		t.Fatalf("expected provenance SHA %q, got %q", expectedSHA, out.Provenance.PlannerHandoffSHA256)
	}
	if out.Provenance.SourceArtifactPath != "" {
		t.Fatalf("file submission should not persist local source path, got %q", out.Provenance.SourceArtifactPath)
	}
	if out.Provenance.SourceMode != "file_parameter" {
		t.Fatalf("expected provenance source_mode=file_parameter, got %q", out.Provenance.SourceMode)
	}

	row, err := deps.Store.GetRunSubmissionProvenanceByRun(out.RunID)
	if err != nil {
		t.Fatalf("load provenance row: %v", err)
	}
	if row.PlannerHandoffSha256 != expectedSHA {
		t.Fatalf("unexpected provenance row hash: %q", row.PlannerHandoffSha256)
	}
	if row.SourceArtifactPath != "" {
		t.Fatalf("provenance row should not persist local file path, got %q", row.SourceArtifactPath)
	}
	if contains(row.SubmissionArgsJson, handoffPath) {
		t.Fatalf("submission args should not persist local file path: %s", row.SubmissionArgsJson)
	}

	storedHandoff, err := artifacts.Read(out.RunID, "planner_handoff", "planner_handoff.md")
	if err != nil {
		t.Fatalf("read stored handoff artifact: %v", err)
	}
	if string(storedHandoff) != string(markdownBytes) {
		t.Fatalf("stored handoff bytes differ from submitted file bytes")
	}

	provenanceArtifact, err := artifacts.Read(out.RunID, "planner_handoff_provenance_json", "planner_handoff_provenance.json")
	if err != nil {
		t.Fatalf("read provenance artifact: %v", err)
	}
	provenanceText := string(provenanceArtifact)
	if !contains(provenanceText, `"source_mode": "file_parameter"`) {
		t.Fatalf("expected file source mode in provenance artifact: %s", provenanceText)
	}
	if contains(provenanceText, handoffPath) {
		t.Fatalf("provenance artifact should not persist local file path: %s", provenanceText)
	}
}

func TestHandleCreateRunFromPlannerHandoffFile_SHAMismatchCreatesNoRun(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	handoffPath := filepath.Join(t.TempDir(), "reviewed-handoff.md")
	if err := os.WriteFile(handoffPath, []byte("---\ntitle: File Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# File Run\n"), 0644); err != nil {
		t.Fatalf("write handoff fixture: %v", err)
	}

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_file": handoffPath,
		"expected_sha256":      strings.Repeat("0", 64),
		"repo_target":          "test-repo",
	})
	result := srv.HandleCreateRunFromPlannerHandoffFile(args)
	if !result.IsError {
		t.Fatal("expected SHA mismatch validation error")
	}
	if !contains(result.Content[0].Text, MCPBlockerExpectedHashMismatch) || !contains(result.Content[0].Text, `"recoverable":true`) {
		t.Fatalf("expected recoverable expected_hash_mismatch blocker, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "run_submission_provenance"); got != 0 {
		t.Fatalf("expected no provenance rows, got %d", got)
	}
}

func TestHandleCreateRunFromPlannerHandoffFile_PassWithoutPlanRejectedBeforeWrites(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	handoffPath := filepath.Join(t.TempDir(), "reviewed-handoff.md")
	if err := os.WriteFile(handoffPath, []byte(validMCPHandoffMarkdown("File Pass Without Plan", "test-repo")), 0644); err != nil {
		t.Fatalf("write handoff fixture: %v", err)
	}

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_file": handoffPath,
		"repo_target":          "test-repo",
		"pass_id":              "PASS-001",
	})
	result := srv.HandleCreateRunFromPlannerHandoffFile(args)
	if !result.IsError {
		t.Fatal("expected validation error for pass without plan")
	}
	if !contains(result.Content[0].Text, "managed_plan_missing") {
		t.Fatalf("expected managed_plan_missing, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "artifacts"); got != 0 {
		t.Fatalf("expected no artifact rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "run_submission_provenance"); got != 0 {
		t.Fatalf("expected no provenance rows, got %d", got)
	}
}

func TestHandleValidatePlannerHandoffForCompile_StrictInputAndSources(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	markdown := validMCPHandoffMarkdown("Standalone Validation", "test-repo")

	cases := []struct {
		name     string
		args     map[string]any
		wantErr  bool
		wantText string
	}{
		{name: "unknown field rejected", args: map[string]any{"planner_handoff_markdown": markdown, "repo_target": "test-repo", "surprise": true}, wantErr: true, wantText: "unknown field"},
		{name: "both source fields rejected", args: map[string]any{"planner_handoff_markdown": markdown, "planner_handoff_file": "handoff.md", "repo_target": "test-repo"}, wantErr: true, wantText: "exactly one"},
		{name: "neither source field rejected", args: map[string]any{"repo_target": "test-repo"}, wantErr: true, wantText: "exactly one"},
		{name: "inline expected sha rejected", args: map[string]any{"planner_handoff_markdown": markdown, "repo_target": "test-repo", "expected_sha256": strings.Repeat("0", 64)}, wantErr: true, wantText: "expected_sha256"},
		{name: "valid inline accepted", args: map[string]any{"planner_handoff_markdown": markdown, "repo_target": "test-repo"}, wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.args)
			result := srv.HandleValidatePlannerHandoffForCompile(raw)
			if tc.wantErr {
				if !result.IsError {
					t.Fatal("expected tool error")
				}
				if !contains(result.Content[0].Text, tc.wantText) {
					t.Fatalf("expected error containing %q, got: %s", tc.wantText, result.Content[0].Text)
				}
				return
			}
			if result.IsError {
				t.Fatalf("expected success, got: %s", result.Content[0].Text)
			}
			if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
				t.Fatalf("standalone validation must not create runs, got %d", got)
			}
			rawResult := string(mustMarshal(t, result.StructuredContent))
			if contains(rawResult, markdown) {
				t.Fatal("standalone validation result must not include full handoff markdown")
			}
		})
	}
}

func TestHandleValidatePlannerHandoffForCompile_FileMode(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	markdownBytes := []byte(validMCPHandoffMarkdown("Standalone File Validation", "test-repo"))
	handoffPath := filepath.Join(t.TempDir(), "reviewed-handoff.md")
	if err := os.WriteFile(handoffPath, markdownBytes, 0644); err != nil {
		t.Fatalf("write handoff fixture: %v", err)
	}
	sum := sha256.Sum256(markdownBytes)
	expectedSHA := hex.EncodeToString(sum[:])

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_file": handoffPath,
		"expected_sha256":      expectedSHA,
		"repo_target":          "test-repo",
	})
	result := srv.HandleValidatePlannerHandoffForCompile(args)
	if result.IsError {
		t.Fatalf("expected valid file request, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("standalone validation must not create runs, got %d", got)
	}
	rawResult := string(mustMarshal(t, result.StructuredContent))
	if contains(rawResult, handoffPath) {
		t.Fatalf("standalone validation result must not expose file path: %s", rawResult)
	}

	badFormatArgs, _ := json.Marshal(map[string]any{
		"planner_handoff_file": handoffPath,
		"expected_sha256":      "ABC",
		"repo_target":          "test-repo",
	})
	badFormat := srv.HandleValidatePlannerHandoffForCompile(badFormatArgs)
	if !badFormat.IsError || !contains(badFormat.Content[0].Text, "expected_sha256") {
		t.Fatalf("expected malformed expected_sha256 rejection, got: %+v", badFormat)
	}

	mismatchArgs, _ := json.Marshal(map[string]any{
		"planner_handoff_file": handoffPath,
		"expected_sha256":      strings.Repeat("0", 64),
		"repo_target":          "test-repo",
	})
	mismatch := srv.HandleValidatePlannerHandoffForCompile(mismatchArgs)
	if !mismatch.IsError || !contains(mismatch.Content[0].Text, "expected_sha256") {
		t.Fatalf("expected expected_sha256 mismatch rejection, got: %+v", mismatch)
	}
	var blockedMismatch MCPBlockedResponse
	if err := json.Unmarshal(mustMarshal(t, mismatch.StructuredContent), &blockedMismatch); err != nil {
		t.Fatalf("decode mismatch structuredContent: %v", err)
	}
	if len(blockedMismatch.Blockers) != 1 || blockedMismatch.Blockers[0].Code != MCPBlockerExpectedHashMismatch || !blockedMismatch.Blockers[0].Recoverable {
		t.Fatalf("expected recoverable expected_hash_mismatch blocker, got %+v", blockedMismatch.Blockers)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("standalone validation failures must not create runs, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "run_submission_provenance"); got != 0 {
		t.Fatalf("standalone validation failures must not create provenance, got %d", got)
	}
}

func TestHandleValidatePlannerHandoffForCompile_PassWithoutPlanBlocks(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": validMCPHandoffMarkdown("Standalone Pass Without Plan", "test-repo"),
		"repo_target":              "test-repo",
		"pass_id":                  "PASS-007",
	})
	result := srv.HandleValidatePlannerHandoffForCompile(args)
	if !result.IsError {
		t.Fatal("expected blocked preflight tool error")
	}
	rawResult := string(mustMarshal(t, result.StructuredContent))
	if !contains(rawResult, `"status":"blocked"`) || !contains(rawResult, "managed_plan_missing") {
		t.Fatalf("expected managed_plan_missing structured blocker, got: %s", rawResult)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("standalone validation must not create runs, got %d", got)
	}
}

func TestHandleCreateRunFromPlannerHandoffFile_InvalidFileInputs(t *testing.T) {
	cases := []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "missing",
			path: func(t *testing.T) string {
				return ""
			},
		},
		{
			name: "directory",
			path: func(t *testing.T) string {
				return t.TempDir()
			},
		},
		{
			name: "oversized",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "too-large.md")
				if err := os.WriteFile(path, bytes.Repeat([]byte("a"), maxPlannerHandoffFileBytes+1), 0644); err != nil {
					t.Fatalf("write oversized fixture: %v", err)
				}
				return path
			},
		},
		{
			name: "non_md",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "handoff.txt")
				if err := os.WriteFile(path, []byte("# Handoff\n"), 0644); err != nil {
					t.Fatalf("write non-md fixture: %v", err)
				}
				return path
			},
		},
		{
			name: "empty",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "empty.md")
				if err := os.WriteFile(path, nil, 0644); err != nil {
					t.Fatalf("write empty fixture: %v", err)
				}
				return path
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := setupTestDeps(t)
			srv := NewServer(discardLogger(), deps)
			args, _ := json.Marshal(map[string]any{
				"planner_handoff_file": tc.path(t),
				"repo_target":          "test-repo",
			})
			result := srv.HandleCreateRunFromPlannerHandoffFile(args)
			if !result.IsError {
				t.Fatal("expected validation error")
			}
			if !contains(result.Content[0].Text, "VALIDATION_ERROR") {
				t.Fatalf("expected VALIDATION_ERROR, got: %s", result.Content[0].Text)
			}
			if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
				t.Fatalf("expected no run rows, got %d", got)
			}
		})
	}
}

func TestHandleCreateRunFromPlannerHandoff_PlanOnlyAssociation(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: Planned Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Planned Run\n\nContent.",
		"repo_target":              "test-repo",
		"plan_id":                  "plan-123",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	var out createRunOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.PlanID != "plan-123" {
		t.Fatalf("expected plan_id to echo, got %q", out.PlanID)
	}
	if out.PassID != "" {
		t.Fatalf("expected empty pass_id, got %q", out.PassID)
	}

	run, err := deps.Store.GetRun(out.RunID)
	if err != nil {
		t.Fatalf("get created run: %v", err)
	}
	plan, err := deps.Store.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("get plan by plan_id: %v", err)
	}
	if !run.PlanRowID.Valid || run.PlanRowID.Int64 != plan.ID {
		t.Fatalf("expected plan_row_id=%d, got %+v", plan.ID, run.PlanRowID)
	}
	if run.PlanPassRowID.Valid {
		t.Fatalf("expected empty plan_pass_row_id, got %+v", run.PlanPassRowID)
	}
}

func TestHandleCreateRunFromPlannerHandoff_PlanPassAssociation(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	seedMCPSourceSnapshot(t, deps.Store, "plan-123", "snapshot-mcp-plan-pass")

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: Pass Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Pass Run\n\nContent.",
		"repo_target":              "test-repo",
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-002",
		"source_snapshot_id":       "snapshot-mcp-plan-pass",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	var out createRunOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.PlanID != "plan-123" || out.PassID != "PASS-002" {
		t.Fatalf("expected echoed plan/pass ids, got %q / %q", out.PlanID, out.PassID)
	}

	run, err := deps.Store.GetRun(out.RunID)
	if err != nil {
		t.Fatalf("get created run: %v", err)
	}
	plan, err := deps.Store.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("get plan by plan_id: %v", err)
	}
	pass, err := deps.Store.GetPlanPassByPassID(plan.ID, "PASS-002")
	if err != nil {
		t.Fatalf("get pass by plan/pass id: %v", err)
	}
	if !run.PlanRowID.Valid || run.PlanRowID.Int64 != plan.ID {
		t.Fatalf("expected plan_row_id=%d, got %+v", plan.ID, run.PlanRowID)
	}
	if !run.PlanPassRowID.Valid || run.PlanPassRowID.Int64 != pass.ID {
		t.Fatalf("expected plan_pass_row_id=%d, got %+v", pass.ID, run.PlanPassRowID)
	}
	if pass.Status != "run_created" {
		t.Fatalf("expected pass status to become run_created, got %q", pass.Status)
	}
}

func TestHandleCreateRunFromPlannerHandoff_PassWithoutPlanRejected(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: Invalid Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Invalid Run\n\nContent.",
		"repo_target":              "test-repo",
		"pass_id":                  "PASS-001",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected validation error for pass without plan")
	}
	if !contains(result.Content[0].Text, "managed_plan_missing") {
		t.Fatalf("expected managed_plan_missing, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "artifacts"); got != 0 {
		t.Fatalf("expected no artifact rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "run_submission_provenance"); got != 0 {
		t.Fatalf("expected no provenance rows, got %d", got)
	}
}

func TestHandleCreateRunFromPlannerHandoff_UnknownPlanRejected(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: Missing Plan Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Missing Plan Run\n\nContent.",
		"repo_target":              "test-repo",
		"plan_id":                  "plan-missing",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected not found error for unknown plan")
	}
	if !contains(result.Content[0].Text, MCPBlockerUnknownResource) {
		t.Fatalf("expected unknown_resource blocker, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
}

func TestHandleCreateRunFromPlannerHandoff_UnknownPassRejected(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: Missing Pass Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Missing Pass Run\n\nContent.",
		"repo_target":              "test-repo",
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-999",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected not found error for unknown pass")
	}
	if !contains(result.Content[0].Text, MCPBlockerUnknownResource) {
		t.Fatalf("expected unknown_resource blocker, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
}

func TestHandleCreateRunFromPlannerHandoff_PassNotOpenRejected(t *testing.T) {
	for _, status := range []string{"completed", "skipped"} {
		t.Run(status, func(t *testing.T) {
			deps := setupTestDeps(t)
			srv := NewServer(discardLogger(), deps)

			planArgs, _ := json.Marshal(map[string]any{
				"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
				"unmanaged_acknowledged": true,
			})
			planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
			if planResult.IsError {
				t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
			}

			plan, err := deps.Store.GetPlanByPlanID("plan-123")
			if err != nil {
				t.Fatalf("get plan: %v", err)
			}
			pass, err := deps.Store.GetPlanPassByPassID(plan.ID, "PASS-001")
			if err != nil {
				t.Fatalf("get pass: %v", err)
			}
			if _, err := deps.Store.UpdatePlanPassStatus(pass.ID, status); err != nil {
				t.Fatalf("seed pass status %q: %v", status, err)
			}

			args, _ := json.Marshal(map[string]any{
				"planner_handoff_markdown": "---\ntitle: Closed Pass Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Closed Pass Run\n\nContent.",
				"repo_target":              "test-repo",
				"plan_id":                  "plan-123",
				"pass_id":                  "PASS-001",
			})
			result := srv.HandleCreateRunFromPlannerHandoff(args)
			if !result.IsError {
				t.Fatal("expected closed pass to reject associated run creation")
			}
			if !contains(result.Content[0].Text, MCPBlockerSchemaMismatch) {
				t.Fatalf("expected schema_mismatch blocker, got: %s", result.Content[0].Text)
			}
			if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
				t.Fatalf("expected no run rows, got %d", got)
			}
			if got := countTableRows(t, deps.Store.DB(), "run_submission_provenance"); got != 0 {
				t.Fatalf("expected no provenance rows, got %d", got)
			}

			reloadedPass, err := deps.Store.GetPlanPass(pass.ID)
			if err != nil {
				t.Fatalf("reload pass: %v", err)
			}
			if reloadedPass.Status != status {
				t.Fatalf("expected pass to remain %q, got %q", status, reloadedPass.Status)
			}
		})
	}
}

func TestHandleCreateRunFromPlannerHandoff_ManagedPassMismatchRejected(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	markdown := "---\ntitle: Mismatch Run\nrepo_target: test-repo\nbranch_context: main\nmanaged_plan_pass: PASS-001\nmanaged_plan_pass_name: First pass\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Mismatch Run\n\nBody content.\n"
	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": markdown,
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-002",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected managed_plan_pass mismatch to reject associated run creation")
	}
	if !contains(result.Content[0].Text, "managed_pass_mismatch") {
		t.Fatalf("expected managed_pass_mismatch, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
}

func TestHandleCreateRunFromPlannerHandoff_PlanRepoConflictRejected(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: Repo Conflict Run\nrepo_target: other-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Repo Conflict Run\n\nContent.",
		"repo_target":              "other-repo",
		"plan_id":                  "plan-123",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected conflicting repo_target to reject plan-associated run creation")
	}
	if !contains(result.Content[0].Text, MCPBlockerSchemaMismatch) {
		t.Fatalf("expected schema_mismatch blocker, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
}

func TestHandleCreateRunFromPlannerHandoff_ProvenanceRecorded(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	markdown := "---\ntitle: Provenance Run\nhandoff_id: planner-handoff-2026-06-23-provenance\nrepo_target: test-repo\nbranch_context: main\nintended_handoff_path: handoffs/planner/2026-06-23_provenance.planner-handoff.md\nmanaged_plan_pass: PASS-002\nmanaged_plan_pass_name: Store plans\ncontext_packet_id: ctxpkt-123\nsource_snapshot_id: srcsnap-456\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n<context_snapshot>\nOnly durable execution context belongs here.\n</context_snapshot>\n\n# Provenance Run\n\nProvenance test content.\n"

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": markdown,
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-002",
		"client_trace_id":          "trace-123",
		"source":                   "mcp_test",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	var out createRunOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Provenance.SourceArtifactPath != "handoffs/planner/2026-06-23_provenance.planner-handoff.md" {
		t.Fatalf("unexpected provenance source_artifact_path: %q", out.Provenance.SourceArtifactPath)
	}
	if out.Provenance.ContextPacketID != "ctxpkt-123" {
		t.Fatalf("unexpected context_packet_id: %q", out.Provenance.ContextPacketID)
	}
	if out.Provenance.SourceSnapshotID != "srcsnap-456" {
		t.Fatalf("unexpected source_snapshot_id: %q", out.Provenance.SourceSnapshotID)
	}
	if !contains(strings.Join(out.ArtifactKinds, ","), "planner_handoff_provenance_json") {
		t.Fatalf("expected provenance artifact kind in output, got %+v", out.ArtifactKinds)
	}

	row, err := deps.Store.GetRunSubmissionProvenanceByRun(out.RunID)
	if err != nil {
		t.Fatalf("load provenance row: %v", err)
	}
	sum := sha256.Sum256([]byte(markdown))
	if row.PlannerHandoffSha256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected provenance hash: %q", row.PlannerHandoffSha256)
	}
	if row.PlanID != "plan-123" || row.PassID != "PASS-002" {
		t.Fatalf("unexpected plan/pass in provenance row: %q / %q", row.PlanID, row.PassID)
	}
	if row.ClientTraceID != "trace-123" {
		t.Fatalf("unexpected client trace id: %q", row.ClientTraceID)
	}
	if row.SourceArtifactPath != "handoffs/planner/2026-06-23_provenance.planner-handoff.md" {
		t.Fatalf("unexpected source artifact path: %q", row.SourceArtifactPath)
	}
	if contains(row.HandoffMetadataJson, "# Planner Handoff") {
		t.Fatalf("handoff metadata JSON should not contain full handoff markdown: %s", row.HandoffMetadataJson)
	}

	artifactBytes, err := artifacts.Read(out.RunID, "planner_handoff_provenance_json", "planner_handoff_provenance.json")
	if err != nil {
		t.Fatalf("read provenance artifact: %v", err)
	}
	artifactText := string(artifactBytes)
	if !contains(artifactText, `"planner_handoff_sha256"`) {
		t.Fatalf("expected hash field in provenance artifact, got: %s", artifactText)
	}
	if contains(artifactText, "<context_snapshot>") {
		t.Fatalf("provenance artifact should not embed full handoff markdown: %s", artifactText)
	}
}

func TestHandleCreateRunFromPlannerHandoff_ArtifactFailureRollsBackPassTransition(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	plan, err := deps.Store.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	pass, err := deps.Store.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("get pass: %v", err)
	}

	seedMCPSourceSnapshot(t, deps.Store, "plan-123", "snapshot-mcp-rollback")

	blockingPath := filepath.Join(t.TempDir(), "artifact-file")
	if err := os.WriteFile(blockingPath, []byte("block"), 0644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}
	artifacts.SetBaseDir(blockingPath)
	t.Cleanup(func() { artifacts.SetBaseDir("data/artifacts") })

	args, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: Rollback Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Rollback Run\n\nContent.",
		"repo_target":              "test-repo",
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-001",
		"source_snapshot_id":       "snapshot-mcp-rollback",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if !result.IsError {
		t.Fatal("expected artifact write failure to surface as tool error")
	}
	if !contains(result.Content[0].Text, MCPBlockerToolUnavailable) {
		t.Fatalf("expected tool_unavailable blocker, got: %s", result.Content[0].Text)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "run_submission_provenance"); got != 0 {
		t.Fatalf("expected no provenance rows, got %d", got)
	}
	reloadedPass, err := deps.Store.GetPlanPass(pass.ID)
	if err != nil {
		t.Fatalf("reload pass: %v", err)
	}
	if reloadedPass.Status != "planned" {
		t.Fatalf("expected pass to remain planned after rollback, got %q", reloadedPass.Status)
	}
}

func TestHandleGetRunStatus_IncludesProvenance(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	markdown := "---\ntitle: Status Run\nrepo_target: test-repo\nbranch_context: main\nintended_handoff_path: handoffs/planner/2026-06-23_status.planner-handoff.md\nmanaged_plan_pass: PASS-001\ncontext_packet_id: ctxpkt-status\nsource_snapshot_id: srcsnap-status\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Status Run\n\nStatus test content.\n"

	createArgs, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": markdown,
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-001",
	})
	createResult := srv.HandleCreateRunFromPlannerHandoff(createArgs)
	if createResult.IsError {
		t.Fatalf("create run failed: %s", createResult.Content[0].Text)
	}

	var createOut createRunOutput
	if err := json.Unmarshal([]byte(createResult.Content[0].Text), &createOut); err != nil {
		t.Fatalf("unmarshal create output: %v", err)
	}

	statusResult := srv.HandleGetRunStatus(json.RawMessage(fmt.Sprintf(`{"run_id":"%d"}`, createOut.RunID)))
	if statusResult.IsError {
		t.Fatalf("get run status failed: %s", statusResult.Content[0].Text)
	}

	var out runStatusOutput
	if err := json.Unmarshal([]byte(statusResult.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal status output: %v", err)
	}
	if out.PlanID != "plan-123" || out.PassID != "PASS-001" {
		t.Fatalf("expected associated plan/pass ids, got %q / %q", out.PlanID, out.PassID)
	}
	if out.PlanRowID == nil || out.PlanPassRowID == nil {
		t.Fatalf("expected plan row ids in status output, got %+v / %+v", out.PlanRowID, out.PlanPassRowID)
	}
	if out.Provenance == nil {
		t.Fatal("expected provenance summary in run status output")
	}
	if out.Provenance.SourceArtifactPath != "handoffs/planner/2026-06-23_status.planner-handoff.md" {
		t.Fatalf("unexpected provenance source_artifact_path: %q", out.Provenance.SourceArtifactPath)
	}
	if out.Provenance.ContextPacketID != "ctxpkt-status" || out.Provenance.SourceSnapshotID != "srcsnap-status" {
		t.Fatalf("unexpected provenance summary: %+v", out.Provenance)
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
	args, _ := json.Marshal(map[string]any{
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
	args, _ := json.Marshal(map[string]any{
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
	args, _ := json.Marshal(map[string]any{
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
	args, _ := json.Marshal(map[string]any{
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
	args, _ := json.Marshal(map[string]any{
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
	markdown := "---\ntitle: Audit Test Run\nrepo_target: audit-test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Audit Test Run\n\nContent."
	createArgs, _ := json.Marshal(map[string]any{
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
	if _, err := deps.Store.UpdateRunStatus(createOut.RunID, "audit_ready"); err != nil {
		t.Fatalf("set audit_ready: %v", err)
	}

	// Now submit an audit packet.
	auditArgs, _ := json.Marshal(map[string]any{
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

func TestHandleSubmitAuditPacket_AssociatedPassLifecycle(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	planArgs, _ := json.Marshal(map[string]any{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
		"unmanaged_acknowledged": true,
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	seedMCPSourceSnapshot(t, deps.Store, "plan-123", "snapshot-mcp-audit-lifecycle")

	createArgs, _ := json.Marshal(map[string]any{
		"planner_handoff_markdown": "---\ntitle: MCP Associated Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# MCP Associated Run\n\nContent.",
		"repo_target":              "test-repo",
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-001",
		"source_snapshot_id":       "snapshot-mcp-audit-lifecycle",
	})
	createResult := srv.HandleCreateRunFromPlannerHandoff(createArgs)
	if createResult.IsError {
		t.Fatalf("create run failed: %s", createResult.Content[0].Text)
	}

	var createOut createRunOutput
	if err := json.Unmarshal([]byte(createResult.Content[0].Text), &createOut); err != nil {
		t.Fatalf("unmarshal create output: %v", err)
	}

	plan, err := deps.Store.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	pass, err := deps.Store.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("get pass: %v", err)
	}
	if pass.Status != "run_created" {
		t.Fatalf("expected pass to be run_created after run creation, got %q", pass.Status)
	}
	if _, err := deps.Store.UpdateRunStatus(createOut.RunID, "audit_ready"); err != nil {
		t.Fatalf("set audit_ready: %v", err)
	}

	auditArgs, _ := json.Marshal(map[string]any{
		"run_id":                fmt.Sprintf("%d", createOut.RunID),
		"audit_packet_markdown": "# Audit\n\nAccept with warnings.",
		"decision":              "accepted_with_warnings",
	})
	auditResult := srv.HandleSubmitAuditPacket(auditArgs)
	if auditResult.IsError {
		t.Fatalf("submit audit failed: %s", auditResult.Content[0].Text)
	}

	pass, err = deps.Store.GetPlanPass(pass.ID)
	if err != nil {
		t.Fatalf("reload pass: %v", err)
	}
	if pass.Status != "completed" {
		t.Fatalf("expected pass to become completed, got %q", pass.Status)
	}
}

// TestHandleSubmitAuditPacket_TerminalRunRejected verifies completed runs cannot receive audits.
func TestHandleSubmitAuditPacket_TerminalRunRejected(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	// Create a run.
	markdown := "---\ntitle: Terminal Test\nrepo_target: term-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Terminal Test\n\nContent."
	createArgs, _ := json.Marshal(map[string]any{
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
	auditArgs, _ := json.Marshal(map[string]any{
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

func validPlannerPassPlan() appplans.PlannerPassPlan {
	return appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        "plan-123",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-21T16:10:00Z",
			Title:         "Relay plan submission service",
			Goal:          "Store validated planner plans",
			RepoTarget:    "test-repo",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "relay-mcp-tests",
				Mode:                 "submission_only",
				ContextBrokerEnabled: mcpBoolPtr(false),
			},
		},
		SourceIntent: appplans.SourceIntent{
			Summary: "Add a backend service for validated plan submission.",
		},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:   "Relay managed plan records.",
			PlannerContextBoundary: "MCP tests validate plan submission without broker tool exposure.",
			ForbiddenContextDomains: []string{
				"GitHub issues",
			},
		},
		Passes: []appplans.PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "Validate plans",
				Goal:                   "Validate syntax and semantics.",
				IntendedExecutionScope: []string{"internal/plans/validator.go"},
				NonGoals:               []string{"No API routes"},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "schema_contract",
				ContextPlan: appplans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []appplans.ContextSearchTerm{
						{RepoID: "relay", Query: "submit_planner_pass_plan", Purpose: "Locate the MCP submission flow.", Required: mcpBoolPtr(true)},
					},
					SeedFilesToRead: []appplans.ContextFileRead{
						{RepoID: "relay", Path: "internal/mcp/plan_tools.go", Purpose: "Keep submission semantics bounded.", Required: mcpBoolPtr(true)},
					},
					ContextCoverageExpectations: []string{"Planner plan submission creates plan/pass records only."},
					BlockedIfMissing:            []string{"Plan submission tool wiring cannot be read."},
				},
				SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
					RequireGitStatus:   mcpBoolPtr(true),
					RequireCommitSHA:   mcpBoolPtr(false),
					AllowDirtyWorktree: mcpBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"MCP submission rejects incomplete Plan v2 passes."},
			},
			{
				PassID:                 "PASS-002",
				Sequence:               2,
				Name:                   "Store plans",
				Goal:                   "Store validated plans transactionally.",
				IntendedExecutionScope: []string{"internal/plans/service.go"},
				NonGoals:               []string{"No UI changes"},
				Dependencies:           []string{"PASS-001"},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: appplans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []appplans.ContextSearchTerm{
						{RepoID: "relay", Query: "CreatePlanPass", Purpose: "Locate persistence fields.", Required: mcpBoolPtr(true)},
					},
					SeedFilesToRead: []appplans.ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/service.go", Purpose: "Persist full Plan v2 JSON metadata.", Required: mcpBoolPtr(true)},
					},
					ContextCoverageExpectations: []string{"Stored pass rows preserve later workflow context."},
					BlockedIfMissing:            []string{"Persistence wiring cannot be read."},
				},
				SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
					RequireGitStatus:   mcpBoolPtr(true),
					RequireCommitSHA:   mcpBoolPtr(false),
					AllowDirtyWorktree: mcpBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Stored pass rows preserve context for later runs."},
			},
		},
	}
}

func mustMarshalPlannerPassPlan(t *testing.T, plan appplans.PlannerPassPlan) []byte {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal planner pass plan: %v", err)
	}
	return raw
}

func assertValidationIssueCode(t *testing.T, report appplans.PlanValidationReport, code string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected validation issue %s, got %+v", code, report.Issues)
}

func countTableRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	query := "SELECT COUNT(*) FROM " + table
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count rows for %s: %v", table, err)
	}
	return count
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

func mcpBoolPtr(value bool) *bool {
	return &value
}
