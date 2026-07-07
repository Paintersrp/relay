package projects

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workflowprojects "relay/internal/app/projects/workflow"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeProjectService struct {
	project workflowstore.Project
	detail  workflowprojects.ProjectDetail
	note    workflowstore.ProjectNote
	repo    workflowstore.ProjectRepositoryTarget
	err     error
}

func (f *fakeProjectService) ListProjects(context.Context, workflowprojects.ListProjectsInput) ([]workflowstore.Project, error) {
	return []workflowstore.Project{f.project}, f.err
}

func (f *fakeProjectService) GetProject(context.Context, workflowprojects.GetProjectInput) (workflowprojects.ProjectDetail, error) {
	return f.detail, f.err
}

func (f *fakeProjectService) CreateProject(context.Context, workflowprojects.CreateProjectInput) (workflowstore.Project, error) {
	return f.project, f.err
}

func (f *fakeProjectService) UpdateProject(context.Context, workflowprojects.UpdateProjectInput) (workflowstore.Project, error) {
	return f.project, f.err
}

func (f *fakeProjectService) ArchiveProject(context.Context, string) (workflowstore.Project, error) {
	value := f.project
	value.Status = workflowstore.ProjectStatusArchived
	return value, f.err
}

func (f *fakeProjectService) RestoreProject(context.Context, string) (workflowstore.Project, error) {
	return f.project, f.err
}

func (f *fakeProjectService) AttachRepository(context.Context, string, string) (workflowstore.ProjectRepositoryTarget, error) {
	return f.repo, f.err
}

func (f *fakeProjectService) DetachRepository(context.Context, string, string) error {
	return f.err
}

func (f *fakeProjectService) CreateNote(context.Context, workflowprojects.CreateNoteInput) (workflowstore.ProjectNote, error) {
	return f.note, f.err
}

func (f *fakeProjectService) UpdateNote(context.Context, workflowprojects.UpdateNoteInput) (workflowstore.ProjectNote, error) {
	return f.note, f.err
}

func (f *fakeProjectService) DeleteNote(context.Context, string, string) error {
	return f.err
}

func projectRouter(service WorkflowProjectService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service))
	return router
}

func TestProjectRoutesExposeSimplifiedModel(t *testing.T) {
	project := workflowstore.Project{
		ProjectID: "project-test",
		Name:      "Relay",
		Status:    workflowstore.ProjectStatusActive,
	}
	service := &fakeProjectService{
		project: project,
		detail: workflowprojects.ProjectDetail{
			Project: project,
			Repositories: []workflowstore.ProjectRepositoryTarget{
				workflowstore.ProjectRepositoryTarget{
					RepoTarget: "relay",
				},
			},
			Notes: []workflowstore.ProjectNote{
				workflowstore.ProjectNote{
					NoteID: "note-test",
					Title:  "Later",
					Body:   "Future work",
					Status: workflowstore.ProjectNoteStatusOpen,
				},
			},
			Plans: []workflowstore.Plan{
				workflowstore.Plan{
					PlanID: "plan-test",
					Status: workflowstore.PlanStatusActive,
				},
			},
		},
		note: workflowstore.ProjectNote{
			NoteID: "note-test",
			Title:  "Later",
			Body:   "Future work",
			Status: workflowstore.ProjectNoteStatusOpen,
		},
		repo: workflowstore.ProjectRepositoryTarget{RepoTarget: "relay"},
	}
	handler := projectRouter(service)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/projects/project-test?repositoryLimit=1&noteLimit=1&planLimit=1", nil))
	body := response.Body.String()
	for _, expected := range []string{`"projectId":"project-test"`, `"repoTarget":"relay"`, `"noteId":"note-test"`, `"planId":"plan-test"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %s: %s", expected, body)
		}
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/projects",
		strings.NewReader(`{"name":"Relay","description":"Primary"}`),
	))
	if response.Code != http.StatusCreated {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodDelete,
		"/projects/project-test/notes/note-test",
		nil,
	))
	if response.Code != http.StatusNoContent {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestProjectRouteRejectsUnknownFields(t *testing.T) {
	response := httptest.NewRecorder()
	projectRouter(&fakeProjectService{}).ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/projects",
		strings.NewReader(`{"name":"Relay","legacy":true}`),
	))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
