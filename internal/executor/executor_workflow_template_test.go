package executor

import (
	"context"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

// newExecutorWorkflowStore provides an isolated copy of the closed, schema-only
// template for tests whose subject is not workflow migration behavior.
func newExecutorWorkflowStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	return workflowfixture.Open(t, workflowstore.Open)
}

func TestExecutorWorkflowTemplateIsLatestAndFinalized(t *testing.T) {
	store := newExecutorWorkflowStore(t)
	var version int
	if err := store.DB().QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil || version <= 0 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
}

func TestExecutorWorkflowTemplateFixturesAreIsolatedAndUsable(t *testing.T) {
	first := newExecutorWorkflowStore(t)
	second := newExecutorWorkflowStore(t)
	if first.ArtifactStore().Root() == second.ArtifactStore().Root() {
		t.Fatal("fixture artifact roots are shared")
	}
	if _, err := first.DB().Exec("INSERT INTO projects (project_id, name) VALUES ('project-template-first', 'First')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_id = 'project-template-first'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second fixture contains first fixture data: %d", count)
	}

	transaction, err := first.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec("INSERT INTO projects (project_id, name) VALUES ('project-template-rollback', 'Rollback')"); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := first.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_id = 'project-template-rollback'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back project count = %d", count)
	}
	if _, err := first.DB().Exec("INSERT INTO project_notes (note_id, project_row_id, title, body) VALUES ('note-template-fk', 999, 'Note', 'body')"); err == nil {
		t.Fatal("foreign key violation succeeded")
	}
	if _, err := first.DB().Exec("DELETE FROM projects WHERE project_id = 'project-template-first'"); err == nil {
		t.Fatal("project delete trigger did not run")
	}

	batch, err := first.ArtifactStore().Begin("template")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("fixture", "artifact.txt", "text/plain", []byte("artifact"))
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := batch.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	if _, content, err := first.ArtifactStore().ReadVerifiedFile(file, 32); err != nil || string(content) != "artifact" {
		t.Fatalf("artifact-backed operation content=%q err=%v", content, err)
	}
}
