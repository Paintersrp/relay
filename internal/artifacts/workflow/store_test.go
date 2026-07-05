package workflowartifacts

import (
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
