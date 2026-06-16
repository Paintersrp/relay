package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func TestAPI(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	repo, err := s.CreateRepo("test-repo", filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	run, err := s.CreateRun(repo.ID, "Test Run Title", "draft", "gpt-4o", "gpt-4o", "main")
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	_, err = s.CreateCheck(run.ID, "validation", "pass", "Intake validation passed", `{"status":"pass"}`)
	if err != nil {
		t.Fatalf("failed to create check: %v", err)
	}

	_, err = s.CreateEvent(run.ID, "info", "Run initialized")
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	apiH := NewAPIHandler(s, logger)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(CORSMiddleware)
		r.Get("/runs", apiH.ListRuns)
		r.Get("/runs/{id}", apiH.GetRun)
		r.Get("/runs/{id}/artifacts", apiH.ListArtifacts)
		r.Get("/runs/{id}/events", apiH.ListEvents)
		r.Post("/intake/planner-handoff", apiH.IntakePlannerHandoff)
		r.Post("/runs/{id}/approve-intake", apiH.ApproveIntake)
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"NOT_FOUND","message":"API route not found"}`))
		})
	})

	t.Run("GET /api/runs", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/runs", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Errorf("expected CORS header set, got %q", w.Header().Get("Access-Control-Allow-Origin"))
		}

		var runs []RelayRun
		if err := json.NewDecoder(w.Body).Decode(&runs); err != nil {
			t.Fatalf("failed to decode runs: %v", err)
		}
		if len(runs) != 1 {
			t.Errorf("expected 1 run, got %d", len(runs))
		}
		if runs[0].ID != strconv.FormatInt(run.ID, 10) {
			t.Errorf("expected run ID %d, got %s", run.ID, runs[0].ID)
		}
		if runs[0].Validation.Passed != 1 {
			t.Errorf("expected 1 passed check, got %d", runs[0].Validation.Passed)
		}
	})

	t.Run("GET /api/runs/{id}", func(t *testing.T) {
		runIDStr := strconv.FormatInt(run.ID, 10)
		req := httptest.NewRequest("GET", "/api/runs/"+runIDStr, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var relayRun RelayRun
		if err := json.NewDecoder(w.Body).Decode(&relayRun); err != nil {
			t.Fatalf("failed to decode run: %v", err)
		}
		if relayRun.ID != runIDStr {
			t.Errorf("expected run ID %s, got %s", runIDStr, relayRun.ID)
		}
		if relayRun.ActiveStep != "intake" {
			t.Errorf("expected active step 'intake', got %q", relayRun.ActiveStep)
		}
	})

	t.Run("GET /api/runs/{id} - NOT FOUND", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/runs/999999", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}

		var errShape RelayApiErrorShape
		if err := json.NewDecoder(w.Body).Decode(&errShape); err != nil {
			t.Fatalf("failed to decode error shape: %v", err)
		}
		if errShape.Error != "NOT_FOUND" {
			t.Errorf("expected error code 'NOT_FOUND', got %q", errShape.Error)
		}
	})

	t.Run("GET /api/runs/{id}/artifacts", func(t *testing.T) {
		runIDStr := strconv.FormatInt(run.ID, 10)
		req := httptest.NewRequest("GET", "/api/runs/"+runIDStr+"/artifacts", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var artifacts []RelayArtifact
		if err := json.NewDecoder(w.Body).Decode(&artifacts); err != nil {
			t.Fatalf("failed to decode artifacts: %v", err)
		}
		if len(artifacts) != 0 {
			t.Errorf("expected 0 artifacts, got %d", len(artifacts))
		}
	})

	t.Run("GET /api/runs/{id}/events", func(t *testing.T) {
		runIDStr := strconv.FormatInt(run.ID, 10)
		req := httptest.NewRequest("GET", "/api/runs/"+runIDStr+"/events", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var events []RelayRunEvent
		if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
			t.Fatalf("failed to decode events: %v", err)
		}
		if len(events) != 1 {
			t.Errorf("expected 1 event, got %d", len(events))
		}
		if events[0].Message != "Run initialized" {
			t.Errorf("expected event message 'Run initialized', got %q", events[0].Message)
		}
	})

	t.Run("POST /api/intake/planner-handoff - Success (New Run)", func(t *testing.T) {
		body := `{"planner_handoff_markdown":"---\ntitle: Standard Handoff\nrepo: test-repo\nbranch: main\n---\n# Standard Handoff\nGoal: test","repo":"test-repo"}`
		req := httptest.NewRequest("POST", "/api/intake/planner-handoff", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp PlannerHandoffIntakeResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Error("expected success = true")
		}
		if resp.RunID == "" {
			t.Error("expected non-empty runId")
		}
		if resp.Status != "intake_needs_review" && resp.Status != "intake_received" {
			t.Errorf("unexpected status %q", resp.Status)
		}
		if resp.ReviewURL != "/runs/"+resp.RunID+"/intake" {
			t.Errorf("expected review url '/runs/%s/intake', got %q", resp.RunID, resp.ReviewURL)
		}
		if len(resp.Artifacts) == 0 {
			t.Error("expected artifacts in response")
		}
	})

	t.Run("POST /api/intake/planner-handoff - Success (Attach Run)", func(t *testing.T) {
		runIDStr := strconv.FormatInt(run.ID, 10)
		body := fmt.Sprintf(`{"planner_handoff_markdown":"---\ntitle: Attach Handoff\nrepo: test-repo\nbranch: main\n---\n# Attach Handoff","run_id":"%s"}`, runIDStr)
		req := httptest.NewRequest("POST", "/api/intake/planner-handoff", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp PlannerHandoffIntakeResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.RunID != runIDStr {
			t.Errorf("expected runId %s, got %s", runIDStr, resp.RunID)
		}
	})

	t.Run("POST /api/intake/planner-handoff - Empty Markdown (400)", func(t *testing.T) {
		body := `{"planner_handoff_markdown":"","repo":"test-repo"}`
		req := httptest.NewRequest("POST", "/api/intake/planner-handoff", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("POST /api/intake/planner-handoff - Unknown run_id (404)", func(t *testing.T) {
		body := `{"planner_handoff_markdown":"# Title","run_id":"999999","repo":"test-repo"}`
		req := httptest.NewRequest("POST", "/api/intake/planner-handoff", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("POST /api/runs/{id}/approve-intake - Success Approve", func(t *testing.T) {
		_, err := s.UpdateRunStatus(run.ID, "intake_received")
		if err != nil {
			t.Fatalf("failed to reset run status: %v", err)
		}

		body := `{"action":"approve","notes":"All clean!","overrides":{"model":"gpt-4o-custom"}}`
		req := httptest.NewRequest("POST", "/api/runs/"+strconv.FormatInt(run.ID, 10)+"/approve-intake", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		dbRun, err := s.GetRun(run.ID)
		if err != nil {
			t.Fatalf("failed to query run: %v", err)
		}
		if dbRun.Status != "approved_for_prepare" {
			t.Errorf("expected status approved_for_prepare, got %s", dbRun.Status)
		}
		if dbRun.SelectedModel != "gpt-4o-custom" {
			t.Errorf("expected model gpt-4o-custom, got %s", dbRun.SelectedModel)
		}
	})

	t.Run("POST /api/runs/{id}/approve-intake - Success Needs Revision", func(t *testing.T) {
		_, err := s.UpdateRunStatus(run.ID, "intake_received")
		if err != nil {
			t.Fatalf("failed to reset run status: %v", err)
		}

		body := `{"action":"needs_revision","notes":"Please fix typos"}`
		req := httptest.NewRequest("POST", "/api/runs/"+strconv.FormatInt(run.ID, 10)+"/approve-intake", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		dbRun, err := s.GetRun(run.ID)
		if err != nil {
			t.Fatalf("failed to query run: %v", err)
		}
		if dbRun.Status != "intake_needs_review" {
			t.Errorf("expected status intake_needs_review, got %s", dbRun.Status)
		}
	})

	t.Run("POST /api/runs/{id}/approve-intake - Conflict (409)", func(t *testing.T) {
		// Run status is already "intake_needs_review", let's update it to something invalid like "completed"
		_, err := s.UpdateRunStatus(run.ID, "completed")
		if err != nil {
			t.Fatalf("failed to update status: %v", err)
		}

		body := `{"action":"approve","notes":"All clean!"}`
		req := httptest.NewRequest("POST", "/api/runs/"+strconv.FormatInt(run.ID, 10)+"/approve-intake", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Errorf("expected 409, got %d", w.Code)
		}
	})
}
