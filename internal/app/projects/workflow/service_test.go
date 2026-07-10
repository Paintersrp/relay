package workflowprojects

import (
	"context"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

type projectTestIDs struct {
	project int
	note    int
}

func (ids *projectTestIDs) ProjectID() string {
	ids.project++
	return "project-test-" + string(rune('0'+ids.project))
}

func (ids *projectTestIDs) NoteID() string {
	ids.note++
	return "note-test-" + string(rune('0'+ids.note))
}

func TestProjectLifecycleAttachmentsAndNotes(t *testing.T) {
	ctx := context.Background()
	store := openProjectTestStore(t)
	service, err := NewServiceWithIDs(store, &projectTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	repositoryPath := filepath.Join(t.TempDir(), "relay")
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTarget(ctx, "relay", repositoryPath)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	project, err := service.CreateProject(ctx, CreateProjectInput{Name: "Relay", Description: "Primary work"})
	if err != nil {
		t.Fatal(err)
	}
	if project.Status != workflowstore.ProjectStatusActive {
		t.Fatalf("status = %q", project.Status)
	}
	attached, err := service.AttachRepository(ctx, project.ProjectID, "relay")
	if err != nil {
		t.Fatal(err)
	}
	if attached.RepoTarget != "relay" {
		t.Fatalf("attachment = %+v", attached)
	}
	if _, err := service.AttachRepository(ctx, project.ProjectID, "relay"); err != nil {
		t.Fatalf("idempotent attachment failed: %v", err)
	}

	note, err := service.CreateNote(ctx, CreateNoteInput{
		ProjectID: project.ProjectID,
		Title:     "Follow up",
		Body:      "Prepare the frontend pass.",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := workflowstore.ProjectNoteStatusDone
	updatedNote, err := service.UpdateNote(ctx, UpdateNoteInput{
		ProjectID: project.ProjectID,
		NoteID:    note.NoteID,
		Status:    &done,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatedNote.Status != workflowstore.ProjectNoteStatusDone {
		t.Fatalf("note status = %q", updatedNote.Status)
	}

	archived, err := service.ArchiveProject(ctx, project.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != workflowstore.ProjectStatusArchived {
		t.Fatalf("archived status = %q", archived.Status)
	}
	detail, err := service.GetProject(ctx, GetProjectInput{ProjectID: project.ProjectID, RepositoryLimit: 1, NoteLimit: 1, PlanLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Repositories) != 1 || len(detail.Notes) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	if err := service.DetachRepository(ctx, project.ProjectID, "relay"); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteNote(ctx, project.ProjectID, note.NoteID); err != nil {
		t.Fatal(err)
	}
	restored, err := service.RestoreProject(ctx, project.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != workflowstore.ProjectStatusActive {
		t.Fatalf("restored status = %q", restored.Status)
	}
}

func TestProjectUpdatesAreBoundedAndValidateState(t *testing.T) {
	ctx := context.Background()
	store := openProjectTestStore(t)
	service, err := NewServiceWithIDs(store, &projectTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.CreateProject(ctx, CreateProjectInput{Name: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	name := "Renamed"
	updated, err := service.UpdateProject(ctx, UpdateProjectInput{
		ProjectID: project.ProjectID,
		Name:      &name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != name {
		t.Fatalf("name = %q", updated.Name)
	}
	blank := " "
	if _, err := service.UpdateProject(ctx, UpdateProjectInput{
		ProjectID: project.ProjectID,
		Name:      &blank,
	}); err == nil {
		t.Fatal("blank Project name was accepted")
	}
	projects, err := service.ListProjects(ctx, ListProjectsInput{Status: workflowstore.ProjectStatusActive, Limit: 1})
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects = %+v, error = %v", projects, err)
	}
}

func TestProjectDetailChildCollectionsRespectExplicitBounds(t *testing.T) {
	ctx := context.Background()
	store := openProjectTestStore(t)
	service, err := NewServiceWithIDs(store, &projectTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.CreateProject(ctx, CreateProjectInput{Name: "Bounded"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		for _, target := range []string{"relay", "other"} {
			if _, err := tx.CreateRepositoryTarget(ctx, target, filepath.Join(t.TempDir(), target)); err != nil {
				return err
			}
			if _, err := tx.AttachProjectRepository(ctx, project.ID, target); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := service.CreateNote(ctx, CreateNoteInput{
			ProjectID: project.ProjectID,
			Title:     "Note " + string(rune('A'+index)),
			Body:      "Bounded note body.",
		}); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := service.GetProject(ctx, GetProjectInput{
		ProjectID:       project.ProjectID,
		RepositoryLimit: 1,
		NoteLimit:       1,
		PlanLimit:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Repositories) != 1 || len(detail.Notes) != 1 || len(detail.Plans) > 1 {
		t.Fatalf("bounded detail = %+v", detail)
	}
}

func openProjectTestStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
