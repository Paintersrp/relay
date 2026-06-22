package store

import (
	"context"

	"relay/internal/store/generated"
)

type Project = generated.Project
type ProjectRepository = generated.ProjectRepository

type UpdateProjectParams struct {
	ID                  int64
	Name                string
	Description         string
	Status              string
	DefaultRepositoryID string
}

type UpsertProjectRepositoryParams struct {
	ProjectRowID     int64
	RepoID           string
	Role             string
	LocalPath        string
	RemoteLabel      string
	RemoteURL        string
	DefaultBranch    string
	AllowedRootsJSON string
	IgnoredGlobsJSON string
	MaxFileSizeBytes int64
	IncludeUntracked int64
	Enabled          int64
}

type GetProjectRepositoryByRepoIDParams struct {
	ProjectRowID int64
	RepoID       string
}

type ListProjectRepositoriesByRoleParams struct {
	ProjectRowID int64
	Role         string
}

type UpdateProjectRepositoryParams struct {
	ProjectRowID     int64
	RepoID           string
	Role             string
	LocalPath        string
	RemoteLabel      string
	RemoteURL        string
	DefaultBranch    string
	AllowedRootsJSON string
	IgnoredGlobsJSON string
	MaxFileSizeBytes int64
	IncludeUntracked int64
	Enabled          int64
}

type SetProjectRepositoryEnabledParams struct {
	ProjectRowID int64
	RepoID       string
	Enabled      bool
}

func (s *Store) CreateProject(projectID, name, description, status, defaultRepositoryID string) (*Project, error) {
	project, err := s.queries.CreateProject(context.Background(), generated.CreateProjectParams{
		ProjectID:           projectID,
		Name:                name,
		Description:         description,
		Status:              status,
		DefaultRepositoryID: defaultRepositoryID,
	})
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Store) GetProject(id int64) (*Project, error) {
	project, err := s.queries.GetProject(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Store) GetProjectByProjectID(projectID string) (*Project, error) {
	project, err := s.queries.GetProjectByProjectID(context.Background(), projectID)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Store) ListProjects(limit int64) ([]Project, error) {
	return s.queries.ListProjects(context.Background(), limit)
}

func (s *Store) ListProjectsByStatus(status string, limit int64) ([]Project, error) {
	return s.queries.ListProjectsByStatus(context.Background(), generated.ListProjectsByStatusParams{
		Status: status,
		Limit:  limit,
	})
}

func (s *Store) UpdateProject(params UpdateProjectParams) (*Project, error) {
	project, err := s.queries.UpdateProject(context.Background(), generated.UpdateProjectParams{
		Name:                params.Name,
		Description:         params.Description,
		Status:              params.Status,
		DefaultRepositoryID: params.DefaultRepositoryID,
		ID:                  params.ID,
	})
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Store) ArchiveProject(id int64) (*Project, error) {
	project, err := s.queries.ArchiveProject(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Store) UpsertProjectRepository(params UpsertProjectRepositoryParams) (*ProjectRepository, error) {
	repo, err := s.queries.UpsertProjectRepository(context.Background(), generated.UpsertProjectRepositoryParams{
		ProjectRowID:     params.ProjectRowID,
		RepoID:           params.RepoID,
		Role:             params.Role,
		LocalPath:        params.LocalPath,
		RemoteLabel:      params.RemoteLabel,
		RemoteUrl:        params.RemoteURL,
		DefaultBranch:    params.DefaultBranch,
		AllowedRootsJson: params.AllowedRootsJSON,
		IgnoredGlobsJson: params.IgnoredGlobsJSON,
		MaxFileSizeBytes: params.MaxFileSizeBytes,
		IncludeUntracked: params.IncludeUntracked,
		Enabled:          params.Enabled,
	})
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (s *Store) GetProjectRepositoryByRepoID(params GetProjectRepositoryByRepoIDParams) (*ProjectRepository, error) {
	repo, err := s.queries.GetProjectRepositoryByRepoID(context.Background(), generated.GetProjectRepositoryByRepoIDParams{
		ProjectRowID: params.ProjectRowID,
		RepoID:       params.RepoID,
	})
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (s *Store) ListProjectRepositories(projectRowID int64) ([]ProjectRepository, error) {
	return s.queries.ListProjectRepositories(context.Background(), projectRowID)
}

func (s *Store) ListEnabledProjectRepositories(projectRowID int64) ([]ProjectRepository, error) {
	return s.queries.ListEnabledProjectRepositories(context.Background(), projectRowID)
}

func (s *Store) ListProjectRepositoriesByRole(params ListProjectRepositoriesByRoleParams) ([]ProjectRepository, error) {
	return s.queries.ListProjectRepositoriesByRole(context.Background(), generated.ListProjectRepositoriesByRoleParams{
		ProjectRowID: params.ProjectRowID,
		Role:         params.Role,
	})
}

func (s *Store) UpdateProjectRepository(params UpdateProjectRepositoryParams) (*ProjectRepository, error) {
	repo, err := s.queries.UpdateProjectRepository(context.Background(), generated.UpdateProjectRepositoryParams{
		Role:             params.Role,
		LocalPath:        params.LocalPath,
		RemoteLabel:      params.RemoteLabel,
		RemoteUrl:        params.RemoteURL,
		DefaultBranch:    params.DefaultBranch,
		AllowedRootsJson: params.AllowedRootsJSON,
		IgnoredGlobsJson: params.IgnoredGlobsJSON,
		MaxFileSizeBytes: params.MaxFileSizeBytes,
		IncludeUntracked: params.IncludeUntracked,
		Enabled:          params.Enabled,
		ProjectRowID:     params.ProjectRowID,
		RepoID:           params.RepoID,
	})
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (s *Store) SetProjectRepositoryEnabled(params SetProjectRepositoryEnabledParams) (*ProjectRepository, error) {
	enabled := int64(0)
	if params.Enabled {
		enabled = 1
	}
	repo, err := s.queries.SetProjectRepositoryEnabled(context.Background(), generated.SetProjectRepositoryEnabledParams{
		Enabled:      enabled,
		ProjectRowID: params.ProjectRowID,
		RepoID:       params.RepoID,
	})
	if err != nil {
		return nil, err
	}
	return &repo, nil
}
