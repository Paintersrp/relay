package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/repos"
	"relay/internal/store"
)

func TestRoutingCompatibility(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000")

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	rs := repos.NewService(st, logger)
	handler := BuildRoutes(st, rs, logger)

	// 1. GET / redirects to React base /runs
	t.Run("GET / redirects to /runs", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if loc != "http://localhost:3000/runs" {
			t.Errorf("expected redirect to http://localhost:3000/runs, got %q", loc)
		}
	})

	// 2. GET /handoffs/new redirects to /runs/new
	t.Run("GET /handoffs/new redirects to /runs/new", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/handoffs/new", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if loc != "http://localhost:3000/runs/new" {
			t.Errorf("expected redirect to http://localhost:3000/runs/new, got %q", loc)
		}
	})

	// 3. GET /runs/{id} redirects to /runs/{id}/{resolvedStep}
	t.Run("GET /runs/{id} redirects based on status", func(t *testing.T) {
		repo, err := st.CreateRepo("compat-test-repo", filepath.Join(dir, "compat-repo"))
		if err != nil {
			t.Fatalf("failed to create repo: %v", err)
		}

		// draft status -> intake step
		runDraft, err := st.CreateRun(repo.ID, "Run Draft", "draft", "gpt-4", "gpt-4", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
		reqDraft := httptest.NewRequest("GET", fmt.Sprintf("/runs/%d", runDraft.ID), nil)
		wDraft := httptest.NewRecorder()
		handler.ServeHTTP(wDraft, reqDraft)
		if wDraft.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", wDraft.Code)
		}
		locDraft := wDraft.Header().Get("Location")
		expectedDraft := fmt.Sprintf("http://localhost:3000/runs/%d/intake", runDraft.ID)
		if locDraft != expectedDraft {
			t.Errorf("expected redirect to %q, got %q", expectedDraft, locDraft)
		}

		// validation_passed status -> audit step
		runAudit, err := st.CreateRun(repo.ID, "Run Audit", "validation_passed", "gpt-4", "gpt-4", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
		reqAudit := httptest.NewRequest("GET", fmt.Sprintf("/runs/%d", runAudit.ID), nil)
		wAudit := httptest.NewRecorder()
		handler.ServeHTTP(wAudit, reqAudit)
		if wAudit.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", wAudit.Code)
		}
		locAudit := wAudit.Header().Get("Location")
		expectedAudit := fmt.Sprintf("http://localhost:3000/runs/%d/audit", runAudit.ID)
		if locAudit != expectedAudit {
			t.Errorf("expected redirect to %q, got %q", expectedAudit, locAudit)
		}
	})

	// 4. GET /runs/{id}/agent-run-monitor redirects to React /runs/{id}/execute
	t.Run("GET /runs/{id}/agent-run-monitor redirects to execute", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/runs/42/agent-run-monitor", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if loc != "http://localhost:3000/runs/42/execute" {
			t.Errorf("expected redirect to http://localhost:3000/runs/42/execute, got %q", loc)
		}
	})

	// 5. raw artifact view and download routes remain registered (do not return 404)
	t.Run("GET /runs/{id}/artifacts/{kind} and download", func(t *testing.T) {
		repo, _ := st.CreateRepo("compat-test-repo-artifacts", filepath.Join(dir, "compat-repo-2"))
		run, _ := st.CreateRun(repo.ID, "Run Artifacts", "draft", "gpt-4", "gpt-4", "main")

		reqView := httptest.NewRequest("GET", fmt.Sprintf("/runs/%d/artifacts/planner_handoff", run.ID), nil)
		wView := httptest.NewRecorder()
		handler.ServeHTTP(wView, reqView)
		// We expect StatusNotFound or StatusOK depending on file presence, but NOT API fallback or routing mismatch.
		// Since the file doesn't exist, handlers.ArtifactsHandler.View will return 404 (file not found), which is correct.
		// Let's assert it is registered (doesn't return default 404 router payload).
		if wView.Code != http.StatusNotFound {
			t.Errorf("expected 440/404 file not found status, got %d", wView.Code)
		}
		if strings.Contains(wView.Body.String(), "API route not found") {
			t.Errorf("unexpected router fallback view: %s", wView.Body.String())
		}
	})

	// 6. instructions routes remain registered
	t.Run("GET /instructions list/view/download", func(t *testing.T) {
		reqList := httptest.NewRequest("GET", "/instructions", nil)
		wList := httptest.NewRecorder()
		handler.ServeHTTP(wList, reqList)
		if wList.Code != http.StatusOK {
			t.Errorf("expected 200, got %d. Body: %s", wList.Code, wList.Body.String())
		}

		reqView := httptest.NewRequest("GET", "/instructions/handoff", nil)
		wView := httptest.NewRecorder()
		handler.ServeHTTP(wView, reqView)
		// Since handoff instructions exist or might not exist in test env, we just want to ensure it is handled
		if wView.Code == http.StatusInternalServerError {
			t.Errorf("unexpected 500 error in instructions view: %s", wView.Body.String())
		}
	})

	// 7. settings repo routes remain registered
	t.Run("GET /settings/repos remains handled", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/settings/repos", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code == http.StatusInternalServerError {
			t.Errorf("unexpected 500 in settings repos: %s", w.Body.String())
		}
	})

	// 8. /api/* unknown route returns JSON NOT_FOUND
	t.Run("GET /api/unknown returns JSON 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/unknown_route_999", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("expected JSON response, got %q", w.Header().Get("Content-Type"))
		}
		if !strings.Contains(w.Body.String(), `"error":"NOT_FOUND"`) {
			t.Errorf("expected NOT_FOUND body, got %s", w.Body.String())
		}
	})
}
