package projects

import (
	"context"
	"database/sql"
	"errors"
	"strings"

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

// UpdateProjectRepository applies an update to an existing project repository.
// It owns the store interaction the project API handler previously performed
// directly: it resolves the project, loads the existing repository row, applies
// route-default behavior (blank request repo_id falls back to the route repoID,
// and an omitted enabled flag preserves the current enabled state), then
// upserts. unknown project or repository surfaces sql.ErrNoRows to the caller.
func (s *Service) UpdateProjectRepository(ctx context.Context, projectID, repoID string, input ProjectRepositoryInput, enabledProvided bool) (*store.ProjectRepository, []ProjectValidationIssue, error) {
	_ = ctx

	project, err := s.store.GetProjectByProjectID(projectID)
	if err != nil {
		return nil, nil, err
	}

	existing, err := s.store.GetProjectRepositoryByRepoID(store.GetProjectRepositoryByRepoIDParams{
		ProjectRowID: project.ID,
		RepoID:       repoID,
	})
	if err != nil {
		return nil, nil, err
	}

	if strings.TrimSpace(input.RepoID) == "" {
		input.RepoID = repoID
	}

	enabled := existing.Enabled == 1
	if enabledProvided {
		enabled = input.Enabled
	}
	input.Enabled = enabled
	input.ProjectID = projectID

	return s.UpsertProjectRepository(ctx, projectID, input)
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
