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

	workflowapp "relay/internal/app/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

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
	store, service, vaults := openWorkflowRouteTestStore(t)
	handler, err := BuildWorkflowRoutes(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "owner-test", vaults)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/repositories", "/api/projects", "/api/plans", "/api/runs"} {
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
		{http.MethodPost, "/api/runs"},
		{http.MethodPost, "/api/runs/1/approve-intake"},
		{http.MethodPost, "/api/runs/1/prepare"},
		{http.MethodPost, "/api/runs/1/render-brief"},
		{http.MethodGet, "/api/workflow/runs/run-test/attempts"},
		{http.MethodPost, "/api/projects/project/plan-attempts"},
		{http.MethodGet, "/api/projects/project/refactor/candidates"},
		{http.MethodPost, "/handoffs"},

		{http.MethodPost, "/mcp/apps/planner-authoring/v1"},
		{http.MethodPost, "/mcp/apps/planner-plan/v1"},
		{http.MethodPost, "/mcp/apps/planner-execution/v1"},
		{http.MethodPost, "/mcp/apps/auditor-review/v1"},
		{http.MethodPost, "/mcp/apps/auditor-audit/v1"},
		{http.MethodPost, "/mcp/apps/auditor-remediation/v1"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s => %d %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	_ = service
}

// The cutover control plane is removed and legacy Plan write admission is
// retired: the workflow runtime mounts no cutover endpoint and no route capable
// of creating or changing a Plan.
func TestWorkflowRuntimeMountsNoCutoverOrPlanWriteRoutes(t *testing.T) {
	store, _, vaults := openWorkflowRouteTestStore(t)
	handler, err := BuildWorkflowRoutes(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "owner-test", vaults)
	if err != nil {
		t.Fatal(err)
	}

	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/cutover/state"},
		{http.MethodGet, "/api/cutover/history"},
		{http.MethodGet, "/api/cutover/readiness"},
		{http.MethodPost, "/api/cutover/prepare"},
		{http.MethodPost, "/api/cutover/activate"},
		{http.MethodPost, "/api/cutover/rollback"},
		{http.MethodPost, "/api/cutover/roll-forward"},
		{http.MethodPost, "/api/cutover/execution-boundary"},
		{http.MethodPost, "/api/plans"},
		{http.MethodPatch, "/api/plans/plan-test/project"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s => %d %s", request.method, request.path, response.Code, response.Body.String())
		}
	}

	// The retired aggregate MCP route is unavailable.
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("POST /mcp => %d %s, want 404", response.Code, response.Body.String())
	}
}

func TestWorkflowRunRedirectUsesSpecificationStage(t *testing.T) {
	store, service, vaults := openWorkflowRouteTestStore(t)
	var created workflowstore.Run
	err := store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		var createErr error
		created, createErr = tx.CreateRun(context.Background(), workflowstore.CreateRunParams{RunID: "run-route-test", FeatureSlug: "route-test", RepoTarget: "relay", Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: strings.Repeat("a", 40)})
		return createErr
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000/")
	handler, err := BuildWorkflowRoutes(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "owner-test", vaults)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs/"+created.RunID, nil))
	if response.Code != http.StatusFound {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	expected := "http://localhost:3000/runs/" + created.RunID + "/specification"
	if response.Header().Get("Location") != expected {
		t.Fatalf("location = %q, want %q", response.Header().Get("Location"), expected)
	}
	_ = service
}

type routeTestSourceVaults struct {
	root string
}

func (v routeTestSourceVaults) Root() string {
	return v.root
}

func (routeTestSourceVaults) ReadPath(context.Context, sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	return sourcevault.ReadPathResult{}, &sourcevault.Error{Code: sourcevault.CodeVaultUnavailable}
}

func openWorkflowRouteTestStore(t *testing.T) (*workflowstore.Store, *workflowapp.Service, routeTestSourceVaults) {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	root := filepath.Dir(store.ArtifactStore().Root())
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
	return store, service, routeTestSourceVaults{root: filepath.Join(root, "source-vaults")}
}
