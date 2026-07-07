package canonical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workflowcanonical "relay/internal/app/canonical"
	workflowplans "relay/internal/app/plans/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeCanonicalService struct {
	validation     workflowcanonical.ValidationResult
	plan           workflowcanonical.SubmitPlanResult
	run            workflowcanonical.CreateRunResult
	lastValidation workflowcanonical.ValidationInput
	lastPlan       workflowcanonical.SubmitPlanInput
	lastRun        workflowcanonical.CreateRunInput
	err            error
}

func (f *fakeCanonicalService) ValidateArtifact(_ context.Context, input workflowcanonical.ValidationInput) (workflowcanonical.ValidationResult, error) {
	f.lastValidation = input
	return f.validation, f.err
}

func (f *fakeCanonicalService) SubmitPlan(_ context.Context, input workflowcanonical.SubmitPlanInput) (workflowcanonical.SubmitPlanResult, error) {
	f.lastPlan = input
	return f.plan, f.err
}

func (f *fakeCanonicalService) CreateRun(_ context.Context, input workflowcanonical.CreateRunInput) (workflowcanonical.CreateRunResult, error) {
	f.lastRun = input
	return f.run, f.err
}

type fakePlanMover struct {
	result workflowplans.MovePlanResult
	err    error
}

func (f *fakePlanMover) MovePlan(context.Context, workflowplans.MovePlanInput) (workflowplans.MovePlanResult, error) {
	return f.result, f.err
}

func canonicalRouter(service WorkflowCanonicalService, mover WorkflowPlanMover) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service, mover))
	return router
}

func TestCanonicalHTTPRoutesPreserveExactCanonicalIdentityInputs(t *testing.T) {
	service := &fakeCanonicalService{
		validation: workflowcanonical.ValidationResult{OK: true, Status: "valid", Kind: "plan", SHA256: strings.Repeat("a", 64)},
		plan: workflowcanonical.SubmitPlanResult{
			Project: workflowstore.Project{ProjectID: "project-test", Name: "Relay", Status: workflowstore.ProjectStatusActive},
			Plan:    workflowstore.Plan{PlanID: "plan-test", FeatureSlug: "feature", Status: workflowstore.PlanStatusActive},
		},
		run: workflowcanonical.CreateRunResult{
			Run: workflowstore.Run{
				RunID: "run-test", FeatureSlug: "feature", RepoTarget: "relay",
				Status: workflowstore.RunStatusSetupReady, Branch: "main",
				BaseCommit: strings.Repeat("a", 40),
			},
		},
	}
	handler := canonicalRouter(service, &fakePlanMover{})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/canonical-artifacts/validate",
		strings.NewReader(`{"fileName":" feature.plan.json","canonicalContent":"{}"}`),
	))
	if response.Code != http.StatusOK || service.lastValidation.DisplayName != " feature.plan.json" {
		t.Fatalf("validation response = %d %s; input = %+v", response.Code, response.Body.String(), service.lastValidation)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/plans",
		strings.NewReader(`{"projectId":"project-test","fileName":"feature.plan.json ","canonicalContent":"{}","expectedSha256":" `+strings.Repeat("a", 64)+`"}`),
	))
	if response.Code != http.StatusCreated {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if service.lastPlan.DisplayName != "feature.plan.json " ||
		service.lastPlan.ExpectedSHA256 != " "+strings.Repeat("a", 64) {
		t.Fatalf("Plan input was normalized: %+v", service.lastPlan)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/runs",
		strings.NewReader(`{"fileName":"feature.pass-1.execution-spec.json","canonicalContent":"{}","expectedSha256":"`+strings.Repeat("b", 64)+`","planId":"plan-test","passNumber":1}`),
	))
	if response.Code != http.StatusCreated ||
		service.lastRun.DisplayName != "feature.pass-1.execution-spec.json" ||
		service.lastRun.PlanID != "plan-test" ||
		service.lastRun.PassNumber != 1 {
		t.Fatalf("Run response = %d %s; input = %+v", response.Code, response.Body.String(), service.lastRun)
	}
}

func TestCanonicalHTTPValidationDoesNotAcceptExpectedHashField(t *testing.T) {
	response := httptest.NewRecorder()
	canonicalRouter(&fakeCanonicalService{}, &fakePlanMover{}).ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/canonical-artifacts/validate",
		strings.NewReader(`{"fileName":"feature.plan.json","canonicalContent":"{}","expectedSha256":"`+strings.Repeat("a", 64)+`"}`),
	))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestCanonicalHTTPApplicationErrorsHaveStableClassifications(t *testing.T) {
	tests := []struct {
		name        string
		application *workflowcanonical.ApplicationError
		status      int
		code        string
	}{
		{
			name: "compiler",
			application: &workflowcanonical.ApplicationError{
				Code:    workflowcanonical.ErrorCompilerRejected,
				Message: "rejected",
				Diagnostics: []speccompiler.Diagnostic{
					speccompiler.Diagnostic{Code: "invalid_json", Message: "invalid"},
				},
			},
			status: http.StatusUnprocessableEntity,
			code:   "COMPILER_REJECTED",
		},
		{name: "hash", application: &workflowcanonical.ApplicationError{Code: workflowcanonical.ErrorInvalidExpectedHash, Message: "invalid"}, status: http.StatusBadRequest, code: "INVALID_EXPECTED_HASH"},
		{name: "association", application: &workflowcanonical.ApplicationError{Code: workflowcanonical.ErrorSelectedPassFilename, Message: "invalid"}, status: http.StatusBadRequest, code: "ASSOCIATION_INVALID"},
		{name: "repository", application: &workflowcanonical.ApplicationError{Code: workflowcanonical.ErrorRepositoryNotFound, Message: "missing"}, status: http.StatusNotFound, code: "UNKNOWN_REPOSITORY"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeCanonicalService{err: test.application}
			response := httptest.NewRecorder()
			canonicalRouter(service, &fakePlanMover{}).ServeHTTP(response, httptest.NewRequest(
				http.MethodPost,
				"/runs",
				strings.NewReader(`{"fileName":"feature.execution-spec.json","canonicalContent":"{}","expectedSha256":"`+strings.Repeat("a", 64)+`"}`),
			))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"`+test.code+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMovePlanUsesCommittedAggregateWithoutStoreRead(t *testing.T) {
	mover := &fakePlanMover{
		result: workflowplans.MovePlanResult{
			Project: workflowstore.Project{ProjectID: "project-destination", Name: "Destination", Status: workflowstore.ProjectStatusActive},
			Plan:    workflowstore.Plan{PlanID: "plan-test", FeatureSlug: "feature", Status: workflowstore.PlanStatusActive},
		},
	}
	response := httptest.NewRecorder()
	canonicalRouter(&fakeCanonicalService{}, mover).ServeHTTP(response, httptest.NewRequest(
		http.MethodPatch,
		"/plans/plan-test/project",
		strings.NewReader(`{"projectId":"project-destination"}`),
	))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"projectId":"project-destination"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
