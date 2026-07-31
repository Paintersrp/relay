package canonical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workflowsubmissions "relay/internal/app/submissions"
	"relay/internal/speccompiler"

	"github.com/go-chi/chi/v5"
)

type fakeCanonicalService struct {
	validation     workflowsubmissions.ValidationResult
	lastValidation workflowsubmissions.ValidationInput
	err            error
}

func (f *fakeCanonicalService) ValidateArtifact(_ context.Context, input workflowsubmissions.ValidationInput) (workflowsubmissions.ValidationResult, error) {
	f.lastValidation = input
	return f.validation, f.err
}

func canonicalRouter(service WorkflowCanonicalService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service))
	return router
}

func TestCanonicalHTTPValidationPreservesExactCanonicalIdentityInputs(t *testing.T) {
	service := &fakeCanonicalService{
		validation: workflowsubmissions.ValidationResult{
			OK:          true,
			Status:      "valid",
			Kind:        "plan",
			SHA256:      strings.Repeat("a", 64),
			Diagnostics: []speccompiler.Diagnostic{},
			Notices:     []speccompiler.Diagnostic{},
		},
	}
	handler := canonicalRouter(service)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/canonical-artifacts/validate",
		strings.NewReader(`{"fileName":" feature.plan.json","canonicalContent":"{}"}`),
	))
	if response.Code != http.StatusOK || service.lastValidation.DisplayName != " feature.plan.json" {
		t.Fatalf("validation response = %d %s; input = %+v", response.Code, response.Body.String(), service.lastValidation)
	}
	if !strings.Contains(response.Body.String(), `"diagnostics":[]`) ||
		!strings.Contains(response.Body.String(), `"notices":[]`) ||
		strings.Contains(response.Body.String(), `"diagnostics":null`) ||
		strings.Contains(response.Body.String(), `"notices":null`) {
		t.Fatalf("validation response collections = %s", response.Body.String())
	}
}

func TestCanonicalHTTPValidationDoesNotAcceptExpectedHashField(t *testing.T) {
	response := httptest.NewRecorder()
	canonicalRouter(&fakeCanonicalService{}).ServeHTTP(response, httptest.NewRequest(
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
		application *workflowsubmissions.ApplicationError
		status      int
		code        string
	}{
		{
			name: "compiler",
			application: &workflowsubmissions.ApplicationError{
				Code:    workflowsubmissions.ErrorCompilerRejected,
				Message: "rejected",
				Diagnostics: []speccompiler.Diagnostic{
					{Code: "invalid_json", Message: "invalid"},
				},
			},
			status: http.StatusUnprocessableEntity,
			code:   "COMPILER_REJECTED",
		},
		{name: "hash", application: &workflowsubmissions.ApplicationError{Code: workflowsubmissions.ErrorInvalidExpectedHash, Message: "invalid"}, status: http.StatusBadRequest, code: "INVALID_EXPECTED_HASH"},
		{name: "association", application: &workflowsubmissions.ApplicationError{Code: workflowsubmissions.ErrorSelectedPassFilename, Message: "invalid"}, status: http.StatusBadRequest, code: "ASSOCIATION_INVALID"},
		{name: "repository", application: &workflowsubmissions.ApplicationError{Code: workflowsubmissions.ErrorRepositoryNotFound, Message: "missing"}, status: http.StatusNotFound, code: "UNKNOWN_REPOSITORY"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeCanonicalService{err: test.application}
			response := httptest.NewRecorder()
			canonicalRouter(service).ServeHTTP(response, httptest.NewRequest(
				http.MethodPost,
				"/canonical-artifacts/validate",
				strings.NewReader(`{"fileName":"feature.plan.json","canonicalContent":"{}"}`),
			))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"`+test.code+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

// Legacy Plan write admission is retired: the canonical router mounts exactly
// one validation route and no Plan-creating or Plan-mutating route.
func TestCanonicalRouterMountsNoPlanWriteRoutes(t *testing.T) {
	handler := canonicalRouter(&fakeCanonicalService{})
	retired := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/plans", `{"projectId":"project-test","fileName":"feature.plan.json","canonicalContent":"{}","expectedSha256":"` + strings.Repeat("a", 64) + `"}`},
		{http.MethodPatch, "/plans/plan-test/project", `{"projectId":"project-destination"}`},
	}
	for _, current := range retired {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(current.method, current.path, strings.NewReader(current.body)))
		if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s is still mounted: %d %s", current.method, current.path, response.Code, response.Body.String())
		}
	}
}
