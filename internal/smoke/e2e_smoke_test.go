package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"relay/internal/api"
	"relay/internal/artifacts"
	"relay/internal/repos"
	"relay/internal/server"
	"relay/internal/store"
)

var smokePlanJSON = `{
  "plan_meta": {
    "plan_id": "e2e-smoke-plan-1",
    "schema_version": "2.0.0",
    "created_at": "2026-06-21T00:00:00Z",
    "title": "E2E Smoke Plan",
    "goal": "Verify E2E path.",
    "repo_target": "smoke-test-repo",
    "branch_context": "main",
    "status": "active",
    "project_id": "smoke-project"
  },
  "source_intent": {
    "summary": "Synthetic E2E smoke plan."
  },
  "passes": [
    {
      "pass_id": "PASS-001",
      "sequence": 1,
      "name": "First smoke pass",
      "goal": "Validate step.",
      "intended_execution_scope": ["src/ui/overflowPage.ts"],
      "non_goals": ["No production data mutation"],
      "dependencies": [],
      "status": "planned",
      "pass_type": "backend_vertical_slice",
      "context_plan": {
        "required_repositories": ["smoke-test-repo"],
        "seed_search_terms": [
          {"repo_id": "smoke-test-repo", "query": "overflowPage", "purpose": "Locate UI code.", "required": true}
        ],
        "seed_files_to_read": [
          {"repo_id": "smoke-test-repo", "path": "src/ui/overflowPage.ts", "purpose": "Locate UI code.", "required": true}
        ],
        "context_coverage_expectations": ["Coverage expectations."],
        "blocked_if_missing": ["Blocked if missing."]
      },
      "source_snapshot_requirements": {
        "require_git_status": true,
        "require_commit_sha": false,
        "allow_dirty_worktree": true
      },
      "handoff_readiness_criteria": ["Readiness criteria."]
    }
  ]
}`

func findHandoffFixture() string {
	dir := "."
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, "internal/compiler/testdata/formal_planner_handoff.md")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		dir = filepath.Join(dir, "..")
	}
	return ""
}

func TestE2EPipelineSmoke(t *testing.T) {
	fixturePath := findHandoffFixture()
	if fixturePath == "" {
		t.Fatal("could not find formal_planner_handoff.md fixture")
	}
	smokeHandoffMarkdownBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read formal_planner_handoff.md: %v", err)
	}
	smokeHandoffMarkdown := string(smokeHandoffMarkdownBytes)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Direct artifacts base directory to our temp directory
	artifacts.SetBaseDir(filepath.Join(dir, "artifacts"))
	t.Cleanup(func() { artifacts.SetBaseDir("data/artifacts") })

	s, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Seed repository
	repo, err := s.CreateRepo("smoke-test-repo", filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	rs := repos.NewService(s, logger)
	handler := server.BuildRoutes(s, rs, logger)

	// Create project for validation/submission
	project, err := s.CreateProject("smoke-project", "Smoke Project", "E2E Smoke Project", "active", "")
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Seed a completed source snapshot for the project. PASS-001 declares
	// source/context requirements, so the intake provenance gate requires a
	// valid source_snapshot_id (or context_packet_id) before a managed run can
	// be created. This mirrors the snapshot a Planner produces during context
	// gathering.
	const smokeSourceSnapshotID = "e2e-smoke-snapshot-1"
	if _, err := s.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: smokeSourceSnapshotID,
		ProjectRowID:     project.ID,
		ProjectID:        "smoke-project",
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-21T00:00:00Z",
		SummaryJSON:      "{}",
	}); err != nil {
		t.Fatalf("failed to seed source snapshot: %v", err)
	}

	// 1. Submit Plan v2 JSON to /api/plans
	t.Log("1. Submit Plan v2 JSON")
	planPayload := fmt.Sprintf(`{"plan":%s,"sourceArtifactPath":"handoffs/plans/e2e-smoke-plan.json","projectId":"smoke-project"}`, smokePlanJSON)
	req := httptest.NewRequest("POST", "/api/plans", strings.NewReader(planPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected plan submit 201, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify plan is active
	dbPlan, err := s.GetPlanByPlanID("e2e-smoke-plan-1")
	if err != nil || dbPlan == nil {
		t.Fatalf("failed to load plan from database: %v", err)
	}

	// 2. Submit handoff to /api/intake/planner-handoff to create pass-associated run
	t.Log("2. Submit planner handoff to create run")
	handoffReq := api.PlannerHandoffIntakeRequest{
		PlannerHandoffMarkdown: smokeHandoffMarkdown,
		Repo:                   "smoke-test-repo",
		Branch:                 "main",
		PlanID:                 "e2e-smoke-plan-1",
		PassID:                 "PASS-001",
		SourceSnapshotID:       smokeSourceSnapshotID,
	}
	handoffReqBytes, _ := json.Marshal(handoffReq)
	req = httptest.NewRequest("POST", "/api/intake/planner-handoff", bytes.NewReader(handoffReqBytes))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected handoff intake 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var handoffResp api.PlannerHandoffIntakeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &handoffResp); err != nil {
		t.Fatalf("failed to decode handoff response: %v", err)
	}
	runID, err := strconv.ParseInt(handoffResp.RunID, 10, 64)
	if err != nil {
		t.Fatalf("invalid run ID from handoff: %v", err)
	}

	// Verify provenance and plan association
	dbRun, err := s.GetRun(runID)
	if err != nil || dbRun == nil {
		t.Fatalf("failed to load run %d: %v", runID, err)
	}
	if !dbRun.PlanRowID.Valid || !dbRun.PlanPassRowID.Valid {
		t.Fatalf("expected run %d to be associated with plan/pass, got PlanRowID: %+v, PassRowID: %+v", runID, dbRun.PlanRowID, dbRun.PlanPassRowID)
	}

	// 3. Approve intake: POST /api/runs/{id}/approve-intake
	t.Log("3. Approve intake")
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/approve-intake", runID), strings.NewReader(`{"action":"approve"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected approve-intake 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	dbRun, _ = s.GetRun(runID)
	if dbRun.Status != "approved_for_prepare" {
		t.Fatalf("expected status approved_for_prepare, got %q", dbRun.Status)
	}

	// 4. Write compiler inputs (run_config.json) into the run's artifacts directory
	t.Log("4. Write compile inputs")
	configMap := map[string]interface{}{
		"repo_target":    repo.Path,
		"branch_context": "main",
		"file_targets":   []string{"src/ui/overflowPage.ts"},
	}
	configJSON, _ := json.Marshal(configMap)
	configPath, err := artifacts.Write(runID, "run_config", "run_config.json", configJSON)
	if err != nil {
		t.Fatalf("failed to write run_config.json: %v", err)
	}
	if _, err := s.CreateArtifact(runID, "run_config", configPath, "application/json"); err != nil {
		t.Fatalf("failed to register run_config artifact: %v", err)
	}

	// 5. Compile/prepare: POST /api/runs/{id}/prepare
	t.Log("5. Compile/prepare run")
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/prepare", runID), nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected prepare 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	dbRun, _ = s.GetRun(runID)
	if dbRun.Status != "packet_validated" {
		t.Fatalf("expected status packet_validated, got %q", dbRun.Status)
	}

	// 6. Render brief: POST /api/runs/{id}/render-brief
	t.Log("6. Render executor brief")
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/render-brief", runID), nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected render-brief 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	dbRun, _ = s.GetRun(runID)
	if dbRun.Status != "brief_ready_for_review" {
		t.Fatalf("expected status brief_ready_for_review, got %q", dbRun.Status)
	}

	// 7. Approve brief: POST /api/runs/{id}/approve-brief
	t.Log("7. Approve brief")
	approveBriefReq := `{"action":"approve"}`
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/approve-brief", runID), strings.NewReader(approveBriefReq))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected approve-brief 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	dbRun, _ = s.GetRun(runID)
	if dbRun.Status != "approved_for_executor" {
		t.Fatalf("expected status approved_for_executor, got %q", dbRun.Status)
	}

	// 8. Manually simulate executor completing.
	// Write the required validation run JSON ("validation_run_json") and set run status to "executor_done".
	t.Log("8. Simulate executor done and write validation_run_json")
	validationJSON := []byte(`{"status":"pass","errors":[],"warnings":[]}`)
	valPath, err := artifacts.Write(runID, "validation_run_json", "validation_run.json", validationJSON)
	if err != nil {
		t.Fatalf("failed to write validation_run.json: %v", err)
	}
	if _, err := s.CreateArtifact(runID, "validation_run_json", valPath, "application/json"); err != nil {
		t.Fatalf("failed to register validation_run_json artifact: %v", err)
	}
	if _, err := s.UpdateRunStatus(runID, "executor_done"); err != nil {
		t.Fatalf("failed to update run status to executor_done: %v", err)
	}

	// 9. Generate audit: POST /api/runs/{id}/audit
	// Verify status becomes "audit_ready" and no validation commands were auto-run.
	t.Log("9. Generate audit")
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/audit", runID), nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected audit generation 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	dbRun, _ = s.GetRun(runID)
	if dbRun.Status != "audit_ready" {
		t.Fatalf("expected status audit_ready, got %q", dbRun.Status)
	}

	// 10. Submit audit decision: POST /api/runs/{id}/audit/submit
	t.Log("10. Submit audit decision")
	submitPayload := `{"decision":"accepted_with_warnings","audit_packet_markdown":"# Audit Evidence\nAccepting with warnings.","notes":"Test E2E closeout."}`
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/audit/submit", runID), strings.NewReader(submitPayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected audit submit 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify run status becomes accepted (not completed/closed)
	dbRun, _ = s.GetRun(runID)
	if dbRun.Status != "accepted_with_warnings" {
		t.Fatalf("expected status accepted_with_warnings, got %q", dbRun.Status)
	}

	// Verify the associated plan pass status updates to completed
	pass, err := s.GetPlanPass(dbRun.PlanPassRowID.Int64)
	if err != nil {
		t.Fatalf("failed to get associated pass: %v", err)
	}
	if pass.Status != "completed" {
		t.Fatalf("expected associated plan pass status 'completed', got %q", pass.Status)
	}
}
