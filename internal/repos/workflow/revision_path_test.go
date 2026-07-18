package workflowrepos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestResolvePathBlobUsesTheResolvedTreeAndExactPath(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	runRevisionPathGit(t, repo, "init", "-b", "main")
	runRevisionPathGit(t, repo, "config", "user.email", "relay@example.test")
	runRevisionPathGit(t, repo, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(repo, "nested", "source.txt"), []byte("source bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRevisionPathGit(t, repo, "add", ".")
	runRevisionPathGit(t, repo, "commit", "-m", "fixture")
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	target, err := registry.Register(ctx, "project", repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.ConfigureRepositoryTarget(ctx, workflowstore.ConfigureRepositoryTargetParams{RepoTarget: "project", ExpectedConfigurationVersion: target.ConfigurationVersion, ConfiguredBranchRef: "refs/heads/main"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	revision, err := registry.ResolveRevision(ctx, RevisionRequest{RepoTarget: "project"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.ResolvePathBlob(ctx, revision, "nested/source.txt")
	if err != nil || resolved.Path != "nested/source.txt" || resolved.BlobOID == "" {
		t.Fatalf("resolved = %#v err=%v", resolved, err)
	}
	if _, err := registry.ResolvePathBlob(ctx, revision, "../source.txt"); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func runRevisionPathGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
