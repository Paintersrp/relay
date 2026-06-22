-- name: CreateProject :one
INSERT INTO projects (
  project_id,
  name,
  description,
  status,
  default_repository_id
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: GetProjectByProjectID :one
SELECT * FROM projects WHERE project_id = ?;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY updated_at DESC, id DESC LIMIT ?;

-- name: ListProjectsByStatus :many
SELECT * FROM projects WHERE status = ? ORDER BY updated_at DESC, id DESC LIMIT ?;

-- name: UpdateProject :one
UPDATE projects
SET
  name = ?,
  description = ?,
  status = ?,
  default_repository_id = ?,
  updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: ArchiveProject :one
UPDATE projects
SET
  status = 'archived',
  updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: CreateProjectRepository :one
INSERT INTO project_repositories (
  project_row_id,
  repo_id,
  role,
  local_path,
  remote_label,
  remote_url,
  default_branch,
  allowed_roots_json,
  ignored_globs_json,
  max_file_size_bytes,
  include_untracked,
  enabled
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpsertProjectRepository :one
INSERT INTO project_repositories (
  project_row_id,
  repo_id,
  role,
  local_path,
  remote_label,
  remote_url,
  default_branch,
  allowed_roots_json,
  ignored_globs_json,
  max_file_size_bytes,
  include_untracked,
  enabled
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_row_id, repo_id) DO UPDATE SET
  role = excluded.role,
  local_path = excluded.local_path,
  remote_label = excluded.remote_label,
  remote_url = excluded.remote_url,
  default_branch = excluded.default_branch,
  allowed_roots_json = excluded.allowed_roots_json,
  ignored_globs_json = excluded.ignored_globs_json,
  max_file_size_bytes = excluded.max_file_size_bytes,
  include_untracked = excluded.include_untracked,
  enabled = excluded.enabled,
  updated_at = datetime('now')
RETURNING *;

-- name: GetProjectRepository :one
SELECT * FROM project_repositories WHERE id = ?;

-- name: GetProjectRepositoryByRepoID :one
SELECT * FROM project_repositories WHERE project_row_id = ? AND repo_id = ?;

-- name: ListProjectRepositories :many
SELECT * FROM project_repositories WHERE project_row_id = ? ORDER BY role ASC, repo_id ASC;

-- name: ListEnabledProjectRepositories :many
SELECT * FROM project_repositories WHERE project_row_id = ? AND enabled = 1 ORDER BY role ASC, repo_id ASC;

-- name: ListProjectRepositoriesByRole :many
SELECT * FROM project_repositories WHERE project_row_id = ? AND role = ? ORDER BY repo_id ASC;

-- name: UpdateProjectRepository :one
UPDATE project_repositories
SET
  role = ?,
  local_path = ?,
  remote_label = ?,
  remote_url = ?,
  default_branch = ?,
  allowed_roots_json = ?,
  ignored_globs_json = ?,
  max_file_size_bytes = ?,
  include_untracked = ?,
  enabled = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND repo_id = ?
RETURNING *;

-- name: SetProjectRepositoryEnabled :one
UPDATE project_repositories
SET
  enabled = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND repo_id = ?
RETURNING *;
