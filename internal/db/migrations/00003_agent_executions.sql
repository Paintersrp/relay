-- +goose Up
CREATE TABLE agent_executions (
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

CREATE INDEX idx_agent_executions_run_id ON agent_executions(run_id);

-- +goose Down
DROP TABLE IF EXISTS agent_executions;