-- +goose Up
-- Plan attempt persistence: intent packets, plan attempts, drift reviews, and managed-plan lineage

-- Immutable intent packets: original and revision intents within a thread
CREATE TABLE IF NOT EXISTS intent_packets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    intent_packet_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL,
    project_id TEXT NOT NULL,
    intent_thread_id TEXT NOT NULL,
    root_intent_packet_id TEXT NOT NULL,
    parent_intent_packet_id TEXT,
    revision_of_plan_attempt_id TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('original', 'revision')),
    captured_from TEXT NOT NULL CHECK (captured_from IN ('planner_chat', 'revision_notes', 'imported_request')),
    captured_by TEXT NOT NULL,
    source_artifact_path TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL,
    literal_user_request TEXT NOT NULL,
    constraints_json TEXT NOT NULL DEFAULT '[]',
    redaction_status TEXT NOT NULL CHECK (redaction_status IN ('not_required', 'redacted', 'verified_no_secrets', 'blocked_sensitive_content')),
    content_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(project_row_id) REFERENCES projects(id) ON DELETE RESTRICT,
    CHECK (
        (kind = 'original' AND parent_intent_packet_id IS NULL AND revision_of_plan_attempt_id IS NULL)
        OR
        (kind = 'revision' AND parent_intent_packet_id IS NOT NULL AND revision_of_plan_attempt_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_intent_packets_project_thread ON intent_packets(project_row_id, intent_thread_id);
CREATE INDEX IF NOT EXISTS idx_intent_packets_root ON intent_packets(root_intent_packet_id);

-- Draft plan attempts: pre-submission state for plan review and approval
CREATE TABLE IF NOT EXISTS plan_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_attempt_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL,
    project_id TEXT NOT NULL,
    intent_thread_id TEXT NOT NULL,
    root_intent_packet_id TEXT NOT NULL,
    current_intent_packet_id TEXT NOT NULL,
    supersedes_plan_attempt_id TEXT,
    replacement_plan_attempt_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('draft', 'approved', 'submitted', 'voided', 'superseded')),
    review_state TEXT NOT NULL CHECK (review_state IN ('not_requested', 'review_packet_ready', 'external_review_submitted', 'internal_review_generated', 'approval_ready', 'revision_requested', 'blocked')),
    drift_review_mode TEXT NOT NULL DEFAULT 'disabled' CHECK (drift_review_mode IN ('disabled', 'manual', 'automatic', 'external')),
    model_tier TEXT NOT NULL DEFAULT 'standard' CHECK (model_tier IN ('economy', 'standard', 'high_assurance', 'auto_escalate')),
    plan_json_artifact_path TEXT NOT NULL,
    plan_json_artifact_sha256 TEXT NOT NULL,
    raw_plan_json TEXT NOT NULL,
    raw_plan_json_hash TEXT NOT NULL,
    plan_markdown_artifact_path TEXT,
    plan_markdown_artifact_sha256 TEXT,
    accepted_drift_review_id TEXT,
    submitted_plan_row_id INTEGER,
    submitted_plan_id TEXT,
    submitted_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(project_row_id) REFERENCES projects(id) ON DELETE RESTRICT,
    FOREIGN KEY(submitted_plan_row_id) REFERENCES plans(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_plan_attempts_project_status ON plan_attempts(project_row_id, status);
CREATE INDEX IF NOT EXISTS idx_plan_attempts_thread ON plan_attempts(intent_thread_id);
CREATE INDEX IF NOT EXISTS idx_plan_attempts_current_intent ON plan_attempts(current_intent_packet_id);

-- External/internal drift review evidence storage
CREATE TABLE IF NOT EXISTS intent_drift_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    intent_drift_review_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL,
    project_id TEXT NOT NULL,
    plan_attempt_row_id INTEGER NOT NULL,
    plan_attempt_id TEXT NOT NULL,
    intent_thread_id TEXT NOT NULL,
    root_intent_packet_id TEXT NOT NULL,
    reviewed_intent_packet_id TEXT NOT NULL,
    review_packet_hash TEXT NOT NULL,
    review_source TEXT NOT NULL CHECK (review_source IN ('external', 'internal')),
    submitted_by TEXT NOT NULL,
    source_artifact_path TEXT NOT NULL DEFAULT '',
    overall_alignment TEXT NOT NULL CHECK (overall_alignment IN ('aligned', 'minor_drift', 'major_drift', 'unclear')),
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    findings_json TEXT NOT NULL DEFAULT '[]',
    recommended_action TEXT NOT NULL CHECK (recommended_action IN ('approve', 'approve_with_acknowledgement', 'revise', 'void', 'manual_review')),
    approval_gate_status TEXT NOT NULL CHECK (approval_gate_status IN ('not_required', 'ready', 'acknowledgement_required', 'revision_required', 'blocked')),
    model_metadata_json TEXT,
    input_hash TEXT NOT NULL,
    output_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(project_row_id) REFERENCES projects(id) ON DELETE RESTRICT,
    FOREIGN KEY(plan_attempt_row_id) REFERENCES plan_attempts(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_intent_drift_reviews_attempt ON intent_drift_reviews(plan_attempt_row_id);
CREATE INDEX IF NOT EXISTS idx_intent_drift_reviews_thread ON intent_drift_reviews(intent_thread_id);

-- Managed plan lineage: trace submitted plans back to their originating attempts
ALTER TABLE plans ADD COLUMN submitted_plan_attempt_id TEXT;
ALTER TABLE plans ADD COLUMN intent_thread_id TEXT;
ALTER TABLE plans ADD COLUMN root_intent_packet_id TEXT;
ALTER TABLE plans ADD COLUMN submitted_intent_packet_id TEXT;
ALTER TABLE plans ADD COLUMN accepted_drift_review_id TEXT;

-- +goose Down
-- SQLite cannot reliably drop columns without table rebuild
-- These tables are intentionally retained for audit trail
DROP TABLE IF EXISTS intent_drift_reviews;
DROP TABLE IF EXISTS plan_attempts;
DROP TABLE IF EXISTS intent_packets;

-- Note: Column removal from plans requires table rebuild
-- leaving added columns in place for compatibility