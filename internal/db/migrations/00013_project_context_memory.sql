-- +goose Up
CREATE TABLE project_context_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    context_record_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    body_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    importance TEXT NOT NULL DEFAULT 'normal',
    tags_json TEXT NOT NULL DEFAULT '[]',
    source TEXT NOT NULL DEFAULT 'chat',
    created_by TEXT NOT NULL DEFAULT 'chat_agent',
    dedupe_reason TEXT NOT NULL DEFAULT '',
    redaction_status TEXT NOT NULL DEFAULT 'not_needed',
    supersedes_record_id TEXT NOT NULL DEFAULT '',
    superseded_by_record_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK (kind IN ('decision', 'constraint', 'architecture_rationale', 'operator_preference', 'project_principle', 'risk', 'terminology', 'supersession', 'open_question')),
    CHECK (status IN ('active', 'superseded', 'archived')),
    CHECK (importance IN ('low', 'normal', 'high', 'critical')),
    CHECK (redaction_status IN ('not_needed', 'redacted'))
);

CREATE INDEX idx_project_context_records_project_status
    ON project_context_records(project_row_id, status, updated_at DESC);

CREATE INDEX idx_project_context_records_project_kind
    ON project_context_records(project_row_id, kind, updated_at DESC);

CREATE INDEX idx_project_context_records_project_importance
    ON project_context_records(project_row_id, importance, updated_at DESC);

CREATE UNIQUE INDEX idx_project_context_records_project_body_hash
    ON project_context_records(project_row_id, body_hash)
    WHERE status = 'active';

-- +goose Down
DROP INDEX IF EXISTS idx_project_context_records_project_body_hash;
DROP INDEX IF EXISTS idx_project_context_records_project_importance;
DROP INDEX IF EXISTS idx_project_context_records_project_kind;
DROP INDEX IF EXISTS idx_project_context_records_project_status;
DROP TABLE IF EXISTS project_context_records;
