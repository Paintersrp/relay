package artifacts

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workflowapp "relay/internal/app/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWorkflowArtifactService struct {
	metadata workflowapp.ArtifactMetadata
	content  workflowapp.ArtifactContent
	err      error
}

func (f *fakeWorkflowArtifactService) GetArtifact(context.Context, string) (workflowapp.ArtifactMetadata, error) {
	return f.metadata, f.err
}
func (f *fakeWorkflowArtifactService) GetArtifactContent(context.Context, workflowapp.ArtifactContentInput) (workflowapp.ArtifactContent, error) {
	return f.content, f.err
}

func workflowArtifactRouter(service WorkflowArtifactService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service))
	return router
}

func TestWorkflowArtifactContentIsExplicitAndBounded(t *testing.T) {
	metadata := workflowapp.ArtifactMetadata{
		ArtifactID: "artifact-test", OwnerType: "run", Kind: "executor_brief",
		MediaType: "text/markdown", SHA256: strings.Repeat("a", 64), SizeBytes: 10,
	}
	service := &fakeWorkflowArtifactService{
		metadata: metadata,
		content: workflowapp.ArtifactContent{
			Artifact: metadata, Offset: 0, Bytes: []byte("abcd"), Encoding: "utf-8",
			Truncated: true, NextOffset: 4, HasNext: true,
		},
	}
	response := httptest.NewRecorder()
	workflowArtifactRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/artifacts/artifact-test/content?limit=4", nil),
	)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"content":"abcd"`) ||
		!strings.Contains(response.Body.String(), `"nextOffset":4`) ||
		strings.Contains(response.Body.String(), "relativePath") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowArtifactMissingRecordIsNotFound(t *testing.T) {
	service := &fakeWorkflowArtifactService{err: sql.ErrNoRows}
	response := httptest.NewRecorder()
	workflowArtifactRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/artifacts/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
