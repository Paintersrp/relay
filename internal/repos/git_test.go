package repos

import (
	"os"
	"os/exec"
	"strings"
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

func TestCaptureGitChangeEvidence_NoChanges(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	// Get baseline SHA
	baseline := getHeadSHA(t, root)

	ev := CaptureGitChangeEvidence(root, baseline)
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeNoChanges {
		t.Fatalf("expected mode %q, got %q", EvidenceModeNoChanges, ev.Mode)
	}
	if ev.Dirty {
		t.Fatal("expected not dirty")
	}
	if ev.CommitCount != 0 {
		t.Fatalf("expected 0 commits, got %d", ev.CommitCount)
	}
}

func TestCaptureGitChangeEvidence_UncommittedWorktree(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	baseline := getHeadSHA(t, root)

	// Make a dirty change
	if err := os.WriteFile(root+"/file.txt", []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}

	ev := CaptureGitChangeEvidence(root, baseline)
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeUncommittedWorktree {
		t.Fatalf("expected mode %q, got %q", EvidenceModeUncommittedWorktree, ev.Mode)
	}
	if !ev.Dirty {
		t.Fatal("expected dirty")
	}
	if ev.CurrentHeadSHA != baseline {
		t.Fatal("expected HEAD to equal baseline")
	}
}

func TestCaptureGitChangeEvidence_CommittedRange(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")

	// Initial commit
	if err := os.WriteFile(root+"/initial.txt", []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "initial commit")

	baseline := getHeadSHA(t, root)

	// Second commit
	if err := os.WriteFile(root+"/feature.txt", []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "add feature")

	ev := CaptureGitChangeEvidence(root, baseline)
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeCommittedRange {
		t.Fatalf("expected mode %q, got %q", EvidenceModeCommittedRange, ev.Mode)
	}
	if ev.Dirty {
		t.Fatal("expected not dirty")
	}
	if ev.CurrentHeadSHA == baseline {
		t.Fatal("expected HEAD to differ from baseline")
	}
	if ev.CommitCount != 1 {
		t.Fatalf("expected 1 commit, got %d", ev.CommitCount)
	}
	if len(ev.Commits) != 1 {
		t.Fatalf("expected 1 commit summary, got %d", len(ev.Commits))
	}
	if ev.Commits[0].Subject != "add feature" {
		t.Fatalf("expected subject 'add feature', got %q", ev.Commits[0].Subject)
	}
	if ev.NameStatus == "" {
		t.Fatal("expected non-empty name status for committed range")
	}
	if ev.Stat == "" {
		t.Fatal("expected non-empty diff stat for committed range")
	}
	if ev.Patch == "" {
		t.Fatal("expected non-empty diff patch for committed range")
	}
}

func TestCaptureGitChangeEvidence_MixedCommittedAndUncommitted(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")

	// Initial commit
	if err := os.WriteFile(root+"/initial.txt", []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "initial commit")

	baseline := getHeadSHA(t, root)

	// Second commit (committed)
	if err := os.WriteFile(root+"/feature.txt", []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "add feature")

	// Uncommitted dirty change
	if err := os.WriteFile(root+"/initial.txt", []byte("initial\nmodified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ev := CaptureGitChangeEvidence(root, baseline)
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeMixedCommittedUncommitted {
		t.Fatalf("expected mode %q, got %q", EvidenceModeMixedCommittedUncommitted, ev.Mode)
	}
	if !ev.Dirty {
		t.Fatal("expected dirty")
	}
	if ev.CommitCount < 1 {
		t.Fatal("expected at least 1 commit")
	}
}

func TestCaptureGitChangeEvidence_BaselineUnavailableDirty(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	// Make dirty change
	if err := os.WriteFile(root+"/dirty.txt", []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ev := CaptureGitChangeEvidence(root, "")
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeBaselineUnavailableDirty {
		t.Fatalf("expected mode %q, got %q", EvidenceModeBaselineUnavailableDirty, ev.Mode)
	}
}

func TestCaptureGitChangeEvidence_BaselineUnavailableClean(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	ev := CaptureGitChangeEvidence(root, "")
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeBaselineUnavailableClean {
		t.Fatalf("expected mode %q, got %q", EvidenceModeBaselineUnavailableClean, ev.Mode)
	}
}

// getHeadSHA returns the current HEAD SHA for the given repo.
func getHeadSHA(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("get HEAD SHA: %v", err)
	}
	return strings.TrimSpace(string(out))
}
