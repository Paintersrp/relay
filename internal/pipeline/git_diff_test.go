package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	cmd.CombinedOutput()
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = dir
	cmd.CombinedOutput()
}

func gitAddCommit(t *testing.T, dir string, msg string) {
	t.Helper()
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", msg)
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func TestCollectGitDiffEvidenceNoChanges(t *testing.T) {
	dir, err := os.MkdirTemp("", "relay-git-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	initGitRepo(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitAddCommit(t, dir, "initial commit")

	ctx := context.Background()
	ev, err := CollectGitDiffEvidence(ctx, dir, 30*time.Second)
	if err != nil {
		t.Fatalf("CollectGitDiffEvidence: %v", err)
	}
	if ev.HasChanges {
		t.Errorf("expected HasChanges=false for clean repo, got true")
	}
	if ev.StatusText != "" {
		t.Errorf("expected empty status, got %q", ev.StatusText)
	}
	if ev.DiffStat != "" {
		t.Errorf("expected empty diff stat, got %q", ev.DiffStat)
	}
	if ev.DiffPatch != "" {
		t.Errorf("expected empty diff patch, got %q", ev.DiffPatch)
	}
}

func TestCollectGitDiffEvidenceModifiedTrackedFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "relay-git-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	initGitRepo(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitAddCommit(t, dir, "initial commit")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified\n\nChanged content.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ev, err := CollectGitDiffEvidence(ctx, dir, 30*time.Second)
	if err != nil {
		t.Fatalf("CollectGitDiffEvidence: %v", err)
	}
	if !ev.HasChanges {
		t.Errorf("expected HasChanges=true for modified repo, got false")
	}
	if !strings.Contains(ev.StatusText, "README.md") {
		t.Errorf("expected status to mention README.md, got %q", ev.StatusText)
	}
	if !strings.Contains(ev.DiffPatch, "Modified") {
		t.Errorf("expected diff patch to contain 'Modified', got %q", ev.DiffPatch)
	}
}

func TestCollectGitDiffEvidenceUntrackedFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "relay-git-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	initGitRepo(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitAddCommit(t, dir, "initial commit")

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ev, err := CollectGitDiffEvidence(ctx, dir, 30*time.Second)
	if err != nil {
		t.Fatalf("CollectGitDiffEvidence: %v", err)
	}
	if !ev.HasChanges {
		t.Errorf("expected HasChanges=true with untracked file, got false")
	}
	if !strings.Contains(ev.StatusText, "untracked.txt") {
		t.Errorf("expected status to mention untracked.txt, got %q", ev.StatusText)
	}
}
