-- name: CreateSourceSnapshot :one
INSERT INTO source_snapshots (
  source_snapshot_id,
  project_row_id,
  project_id,
  snapshot_kind,
  status,
  completed_at,
  summary_json
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSourceSnapshotByID :one
SELECT * FROM source_snapshots WHERE source_snapshot_id = ?;

-- name: ListSourceSnapshotsByProject :many
SELECT * FROM source_snapshots WHERE project_row_id = ? ORDER BY created_at DESC, id DESC;

-- name: UpdateSourceSnapshotStatus :one
UPDATE source_snapshots
SET
  snapshot_kind = ?,
  status = ?,
  completed_at = ?,
  summary_json = ?
WHERE source_snapshot_id = ?
RETURNING *;

-- name: GetLatestSourceSnapshotForProject :one
SELECT * FROM source_snapshots WHERE project_row_id = ? ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: CreateSourceSnapshotRepository :one
INSERT INTO source_snapshot_repositories (
  source_snapshot_row_id,
  project_repository_row_id,
  repo_id,
  role,
  local_path,
  default_branch,
  current_branch,
  head_sha,
  dirty,
  staged_count,
  unstaged_count,
  untracked_count,
  changed_file_count,
  git_status_available,
  git_error,
  status_porcelain_hash
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListSourceSnapshotRepositories :many
SELECT * FROM source_snapshot_repositories WHERE source_snapshot_row_id = ? ORDER BY role ASC, repo_id ASC;

-- name: CreateSourceSnapshotFile :one
INSERT INTO source_snapshot_files (
  source_snapshot_repository_row_id,
  path,
  size_bytes,
  content_hash,
  hash_algorithm,
  tracked,
  included,
  exclusion_reason,
  redaction_status
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListSourceSnapshotFiles :many
SELECT * FROM source_snapshot_files WHERE source_snapshot_repository_row_id = ? ORDER BY path ASC;
