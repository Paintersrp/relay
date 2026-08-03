package workflowruns

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

func openRunTestStore(t *testing.T) (*workflowstore.Store, string) {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	return store, filepath.Dir(store.ArtifactStore().Root())
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
