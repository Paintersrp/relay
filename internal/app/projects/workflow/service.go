package workflowprojects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var ErrInvalidProjectRequest = errors.New("invalid Project request")

type IDGenerator interface {
	ProjectID() string
	NoteID() string
}

type defaultIDGenerator struct{}

func (defaultIDGenerator) ProjectID() string { return workflowstore.NewProjectID() }
func (defaultIDGenerator) NoteID() string    { return workflowstore.NewProjectNoteID() }

type Service struct {
	store *workflowstore.Store
	ids   IDGenerator
}

type ListProjectsInput struct {
	Status string
	Limit  int
}

type GetProjectInput struct {
	ProjectID       string
	RepositoryLimit int
	NoteLimit       int
	PlanLimit       int
}

type CreateProjectInput struct {
	Name        string
	Description string
}

type UpdateProjectInput struct {
	ProjectID   string
	Name        *string
	Description *string
}

type ProjectDetail struct {
	Project      workflowstore.Project
	Repositories []workflowstore.ProjectRepositoryTarget
	Notes        []workflowstore.ProjectNote
	Plans        []workflowstore.Plan
}

type CreateNoteInput struct {
	ProjectID string
	Title     string
	Body      string
}

type UpdateNoteInput struct {
	ProjectID string
	NoteID    string
	Title     *string
	Body      *string
	Status    *string
}

func NewService(store *workflowstore.Store) (*Service, error) {
	return NewServiceWithIDs(store, defaultIDGenerator{})
}

func NewServiceWithIDs(store *workflowstore.Store, ids IDGenerator) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if ids == nil {
		return nil, fmt.Errorf("Project ID generator is required")
	}
	return &Service{store: store, ids: ids}, nil
}

func (s *Service) ListProjects(ctx context.Context, input ListProjectsInput) ([]workflowstore.Project, error) {
	input.Status = strings.TrimSpace(input.Status)
	if err := workflowstore.ValidateWorkflowListStatus(
		input.Status,
		workflowstore.ProjectStatusActive,
		workflowstore.ProjectStatusArchived,
	); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProjectRequest, err)
	}
	return s.store.ListProjects(ctx, workflowstore.ProjectListQuery{Status: input.Status, Limit: input.Limit})
}

func (s *Service) GetProject(ctx context.Context, input GetProjectInput) (ProjectDetail, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return ProjectDetail{}, fmt.Errorf("%w: Project ID is required", ErrInvalidProjectRequest)
	}
	project, err := s.store.GetProjectByProjectID(ctx, projectID)
	if err != nil {
		return ProjectDetail{}, err
	}
	repositories, err := s.store.ListProjectRepositoryTargets(ctx, project.ID, input.RepositoryLimit)
	if err != nil {
		return ProjectDetail{}, err
	}
	notes, err := s.store.ListProjectNotes(ctx, project.ID, input.NoteLimit)
	if err != nil {
		return ProjectDetail{}, err
	}
	plans, err := s.store.ListPlans(ctx, workflowstore.PlanListQuery{
		ProjectRowID: sql.NullInt64{Int64: project.ID, Valid: true},
		Limit:        input.PlanLimit,
	})
	if err != nil {
		return ProjectDetail{}, err
	}
	return ProjectDetail{
		Project:      project,
		Repositories: repositories,
		Notes:        notes,
		Plans:        plans,
	}, nil
}

func (s *Service) CreateProject(ctx context.Context, input CreateProjectInput) (workflowstore.Project, error) {
	name := strings.TrimSpace(input.Name)
	description := strings.TrimSpace(input.Description)
	if name == "" {
		return workflowstore.Project{}, fmt.Errorf("%w: Project name is required", ErrInvalidProjectRequest)
	}
	var project workflowstore.Project
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		project, err = tx.CreateProject(ctx, workflowstore.CreateProjectParams{
			ProjectID:   s.ids.ProjectID(),
			Name:        name,
			Description: description,
		})
		return err
	})
	return project, err
}

func (s *Service) UpdateProject(ctx context.Context, input UpdateProjectInput) (workflowstore.Project, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" || (input.Name == nil && input.Description == nil) {
		return workflowstore.Project{}, fmt.Errorf("%w: Project ID and at least one changed field are required", ErrInvalidProjectRequest)
	}
	var updated workflowstore.Project
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		name := current.Name
		description := current.Description
		if input.Name != nil {
			name = strings.TrimSpace(*input.Name)
			if name == "" {
				return fmt.Errorf("%w: Project name is required", ErrInvalidProjectRequest)
			}
		}
		if input.Description != nil {
			description = strings.TrimSpace(*input.Description)
		}
		updated, err = tx.UpdateProject(ctx, projectID, name, description)
		return err
	})
	return updated, err
}

func (s *Service) ArchiveProject(ctx context.Context, projectID string) (workflowstore.Project, error) {
	return s.transitionProject(ctx, projectID, workflowstore.ProjectStatusActive, workflowstore.ProjectStatusArchived)
}

func (s *Service) RestoreProject(ctx context.Context, projectID string) (workflowstore.Project, error) {
	return s.transitionProject(ctx, projectID, workflowstore.ProjectStatusArchived, workflowstore.ProjectStatusActive)
}

func (s *Service) transitionProject(ctx context.Context, projectID, expected, next string) (workflowstore.Project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return workflowstore.Project{}, fmt.Errorf("%w: Project ID is required", ErrInvalidProjectRequest)
	}
	var updated workflowstore.Project
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		if current.Status == next {
			updated = current
			return nil
		}
		if current.Status != expected {
			return fmt.Errorf("%w: Project has unsupported status %q", ErrInvalidProjectRequest, current.Status)
		}
		updated, err = tx.TransitionProjectStatus(ctx, projectID, expected, next)
		return err
	})
	return updated, err
}

func (s *Service) AttachRepository(ctx context.Context, projectID, repoTarget string) (workflowstore.ProjectRepositoryTarget, error) {
	projectID = strings.TrimSpace(projectID)
	repoTarget = strings.TrimSpace(repoTarget)
	if projectID == "" || repoTarget == "" {
		return workflowstore.ProjectRepositoryTarget{}, fmt.Errorf("%w: Project ID and repository target are required", ErrInvalidProjectRequest)
	}
	var attached workflowstore.ProjectRepositoryTarget
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		repository, err := tx.GetRepositoryTarget(ctx, repoTarget)
		if err != nil {
			return err
		}
		if repository.RepoTarget != repoTarget {
			return fmt.Errorf("%w: repository target must use registered key casing %q", ErrInvalidProjectRequest, repository.RepoTarget)
		}
		attached, err = tx.AttachProjectRepository(ctx, project.ID, repository.RepoTarget)
		return err
	})
	return attached, err
}

func (s *Service) DetachRepository(ctx context.Context, projectID, repoTarget string) error {
	projectID = strings.TrimSpace(projectID)
	repoTarget = strings.TrimSpace(repoTarget)
	if projectID == "" || repoTarget == "" {
		return fmt.Errorf("%w: Project ID and repository target are required", ErrInvalidProjectRequest)
	}
	return s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		return tx.DetachProjectRepository(ctx, project.ID, repoTarget)
	})
}

func (s *Service) CreateNote(ctx context.Context, input CreateNoteInput) (workflowstore.ProjectNote, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	title := strings.TrimSpace(input.Title)
	body := strings.TrimSpace(input.Body)
	if projectID == "" || title == "" || body == "" {
		return workflowstore.ProjectNote{}, fmt.Errorf("%w: Project ID, note title, and note body are required", ErrInvalidProjectRequest)
	}
	var note workflowstore.ProjectNote
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		note, err = tx.CreateProjectNote(ctx, workflowstore.CreateProjectNoteParams{
			NoteID:       s.ids.NoteID(),
			ProjectRowID: project.ID,
			Title:        title,
			Body:         body,
		})
		return err
	})
	return note, err
}

func (s *Service) UpdateNote(ctx context.Context, input UpdateNoteInput) (workflowstore.ProjectNote, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	noteID := strings.TrimSpace(input.NoteID)
	if projectID == "" || noteID == "" || (input.Title == nil && input.Body == nil && input.Status == nil) {
		return workflowstore.ProjectNote{}, fmt.Errorf("%w: Project ID, note ID, and at least one changed field are required", ErrInvalidProjectRequest)
	}
	var updated workflowstore.ProjectNote
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		current, err := tx.GetProjectNoteByNoteID(ctx, noteID)
		if err != nil {
			return err
		}
		if current.ProjectRowID != project.ID {
			return sql.ErrNoRows
		}
		title := current.Title
		body := current.Body
		status := current.Status
		if input.Title != nil {
			title = strings.TrimSpace(*input.Title)
			if title == "" {
				return fmt.Errorf("%w: note title is required", ErrInvalidProjectRequest)
			}
		}
		if input.Body != nil {
			body = strings.TrimSpace(*input.Body)
			if body == "" {
				return fmt.Errorf("%w: note body is required", ErrInvalidProjectRequest)
			}
		}
		if input.Status != nil {
			status = strings.TrimSpace(*input.Status)
			if status != workflowstore.ProjectNoteStatusOpen && status != workflowstore.ProjectNoteStatusDone {
				return fmt.Errorf("%w: unsupported note status %q", ErrInvalidProjectRequest, status)
			}
		}
		updated, err = tx.UpdateProjectNote(ctx, workflowstore.UpdateProjectNoteParams{
			NoteID:       noteID,
			ProjectRowID: project.ID,
			Title:        title,
			Body:         body,
			Status:       status,
		})
		return err
	})
	return updated, err
}

func (s *Service) DeleteNote(ctx context.Context, projectID, noteID string) error {
	projectID = strings.TrimSpace(projectID)
	noteID = strings.TrimSpace(noteID)
	if projectID == "" || noteID == "" {
		return fmt.Errorf("%w: Project ID and note ID are required", ErrInvalidProjectRequest)
	}
	return s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if err != nil {
			return err
		}
		return tx.DeleteProjectNote(ctx, project.ID, noteID)
	})
}
