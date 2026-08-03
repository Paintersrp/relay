package workflowfixture_test

import (
	"context"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

func TestFixturesArePrivateMigratedCopies(t *testing.T) {
	first := workflowfixture.Open(t, workflowstore.Open)
	second := workflowfixture.Open(t, workflowstore.Open)
	if filepath.Dir(first.ArtifactStore().Root()) == filepath.Dir(second.ArtifactStore().Root()) {
		t.Fatal("fixture roots are shared")
	}
	if _, err := first.DB().Exec("INSERT INTO projects (project_id, name) VALUES ('project-fixture-first', 'First')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_id = 'project-fixture-first'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("isolated project count=%d err=%v", count, err)
	}
	tx, err := first.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO projects (project_id, name) VALUES ('project-rolled-back', 'Rollback')"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := first.DB().QueryRow("SELECT COUNT(*) FROM projects WHERE project_id = 'project-rolled-back'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	if _, err := first.DB().Exec("INSERT INTO project_notes (note_id, project_row_id, title, body) VALUES ('bad-fk', 999, 'Bad', 'bad')"); err == nil {
		t.Fatal("foreign key violation succeeded")
	}
	if _, err := first.DB().Exec("DELETE FROM projects WHERE project_id = 'project-fixture-first'"); err == nil {
		t.Fatal("delete trigger did not reject referenced project")
	}
}
