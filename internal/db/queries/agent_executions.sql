-- name: CreateAgentExecution :one
INSERT INTO agent_executions (
    run_id, provider, status, command_preview,
    runner_kind, owner_instance_id, ownership_token
)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *;

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

-- name: GetActiveAgentExecutionByRun :one
SELECT * FROM agent_executions
WHERE run_id = ?
  AND status IN ('starting', 'running', 'cancel_requested')
  AND terminalized_at IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: ListActiveAgentExecutions :many
SELECT * FROM agent_executions
WHERE status IN ('starting', 'running', 'cancel_requested')
  AND terminalized_at IS NULL
ORDER BY created_at ASC;

-- name: RegisterAgentExecutionProcess :one
UPDATE agent_executions
SET status = 'running',
    process_id = ?,
    process_group_id = ?,
    process_identity = ?,
    process_started_at = ?,
    started_at = ?,
    updated_at = datetime('now')
WHERE id = ?
  AND ownership_token = ?
  AND status = 'starting'
  AND terminalized_at IS NULL
RETURNING *;

-- name: RequestAgentExecutionCancellation :one
UPDATE agent_executions
SET status = 'cancel_requested',
    cancellation_requested_at = COALESCE(cancellation_requested_at, ?),
    updated_at = datetime('now')
WHERE id = ?
  AND status IN ('starting', 'running', 'cancel_requested')
  AND terminalized_at IS NULL
RETURNING *;

-- name: TerminalizeAgentExecutionCAS :one
UPDATE agent_executions
SET status = ?,
    exit_code = ?,
    started_at = COALESCE(started_at, ?),
    finished_at = ?,
    stdout_artifact_path = ?,
    stderr_artifact_path = ?,
    combined_artifact_path = ?,
    result_artifact_path = ?,
    error = ?,
    cancellation_completed_at = ?,
    terminal_reason = ?,
    terminalized_at = ?,
    updated_at = datetime('now')
WHERE id = ?
  AND status IN ('starting', 'running', 'cancel_requested')
  AND terminalized_at IS NULL
RETURNING *;
