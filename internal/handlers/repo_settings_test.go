package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"relay/internal/repos"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func setupRepoSettingsTest(t *testing.T) (*store.Store, *RepoSettingsHandler) {
	t.Helper()
	s := setupTestStore(t)
	rs := repos.NewService(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h := NewRepoSettingsHandler(s, rs, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return s, h
}

func TestRepoSettingsAddRootNonHTMXStillRedirects(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	_, err := s.CreateRepoRoot("D:/existing")
	if err != nil {
		t.Fatalf("create existing root: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/settings/repos/roots", strings.NewReader("path=D:/new"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.AddRoot(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 See Other for non-HTMX request, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/settings/repos" {
		t.Errorf("expected Location /settings/repos, got %q", loc)
	}
}

func TestRepoSettingsAddRootHTMXRendersShellWithoutRedirect(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	_, err := s.CreateRepoRoot("D:/existing")
	if err != nil {
		t.Fatalf("create existing root: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/settings/repos/roots", strings.NewReader("path=D:/new"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")

	h.AddRoot(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for HTMX request, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "" {
		t.Errorf("expected no Location header for HTMX request, got %q", resp.Header.Get("Location"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="repo-settings-shell"`) {
		t.Errorf("expected repo-settings-shell in HTMX response body")
	}
}

func TestRepoSettingsToggleRootHTMXRendersShellWithoutRedirect(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	root, err := s.CreateRepoRoot("D:/test")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(root.ID))
	url := "/settings/repos/roots/" + testItoa(root.ID) + "/toggle"
	r := httptest.NewRequest("POST", url, strings.NewReader("enabled=0"))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")

	w := httptest.NewRecorder()
	h.ToggleRoot(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for HTMX toggle, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "" {
		t.Errorf("expected no Location header for HTMX toggle, got %q", resp.Header.Get("Location"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="repo-settings-shell"`) {
		t.Errorf("expected repo-settings-shell in HTMX toggle response")
	}
}

func TestRepoSettingsDeleteRootHTMXRendersShellWithoutRedirect(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	root, err := s.CreateRepoRoot("D:/test")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(root.ID))
	url := "/settings/repos/roots/" + testItoa(root.ID) + "/delete"
	r := httptest.NewRequest("POST", url, nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.Header.Set("HX-Request", "true")

	w := httptest.NewRecorder()
	h.DeleteRoot(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for HTMX delete, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "" {
		t.Errorf("expected no Location header for HTMX delete, got %q", resp.Header.Get("Location"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="repo-settings-shell"`) {
		t.Errorf("expected repo-settings-shell in HTMX delete response")
	}
}

func TestRepoSettingsScanHTMXRendersShellWithSummary(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	_, err := s.CreateRepoRoot("D:/test")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/settings/repos/scan", nil)
	r.Header.Set("HX-Request", "true")

	h.Scan(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for HTMX scan, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="repo-settings-shell"`) {
		t.Errorf("expected repo-settings-shell in HTMX scan response")
	}
	if !strings.Contains(string(body), "Scan Summary") {
		t.Errorf("expected Scan Summary in HTMX scan response")
	}
}

func TestRepoSettingsAddRootInvalidPathReturns400(t *testing.T) {
	_, h := setupRepoSettingsTest(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/settings/repos/roots", strings.NewReader("path="))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.AddRoot(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for empty path, got %d", resp.StatusCode)
	}
}

func TestRepoSettingsAddRootNonHTMXStillCreatesRoot(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/settings/repos/roots", strings.NewReader("path=D:/new"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.AddRoot(w, r)

	// Root should exist in store
	roots, err := s.ListRepoRoots()
	if err != nil {
		t.Fatalf("list roots: %v", err)
	}
	found := false
	for _, root := range roots {
		if root.Path == "D:/new" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected root D:/new to be created in store")
	}
}

func TestRepoSettingsDeleteRootNonHTMXStillDeletesRoot(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	root, err := s.CreateRepoRoot("D:/test")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(root.ID))
	url := "/settings/repos/roots/" + testItoa(root.ID) + "/delete"
	r := httptest.NewRequest("POST", url, nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.DeleteRoot(w, r)

	// Root should no longer exist
	roots, err := s.ListRepoRoots()
	if err != nil {
		t.Fatalf("list roots: %v", err)
	}
	for _, r := range roots {
		if r.ID == root.ID {
			t.Errorf("expected root to be deleted")
			break
		}
	}
}

func TestRepoSettingsToggleRootNonHTMXStillRedirects(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	root, err := s.CreateRepoRoot("D:/test")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(root.ID))
	url := "/settings/repos/roots/" + testItoa(root.ID) + "/toggle"
	r := httptest.NewRequest("POST", url, strings.NewReader("enabled=0"))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	h.ToggleRoot(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 See Other for non-HTMX toggle, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/settings/repos" {
		t.Errorf("expected Location /settings/repos, got %q", loc)
	}
}

func TestRepoSettingsDeleteRootNonHTMXStillRedirects(t *testing.T) {
	s, h := setupRepoSettingsTest(t)

	root, err := s.CreateRepoRoot("D:/test")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testItoa(root.ID))
	url := "/settings/repos/roots/" + testItoa(root.ID) + "/delete"
	r := httptest.NewRequest("POST", url, nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.DeleteRoot(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 See Other for non-HTMX delete, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/settings/repos" {
		t.Errorf("expected Location /settings/repos, got %q", loc)
	}
}
