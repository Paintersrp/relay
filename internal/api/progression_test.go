package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/plans"
	"relay/internal/store"
	"relay/internal/validationrunner"

	"github.com/go-chi/chi/v5"
)

// newProgressionTestServer builds a store and router exposing the run-stage
// routes needed to exercise pass-associated run progression.
func newProgressionTestServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.CreateProject("relay", "Relay", "Default Test Project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := st.CreateRepo("relay", filepath.Join(dir, "repo")); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	artifacts.SetBaseDir(dir)

	apiH := NewAPIHandler(st, logger)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		r.Post("/intake/planner-handoff", apiH.IntakePlannerHandoff)
		r.Post("/runs/{id}/approve-intake", apiH.ApproveIntake)
		r.Post("/runs/{id}/validate/accept-failure", apiH.AcceptFailedValidation)
		r.Post("/runs/{id}/audit/submit", apiH.SubmitAuditPacket)
		r.Post("/runs/{id}/audit/approve", apiH.ApproveAudit)
		r.Post("/runs/{id}/audit/request-revision", apiH.RequestAuditRevision)
		r.Post("/runs/{id}/audit/close", apiH.CloseRun)
	})

	return st, router
}

// seedProgressionPlan submits a minimal single-pass managed plan and returns the
// created plan and PASS-001 row.
func seedProgressionPlan(t *testing.T, st *store.Store, planID string) (*store.Plan, *store.PlanPass) {
	t.Helper()

	plan := plans.PlannerPassPlan{
		PlanMeta: plans.PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Progression test plan",
			Goal:          "Exercise pass-associated run progression.",
			RepoTarget:    "relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &plans.MCPCapabilityProfile{
				ProfileID:            "test",
				Mode:                 "submission_only",
				ContextBrokerEnabled: progressionBoolPtr(false),
			},
		},
		SourceIntent: plans.SourceIntent{Summary: "Progression API test."},
		GlobalContextRules: &plans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []plans.PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "First pass",
				Goal:                   "First pass goal",
				IntendedExecutionScope: []string{"internal/plans"},
				NonGoals:               []string{"No UI"},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: plans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []plans.ContextSearchTerm{
						{RepoID: "relay", Query: "plans validate", Purpose: "optional", Required: progressionBoolPtr(false)},
					},
					SeedFilesToRead: []plans.ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "optional", Required: progressionBoolPtr(false)},
					},
					ContextCoverageExpectations: []string{"coverage ok"},
					BlockedIfMissing:            []string{"not blocked"},
				},
				SourceSnapshotRequirements: plans.SourceSnapshotRequirements{
					RequireGitStatus:   progressionBoolPtr(false),
					RequireCommitSHA:   progressionBoolPtr(false),
					AllowDirtyWorktree: progressionBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Pass 1 complete"},
			},
		},
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	result, err := plans.NewService(st).SubmitPlan(context.Background(), plans.SubmitPlanRequest{
		RawJSON:            raw,
		SourceArtifactPath: "handoffs/planner/progression-test.json",
		ProjectID:          "relay",
	})
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("expected valid plan, got issues: %+v", result.Report.Issues)
	}

	createdPlan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(createdPlan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("get pass: %v", err)
	}
	return createdPlan, pass
}

func passStatus(t *testing.T, st *store.Store, passID int64) string {
	t.Helper()
	pass, err := st.GetPlanPass(passID)
	if err != nil {
		t.Fatalf("get pass: %v", err)
	}
	return pass.Status
}

func TestRunProgression_IntakeCreatesRunCreatedThenInProgress(t *testing.T) {
	st, router := newProgressionTestServer(t)
	plan, pass := seedProgressionPlan(t, st, "progression-intake")

	// 1. Intake a planner handoff associated with the managed pass.
	intakeBody, _ := json.Marshal(map[string]string{
		"planner_handoff_markdown": "# Progression Intake\n\nManaged pass run.\n",
		"repo":                     "relay",
		"branch":                   "main",
		"plan_id":                  plan.PlanID,
		"pass_id":                  "PASS-001",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/intake/planner-handoff", bytes.NewReader(intakeBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("intake: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var intakeResp PlannerHandoffIntakeResponse
	if err := json.NewDecoder(rec.Body).Decode(&intakeResp); err != nil {
		t.Fatalf("decode intake response: %v", err)
	}
	if got := passStatus(t, st, pass.ID); got != "run_created" {
		t.Fatalf("after intake expected pass run_created, got %q", got)
	}

	// 2. Approve intake -> pass in_progress.
	approveBody := `{"action":"approve"}`
	req = httptest.NewRequest(http.MethodPost, "/api/runs/"+intakeResp.RunID+"/approve-intake", bytes.NewReader([]byte(approveBody)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve-intake: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := passStatus(t, st, pass.ID); got != "in_progress" {
		t.Fatalf("after approve expected pass in_progress, got %q", got)
	}
}

func TestRunProgression_IntakeBlockedMarksPassBlocked(t *testing.T) {
	st, router := newProgressionTestServer(t)
	plan, pass := seedProgressionPlan(t, st, "progression-blocked-intake")

	run := seedAssociatedRun(t, st, plan, pass, "intake_received")
	if _, err := st.UpdatePlanPassStatus(pass.ID, "run_created"); err != nil {
		t.Fatalf("seed pass: %v", err)
	}

	body := `{"action":"blocked","notes":"missing context"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%d/approve-intake", run.ID), bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve-intake blocked: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := passStatus(t, st, pass.ID); got != "blocked" {
		t.Fatalf("expected pass blocked, got %q", got)
	}
}

func TestRunProgression_AcceptFailedValidationReturnsInProgress(t *testing.T) {
	st, router := newProgressionTestServer(t)
	plan, pass := seedProgressionPlan(t, st, "progression-accept-failure")

	run := seedAssociatedRun(t, st, plan, pass, "validation_failed")
	if _, err := st.UpdatePlanPassStatus(pass.ID, "blocked"); err != nil {
		t.Fatalf("seed pass: %v", err)
	}
	// Seed final validation evidence so acceptance is permitted.
	if _, err := st.CreateArtifact(run.ID, validationrunner.ArtifactKindJSON, filepath.Join(artifacts.Dir(run.ID), "validation_run.json"), "application/json"); err != nil {
		t.Fatalf("seed validation artifact: %v", err)
	}

	body := `{"reason":"flaky external dependency, accepted for now"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%d/validate/accept-failure", run.ID), bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept-failure: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := passStatus(t, st, pass.ID); got != "in_progress" {
		t.Fatalf("expected pass in_progress after acceptance, got %q", got)
	}
}

func TestRunProgression_AuditApproveCompletesPass(t *testing.T) {
	st, router := newProgressionTestServer(t)
	plan, pass := seedProgressionPlan(t, st, "progression-audit-accept")

	run := seedAssociatedRun(t, st, plan, pass, "audit_ready")
	if _, err := st.UpdatePlanPassStatus(pass.ID, "audit_ready"); err != nil {
		t.Fatalf("seed pass: %v", err)
	}

	body := `{"decision":"accepted","notes":"looks good"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%d/audit/approve", run.ID), bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit approve: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := passStatus(t, st, pass.ID); got != "completed" {
		t.Fatalf("expected pass completed, got %q", got)
	}
}

func TestRunProgression_AuditRequestRevisionMarksRevisionRequired(t *testing.T) {
	st, router := newProgressionTestServer(t)
	plan, pass := seedProgressionPlan(t, st, "progression-audit-revision")

	run := seedAssociatedRun(t, st, plan, pass, "audit_ready")
	if _, err := st.UpdatePlanPassStatus(pass.ID, "audit_ready"); err != nil {
		t.Fatalf("seed pass: %v", err)
	}

	body := `{"decision":"revision_required","notes":"please address review"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%d/audit/request-revision", run.ID), bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit request-revision: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := passStatus(t, st, pass.ID); got != "revision_required" {
		t.Fatalf("expected pass revision_required, got %q", got)
	}
}

func TestRunProgression_AuditSubmitBlockedMarksPassBlocked(t *testing.T) {
	for _, decision := range []string{"blocked", "manual_review_required"} {
		t.Run(decision, func(t *testing.T) {
			st, router := newProgressionTestServer(t)
			plan, pass := seedProgressionPlan(t, st, "progression-audit-"+decision)

			run := seedAssociatedRun(t, st, plan, pass, "audit_ready")
			if _, err := st.UpdatePlanPassStatus(pass.ID, "audit_ready"); err != nil {
				t.Fatalf("seed pass: %v", err)
			}

			body := fmt.Sprintf(`{"decision":%q,"audit_packet_markdown":"# Audit\n\nBlocked.","notes":"blocker"}`, decision)
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%d/audit/submit", run.ID), bytes.NewReader([]byte(body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("audit submit %s: expected 200, got %d: %s", decision, rec.Code, rec.Body.String())
			}

			// Existing run-status mapping is preserved (blocked/manual_review -> revision_required).
			var resp map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode submit response: %v", err)
			}
			if resp["status"] != "revision_required" {
				t.Fatalf("expected run status revision_required, got %v", resp["status"])
			}
			// But the associated pass must be blocked.
			if got := passStatus(t, st, pass.ID); got != "blocked" {
				t.Fatalf("expected pass blocked for decision %s, got %q", decision, got)
			}
		})
	}
}

func seedAssociatedRun(t *testing.T, st *store.Store, plan *store.Plan, pass *store.PlanPass, status string) *store.Run {
	t.Helper()
	run, err := st.CreateRunWithAssociation(
		1,
		"Progression Run",
		status,
		"gpt-4o",
		"gpt-4o",
		store.DefaultExecutorAdapter,
		"main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("create associated run: %v", err)
	}
	return run
}

func progressionBoolPtr(value bool) *bool {
	return &value
}
