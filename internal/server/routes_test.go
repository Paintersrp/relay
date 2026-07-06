package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowruns "relay/internal/app/runs/workflow"
	workflowapp "relay/internal/app/workflow"
	"relay/internal/repos"
	"relay/internal/store"
	workflowstore "relay/internal/store/workflow"
)

func BuildRoutes(s *store.Store, rs *repos.Service, log *slog.Logger) http.Handler {
	return buildLegacyRoutes(s, rs, log)
}

func TestResolveWorkflowRunStage(t *testing.T) {
	tests := map[string]string{
		workflowstore.RunStatusCreated:          workflowapp.RunStageSpecification,
		workflowstore.RunStatusSetupReady:       workflowapp.RunStageSpecification,
		workflowstore.RunStatusExecuting:        workflowapp.RunStageExecute,
		workflowstore.RunStatusExecutionFailed:  workflowapp.RunStageExecute,
		workflowstore.RunStatusCancelled:        workflowapp.RunStageExecute,
		workflowstore.RunStatusValidating:       workflowapp.RunStageAudit,
		workflowstore.RunStatusValidationFailed: workflowapp.RunStageAudit,
		workflowstore.RunStatusAuditReady:       workflowapp.RunStageAudit,
		workflowstore.RunStatusNeedsRevision:    workflowapp.RunStageAudit,
		workflowstore.RunStatusCompleted:        workflowapp.RunStageAudit,
	}
	for status, expected := range tests {
		stage, err := resolveWorkflowRunStage(status)
		if err != nil || stage != expected {
			t.Fatalf("status %q => %q, %v; want %q", status, stage, err, expected)
		}
	}
	if _, err := resolveWorkflowRunStage("intake_received"); err == nil {
		t.Fatal("legacy status was routed")
	}
}

func TestWorkflowRuntimeMountsOnlyNewOperationalRoutes(t *testing.T) {
	store, service := openWorkflowRouteTestStore(t)
	handler := BuildWorkflowRoutes(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "owner-test")

	for _, path := range []string{"/api/repositories", "/api/plans", "/api/runs"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s => %d %s", path, response.Code, response.Body.String())
		}
	}

	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/projects"},
		{http.MethodPost, "/api/runs/1/approve-intake"},
		{http.MethodPost, "/api/runs/1/prepare"},
		{http.MethodPost, "/api/runs/1/render-brief"},
		{http.MethodGet, "/api/workflow/runs/run-test/attempts"},
		{http.MethodPost, "/api/projects/project/plan-attempts"},
		{http.MethodGet, "/api/projects/project/refactor/candidates"},
		{http.MethodPost, "/handoffs"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s => %d %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	_ = service
}

func TestWorkflowRunRedirectUsesSpecificationStage(t *testing.T) {
	store, service := openWorkflowRouteTestStore(t)
	runService, err := workflowruns.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := runService.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      "route-test",
		RepoTarget:       "relay",
		Branch:           "main",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    []byte(`{"feature_slug":"route-test"}`),
		RenderedMarkdown: []byte("# Brief\n"),
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000/")
	handler := BuildWorkflowRoutes(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "owner-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs/"+created.Run.RunID, nil))
	if response.Code != http.StatusFound {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	expected := "http://localhost:3000/runs/" + created.Run.RunID + "/specification"
	if response.Header().Get("Location") != expected {
		t.Fatalf("location = %q, want %q", response.Header().Get("Location"), expected)
	}
	_ = service
}

func openWorkflowRouteTestStore(t *testing.T) (*workflowstore.Store, *workflowapp.Service) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := workflowapp.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterRepository(context.Background(), "relay", repoPath); err != nil {
		t.Fatal(err)
	}
	return store, service
}
