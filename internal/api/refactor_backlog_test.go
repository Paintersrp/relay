package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func newRefactorAPITestServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(t.TempDir(), "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	apiH := NewAPIHandler(st, logger)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		r.Get("/projects/{projectId}/refactor/discovery-tasks", apiH.ListRefactorDiscoveryTasks)
		r.Post("/projects/{projectId}/refactor/discovery-tasks", apiH.CreateRefactorDiscoveryTask)
		r.Get("/projects/{projectId}/refactor/discovery-tasks/{taskId}", apiH.GetRefactorDiscoveryTask)
		r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/update", apiH.UpdateRefactorDiscoveryTask)
		r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/complete", apiH.CompleteRefactorDiscoveryTask)
		r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/close", apiH.CloseRefactorDiscoveryTask)
		r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/supersede", apiH.SupersedeRefactorDiscoveryTask)
		r.Get("/projects/{projectId}/refactor/candidates", apiH.ListRefactorCandidates)
		r.Post("/projects/{projectId}/refactor/candidates", apiH.CreateRefactorCandidate)
		r.Get("/projects/{projectId}/refactor/candidates/{candidateId}", apiH.GetRefactorCandidate)
		r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/update", apiH.UpdateRefactorCandidate)
		r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/defer", apiH.DeferRefactorCandidate)
		r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/reject", apiH.RejectRefactorCandidate)
		r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/supersede", apiH.SupersedeRefactorCandidate)
	})

	return st, router
}

func seedRefactorProject(t *testing.T, st *store.Store, projectID string) {
	t.Helper()
	if _, err := st.CreateProject(projectID, projectID+" name", "", "active", ""); err != nil {
		t.Fatalf("CreateProject(%s): %v", projectID, err)
	}
}

func validCandidateBody() []byte {
	return []byte(`{
		"candidate_id": "cand-1",
		"title": "Consolidate parsing",
		"problem_summary": "Duplicate parsing branch causes drift.",
		"desired_behavior": "Single parsing path.",
		"rationale": "Reduce maintenance burden.",
		"proposed_pass_name": "Consolidate parsing",
		"proposed_pass_goal": "Remove duplicate parsing branch.",
		"proposed_pass_scope": ["Replace duplicate branch in internal/foo/bar.go"],
		"non_goals": ["Do not change public API"],
		"target_files": ["internal/foo/bar.go"],
		"validation_commands": ["go test ./internal/foo/..."],
		"audit_focus": ["Verify behavior unchanged"],
		"risk_level": "medium"
	}`)
}

func doRequest(t *testing.T, router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRefactorCandidateAPIRequiresProjectScopedRoute(t *testing.T) {
	st, router := newRefactorAPITestServer(t)
	seedRefactorProject(t, st, "relay")

	rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates", validCandidateBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp RefactorBacklogAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success || resp.Candidate == nil {
		t.Fatalf("expected success candidate response, got %+v", resp)
	}
	if resp.Candidate.Status != "ready" {
		t.Fatalf("expected ready status, got %q", resp.Candidate.Status)
	}
}

func TestRefactorCandidateAPIValidationErrorShape(t *testing.T) {
	st, router := newRefactorAPITestServer(t)
	seedRefactorProject(t, st, "relay")

	rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates", []byte(`{"candidate_id":"cand-x","title":"Incomplete"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp RelayApiErrorShape
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %+v", errResp)
	}
	if errResp.Details == nil || errResp.Details["validation"] == nil {
		t.Fatalf("expected details.validation to be present, got %+v", errResp.Details)
	}
}

func TestRefactorCandidateAPINotFoundProject(t *testing.T) {
	_, router := newRefactorAPITestServer(t)

	rec := doRequest(t, router, http.MethodPost, "/api/projects/does-not-exist/refactor/candidates", validCandidateBody())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	getRec := doRequest(t, router, http.MethodGet, "/api/projects/does-not-exist/refactor/candidates/cand-1", nil)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on get, got %d: %s", getRec.Code, getRec.Body.String())
	}
}

func TestRefactorCandidateAPIBadJSON(t *testing.T) {
	st, router := newRefactorAPITestServer(t)
	seedRefactorProject(t, st, "relay")

	rec := doRequest(t, router, http.MethodPost, "/api/projects/relay/refactor/candidates", []byte(`{not json`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp RelayApiErrorShape
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != "BAD_REQUEST" {
		t.Fatalf("expected BAD_REQUEST, got %+v", errResp)
	}
}

func TestRefactorDiscoveryTaskAPIListAndCreate(t *testing.T) {
	st, router := newRefactorAPITestServer(t)
	seedRefactorProject(t, st, "project-a")
	seedRefactorProject(t, st, "project-b")

	createBody := []byte(`{
		"discovery_task_id": "task-1",
		"title": "Investigate parsing",
		"analysis_prompt": "Analyze duplication.",
		"target_scope": {"kind": "directory", "values": ["internal/foo"]}
	}`)
	rec := doRequest(t, router, http.MethodPost, "/api/projects/project-a/refactor/discovery-tasks", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// List in project A: one task.
	listA := doRequest(t, router, http.MethodGet, "/api/projects/project-a/refactor/discovery-tasks", nil)
	if listA.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listA.Code, listA.Body.String())
	}
	var respA RefactorBacklogAPIResponse
	if err := json.NewDecoder(listA.Body).Decode(&respA); err != nil {
		t.Fatalf("decode list A: %v", err)
	}
	if respA.Count != 1 || len(respA.DiscoveryTasks) != 1 {
		t.Fatalf("expected one task in project A, got %+v", respA)
	}
	if respA.DiscoveryTasks[0].TargetScope.Kind != "directory" {
		t.Fatalf("expected directory target scope, got %+v", respA.DiscoveryTasks[0].TargetScope)
	}

	// List in project B: zero tasks.
	listB := doRequest(t, router, http.MethodGet, "/api/projects/project-b/refactor/discovery-tasks", nil)
	if listB.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listB.Code, listB.Body.String())
	}
	var respB RefactorBacklogAPIResponse
	if err := json.NewDecoder(listB.Body).Decode(&respB); err != nil {
		t.Fatalf("decode list B: %v", err)
	}
	if respB.Count != 0 || len(respB.DiscoveryTasks) != 0 {
		t.Fatalf("expected no tasks in project B, got %+v", respB)
	}
}

func TestRefactorCandidateAPIInvalidLimit(t *testing.T) {
	st, router := newRefactorAPITestServer(t)
	seedRefactorProject(t, st, "relay")

	rec := doRequest(t, router, http.MethodGet, "/api/projects/relay/refactor/candidates?limit=0", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d: %s", rec.Code, rec.Body.String())
	}
}
