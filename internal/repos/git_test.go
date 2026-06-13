package repos

import (
	"os"
	"os/exec"
	"testing"
)

func TestCaptureGitSnapshot_EmptyPath(t *testing.T) {
	snap := CaptureGitSnapshot("", "run_created")
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Error == "" {
		t.Fatal("expected error for empty path")
	}
	if snap.IsGitRepo {
		t.Fatal("expected IsGitRepo false for empty path")
	}
}

func TestCaptureGitSnapshot_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()

	snap := CaptureGitSnapshot(dir, "run_created")
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Error == "" {
		t.Fatal("expected error for non-git directory")
	}
	if snap.IsGitRepo {
		t.Fatal("expected IsGitRepo false for non-git directory")
	}
}

func TestCaptureGitSnapshot_ValidRepo(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	snap := CaptureGitSnapshot(root, "run_created")
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Error != "" {
		t.Fatalf("unexpected error: %s", snap.Error)
	}
	if !snap.IsGitRepo {
		t.Fatal("expected IsGitRepo true")
	}
	if snap.HeadSHA == "" {
		t.Fatal("expected non-empty HeadSHA")
	}
	if snap.Branch != "main" {
		t.Fatalf("expected branch 'main', got %q", snap.Branch)
	}
	if snap.CaptureStage != "run_created" {
		t.Fatalf("expected capture_stage 'run_created', got %q", snap.CaptureStage)
	}
	if snap.Dirty {
		t.Fatal("expected not dirty for empty commit")
	}
}

func TestCaptureGitSnapshot_Dirty(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	// Create an untracked file
	if err := os.WriteFile(root+"/untracked.txt", []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	snap := CaptureGitSnapshot(root, "run_created")
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if !snap.Dirty {
		t.Fatal("expected dirty with untracked file")
	}
	if snap.StatusPorcelain == "" {
		t.Fatal("expected non-empty status porcelain")
	}
}

func TestCaptureGitSnapshot_DetachedHead(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	// Get HEAD SHA and detach
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := string(out)
	sha = sha[:len(sha)-1] // trim newline

	// Detach HEAD
	runCmd(t, root, "git", "checkout", "--detach")

	snap := CaptureGitSnapshot(root, "run_created")
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Error != "" {
		t.Fatalf("unexpected error: %s", snap.Error)
	}
	if !snap.IsGitRepo {
		t.Fatal("expected IsGitRepo true")
	}
	if snap.HeadSHA == "" {
		t.Fatal("expected non-empty HeadSHA")
	}
	if snap.HeadSHA[:7] != sha[:7] {
		t.Fatalf("expected HEAD SHA %s, got %s", sha[:7], snap.HeadSHA[:7])
	}
	// Detached HEAD may produce empty branch
	if snap.Branch != "" {
		t.Logf("detached HEAD branch: %q", snap.Branch)
	}
}

func TestCaptureGitSnapshot_AgentStartStage(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	snap := CaptureGitSnapshot(root, "agent_start")
	if snap.CaptureStage != "agent_start" {
		t.Fatalf("expected capture_stage 'agent_start', got %q", snap.CaptureStage)
	}
	if snap.HeadSHA == "" {
		t.Fatal("expected non-empty HeadSHA")
	}
}
