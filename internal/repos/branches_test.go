package repos

import (
	"os"
	"os/exec"
	"testing"
)

func TestListLocalBranches_EmptyPath(t *testing.T) {
	branches, err := ListLocalBranches("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if branches != nil {
		t.Fatalf("expected nil branches, got %#v", branches)
	}
}

func TestListLocalBranches_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()

	branches, err := ListLocalBranches(dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
	if branches != nil {
		t.Fatalf("expected nil branches, got %#v", branches)
	}
}

func TestListLocalBranches_DetectsCurrentBranch(t *testing.T) {
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")

	branches, err := ListLocalBranches(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 1 {
		t.Fatalf("expected 1 branch, got %d: %#v", len(branches), branches)
	}
	if branches[0].Name != "main" {
		t.Errorf("expected branch name 'main', got %q", branches[0].Name)
	}
	if !branches[0].IsCurrent {
		t.Errorf("expected 'main' to be current branch")
	}
}

func TestListLocalBranches_MultipleBranches(t *testing.T) {
	root := t.TempDir()

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "commit", "--allow-empty", "-m", "initial")
	runCmd(t, root, "git", "branch", "feature-a")
	runCmd(t, root, "git", "branch", "feature-b")

	branches, err := ListLocalBranches(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 3 {
		t.Fatalf("expected 3 branches, got %d: %#v", len(branches), branches)
	}

	nameSet := make(map[string]struct{})
	for _, b := range branches {
		nameSet[b.Name] = struct{}{}
	}
	if _, ok := nameSet["main"]; !ok {
		t.Error("expected 'main' in branch list")
	}
	if _, ok := nameSet["feature-a"]; !ok {
		t.Error("expected 'feature-a' in branch list")
	}
	if _, ok := nameSet["feature-b"]; !ok {
		t.Error("expected 'feature-b' in branch list")
	}

	for _, b := range branches {
		if b.Name == "main" && !b.IsCurrent {
			t.Error("expected 'main' to be current")
		}
		if b.Name != "main" && b.IsCurrent {
			t.Errorf("expected %q to not be current", b.Name)
		}
	}
}

func runCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
}
