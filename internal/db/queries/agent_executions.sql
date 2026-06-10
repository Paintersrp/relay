-- name: CreateAgentExecution :one
INSERT INTO agent_executions (run_id, provider, status, command_preview)
VALUES (?, ?, ?, ?) RETURNING *;

-- name: GetAgentExecution :one
SELECT * FROM agent_executions WHERE id = ?;

-- name: ListAgentExecutionsByRun :many
SELECT * FROM agent_executions WHERE run_id = ? ORDER BY created_at DESC;

-- name: GetLatestAgentExecutionByRun :one
SELECT * FROM agent_executions WHERE run_id = ? ORDER BY created_at DESC LIMIT 1;

-- name: UpdateAgentExecutionStatus :one
UPDATE agent_executions
SET status = ?, exit_code = ?, started_at = ?, finished_at = ?,
    stdout_artifact_path = ?, stderr_artifact_path = ?,
    combined_artifact_path = ?, result_artifact_path = ?, error = ?,
    updated_at = datetime('now')
WHERE id = ? RETURNING *;