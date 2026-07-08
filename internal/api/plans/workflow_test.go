package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workflowapp "relay/internal/app/workflow"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWorkflowPlanService struct {
	summaries []workflowapp.PlanSummary
	detail    workflowapp.PlanDetail
	pass      workflowapp.PlanPassDetail
	err       error
}

func (f *fakeWorkflowPlanService) ListPlans(context.Context, workflowapp.ListPlansInput) ([]workflowapp.PlanSummary, error) {
	return f.summaries, f.err
}

func (f *fakeWorkflowPlanService) GetPlan(context.Context, string) (workflowapp.PlanDetail, error) {
	return f.detail, f.err
}

func (f *fakeWorkflowPlanService) GetPlanPass(context.Context, string, string) (workflowapp.PlanPassDetail, error) {
	return f.pass, f.err
}

func workflowPlanRouter(service WorkflowPlanService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service))
	return router
}

func TestWorkflowPlanRoutesReturnCanonicalIdentitiesAndConcreteCollections(t *testing.T) {
	plan := workflowstore.Plan{
		PlanID:          "plan-test",
		FeatureSlug:     "feature",
		Status:          workflowstore.PlanStatusActive,
		CanonicalSHA256: strings.Repeat("a", 64),
		CreatedAt:       "2026-07-08T00:00:00Z",
		UpdatedAt:       "2026-07-08T00:00:00Z",
	}
	project := workflowapp.ProjectReference{
		ProjectID: "project-test",
		Name:      "Relay",
		Status:    workflowstore.ProjectStatusActive,
	}
	pass := workflowstore.PlanPass{
		PassID:     "pass-test",
		PassNumber: 1,
		Name:       "Pass",
		RepoTarget: "relay",
		Status:     workflowstore.PassStatusPlanned,
		CreatedAt:  "2026-07-08T00:00:00Z",
		UpdatedAt:  "2026-07-08T00:00:00Z",
	}
	passDetail := workflowapp.PlanPassDetail{
		Pass: pass,
		// A pass with no dependencies is a normal domain value. The transport
		// must encode it as [] rather than null.
		DependsOn: nil,
		Runs:      []workflowapp.RunSummary{},
	}
	service := &fakeWorkflowPlanService{
		summaries: []workflowapp.PlanSummary{{
			Plan:               plan,
			Project:            project,
			PassCount:          1,
			PlannedPassCount:   1,
			CurrentPassID:      pass.PassID,
		}},
		detail: workflowapp.PlanDetail{
			Plan:         plan,
			Project:      project,
			Repositories: []workflowstore.PlanRepositoryTarget{},
			Passes:       []workflowapp.PlanPassDetail{passDetail},
			Artifacts: []workflowapp.ArtifactMetadata{{
				ArtifactID: "artifact-plan",
				OwnerType:  workflowstore.ArtifactOwnerPlan,
				Kind:       "canonical_plan",
				MediaType:  "application/json",
				SHA256:     strings.Repeat("b", 64),
				CreatedAt:  "2026-07-08T00:00:00Z",
			}},
		},
		pass: passDetail,
	}

	response := httptest.NewRecorder()
	workflowPlanRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/plans", nil),
	)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"planId":"plan-test"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	workflowPlanRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/plans/plan-test", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	var detailResponse struct {
		Plan      workflowPlanSummaryResponse  `json:"plan"`
		Passes    []workflowPassResponse       `json:"passes"`
		Artifacts []workflowArtifactResponse   `json:"artifacts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &detailResponse); err != nil {
		t.Fatal(err)
	}
	if detailResponse.Plan.Project.ProjectID != "project-test" ||
		len(detailResponse.Passes) != 1 ||
		detailResponse.Passes[0].DependsOn == nil ||
		len(detailResponse.Passes[0].DependsOn) != 0 ||
		len(detailResponse.Artifacts) != 1 ||
		detailResponse.Artifacts[0].ContentURL != "/api/artifacts/artifact-plan/content" {
		t.Fatalf("detail response = %+v", detailResponse)
	}
	if strings.Contains(response.Body.String(), `"dependsOn":null`) {
		t.Fatalf("detail response contains null dependsOn: %s", response.Body.String())
	}

	response = httptest.NewRecorder()
	workflowPlanRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/plans/plan-test/passes/pass-test", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	var passResponse workflowPassResponse
	if err := json.Unmarshal(response.Body.Bytes(), &passResponse); err != nil {
		t.Fatal(err)
	}
	if passResponse.DependsOn == nil || len(passResponse.DependsOn) != 0 {
		t.Fatalf("pass response dependsOn = %#v", passResponse.DependsOn)
	}
	if strings.Contains(response.Body.String(), `"dependsOn":null`) {
		t.Fatalf("pass response contains null dependsOn: %s", response.Body.String())
	}
}

func TestWorkflowPlanMissingRecordIsNotFound(t *testing.T) {
	service := &fakeWorkflowPlanService{err: sql.ErrNoRows}
	response := httptest.NewRecorder()
	workflowPlanRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/plans/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
