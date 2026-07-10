package workflowstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectDatabaseConstraintsEnforceOrganizationBoundary(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)

	var active Project
	var archived Project
	var empty Project
	if err := store.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", filepath.Join(t.TempDir(), "relay")); err != nil {
			return err
		}
		var err error
		active, err = tx.CreateProject(ctx, CreateProjectParams{
			ProjectID: "project-active",
			Name:      "Active",
		})
		if err != nil {
			return err
		}
		archived, err = tx.CreateProject(ctx, CreateProjectParams{
			ProjectID: "project-archived",
			Name:      "Archived",
		})
		if err != nil {
			return err
		}
		archived, err = tx.TransitionProjectStatus(ctx, archived.ProjectID, ProjectStatusActive, ProjectStatusArchived)
		if err != nil {
			return err
		}
		empty, err = tx.CreateProject(ctx, CreateProjectParams{
			ProjectID: "project-empty",
			Name:      "Empty",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DB().Exec(`
INSERT INTO plans (plan_id, feature_slug, status, canonical_sha256)
VALUES ('plan-unassigned', 'feature', 'active', ?)`, strings.Repeat("a", 64)); err == nil {
		t.Fatal("unassigned Plan was accepted")
	}
	if _, err := store.DB().Exec(`
INSERT INTO plans (project_row_id, plan_id, feature_slug, status, canonical_sha256)
VALUES (?, 'plan-archived', 'feature', 'active', ?)`, archived.ID, strings.Repeat("b", 64)); err == nil {
		t.Fatal("archived Project accepted a new Plan")
	}

	var plan Plan
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		plan, err = tx.CreatePlan(ctx, CreatePlanParams{
			ProjectRowID:    active.ID,
			PlanID:          "plan-active",
			FeatureSlug:     "feature",
			CanonicalSHA256: strings.Repeat("c", 64),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE plans SET project_row_id = ? WHERE id = ?`, archived.ID, plan.ID); err == nil {
		t.Fatal("Plan moved into archived Project")
	}
	if _, err := store.DB().Exec(`DELETE FROM projects WHERE id = ?`, active.ID); err == nil {
		t.Fatal("Project hard delete with attached Plans was accepted")
	}
	if _, err := store.DB().Exec(`DELETE FROM projects WHERE id = ?`, empty.ID); err == nil {
		t.Fatal("empty Project hard delete was accepted")
	}
}

func TestProjectAndProjectNoteStatusConstraintsRejectUnsupportedValues(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)

	var project Project
	var note ProjectNote
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		project, err = tx.CreateProject(ctx, CreateProjectParams{
			ProjectID: "project-status",
			Name:      "Status",
		})
		if err != nil {
			return err
		}
		note, err = tx.CreateProjectNote(ctx, CreateProjectNoteParams{
			NoteID:       "note-status",
			ProjectRowID: project.ID,
			Title:        "Status",
			Body:         "Status validation",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DB().Exec(`UPDATE projects SET status = 'paused' WHERE id = ?`, project.ID); err == nil {
		t.Fatal("unsupported Project status update was accepted")
	}
	if _, err := store.DB().Exec(`
INSERT INTO projects (project_id, name, status)
VALUES ('project-invalid-status', 'Invalid', 'paused')`); err == nil {
		t.Fatal("unsupported Project status insert was accepted")
	}
	if _, err := store.DB().Exec(`UPDATE project_notes SET status = 'blocked' WHERE id = ?`, note.ID); err == nil {
		t.Fatal("unsupported Project Note status update was accepted")
	}
	if _, err := store.DB().Exec(`
INSERT INTO project_notes (note_id, project_row_id, title, body, status)
VALUES ('note-invalid-status', ?, 'Invalid', 'Invalid status', 'blocked')`, project.ID); err == nil {
		t.Fatal("unsupported Project Note status insert was accepted")
	}

	var projectStatus string
	if err := store.DB().QueryRow(`SELECT status FROM projects WHERE id = ?`, project.ID).Scan(&projectStatus); err != nil {
		t.Fatal(err)
	}
	if projectStatus != ProjectStatusActive {
		t.Fatalf("Project status = %q, want %q", projectStatus, ProjectStatusActive)
	}
	var noteStatus string
	if err := store.DB().QueryRow(`SELECT status FROM project_notes WHERE id = ?`, note.ID).Scan(&noteStatus); err != nil {
		t.Fatal(err)
	}
	if noteStatus != ProjectNoteStatusOpen {
		t.Fatalf("Project Note status = %q, want %q", noteStatus, ProjectNoteStatusOpen)
	}

	var invalidProjects int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM projects WHERE project_id = 'project-invalid-status'`).Scan(&invalidProjects); err != nil {
		t.Fatal(err)
	}
	var invalidNotes int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM project_notes WHERE note_id = 'note-invalid-status'`).Scan(&invalidNotes); err != nil {
		t.Fatal(err)
	}
	var attachments int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM project_repository_targets WHERE project_row_id = ?`, project.ID).Scan(&attachments); err != nil {
		t.Fatal(err)
	}
	var plans int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM plans WHERE project_row_id = ?`, project.ID).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if invalidProjects != 0 || invalidNotes != 0 || attachments != 0 || plans != 0 {
		t.Fatalf("failed status writes left partial state: projects=%d notes=%d attachments=%d plans=%d", invalidProjects, invalidNotes, attachments, plans)
	}
}

func TestProjectRepositoryAttachmentsAreIdempotentAndReferenceGlobalTargets(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	var project Project
	if err := store.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", filepath.Join(t.TempDir(), "relay")); err != nil {
			return err
		}
		var err error
		project, err = tx.CreateProject(ctx, CreateProjectParams{
			ProjectID: "project-attachments",
			Name:      "Attachments",
		})
		if err != nil {
			return err
		}
		if _, err := tx.AttachProjectRepository(ctx, project.ID, "relay"); err != nil {
			return err
		}
		_, err = tx.AttachProjectRepository(ctx, project.ID, "relay")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	attachments, err := store.ListProjectRepositoryTargets(ctx, project.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 || attachments[0].RepoTarget != "relay" {
		t.Fatalf("attachments = %+v", attachments)
	}

	if _, err := store.DB().Exec(`
INSERT INTO project_repository_targets (project_row_id, repo_target)
VALUES (?, 'RELAY')`, project.ID); err == nil {
		t.Fatal("case-variant duplicate repository attachment was accepted")
	}
	if _, err := store.DB().Exec(`
INSERT INTO project_repository_targets (project_row_id, repo_target)
VALUES (?, 'missing')`, project.ID); err == nil {
		t.Fatal("unknown repository attachment was accepted")
	}
}
