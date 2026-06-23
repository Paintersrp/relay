-- +goose Up
CREATE TABLE context_packets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    context_packet_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    plan_id TEXT NOT NULL DEFAULT '',
    pass_id TEXT NOT NULL DEFAULT '',
    task_slug TEXT NOT NULL,
    source_snapshot_row_id INTEGER NOT NULL REFERENCES source_snapshots(id) ON DELETE CASCADE,
    source_snapshot_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('created', 'partial', 'blocked')),
    packet_json_path TEXT NOT NULL,
    packet_markdown_path TEXT NOT NULL,
    coverage_report_path TEXT NOT NULL,
    source_count INTEGER NOT NULL DEFAULT 0,
    covered_seed_count INTEGER NOT NULL DEFAULT 0,
    blocked_seed_count INTEGER NOT NULL DEFAULT 0,
    missing_seed_count INTEGER NOT NULL DEFAULT 0,
    truncated INTEGER NOT NULL DEFAULT 0,
    blockers_json TEXT NOT NULL DEFAULT '[]',
    summary_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE context_packet_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    context_packet_row_id INTEGER NOT NULL REFERENCES context_packets(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL,
    source_type TEXT NOT NULL,
    project_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    source_snapshot_id TEXT NOT NULL,
    path TEXT NOT NULL,
    line_start INTEGER NOT NULL DEFAULT 0,
    line_end INTEGER NOT NULL DEFAULT 0,
    content_hash TEXT NOT NULL DEFAULT '',
    snippet_hash TEXT NOT NULL DEFAULT '',
    redaction_status TEXT NOT NULL,
    truncated INTEGER NOT NULL DEFAULT 0,
    generated_at TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(context_packet_row_id, source_id)
);

CREATE INDEX IF NOT EXISTS idx_context_packets_context_packet_id ON context_packets(context_packet_id);
CREATE INDEX IF NOT EXISTS idx_context_packets_project_id ON context_packets(project_id);
CREATE INDEX IF NOT EXISTS idx_context_packets_source_snapshot_id ON context_packets(source_snapshot_id);
CREATE INDEX IF NOT EXISTS idx_context_packet_sources_packet_row_id ON context_packet_sources(context_packet_row_id);
CREATE INDEX IF NOT EXISTS idx_context_packet_sources_path ON context_packet_sources(path);

-- +goose Down
DROP TABLE IF EXISTS context_packet_sources;
DROP TABLE IF EXISTS context_packets;
