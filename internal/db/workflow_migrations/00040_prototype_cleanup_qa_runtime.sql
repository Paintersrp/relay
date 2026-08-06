-- +goose Up
-- Part 3 owns cleanup reconciliation and QA admission history. The Part 2 QA
-- association table is intentionally unchanged.
DROP INDEX IF EXISTS idx_prototype_transitions_run;
ALTER TABLE feature_workspace_prototype_lifecycle_transitions RENAME TO feature_workspace_prototype_lifecycle_transitions_part3;
CREATE TABLE feature_workspace_prototype_lifecycle_transitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    transition_identity TEXT NOT NULL UNIQUE,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    run_version INTEGER NOT NULL CHECK (run_version >= 1),
    approval_row_id INTEGER REFERENCES feature_workspace_prototype_approvals(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(run_row_id, run_version),
    CHECK ((from_state='proposed' AND to_state='approved' AND approval_row_id IS NOT NULL) OR
           (from_state='approved' AND to_state IN ('preparing','failed','closed') AND approval_row_id IS NULL) OR
           (from_state='preparing' AND to_state IN ('running','launch_uncertain','failed','closed') AND approval_row_id IS NULL) OR
           (from_state='launch_uncertain' AND to_state IN ('running','succeeded','failed','cancelled','timed_out','cleanup_required','closed') AND approval_row_id IS NULL) OR
           (from_state='running' AND to_state IN ('succeeded','failed','cancelled','timed_out','cleanup_required','closed') AND approval_row_id IS NULL) OR
           (from_state IN ('succeeded','failed','cancelled','timed_out','cleanup_required') AND to_state='closed' AND approval_row_id IS NULL))
);
INSERT INTO feature_workspace_prototype_lifecycle_transitions SELECT * FROM feature_workspace_prototype_lifecycle_transitions_part3;
DROP TABLE feature_workspace_prototype_lifecycle_transitions_part3;
CREATE INDEX idx_prototype_transitions_run ON feature_workspace_prototype_lifecycle_transitions(run_row_id,id);

CREATE TABLE feature_workspace_prototype_cleanup_reconciliations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reconciliation_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    expected_run_version INTEGER NOT NULL CHECK (expected_run_version >= 1),
    mutation_identity TEXT NOT NULL UNIQUE,
    trigger_kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','in_progress','reconciled','failed','closed')),
    detail TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    closed_at TEXT,
    CHECK (reconciliation_id GLOB 'prototype-reconciliation-*' AND trim(reconciliation_id)=reconciliation_id),
    CHECK (trim(mutation_identity)<>'' AND trim(trigger_kind)<>'')
);
CREATE INDEX idx_prototype_cleanup_reconciliations_run ON feature_workspace_prototype_cleanup_reconciliations(run_row_id,id);
CREATE INDEX idx_prototype_cleanup_reconciliations_status ON feature_workspace_prototype_cleanup_reconciliations(status,updated_at,id);

CREATE TABLE feature_workspace_prototype_qa_packet_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    packet_member_id TEXT NOT NULL UNIQUE,
    reconciliation_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_cleanup_reconciliations(id) ON DELETE RESTRICT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    member_kind TEXT NOT NULL,
    artifact_row_id INTEGER REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    sha256 TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(reconciliation_row_id,sequence),
    CHECK (packet_member_id GLOB 'prototype-qa-packet-member-*' AND trim(packet_member_id)=packet_member_id AND trim(member_kind)<>'')
);
CREATE INDEX idx_prototype_qa_packet_members_reconciliation ON feature_workspace_prototype_qa_packet_members(reconciliation_row_id,sequence);
CREATE INDEX idx_prototype_qa_packet_members_run ON feature_workspace_prototype_qa_packet_members(run_row_id,sequence);

CREATE TABLE feature_workspace_prototype_qa_admissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    admission_id TEXT NOT NULL UNIQUE,
    reconciliation_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_cleanup_reconciliations(id) ON DELETE RESTRICT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    packet_member_row_id INTEGER REFERENCES feature_workspace_prototype_qa_packet_members(id) ON DELETE RESTRICT,
    admission_kind TEXT NOT NULL,
    decision TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (admission_id GLOB 'prototype-qa-admission-*' AND trim(admission_id)=admission_id AND trim(admission_kind)<>'' AND trim(decision)<>'')
);
CREATE INDEX idx_prototype_qa_admissions_reconciliation ON feature_workspace_prototype_qa_admissions(reconciliation_row_id,id);
CREATE INDEX idx_prototype_qa_admissions_run ON feature_workspace_prototype_qa_admissions(run_row_id,id);

-- Ownership and history are database-enforced; closed reconciliations cannot be
-- mutated or used as a foreign-key parent for new QA records.
-- +goose StatementBegin
CREATE TRIGGER prototype_cleanup_reconciliation_ownership_guard BEFORE INSERT ON feature_workspace_prototype_cleanup_reconciliations FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runs WHERE id=NEW.run_row_id) BEGIN SELECT RAISE(ABORT,'prototype cleanup reconciliation ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_cleanup_reconciliation_immutable BEFORE UPDATE ON feature_workspace_prototype_cleanup_reconciliations FOR EACH ROW WHEN OLD.reconciliation_id<>NEW.reconciliation_id OR OLD.run_row_id<>NEW.run_row_id OR OLD.expected_run_version<>NEW.expected_run_version OR OLD.mutation_identity<>NEW.mutation_identity OR OLD.trigger_kind<>NEW.trigger_kind BEGIN SELECT RAISE(ABORT,'prototype cleanup reconciliation identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_cleanup_reconciliation_closed BEFORE UPDATE ON feature_workspace_prototype_cleanup_reconciliations FOR EACH ROW WHEN OLD.status='closed' AND (NEW.status<>OLD.status OR NEW.closed_at<>OLD.closed_at OR NEW.detail<>OLD.detail) BEGIN SELECT RAISE(ABORT,'prototype cleanup reconciliation is closed'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_cleanup_packet_member_ownership_guard BEFORE INSERT ON feature_workspace_prototype_qa_packet_members FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_cleanup_reconciliations c JOIN feature_workspace_prototype_runs r ON r.id=c.run_row_id WHERE c.id=NEW.reconciliation_row_id AND r.id=NEW.run_row_id AND c.status<>'closed') OR (NEW.artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_artifacts a JOIN feature_workspace_prototype_runs r ON r.workspace_row_id=a.workspace_row_id WHERE a.id=NEW.artifact_row_id AND r.id=NEW.run_row_id)) BEGIN SELECT RAISE(ABORT,'prototype QA packet member ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_packet_member_immutable BEFORE UPDATE ON feature_workspace_prototype_qa_packet_members BEGIN SELECT RAISE(ABORT,'prototype QA packet members are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_admission_ownership_guard BEFORE INSERT ON feature_workspace_prototype_qa_admissions FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_cleanup_reconciliations c JOIN feature_workspace_prototype_runs r ON r.id=c.run_row_id WHERE c.id=NEW.reconciliation_row_id AND r.id=NEW.run_row_id AND c.status<>'closed') OR (NEW.packet_member_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_qa_packet_members m WHERE m.id=NEW.packet_member_row_id AND m.reconciliation_row_id=NEW.reconciliation_row_id AND m.run_row_id=NEW.run_row_id)) BEGIN SELECT RAISE(ABORT,'prototype QA admission ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_admission_immutable BEFORE UPDATE ON feature_workspace_prototype_qa_admissions BEGIN SELECT RAISE(ABORT,'prototype QA admissions are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS prototype_qa_admission_immutable;
DROP TRIGGER IF EXISTS prototype_qa_admission_ownership_guard;
DROP TRIGGER IF EXISTS prototype_qa_packet_member_immutable;
DROP TRIGGER IF EXISTS prototype_cleanup_packet_member_ownership_guard;
DROP TRIGGER IF EXISTS prototype_cleanup_reconciliation_closed;
DROP TRIGGER IF EXISTS prototype_cleanup_reconciliation_immutable;
DROP TRIGGER IF EXISTS prototype_cleanup_reconciliation_ownership_guard;
DROP INDEX IF EXISTS idx_prototype_qa_admissions_run;
DROP INDEX IF EXISTS idx_prototype_qa_admissions_reconciliation;
DROP INDEX IF EXISTS idx_prototype_qa_packet_members_run;
DROP INDEX IF EXISTS idx_prototype_qa_packet_members_reconciliation;
DROP INDEX IF EXISTS idx_prototype_cleanup_reconciliations_status;
DROP INDEX IF EXISTS idx_prototype_cleanup_reconciliations_run;
DROP TABLE IF EXISTS feature_workspace_prototype_qa_admissions;
DROP TABLE IF EXISTS feature_workspace_prototype_qa_packet_members;
DROP TABLE IF EXISTS feature_workspace_prototype_cleanup_reconciliations;
DROP INDEX IF EXISTS idx_prototype_transitions_run;
ALTER TABLE feature_workspace_prototype_lifecycle_transitions RENAME TO feature_workspace_prototype_lifecycle_transitions_part3;
CREATE TABLE feature_workspace_prototype_lifecycle_transitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    transition_identity TEXT NOT NULL UNIQUE,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    run_version INTEGER NOT NULL CHECK (run_version >= 1),
    approval_row_id INTEGER REFERENCES feature_workspace_prototype_approvals(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(run_row_id,run_version), CHECK (from_state='proposed' AND to_state='approved')
);
INSERT INTO feature_workspace_prototype_lifecycle_transitions SELECT * FROM feature_workspace_prototype_lifecycle_transitions_part3 WHERE from_state='proposed' AND to_state='approved';
DROP TABLE feature_workspace_prototype_lifecycle_transitions_part3;
CREATE INDEX idx_prototype_transitions_run ON feature_workspace_prototype_lifecycle_transitions(run_row_id,id);
