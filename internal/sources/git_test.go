package sources

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/store"
)

func TestIsAllowedGitCommandRejectsMutation(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"checkout", "main"},
		{"reset", "--hard"},
		{"push", "origin", "main"},
		{"commit", "-m", "bad"},
		{"merge", "main"},
		{"rebase", "main"},
		{"tag", "v1.0.0"},
		{"add", "."},
		{"restore", "."},
		{"clean", "-fd"},
	} {
		if isAllowedGitCommand(args) {
			t.Fatalf("expected git %s to be rejected", strings.Join(args, " "))
		}
	}
}

func TestIsAllowedGitCommandAllowsReadOnlyEvidenceCommands(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"rev-parse", "--abbrev-ref", "HEAD"},
		{"rev-parse", "HEAD"},
		{"status", "--porcelain=v1", "-z"},
		{"ls-files", "-z"},
		{"show", "-s", "--format=%H%x00%an%x00%ae%x00%aI%x00%s", "HEAD"},
		{"diff", "--name-status", "--no-ext-diff", "-z"},
		{"diff", "--cached", "--name-status", "--no-ext-diff", "-z"},
		{"diff", "--no-ext-diff", "--unified=3", "--"},
		{"diff", "--cached", "--no-ext-diff", "--unified=3", "--"},
		{"show", "--name-status", "--format=", "--no-ext-diff", "-z", "HEAD"},
		{"show", "--format=", "--no-ext-diff", "--unified=3", "HEAD", "--"},
	} {
		if !isAllowedGitCommand(args) {
			t.Fatalf("expected git %s to be allowed", strings.Join(args, " "))
		}
	}
}

func TestIsAllowedGitCommandRejectsRecentCommitVariants(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"show", "HEAD"},
		{"show", "--name-status", "--format=", "--no-ext-diff", "-z", "HEAD~1"},
		{"show", "--format=", "--no-ext-diff", "--unified=3", "abc123", "--"},
		{"show", "--format=", "--no-ext-diff", "--unified=3", "HEAD", "--", "internal"},
		{"diff", "internal/foo.go"},
		{"branch"},
	} {
		if isAllowedGitCommand(args) {
			t.Fatalf("expected git %s to be rejected", strings.Join(args, " "))
		}
	}
}

func TestGetRepositoryGitStatusCountsChanges(t *testing.T) {
	requireGit(t)
	root := setupGitRepo(t)
	service := NewService(nil)

	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked\n"), 0644); err != nil {
		t.Fatalf("WriteFile tracked: %v", err)
	}
	runGit(t, root, "git", "add", "tracked.txt")

	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte("updated\n"), 0644); err != nil {
		t.Fatalf("WriteFile committed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatalf("WriteFile new: %v", err)
	}

	status, err := service.GetRepositoryGitStatus(t.Context(), store.ProjectRepository{
		RepoID:    "relay",
		LocalPath: root,
	})
	if err != nil {
		t.Fatalf("GetRepositoryGitStatus error: %v", err)
	}
	if !status.GitStatusAvailable {
		t.Fatal("expected git status to be available")
	}
	if !status.Dirty {
		t.Fatal("expected dirty status")
	}
	if status.StagedCount != 1 {
		t.Fatalf("expected 1 staged change, got %d", status.StagedCount)
	}
	if status.UnstagedCount != 1 {
		t.Fatalf("expected 1 unstaged change, got %d", status.UnstagedCount)
	}
	if status.UntrackedCount != 1 {
		t.Fatalf("expected 1 untracked change, got %d", status.UntrackedCount)
	}
}

func TestGetRecentCommitParsesHeadCommit(t *testing.T) {
	requireGit(t)
	root := setupGitRepo(t)
	service := NewService(nil)

	commit, err := service.GetRecentCommit(t.Context(), store.ProjectRepository{
		RepoID:    "relay",
		LocalPath: root,
	})
	if err != nil {
		t.Fatalf("GetRecentCommit error: %v", err)
	}
	if commit.CommitSHA == "" {
		t.Fatal("expected commit sha")
	}
	if commit.Subject != "initial commit" {
		t.Fatalf("expected subject initial commit, got %q", commit.Subject)
	}
	if commit.AuthorEmail != "relay-test@example.invalid" {
		t.Fatalf("expected author email relay-test@example.invalid, got %q", commit.AuthorEmail)
	}
}

func TestGetChangedFilesByMode(t *testing.T) {
	requireGit(t)
	root := setupGitRepo(t)
	service := NewService(nil)

	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("staged\n"), 0644); err != nil {
		t.Fatalf("WriteFile staged: %v", err)
	}
	runGit(t, root, "git", "add", "staged.txt")

	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte("updated\n"), 0644); err != nil {
		t.Fatalf("WriteFile committed: %v", err)
	}

	stagedFiles, err := service.GetChangedFiles(t.Context(), store.ProjectRepository{RepoID: "relay", LocalPath: root}, DiffModeStaged)
	if err != nil {
		t.Fatalf("GetChangedFiles staged error: %v", err)
	}
	if len(stagedFiles) != 1 || stagedFiles[0].Path != "staged.txt" || stagedFiles[0].Status != "A" {
		t.Fatalf("unexpected staged files: %+v", stagedFiles)
	}

	worktreeFiles, err := service.GetChangedFiles(t.Context(), store.ProjectRepository{RepoID: "relay", LocalPath: root}, DiffModeWorktree)
	if err != nil {
		t.Fatalf("GetChangedFiles worktree error: %v", err)
	}
	if len(worktreeFiles) != 1 || worktreeFiles[0].Path != "committed.txt" || worktreeFiles[0].Status != "M" {
		t.Fatalf("unexpected worktree files: %+v", worktreeFiles)
	}
}

func TestGetBoundedDiffRedactsAndTruncates(t *testing.T) {
	requireGit(t)
	root := setupGitRepo(t)
	service := NewService(nil)

	secretContent := strings.Repeat("Authorization: Bearer super-secret-token\n", 64)
	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte(secretContent), 0644); err != nil {
		t.Fatalf("WriteFile committed: %v", err)
	}

	diff, err := service.GetBoundedDiff(t.Context(), store.ProjectRepository{RepoID: "relay", LocalPath: root}, DiffModeWorktree, 256, 3)
	if err != nil {
		t.Fatalf("GetBoundedDiff error: %v", err)
	}
	if !diff.Truncated {
		t.Fatal("expected diff to be truncated")
	}
	if strings.Contains(diff.Content, "super-secret-token") {
		t.Fatalf("expected diff content to be redacted, got %q", diff.Content)
	}
	if diff.RedactionStatus != RedactionStatusRedacted {
		t.Fatalf("expected redaction status redacted, got %q", diff.RedactionStatus)
	}
}

func TestGetRecentCommitEvidence(t *testing.T) {
	requireGit(t)
	root := setupGitRepo(t)
	service := NewService(nil)

	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte("updated\n"), 0644); err != nil {
		t.Fatalf("WriteFile committed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("Authorization: Bearer super-secret-token\n"), 0644); err != nil {
		t.Fatalf("WriteFile added: %v", err)
	}
	runGit(t, root, "git", "add", ".")
	runGit(t, root, "git", "commit", "-m", "second commit")

	files, err := service.GetRecentCommitChangedFiles(t.Context(), store.ProjectRepository{RepoID: "relay", LocalPath: root})
	if err != nil {
		t.Fatalf("GetRecentCommitChangedFiles error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected two changed files, got %+v", files)
	}

	diff, err := service.GetRecentCommitBoundedDiff(t.Context(), store.ProjectRepository{RepoID: "relay", LocalPath: root}, 4096, 3)
	if err != nil {
		t.Fatalf("GetRecentCommitBoundedDiff error: %v", err)
	}
	if diff.Mode != DiffModeRecentCommit || diff.ContentHash == "" {
		t.Fatalf("unexpected recent commit diff metadata: %+v", diff)
	}
	if strings.Contains(diff.Content, "super-secret-token") {
		t.Fatalf("expected recent commit diff to be redacted, got %q", diff.Content)
	}
	if diff.RedactionStatus != RedactionStatusRedacted {
		t.Fatalf("expected redacted status, got %q", diff.RedactionStatus)
	}
}

func TestGetBoundedDiffBlocksPrivateKey(t *testing.T) {
	requireGit(t)
	root := setupGitRepo(t)
	service := NewService(nil)

	privateKey := "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"
	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte(privateKey), 0644); err != nil {
		t.Fatalf("WriteFile committed: %v", err)
	}

	diff, err := service.GetBoundedDiff(t.Context(), store.ProjectRepository{RepoID: "relay", LocalPath: root}, DiffModeWorktree, 4096, 3)
	if err != nil {
		t.Fatalf("GetBoundedDiff error: %v", err)
	}
	if diff.RedactionStatus != RedactionStatusBlocked {
		t.Fatalf("expected blocked redaction status, got %q", diff.RedactionStatus)
	}
	if diff.Content != "" {
		t.Fatalf("expected blocked diff content to be empty, got %q", diff.Content)
	}
}

func TestGetBoundedDiffBlocksTruncatedPrivateKeyStart(t *testing.T) {
	requireGit(t)
	root := setupGitRepo(t)
	service := NewService(nil)

	privateKeyStart := "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n"
	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte(privateKeyStart), 0644); err != nil {
		t.Fatalf("WriteFile committed: %v", err)
	}

	diff, err := service.GetBoundedDiff(t.Context(), store.ProjectRepository{RepoID: "relay", LocalPath: root}, DiffModeWorktree, 4096, 3)
	if err != nil {
		t.Fatalf("GetBoundedDiff error: %v", err)
	}
	if diff.RedactionStatus != RedactionStatusBlocked {
		t.Fatalf("expected blocked redaction status, got %q", diff.RedactionStatus)
	}
	if diff.Content != "" {
		t.Fatalf("expected blocked diff content to be empty, got %q", diff.Content)
	}
}

func TestRedactSourceContentUsesPolicySpecificMarkers(t *testing.T) {
	content := "Authorization: Bearer secret\napi_key = abc123\ntoken: xyz\npassword: open-sesame\n"
	redacted, status := redactSourceContent(content)
	if status != RedactionStatusRedacted {
		t.Fatalf("expected redacted status, got %q", status)
	}
	for _, forbidden := range []string{"secret", "abc123", "xyz", "open-sesame", "[REDACTED]\n"} {
		if strings.Contains(redacted, forbidden) {
			t.Fatalf("expected %q to be absent from %q", forbidden, redacted)
		}
	}
	for _, marker := range []string{"[REDACTED_AUTH_HEADER]", "[REDACTED_TOKEN]", "[REDACTED_SECRET]"} {
		if !strings.Contains(redacted, marker) {
			t.Fatalf("expected marker %s in %q", marker, redacted)
		}
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "git", "init", "-b", "main")
	runGit(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runGit(t, root, "git", "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(root, "committed.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatalf("WriteFile initial: %v", err)
	}
	runGit(t, root, "git", "add", ".")
	runGit(t, root, "git", "commit", "-m", "initial commit")
	return root
}

func runGit(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, string(output))
	}
}
