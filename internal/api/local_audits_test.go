package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func TestLocalAuditAPIFlowAndValidation(t *testing.T) {
	requireAPILocalAuditGit(t)
	apiH, st, router, repoRoot := newLocalAuditAPITestServer(t)
	_ = apiH
	project, err := st.CreateProject("relay", "Relay", "", "active", "relay")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := st.UpsertProjectRepository(store.UpsertProjectRepositoryParams{
		ProjectRowID:     project.ID,
		RepoID:           "relay",
		Role:             "primary",
		LocalPath:        repoRoot,
		DefaultBranch:    "main",
		AllowedRootsJSON: "[]",
		IgnoredGlobsJSON: "[]",
		MaxFileSizeBytes: 262144,
		IncludeUntracked: 1,
		Enabled:          1,
	}); err != nil {
		t.Fatalf("UpsertProjectRepository: %v", err)
	}

	body := []byte(`{"mode":"recent_commit","project_id":"relay","repo_ids":["relay"],"title":"Audit latest Relay commit"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/audits/local", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Success   bool   `json:"success"`
		AuditID   string `json:"audit_id"`
		Mode      string `json:"mode"`
		Status    string `json:"status"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !createResp.Success || createResp.AuditID == "" || createResp.Mode != "recent_commit" {
		t.Fatalf("unexpected create response: %+v", createResp)
	}

	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/audits/local/"+createResp.AuditID, nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/projects/relay/audits?limit=10", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	cases := []struct {
		name string
		body string
	}{
		{"invalid mode", `{"mode":"github_pr","project_id":"relay"}`},
		{"missing project", `{"mode":"full_repository"}`},
		{"feature no selectors", `{"mode":"feature_slice","project_id":"relay"}`},
		{"absolute path", `{"mode":"feature_slice","project_id":"relay","paths":["C:/tmp/x"]}`},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/audits/local", bytes.NewReader([]byte(tc.body)))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func newLocalAuditAPITestServer(t *testing.T) (*APIHandler, *store.Store, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	artifacts.SetBaseDir(filepath.Join(dir, "artifacts"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repoRoot := setupAPILocalAuditGitRepo(t)
	apiH := NewAPIHandler(st, logger)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		r.Post("/audits/local", apiH.CreateLocalAudit)
		r.Get("/audits/local/{auditId}", apiH.GetLocalAudit)
		r.Get("/projects/{projectId}/audits", apiH.ListProjectLocalAudits)
	})
	return apiH, st, router, repoRoot
}

func requireAPILocalAuditGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func setupAPILocalAuditGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runAPILocalAuditGit(t, root, "init", "-b", "main")
	runAPILocalAuditGit(t, root, "config", "user.email", "relay-test@example.invalid")
	runAPILocalAuditGit(t, root, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}
	runAPILocalAuditGit(t, root, "add", ".")
	runAPILocalAuditGit(t, root, "commit", "-m", "initial commit")
	return root
}

func runAPILocalAuditGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
