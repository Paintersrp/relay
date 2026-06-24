package projects

import "relay/internal/store"

const (
	RepositoryRolePrimary    = "primary"
	RepositoryRoleReference  = "reference"
	RepositoryRoleContracts  = "contracts"
	RepositoryRoleDocs       = "docs"
	ProjectStatusActive      = "active"
	ProjectStatusArchived    = "archived"
	DefaultBranch            = "main"
	DefaultMaxFileSizeBytes  = 262144
	MinMaxFileSizeBytes      = 1024
	MaxAllowedFileSizeBytes  = 10485760
	DefaultListProjectsLimit = 50
)

type Project = store.Project
type ProjectRepository = store.ProjectRepository

type ProjectInput struct {
	ProjectID           string `json:"project_id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Status              string `json:"status"`
	DefaultRepositoryID string `json:"default_repository_id"`
}

type ProjectRepositoryInput struct {
	ProjectID        string   `json:"project_id"`
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
	Enabled          bool     `json:"enabled"`
}

type ProjectValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NormalizedProjectInput struct {
	ProjectID           string
	Name                string
	Description         string
	Status              string
	DefaultRepositoryID string
}

type NormalizedProjectRepositoryInput struct {
	ProjectID           string
	RepoID              string
	Role                string
	LocalPath           string
	RemoteLabel         string
	RemoteURL           string
	DefaultBranch       string
	AllowedRoots        []string
	IgnoredGlobs        []string
	AllowedRootsJSON    string
	IgnoredGlobsJSON    string
	MaxFileSizeBytes    int64
	IncludeUntracked    bool
	Enabled             bool
	IncludeUntrackedInt int64
	EnabledInt          int64
}
