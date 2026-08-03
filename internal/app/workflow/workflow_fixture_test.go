package workflow

import (
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

func openApplicationWorkflowStore(t *testing.T) (*workflowstore.Store, string) {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	return store, filepath.Dir(store.ArtifactStore().Root())
}
