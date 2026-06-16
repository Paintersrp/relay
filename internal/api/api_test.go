package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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
}
