package projects

import appprojects "relay/internal/app/projects"

type ProjectAPIProject struct {
	ProjectID           string                 `json:"projectId"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	Status              string                 `json:"status"`
	DefaultRepositoryID string                 `json:"defaultRepositoryId"`
	CreatedAt           string                 `json:"createdAt"`
	UpdatedAt           string                 `json:"updatedAt"`
	Repositories        []ProjectAPIRepository `json:"repositories,omitempty"`
}

type ProjectAPIRepository struct {
	RepoID           string   `json:"repoId"`
	Role             string   `json:"role"`
	LocalPath        string   `json:"localPath"`
	RemoteLabel      string   `json:"remoteLabel"`
	RemoteURL        string   `json:"remoteUrl"`
	DefaultBranch    string   `json:"defaultBranch"`
	AllowedRoots     []string `json:"allowedRoots"`
	IgnoredGlobs     []string `json:"ignoredGlobs"`
	MaxFileSizeBytes int64    `json:"maxFileSizeBytes"`
	IncludeUntracked bool     `json:"includeUntracked"`
	Enabled          bool     `json:"enabled"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
}

type ProjectAPIResponse struct {
	Success      bool                                 `json:"success"`
	Count        int                                  `json:"count,omitempty"`
	Projects     []ProjectAPIProject                  `json:"projects,omitempty"`
	Project      *ProjectAPIProject                   `json:"project,omitempty"`
	Repository   *ProjectAPIRepository                `json:"repository,omitempty"`
	Repositories []ProjectAPIRepository               `json:"repositories,omitempty"`
	PlanSeed     *ProjectAPIPlanSeed                  `json:"seed,omitempty"`
	PlanSeeds    []ProjectAPIPlanSeed                 `json:"seeds,omitempty"`
	Validation   []appprojects.ProjectValidationIssue `json:"validation,omitempty"`
}

type ProjectAPIRequest struct {
	ProjectID           string `json:"project_id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Status              string `json:"status"`
	DefaultRepositoryID string `json:"default_repository_id"`
}

type ProjectRepositoryAPIRequest struct {
	RepoID           string   `json:"repo_id"`
	Role             string   `json:"role"`
	LocalPath        string   `json:"local_path"`
	RemoteLabel      string   `json:"remote_label"`
	RemoteURL        string   `json:"remote_url"`
	DefaultBranch    string   `json:"default_branch"`
	AllowedRoots     []string `json:"allowed_roots"`
	IgnoredGlobs     []string `json:"ignored_globs"`
	MaxFileSizeBytes int64    `json:"max_file_size_bytes"`
	IncludeUntracked bool     `json:"include_untracked"`
	Enabled          *bool    `json:"enabled,omitempty"`
}

type ProjectRepositoryEnabledAPIRequest struct {
	Enabled bool `json:"enabled"`
}
