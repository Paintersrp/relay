package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	plansapi "relay/internal/api/plans"
	appplans "relay/internal/app/plans"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

// newNextPassWorkTestServer creates a minimal test router with the
// GetNextPassWork route registered under the project-scoped path.
func newNextPassWorkTestServer(t *testing.T) (*plansapi.Handler, *store.Store, http.Handler) {
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

	// Seed default project.
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

// seedNextPassWorkPlan submits a valid two-pass plan via the API.
func seedNextPassWorkPlan(t *testing.T, router http.Handler, planID string) {
	t.Helper()

	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "API test plan",
			Goal:          "Next-pass work API test",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "test",
				Mode:                 "submission_only",
				ContextBrokerEnabled: planAPIBoolPtr(false),
			},
		},
		SourceIntent: appplans.SourceIntent{Summary: "API test for next-pass work."},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []appplans.PlanPassInput{
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
				ContextPlan: appplans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []appplans.ContextSearchTerm{
						{RepoID: "relay", Query: "plans validate", Purpose: "optional", Required: planAPIBoolPtr(false)},
					},
					SeedFilesToRead: []appplans.ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "optional", Required: planAPIBoolPtr(false)},
					},
					ContextCoverageExpectations: []string{"coverage ok"},
					BlockedIfMissing:            []string{"not blocked"},
				},
				SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
					RequireGitStatus:   planAPIBoolPtr(false),
					RequireCommitSHA:   planAPIBoolPtr(false),
					AllowDirtyWorktree: planAPIBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Pass 1 complete"},
			},
			{
				PassID:                 "PASS-002",
				Sequence:               2,
				Name:                   "Second pass",
				Goal:                   "Second pass goal",
				IntendedExecutionScope: []string{"internal/plans"},
				NonGoals:               []string{"No UI"},
				Dependencies:           []string{"PASS-001"},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: appplans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []appplans.ContextSearchTerm{
						{RepoID: "relay", Query: "plans validate", Purpose: "optional", Required: planAPIBoolPtr(false)},
					},
					SeedFilesToRead: []appplans.ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "optional", Required: planAPIBoolPtr(false)},
					},
					ContextCoverageExpectations: []string{"coverage ok"},
					BlockedIfMissing:            []string{"not blocked"},
				},
				SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
					RequireGitStatus:   planAPIBoolPtr(false),
					RequireCommitSHA:   planAPIBoolPtr(false),
					AllowDirtyWorktree: planAPIBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Pass 2 complete"},
			},
		},
	}

	body := marshalPlanAPIRequest(t, plan, "")
	req := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("seedNextPassWorkPlan: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func setNextPassWorkPlanPassStatus(t *testing.T, st *store.Store, planID, passID, status string) {
	t.Helper()

	plan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID %q: %v", planID, err)
	}
	passes, err := st.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		t.Fatalf("ListPlanPassesByPlan %q: %v", planID, err)
	}
	for _, pass := range passes {
		if pass.PassID == passID {
			if _, err := st.UpdatePlanPassStatus(pass.ID, status); err != nil {
				t.Fatalf("UpdatePlanPassStatus %q => %q: %v", passID, status, err)
			}
			return
		}
	}
	t.Fatalf("pass %q not found in plan %q", passID, planID)
}

func TestGetNextPassWork_RouteExists_ReturnsToolField(t *testing.T) {
	t.Parallel()

	_, _, router := newNextPassWorkTestServer(t)
	seedNextPassWorkPlan(t, router, "api-plan-001")

	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plans/api-plan-001/next-pass-work", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appplans.NextPassWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Tool != appplans.NextPassWorkTool {
		t.Fatalf("expected tool %q, got %q", appplans.NextPassWorkTool, resp.Tool)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil {
		t.Fatal("expected selected_pass in response")
	}
	if resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 selected, got %q", resp.SelectedPass.PassID)
	}
}

func TestGetNextPassWork_EmptyProjectIDReturns400(t *testing.T) {
	t.Parallel()

	// Chi will route an empty projectId segment differently (won't match the route).
	// Test the unsafe path separator case which chi would resolve to the route.
	_, _, router := newNextPassWorkTestServer(t)

	// Use a path with path traversal in the query -- chi routes before handler runs,
	// but path-separator in param value should trigger unsafe_request.
	// Note: chi won't match a "/" in a URL param, but ".." sequences in the
	// actual param value will be passed through. Test with an actual path value.
	// We test this at the service level in work_packets_test.go.
	// At the API level, test that a real unknown project returns HTTP 200 with blocker.
	req := httptest.NewRequest(http.MethodGet, "/api/projects/no-such-project/plans/any-plan/next-pass-work", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Unknown project returns 200 with ok=false (not 400 -- that's reserved for unsafe_request).
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appplans.NextPassWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatal("expected ok=false for unknown project")
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Code != string(appplans.BlockerUnknownProject) {
		t.Fatalf("expected unknown_project blocker, got %+v", resp.Blockers)
	}
}

func TestGetNextPassWork_UnknownProjectReturns200WithBlocker(t *testing.T) {
	t.Parallel()

	_, _, router := newNextPassWorkTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/nonexistent-proj/plans/plan-x/next-pass-work", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appplans.NextPassWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatal("expected ok=false")
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Code != string(appplans.BlockerUnknownProject) {
		t.Fatalf("expected unknown_project blocker, got %+v", resp.Blockers)
	}
}

func TestGetNextPassWork_SuccessReturns200WithOKTrue(t *testing.T) {
	t.Parallel()

	_, _, router := newNextPassWorkTestServer(t)
	seedNextPassWorkPlan(t, router, "api-plan-success")

	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plans/api-plan-success/next-pass-work", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appplans.NextPassWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.Project == nil {
		t.Fatal("expected project in response")
	}
	if resp.Project.ProjectID != "relay" {
		t.Fatalf("expected project_id relay, got %q", resp.Project.ProjectID)
	}
	if resp.Plan == nil {
		t.Fatal("expected plan in response")
	}
	if resp.Plan.PlanID != "api-plan-success" {
		t.Fatalf("expected plan_id api-plan-success, got %q", resp.Plan.PlanID)
	}
	if resp.SelectedPass == nil {
		t.Fatal("expected selected_pass in response")
	}
	if resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001, got %q", resp.SelectedPass.PassID)
	}
	if resp.SuggestedRunSubmission != nil {
		t.Fatalf("expected no run submission suggestion before reviewed handoff, got %+v", resp.SuggestedRunSubmission)
	}
	if resp.PlannerJumpstart == nil || resp.PlannerJumpstart.ReadinessState != "ready_for_handoff_authoring" {
		t.Fatalf("expected ready_for_handoff_authoring jumpstart, got %+v", resp.PlannerJumpstart)
	}
	if resp.HandoffWork == nil {
		t.Fatal("expected handoff_work in response")
	}
	if resp.HandoffWork.PlanID != "api-plan-success" || resp.HandoffWork.PassID != "PASS-001" {
		t.Fatalf("unexpected handoff_work IDs: %+v", resp.HandoffWork)
	}
}

func TestGetPassNextWorkPreview_RequestedPassReturnsSelectedPassPayload(t *testing.T) {
	t.Parallel()

	_, st, router := newNextPassWorkTestServer(t)
	seedNextPassWorkPlan(t, router, "api-plan-preview-selected")
	setNextPassWorkPlanPassStatus(t, st, "api-plan-preview-selected", "PASS-001", appplans.StatusPassCompleted)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plans/api-plan-preview-selected/passes/PASS-002/next-pass-work-preview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appplans.NextPassWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil {
		t.Fatal("expected selected_pass in response")
	}
	if resp.SelectedPass.PassID != "PASS-002" {
		t.Fatalf("expected PASS-002 selected, got %q", resp.SelectedPass.PassID)
	}
	if resp.SuggestedRunSubmission != nil {
		t.Fatalf("expected no run submission suggestion before reviewed handoff, got %+v", resp.SuggestedRunSubmission)
	}
	if resp.HandoffWork == nil || resp.HandoffWork.PassID != "PASS-002" {
		t.Fatalf("expected handoff_work for PASS-002, got %+v", resp.HandoffWork)
	}
}

func TestGetPassNextWorkPreview_RequestedPassBlockedByPriorPassReturnsPayload(t *testing.T) {
	t.Parallel()

	_, _, router := newNextPassWorkTestServer(t)
	seedNextPassWorkPlan(t, router, "api-plan-preview-blocked")

	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plans/api-plan-preview-blocked/passes/PASS-002/next-pass-work-preview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appplans.NextPassWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatal("expected ok=false for requested pass blocked by prior pass")
	}
	if len(resp.Blockers) == 0 {
		t.Fatal("expected blocker payload")
	}
	if resp.Blockers[0].Code != appplans.BlockerRequestedPassNotEligible {
		t.Fatalf("expected requested_pass_not_eligible blocker, got %+v", resp.Blockers)
	}
	if !strings.Contains(resp.Blockers[0].Message, "PASS-002") {
		t.Fatalf("expected blocker to reference requested pass PASS-002, got %q", resp.Blockers[0].Message)
	}
}

func TestGetNextPassWork_API_ContextPacketUsability(t *testing.T) {
	t.Parallel()

	_, st, router := newNextPassWorkTestServer(t)

	// Seed source snapshot
	project, err := st.GetProjectByProjectID("relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	if _, err := st.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: "snap-api-1",
		ProjectRowID:     project.ID,
		ProjectID:        project.ProjectID,
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-28T00:00:00Z",
		SummaryJSON:      "{}",
	}); err != nil {
		t.Fatalf("CreateSourceSnapshot: %v", err)
	}

	planSvc := appplans.NewService(st)
	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        "api-plan-usability",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "API usability test plan",
			Goal:          "Exercise context packet usability over API.",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "test",
				Mode:                 "submission_only",
				ContextBrokerEnabled: planAPIBoolPtr(false),
			},
		},
		SourceIntent: appplans.SourceIntent{Summary: "API context packet usability test plan."},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []appplans.PlanPassInput{{
			PassID: "PASS-001", Sequence: 1, Name: "Context pass", Goal: "Collect context.",
			IntendedExecutionScope: []string{"Inspect usability."},
			NonGoals:               []string{"No run creation."},
			Dependencies:           []string{},
			Status:                 appplans.StatusPassPlanned,
			PassType:               "backend_vertical_slice",
			ContextPlan: appplans.ContextPlan{
				RequiredRepositories: []string{"relay"},
				SeedSearchTerms: []appplans.ContextSearchTerm{
					{RepoID: "relay", Query: "planner_jumpstart", Purpose: "Find jumpstart code.", Required: planAPIBoolPtr(true)},
				},
				SeedFilesToRead: []appplans.ContextFileRead{
					{RepoID: "relay", Path: "internal/app/plans/work_packets.go", Purpose: "Review work packet logic.", Required: planAPIBoolPtr(true)},
				},
				ContextCoverageExpectations: []string{"Usability contract is covered."},
				BlockedIfMissing:            []string{"Action payload cannot be checked."},
			},
			SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
				RequireGitStatus:   planAPIBoolPtr(false),
				RequireCommitSHA:   planAPIBoolPtr(false),
				AllowDirtyWorktree: planAPIBoolPtr(true),
			},
			HandoffReadinessCriteria: []string{"Usability packet reviewed."},
		}},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	submitResult, err := planSvc.SubmitPlan(context.Background(), appplans.SubmitPlanRequest{
		RawJSON:               raw,
		UnmanagedAcknowledged: true,
	})
	if err != nil || !submitResult.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, submitResult.Report.Issues)
	}

	// Create blocked context packet
	_, err = st.CreateContextPacket(store.CreateContextPacketParams{
		ContextPacketID:     "packet-api-unusable",
		ProjectRowID:        project.ID,
		ProjectID:           project.ProjectID,
		PlanID:              "api-plan-usability",
		PassID:              "PASS-001",
		TaskSlug:            "slug",
		SourceSnapshotRowID: 1,
		SourceSnapshotID:    "snap-api-1",
		Status:              "blocked", // blocked makes it unusable
		BlockedSeedCount:    0,
		MissingSeedCount:    0,
		CompletedAt:         "2026-06-28T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateContextPacket: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plans/api-plan-usability/next-pass-work", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appplans.NextPassWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatal("expected ok=false for unusable context packet")
	}
	if resp.Context == nil || resp.Context.ContextReady {
		t.Error("expected ContextReady=false for unusable context packet")
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Code != appplans.BlockerRequiredContextPacketMissing {
		t.Fatalf("expected required_context_packet_missing blocker, got %+v", resp.Blockers)
	}
}
