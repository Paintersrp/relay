-- name: CreateRun :one
INSERT INTO runs (repo_id, title, status, recommended_model, selected_model, executor_adapter, branch_name, base_commit, head_commit)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = ?;

-- name: ListRecentRuns :many
SELECT * FROM runs ORDER BY updated_at DESC LIMIT ?;

-- name: ListRecentRunsWithRepo :many
SELECT
  runs.id,
  runs.repo_id,
  runs.title,
  runs.status,
  runs.recommended_model,
  runs.selected_model,
  runs.executor_adapter,
  runs.branch_name,
  runs.base_commit,
  runs.head_commit,
  runs.created_at,
  runs.updated_at,
  repos.name AS repo_name
FROM runs
JOIN repos ON repos.id = runs.repo_id
ORDER BY runs.updated_at DESC
LIMIT ?;

-- name: ListRunsByRepo :many
SELECT * FROM runs WHERE repo_id = ? ORDER BY updated_at DESC;

-- name: UpdateRunStatus :one
UPDATE runs SET status = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateRunModel :one
UPDATE runs SET recommended_model = ?, selected_model = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateRunBranch :one
UPDATE runs SET branch_name = ?, base_commit = ?, head_commit = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateRunTitle :one
UPDATE runs SET title = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateRunRepo :one
UPDATE runs SET repo_id = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateRunExecutorAdapter :one
UPDATE runs SET executor_adapter = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;
