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
  AND status IN ('starting', 'running', 'cancel_requested', 'termination_pending')
  AND terminalized_at IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: ListActiveAgentExecutions :many
SELECT * FROM agent_executions
WHERE status IN ('starting', 'running', 'cancel_requested', 'termination_pending')
  AND terminalized_at IS NULL
ORDER BY created_at ASC;

-- name: ClaimAgentExecutionLaunch :one
UPDATE agent_executions
SET launch_state = 'start_in_progress',
    updated_at = datetime('now')
WHERE id = ?
  AND ownership_token = ?
  AND status = 'starting'
  AND cancellation_requested_at IS NULL
  AND launch_state = 'created'
  AND terminalized_at IS NULL
RETURNING *;

-- name: RecordAgentExecutionStartPrevented :one
UPDATE agent_executions
SET launch_state = 'start_prevented',
    updated_at = datetime('now')
WHERE id = ?
  AND ownership_token = ?
  AND status IN ('starting', 'cancel_requested')
  AND launch_state IN ('created', 'start_in_progress')
  AND terminalized_at IS NULL
RETURNING *;

-- name: RegisterAgentExecutionProcess :one
UPDATE agent_executions
SET status = 'running',
    process_id = ?,
    process_group_id = ?,
    process_identity = ?,
    process_started_at = ?,
    started_at = ?,
    launch_state = 'registered',
    platform_ownership_id = ?,
    updated_at = datetime('now')
WHERE id = ?
  AND ownership_token = ?
  AND status IN ('starting', 'cancel_requested')
  AND launch_state = 'start_in_progress'
  AND terminalized_at IS NULL
RETURNING *;

-- name: MarkAgentExecutionTerminationRequested :one
UPDATE agent_executions
SET termination_state = CASE
        WHEN termination_state = 'none' THEN 'requested'
        ELSE termination_state
    END,
    termination_requested_reason = COALESCE(termination_requested_reason, ?),
    termination_attempted_at = COALESCE(termination_attempted_at, ?),
    updated_at = datetime('now')
WHERE id = ?
  AND terminalized_at IS NULL
RETURNING *;

-- name: MarkAgentExecutionTerminationFailed :one
UPDATE agent_executions
SET status = 'termination_pending',
    termination_state = 'failed',
    termination_last_error = CASE
        WHEN termination_last_error IS NULL OR termination_last_error = '' THEN ?
        ELSE termination_last_error
    END,
    updated_at = datetime('now')
WHERE id = ?
  AND terminalized_at IS NULL
RETURNING *;

-- name: MarkAgentExecutionTreeVerifiedAbsent :one
UPDATE agent_executions
SET termination_state = 'verified_absent',
    termination_verified_at = ?,
    termination_last_error = NULL,
    updated_at = datetime('now')
WHERE id = ?
  AND terminalized_at IS NULL
RETURNING *;

-- name: RequestAgentExecutionCancellation :one
UPDATE agent_executions
SET status = 'cancel_requested',
    cancellation_requested_at = COALESCE(cancellation_requested_at, ?),
    updated_at = CASE
        WHEN cancellation_requested_at IS NULL THEN datetime('now')
        ELSE updated_at
    END
WHERE id = ?
  AND status IN ('starting', 'running', 'cancel_requested', 'termination_pending')
  AND terminalized_at IS NULL
RETURNING *;

-- name: TerminalizeAgentExecutionCAS :one
UPDATE agent_executions
SET status = ?,
    exit_code = ?,
    started_at = COALESCE(started_at, ?),
    finished_at = ?,
    stdout_artifact_path = COALESCE(?, stdout_artifact_path),
    stderr_artifact_path = COALESCE(?, stderr_artifact_path),
    combined_artifact_path = COALESCE(?, combined_artifact_path),
    result_artifact_path = COALESCE(?, result_artifact_path),
    error = ?,
    cancellation_completed_at = ?,
    terminal_reason = ?,
    terminalized_at = ?,
    updated_at = datetime('now')
WHERE id = ?
  AND status IN ('starting', 'running', 'cancel_requested', 'termination_pending')
  AND (
      launch_state = 'start_prevented'
      OR termination_state = 'verified_absent'
  )
  AND terminalized_at IS NULL
RETURNING *;
