-- name: CreateArtifact :one
INSERT INTO artifacts (run_id, kind, path, mime_type) VALUES (?, ?, ?, ?) RETURNING *;

-- name: GetArtifact :one
SELECT * FROM artifacts WHERE id = ?;

-- name: GetArtifactByRunKind :one
SELECT * FROM artifacts WHERE run_id = ? AND kind = ? ORDER BY created_at DESC LIMIT 1;

-- name: ListArtifactsByRun :many
SELECT * FROM artifacts WHERE run_id = ? ORDER BY created_at DESC;

-- name: ListArtifactsByRunKind :many
SELECT * FROM artifacts WHERE run_id = ? AND kind = ? ORDER BY created_at DESC;
