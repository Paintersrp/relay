package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/projects"
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
	if inventory.FreshnessReport.Status != SourceFreshnessStatusFresh || !inventory.FreshnessReport.ReusableForHandoff {
		t.Fatalf("expected fresh inventory report, got %+v", inventory.FreshnessReport)
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
	if result.FreshnessReport.Status != SourceFreshnessStatusFresh || !result.FreshnessReport.ReusableForHandoff {
		t.Fatalf("expected fresh read report, got %+v", result.FreshnessReport)
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

func TestProjectFileOperationsResolveUniqueRepositoryAliases(t *testing.T) {
	requireGit(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{repoID: "Paintersrp/relay"})

	inventory, err := service.ListProjectFiles(t.Context(), FileInventoryInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoIDs:          []string{"relay"},
	})
	if err != nil {
		t.Fatalf("ListProjectFiles alias error: %v", err)
	}
	if len(inventory.Blockers) != 0 || len(inventory.Files) == 0 || inventory.Files[0].RepoID != "Paintersrp/relay" {
		t.Fatalf("expected alias inventory to resolve canonical repo ID, got %+v", inventory)
	}

	read, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/app.txt",
	})
	if err != nil {
		t.Fatalf("ReadProjectFile alias error: %v", err)
	}
	if len(read.Blockers) != 0 || read.RepoID != "Paintersrp/relay" || read.Content == "" {
		t.Fatalf("expected alias read to resolve canonical repo ID, got %+v", read)
	}
}

func TestProjectFileOperationsBlockUnknownRepositoryAlias(t *testing.T) {
	requireGit(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{repoID: "Paintersrp/relay"})

	inventory, err := service.ListProjectFiles(t.Context(), FileInventoryInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoIDs:          []string{"missing"},
	})
	if err != nil {
		t.Fatalf("ListProjectFiles unknown alias error: %v", err)
	}
	if len(inventory.Blockers) != 1 || inventory.Blockers[0].Code != SourceBlockerUnknownRepository {
		t.Fatalf("expected unknown repository blocker, got %+v", inventory.Blockers)
	}
	if len(inventory.Files) != 0 {
		t.Fatalf("expected unknown repository to stop inventory, got %+v", inventory.Files)
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

func TestReadProjectFileBlocksWhenFileChangedAfterSnapshot(t *testing.T) {
	requireGit(t)
	service, snapshotID, repoRoot := setupSourceSnapshotFixtureWithRoot(t, sourceFixtureOptions{})

	original, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/app.txt",
	})
	if err != nil {
		t.Fatalf("ReadProjectFile original error: %v", err)
	}
	if original.ContentHash == "" || original.CurrentHash == "" || original.ContentHash != original.CurrentHash {
		t.Fatalf("expected matching snapshot/current hash before mutation, got %+v", original)
	}

	writeFile(t, filepath.Join(repoRoot, "src", "app.txt"), "line one\nchanged\nline three\n")
	changed, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/app.txt",
	})
	if err != nil {
		t.Fatalf("ReadProjectFile changed error: %v", err)
	}
	if len(changed.Blockers) != 1 || changed.Blockers[0].Code != SourceBlockerSnapshotFileChanged {
		t.Fatalf("expected source_snapshot_file_changed blocker, got %+v", changed.Blockers)
	}
	if changed.Content != "" || changed.SnippetHash != "" {
		t.Fatalf("expected no stale content/snippet hash, got %+v", changed)
	}
	if changed.ContentHash != original.ContentHash {
		t.Fatalf("expected snapshot hash to be preserved, got original=%s changed=%s", original.ContentHash, changed.ContentHash)
	}
	if changed.CurrentHash == "" || changed.CurrentHash == changed.ContentHash {
		t.Fatalf("expected diagnostic current hash to differ, got %+v", changed)
	}
	if !freshnessHasCode(changed.FreshnessReport, SourceBlockerSnapshotFileChanged) {
		t.Fatalf("expected changed file code in freshness blockers, got %+v", changed.FreshnessReport)
	}
	if changed.FreshnessReport.ReusableForHandoff {
		t.Fatalf("expected changed file freshness to be non-reusable, got %+v", changed.FreshnessReport)
	}
}

func TestReadProjectFileMarksUnrelatedRepositoryDrift(t *testing.T) {
	requireGit(t)
	service, snapshotID, repoRoot := setupSourceSnapshotFixtureWithRoot(t, sourceFixtureOptions{})

	writeFile(t, filepath.Join(repoRoot, "src", "unrelated.txt"), "new drift\n")
	result, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/app.txt",
	})
	if err != nil {
		t.Fatalf("ReadProjectFile drift error: %v", err)
	}
	if len(result.Blockers) != 0 || result.Content == "" {
		t.Fatalf("expected unchanged file content without read blockers, got %+v", result)
	}
	if result.FreshnessReport.Status != SourceFreshnessStatusDrifted || result.FreshnessReport.ReusableForHandoff {
		t.Fatalf("expected drifted non-reusable freshness, got %+v", result.FreshnessReport)
	}
	if !freshnessHasCode(result.FreshnessReport, SourceFreshnessCodeDrifted) {
		t.Fatalf("expected drifted freshness code, got %+v", result.FreshnessReport)
	}
}

func TestReadProjectFileStreamsBoundedLineRange(t *testing.T) {
	requireGit(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{longFileLines: defaultReadMaxLines + 5})

	result, err := service.ReadProjectFile(t.Context(), BoundedFileReadInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		RepoID:           "relay",
		Path:             "src/long.txt",
		LineStart:        10,
		LineEnd:          12,
		MaxBytes:         8,
	})
	if err != nil {
		t.Fatalf("ReadProjectFile error: %v", err)
	}
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", result.Blockers)
	}
	if result.LineStart != 10 || result.LineEnd != 12 {
		t.Fatalf("unexpected line range: %+v", result)
	}
	if result.Content != "x\nx\nx\n" || result.Truncated {
		t.Fatalf("expected bounded requested range without truncation, got %+v", result)
	}
}

type sourceFixtureOptions struct {
	withPrivateKey bool
	withBinary     bool
	longFileLines  int
	repoID         string
}

func setupSourceSnapshotFixture(t *testing.T, opts sourceFixtureOptions) (*Service, string) {
	t.Helper()
	service, snapshotID, _ := setupSourceSnapshotFixtureWithRoot(t, opts)
	return service, snapshotID
}

func setupSourceSnapshotFixtureWithRoot(t *testing.T, opts sourceFixtureOptions) (*Service, string, string) {
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
	repoID := opts.repoID
	if repoID == "" {
		repoID = "relay"
	}

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
		RepoID:           repoID,
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
	return service, snapshot.SourceSnapshotID, repoRoot
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
