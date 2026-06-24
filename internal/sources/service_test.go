package sources

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/projects"
	"relay/internal/store"
)

func TestCreateSourceSnapshotRecordsRepositoryAndFiles(t *testing.T) {
	requireGit(t)
	service, projectService, st := newSourceTestServices(t)

	repoRoot := setupGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repoRoot, "src"), 0755); err != nil {
		t.Fatalf("MkdirAll src: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "ignored"), 0755); err != nil {
		t.Fatalf("MkdirAll ignored: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "big"), 0755); err != nil {
		t.Fatalf("MkdirAll big: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0755); err != nil {
		t.Fatalf("MkdirAll docs: %v", err)
	}

	writeFile(t, filepath.Join(repoRoot, "src", "app.txt"), "app\n")
	writeFile(t, filepath.Join(repoRoot, "ignored", "secret.txt"), "secret\n")
	writeFile(t, filepath.Join(repoRoot, "big", "artifact.bin"), strings.Repeat("0123456789", 130))
	writeFile(t, filepath.Join(repoRoot, "docs", "outside.txt"), "outside\n")
	runGit(t, repoRoot, "git", "add", ".")
	runGit(t, repoRoot, "git", "commit", "-m", "seed files")

	writeFile(t, filepath.Join(repoRoot, "notes.txt"), "untracked\n")

	project, issues, err := projectService.CreateProject(t.Context(), projects.ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
		Status:    projects.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no project issues, got %+v", issues)
	}

	_, issues, err = projectService.UpsertProjectRepository(t.Context(), project.ProjectID, projects.ProjectRepositoryInput{
		RepoID:           "relay",
		Role:             projects.RepositoryRolePrimary,
		LocalPath:        repoRoot,
		DefaultBranch:    "main",
		AllowedRoots:     []string{"src", "ignored", "big", "notes.txt"},
		IgnoredGlobs:     []string{"ignored/**"},
		MaxFileSizeBytes: projects.MinMaxFileSizeBytes,
		IncludeUntracked: true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("UpsertProjectRepository error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no repo issues, got %+v", issues)
	}

	result, err := service.CreateSourceSnapshot(t.Context(), SourceSnapshotInput{
		ProjectID:           project.ProjectID,
		IncludeFileMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}
	if result.Status != SnapshotStatusCreated {
		t.Fatalf("expected snapshot status created, got %q", result.Status)
	}
	if result.SnapshotKind != SnapshotKindDirtyWorktree {
		t.Fatalf("expected snapshot kind dirty_worktree, got %q", result.SnapshotKind)
	}
	if len(result.Repositories) != 1 {
		t.Fatalf("expected 1 repository result, got %d", len(result.Repositories))
	}
	if result.Repositories[0].GitStatus.UntrackedCount != 1 {
		t.Fatalf("expected 1 untracked file, got %d", result.Repositories[0].GitStatus.UntrackedCount)
	}

	snapshot, err := st.GetSourceSnapshotByID(result.SourceSnapshotID)
	if err != nil {
		t.Fatalf("GetSourceSnapshotByID error: %v", err)
	}
	if snapshot.Status != SnapshotStatusCreated {
		t.Fatalf("expected stored snapshot status created, got %q", snapshot.Status)
	}

	repoRows, err := st.ListSourceSnapshotRepositories(snapshot.ID)
	if err != nil {
		t.Fatalf("ListSourceSnapshotRepositories error: %v", err)
	}
	if len(repoRows) != 1 {
		t.Fatalf("expected 1 repo row, got %d", len(repoRows))
	}
	if repoRows[0].GitStatusAvailable != 1 {
		t.Fatal("expected git status to be available")
	}

	files, err := st.ListSourceSnapshotFiles(repoRows[0].ID)
	if err != nil {
		t.Fatalf("ListSourceSnapshotFiles error: %v", err)
	}
	byPath := make(map[string]store.SourceSnapshotFile, len(files))
	for _, file := range files {
		byPath[file.Path] = file
	}
	if byPath["src/app.txt"].Included != 1 || byPath["src/app.txt"].ContentHash == "" {
		t.Fatalf("expected src/app.txt to be included with a hash, got %+v", byPath["src/app.txt"])
	}
	if byPath["ignored/secret.txt"].ExclusionReason != "ignored_glob" {
		t.Fatalf("expected ignored/secret.txt to be excluded by ignored_glob, got %+v", byPath["ignored/secret.txt"])
	}
	if byPath["big/artifact.bin"].ExclusionReason != "max_file_size_exceeded" {
		t.Fatalf("expected big/artifact.bin to be excluded by size, got %+v", byPath["big/artifact.bin"])
	}
	if byPath["docs/outside.txt"].ExclusionReason != "outside_allowed_roots" {
		t.Fatalf("expected docs/outside.txt to be excluded by allowed roots, got %+v", byPath["docs/outside.txt"])
	}
	if byPath["notes.txt"].Tracked != 0 || byPath["notes.txt"].Included != 1 {
		t.Fatalf("expected untracked notes.txt to be recorded and included, got %+v", byPath["notes.txt"])
	}
}

func TestCreateSourceSnapshotPartialWhenOneRepoUnavailable(t *testing.T) {
	requireGit(t)
	service, projectService, _ := newSourceTestServices(t)

	gitRepo := setupGitRepo(t)
	plainDir := t.TempDir()

	project, _, err := projectService.CreateProject(t.Context(), projects.ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
		Status:    projects.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	for _, repository := range []projects.ProjectRepositoryInput{
		{
			RepoID:           "relay",
			Role:             projects.RepositoryRolePrimary,
			LocalPath:        gitRepo,
			DefaultBranch:    "main",
			MaxFileSizeBytes: projects.DefaultMaxFileSizeBytes,
			Enabled:          true,
		},
		{
			RepoID:           "plain",
			Role:             projects.RepositoryRoleReference,
			LocalPath:        plainDir,
			DefaultBranch:    "main",
			MaxFileSizeBytes: projects.DefaultMaxFileSizeBytes,
			Enabled:          true,
		},
	} {
		if _, issues, err := projectService.UpsertProjectRepository(t.Context(), project.ProjectID, repository); err != nil {
			t.Fatalf("UpsertProjectRepository(%s) error: %v", repository.RepoID, err)
		} else if len(issues) != 0 {
			t.Fatalf("expected no issues for %s, got %+v", repository.RepoID, issues)
		}
	}

	result, err := service.CreateSourceSnapshot(t.Context(), SourceSnapshotInput{ProjectID: project.ProjectID})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}
	if result.Status != SnapshotStatusPartial {
		t.Fatalf("expected snapshot status partial, got %q", result.Status)
	}
	if result.SnapshotKind != SnapshotKindMixed {
		t.Fatalf("expected snapshot kind mixed, got %q", result.SnapshotKind)
	}
	if len(result.Blockers) == 0 {
		t.Fatal("expected at least one blocker for unavailable repo")
	}
}

func TestCreateSourceSnapshotBlockedWhenAllReposUnavailable(t *testing.T) {
	service, projectService, _ := newSourceTestServices(t)
	plainDir := t.TempDir()

	project, _, err := projectService.CreateProject(t.Context(), projects.ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
		Status:    projects.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	if _, issues, err := projectService.UpsertProjectRepository(t.Context(), project.ProjectID, projects.ProjectRepositoryInput{
		RepoID:           "plain",
		Role:             projects.RepositoryRolePrimary,
		LocalPath:        plainDir,
		DefaultBranch:    "main",
		MaxFileSizeBytes: projects.DefaultMaxFileSizeBytes,
		Enabled:          true,
	}); err != nil {
		t.Fatalf("UpsertProjectRepository error: %v", err)
	} else if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}

	result, err := service.CreateSourceSnapshot(t.Context(), SourceSnapshotInput{ProjectID: project.ProjectID})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}
	if result.Status != SnapshotStatusBlocked {
		t.Fatalf("expected snapshot status blocked, got %q", result.Status)
	}
	if result.SnapshotKind != SnapshotKindUnavailable {
		t.Fatalf("expected snapshot kind unavailable, got %q", result.SnapshotKind)
	}
}

func newSourceTestServices(t *testing.T) (*Service, *projects.Service, *store.Store) {
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

	return NewService(st), projects.NewService(st), st
}

func writeFile(t *testing.T, filePath string, content string) {
	t.Helper()
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s) error: %v", filePath, err)
	}
}
