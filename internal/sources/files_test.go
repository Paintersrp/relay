package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/projects"
)

func TestListProjectFilesUsesSnapshotRowsAndCaps(t *testing.T) {
	requireGit(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{})

	inventory, err := service.ListProjectFiles(t.Context(), FileInventoryInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		MaxResults:       1,
	})
	if err != nil {
		t.Fatalf("ListProjectFiles error: %v", err)
	}
	if len(inventory.Files) != 1 || !inventory.Truncated {
		t.Fatalf("expected one truncated result, got len=%d truncated=%v", len(inventory.Files), inventory.Truncated)
	}
	if inventory.Files[0].ContentHash == "" || inventory.Files[0].Path == "" {
		t.Fatalf("expected provenance fields, got %+v", inventory.Files[0])
	}
	if strings.Contains(inventory.Files[0].Path, "secret") {
		t.Fatalf("expected no content-like data in inventory path: %+v", inventory.Files[0])
	}

	withExcluded, err := service.ListProjectFiles(t.Context(), FileInventoryInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		IncludeExcluded:  true,
	})
	if err != nil {
		t.Fatalf("ListProjectFiles include excluded error: %v", err)
	}
	if !hasInventoryPath(withExcluded, "ignored/secret.txt", false, "ignored_glob") {
		t.Fatalf("expected ignored/secret.txt excluded row, got %+v", withExcluded.Files)
	}
}

func TestListProjectFilesMissingSnapshotReturnsBlocker(t *testing.T) {
	service, _, _ := newSourceTestServices(t)
	result, err := service.ListProjectFiles(t.Context(), FileInventoryInput{ProjectID: "missing"})
	if err == nil {
		t.Fatalf("expected missing project error, got result %+v", result)
	}

	service, projectService, _ := newSourceTestServices(t)
	project, _, err := projectService.CreateProject(t.Context(), projects.ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
		Status:    projects.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	result, err = service.ListProjectFiles(t.Context(), FileInventoryInput{ProjectID: project.ProjectID})
	if err != nil {
		t.Fatalf("ListProjectFiles error: %v", err)
	}
	if len(result.Blockers) != 1 || result.Blockers[0].Code != SourceBlockerSnapshotMissing {
		t.Fatalf("expected source snapshot missing blocker, got %+v", result.Blockers)
	}
}

func TestReadProjectFileExactRangeAndRedaction(t *testing.T) {
	requireGit(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{})

	result, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/app.txt",
		LineStart:        2,
		LineEnd:          2,
	})
	if err != nil {
		t.Fatalf("ReadProjectFile error: %v", err)
	}
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", result.Blockers)
	}
	if result.Content != "line two\n" || result.LineStart != 2 || result.LineEnd != 2 {
		t.Fatalf("unexpected read result: %+v", result)
	}

	secret, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/token.txt",
	})
	if err != nil {
		t.Fatalf("ReadProjectFile token error: %v", err)
	}
	if secret.RedactionStatus != RedactionStatusRedacted || strings.Contains(secret.Content, "super-secret-token") {
		t.Fatalf("expected redacted token content, got %+v", secret)
	}
	if !strings.Contains(secret.Content, "[REDACTED_TOKEN]") {
		t.Fatalf("expected token marker, got %q", secret.Content)
	}
}

func TestReadProjectFileBlocksUnsafeExcludedBinaryOversizedAndPrivateKey(t *testing.T) {
	requireGit(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{withPrivateKey: true, withBinary: true})

	cases := []struct {
		name string
		path string
		code string
	}{
		{name: "unsafe", path: "../committed.txt", code: SourceBlockerUnsafePath},
		{name: "excluded", path: "ignored/secret.txt", code: SourceBlockerExcludedPath},
		{name: "binary", path: "src/blob.bin", code: SourceBlockerBinary},
		{name: "private key", path: "src/private.txt", code: SourceBlockerRedactionBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
				ProjectID:        "relay",
				SourceSnapshotID: snapshotID,
				RepoID:           "relay",
				Path:             tc.path,
			})
			if err != nil {
				t.Fatalf("ReadProjectFile error: %v", err)
			}
			if len(result.Blockers) != 1 || result.Blockers[0].Code != tc.code {
				t.Fatalf("expected blocker %s, got %+v", tc.code, result.Blockers)
			}
			if result.Content != "" {
				t.Fatalf("expected no blocked content, got %q", result.Content)
			}
		})
	}

	oversized, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "big/artifact.txt",
	})
	if err != nil {
		t.Fatalf("ReadProjectFile oversized error: %v", err)
	}
	if len(oversized.Blockers) != 1 || oversized.Blockers[0].Code != SourceBlockerExcludedPath {
		t.Fatalf("expected excluded oversized snapshot row, got %+v", oversized.Blockers)
	}
}

func TestReadProjectFileDefaultLineCapTruncates(t *testing.T) {
	requireGit(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{longFileLines: defaultReadMaxLines + 5})

	result, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/long.txt",
	})
	if err != nil {
		t.Fatalf("ReadProjectFile error: %v", err)
	}
	if !result.Truncated || result.LineEnd != defaultReadMaxLines {
		t.Fatalf("expected default line cap truncation, got %+v", result)
	}
}

type sourceFixtureOptions struct {
	withPrivateKey bool
	withBinary     bool
	longFileLines  int
}

func setupSourceSnapshotFixture(t *testing.T, opts sourceFixtureOptions) (*Service, string) {
	t.Helper()
	service, projectService, _ := newSourceTestServices(t)
	repoRoot := setupGitRepo(t)
	mkdirAll(t, filepath.Join(repoRoot, "src"))
	mkdirAll(t, filepath.Join(repoRoot, "ignored"))
	mkdirAll(t, filepath.Join(repoRoot, "big"))
	writeFile(t, filepath.Join(repoRoot, "src", "app.txt"), "line one\nline two\nline three\n")
	writeFile(t, filepath.Join(repoRoot, "src", "token.txt"), "token: super-secret-token\n")
	writeFile(t, filepath.Join(repoRoot, "ignored", "secret.txt"), "secret\n")
	writeFile(t, filepath.Join(repoRoot, "big", "artifact.txt"), strings.Repeat("x", int(projects.MinMaxFileSizeBytes)+1))
	if opts.withPrivateKey {
		writeFile(t, filepath.Join(repoRoot, "src", "private.txt"), "-----BEGIN PRIVATE KEY-----\nabc\n")
	}
	if opts.withBinary {
		if err := os.WriteFile(filepath.Join(repoRoot, "src", "blob.bin"), []byte{1, 2, 0, 4}, 0644); err != nil {
			t.Fatalf("WriteFile blob.bin: %v", err)
		}
	}
	if opts.longFileLines > 0 {
		var builder strings.Builder
		for i := 0; i < opts.longFileLines; i++ {
			builder.WriteString("x\n")
		}
		writeFile(t, filepath.Join(repoRoot, "src", "long.txt"), builder.String())
	}
	runGit(t, repoRoot, "git", "add", ".")
	runGit(t, repoRoot, "git", "commit", "-m", "source fixture")

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
		AllowedRoots:     []string{"src", "ignored", "big"},
		IgnoredGlobs:     []string{"ignored/**"},
		MaxFileSizeBytes: projects.MinMaxFileSizeBytes,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("UpsertProjectRepository error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no repo issues, got %+v", issues)
	}
	snapshot, err := service.CreateSourceSnapshot(t.Context(), SourceSnapshotInput{
		ProjectID:           project.ProjectID,
		IncludeFileMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}
	return service, snapshot.SourceSnapshotID
}

func hasInventoryPath(result *FileInventoryResult, path string, included bool, reason string) bool {
	for _, file := range result.Files {
		if file.Path == path && file.Included == included && file.ExclusionReason == reason {
			return true
		}
	}
	return false
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}
