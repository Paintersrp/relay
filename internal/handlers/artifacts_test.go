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

func TestArtifactPreviewRendersPartialHTML(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "agent_prompt")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/agent_prompt/preview", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Preview(w, r)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type to contain text/html, got %q", ct)
	}
	if !strings.Contains(string(body), `id="run-artifact-preview"`) {
		t.Errorf("expected id=\"run-artifact-preview\" in response")
	}
	if !strings.Contains(string(body), "agent_prompt") {
		t.Errorf("expected artifact kind in response")
	}
	if !strings.Contains(string(body), "test artifact content") {
		t.Errorf("expected artifact content in response")
	}
}

func TestArtifactPreviewTruncatesLargeContent(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	largeContent := strings.Repeat("A", artifactPreviewMaxBytes+1000)
	artifactPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte(largeContent))
	if err != nil {
		t.Fatalf("write large artifact: %v", err)
	}
	_, err = h.store.CreateArtifact(runID, "agent_result_raw", artifactPath, "text/plain")
	if err != nil {
		t.Fatalf("create artifact record: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "agent_result_raw")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/agent_result_raw/preview", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Preview(w, r)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Preview truncated to the first 64 KB") {
		t.Errorf("expected truncation helper text in response")
	}
	if strings.Contains(string(body), largeContent) {
		t.Errorf("expected response not to contain full oversized content")
	}
}

func TestArtifactPlainViewStillReturnsTextPlain(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "agent_prompt")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/agent_prompt", nil)
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
	if strings.Contains(string(body), `id="run-artifact-preview"`) {
		t.Errorf("expected no preview wrapper in plain view response")
	}
}

func TestArtifactDownloadStillReturnsAttachment(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "agent_prompt")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/agent_prompt/download", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Download(w, r)

	resp := w.Result()

	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Errorf("expected Content-Disposition to include attachment, got %q", cd)
	}
}

func TestArtifactPreviewInvalidRunID(t *testing.T) {
	_, h, _ := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "invalid")
	rctx.URLParams.Add("kind", "agent_prompt")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/invalid/artifacts/agent_prompt/preview", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Preview(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", resp.StatusCode)
	}
}

func TestArtifactPreviewNotFound(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "nonexistent")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/nonexistent/preview", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Preview(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", resp.StatusCode)
	}
}

func TestArtifactPreviewContentHelper(t *testing.T) {
	small := []byte("small content")
	content, truncated := artifactPreviewContent(small)
	if truncated {
		t.Error("expected truncated=false for small content")
	}
	if content != "small content" {
		t.Errorf("expected 'small content', got %q", content)
	}

	large := make([]byte, artifactPreviewMaxBytes+10)
	for i := range large {
		large[i] = 'B'
	}
	content, truncated = artifactPreviewContent(large)
	if !truncated {
		t.Error("expected truncated=true for large content")
	}
	if len(content) != artifactPreviewMaxBytes {
		t.Errorf("expected truncated length %d, got %d", artifactPreviewMaxBytes, len(content))
	}
}

func TestArtifactNotFound(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "nonexistent")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/nonexistent", nil)
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
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "nonexistent")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/nonexistent/download", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Download(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing artifact via Download, got %d", resp.StatusCode)
	}
}

func TestArtifactWritingLargeKind(t *testing.T) {
	_, h, runID := setupArtifactTest(t)

	content := "small"
	artifactPath, err := artifacts.Write(runID, "git_diff_patch", pipeline.ArtifactFilename("git_diff_patch"), []byte(content))
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	_, err = h.store.CreateArtifact(runID, "git_diff_patch", artifactPath, "text/plain")
	if err != nil {
		t.Fatalf("create artifact record: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	rctx.URLParams.Add("kind", "git_diff_patch")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/git_diff_patch/preview", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.Preview(w, r)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), content) {
		t.Errorf("expected artifact content in preview")
	}
}
