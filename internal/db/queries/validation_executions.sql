-- name: CreateValidationExecution :one
INSERT INTO validation_executions (run_id, status)
VALUES (?, 'starting')
RETURNING *;

-- name: GetValidationExecution :one
SELECT * FROM validation_executions WHERE id = ?;

-- name: GetActiveValidationExecutionByRun :one
SELECT * FROM validation_executions WHERE run_id = ? AND status IN ('starting', 'running') ORDER BY id DESC LIMIT 1;

-- name: UpdateValidationExecutionStatus :exec
UPDATE validation_executions
SET status = ?, updated_at = datetime('now'), finished_at = datetime('now'), error = ?
WHERE id = ?;

-- name: MarkValidationExecutionRunning :exec
UPDATE validation_executions
SET status = 'running', updated_at = datetime('now')
WHERE id = ? AND status = 'starting';

-- name: MarkStaleValidationExecutionsError :exec
UPDATE validation_executions
SET status = 'error', updated_at = datetime('now'), finished_at = datetime('now'), error = ?
WHERE run_id = ? AND status IN ('starting', 'running') AND updated_at < ?;
