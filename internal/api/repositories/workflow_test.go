package repositories

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWorkflowRepositoryService struct {
	values []workflowstore.RepositoryTarget
	value  workflowstore.RepositoryTarget
	err    error
}

func (f *fakeWorkflowRepositoryService) ListRepositories(context.Context) ([]workflowstore.RepositoryTarget, error) {
	return f.values, f.err
}

func (f *fakeWorkflowRepositoryService) GetRepository(context.Context, string) (workflowstore.RepositoryTarget, error) {
	return f.value, f.err
}

func (f *fakeWorkflowRepositoryService) RegisterRepository(context.Context, string, string) (workflowstore.RepositoryTarget, error) {
	return f.value, f.err
}

func workflowRepositoryRouter(service WorkflowRepositoryService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service))
	return router
}

func TestWorkflowRepositoryRoutesUseGlobalTargetKeys(t *testing.T) {
	service := &fakeWorkflowRepositoryService{
		values: []workflowstore.RepositoryTarget{{RepoTarget: "relay", LocalPath: "/repo"}},
		value:  workflowstore.RepositoryTarget{RepoTarget: "relay", LocalPath: "/repo"},
	}
	response := httptest.NewRecorder()
	workflowRepositoryRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repositories", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"repoTarget":"relay"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	workflowRepositoryRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/repositories", strings.NewReader(`{"repoTarget":"relay","localPath":"/repo"}`)),
	)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "project") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowRepositoryMissingTargetIsNotFound(t *testing.T) {
	service := &fakeWorkflowRepositoryService{err: sql.ErrNoRows}
	response := httptest.NewRecorder()
	workflowRepositoryRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repositories/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
