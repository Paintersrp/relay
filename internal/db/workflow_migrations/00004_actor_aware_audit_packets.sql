-- +goose Up
PRAGMA foreign_keys=off;
DROP TRIGGER IF EXISTS audit_decision_guard;

CREATE TABLE audit_packets_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_packet_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    implementation_actor_kind TEXT NOT NULL DEFAULT 'executor' CHECK (implementation_actor_kind IN ('applier','executor','hybrid')),
    execution_attempt_row_id INTEGER REFERENCES execution_attempts(id) ON DELETE RESTRICT,
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
        (implementation_actor_kind = 'applier' AND execution_attempt_row_id IS NULL) OR
        (implementation_actor_kind IN ('executor','hybrid') AND execution_attempt_row_id IS NOT NULL)
    ),
    CHECK (
        (status = 'current' AND superseded_at IS NULL AND stale_reason = '') OR
        (status = 'stale' AND superseded_at IS NOT NULL AND stale_reason <> '')
    ),
    UNIQUE (run_row_id, packet_sha256)
);

INSERT INTO audit_packets_new (
    id, audit_packet_id, run_row_id, implementation_actor_kind, execution_attempt_row_id,
    artifact_row_id, base_commit, audited_commit, packet_sha256, status, stale_reason,
    created_at, superseded_at
)
SELECT id, audit_packet_id, run_row_id, 'executor', execution_attempt_row_id,
       artifact_row_id, base_commit, audited_commit, packet_sha256, status, stale_reason,
       created_at, superseded_at
FROM audit_packets;

DROP TABLE audit_packets;
ALTER TABLE audit_packets_new RENAME TO audit_packets;

CREATE UNIQUE INDEX idx_audit_packets_current_run
ON audit_packets(run_row_id)
WHERE status = 'current';
CREATE INDEX idx_audit_packets_run_created
ON audit_packets(run_row_id, created_at);

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

PRAGMA foreign_keys=on;

-- +goose Down
DROP INDEX IF EXISTS idx_audit_packets_run_created;
DROP INDEX IF EXISTS idx_audit_packets_current_run;
DROP TABLE IF EXISTS audit_packets;
