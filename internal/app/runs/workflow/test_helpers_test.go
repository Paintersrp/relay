package workflowruns

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func openRunTestStore(t *testing.T) (*workflowstore.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root
}

func registerRunTestRepo(t *testing.T, ctx context.Context, store *workflowstore.Store, target string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), target)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTarget(ctx, target, path)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
