-- +goose Up
CREATE TABLE IF NOT EXISTS agent_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES runs(id),
    provider TEXT NOT NULL DEFAULT 'opencode_go',
    status TEXT NOT NULL DEFAULT 'configured',
    command_preview TEXT NOT NULL DEFAULT '',
    exit_code INTEGER,
    started_at TEXT,
    finished_at TEXT,
    stdout_artifact_path TEXT,
    stderr_artifact_path TEXT,
    combined_artifact_path TEXT,
    result_artifact_path TEXT,
    error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_executions_run_id ON agent_executions(run_id);

ALTER TABLE agent_executions ADD COLUMN runner_kind TEXT;
ALTER TABLE agent_executions ADD COLUMN owner_instance_id TEXT;
ALTER TABLE agent_executions ADD COLUMN ownership_token TEXT;
ALTER TABLE agent_executions ADD COLUMN process_id INTEGER;
ALTER TABLE agent_executions ADD COLUMN process_group_id INTEGER;
ALTER TABLE agent_executions ADD COLUMN process_identity TEXT;
ALTER TABLE agent_executions ADD COLUMN process_started_at TEXT;
ALTER TABLE agent_executions ADD COLUMN cancellation_requested_at TEXT;
ALTER TABLE agent_executions ADD COLUMN cancellation_completed_at TEXT;
ALTER TABLE agent_executions ADD COLUMN terminal_reason TEXT;
ALTER TABLE agent_executions ADD COLUMN terminalized_at TEXT;

CREATE INDEX idx_agent_executions_active_ownership
ON agent_executions(status, terminalized_at, owner_instance_id);

CREATE INDEX idx_agent_executions_owner_token
ON agent_executions(owner_instance_id, ownership_token);

-- +goose Down
DROP INDEX IF EXISTS idx_agent_executions_owner_token;
DROP INDEX IF EXISTS idx_agent_executions_active_ownership;

ALTER TABLE agent_executions DROP COLUMN terminalized_at;
ALTER TABLE agent_executions DROP COLUMN terminal_reason;
ALTER TABLE agent_executions DROP COLUMN cancellation_completed_at;
ALTER TABLE agent_executions DROP COLUMN cancellation_requested_at;
ALTER TABLE agent_executions DROP COLUMN process_started_at;
ALTER TABLE agent_executions DROP COLUMN process_identity;
ALTER TABLE agent_executions DROP COLUMN process_group_id;
ALTER TABLE agent_executions DROP COLUMN process_id;
ALTER TABLE agent_executions DROP COLUMN ownership_token;
ALTER TABLE agent_executions DROP COLUMN owner_instance_id;
ALTER TABLE agent_executions DROP COLUMN runner_kind;
