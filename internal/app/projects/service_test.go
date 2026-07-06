package projects

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"relay/internal/store"
)

func TestServiceCreateProjectAndUpsertRepository(t *testing.T) {
	t.Parallel()

	svc, st := newProjectTestService(t)

	project, issues, err := svc.CreateProject(t.Context(), ProjectInput{
		ProjectID:   "relay",
		Name:        "Relay",
		Description: "Local-first handoff orchestration",
		Status:      ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no project issues, got %+v", issues)
	}

	repo, issues, err := svc.UpsertProjectRepository(t.Context(), project.ProjectID, ProjectRepositoryInput{
		RepoID:           "relay",
		Role:             RepositoryRolePrimary,
		LocalPath:        filepath.Join(`D:\Code`, "relay"),
		RemoteLabel:      "origin",
		RemoteURL:        "https://example.invalid/Paintersrp/relay",
		DefaultBranch:    "main",
		AllowedRoots:     []string{"internal", "docs/specs"},
		IgnoredGlobs:     []string{"node_modules/**"},
		MaxFileSizeBytes: DefaultMaxFileSizeBytes,
		IncludeUntracked: true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("UpsertProjectRepository error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no repo issues, got %+v", issues)
	}
	if repo.Role != RepositoryRolePrimary {
		t.Fatalf("expected role %q, got %q", RepositoryRolePrimary, repo.Role)
	}
	if repo.IncludeUntracked != 1 {
		t.Fatalf("expected include_untracked=1, got %d", repo.IncludeUntracked)
	}

	repos, err := svc.ListProjectRepositories(t.Context(), project.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectRepositories error: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos))
	}

	storedProject, err := st.GetProjectByProjectID(project.ProjectID)
	if err != nil {
		t.Fatalf("GetProjectByProjectID error: %v", err)
	}
	if storedProject.Name != "Relay" {
		t.Fatalf("expected project name Relay, got %q", storedProject.Name)
	}
}

func TestListEnabledProjectRepositoriesExcludesDisabled(t *testing.T) {
	t.Parallel()

	svc, st := newProjectTestService(t)

	project, issues, err := svc.CreateProject(t.Context(), ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
		Status:    ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}

	for _, repoID := range []string{"relay", "relay-specs"} {
		_, issues, err := svc.UpsertProjectRepository(t.Context(), project.ProjectID, ProjectRepositoryInput{
			RepoID:           repoID,
			Role:             RepositoryRolePrimary,
			LocalPath:        filepath.Join(`D:\Code`, repoID),
			MaxFileSizeBytes: DefaultMaxFileSizeBytes,
			Enabled:          true,
		})
		if err != nil {
			t.Fatalf("UpsertProjectRepository(%s) error: %v", repoID, err)
		}
		if len(issues) != 0 {
			t.Fatalf("expected no issues for %s, got %+v", repoID, issues)
		}
	}

	if _, err := svc.SetProjectRepositoryEnabled(t.Context(), project.ProjectID, "relay-specs", false); err != nil {
		t.Fatalf("SetProjectRepositoryEnabled error: %v", err)
	}

	projectRow, err := st.GetProjectByProjectID(project.ProjectID)
	if err != nil {
		t.Fatalf("GetProjectByProjectID error: %v", err)
	}
	enabledRepos, err := st.ListEnabledProjectRepositories(projectRow.ID)
	if err != nil {
		t.Fatalf("ListEnabledProjectRepositories error: %v", err)
	}
	if len(enabledRepos) != 1 {
		t.Fatalf("expected 1 enabled repository, got %d", len(enabledRepos))
	}
	if enabledRepos[0].RepoID != "relay" {
		t.Fatalf("expected relay to stay enabled, got %q", enabledRepos[0].RepoID)
	}
}

func newProjectTestService(t *testing.T) (*Service, *store.Store) {
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

	return NewService(st), st
}
