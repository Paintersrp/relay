package workflowartifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestBatchPromotePrepareCommitAndCommit(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := store.Begin("plans/plan-1")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("canonical_plan", "feature.plan.json", "application/json", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := batch.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	batch.Commit()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}\n" {
		t.Fatalf("unexpected artifact content %q", data)
	}
}

func TestBatchStageFileCopiesLargeSourceWithoutLoadingItIntoTheCaller(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.log")
	content := bytes.Repeat([]byte("0123456789abcdef"), 128*1024)
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	batch, err := store.Begin("attempts/attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := batch.StageFile("executor_stdout", "stdout.log", "text/plain", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if staged.SizeBytes != int64(len(content)) {
		t.Fatalf("size = %d, want %d", staged.SizeBytes, len(content))
	}
	digest := sha256.Sum256(content)
	if staged.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("sha256 = %q", staged.SHA256)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := batch.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	batch.Commit()
	persisted, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(staged.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(persisted, content) {
		t.Fatal("persisted artifact content changed")
	}
}

func TestBatchRollbackRemovesPromotedArtifactsBeforeDatabaseCommit(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := store.Begin("runs/run-1")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("execution_spec", "feature.execution-spec.json", "application/json", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := batch.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	if err := batch.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("promoted artifact remains after rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "runs", "run-1")); !os.IsNotExist(err) {
		t.Fatalf("empty artifact namespace remains after rollback: %v", err)
	}
}

func TestBatchPromoteFailureLeavesNoDurableArtifact(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := store.Begin("runs/run-1")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("execution_spec", "feature.execution-spec.json", "application/json", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runs"), []byte("block directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err == nil {
		t.Fatal("expected promotion failure")
	}
	if err := batch.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("artifact became durable after promotion failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "runs", "run-1")); !os.IsNotExist(err) {
		t.Fatalf("empty artifact namespace remains after promotion failure: %v", err)
	}
}

func TestBatchRejectsUnsafeNamesAndNamespaces(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, namespace := range []string{"", "../runs", "runs//one", " runs/one"} {
		if _, err := store.Begin(namespace); err == nil {
			t.Fatalf("expected namespace %q to be rejected", namespace)
		}
	}
	batch, err := store.Begin("runs/run-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"", "../file.json", " file.json", "dir/file.json"} {
		if _, err := batch.Stage("kind", filename, "application/json", nil); err == nil {
			t.Fatalf("expected filename %q to be rejected", filename)
		}
	}
	_ = batch.Rollback()
}
