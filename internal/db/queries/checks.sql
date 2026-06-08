-- name: CreateCheck :one
INSERT INTO checks (run_id, kind, status, summary, details_json) VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: GetChecksByRunKind :many
SELECT * FROM checks WHERE run_id = ? AND kind = ? ORDER BY created_at DESC;

-- name: ListChecksByRun :many
SELECT * FROM checks WHERE run_id = ? ORDER BY created_at DESC;

-- name: DeleteChecksByRunKind :exec
DELETE FROM checks WHERE run_id = ? AND kind = ?;
