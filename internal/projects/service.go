package projects

import (
	"context"
	"database/sql"
	"errors"

	"relay/internal/store"
)

type Service struct {
	store *store.Store
}

func NewService(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) CreateProject(ctx context.Context, input ProjectInput) (*store.Project, []ProjectValidationIssue, error) {
	_ = ctx

	normalized, issues := NormalizeProjectInput(input)
	if len(issues) > 0 {
		return nil, issues, nil
	}

	existing, err := s.store.GetProjectByProjectID(normalized.ProjectID)
	if err == nil && existing != nil {
		return nil, []ProjectValidationIssue{validationIssue("project_id", "duplicate", "project_id already exists")}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	project, err := s.store.CreateProject(
		normalized.ProjectID,
		normalized.Name,
		normalized.Description,
		normalized.Status,
		normalized.DefaultRepositoryID,
	)
	if err != nil {
		return nil, nil, err
	}

	return project, nil, nil
}

func (s *Service) GetProjectByProjectID(ctx context.Context, projectID string) (*store.Project, error) {
	_ = ctx
	return s.store.GetProjectByProjectID(projectID)
}

func (s *Service) ListProjects(ctx context.Context, limit int64) ([]store.Project, error) {
	_ = ctx
	if limit <= 0 {
		limit = DefaultListProjectsLimit
	}
	return s.store.ListProjects(limit)
}

func (s *Service) UpsertProjectRepository(ctx context.Context, projectID string, input ProjectRepositoryInput) (*store.ProjectRepository, []ProjectValidationIssue, error) {
	_ = ctx

	input.ProjectID = projectID
	normalized, issues := NormalizeProjectRepositoryInput(input)
	if len(issues) > 0 {
		return nil, issues, nil
	}

	project, err := s.store.GetProjectByProjectID(normalized.ProjectID)
	if err != nil {
		return nil, nil, err
	}

	repo, err := s.store.UpsertProjectRepository(store.UpsertProjectRepositoryParams{
		ProjectRowID:     project.ID,
		RepoID:           normalized.RepoID,
		Role:             normalized.Role,
		LocalPath:        normalized.LocalPath,
		RemoteLabel:      normalized.RemoteLabel,
		RemoteURL:        normalized.RemoteURL,
		DefaultBranch:    normalized.DefaultBranch,
		AllowedRootsJSON: normalized.AllowedRootsJSON,
		IgnoredGlobsJSON: normalized.IgnoredGlobsJSON,
		MaxFileSizeBytes: normalized.MaxFileSizeBytes,
		IncludeUntracked: normalized.IncludeUntrackedInt,
		Enabled:          normalized.EnabledInt,
	})
	if err != nil {
		return nil, nil, err
	}

	return repo, nil, nil
}

func (s *Service) ListProjectRepositories(ctx context.Context, projectID string) ([]store.ProjectRepository, error) {
	_ = ctx

	project, err := s.store.GetProjectByProjectID(projectID)
	if err != nil {
		return nil, err
	}
	return s.store.ListProjectRepositories(project.ID)
}

func (s *Service) SetProjectRepositoryEnabled(ctx context.Context, projectID, repoID string, enabled bool) (*store.ProjectRepository, error) {
	_ = ctx

	project, err := s.store.GetProjectByProjectID(projectID)
	if err != nil {
		return nil, err
	}
	return s.store.SetProjectRepositoryEnabled(store.SetProjectRepositoryEnabledParams{
		ProjectRowID: project.ID,
		RepoID:       repoID,
		Enabled:      enabled,
	})
}
