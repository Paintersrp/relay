package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/store/generated"

	"github.com/go-chi/chi/v5"
)

func newRefactorPromotionAPITestServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(t.TempDir(), "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	apiH := NewAPIHandler(st, logger)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		r.Post("/projects/{projectId}/refactor/candidates", apiH.CreateRefactorCandidate)
		r.Get("/projects/{projectId}/refactor/candidates/{candidateId}/placement-suggestion", apiH.GetRefactorCandidatePlacementSuggestion)
		r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/promote", apiH.PromoteRefactorCandidate)
		r.Post("/projects/{projectId}/refactor/plans/generate", apiH.GenerateRefactorOnlyPlan)
	})
	return st, router
}

func apiSeedActivePlan(t *testing.T, st *store.Store, projectID, planID string) {
	t.Helper()
	project, err := st.GetProjectByProjectID(projectID)
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	if _, err := generated.New(st.DB()).CreatePlan(context.Background(), generated.CreatePlanParams{
		PlanID:                   planID,
		SchemaVersion:            "2.0.0",
		Title:                    "Plan",
		Goal:                     "Goal",
		RepoTarget:               "Paintersrp/relay",
		BranchContext:            "main",
		Status:                   "active",
		SourceIntentSummary:      "s",
		PlanMetaJson:             "{}",
		ProjectContextJson:       "{}",
		McpCapabilityProfileJson: "{}",
		GlobalContextRulesJson:   "{}",
		RawPlanJson:              "{}",
		ProjectRowID:             project.ID,
		ProjectID:                project.ProjectID,
	}); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
}

func TestPromoteRefactorCandidateAPISuccess(t *testing.T) {
	st, router := newRefactorPromotionAPITestServer(t)
	seedRefactorProject(t, st, "relay")
	apiSeedActivePlan(t, st, "relay", "plan-1")

	if rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates", validCandidateBody()); rec.Code != http.StatusCreated {
		t.Fatalf("create candidate failed: %d %s", rec.Code, rec.Body.String())
	}

	rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates/cand-1/promote", []byte(`{"plan_id":"plan-1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("promote failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp RefactorPromoteAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode promote response: %v", err)
	}
	if !resp.Success || resp.PassID != "PASS-001" || resp.CandidateStatus != "scheduled" {
		t.Fatalf("unexpected promote response: %+v", resp)
	}
}

func TestPromoteRefactorCandidateAPIValidationError(t *testing.T) {
	st, router := newRefactorPromotionAPITestServer(t)
	seedRefactorProject(t, st, "relay")

	// Missing plan_id -> 400 VALIDATION_ERROR.
	rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates/cand-1/promote", []byte(`{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp RelayApiErrorShape
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %+v", errResp)
	}
}

func TestPromoteRefactorCandidateAPIBadJSON(t *testing.T) {
	st, router := newRefactorPromotionAPITestServer(t)
	seedRefactorProject(t, st, "relay")
	rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates/cand-1/promote", []byte(`{not json`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestGenerateRefactorOnlyPlanAPISuccess(t *testing.T) {
	st, router := newRefactorPromotionAPITestServer(t)
	seedRefactorProject(t, st, "relay")

	orig := artifacts.BaseDir
	t.Cleanup(func() { artifacts.SetBaseDir(orig) })
	artifacts.SetBaseDir(t.TempDir())

	if rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates", validCandidateBody()); rec.Code != http.StatusCreated {
		t.Fatalf("create candidate failed: %d %s", rec.Code, rec.Body.String())
	}

	rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/plans/generate", []byte(`{"candidate_ids":["cand-1"],"title":"Review"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("generate failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp RefactorGeneratePlanAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}
	if !resp.Success || resp.SubmissionPolicy != "review_required_no_auto_submit" || resp.JSONArtifactPath == "" {
		t.Fatalf("unexpected generate response: %+v", resp)
	}
}

func TestPlacementSuggestionAPI(t *testing.T) {
	st, router := newRefactorPromotionAPITestServer(t)
	seedRefactorProject(t, st, "relay")
	apiSeedActivePlan(t, st, "relay", "plan-1")
	if rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates", validCandidateBody()); rec.Code != http.StatusCreated {
		t.Fatalf("create candidate failed: %d %s", rec.Code, rec.Body.String())
	}

	rec := doRequest(t, router, http.MethodGet, "/api/projects/relay/refactor/candidates/cand-1/placement-suggestion?plan_id=plan-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("placement suggestion failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp RefactorPlacementSuggestionAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode placement response: %v", err)
	}
	if !resp.Success || resp.Suggestion == nil {
		t.Fatalf("unexpected placement response: %+v", resp)
	}
	// Empty plan -> no suggestion.
	if resp.Suggestion.PlacementReason != "no_suggestion" {
		t.Fatalf("expected no_suggestion for empty plan, got %q", resp.Suggestion.PlacementReason)
	}
}
