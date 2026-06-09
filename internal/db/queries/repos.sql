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

-- name: GetRepoByPath :one
SELECT * FROM repos WHERE path = ?;

-- name: UpsertDiscoveredRepo :one
INSERT INTO repos (
  name,
  path,
  default_validation_commands,
  source,
  discovered_at,
  last_seen_at
)
VALUES (?, ?, '[]', 'discovered', datetime('now'), datetime('now'))
ON CONFLICT(path) WHERE path <> ''
DO UPDATE SET
  name = excluded.name,
  last_seen_at = datetime('now'),
  updated_at = datetime('now')
RETURNING *;

-- name: ListReposByName :many
SELECT * FROM repos ORDER BY lower(name), lower(path);
