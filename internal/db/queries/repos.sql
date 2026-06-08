-- name: CreateRepo :one
INSERT INTO repos (name, path, default_validation_commands) VALUES (?, ?, ?) RETURNING *;

-- name: GetRepo :one
SELECT * FROM repos WHERE id = ?;

-- name: ListRepos :many
SELECT * FROM repos ORDER BY updated_at DESC;

-- name: GetRepoByName :one
SELECT * FROM repos WHERE name = ?;

-- name: UpdateRepo :one
UPDATE repos SET name = ?, path = ?, default_validation_commands = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;
