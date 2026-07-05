-- +goose Up
CREATE TABLE audit_packets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_packet_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    execution_attempt_row_id INTEGER NOT NULL REFERENCES execution_attempts(id) ON DELETE RESTRICT,
    artifact_row_id INTEGER NOT NULL UNIQUE REFERENCES artifacts(id) ON DELETE RESTRICT,
    base_commit TEXT NOT NULL,
    audited_commit TEXT NOT NULL,
    packet_sha256 TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'current' CHECK (status IN ('current', 'stale')),
    stale_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    superseded_at TEXT,
    CHECK (audit_packet_id GLOB 'packet-*' AND trim(audit_packet_id) = audit_packet_id),
    CHECK (length(base_commit) = 40 AND base_commit NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(audited_commit) = 40 AND audited_commit NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(packet_sha256) = 64 AND packet_sha256 NOT GLOB '*[^0-9a-f]*'),
    CHECK (
        (status = 'current' AND superseded_at IS NULL AND stale_reason = '') OR
        (status = 'stale' AND superseded_at IS NOT NULL AND stale_reason <> '')
    ),
    UNIQUE (run_row_id, packet_sha256)
);

CREATE UNIQUE INDEX idx_audit_packets_current_run
ON audit_packets(run_row_id)
WHERE status = 'current';

CREATE INDEX idx_audit_packets_run_created
ON audit_packets(run_row_id, created_at);

DROP TRIGGER IF EXISTS run_status_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER run_status_transition_guard
BEFORE UPDATE OF status ON runs
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'created' AND NEW.status = 'setup_ready') OR
    (OLD.status = 'setup_ready' AND NEW.status IN ('executing', 'cancelled')) OR
    (OLD.status = 'executing' AND NEW.status IN ('execution_failed', 'cancelled', 'validating')) OR
    (OLD.status = 'execution_failed' AND NEW.status IN ('executing', 'validating')) OR
    (OLD.status = 'cancelled' AND NEW.status = 'executing') OR
    (OLD.status = 'validating' AND NEW.status IN ('executing', 'validation_failed', 'audit_ready')) OR
    (OLD.status = 'validation_failed' AND NEW.status = 'needs_revision') OR
    (OLD.status = 'audit_ready' AND NEW.status IN ('executing', 'needs_revision', 'completed'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid run status transition');
END;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS audit_decision_guard;

-- +goose StatementBegin
CREATE TRIGGER audit_decision_guard
BEFORE INSERT ON audit_decisions
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM runs
    JOIN artifacts ON artifacts.id = NEW.audit_packet_artifact_row_id
    JOIN audit_packets ON audit_packets.artifact_row_id = artifacts.id
    WHERE runs.id = NEW.run_row_id
      AND runs.status = 'audit_ready'
      AND artifacts.owner_type = 'run'
      AND artifacts.run_row_id = runs.id
      AND artifacts.kind = 'audit_packet'
      AND artifacts.sha256 = NEW.packet_sha256
      AND audit_packets.run_row_id = runs.id
      AND audit_packets.status = 'current'
      AND audit_packets.packet_sha256 = NEW.packet_sha256
      AND audit_packets.audited_commit = NEW.audited_commit
)
BEGIN
    SELECT RAISE(ABORT, 'audit decision requires the current matching run audit packet');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS audit_decision_guard;

-- +goose StatementBegin
CREATE TRIGGER audit_decision_guard
BEFORE INSERT ON audit_decisions
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM runs
    JOIN artifacts ON artifacts.id = NEW.audit_packet_artifact_row_id
    WHERE runs.id = NEW.run_row_id
      AND runs.status = 'audit_ready'
      AND artifacts.owner_type = 'run'
      AND artifacts.run_row_id = runs.id
      AND artifacts.kind = 'audit_packet'
      AND artifacts.sha256 = NEW.packet_sha256
)
BEGIN
    SELECT RAISE(ABORT, 'audit decision requires the matching run-owned audit packet');
END;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS run_status_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER run_status_transition_guard
BEFORE UPDATE OF status ON runs
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'created' AND NEW.status = 'setup_ready') OR
    (OLD.status = 'setup_ready' AND NEW.status IN ('executing', 'cancelled')) OR
    (OLD.status = 'executing' AND NEW.status IN ('execution_failed', 'cancelled', 'validating')) OR
    (OLD.status = 'execution_failed' AND NEW.status IN ('executing', 'validating')) OR
    (OLD.status = 'cancelled' AND NEW.status = 'executing') OR
    (OLD.status = 'validating' AND NEW.status IN ('validation_failed', 'audit_ready')) OR
    (OLD.status = 'validation_failed' AND NEW.status = 'needs_revision') OR
    (OLD.status = 'audit_ready' AND NEW.status IN ('needs_revision', 'completed'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid run status transition');
END;
-- +goose StatementEnd

DROP INDEX IF EXISTS idx_audit_packets_run_created;
DROP INDEX IF EXISTS idx_audit_packets_current_run;
DROP TABLE IF EXISTS audit_packets;
