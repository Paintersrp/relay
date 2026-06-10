-- +goose Up
CREATE TABLE validation_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'starting',
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at TEXT,
    error TEXT
);

CREATE UNIQUE INDEX idx_validation_executions_one_active_per_run
ON validation_executions(run_id)
WHERE status IN ('starting', 'running');

CREATE INDEX idx_validation_executions_run_id ON validation_executions(run_id);

-- +goose Down
DROP TABLE IF EXISTS validation_executions;
