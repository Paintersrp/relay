package auditor

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
)

func TestLocalAuditServiceCreatesAllModes(t *testing.T) {
	requireLocalAuditGit(t)
	dir := t.TempDir()
	artifacts.SetBaseDir(filepath.Join(dir, "artifacts"))
	st := newLocalAuditTestStore(t, dir)
	repoRoot := setupLocalAuditGitRepo(t)
	project := createLocalAuditProject(t, st, repoRoot)
	insertLocalAuditPlan(t, st)

	if err := os.WriteFile(filepath.Join(repoRoot, "committed.txt"), []byte("updated\n"), 0644); err != nil {
		t.Fatalf("write committed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "token.txt"), []byte("Authorization: Bearer super-secret-token\n"), 0644); err != nil {
		t.Fatalf("write token: %v", err)
	}
	runLocalAuditGit(t, repoRoot, "add", ".")
	runLocalAuditGit(t, repoRoot, "commit", "-m", "second commit")
	if err := os.WriteFile(filepath.Join(repoRoot, "committed.txt"), []byte("worktree change\n"), 0644); err != nil {
		t.Fatalf("write worktree: %v", err)
	}

	svc := NewLocalAuditService(st)
	inputs := []LocalAuditInput{
		{Mode: string(LocalAuditModeRecentCommit), ProjectID: project.ProjectID, RepoIDs: []string{"relay"}, Title: "Recent Commit"},
		{Mode: string(LocalAuditModeSelectedPassChanges), ProjectID: project.ProjectID, RepoIDs: []string{"relay"}, PlanID: "plan-1", PassID: "PASS-001", DiffMode: "worktree"},
		{Mode: string(LocalAuditModeFeatureSlice), ProjectID: project.ProjectID, RepoIDs: []string{"relay"}, Paths: []string{"committed.txt"}, SearchTerms: []string{"worktree"}},
		{Mode: string(LocalAuditModeFullRepository), ProjectID: project.ProjectID, RepoIDs: []string{"relay"}, MaxFiles: 20},
	}
	for _, input := range inputs {
		result, err := svc.Create(t.Context(), input)
		if err != nil {
			t.Fatalf("Create(%s) error: %v", input.Mode, err)
		}
		if result.AuditID == "" || result.ManifestPath == "" || result.PacketPath == "" || result.InputSummaryPath == "" {
			t.Fatalf("Create(%s) missing artifact paths: %+v", input.Mode, result)
		}
		if _, err := st.GetLocalAuditByAuditID(result.AuditID); err != nil {
			t.Fatalf("local audit row missing for %s: %v", input.Mode, err)
		}
		manifestBytes, err := os.ReadFile(result.ManifestPath)
		if err != nil {
			t.Fatalf("read manifest for %s: %v", input.Mode, err)
		}
		var manifest LocalAuditManifest
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			t.Fatalf("unmarshal manifest for %s: %v", input.Mode, err)
		}
		if !manifest.LocalOnly || manifest.RemoteEvidence["github_pr"] != "not_used" || manifest.RemoteEvidence["github_actions"] != "not_used" {
			t.Fatalf("manifest is not local-only for %s: %+v", input.Mode, manifest.RemoteEvidence)
		}
		if strings.Contains(string(manifestBytes), "diff --git") || strings.Contains(string(manifestBytes), "super-secret-token") {
			t.Fatalf("manifest contains raw diff or secret for %s: %s", input.Mode, string(manifestBytes))
		}
		packetBytes, err := os.ReadFile(result.PacketPath)
		if err != nil {
			t.Fatalf("read packet for %s: %v", input.Mode, err)
		}
		if strings.Contains(string(packetBytes), "super-secret-token") {
			t.Fatalf("packet contains unredacted token for %s: %s", input.Mode, string(packetBytes))
		}
	}
}

func newLocalAuditTestStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func createLocalAuditProject(t *testing.T, st *store.Store, repoRoot string) *store.Project {
	t.Helper()
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
	return project
}

func insertLocalAuditPlan(t *testing.T, st *store.Store) {
	t.Helper()
	res, err := st.DB().ExecContext(t.Context(), `INSERT INTO plans (plan_id, title, goal, status, raw_plan_json) VALUES ('plan-1', 'Plan', 'Goal', 'active', '{}')`)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("plan id: %v", err)
	}
	if _, err := st.DB().ExecContext(t.Context(), `INSERT INTO plan_passes (
		plan_row_id, pass_id, sequence, name, goal, status, context_plan_json,
		source_snapshot_requirements_json, handoff_readiness_criteria_json, risk_level, raw_pass_json
	) VALUES (?, 'PASS-001', 1, 'Pass', 'Goal', 'planned', '{"steps":["audit"]}', '{"required":true}', '["ready"]', 'low', '{}')`, planID); err != nil {
		t.Fatalf("insert pass: %v", err)
	}
}

func requireLocalAuditGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func setupLocalAuditGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runLocalAuditGit(t, root, "init", "-b", "main")
	runLocalAuditGit(t, root, "config", "user.email", "relay-test@example.invalid")
	runLocalAuditGit(t, root, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}
	runLocalAuditGit(t, root, "add", ".")
	runLocalAuditGit(t, root, "commit", "-m", "initial commit")
	return root
}

func runLocalAuditGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
