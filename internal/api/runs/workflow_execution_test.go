package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"relay/internal/executor"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWorkflowExecutionService struct {
	startResult  executor.WorkflowStartResult
	startErr     error
	views        []executor.WorkflowAttemptView
	cancelResult executor.WorkflowCancelResult
}

func (f *fakeWorkflowExecutionService) Start(context.Context, executor.WorkflowStartInput) (executor.WorkflowStartResult, error) {
	return f.startResult, f.startErr
}
func (f *fakeWorkflowExecutionService) Cancel(context.Context, string, string) (executor.WorkflowCancelResult, error) {
	return f.cancelResult, nil
}
func (f *fakeWorkflowExecutionService) Reconcile(context.Context, string, string) (executor.WorkflowCancelResult, error) {
	return f.cancelResult, nil
}
func (f *fakeWorkflowExecutionService) ListAttempts(context.Context, string) ([]executor.WorkflowAttemptView, error) {
	return f.views, nil
}
func (f *fakeWorkflowExecutionService) GetAttempt(_ context.Context, _, attemptID string) (executor.WorkflowAttemptView, error) {
	for _, view := range f.views {
		if view.Attempt.AttemptID == attemptID {
			return view, nil
		}
	}
	return executor.WorkflowAttemptView{}, sql.ErrNoRows
}

func workflowExecutionRouter(service WorkflowExecutionService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowExecutionRoutes(router, NewWorkflowExecutionHandler(service))
	return router
}

func TestWorkflowExecutionStartPreflightBlocker(t *testing.T) {
	service := &fakeWorkflowExecutionService{
		startErr: &executor.WorkflowPreflightError{Result: workflowrepos.ExecutionPreflightResult{
			OK:          false,
			BlockerCode: "repository_dirty",
			BlockerText: "repository is dirty",
		}},
	}
	request := httptest.NewRequest(http.MethodPost, "/workflow/runs/run-test/attempts", strings.NewReader(`{"adapter":"codex","model":"model"}`))
	response := httptest.NewRecorder()
	workflowExecutionRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "EXECUTION_PREFLIGHT_BLOCKED") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowExecutionAttemptMonitoringIsBounded(t *testing.T) {
	attempt := workflowstore.ExecutionAttempt{
		AttemptID:     "attempt-test",
		AttemptNumber: 1,
		Adapter:       "codex",
		Model:         "model",
		Status:        workflowstore.AttemptStatusRunning,
		ResultJSON:    `{"command_preview":"codex exec"}`,
		CreatedAt:     "2026-07-05T00:00:00Z",
	}
	view := executor.WorkflowAttemptView{
		Attempt:             attempt,
		LiveStdout:          "working\n",
		LiveStdoutTruncated: true,
		LiveStdoutBytes:     70000,
	}
	service := &fakeWorkflowExecutionService{
		startResult: executor.WorkflowStartResult{Attempt: attempt, Preflight: workflowrepos.ExecutionPreflightResult{OK: true}},
		views:       []executor.WorkflowAttemptView{view},
	}
	request := httptest.NewRequest(http.MethodGet, "/workflow/runs/run-test/attempts/attempt-test", nil)
	response := httptest.NewRecorder()
	workflowExecutionRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["attemptId"] != "attempt-test" || body["liveStdout"] != "working\n" {
		t.Fatalf("body = %+v", body)
	}
	if body["liveStdoutTruncated"] != true || body["liveStdoutBytes"] != float64(70000) {
		t.Fatalf("bounded-output metadata = %+v", body)
	}
	if strings.Contains(response.Body.String(), "local_path") || strings.Contains(response.Body.String(), "executor_brief") {
		t.Fatalf("response leaked local execution details: %s", response.Body.String())
	}
}

func TestWorkflowExecutionReconcileRoute(t *testing.T) {
	attempt := workflowstore.ExecutionAttempt{
		AttemptID:     "attempt-cleanup",
		AttemptNumber: 1,
		Adapter:       "codex",
		Model:         "model",
		Status:        workflowstore.AttemptStatusSucceeded,
		ResultJSON:    `{}`,
		CreatedAt:     "2026-07-05T00:00:00Z",
	}
	view := executor.WorkflowAttemptView{Attempt: attempt}
	service := &fakeWorkflowExecutionService{
		cancelResult: executor.WorkflowCancelResult{Attempt: attempt},
		views:        []executor.WorkflowAttemptView{view},
	}
	request := httptest.NewRequest(http.MethodPost, "/workflow/runs/run-test/attempts/attempt-cleanup/reconcile", nil)
	response := httptest.NewRecorder()
	workflowExecutionRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "attempt-cleanup") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
