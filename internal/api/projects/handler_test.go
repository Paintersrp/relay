package projects

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"relay/internal/api/shared"
	appprojects "relay/internal/app/projects"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func TestProjectAPIFlow(t *testing.T) {
	t.Parallel()

	router := newProjectAPITestServer(t)

	createBody := []byte(`{"project_id":"relay","name":"Relay","description":"Registry test","status":"active"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp ProjectAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !createResp.Success || createResp.Project == nil {
		t.Fatalf("expected created project response, got %+v", createResp)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var listResp ProjectAPIResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listResp.Count != 1 || len(listResp.Projects) != 1 {
		t.Fatalf("expected one project, got %+v", listResp)
	}

	repoBody := []byte(`{"repo_id":"relay","role":"primary","local_path":"D:\\Code\\relay","allowed_roots":["internal"],"ignored_globs":["node_modules/**"],"max_file_size_bytes":262144,"include_untracked":true}`)
	repoReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/repositories", bytes.NewReader(repoBody))
	repoReq.Header.Set("Content-Type", "application/json")
	repoRec := httptest.NewRecorder()
	router.ServeHTTP(repoRec, repoReq)
	if repoRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", repoRec.Code, repoRec.Body.String())
	}

	var repoResp ProjectAPIResponse
	if err := json.NewDecoder(repoRec.Body).Decode(&repoResp); err != nil {
		t.Fatalf("decode repo response: %v", err)
	}
	if repoResp.Repository == nil || repoResp.Repository.Role != "primary" {
		t.Fatalf("expected primary repo response, got %+v", repoResp)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/projects/relay", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var getResp ProjectAPIResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.Project == nil {
		t.Fatal("expected project payload")
	}
	if len(getResp.Project.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %+v", getResp.Project.Repositories)
	}
	if getResp.Project.Repositories[0].RepoID != "relay" {
		t.Fatalf("expected repoId relay, got %+v", getResp.Project.Repositories[0])
	}
}

func TestProjectAPIRejectsInvalidRepositoryConfig(t *testing.T) {
	t.Parallel()

	router := newProjectAPITestServer(t)

	createBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	invalidBody := []byte("{\"repo_id\":\"relay\",\"role\":\"invalid\",\"local_path\":\"D:\\\\Code\\\\relay\\nnope\"}")
	req := httptest.NewRequest(http.MethodPost, "/api/projects/relay/repositories", bytes.NewReader(invalidBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp shared.ErrorShape
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %+v", errResp)
	}
}

func newProjectAPITestServer(t *testing.T) http.Handler {
	t.Helper()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})

	h := NewHandler(appprojects.NewService(st))
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		MountRoutes(r, h)
	})

	return router
}
