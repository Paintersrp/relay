-- +goose Up
CREATE TABLE run_submission_provenance (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,
    planner_handoff_sha256 TEXT NOT NULL,
    planner_handoff_bytes INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT '',
    client_trace_id TEXT NOT NULL DEFAULT '',
    source_artifact_path TEXT NOT NULL DEFAULT '',
    repo_target TEXT NOT NULL DEFAULT '',
    branch_context TEXT NOT NULL DEFAULT '',
    plan_id TEXT NOT NULL DEFAULT '',
    pass_id TEXT NOT NULL DEFAULT '',
    plan_row_id INTEGER REFERENCES plans(id),
    plan_pass_row_id INTEGER REFERENCES plan_passes(id),
    managed_plan_pass TEXT NOT NULL DEFAULT '',
    managed_plan_pass_name TEXT NOT NULL DEFAULT '',
    context_packet_id TEXT NOT NULL DEFAULT '',
    source_snapshot_id TEXT NOT NULL DEFAULT '',
    handoff_metadata_json TEXT NOT NULL DEFAULT '{}',
    submission_args_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_run_submission_provenance_plan_pass
    ON run_submission_provenance(plan_id, pass_id);

-- +goose Down
DROP INDEX IF EXISTS idx_run_submission_provenance_plan_pass;
DROP TABLE IF EXISTS run_submission_provenance;
