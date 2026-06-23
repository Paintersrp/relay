-- +goose Up
CREATE TABLE local_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('recent_commit', 'selected_pass_changes', 'feature_slice', 'full_repository')),
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('created', 'partial', 'blocked')),
    plan_id TEXT NOT NULL DEFAULT '',
    pass_id TEXT NOT NULL DEFAULT '',
    source_snapshot_id TEXT NOT NULL DEFAULT '',
    context_packet_id TEXT NOT NULL DEFAULT '',
    manifest_path TEXT NOT NULL,
    packet_path TEXT NOT NULL DEFAULT '',
    input_summary_path TEXT NOT NULL DEFAULT '',
    blockers_json TEXT NOT NULL DEFAULT '[]',
    warnings_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_local_audits_project_id ON local_audits(project_id);
CREATE INDEX IF NOT EXISTS idx_local_audits_mode ON local_audits(mode);
CREATE INDEX IF NOT EXISTS idx_local_audits_created_at ON local_audits(created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_local_audits_created_at;
DROP INDEX IF EXISTS idx_local_audits_mode;
DROP INDEX IF EXISTS idx_local_audits_project_id;
DROP TABLE IF EXISTS local_audits;
