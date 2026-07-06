package runs

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

type fakeWorkflowReadService struct {
	runs          []workflowapp.RunSummary
	detail        workflowapp.RunDetail
	specification workflowapp.SpecificationReview
	err           error
}

func (f *fakeWorkflowReadService) ListRuns(context.Context, workflowapp.ListRunsInput) ([]workflowapp.RunSummary, error) {
	return f.runs, f.err
}
func (f *fakeWorkflowReadService) GetRun(context.Context, string) (workflowapp.RunDetail, error) {
	return f.detail, f.err
}
func (f *fakeWorkflowReadService) GetSpecification(context.Context, string) (workflowapp.SpecificationReview, error) {
	return f.specification, f.err
}

func workflowReadRouter(service WorkflowReadService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowReadRoutes(router, NewWorkflowReadHandler(service))
	return router
}

func TestWorkflowRunRoutesExposeThreeStageStateAndExplicitArtifacts(t *testing.T) {
	run := workflowstore.Run{
		RunID: "run-test", FeatureSlug: "feature", RepoTarget: "relay",
		Status: workflowstore.RunStatusSetupReady, Branch: "main",
		BaseCommit: strings.Repeat("a", 40), CanonicalSHA256: strings.Repeat("b", 64),
	}
	summary := workflowapp.RunSummary{Run: run, Stage: workflowapp.RunStageSpecification}
	service := &fakeWorkflowReadService{
		runs:   []workflowapp.RunSummary{summary},
		detail: workflowapp.RunDetail{Summary: summary, Attempts: []workflowapp.ExecutionAttemptSummary{}, Artifacts: []workflowapp.ArtifactMetadata{}},
		specification: workflowapp.SpecificationReview{
			Run:           summary,
			ExecutionSpec: workflowapp.ArtifactMetadata{ArtifactID: "artifact-spec", Kind: "execution_spec"},
			ExecutorBrief: workflowapp.ArtifactMetadata{ArtifactID: "artifact-brief", Kind: "executor_brief"},
		},
	}
	response := httptest.NewRecorder()
	workflowReadRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs", nil))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"stage":"specification"`) ||
		strings.Contains(response.Body.String(), "intake") ||
		strings.Contains(response.Body.String(), "prepare") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	workflowReadRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs/run-test/specification", nil))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"/api/artifacts/artifact-spec/content"`) ||
		!strings.Contains(response.Body.String(), `"/api/artifacts/artifact-brief/content"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowRunMissingRecordIsNotFound(t *testing.T) {
	service := &fakeWorkflowReadService{err: sql.ErrNoRows}
	response := httptest.NewRecorder()
	workflowReadRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
