package workflowrepos

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestRegistryRequiresExplicitGloballyUniqueKeysAndPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "relay.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}

	firstPath := t.TempDir()
	first, err := registry.Register(ctx, "relay", firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.RepoTarget != "relay" || !filepath.IsAbs(first.LocalPath) {
		t.Fatalf("unexpected registry row: %+v", first)
	}
	if _, err := registry.Register(ctx, "Relay", t.TempDir()); err == nil {
		t.Fatal("expected case-insensitive repository key collision")
	}
	if _, err := registry.Register(ctx, "other", firstPath); err == nil {
		t.Fatal("expected repository path collision")
	}
}

func TestRegistryResolveNeverCreatesUnknownTargets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "relay.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Resolve(ctx, "missing"); err == nil {
		t.Fatal("unknown repository target resolved or was created")
	}
	created, err := registry.Register(ctx, "relay", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve(ctx, "RELAY")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RepoTarget != created.RepoTarget || resolved.LocalPath != created.LocalPath {
		t.Fatalf("resolved target mismatch: got %+v want %+v", resolved, created)
	}
}

func TestRegistryRejectsUnsafeKeysAndNonDirectories(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "relay.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"", " relay", "relay repo", "owner/repo", "repo\\name", "repo\nname"} {
		if _, err := registry.Register(ctx, key, t.TempDir()); err == nil {
			t.Fatalf("expected key %q to be rejected", key)
		}
	}
	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(ctx, "file-target", file); err == nil {
		t.Fatal("expected file path to be rejected")
	}
}
