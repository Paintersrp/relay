package plans

import (
	"context"
	"database/sql"
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

func TestWorkflowPlanRoutesReturnNewRecordIdentities(t *testing.T) {
	plan := workflowstore.Plan{PlanID: "plan-test", FeatureSlug: "feature", Status: workflowstore.PlanStatusActive}
	project := workflowapp.ProjectReference{ProjectID: "project-test", Name: "Relay", Status: workflowstore.ProjectStatusActive}
	pass := workflowstore.PlanPass{PassID: "pass-test", PassNumber: 1, Name: "Pass", RepoTarget: "relay", Status: workflowstore.PassStatusPlanned}
	service := &fakeWorkflowPlanService{
		summaries: []workflowapp.PlanSummary{
			workflowapp.PlanSummary{Plan: plan, Project: project, PassCount: 1, PlannedPassCount: 1, CurrentPassID: pass.PassID},
		},
		detail: workflowapp.PlanDetail{
			Plan:      plan,
			Project:   project,
			Passes:    []workflowapp.PlanPassDetail{{Pass: pass, DependsOn: []string{}, Runs: []workflowapp.RunSummary{}}},
			Artifacts: []workflowapp.ArtifactMetadata{{ArtifactID: "artifact-plan", Kind: "canonical_plan"}},
		},
		pass: workflowapp.PlanPassDetail{Pass: pass, DependsOn: []string{}, Runs: []workflowapp.RunSummary{}},
	}
	response := httptest.NewRecorder()
	workflowPlanRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/plans", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"planId":"plan-test"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	workflowPlanRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/plans/plan-test", nil))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"passId":"pass-test"`) ||
		!strings.Contains(response.Body.String(), `"/api/artifacts/artifact-plan/content"`) ||
		!strings.Contains(response.Body.String(), `"projectId":"project-test"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
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
