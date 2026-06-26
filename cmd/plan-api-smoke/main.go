// Command plan-api-smoke is an executable smoke harness for the managed-plan HTTP API.
//
// It spins up an isolated temporary SQLite store and chi router, exercises plan
// validation/submission/read endpoints, and verifies completionReady behavior.
//
// Usage:
//
//	go run ./cmd/plan-api-smoke
//	make plan-api-smoke
//
// The harness exits 0 on full pass, nonzero on any mismatch. It never touches
// production data/relay.sqlite.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"

	plansapi "relay/internal/api/plans"
	"relay/internal/api/shared"
	appplans "relay/internal/app/plans"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

var smokePlanJSON = `{
  "plan_meta": {
    "plan_id": "plan-api-smoke-plan",
    "schema_version": "2.0.0",
    "created_at": "2026-06-21T00:00:00Z",
    "title": "Plan API Smoke Plan",
    "goal": "Verify managed plan HTTP API smoke coverage.",
    "repo_target": "smoke-test-repo",
    "branch_context": "main",
    "status": "active",
    "project_context": {
      "primary_project": "relay",
      "primary_repository": "relay",
      "github_role": "repo_host_and_origin_only"
    }
  },
  "source_intent": {
    "summary": "Synthetic smoke plan for managed-plan HTTP API coverage."
  },
  "passes": [
    {
      "pass_id": "PASS-001",
      "sequence": 1,
      "name": "Smoke pass one",
      "goal": "Validate plan submission and pass ordering.",
      "intended_execution_scope": ["cmd/plan-api-smoke/main.go"],
      "non_goals": ["No production data mutation"],
      "dependencies": [],
      "status": "planned",
      "pass_type": "backend_vertical_slice",
      "context_plan": {
        "required_repositories": ["relay"],
        "seed_search_terms": [
          {"repo_id": "relay", "query": "main", "purpose": "Locate entrypoint.", "required": true}
        ],
        "seed_files_to_read": [
          {"repo_id": "relay", "path": "cmd/plan-api-smoke/main.go", "purpose": "Inspect smoke harness.", "required": true}
        ],
        "context_coverage_expectations": ["Coverage is checked."],
        "blocked_if_missing": ["Smoke script is missing."]
      },
      "source_snapshot_requirements": {
        "require_git_status": true,
        "require_commit_sha": false,
        "allow_dirty_worktree": true
      },
      "handoff_readiness_criteria": ["Validation checks pass."]
    },
    {
      "pass_id": "PASS-002",
      "sequence": 2,
      "name": "Smoke pass two",
      "goal": "Validate pass detail and dependency reporting.",
      "intended_execution_scope": ["docs/api/frontend-api-contract.md"],
      "non_goals": ["No UI changes"],
      "dependencies": ["PASS-001"],
      "status": "planned",
      "pass_type": "testing_release_hardening",
      "context_plan": {
        "required_repositories": ["relay"],
        "seed_search_terms": [
          {"repo_id": "relay", "query": "main", "purpose": "Locate entrypoint.", "required": true}
        ],
        "seed_files_to_read": [
          {"repo_id": "relay", "path": "cmd/plan-api-smoke/main.go", "purpose": "Inspect smoke harness.", "required": true}
        ],
        "context_coverage_expectations": ["Coverage is checked."],
        "blocked_if_missing": ["Smoke script is missing."]
      },
      "source_snapshot_requirements": {
        "require_git_status": true,
        "require_commit_sha": false,
        "allow_dirty_worktree": true
      },
      "handoff_readiness_criteria": ["Validation checks pass."]
    }
  ]
}`

type harness struct {
	st     *store.Store
	router http.Handler
	pass   int
	fail   int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nPLAN API SMOKE FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	tmpDir, err := os.MkdirTemp("", "relay-plan-api-smoke-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "relay.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if _, err := st.CreateProject("relay", "Relay", "Smoke test project", "active", ""); err != nil {
		return fmt.Errorf("create smoke project: %w", err)
	}

	planSvc := appplans.NewService(st)
	planWorkSvc := appplans.NewOrchestratorWorkService(st)
	planH := plansapi.NewHandler(planSvc, planWorkSvc, nil)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(shared.CORSMiddleware)
		plansapi.MountRoutes(r, planH)
	})

	h := &harness{st: st, router: r}

	// -------------------------------------------------------
	// 1. Validate a plan without persisting it.
	// -------------------------------------------------------
	body := []byte(`{"plan":` + smokePlanJSON + `}`)
	rec := h.post("/api/plans/validate", body)
	h.check("validate status 200", rec.Code == http.StatusOK)

	var validateResp plansapi.PlanAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &validateResp); err != nil {
		return fmt.Errorf("decode validate response: %w", err)
	}
	if !validateResp.Validation.Valid {
		fmt.Printf("SMOKE TEST VALIDATION ISSUES: %+v\n", validateResp.Validation.Issues)
	}
	h.check("validate success true", validateResp.Success)
	h.check("validate validation valid", validateResp.Validation.Valid)
	h.check("validate did not persist plans", countRows(h.st.DB(), "plans") == 0)
	h.check("validate did not persist passes", countRows(h.st.DB(), "plan_passes") == 0)

	// -------------------------------------------------------
	// 2. Submit the plan and verify 201 + stored rows.
	// -------------------------------------------------------
	body = []byte(`{"plan":` + smokePlanJSON + `,"sourceArtifactPath":"handoffs/plans/plan-api-smoke.planner-pass-plan.json"}`)
	rec = h.post("/api/plans", body)
	h.check("submit status 201", rec.Code == http.StatusCreated)

	var submitResp plansapi.PlanAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &submitResp); err != nil {
		return fmt.Errorf("decode submit response: %w", err)
	}
	h.check("submit success true", submitResp.Success)
	h.check("submit returned plan", submitResp.Plan != nil)
	h.check("submit plan_id matches", submitResp.Plan != nil && submitResp.Plan.PlanID == "plan-api-smoke-plan")
	h.check("submit returned 2 passes", len(submitResp.Passes) == 2)
	h.check("submit persisted 1 plan", countRows(h.st.DB(), "plans") == 1)
	h.check("submit persisted 2 passes", countRows(h.st.DB(), "plan_passes") == 2)

	// -------------------------------------------------------
	// 3. Create a pass-associated run for read-side association checks.
	// -------------------------------------------------------
	plan, err := st.GetPlanByPlanID("plan-api-smoke-plan")
	if err != nil {
		return fmt.Errorf("lookup submitted plan: %w", err)
	}
	passTwo, err := st.GetPlanPassByPassID(plan.ID, "PASS-002")
	if err != nil {
		return fmt.Errorf("lookup PASS-002: %w", err)
	}
	repo, err := st.CreateRepo("smoke-test-repo", "smoke-test-repo")
	if err != nil {
		return fmt.Errorf("create smoke repo: %w", err)
	}
	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"Associated smoke run",
		"intake_received",
		"deepseek-v4-flash",
		"deepseek-v4-flash",
		store.DefaultExecutorAdapter,
		"main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: passTwo.ID, Valid: true},
	)
	if err != nil {
		return fmt.Errorf("create associated smoke run: %w", err)
	}
	runID := strconv.FormatInt(run.ID, 10)

	// -------------------------------------------------------
	// 4. List plans and verify passCount/completionReady.
	// -------------------------------------------------------
	rec = h.get("/api/plans")
	h.check("list status 200", rec.Code == http.StatusOK)

	var listResp plansapi.PlanReadAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		return fmt.Errorf("decode list response: %w", err)
	}
	h.check("list success true", listResp.Success)
	h.check("list count 1", listResp.Count == 1)
	h.check("list returned 1 plan", len(listResp.Plans) == 1)
	if len(listResp.Plans) == 1 {
		plan := listResp.Plans[0]
		h.check("list passCount 2", plan.PassCount == 2)
		h.check("list completionReady false", !plan.CompletionReady)
		h.check("list plan status active", plan.Status == "active")
	}

	// -------------------------------------------------------
	// 5. Get plan detail and verify pass ordering and associated runs.
	// -------------------------------------------------------
	rec = h.get("/api/plans/plan-api-smoke-plan")
	h.check("detail status 200", rec.Code == http.StatusOK)

	var detailResp plansapi.PlanReadAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detailResp); err != nil {
		return fmt.Errorf("decode detail response: %w", err)
	}
	h.check("detail success true", detailResp.Success)
	h.check("detail returned plan", detailResp.Plan != nil)
	h.check("detail plan_id matches", detailResp.Plan != nil && detailResp.Plan.PlanID == "plan-api-smoke-plan")
	h.check("detail returned 2 passes", len(detailResp.Passes) == 2)
	if len(detailResp.Passes) == 2 {
		h.check("detail pass order", detailResp.Passes[0].PassID == "PASS-001" && detailResp.Passes[1].PassID == "PASS-002")
		h.check("detail PASS-001 has no associated run ids", len(detailResp.Passes[0].AssociatedRunIDs) == 0)
		h.check("detail PASS-001 has no associated runs", len(detailResp.Passes[0].AssociatedRuns) == 0)
		h.check("detail PASS-002 has one associated run id", len(detailResp.Passes[1].AssociatedRunIDs) == 1)
		h.check("detail PASS-002 has one associated run", len(detailResp.Passes[1].AssociatedRuns) == 1)
		if len(detailResp.Passes[1].AssociatedRuns) == 1 {
			summary := detailResp.Passes[1].AssociatedRuns[0]
			h.check("detail associated run id matches", summary.ID == runID)
			h.check("detail associated run id list matches", detailResp.Passes[1].AssociatedRunIDs[0] == runID)
			h.check("detail associated run status", summary.Status == "intake_received")
			h.check("detail associated run activeStep", summary.ActiveStep == "intake")
			h.check("detail associated run workbenchPath", summary.WorkbenchPath == "/runs/"+runID+"/intake")
		}
	}
	h.check("detail completionReady false", !detailResp.CompletionReady)

	// -------------------------------------------------------
	// 6. Get pass detail and verify dependency resolution and associated runs.
	// -------------------------------------------------------
	rec = h.get("/api/plans/plan-api-smoke-plan/passes/PASS-002")
	h.check("pass detail status 200", rec.Code == http.StatusOK)

	var passResp plansapi.PlanReadAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &passResp); err != nil {
		return fmt.Errorf("decode pass detail response: %w", err)
	}
	h.check("pass detail success true", passResp.Success)
	h.check("pass detail returned parent plan", passResp.Plan != nil && passResp.Plan.PlanID == "plan-api-smoke-plan")
	h.check("pass detail returned pass", passResp.Pass != nil)
	if passResp.Pass != nil {
		h.check("pass detail passId PASS-002", passResp.Pass.PassID == "PASS-002")
		h.check("pass detail dependencies include PASS-001", len(passResp.Pass.Dependencies) == 1 && passResp.Pass.Dependencies[0] == "PASS-001")
		h.check("pass detail has one associated run id", len(passResp.Pass.AssociatedRunIDs) == 1)
		h.check("pass detail has one associated run", len(passResp.Pass.AssociatedRuns) == 1)
		if len(passResp.Pass.AssociatedRuns) == 1 {
			summary := passResp.Pass.AssociatedRuns[0]
			h.check("pass detail associated run id matches", summary.ID == runID)
			h.check("pass detail associated run id list matches", passResp.Pass.AssociatedRunIDs[0] == runID)
			h.check("pass detail associated run status", summary.Status == "intake_received")
			h.check("pass detail associated run activeStep", summary.ActiveStep == "intake")
			h.check("pass detail associated run workbenchPath", summary.WorkbenchPath == "/runs/"+runID+"/intake")
		}
	}

	// -------------------------------------------------------
	// 7. Mark passes terminal and verify completionReady without mutating plan.status.
	// -------------------------------------------------------
	plan, err = st.GetPlanByPlanID("plan-api-smoke-plan")
	if err != nil {
		return fmt.Errorf("lookup submitted plan: %w", err)
	}
	passes, err := st.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return fmt.Errorf("list plan passes: %w", err)
	}
	if len(passes) != 2 {
		return fmt.Errorf("expected 2 stored passes, got %d", len(passes))
	}
	terminalStatuses := []string{"completed", "skipped"}
	for i, p := range passes {
		if _, err := st.UpdatePlanPassStatus(p.ID, terminalStatuses[i%len(terminalStatuses)]); err != nil {
			return fmt.Errorf("update pass %d status: %w", p.ID, err)
		}
	}

	rec = h.get("/api/plans/plan-api-smoke-plan")
	h.check("completion status 200", rec.Code == http.StatusOK)

	var completionResp plansapi.PlanReadAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &completionResp); err != nil {
		return fmt.Errorf("decode completion response: %w", err)
	}
	h.check("completion success true", completionResp.Success)
	h.check("completionReady true", completionResp.CompletionReady)
	h.check("completion plan status still active", completionResp.Plan != nil && completionResp.Plan.Status == "active")

	reloaded, err := st.GetPlanByPlanID("plan-api-smoke-plan")
	if err != nil {
		return fmt.Errorf("reload plan: %w", err)
	}
	h.check("stored plan status remains active", reloaded.Status == "active")

	// -------------------------------------------------------
	// Summary
	// -------------------------------------------------------
	fmt.Printf("\n=== Plan API Smoke Results ===\n")
	fmt.Printf("PASS: %d\n", h.pass)
	fmt.Printf("FAIL: %d\n", h.fail)

	if h.fail > 0 {
		return fmt.Errorf("%d check(s) failed", h.fail)
	}
	fmt.Println("ALL CHECKS PASSED")
	return nil
}

func (h *harness) post(path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *harness) get(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *harness) check(name string, ok bool) {
	if ok {
		h.pass++
		fmt.Printf("  ✓ %s\n", name)
	} else {
		h.fail++
		fmt.Printf("  ✗ FAIL: %s\n", name)
	}
}

func countRows(db *sql.DB, table string) int {
	var count int
	query := "SELECT COUNT(*) FROM " + table
	if err := db.QueryRow(query).Scan(&count); err != nil {
		return -1
	}
	return count
}
