package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"relay/internal/artifacts"
)

func TestCollectAndPersistGitEvidence(t *testing.T) {
	s := setupExecutorTestStore(t)

	// Create a temp workspace directory
	workDir := t.TempDir()

	// Initialize git repo in the temp directory
	runCmd := func(name string, args ...string) error {
		c := exec.Command(name, args...)
		c.Dir = workDir
		return c.Run()
	}

	if err := runCmd("git", "init"); err != nil {
		t.Skip("skipping test: git command not available in test environment")
		return
	}

	// Configure git author for the test repo
	_ = runCmd("git", "config", "user.name", "Test")
	_ = runCmd("git", "config", "user.email", "test@test.com")

	// Create a dummy file and commit it
	filePath := filepath.Join(workDir, "hello.txt")
	os.WriteFile(filePath, []byte("hello world"), 0644)
	_ = runCmd("git", "add", "hello.txt")
	_ = runCmd("git", "commit", "-m", "initial commit")

	// Modify the file to create diff/status
	os.WriteFile(filePath, []byte("hello modified"), 0644)

	// Create a run in the database
	repo, err := s.CreateRepo("test-git-repo", workDir)
	if err != nil {
		t.Fatalf("create repo failed: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Git Run", "approved_for_executor", "model", "model", "main")
	if err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	// Call collectAndPersistGitEvidence
	collectAndPersistGitEvidence(storeExecutionEvidenceSink{store: s}, s, run.ID, workDir)

	// Assert that git status and diff artifacts were created
	if !artifacts.Exists(run.ID, "git_status_text", "git_status.txt") {
		t.Error("expected git_status.txt artifact to exist")
	}
	if !artifacts.Exists(run.ID, "git_diff_patch", "git_diff.patch") {
		t.Error("expected git_diff.patch artifact to exist")
	}

	// Assert artifact records exist in store
	arts, err := s.ListArtifactsByRunKind(run.ID, "git_status_text")
	if err != nil || len(arts) == 0 {
		t.Error("expected git_status_text artifact record in database")
	}

	diffArts, err := s.ListArtifactsByRunKind(run.ID, "git_diff_patch")
	if err != nil || len(diffArts) == 0 {
		t.Error("expected git_diff_patch artifact record in database")
	}
}
