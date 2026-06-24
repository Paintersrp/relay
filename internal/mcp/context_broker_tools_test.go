package mcp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/projects"
	"relay/internal/sources"
)

type brokerFixture struct {
	deps       *MCPDeps
	server     *Server
	repoRoot   string
	projectID  string
	snapshotID string
}

type brokerSuccessEnvelope struct {
	OK     bool            `json:"ok"`
	Tool   string          `json:"tool"`
	Result json.RawMessage `json:"result"`
}

type brokerErrorEnvelope struct {
	OK    bool `json:"ok"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestServerToolsList_ContextBrokerDisabled(t *testing.T) {
	deps := setupTestDeps(t)
	deps.ToolProfile = ToolProfileRestricted
	deps.ContextBrokerEnabled = false
	srv := NewServer(discardLogger(), deps)

	list := listTools(t, srv)
	if len(list.Tools) != 6 {
		t.Fatalf("expected disabled broker mode to keep 6 tools, got %d", len(list.Tools))
	}
	for _, name := range brokerToolNames() {
		if hasTool(list, name) {
			t.Fatalf("did not expect broker tool %q when disabled", name)
		}
	}
}

func TestServerToolsList_ContextBrokerEnabled(t *testing.T) {
	deps := setupTestDeps(t)
	deps.ToolProfile = ToolProfileLocalOperator
	deps.ContextBrokerEnabled = true
	srv := NewServer(discardLogger(), deps)

	list := listTools(t, srv)
	for _, name := range brokerToolNames() {
		if !hasTool(list, name) {
			t.Fatalf("expected broker tool %q when enabled", name)
		}
	}
	if len(list.Tools) != 48 {
		t.Fatalf("expected 48 total tools when broker is enabled (6 core + 24 broker + 18 refactor backlog), got %d", len(list.Tools))
	}
}

func TestContextBrokerToolSchemasAreBoundedAndSafe(t *testing.T) {
	for _, tool := range contextBrokerToolDefinitions() {
		var schema map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("unmarshal schema for %s: %v", tool.Name, err)
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("expected additionalProperties=false for %s", tool.Name)
		}
		lower := strings.ToLower(string(tool.InputSchema))
		for _, banned := range []string{
			"\"command\"",
			"\"shell\"",
			"\"git_command\"",
			"\"local_path\"",
			"\"absolute_path\"",
			"\"sql\"",
			"\"token\"",
			"\"secret\"",
			"\"auth_header\"",
			"\"cookie\"",
			"\"private_key\"",
		} {
			if strings.Contains(lower, banned) {
				t.Fatalf("schema for %s contains banned field %s", tool.Name, banned)
			}
		}
	}
}

func TestContextBrokerToolsRejectUnknownFields(t *testing.T) {
	fixture := setupBrokerFixture(t)
	argsByTool := map[string]string{
		"get_project":                      `{"project_id":"relay","unexpected":true}`,
		"get_plan":                         `{"plan_id":"plan-123","unexpected":true}`,
		"get_pass":                         `{"plan_id":"plan-123","pass_id":"PASS-001","unexpected":true}`,
		"get_pass_context":                 `{"plan_id":"plan-123","pass_id":"PASS-001","unexpected":true}`,
		"create_source_snapshot":           `{"project_id":"relay","unexpected":true}`,
		"list_project_files":               `{"project_id":"relay","unexpected":true}`,
		"search_project_files":             `{"project_id":"relay","pattern":"needle","unexpected":true}`,
		"read_project_file":                `{"project_id":"relay","repo_id":"relay","path":"src/app.txt","unexpected":true}`,
		"get_repository_git_status":        `{"project_id":"relay","repo_id":"relay","unexpected":true}`,
		"get_repository_recent_commit":     `{"project_id":"relay","repo_id":"relay","unexpected":true}`,
		"list_repository_changed_files":    `{"project_id":"relay","repo_id":"relay","mode":"worktree","unexpected":true}`,
		"get_repository_diff":              `{"project_id":"relay","repo_id":"relay","mode":"worktree","unexpected":true}`,
		"create_context_packet":            `{"project_id":"relay","task_slug":"broker-unknown","source_snapshot_id":"srcsnap-test","include_inventory":true,"unexpected":true}`,
		"get_context_packet":               `{"context_packet_id":"ctxpkt-test","unexpected":true}`,
		"search_project_context_memory":    `{"project_id":"relay","unexpected":true}`,
		"list_project_context_records":     `{"project_id":"relay","unexpected":true}`,
		"get_project_context_record":       `{"project_id":"relay","record_id":"ctxmem-test","unexpected":true}`,
		"create_project_context_record":    `{"project_id":"relay","kind":"decision","title":"Durable","body":"Important durable context.","dedupe_reason":"Checked existing context first.","unexpected":true}`,
		"supersede_project_context_record": `{"project_id":"relay","record_id":"ctxmem-test","kind":"decision","title":"Durable","body":"Important durable context.","dedupe_reason":"Checked existing context first.","unexpected":true}`,
	}
	for toolName, raw := range argsByTool {
		t.Run(toolName, func(t *testing.T) {
			result := callTool(t, fixture.server, toolName, json.RawMessage(raw))
			if !result.IsError {
				t.Fatalf("expected validation error for %s", toolName)
			}
			errEnvelope := decodeBrokerError(t, result)
			if errEnvelope.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR for %s, got %+v", toolName, errEnvelope)
			}
		})
	}
}

func TestHandleGetProjectOmitsLocalPaths(t *testing.T) {
	fixture := setupBrokerFixture(t)
	result := callTool(t, fixture.server, ToolGetProject.Name, json.RawMessage(`{"project_id":"relay"}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	if strings.Contains(result.Content[0].Text, fixture.repoRoot) || strings.Contains(result.Content[0].Text, "local_path") {
		t.Fatalf("project result leaked local path data: %s", result.Content[0].Text)
	}
	success := decodeBrokerSuccess(t, result)
	var payload struct {
		ProjectID    string `json:"project_id"`
		Repositories []struct {
			RepoID string `json:"repo_id"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal project payload: %v", err)
	}
	if payload.ProjectID != "relay" || len(payload.Repositories) != 1 || payload.Repositories[0].RepoID != "relay" {
		t.Fatalf("unexpected project payload: %+v", payload)
	}
}

func TestHandleGetPassContextReturnsPlanV2Context(t *testing.T) {
	fixture := setupBrokerFixture(t)
	packetID := createContextPacketViaTool(t, fixture, "pass-context")

	result := callTool(t, fixture.server, ToolGetPassContext.Name, json.RawMessage(`{
		"plan_id":"plan-123",
		"pass_id":"PASS-001",
		"include_latest_source_snapshot":true,
		"include_latest_context_packet":true
	}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	success := decodeBrokerSuccess(t, result)
	var payload struct {
		ProjectID                  string          `json:"project_id"`
		PlanID                     string          `json:"plan_id"`
		PassID                     string          `json:"pass_id"`
		ContextPlan                json.RawMessage `json:"context_plan"`
		SourceSnapshotRequirements json.RawMessage `json:"source_snapshot_requirements"`
		HandoffReadinessCriteria   json.RawMessage `json:"handoff_readiness_criteria"`
		LatestSourceSnapshot       *struct {
			SourceSnapshotID string `json:"source_snapshot_id"`
		} `json:"latest_source_snapshot"`
		LatestContextPacket *struct {
			ContextPacketID string `json:"context_packet_id"`
		} `json:"latest_context_packet"`
		CoverageReadiness map[string]bool `json:"coverage_readiness"`
		HandoffReadiness  struct {
			Status                 string `json:"status"`
			ReadyForHandoff        bool   `json:"ready_for_handoff"`
			RequiresSourceSnapshot bool   `json:"requires_source_snapshot"`
			RequiresContextPacket  bool   `json:"requires_context_packet"`
			SourceSnapshotID       string `json:"source_snapshot_id"`
			ContextPacketID        string `json:"context_packet_id"`
			CoverageReportPath     string `json:"coverage_report_path"`
		} `json:"handoff_readiness"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal pass context payload: %v", err)
	}
	if payload.ProjectID != fixture.projectID || payload.PlanID != "plan-123" || payload.PassID != "PASS-001" {
		t.Fatalf("unexpected pass context identity: %+v", payload)
	}
	if len(payload.ContextPlan) == 0 || len(payload.SourceSnapshotRequirements) == 0 || len(payload.HandoffReadinessCriteria) == 0 {
		t.Fatalf("expected plan v2 context fields, got %+v", payload)
	}
	if payload.LatestSourceSnapshot == nil || payload.LatestSourceSnapshot.SourceSnapshotID == "" {
		t.Fatalf("expected latest source snapshot metadata, got %+v", payload)
	}
	if payload.LatestContextPacket == nil || payload.LatestContextPacket.ContextPacketID != packetID {
		t.Fatalf("expected latest context packet %q, got %+v", packetID, payload)
	}
	if !payload.CoverageReadiness["source_snapshot_available"] || !payload.CoverageReadiness["context_packet_available"] || !payload.CoverageReadiness["ready_for_handoff"] {
		t.Fatalf("unexpected readiness flags: %+v", payload.CoverageReadiness)
	}
	if payload.HandoffReadiness.Status != "ready" || !payload.HandoffReadiness.ReadyForHandoff || !payload.HandoffReadiness.RequiresSourceSnapshot || !payload.HandoffReadiness.RequiresContextPacket {
		t.Fatalf("unexpected handoff readiness: %+v", payload.HandoffReadiness)
	}
	if payload.HandoffReadiness.SourceSnapshotID != fixture.snapshotID || payload.HandoffReadiness.ContextPacketID != packetID || payload.HandoffReadiness.CoverageReportPath == "" {
		t.Fatalf("expected source/context IDs in handoff readiness, got %+v", payload.HandoffReadiness)
	}
}

func TestHandleGetPassContextBlocksWhenContextPacketMissing(t *testing.T) {
	fixture := setupBrokerFixture(t)
	result := callTool(t, fixture.server, ToolGetPassContext.Name, json.RawMessage(`{
		"plan_id":"plan-123",
		"pass_id":"PASS-001",
		"include_latest_source_snapshot":true,
		"include_latest_context_packet":true
	}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	success := decodeBrokerSuccess(t, result)
	var payload struct {
		LatestSourceSnapshot *struct {
			SourceSnapshotID string `json:"source_snapshot_id"`
		} `json:"latest_source_snapshot"`
		LatestContextPacket *struct {
			ContextPacketID string `json:"context_packet_id"`
		} `json:"latest_context_packet"`
		HandoffReadiness struct {
			Status          string `json:"status"`
			ReadyForHandoff bool   `json:"ready_for_handoff"`
			MissingEvidence []struct {
				Code string `json:"code"`
			} `json:"missing_evidence"`
			NextActions []struct {
				Tool string `json:"tool"`
			} `json:"next_actions"`
		} `json:"handoff_readiness"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal pass context payload: %v", err)
	}
	if payload.LatestSourceSnapshot == nil || payload.LatestSourceSnapshot.SourceSnapshotID != fixture.snapshotID {
		t.Fatalf("expected latest snapshot metadata, got %+v", payload)
	}
	if payload.LatestContextPacket != nil {
		t.Fatalf("did not expect context packet metadata before packet creation: %+v", payload.LatestContextPacket)
	}
	if payload.HandoffReadiness.Status != "blocked" || payload.HandoffReadiness.ReadyForHandoff {
		t.Fatalf("expected blocked readiness, got %+v", payload.HandoffReadiness)
	}
	if len(payload.HandoffReadiness.MissingEvidence) == 0 || payload.HandoffReadiness.MissingEvidence[0].Code != "context_packet_missing" {
		t.Fatalf("expected context_packet_missing evidence, got %+v", payload.HandoffReadiness.MissingEvidence)
	}
	if len(payload.HandoffReadiness.NextActions) == 0 || payload.HandoffReadiness.NextActions[0].Tool != "create_context_packet" {
		t.Fatalf("expected create_context_packet next action, got %+v", payload.HandoffReadiness.NextActions)
	}
}

func TestHandleCreateRunFromPlannerHandoffValidatesExplicitSourceContext(t *testing.T) {
	fixture := setupBrokerFixture(t)
	packetID := createContextPacketViaTool(t, fixture, "run-provenance")
	markdown := "---\ntitle: Source Context Run\nrepo_target: test-repo\nbranch_context: main\n---\n\n# Source Context Run\n\nGrounded handoff."

	validArgs, _ := json.Marshal(map[string]string{
		"planner_handoff_markdown": markdown,
		"repo_target":              "test-repo",
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-001",
		"context_packet_id":        packetID,
		"source_snapshot_id":       fixture.snapshotID,
	})
	validResult := fixture.server.HandleCreateRunFromPlannerHandoff(validArgs)
	if validResult.IsError {
		t.Fatalf("expected valid provenance to create run, got: %s", validResult.Content[0].Text)
	}
	var out createRunOutput
	if err := json.Unmarshal([]byte(validResult.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal create run output: %v", err)
	}
	if out.Provenance.ContextPacketID != packetID || out.Provenance.SourceSnapshotID != fixture.snapshotID {
		t.Fatalf("expected output provenance IDs, got %+v", out.Provenance)
	}
	row, err := fixture.deps.Store.GetRunSubmissionProvenanceByRun(out.RunID)
	if err != nil {
		t.Fatalf("GetRunSubmissionProvenanceByRun error: %v", err)
	}
	if row.ContextPacketID != packetID || row.SourceSnapshotID != fixture.snapshotID {
		t.Fatalf("expected stored provenance IDs, got %+v", row)
	}

	invalidArgs, _ := json.Marshal(map[string]string{
		"planner_handoff_markdown": markdown,
		"repo_target":              "test-repo",
		"plan_id":                  "plan-123",
		"pass_id":                  "PASS-001",
		"context_packet_id":        "ctxpkt-missing",
	})
	invalidResult := fixture.server.HandleCreateRunFromPlannerHandoff(invalidArgs)
	if !invalidResult.IsError {
		t.Fatal("expected missing explicit context_packet_id to be rejected")
	}
	if !strings.Contains(invalidResult.Content[0].Text, "NOT_FOUND") {
		t.Fatalf("expected NOT_FOUND for missing context packet, got %s", invalidResult.Content[0].Text)
	}
}

func TestHandleCreateSourceSnapshotReturnsMetadata(t *testing.T) {
	fixture := setupBrokerFixture(t)
	result := callTool(t, fixture.server, ToolCreateSourceSnapshot.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_ids":["relay"],
		"include_file_metadata":true,
		"max_files_per_repo":200
	}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	success := decodeBrokerSuccess(t, result)
	var payload struct {
		SourceSnapshotID string `json:"source_snapshot_id"`
		Repositories     []struct {
			RepoID string `json:"repo_id"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal source snapshot payload: %v", err)
	}
	if payload.SourceSnapshotID == "" || len(payload.Repositories) != 1 || payload.Repositories[0].RepoID != "relay" {
		t.Fatalf("unexpected source snapshot payload: %+v", payload)
	}
}

func TestHandleListProjectFilesReturnsProvenance(t *testing.T) {
	fixture := setupBrokerFixture(t)
	result := callTool(t, fixture.server, ToolListProjectFiles.Name, json.RawMessage(`{
		"project_id":"relay",
		"source_snapshot_id":"`+fixture.snapshotID+`",
		"repo_ids":["relay"],
		"max_results":10
	}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	success := decodeBrokerSuccess(t, result)
	var payload struct {
		SourceSnapshotID string `json:"source_snapshot_id"`
		Files            []struct {
			ProjectID        string `json:"project_id"`
			RepoID           string `json:"repo_id"`
			SourceSnapshotID string `json:"source_snapshot_id"`
			Path             string `json:"path"`
			ContentHash      string `json:"content_hash"`
			IndexedAt        string `json:"indexed_at"`
		} `json:"files"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal inventory payload: %v", err)
	}
	if payload.SourceSnapshotID != fixture.snapshotID || len(payload.Files) == 0 {
		t.Fatalf("unexpected inventory payload: %+v", payload)
	}
	if payload.Files[0].ProjectID == "" || payload.Files[0].RepoID == "" || payload.Files[0].Path == "" || payload.Files[0].ContentHash == "" || payload.Files[0].IndexedAt == "" {
		t.Fatalf("expected provenance fields, got %+v", payload.Files[0])
	}
}

func TestHandleSearchProjectFilesReturnsProvenance(t *testing.T) {
	requireBrokerRG(t)
	fixture := setupBrokerFixture(t)
	result := callTool(t, fixture.server, ToolSearchProjectFiles.Name, json.RawMessage(`{
		"project_id":"relay",
		"source_snapshot_id":"`+fixture.snapshotID+`",
		"repo_ids":["relay"],
		"pattern":"needle",
		"case_sensitive":false,
		"context_lines":0,
		"max_results":10,
		"max_bytes":65536
	}`))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	success := decodeBrokerSuccess(t, result)
	var payload struct {
		Matches []struct {
			ProjectID        string `json:"project_id"`
			RepoID           string `json:"repo_id"`
			SourceSnapshotID string `json:"source_snapshot_id"`
			Path             string `json:"path"`
			LineStart        int    `json:"line_start"`
			SnippetHash      string `json:"snippet_hash"`
			ContentHash      string `json:"content_hash"`
			GeneratedAt      string `json:"generated_at"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal search payload: %v", err)
	}
	if len(payload.Matches) == 0 {
		t.Fatalf("expected at least one search match")
	}
	match := payload.Matches[0]
	if match.ProjectID == "" || match.RepoID == "" || match.SourceSnapshotID == "" || match.Path == "" || match.LineStart == 0 || match.SnippetHash == "" || match.ContentHash == "" || match.GeneratedAt == "" {
		t.Fatalf("expected provenance-rich search match, got %+v", match)
	}
}

func TestHandleReadProjectFileReturnsProvenanceAndStaleBlocker(t *testing.T) {
	fixture := setupBrokerFixture(t)
	readArgs := func() json.RawMessage {
		return json.RawMessage(`{
			"project_id":"relay",
			"source_snapshot_id":"` + fixture.snapshotID + `",
			"repo_id":"relay",
			"path":"src/app.txt",
			"line_start":1,
			"line_end":2,
			"max_bytes":65536
		}`)
	}

	first := callTool(t, fixture.server, ToolReadProjectFile.Name, readArgs())
	if first.IsError {
		t.Fatalf("unexpected error: %s", first.Content[0].Text)
	}
	success := decodeBrokerSuccess(t, first)
	var payload struct {
		ProjectID        string `json:"project_id"`
		RepoID           string `json:"repo_id"`
		SourceSnapshotID string `json:"source_snapshot_id"`
		Path             string `json:"path"`
		ContentHash      string `json:"content_hash"`
		CurrentHash      string `json:"current_hash"`
		SnippetHash      string `json:"snippet_hash"`
		GeneratedAt      string `json:"generated_at"`
		Blockers         []struct {
			Code string `json:"code"`
		} `json:"blockers"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal read payload: %v", err)
	}
	if payload.ProjectID == "" || payload.RepoID == "" || payload.SourceSnapshotID == "" || payload.Path == "" || payload.ContentHash == "" || payload.CurrentHash == "" || payload.SnippetHash == "" || payload.GeneratedAt == "" {
		t.Fatalf("expected provenance fields in read payload, got %+v", payload)
	}
	if len(payload.Blockers) != 0 {
		t.Fatalf("did not expect blockers before file mutation: %+v", payload.Blockers)
	}

	brokerWriteFile(t, filepath.Join(fixture.repoRoot, "src", "app.txt"), "line one\nchanged\nneedle\n")
	second := callTool(t, fixture.server, ToolReadProjectFile.Name, readArgs())
	if second.IsError {
		t.Fatalf("unexpected tool error after mutation: %s", second.Content[0].Text)
	}
	success = decodeBrokerSuccess(t, second)
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal mutated read payload: %v", err)
	}
	if len(payload.Blockers) != 1 || payload.Blockers[0].Code != sources.SourceBlockerSnapshotFileChanged {
		t.Fatalf("expected stale snapshot blocker, got %+v", payload.Blockers)
	}
}

func TestHandleRepositoryGitToolsReturnBoundedEvidence(t *testing.T) {
	fixture := setupBrokerFixture(t)
	brokerWriteFile(t, filepath.Join(fixture.repoRoot, "src", "changed.txt"), "changed one\n")
	brokerWriteFile(t, filepath.Join(fixture.repoRoot, "src", "changed-two.txt"), "changed two\n")
	brokerRunGit(t, fixture.repoRoot, "add", "src/changed.txt", "src/changed-two.txt")
	brokerWriteFile(t, filepath.Join(fixture.repoRoot, "src", "app.txt"), "line one\nchanged\nneedle\n")

	statusResult := callTool(t, fixture.server, ToolGetRepositoryGitStatus.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_id":"relay"
	}`))
	if statusResult.IsError {
		t.Fatalf("unexpected status error: %s", statusResult.Content[0].Text)
	}
	statusSuccess := decodeBrokerSuccess(t, statusResult)
	var statusPayload struct {
		ProjectID          string `json:"project_id"`
		RepoID             string `json:"repo_id"`
		GeneratedAt        string `json:"generated_at"`
		RedactionStatus    string `json:"redaction_status"`
		Truncated          bool   `json:"truncated"`
		Dirty              bool   `json:"dirty"`
		StagedCount        int    `json:"staged_count"`
		UnstagedCount      int    `json:"unstaged_count"`
		GitStatusAvailable bool   `json:"git_status_available"`
	}
	if err := json.Unmarshal(statusSuccess.Result, &statusPayload); err != nil {
		t.Fatalf("unmarshal status payload: %v", err)
	}
	if statusPayload.ProjectID != "relay" || statusPayload.RepoID != "relay" || statusPayload.GeneratedAt == "" || statusPayload.RedactionStatus != sources.RedactionStatusNotNeeded || statusPayload.Truncated {
		t.Fatalf("unexpected status provenance: %+v", statusPayload)
	}
	if !statusPayload.Dirty || statusPayload.StagedCount != 2 || statusPayload.UnstagedCount == 0 || !statusPayload.GitStatusAvailable {
		t.Fatalf("unexpected status counts: %+v", statusPayload)
	}

	commitResult := callTool(t, fixture.server, ToolGetRepositoryRecentCommit.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_id":"relay"
	}`))
	if commitResult.IsError {
		t.Fatalf("unexpected recent commit error: %s", commitResult.Content[0].Text)
	}
	if strings.Contains(commitResult.Content[0].Text, "author_email") || strings.Contains(commitResult.Content[0].Text, fixture.repoRoot) {
		t.Fatalf("recent commit leaked disallowed data: %s", commitResult.Content[0].Text)
	}

	filesResult := callTool(t, fixture.server, ToolListRepositoryChangedFiles.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_id":"relay",
		"mode":"staged",
		"max_results":1
	}`))
	if filesResult.IsError {
		t.Fatalf("unexpected changed files error: %s", filesResult.Content[0].Text)
	}
	filesSuccess := decodeBrokerSuccess(t, filesResult)
	var filesPayload struct {
		ProjectID       string `json:"project_id"`
		RepoID          string `json:"repo_id"`
		GeneratedAt     string `json:"generated_at"`
		RedactionStatus string `json:"redaction_status"`
		Truncated       bool   `json:"truncated"`
		Mode            string `json:"mode"`
		MaxResults      int    `json:"max_results"`
		Files           []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(filesSuccess.Result, &filesPayload); err != nil {
		t.Fatalf("unmarshal changed files payload: %v", err)
	}
	if filesPayload.ProjectID != "relay" || filesPayload.RepoID != "relay" || filesPayload.GeneratedAt == "" || filesPayload.RedactionStatus != sources.RedactionStatusNotNeeded {
		t.Fatalf("unexpected changed files provenance: %+v", filesPayload)
	}
	if filesPayload.Mode != sources.DiffModeStaged || filesPayload.MaxResults != 1 || len(filesPayload.Files) != 1 || !filesPayload.Truncated {
		t.Fatalf("expected capped changed files result, got %+v", filesPayload)
	}

	diffResult := callTool(t, fixture.server, ToolGetRepositoryDiff.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_id":"relay",
		"mode":"worktree",
		"max_bytes":128,
		"context_lines":1
	}`))
	if diffResult.IsError {
		t.Fatalf("unexpected diff error: %s", diffResult.Content[0].Text)
	}
	if strings.Contains(diffResult.Content[0].Text, fixture.repoRoot) || strings.Contains(diffResult.Content[0].Text, "local_path") {
		t.Fatalf("diff leaked local path data: %s", diffResult.Content[0].Text)
	}
	diffSuccess := decodeBrokerSuccess(t, diffResult)
	var diffPayload struct {
		ProjectID       string `json:"project_id"`
		RepoID          string `json:"repo_id"`
		GeneratedAt     string `json:"generated_at"`
		RedactionStatus string `json:"redaction_status"`
		Truncated       bool   `json:"truncated"`
		Mode            string `json:"mode"`
		Content         string `json:"content"`
		ContentHash     string `json:"content_hash"`
		MaxBytes        int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(diffSuccess.Result, &diffPayload); err != nil {
		t.Fatalf("unmarshal diff payload: %v", err)
	}
	if diffPayload.ProjectID != "relay" || diffPayload.RepoID != "relay" || diffPayload.GeneratedAt == "" || diffPayload.Mode != sources.DiffModeWorktree {
		t.Fatalf("unexpected diff provenance: %+v", diffPayload)
	}
	if diffPayload.MaxBytes != 128 || diffPayload.ContentHash == "" || diffPayload.RedactionStatus == "" || diffPayload.Content == "" {
		t.Fatalf("expected bounded diff metadata and content, got %+v", diffPayload)
	}
}

func TestHandleRepositoryGitToolsValidateScopeAndMode(t *testing.T) {
	fixture := setupBrokerFixture(t)
	for name, raw := range map[string]json.RawMessage{
		"missing project": json.RawMessage(`{"repo_id":"relay"}`),
		"missing repo":    json.RawMessage(`{"project_id":"relay"}`),
	} {
		t.Run(name, func(t *testing.T) {
			result := callTool(t, fixture.server, ToolGetRepositoryGitStatus.Name, raw)
			if !result.IsError {
				t.Fatalf("expected validation error")
			}
			errEnvelope := decodeBrokerError(t, result)
			if errEnvelope.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %+v", errEnvelope)
			}
		})
	}

	result := callTool(t, fixture.server, ToolGetRepositoryDiff.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_id":"relay",
		"mode":"everything"
	}`))
	if !result.IsError {
		t.Fatalf("expected unsupported mode error")
	}
	if errEnvelope := decodeBrokerError(t, result); errEnvelope.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %+v", errEnvelope)
	}

	result = callTool(t, fixture.server, ToolGetRepositoryGitStatus.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_id":"unknown"
	}`))
	if !result.IsError {
		t.Fatalf("expected unknown repo error")
	}
	if errEnvelope := decodeBrokerError(t, result); errEnvelope.Error.Code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND, got %+v", errEnvelope)
	}
}

func TestHandleRepositoryGitToolsRequireStore(t *testing.T) {
	srv := NewServer(discardLogger(), &MCPDeps{ContextBrokerEnabled: true})
	result := callTool(t, srv, ToolGetRepositoryGitStatus.Name, json.RawMessage(`{
		"project_id":"relay",
		"repo_id":"relay"
	}`))
	if !result.IsError {
		t.Fatalf("expected dependency error")
	}
	if errEnvelope := decodeBrokerError(t, result); errEnvelope.Error.Code != "DEPENDENCY_ERROR" {
		t.Fatalf("expected DEPENDENCY_ERROR, got %+v", errEnvelope)
	}
}

func TestHandleCreateAndGetContextPacketMetadata(t *testing.T) {
	fixture := setupBrokerFixture(t)
	packetID := createContextPacketViaTool(t, fixture, "metadata")

	getResult := callTool(t, fixture.server, ToolGetContextPacket.Name, json.RawMessage(`{
		"context_packet_id":"`+packetID+`",
		"include_sources":true
	}`))
	if getResult.IsError {
		t.Fatalf("unexpected get_context_packet error: %s", getResult.Content[0].Text)
	}
	success := decodeBrokerSuccess(t, getResult)
	var payload struct {
		ContextPacketID    string `json:"context_packet_id"`
		ProjectID          string `json:"project_id"`
		PacketJSONPath     string `json:"packet_json_path"`
		PacketMarkdownPath string `json:"packet_markdown_path"`
		CoverageReportPath string `json:"coverage_report_path"`
		Sources            []struct {
			SourceID        string `json:"source_id"`
			Path            string `json:"path"`
			RedactionStatus string `json:"redaction_status"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal context packet payload: %v", err)
	}
	if payload.ContextPacketID != packetID || payload.ProjectID != fixture.projectID {
		t.Fatalf("unexpected context packet identity: %+v", payload)
	}
	if payload.PacketJSONPath == "" || payload.PacketMarkdownPath == "" || payload.CoverageReportPath == "" {
		t.Fatalf("expected sanitized artifact paths, got %+v", payload)
	}
	if filepath.IsAbs(payload.PacketJSONPath) || filepath.IsAbs(payload.PacketMarkdownPath) || filepath.IsAbs(payload.CoverageReportPath) {
		t.Fatalf("expected relative artifact paths, got %+v", payload)
	}
	if len(payload.Sources) == 0 || payload.Sources[0].SourceID == "" || payload.Sources[0].Path == "" || payload.Sources[0].RedactionStatus == "" {
		t.Fatalf("expected source metadata rows, got %+v", payload.Sources)
	}
}

func setupBrokerFixture(t *testing.T) brokerFixture {
	t.Helper()
	requireBrokerGit(t)

	deps := setupTestDeps(t)
	deps.ContextBrokerEnabled = true
	srv := NewServer(discardLogger(), deps)

	projectService := projects.NewService(deps.Store)
	sourceService := sources.NewService(deps.Store)

	repoRoot := brokerSetupGitRepo(t)
	brokerMkdirAll(t, filepath.Join(repoRoot, "src"))
	brokerMkdirAll(t, filepath.Join(repoRoot, "ignored"))
	brokerWriteFile(t, filepath.Join(repoRoot, "src", "app.txt"), "line one\nline two\nneedle\n")
	brokerWriteFile(t, filepath.Join(repoRoot, "src", "token.txt"), "Authorization: Bearer super-secret-token\n")
	brokerWriteFile(t, filepath.Join(repoRoot, "ignored", "secret.txt"), "secret\n")
	brokerRunGit(t, repoRoot, "add", ".")
	brokerRunGit(t, repoRoot, "commit", "-m", "broker fixture")

	project, err := projectService.GetProjectByProjectID(t.Context(), "relay")
	if err != nil {
		var issues []projects.ProjectValidationIssue
		project, issues, err = projectService.CreateProject(t.Context(), projects.ProjectInput{
			ProjectID: "relay",
			Name:      "Relay",
			Status:    projects.ProjectStatusActive,
		})
		if err != nil {
			t.Fatalf("CreateProject error: %v", err)
		}
		if len(issues) != 0 {
			t.Fatalf("unexpected project issues: %+v", issues)
		}
	}
	_, repoIssues, err := projectService.UpsertProjectRepository(t.Context(), project.ProjectID, projects.ProjectRepositoryInput{
		RepoID:           "relay",
		Role:             projects.RepositoryRolePrimary,
		LocalPath:        repoRoot,
		DefaultBranch:    "main",
		AllowedRoots:     []string{"src", "ignored"},
		IgnoredGlobs:     []string{"ignored/**"},
		MaxFileSizeBytes: projects.MinMaxFileSizeBytes,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("UpsertProjectRepository error: %v", err)
	}
	if len(repoIssues) != 0 {
		t.Fatalf("unexpected repository issues: %+v", repoIssues)
	}

	snapshot, err := sourceService.CreateSourceSnapshot(t.Context(), sources.SourceSnapshotInput{
		ProjectID:           project.ProjectID,
		IncludeFileMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}

	planArgs, _ := json.Marshal(map[string]string{
		"planner_pass_plan_json": string(mustMarshalPlannerPassPlan(t, validPlannerPassPlan())),
	})
	planResult := srv.HandleSubmitPlannerPassPlan(planArgs)
	if planResult.IsError {
		t.Fatalf("submit plan failed: %s", planResult.Content[0].Text)
	}

	return brokerFixture{
		deps:       deps,
		server:     srv,
		repoRoot:   repoRoot,
		projectID:  project.ProjectID,
		snapshotID: snapshot.SourceSnapshotID,
	}
}

func createContextPacketViaTool(t *testing.T, fixture brokerFixture, slug string) string {
	t.Helper()
	runsBefore := countTableRows(t, fixture.deps.Store.DB(), "runs")
	result := callTool(t, fixture.server, ToolCreateContextPacket.Name, json.RawMessage(`{
		"project_id":"relay",
		"plan_id":"plan-123",
		"pass_id":"PASS-001",
		"task_slug":"`+slug+`",
		"source_snapshot_id":"`+fixture.snapshotID+`",
		"seed_files":[
			{
				"repo_id":"relay",
				"path":"src/app.txt",
				"line_start":1,
				"line_end":3,
				"reason":"Confirm broker source grounding.",
				"required":true,
				"max_bytes":65536
			}
		],
		"seed_searches":[
			{
				"repo_ids":["relay"],
				"pattern":"needle",
				"case_sensitive":false,
				"context_lines":0,
				"max_results":10,
				"reason":"Locate the broker fixture marker.",
				"required":true
			}
		],
		"include_inventory":false,
		"max_sources":25,
		"max_total_bytes":262144
	}`))
	if result.IsError {
		t.Fatalf("create_context_packet failed: %s", result.Content[0].Text)
	}
	success := decodeBrokerSuccess(t, result)
	var payload struct {
		ContextPacketID string `json:"context_packet_id"`
		PacketJSONPath  string `json:"packet_json_path"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal create_context_packet payload: %v", err)
	}
	if payload.ContextPacketID == "" || payload.PacketJSONPath == "" {
		t.Fatalf("unexpected create_context_packet payload: %+v", payload)
	}
	row, err := fixture.deps.Store.GetContextPacketByID(payload.ContextPacketID)
	if err != nil {
		t.Fatalf("GetContextPacketByID error: %v", err)
	}
	if row.SourceCount == 0 {
		t.Fatalf("expected stored context packet sources, got %+v", row)
	}
	if runsAfter := countTableRows(t, fixture.deps.Store.DB(), "runs"); runsAfter != runsBefore {
		t.Fatalf("create_context_packet should not create runs: before=%d after=%d", runsBefore, runsAfter)
	}
	return payload.ContextPacketID
}

func listTools(t *testing.T, srv *Server) ToolsListResult {
	t.Helper()
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}
	return list
}

func hasTool(list ToolsListResult, name string) bool {
	for _, tool := range list.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func brokerToolNames() []string {
	return []string{
		"get_project",
		"get_plan",
		"get_pass",
		"get_pass_context",
		"create_source_snapshot",
		"list_project_files",
		"search_project_files",
		"read_project_file",
		"get_repository_git_status",
		"get_repository_recent_commit",
		"list_repository_changed_files",
		"get_repository_diff",
		"create_context_packet",
		"get_context_packet",
		"search_project_context_memory",
		"list_project_context_records",
		"get_project_context_record",
		"create_project_context_record",
		"supersede_project_context_record",
	}
}

func callTool(t *testing.T, srv *Server, name string, args json.RawMessage) ToolCallResult {
	t.Helper()
	params, _ := json.Marshal(ToolCallParams{Name: name, Arguments: args})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`99`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("tools/call %s rpc error: %+v", name, resp.Error)
	}
	var result ToolCallResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal tool result for %s: %v", name, err)
	}
	if len(result.Content) == 0 {
		t.Fatalf("expected content for tool %s", name)
	}
	return result
}

func decodeBrokerSuccess(t *testing.T, result ToolCallResult) brokerSuccessEnvelope {
	t.Helper()
	var success brokerSuccessEnvelope
	if err := json.Unmarshal([]byte(result.Content[0].Text), &success); err != nil {
		t.Fatalf("unmarshal broker success: %v\n%s", err, result.Content[0].Text)
	}
	if !success.OK {
		t.Fatalf("expected success payload, got %s", result.Content[0].Text)
	}
	return success
}

func decodeBrokerError(t *testing.T, result ToolCallResult) brokerErrorEnvelope {
	t.Helper()
	var failure brokerErrorEnvelope
	if err := json.Unmarshal([]byte(result.Content[0].Text), &failure); err != nil {
		t.Fatalf("unmarshal broker error: %v\n%s", err, result.Content[0].Text)
	}
	if failure.OK {
		t.Fatalf("expected error payload, got %s", result.Content[0].Text)
	}
	return failure
}

func requireBrokerGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func requireBrokerRG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not available")
	}
}

func brokerSetupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	brokerRunGit(t, dir, "init", "-b", "main")
	brokerRunGit(t, dir, "config", "user.name", "Relay Test")
	brokerRunGit(t, dir, "config", "user.email", "relay@example.test")
	return dir
}

func brokerRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func brokerMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

func brokerWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
