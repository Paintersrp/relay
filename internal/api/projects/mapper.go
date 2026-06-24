package projects

import (
	"encoding/json"
	"strings"

	"relay/internal/api/shared"
	appprojects "relay/internal/app/projects"
)

func mapProjectToAPI(project appprojects.Project) ProjectAPIProject {
	return ProjectAPIProject{
		ProjectID:           project.ProjectID,
		Name:                project.Name,
		Description:         project.Description,
		Status:              project.Status,
		DefaultRepositoryID: project.DefaultRepositoryID,
		CreatedAt:           shared.ParseAndFormatTime(project.CreatedAt),
		UpdatedAt:           shared.ParseAndFormatTime(project.UpdatedAt),
	}
}

func mapProjectRepositoriesToAPI(rows []appprojects.ProjectRepository) []ProjectAPIRepository {
	items := make([]ProjectAPIRepository, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapProjectRepositoryToAPI(row))
	}
	return items
}

func mapProjectRepositoryToAPI(repo appprojects.ProjectRepository) ProjectAPIRepository {
	return ProjectAPIRepository{
		RepoID:           repo.RepoID,
		Role:             repo.Role,
		LocalPath:        repo.LocalPath,
		RemoteLabel:      repo.RemoteLabel,
		RemoteURL:        repo.RemoteUrl,
		DefaultBranch:    repo.DefaultBranch,
		AllowedRoots:     decodeJSONStringArray(repo.AllowedRootsJson),
		IgnoredGlobs:     decodeJSONStringArray(repo.IgnoredGlobsJson),
		MaxFileSizeBytes: repo.MaxFileSizeBytes,
		IncludeUntracked: repo.IncludeUntracked == 1,
		Enabled:          repo.Enabled == 1,
		CreatedAt:        shared.ParseAndFormatTime(repo.CreatedAt),
		UpdatedAt:        shared.ParseAndFormatTime(repo.UpdatedAt),
	}
}

func decodeJSONStringArray(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	if values == nil {
		return []string{}
	}
	return values
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func projectAPIProjectPtr(project ProjectAPIProject) *ProjectAPIProject {
	return &project
}

func projectAPIRepositoryPtr(repo ProjectAPIRepository) *ProjectAPIRepository {
	return &repo
}
