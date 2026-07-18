package sourcevault

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestReadPreparedObjectReadsVerifiedImportedClosureBeforePacketRetention(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runPreparedReadGit(t, repo, "init", "-b", "main")
	runPreparedReadGit(t, repo, "config", "user.email", "relay@example.test")
	runPreparedReadGit(t, repo, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("source bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runPreparedReadGit(t, repo, "add", ".")
	runPreparedReadGit(t, repo, "commit", "-m", "fixture")
	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	target, err := repositories.Register(ctx, "project", repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.ConfigureRepositoryTarget(ctx, workflowstore.ConfigureRepositoryTargetParams{RepoTarget: "project", ExpectedConfigurationVersion: target.ConfigurationVersion, ConfiguredBranchRef: "refs/heads/main"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	manager, err := Open(ctx, filepath.Join(root, "vaults"), store)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{RepoTarget: "project"})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	path, err := repositories.ResolvePathBlob(ctx, revision, "source.txt")
	if err != nil {
		t.Fatal(err)
	}
	read, err := manager.ReadPreparedObject(ctx, PreparedObjectReadRequest{Import: imported, ObjectOID: path.BlobOID, ExpectedType: "blob", MaxBytes: 1024})
	if err != nil || string(read.Bytes) != "source bytes\n" {
		t.Fatalf("prepared read = %#v err=%v", read, err)
	}
	if _, err := manager.ReadObject(ctx, ReadObjectRequest{ClosureID: imported.Closure.ClosureID, ObjectOID: path.BlobOID, ExpectedType: "blob", MaxBytes: 1024}); ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("unretained ordinary read = %v code=%q", err, ErrorCode(err))
	}
}

func runPreparedReadGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
