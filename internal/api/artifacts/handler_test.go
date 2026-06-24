package artifacts

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	appruns "relay/internal/app/runs"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func TestListArtifactsMissingRun(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "test.db"), logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := NewHandler(appruns.NewService(st, logger, nil))
	r := chi.NewRouter()
	r.Get("/api/runs/{id}/artifacts", h.ListArtifacts)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/999999/artifacts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	body := decodeErrorBody(t, w)
	if body.Error != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND, got %q", body.Error)
	}
}

func TestListArtifactsRunLookupError(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "test.db"), logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	h := NewHandler(appruns.NewService(st, logger, nil))
	r := chi.NewRouter()
	r.Get("/api/runs/{id}/artifacts", h.ListArtifacts)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/1/artifacts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	body := decodeErrorBody(t, w)
	if body.Error != "INTERNAL_ERROR" {
		t.Fatalf("expected INTERNAL_ERROR, got %q", body.Error)
	}
	if body.Message != "Failed to lookup run" {
		t.Fatalf("expected lookup failure message, got %q", body.Message)
	}
}

func decodeErrorBody(t *testing.T, w *httptest.ResponseRecorder) struct {
	Error   string `json:"error"`
	Message string `json:"message"`
} {
	t.Helper()
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body
}
