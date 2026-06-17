package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func setupArtifactTest(t *testing.T) (*store.Store, *ArtifactsHandler, int64) {
	t.Helper()
	s := setupTestStore(t)
	h := NewArtifactsHandler(s)

	repo, err := s.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "draft", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	content := "test artifact content for kind=test_kind"
	artifactPath, err := artifacts.Write(run.ID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt"), []byte(content))
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	_, err = s.CreateArtifact(run.ID, "agent_prompt", artifactPath, "text/plain")
	if err != nil {
		t.Fatalf("create artifact record: %v", err)
	}

	return s, h, run.ID
}

func TestArtifactPlainViewStillReturnsTextPlain(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(runID))
	rctx.URLParams.Add("kind", "agent_prompt")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+testItoa(runID)+"/artifacts/agent_prompt", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.View(w, r)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected Content-Type to contain text/plain, got %q", ct)
	}
	if !strings.Contains(string(body), "test artifact content") {
		t.Errorf("expected raw artifact content in response")
	}
}

func TestArtifactDownloadStillReturnsAttachment(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(runID))
	rctx.URLParams.Add("kind", "agent_prompt")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+testItoa(runID)+"/artifacts/agent_prompt/download", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Download(w, r)

	resp := w.Result()

	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Errorf("expected Content-Disposition to include attachment, got %q", cd)
	}
}

func TestArtifactNotFound(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(runID))
	rctx.URLParams.Add("kind", "nonexistent")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+testItoa(runID)+"/artifacts/nonexistent", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.View(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing artifact via View, got %d", resp.StatusCode)
	}
}

func TestArtifactDownloadNotFound(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(runID))
	rctx.URLParams.Add("kind", "nonexistent")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+testItoa(runID)+"/artifacts/nonexistent/download", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Download(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing artifact via Download, got %d", resp.StatusCode)
	}
}
