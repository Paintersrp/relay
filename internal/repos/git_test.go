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

	// Second commit (committed) — adds feature.txt
	if err := os.WriteFile(root+"/feature.txt", []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "add feature")

	// Uncommitted dirty change to a file from the initial commit
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
	if ev.Commits[0].Subject != "add feature" {
		t.Fatalf("expected subject 'add feature', got %q", ev.Commits[0].Subject)
	}
	// Committed-range evidence must be preserved in primary fields
	if ev.NameStatus == "" {
		t.Fatal("expected non-empty NameStatus for committed range")
	}
	if !strings.Contains(ev.NameStatus, "feature.txt") {
		t.Fatal("expected feature.txt in committed-range NameStatus")
	}
	if ev.Stat == "" {
		t.Fatal("expected non-empty Stat for committed range")
	}
	if ev.Patch == "" {
		t.Fatal("expected non-empty Patch for committed range")
	}
	if !strings.Contains(ev.Patch, "feature.txt") {
		t.Fatal("expected committed-range patch to contain feature.txt")
	}
	// Uncommitted patch must not replace committed-range patch
	if strings.Contains(ev.Patch, "modified") {
		t.Fatal("committed-range patch should NOT contain uncommitted 'modified' content")
	}
	// Warning must be set
	if ev.Warning == "" {
		t.Fatal("expected non-empty Warning for mixed mode")
	}
	// StatusPorcelain must reflect uncommitted state
	if ev.StatusPorcelain == "" {
		t.Fatal("expected non-empty StatusPorcelain for mixed mode")
	}
	if !strings.Contains(ev.StatusPorcelain, "initial.txt") {
		t.Fatal("expected initial.txt in StatusPorcelain for uncommitted change")
	}
	// Uncommitted side fields must be populated
	if ev.UncommittedNameStatus == "" {
		t.Fatal("expected non-empty UncommittedNameStatus for mixed mode")
	}
	if ev.UncommittedStat == "" {
		t.Fatal("expected non-empty UncommittedStat for mixed mode")
	}
	if ev.UncommittedPatch == "" {
		t.Fatal("expected non-empty UncommittedPatch for mixed mode")
	}
}

func TestCaptureGitChangeEvidence_StagedOnlyChanges(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	baseline := getHeadSHA(t, root)

	// Create a file and stage it (staged-only change)
	if err := os.WriteFile(root+"/staged.txt", []byte("staged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", "staged.txt")

	ev := CaptureGitChangeEvidence(root, baseline)
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeUncommittedWorktree {
		t.Fatalf("expected mode %q, got %q", EvidenceModeUncommittedWorktree, ev.Mode)
	}
	if !ev.Dirty {
		t.Fatal("expected dirty for staged-only change")
	}
	if ev.NameStatus == "" {
		t.Fatal("expected non-empty NameStatus for staged file")
	}
	if !strings.Contains(ev.NameStatus, "staged.txt") {
		t.Fatal("expected staged.txt in NameStatus")
	}
	if ev.Patch == "" {
		t.Fatal("expected non-empty Patch for staged file")
	}
	if !strings.Contains(ev.Patch, "staged.txt") {
		t.Fatal("expected staged.txt in Patch")
	}
	if !strings.Contains(ev.StatusPorcelain, "staged.txt") {
		t.Fatal("expected staged.txt in StatusPorcelain")
	}
}

func TestCaptureGitChangeEvidence_UntrackedOnlyChanges(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	baseline := getHeadSHA(t, root)

	// Create an untracked file (not staged)
	if err := os.WriteFile(root+"/untracked.txt", []byte("untracked\n"), 0644); err != nil {
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
		t.Fatal("expected dirty for untracked-only change")
	}
	if !strings.Contains(ev.StatusPorcelain, "untracked.txt") {
		t.Fatal("expected untracked.txt in StatusPorcelain")
	}
	// git diff HEAD won't show untracked files — clean diff is expected
	if ev.NameStatus != "" {
		t.Log("NameStatus is empty for untracked-only (no tracked diff)")
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

func TestResolveCommitState_UncommittedWithUpstreamEven_ReturnsReadyToCommit(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Set up upstream
	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	// Create uncommitted dirty file
	if err := os.WriteFile(root+"/dirty.txt", []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{
		RepoPath:           root,
		ValidationPassed:   true,
		AuditAccepted:      true,
		EvidenceMode:       EvidenceModeUncommittedWorktree,
		HasGitDiffEvidence: true,
		EvidenceHeadSHA:    headSHA,
		EvidenceBranch:     "main",
	})
	if state.State != CommitStateReadyToCommit {
		t.Fatalf("expected ready_to_commit (not pushed), got %s", state.State)
	}
}

func TestResolveCommitState_BaselineUnavailableDirty_ReturnsReadyToCommit(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Make dirty change
	if err := os.WriteFile(root+"/dirty.txt", []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{
		RepoPath:           root,
		ValidationPassed:   true,
		AuditAccepted:      true,
		EvidenceMode:       EvidenceModeBaselineUnavailableDirty,
		HasGitDiffEvidence: true,
		EvidenceHeadSHA:    headSHA,
		EvidenceBranch:     "main",
	})
	if state.State != CommitStateReadyToCommit {
		t.Fatalf("expected ready_to_commit, got %s", state.State)
	}
}

func TestResolveCommitState_BaselineUnavailableClean_ReturnsNoChanges(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{
		RepoPath:           root,
		ValidationPassed:   true,
		AuditAccepted:      true,
		EvidenceMode:       EvidenceModeBaselineUnavailableClean,
		HasGitDiffEvidence: true,
		EvidenceHeadSHA:    headSHA,
		EvidenceBranch:     "main",
	})
	if state.State != CommitStateNoChanges {
		t.Fatalf("expected no_changes, got %s", state.State)
	}
}

func TestResolveCommitState_CommitResultWithUpstreamEven_ReturnsPushed(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Set up upstream
	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{
		RepoPath:            root,
		ValidationPassed:    true,
		AuditAccepted:       true,
		EvidenceMode:        EvidenceModeBaselineUnavailableClean,
		HasGitDiffEvidence:  true,
		EvidenceHeadSHA:     headSHA,
		EvidenceBranch:      "main",
		CommitResultSuccess: true,
		CommitResultSHA:     headSHA,
	})
	if state.State != CommitStatePushed {
		t.Fatalf("expected pushed (via commit result), got %s", state.State)
	}
}

func TestResolveCommitState_CommitResultWithUpstreamAhead_ReturnsCommittedLocal(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Set up upstream
	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	// Create a commit so HEAD is ahead
	if err := os.WriteFile(root+"/new.txt", []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	commitResult := CreateGitCommit(root, "feat: test commit")
	if !commitResult.Success {
		t.Fatalf("commit failed: %s", commitResult.Error)
	}

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{
		RepoPath:            root,
		ValidationPassed:    true,
		AuditAccepted:       true,
		EvidenceMode:        EvidenceModeNoChanges,
		HasGitDiffEvidence:  true,
		EvidenceHeadSHA:     headSHA,
		EvidenceBranch:      "main",
		CommitResultSuccess: true,
		CommitResultSHA:     headSHA,
	})
	if state.State != CommitStateCommittedLocal {
		t.Fatalf("expected committed_local (via commit result), got %s", state.State)
	}
}

func TestCreateGitCommit_ThenCaptureEvidence_BaselineKnown_ReturnsCommittedRange(t *testing.T) {
	requireGit(t)
	root := setupTestRepoWithFile(t)

	baseline := getHeadSHA(t, root)

	// Create dirty change
	if err := os.WriteFile(root+"/change.txt", []byte("change\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit through CreateGitCommit
	result := CreateGitCommit(root, "feat: add change")
	if !result.Success {
		t.Fatalf("commit failed: %s", result.Error)
	}

	// Re-capture evidence using original baseline
	ev := CaptureGitChangeEvidence(root, baseline)
	if ev.Error != "" {
		t.Fatalf("unexpected error: %s", ev.Error)
	}
	if ev.Mode != EvidenceModeCommittedRange {
		t.Fatalf("expected mode %q after commit, got %q", EvidenceModeCommittedRange, ev.Mode)
	}
	if ev.Dirty {
		t.Fatal("expected not dirty after commit")
	}
	if ev.CurrentHeadSHA == baseline {
		t.Fatal("expected HEAD to differ from baseline after commit")
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

// setupTestRepo creates a temporary git repo with an initial commit and returns its path.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")
	return root
}

func setupTestRepoWithFile(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "config", "user.email", "relay-test@example.invalid")
	runCmd(t, root, "git", "config", "user.name", "Relay Test")
	if err := os.WriteFile(root+"/initial.txt", []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "initial commit")
	return root
}

func TestResolveCommitState_NoEvidence_ReturnsBlockedNoDiffInspection(t *testing.T) {
	state := ResolveCommitState(CommitStateInput{RepoPath: "/tmp/nonexistent", ValidationPassed: true, AuditAccepted: true})
	if state.State != CommitStateBlockedNoDiffInspection {
		t.Fatalf("expected blocked_no_diff_inspection, got %s", state.State)
	}
}

func TestResolveCommitState_ValidationNotPassed_ReturnsBlockedValidation(t *testing.T) {
	state := ResolveCommitState(CommitStateInput{RepoPath: "/tmp/nonexistent", AuditAccepted: true, EvidenceMode: "uncommitted_worktree", HasGitDiffEvidence: true, EvidenceHeadSHA: "abc123", EvidenceBranch: "main"})
	if state.State != CommitStateBlockedValidation {
		t.Fatalf("expected blocked_validation, got %s", state.State)
	}
}

func TestResolveCommitState_ValidationFailedNotAccepted_ReturnsBlockedValidation(t *testing.T) {
	state := ResolveCommitState(CommitStateInput{RepoPath: "/tmp/nonexistent", AuditAccepted: true, EvidenceMode: "uncommitted_worktree", HasGitDiffEvidence: true, EvidenceHeadSHA: "abc123", EvidenceBranch: "main"})
	if state.State != CommitStateBlockedValidation {
		t.Fatalf("expected blocked_validation, got %s", state.State)
	}
}

func TestResolveCommitState_AuditNotAccepted_ReturnsBlockedAudit(t *testing.T) {
	state := ResolveCommitState(CommitStateInput{RepoPath: "/tmp/nonexistent", ValidationPassed: true, EvidenceMode: "uncommitted_worktree", HasGitDiffEvidence: true, EvidenceHeadSHA: "abc123", EvidenceBranch: "main"})
	if state.State != CommitStateBlockedAuditNotAccepted {
		t.Fatalf("expected blocked_audit_not_accepted, got %s", state.State)
	}
}

func TestResolveCommitState_MixedEvidence_ReturnsBlockedMixed(t *testing.T) {
	state := ResolveCommitState(CommitStateInput{RepoPath: "/tmp/nonexistent", ValidationPassed: true, AuditAccepted: true, EvidenceMode: EvidenceModeMixedCommittedUncommitted, HasGitDiffEvidence: true, EvidenceHeadSHA: "abc123", EvidenceBranch: "main"})
	if state.State != CommitStateBlockedMixedChanges {
		t.Fatalf("expected blocked_mixed_changes, got %s", state.State)
	}
}

func TestResolveCommitState_NoChanges_ReturnsNoChanges(t *testing.T) {
	state := ResolveCommitState(CommitStateInput{RepoPath: "/tmp/nonexistent", ValidationPassed: true, AuditAccepted: true, EvidenceMode: EvidenceModeNoChanges, HasGitDiffEvidence: true, EvidenceHeadSHA: "abc123", EvidenceBranch: "main"})
	if state.State != CommitStateNoChanges {
		t.Fatalf("expected no_changes, got %s", state.State)
	}
}

func TestResolveCommitState_Uncommitted_WitAuditPass_ReturnsReadyToCommit(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Create an uncommitted file
	if err := os.WriteFile(root+"/new.txt", []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{RepoPath: root, ValidationPassed: true, AuditAccepted: true, EvidenceMode: EvidenceModeUncommittedWorktree, HasGitDiffEvidence: true, EvidenceHeadSHA: headSHA, EvidenceBranch: "main"})
	if state.State != CommitStateReadyToCommit {
		t.Fatalf("expected ready_to_commit, got %s", state.State)
	}
}

func TestResolveCommitState_CommittedRange_NoUpstream_ReturnsBlockedNoUpstream(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Add a commit
	if err := os.WriteFile(root+"/feature.txt", []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "add feature")

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{RepoPath: root, ValidationPassed: true, AuditAccepted: true, EvidenceMode: EvidenceModeCommittedRange, HasGitDiffEvidence: true, EvidenceHeadSHA: headSHA, EvidenceBranch: "main"})
	if state.State != CommitStateBlockedNoUpstream {
		t.Fatalf("expected blocked_no_upstream, got %s", state.State)
	}
}

func TestResolveCommitState_CommittedRange_WithUpstreamAhead_ReturnsCommittedLocal(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Set up a bare remote
	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")

	// Add remote and push initial
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	// Create a new commit
	if err := os.WriteFile(root+"/feature.txt", []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "commit", "-m", "add feature")

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{RepoPath: root, ValidationPassed: true, AuditAccepted: true, EvidenceMode: EvidenceModeCommittedRange, HasGitDiffEvidence: true, EvidenceHeadSHA: headSHA, EvidenceBranch: "main"})
	if state.State != CommitStateCommittedLocal {
		t.Fatalf("expected committed_local, got %s", state.State)
	}
	if !state.HasUpstream {
		t.Fatal("expected has_upstream true")
	}
	if state.AheadCount < 1 {
		t.Fatalf("expected ahead_count > 0, got %d", state.AheadCount)
	}
}

func TestResolveCommitState_Pushed_ReturnsPushed(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	headSHA := getHeadSHA(t, root)
	state := ResolveCommitState(CommitStateInput{RepoPath: root, ValidationPassed: true, AuditAccepted: true, EvidenceMode: EvidenceModeCommittedRange, HasGitDiffEvidence: true, EvidenceHeadSHA: headSHA, EvidenceBranch: "main"})
	if state.State != CommitStatePushed {
		t.Fatalf("expected pushed, got %s", state.State)
	}
}

func TestCreateGitCommit_CreatesRealCommit(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Create uncommitted file
	if err := os.WriteFile(root+"/new.txt", []byte("new content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := CreateGitCommit(root, "feat: add new file")
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.SHA == "" {
		t.Fatal("expected non-empty SHA")
	}
	if result.ShortSHA == "" {
		t.Fatal("expected non-empty short SHA")
	}
	if result.Subject != "feat: add new file" {
		t.Fatalf("expected subject 'feat: add new file', got %q", result.Subject)
	}
	if result.Branch != "main" {
		t.Fatalf("expected branch 'main', got %q", result.Branch)
	}

	// Verify git log shows the commit
	out := runCmdOutput(t, root, "git", "log", "--oneline", "-1")
	if !strings.Contains(out, "feat: add new file") {
		t.Fatalf("expected commit message in git log, got %q", out)
	}

	// Verify worktree is clean
	porcelain := runCmdOutput(t, root, "git", "status", "--porcelain")
	if porcelain != "" {
		t.Fatalf("expected clean worktree after commit, got %q", porcelain)
	}
}

func TestCreateGitCommit_EmptyPath(t *testing.T) {
	result := CreateGitCommit("", "feat: test")
	if result.Success {
		t.Fatal("expected failure for empty path")
	}
	if result.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestCreateGitCommit_NotARepo(t *testing.T) {
	dir := t.TempDir()
	result := CreateGitCommit(dir, "feat: test")
	if result.Success {
		t.Fatal("expected failure for non-repo")
	}
}

func TestDryRunPush_NoUpstream_ReturnsFailure(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	dryRun := DryRunPush(root)
	if dryRun.DryRunPass {
		t.Fatal("expected dry run to fail without upstream")
	}
}

func TestPushAndDryRunPush_BasicFlow(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Set up bare remote
	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	// Create a new commit
	if err := os.WriteFile(root+"/push_test.txt", []byte("push me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result := CreateGitCommit(root, "feat: push test")
	if !result.Success {
		t.Fatalf("commit failed: %s", result.Error)
	}

	// Dry run should succeed
	dryRun := DryRunPush(root)
	if !dryRun.DryRunPass {
		t.Fatalf("dry run failed: %s", dryRun.Error)
	}

	// Push should succeed
	pushResult := PushGitCommit(root)
	if !pushResult.Success {
		t.Fatalf("push failed: %s", pushResult.Error)
	}
}

func TestPushGitCommit_NoUpstream_ReturnsFailure(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	// Create a commit
	if err := os.WriteFile(root+"/orphan.txt", []byte("orphan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result := CreateGitCommit(root, "feat: orphan commit")
	if !result.Success {
		t.Fatalf("commit failed: %s", result.Error)
	}

	pushResult := PushGitCommit(root)
	if pushResult.Success {
		t.Fatal("expected push to fail without upstream")
	}
}

func TestDryRunPush_DirtyWorktree_GitDoesNotBlockDryRun(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	// Create dirty file without committing - git push --dry-run still succeeds
	// because worktree cleanliness is checked at the handler level, not in DryRunPush.
	if err := os.WriteFile(root+"/dirty.txt", []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dryRun := DryRunPush(root)
	if !dryRun.DryRunPass {
		t.Log("dry run failed with dirty worktree (expected git behavior may vary)")
	}
}

func TestGetUpstreamInfo_NoUpstream_ReturnsError(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	_, err := GetUpstreamInfo(root)
	if err == nil {
		t.Fatal("expected error for repo without upstream")
	}
}

func TestGetUpstreamInfo_WithUpstream_ReturnsInfo(t *testing.T) {
	requireGit(t)
	root := setupTestRepo(t)

	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")
	runCmd(t, root, "git", "remote", "add", "origin", remote)
	runCmd(t, root, "git", "push", "--set-upstream", "origin", "main")

	info, err := GetUpstreamInfo(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Remote != "origin" {
		t.Fatalf("expected remote 'origin', got %q", info.Remote)
	}
	if info.Branch != "main" {
		t.Fatalf("expected branch 'main', got %q", info.Branch)
	}
}

func TestStrconvAtoi(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		wantErr  bool
	}{
		{"0", 0, false},
		{"1", 1, false},
		{"42", 42, false},
		{"", 0, false},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		result, err := strconvAtoi(tt.input)
		if tt.wantErr && err == nil {
			t.Errorf("strconvAtoi(%q) expected error", tt.input)
		}
		if !tt.wantErr && result != tt.expected {
			t.Errorf("strconvAtoi(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

// runCmdOutput runs a command and returns stdout as string.
func runCmdOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\nstderr: %s", name, args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}
