-- name: CreateEvent :one
INSERT INTO events (run_id, level, message, metadata_json) VALUES (?, ?, ?, ?) RETURNING *;

-- name: ListEventsByRun :many
SELECT * FROM events WHERE run_id = ? ORDER BY created_at DESC;
