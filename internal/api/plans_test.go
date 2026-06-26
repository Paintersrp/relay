package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	plansapi "relay/internal/api/plans"
	"relay/internal/api/shared"
	appplans "relay/internal/app/plans"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func TestValidatePlanReturnsReportWithoutPersisting(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)

	body := marshalPlanAPIRequest(t, validPlanAPIPayload(t), "")
	req := httptest.NewRequest(http.MethodPost, "/api/plans/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got false with issues %+v", resp.Validation.Issues)
	}
	if !resp.Validation.Valid {
		t.Fatalf("expected validation valid=true, got false")
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestValidatePlanReturnsIssuesForInvalidDependency(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)
	plan := validPlanAPIPayload(t)
	plan.Passes[1].Dependencies = []string{"PASS-999"}

	body := marshalPlanAPIRequest(t, plan, "")
	req := httptest.NewRequest(http.MethodPost, "/api/plans/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected success=false for invalid dependency")
	}
	assertPlanIssueCode(t, resp.Validation, appplans.IssuePlanDependencyUnknown)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanStoresValidPlan(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)

	body := marshalPlanAPIRequest(t, validPlanAPIPayload(t), "handoffs/plans/2026-06-21_managed-plans.planner-pass-plan.json")
	req := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got false with issues %+v", resp.Validation.Issues)
	}
	if resp.Plan == nil {
		t.Fatal("expected plan in response")
	}
	if resp.Plan.PlanID != "plan-123" {
		t.Fatalf("expected planId plan-123, got %q", resp.Plan.PlanID)
	}
	if resp.Plan.SourceArtifactPath != "handoffs/plans/2026-06-21_managed-plans.planner-pass-plan.json" {
		t.Fatalf("unexpected sourceArtifactPath %q", resp.Plan.SourceArtifactPath)
	}
	if len(resp.Passes) != 2 {
		t.Fatalf("expected 2 passes in response, got %d", len(resp.Passes))
	}
	if resp.Passes[1].Dependencies[0] != "PASS-001" {
		t.Fatalf("expected PASS-002 to depend on PASS-001, got %+v", resp.Passes[1].Dependencies)
	}
	if got := countRows(t, st.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 2 {
		t.Fatalf("expected 2 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanDuplicatePlanIDReturnsConflict(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)
	body := marshalPlanAPIRequest(t, validPlanAPIPayload(t), "")

	firstReq := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("expected first submit to return 201, got %d: %s", firstRec.Code, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", secondRec.Code, secondRec.Body.String())
	}

	var resp plansapi.PlanAPIResponse
	if err := json.NewDecoder(secondRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected success=false on duplicate plan_id")
	}
	assertPlanIssueCode(t, resp.Validation, appplans.IssuePlanDuplicatePlanID)
	if got := countRows(t, st.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row after duplicate submit, got %d", got)
	}
}

func TestSubmitPlanInvalidPassStatusReturnsUnprocessableEntity(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)
	plan := validPlanAPIPayload(t)
	plan.Passes[1].Status = "completed"

	body := marshalPlanAPIRequest(t, plan, "")
	req := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected success=false for invalid pass status")
	}
	assertPlanIssueCode(t, resp.Validation, appplans.IssuePlanPassStatusInvalid)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestPlanEndpointsRequirePlan(t *testing.T) {
	t.Parallel()

	_, _, router := newPlanAPITestServer(t)

	tests := []struct {
		name string
		path string
	}{
		{name: "validate", path: "/api/plans/validate"},
		{name: "submit", path: "/api/plans"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}

			var resp shared.ErrorShape
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if resp.Error != "BAD_REQUEST" {
				t.Fatalf("expected BAD_REQUEST, got %q", resp.Error)
			}
		})
	}
}

func newPlanAPITestServer(t *testing.T) (*plansapi.Handler, *store.Store, http.Handler) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "relay.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})

	// Create default test project "relay"
	if _, err := st.CreateProject("relay", "Relay", "Default Test Project", "active", ""); err != nil {
		t.Fatalf("st.CreateProject: %v", err)
	}

	planSvc := appplans.NewService(st)
	planWorkSvc := appplans.NewOrchestratorWorkService(st)
	planH := plansapi.NewHandler(planSvc, planWorkSvc, nil)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		plansapi.MountRoutes(r, planH)
	})

	return planH, st, router
}

func validPlanAPIPayload(t *testing.T) appplans.PlannerPassPlan {
	t.Helper()

	return appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        "plan-123",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-21T00:00:00Z",
			Title:         "Managed plan",
			Goal:          "Implement managed plan flow",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "relay-plan-api-tests",
				Mode:                 "submission_only",
				ContextBrokerEnabled: planAPIBoolPtr(false),
			},
		},
		SourceIntent: appplans.SourceIntent{
			Summary: "Implement managed plan support across phases.",
		},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:   "Relay managed plan records.",
			PlannerContextBoundary: "Plan API tests validate backend behavior only.",
			ForbiddenContextDomains: []string{
				"GitHub issues",
			},
		},
		Passes: []appplans.PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "Example",
				Goal:                   "Example pass.",
				IntendedExecutionScope: []string{"Example scope."},
				NonGoals:               []string{"Example non-goal."},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: appplans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []appplans.ContextSearchTerm{
						{RepoID: "relay", Query: "plans validate", Purpose: "Locate validation flow.", Required: planAPIBoolPtr(true)},
					},
					SeedFilesToRead: []appplans.ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "Validate plans.", Required: planAPIBoolPtr(true)},
					},
					ContextCoverageExpectations: []string{"Validation remains fail-closed for Plan v2."},
					BlockedIfMissing:            []string{"Validation code cannot be located."},
				},
				SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
					RequireGitStatus:   planAPIBoolPtr(true),
					RequireCommitSHA:   planAPIBoolPtr(false),
					AllowDirtyWorktree: planAPIBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Validation and persistence requirements are captured."},
			},
			{
				PassID:                 "PASS-002",
				Sequence:               2,
				Name:                   "Follow-up",
				Goal:                   "Follow-up pass.",
				IntendedExecutionScope: []string{"Another scope."},
				NonGoals:               []string{"No UI changes."},
				Dependencies:           []string{"PASS-001"},
				Status:                 "planned",
				PassType:               "schema_contract",
				ContextPlan: appplans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []appplans.ContextSearchTerm{
						{RepoID: "relay", Query: "CreatePlanPass", Purpose: "Locate persistence flow.", Required: planAPIBoolPtr(true)},
					},
					SeedFilesToRead: []appplans.ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/service.go", Purpose: "Persist plan fields.", Required: planAPIBoolPtr(true)},
					},
					ContextCoverageExpectations: []string{"Pass metadata is stored transactionally."},
					BlockedIfMissing:            []string{"Service persistence code cannot be located."},
				},
				SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
					RequireGitStatus:   planAPIBoolPtr(true),
					RequireCommitSHA:   planAPIBoolPtr(false),
					AllowDirtyWorktree: planAPIBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Stored pass rows preserve later workflow context."},
			},
		},
	}
}

func marshalPlanAPIRequest(t *testing.T, plan appplans.PlannerPassPlan, sourceArtifactPath string) []byte {
	t.Helper()

	req := map[string]any{
		"plan": plan,
	}
	if sourceArtifactPath != "" {
		req["sourceArtifactPath"] = sourceArtifactPath
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal request: %v", err)
	}
	return body
}

func assertPlanIssueCode(t *testing.T, report appplans.PlanValidationReport, code string) {
	t.Helper()

	if report.Valid {
		t.Fatalf("expected invalid report for issue %s", code)
	}
	for _, issue := range report.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected issue code %s, got %+v", code, report.Issues)
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int
	query := "SELECT COUNT(*) FROM " + table
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count rows for %s: %v", table, err)
	}
	return count
}

func planAPIBoolPtr(value bool) *bool {
	return &value
}

func submitValidPlan(t *testing.T, router http.Handler, sourceArtifactPath string) string {
	t.Helper()

	body := marshalPlanAPIRequest(t, validPlanAPIPayload(t), sourceArtifactPath)
	req := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected submit to return 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if resp.Plan == nil {
		t.Fatal("expected plan in submit response")
	}
	return resp.Plan.PlanID
}

func TestListPlansEmpty(t *testing.T) {
	t.Parallel()

	_, _, router := newPlanAPITestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanReadAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got false")
	}
	if len(resp.Plans) != 0 {
		t.Fatalf("expected 0 plans, got %d", len(resp.Plans))
	}
}

func TestListPlansAfterSubmit(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)

	planID := submitValidPlan(t, router, "handoffs/plans/2026-06-21_managed-plans.planner-pass-plan.json")

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanReadAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got false")
	}
	if resp.Count != 1 {
		t.Fatalf("expected count=1, got %d", resp.Count)
	}
	if len(resp.Plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(resp.Plans))
	}
	plan := resp.Plans[0]
	if plan.PlanID != planID {
		t.Fatalf("expected planId %q, got %q", planID, plan.PlanID)
	}
	if plan.Status != "active" {
		t.Fatalf("expected status active, got %q", plan.Status)
	}
	if plan.PassCount != 2 {
		t.Fatalf("expected passCount 2, got %d", plan.PassCount)
	}
	if plan.CompletionReady {
		t.Fatal("expected completionReady=false for non-terminal passes")
	}
	if plan.SourceArtifactPath != "handoffs/plans/2026-06-21_managed-plans.planner-pass-plan.json" {
		t.Fatalf("unexpected sourceArtifactPath %q", plan.SourceArtifactPath)
	}
	if got := countRows(t, st.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row, got %d", got)
	}
}

func TestListPlansStatusFilterAndLimitValidation(t *testing.T) {
	t.Parallel()

	_, _, router := newPlanAPITestServer(t)

	submitValidPlan(t, router, "")

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantCount  int
	}{
		{name: "active status", path: "/api/plans?status=active", wantStatus: http.StatusOK, wantCount: 1},
		{name: "invalid status", path: "/api/plans?status=invalid", wantStatus: http.StatusBadRequest},
		{name: "zero limit", path: "/api/plans?limit=0", wantStatus: http.StatusBadRequest},
		{name: "non-numeric limit", path: "/api/plans?limit=abc", wantStatus: http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				var errResp shared.ErrorShape
				if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if errResp.Error != "BAD_REQUEST" {
					t.Fatalf("expected BAD_REQUEST, got %q", errResp.Error)
				}
				return
			}
			var resp plansapi.PlanReadAPIResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(resp.Plans) != tc.wantCount {
				t.Fatalf("expected %d plans, got %d", tc.wantCount, len(resp.Plans))
			}
		})
	}
}

func TestGetPlanDetailAndPassOrdering(t *testing.T) {
	t.Parallel()

	_, _, router := newPlanAPITestServer(t)

	submitValidPlan(t, router, "")

	req := httptest.NewRequest(http.MethodGet, "/api/plans/plan-123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanReadAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got false")
	}
	if resp.Plan == nil {
		t.Fatal("expected plan in response")
	}
	if resp.Plan.PlanID != "plan-123" {
		t.Fatalf("expected planId plan-123, got %q", resp.Plan.PlanID)
	}
	if resp.Plan.PassCount != 2 {
		t.Fatalf("expected passCount 2, got %d", resp.Plan.PassCount)
	}
	if len(resp.Passes) != 2 {
		t.Fatalf("expected 2 passes, got %d", len(resp.Passes))
	}
	if resp.Passes[0].PassID != "PASS-001" || resp.Passes[1].PassID != "PASS-002" {
		t.Fatalf("unexpected pass order: %+v", resp.Passes)
	}
	if resp.Passes[0].PassType != "backend_vertical_slice" {
		t.Fatalf("expected passType to be returned, got %q", resp.Passes[0].PassType)
	}
	if len(resp.Passes[0].ContextPlan.RequiredRepositories) != 1 ||
		resp.Passes[0].ContextPlan.RequiredRepositories[0] != "relay" {
		t.Fatalf("expected parsed required repositories, got %+v", resp.Passes[0].ContextPlan.RequiredRepositories)
	}
	if got := len(resp.Passes[0].ContextPlan.SeedSearchTerms); got != 1 {
		t.Fatalf("expected parsed seed searches, got %d", got)
	}
	if got := len(resp.Passes[0].HandoffReadinessCriteria); got != 1 {
		t.Fatalf("expected parsed readiness criteria, got %d", got)
	}
	if resp.Plan.CurrentPassID != "" {
		t.Fatalf("expected no current pass, got %q", resp.Plan.CurrentPassID)
	}
	if resp.Plan.NextPassID != "PASS-001" {
		t.Fatalf("expected nextPassId PASS-001, got %q", resp.Plan.NextPassID)
	}
	if resp.CompletionReady {
		t.Fatal("expected completionReady=false")
	}

	// Unknown plan returns 404
	req = httptest.NewRequest(http.MethodGet, "/api/plans/plan-999", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetPlanPassDetail(t *testing.T) {
	t.Parallel()

	_, _, router := newPlanAPITestServer(t)

	submitValidPlan(t, router, "")

	req := httptest.NewRequest(http.MethodGet, "/api/plans/plan-123/passes/PASS-002", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanReadAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got false")
	}
	if resp.Plan == nil {
		t.Fatal("expected parent plan in response")
	}
	if resp.Plan.PlanID != "plan-123" {
		t.Fatalf("expected parent planId plan-123, got %q", resp.Plan.PlanID)
	}
	if resp.Pass == nil {
		t.Fatal("expected pass in response")
	}
	if resp.Pass.PassID != "PASS-002" {
		t.Fatalf("expected passId PASS-002, got %q", resp.Pass.PassID)
	}
	if len(resp.Pass.Dependencies) != 1 || resp.Pass.Dependencies[0] != "PASS-001" {
		t.Fatalf("unexpected pass dependencies: %+v", resp.Pass.Dependencies)
	}
	if resp.Pass.ContextPlan.SeedFilesToRead[0].Path != "internal/plans/service.go" {
		t.Fatalf("expected parsed seed file path, got %+v", resp.Pass.ContextPlan.SeedFilesToRead)
	}
	if resp.Pass.SourceSnapshotRequirements.RequireGitStatus == nil ||
		!*resp.Pass.SourceSnapshotRequirements.RequireGitStatus {
		t.Fatalf("expected source snapshot requirements to be returned, got %+v", resp.Pass.SourceSnapshotRequirements)
	}

	// Unknown pass returns 404
	req = httptest.NewRequest(http.MethodGet, "/api/plans/plan-123/passes/PASS-999", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCompletionReadyComputedWithoutMutatingPlanStatus(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)

	submitValidPlan(t, router, "")

	plan, err := st.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}

	passes, err := st.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		t.Fatalf("list passes: %v", err)
	}
	if len(passes) != 2 {
		t.Fatalf("expected 2 passes, got %d", len(passes))
	}

	// Mark all passes terminal without changing plan.status.
	terminalStatuses := []string{"completed", "skipped"}
	for i, pass := range passes {
		if _, err := st.UpdatePlanPassStatus(pass.ID, terminalStatuses[i%len(terminalStatuses)]); err != nil {
			t.Fatalf("update pass status: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/plans/plan-123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanReadAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.CompletionReady {
		t.Fatal("expected completionReady=true when all passes are terminal")
	}
	if resp.Plan == nil || resp.Plan.Status != "active" {
		t.Fatalf("expected plan status to remain active, got %+v", resp.Plan)
	}

	// Plan status in DB must still be active.
	reloaded, err := st.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("reload plan: %v", err)
	}
	if reloaded.Status != "active" {
		t.Fatalf("expected stored plan status active, got %q", reloaded.Status)
	}
}

func TestGetPlanPassDetailMalformedPersistedJSONReturnsWarning(t *testing.T) {
	t.Parallel()

	_, st, router := newPlanAPITestServer(t)

	submitValidPlan(t, router, "")

	plan, err := st.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("get pass: %v", err)
	}

	if _, err := st.DB().Exec(
		`UPDATE plan_passes
		 SET context_plan_json = '{bad json',
		     handoff_readiness_criteria_json = 'not-json'
		 WHERE id = ?`,
		pass.ID,
	); err != nil {
		t.Fatalf("corrupt persisted plan pass json: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/plans/plan-123/passes/PASS-001", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plansapi.PlanReadAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Pass == nil {
		t.Fatal("expected pass in response")
	}
	if len(resp.Pass.ContextParseWarnings) != 2 {
		t.Fatalf("expected bounded parse warnings, got %+v", resp.Pass.ContextParseWarnings)
	}
	if len(resp.Pass.ContextPlan.RequiredRepositories) != 0 {
		t.Fatalf("expected empty context plan fallback, got %+v", resp.Pass.ContextPlan)
	}
	if len(resp.Pass.HandoffReadinessCriteria) != 0 {
		t.Fatalf("expected empty readiness criteria fallback, got %+v", resp.Pass.HandoffReadinessCriteria)
	}
}
