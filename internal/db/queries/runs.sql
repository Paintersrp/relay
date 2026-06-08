-- name: CreateRun :one
INSERT INTO runs (repo_id, title, status, recommended_model, selected_model, branch_name, base_commit, head_commit)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = ?;

-- name: ListRecentRuns :many
SELECT * FROM runs ORDER BY updated_at DESC LIMIT ?;

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
