-- +goose Up
ALTER TABLE agent_executions ADD COLUMN launch_state TEXT NOT NULL DEFAULT 'created';
ALTER TABLE agent_executions ADD COLUMN termination_state TEXT NOT NULL DEFAULT 'none';
ALTER TABLE agent_executions ADD COLUMN termination_requested_reason TEXT;
ALTER TABLE agent_executions ADD COLUMN termination_attempted_at TEXT;
ALTER TABLE agent_executions ADD COLUMN termination_verified_at TEXT;
ALTER TABLE agent_executions ADD COLUMN termination_last_error TEXT;
ALTER TABLE agent_executions ADD COLUMN platform_ownership_id TEXT;

UPDATE agent_executions
SET launch_state = CASE
        WHEN process_identity IS NOT NULL THEN 'registered'
        WHEN terminalized_at IS NOT NULL AND process_identity IS NULL THEN 'start_prevented'
        ELSE 'created'
    END,
    termination_state = CASE
        WHEN terminalized_at IS NOT NULL THEN 'verified_absent'
        ELSE 'none'
    END,
    termination_verified_at = CASE
        WHEN terminalized_at IS NOT NULL THEN terminalized_at
        ELSE NULL
    END
WHERE launch_state = 'created'
  AND termination_state = 'none';

CREATE INDEX idx_agent_executions_launch_state
ON agent_executions(launch_state, status, terminalized_at);

CREATE INDEX idx_agent_executions_termination_state
ON agent_executions(termination_state, status, terminalized_at);

-- +goose Down
DROP INDEX IF EXISTS idx_agent_executions_termination_state;
DROP INDEX IF EXISTS idx_agent_executions_launch_state;

ALTER TABLE agent_executions DROP COLUMN platform_ownership_id;
ALTER TABLE agent_executions DROP COLUMN termination_last_error;
ALTER TABLE agent_executions DROP COLUMN termination_verified_at;
ALTER TABLE agent_executions DROP COLUMN termination_attempted_at;
ALTER TABLE agent_executions DROP COLUMN termination_requested_reason;
ALTER TABLE agent_executions DROP COLUMN termination_state;
ALTER TABLE agent_executions DROP COLUMN launch_state;
