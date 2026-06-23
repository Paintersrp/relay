package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/api"
	"relay/internal/artifacts"
	"relay/internal/mcp"
	"relay/internal/plans"
	"relay/internal/repos"
	"relay/internal/server"
	"relay/internal/store"
)

func TestLocalOperatorSmoke_ContextPacketRunProvenance(t *testing.T) {
	requireSmokeGit(t)

	dir := t.TempDir()
	artifacts.SetBaseDir(filepath.Join(dir, "artifacts"))
	t.Cleanup(func() { artifacts.SetBaseDir("data/artifacts") })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	repoRoot := setupSmokeGitRepo(t)
	handler := server.BuildRoutes(st, repos.NewService(st, logger), logger)

	postJSON(t, handler, "/api/projects", http.StatusCreated, map[string]any{
		"project_id":  "smoke-relay",
		"name":        "Smoke Relay",
		"description": "PASS-006 local smoke project",
		"status":      "active",
	}, nil)
	postJSON(t, handler, "/api/projects/smoke-relay/repositories", http.StatusOK, map[string]any{
		"repo_id":             "relay",
		"role":                "primary",
		"local_path":          repoRoot,
		"default_branch":      "main",
		"allowed_roots":       []string{"."},
		"ignored_globs":       []string{".git/**"},
		"max_file_size_bytes": 262144,
		"include_untracked":   true,
		"enabled":             true,
	}, nil)

	var projectResp api.ProjectAPIResponse
	getJSON(t, handler, "/api/projects/smoke-relay", http.StatusOK, &projectResp)
	if projectResp.Project == nil || len(projectResp.Project.Repositories) != 1 {
		t.Fatalf("expected registered project repository, got %+v", projectResp)
	}

	submitSmokePlan(t, handler)

	mcpServer := mcp.NewServer(logger, &mcp.MCPDeps{
		Store:                st,
		ContextBrokerEnabled: true,
		ToolProfile:          mcp.ToolProfileLocalOperator,
	})

	runsBefore := smokeCountRows(t, st, "runs")
	sourceResult := decodeSmokeToolResult(t, mcpServer.HandleCreateSourceSnapshot(mustJSON(t, map[string]any{
		"project_id":            "smoke-relay",
		"repo_ids":              []string{"relay"},
		"include_file_metadata": true,
		"max_files_per_repo":    50,
	})))
	var sourcePayload struct {
		SourceSnapshotID string `json:"source_snapshot_id"`
		Repositories     []struct {
			RepoID    string `json:"repo_id"`
			GitStatus struct {
				HeadSHA            string
				GitStatusAvailable bool
			} `json:"git_status"`
		} `json:"repositories"`
	}
	unmarshalSmokeResult(t, sourceResult, &sourcePayload)
	if sourcePayload.SourceSnapshotID == "" || len(sourcePayload.Repositories) != 1 {
		t.Fatalf("unexpected source snapshot payload: %+v", sourcePayload)
	}
	if sourcePayload.Repositories[0].RepoID != "relay" ||
		!sourcePayload.Repositories[0].GitStatus.GitStatusAvailable ||
		sourcePayload.Repositories[0].GitStatus.HeadSHA == "" {
		t.Fatalf("expected git-backed repository evidence, got %+v", sourcePayload.Repositories)
	}

	contextResult := decodeSmokeToolResult(t, mcpServer.HandleCreateContextPacket(mustJSON(t, map[string]any{
		"project_id":         "smoke-relay",
		"plan_id":            "smoke-plan",
		"pass_id":            "PASS-006",
		"task_slug":          "pass-006-smoke",
		"source_snapshot_id": sourcePayload.SourceSnapshotID,
		"seed_files": []map[string]any{
			{
				"repo_id":    "relay",
				"path":       "README.md",
				"line_start": 1,
				"line_end":   5,
				"reason":     "Required smoke seed file.",
				"required":   true,
				"max_bytes":  65536,
			},
		},
		"include_inventory": false,
		"max_sources":       10,
		"max_total_bytes":   131072,
	})))
	var contextPayload struct {
		ContextPacketID    string `json:"context_packet_id"`
		PacketJSONPath     string `json:"packet_json_path"`
		PacketMarkdownPath string `json:"packet_markdown_path"`
		CoverageReportPath string `json:"coverage_report_path"`
		SourceCount        int    `json:"source_count"`
		CoveredSeedCount   int    `json:"covered_seed_count"`
	}
	unmarshalSmokeResult(t, contextResult, &contextPayload)
	if contextPayload.ContextPacketID == "" || contextPayload.PacketJSONPath == "" ||
		contextPayload.PacketMarkdownPath == "" || contextPayload.CoverageReportPath == "" {
		t.Fatalf("expected context packet artifacts, got %+v", contextPayload)
	}
	if contextPayload.SourceCount == 0 || contextPayload.CoveredSeedCount == 0 {
		t.Fatalf("expected required seed coverage, got %+v", contextPayload)
	}
	if got := smokeCountRows(t, st, "runs"); got != runsBefore {
		t.Fatalf("source/context evidence creation must not create runs: before=%d after=%d", runsBefore, got)
	}

	passContextResult := decodeSmokeToolResult(t, mcpServer.HandleGetPassContext(mustJSON(t, map[string]any{
		"plan_id":                        "smoke-plan",
		"pass_id":                        "PASS-006",
		"include_latest_source_snapshot": true,
		"include_latest_context_packet":  true,
	})))
	var passContextPayload struct {
		LatestContextPacket *struct {
			ContextPacketID string `json:"context_packet_id"`
		} `json:"latest_context_packet"`
		HandoffReadiness struct {
			ReadyForHandoff  bool   `json:"ready_for_handoff"`
			SourceSnapshotID string `json:"source_snapshot_id"`
			ContextPacketID  string `json:"context_packet_id"`
		} `json:"handoff_readiness"`
	}
	unmarshalSmokeResult(t, passContextResult, &passContextPayload)
	if passContextPayload.LatestContextPacket == nil ||
		passContextPayload.LatestContextPacket.ContextPacketID != contextPayload.ContextPacketID ||
		!passContextPayload.HandoffReadiness.ReadyForHandoff ||
		passContextPayload.HandoffReadiness.SourceSnapshotID != sourcePayload.SourceSnapshotID ||
		passContextPayload.HandoffReadiness.ContextPacketID != contextPayload.ContextPacketID {
		t.Fatalf("expected pass context to discover source/context evidence, got %+v", passContextPayload)
	}

	intakeReq := api.PlannerHandoffIntakeRequest{
		PlannerHandoffMarkdown: "---\ntitle: PASS-006 Smoke Handoff\nrepo_target: relay\nbranch_context: main\n---\n\n# PASS-006 Smoke Handoff\n\nReviewed local smoke handoff.",
		Repo:                   "relay",
		Branch:                 "main",
		PlanID:                 "smoke-plan",
		PassID:                 "PASS-006",
		ContextPacketID:        contextPayload.ContextPacketID,
		SourceSnapshotID:       sourcePayload.SourceSnapshotID,
	}
	var intakeResp api.PlannerHandoffIntakeResponse
	postJSON(t, handler, "/api/intake/planner-handoff", http.StatusOK, intakeReq, &intakeResp)
	if !intakeResp.Success || intakeResp.RunID == "" {
		t.Fatalf("unexpected intake response: %+v", intakeResp)
	}

	var relayRun api.RelayRun
	getJSON(t, handler, "/api/runs/"+intakeResp.RunID, http.StatusOK, &relayRun)
	if relayRun.PlanContext == nil ||
		relayRun.PlanContext.PlanID != "smoke-plan" ||
		relayRun.PlanContext.PassID != "PASS-006" ||
		relayRun.PlanContext.ContextPacketID != contextPayload.ContextPacketID ||
		relayRun.PlanContext.SourceSnapshotID != sourcePayload.SourceSnapshotID {
		t.Fatalf("expected plan/source context in run detail, got %+v", relayRun.PlanContext)
	}
	if relayRun.SourceContext == nil ||
		relayRun.SourceContext.ContextPacketID != contextPayload.ContextPacketID ||
		relayRun.SourceContext.SourceSnapshotID != sourcePayload.SourceSnapshotID {
		t.Fatalf("expected source_context IDs in run detail, got %+v", relayRun.SourceContext)
	}
}

func submitSmokePlan(t *testing.T, handler http.Handler) {
	t.Helper()
	required := true
	allowDirty := false
	plan := plans.PlannerPassPlan{
		PlanMeta: plans.PlanMeta{
			PlanID:        "smoke-plan",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "PASS-006 smoke plan",
			Goal:          "Prove local smoke source context.",
			RepoTarget:    "smoke-relay",
			BranchContext: "main",
			Status:        "active",
			MCPCapabilityProfile: &plans.MCPCapabilityProfile{
				ProfileID:            "pass-006-local-smoke",
				Mode:                 "submission_only",
				ContextBrokerEnabled: &required,
			},
		},
		SourceIntent: plans.SourceIntent{Summary: "Local-only PASS-006 smoke."},
		Passes: []plans.PlanPassInput{
			{
				PassID:                 "PASS-006",
				Sequence:               1,
				Name:                   "Local smoke",
				Goal:                   "Verify local source/context provenance.",
				IntendedExecutionScope: []string{"README.md"},
				NonGoals:               []string{"No executor dispatch"},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "testing_release_hardening",
				ContextPlan: plans.ContextPlan{
					RequiredRepositories: []string{"smoke-relay"},
					SeedSearchTerms: []plans.ContextSearchTerm{
						{RepoID: "relay", Query: "PASS-006", Purpose: "Locate the local smoke marker.", Required: &required},
					},
					SeedFilesToRead: []plans.ContextFileRead{
						{RepoID: "relay", Path: "README.md", Purpose: "Required local smoke seed.", Required: &required},
					},
					ContextCoverageExpectations: []string{"README seed is covered."},
					BlockedIfMissing:            []string{"README seed is missing."},
				},
				SourceSnapshotRequirements: plans.SourceSnapshotRequirements{
					RequireGitStatus:   &required,
					RequireCommitSHA:   &required,
					AllowDirtyWorktree: &allowDirty,
				},
				HandoffReadinessCriteria: []string{"Source snapshot and context packet are ready."},
			},
		},
	}
	postJSON(t, handler, "/api/plans", http.StatusCreated, map[string]any{
		"plan":               plan,
		"sourceArtifactPath": "handoffs/plans/pass-006-smoke.json",
	}, nil)
}

func setupSmokeGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runSmokeGit(t, root, "init", "-b", "main")
	runSmokeGit(t, root, "config", "user.email", "relay-smoke@example.invalid")
	runSmokeGit(t, root, "config", "user.name", "Relay Smoke")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Smoke Repo\n\nPASS-006 local evidence.\n"), 0644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runSmokeGit(t, root, "add", ".")
	runSmokeGit(t, root, "commit", "-m", "initial smoke fixture")
	return root
}

func requireSmokeGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func runSmokeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func postJSON(t *testing.T, handler http.Handler, path string, wantStatus int, payload any, out any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("POST %s expected %d, got %d: %s", path, wantStatus, rec.Code, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode POST %s: %v", path, err)
		}
	}
}

func getJSON(t *testing.T, handler http.Handler, path string, wantStatus int, out any) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != wantStatus {
		t.Fatalf("GET %s expected %d, got %d: %s", path, wantStatus, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode GET %s: %v", path, err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw JSON: %v", err)
	}
	return data
}

func decodeSmokeToolResult(t *testing.T, result mcp.ToolCallResult) json.RawMessage {
	t.Helper()
	if result.IsError {
		t.Fatalf("unexpected MCP tool error: %s", result.Content[0].Text)
	}
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if len(result.Content) == 0 {
		t.Fatal("expected MCP content")
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &envelope); err != nil {
		t.Fatalf("decode MCP envelope: %v\n%s", err, result.Content[0].Text)
	}
	if !envelope.OK {
		t.Fatalf("expected MCP ok envelope, got %s", result.Content[0].Text)
	}
	return envelope.Result
}

func unmarshalSmokeResult(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode MCP result: %v\n%s", err, string(raw))
	}
}

func smokeCountRows(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var count int
	if err := st.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
