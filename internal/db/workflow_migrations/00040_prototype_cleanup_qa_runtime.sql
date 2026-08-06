-- +goose Up
-- Part 3 replaces the earlier unowned QA association with direct packet-owned
-- cleanup and operator-evidence history. The lifecycle rebuild retains every
-- existing transition and adds only the close/cleanup edges required here.
DROP TABLE IF EXISTS feature_workspace_prototype_qa_associations;
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
    CHECK (
        (from_state='proposed' AND to_state='approved' AND approval_row_id IS NOT NULL) OR
        (from_state='approved' AND to_state IN ('preparing','failed','closed') AND approval_row_id IS NULL) OR
        (from_state='preparing' AND to_state IN ('running','launch_uncertain','failed','closed') AND approval_row_id IS NULL) OR
        (from_state='launch_uncertain' AND to_state IN ('running','succeeded','failed','cancelled','timed_out','cleanup_required','closed') AND approval_row_id IS NULL) OR
        (from_state='running' AND to_state IN ('succeeded','failed','cancelled','timed_out','cleanup_required','closed') AND approval_row_id IS NULL) OR
        (from_state IN ('succeeded','failed','cancelled','timed_out') AND to_state IN ('cleanup_required','closed') AND approval_row_id IS NULL) OR
        (from_state='cleanup_required' AND to_state IN ('cleanup_required','closed') AND approval_row_id IS NULL)
    )
);
INSERT INTO feature_workspace_prototype_lifecycle_transitions SELECT * FROM feature_workspace_prototype_lifecycle_transitions_part3;
DROP TABLE feature_workspace_prototype_lifecycle_transitions_part3;
CREATE INDEX idx_prototype_transitions_run ON feature_workspace_prototype_lifecycle_transitions(run_row_id,id);

CREATE TABLE feature_workspace_prototype_cleanup_reconciliations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reconciliation_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    mutation_identity TEXT NOT NULL UNIQUE,
    trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('explicit','startup')),
    expected_run_version INTEGER NOT NULL CHECK (expected_run_version >= 1),
    observed_run_state TEXT NOT NULL,
    process_ownership_status TEXT NOT NULL CHECK (process_ownership_status IN ('pending','complete','failed')),
    evidence_settlement_status TEXT NOT NULL CHECK (evidence_settlement_status IN ('pending','complete','failed')),
    worktree_status TEXT NOT NULL CHECK (worktree_status IN ('pending','complete','failed')),
    ephemeral_target_status TEXT NOT NULL CHECK (ephemeral_target_status IN ('pending','complete','failed')),
    prototype_lease_status TEXT NOT NULL CHECK (prototype_lease_status IN ('pending','complete','failed')),
    resulting_run_state TEXT NOT NULL CHECK (resulting_run_state IN ('cleanup_required','closed')),
    diagnostic TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (reconciliation_id GLOB 'prototype-reconciliation-*' AND trim(reconciliation_id)=reconciliation_id),
    CHECK (trim(mutation_identity)<>''),
    CHECK (trim(diagnostic)=diagnostic)
);
CREATE INDEX idx_prototype_cleanup_reconciliations_run ON feature_workspace_prototype_cleanup_reconciliations(run_row_id,id);
CREATE INDEX idx_prototype_cleanup_reconciliations_id ON feature_workspace_prototype_cleanup_reconciliations(reconciliation_id);

CREATE TABLE feature_workspace_prototype_qa_packets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qa_packet_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    mutation_identity TEXT NOT NULL UNIQUE,
    expected_run_version INTEGER NOT NULL CHECK (expected_run_version >= 1),
    status TEXT NOT NULL CHECK (status IN ('prepared','admitted')),
    member_count INTEGER NOT NULL CHECK (member_count BETWEEN 1 AND 32),
    total_bytes INTEGER NOT NULL CHECK (total_bytes BETWEEN 1 AND 33554432),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    admitted_at TEXT,
    UNIQUE(run_row_id, mutation_identity),
    CHECK (qa_packet_id GLOB 'prototype-qa-packet-*'),
    CHECK ((status='prepared' AND admitted_at IS NULL) OR (status='admitted' AND admitted_at IS NOT NULL))
);
CREATE INDEX idx_prototype_qa_packets_workspace_run ON feature_workspace_prototype_qa_packets(workspace_row_id,run_row_id,id);
CREATE INDEX idx_prototype_qa_packets_id ON feature_workspace_prototype_qa_packets(qa_packet_id);

CREATE TABLE feature_workspace_prototype_qa_packet_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qa_packet_member_id TEXT NOT NULL UNIQUE,
    qa_packet_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_qa_packets(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    member_kind TEXT NOT NULL CHECK (member_kind IN ('prototype_result','prototype_evidence','operator_prompt','validation_instruction')),
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    sha256 TEXT NOT NULL,
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(qa_packet_row_id, sequence),
    CHECK (qa_packet_member_id GLOB 'prototype-qa-member-*'),
    CHECK (length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    CHECK (trim(media_type)<>'')
);
CREATE INDEX idx_prototype_qa_packet_members_packet_sequence ON feature_workspace_prototype_qa_packet_members(qa_packet_row_id,sequence);

CREATE TABLE feature_workspace_prototype_qa_evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qa_evidence_id TEXT NOT NULL UNIQUE,
    qa_packet_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_qa_packets(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 20),
    semantic_role TEXT NOT NULL,
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    sha256 TEXT NOT NULL,
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 8388608),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(qa_packet_row_id, sequence),
    UNIQUE(qa_packet_row_id, semantic_role),
    CHECK (qa_evidence_id GLOB 'prototype-qa-evidence-*'),
    CHECK (trim(semantic_role)=semantic_role AND trim(semantic_role)<>''),
    CHECK (length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    CHECK (trim(media_type)<>'')
);
CREATE INDEX idx_prototype_qa_evidence_packet_sequence ON feature_workspace_prototype_qa_evidence(qa_packet_row_id,sequence);

CREATE TABLE feature_workspace_prototype_qa_admissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qa_admission_id TEXT NOT NULL UNIQUE,
    qa_packet_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_qa_packets(id) ON DELETE RESTRICT,
    mutation_identity TEXT NOT NULL UNIQUE,
    operator_confirmation_evidence TEXT NOT NULL,
    admitted_member_count INTEGER NOT NULL CHECK (admitted_member_count BETWEEN 1 AND 20),
    admitted_total_bytes INTEGER NOT NULL CHECK (admitted_total_bytes BETWEEN 1 AND 33554432),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (qa_admission_id GLOB 'prototype-qa-admission-*'),
    CHECK (trim(operator_confirmation_evidence)<>'')
);
CREATE INDEX idx_prototype_qa_admissions_packet ON feature_workspace_prototype_qa_admissions(qa_packet_row_id);

-- Every Part 3 record is immutable. The packet has one narrowly controlled
-- prepared -> admitted update, and its admission trigger verifies exact sums.
-- +goose StatementBegin
CREATE TRIGGER prototype_cleanup_reconciliation_immutable BEFORE UPDATE ON feature_workspace_prototype_cleanup_reconciliations BEGIN SELECT RAISE(ABORT,'prototype cleanup reconciliations are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_cleanup_reconciliation_ownership_guard BEFORE INSERT ON feature_workspace_prototype_cleanup_reconciliations FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runs WHERE id=NEW.run_row_id) BEGIN SELECT RAISE(ABORT,'prototype cleanup reconciliation ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_packet_ownership_guard BEFORE INSERT ON feature_workspace_prototype_qa_packets FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runs r WHERE r.id=NEW.run_row_id AND r.workspace_row_id=NEW.workspace_row_id) BEGIN SELECT RAISE(ABORT,'prototype QA packet workspace/run ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_packet_immutable BEFORE UPDATE ON feature_workspace_prototype_qa_packets FOR EACH ROW WHEN OLD.qa_packet_id<>NEW.qa_packet_id OR OLD.workspace_row_id<>NEW.workspace_row_id OR OLD.run_row_id<>NEW.run_row_id OR OLD.mutation_identity<>NEW.mutation_identity OR OLD.expected_run_version<>NEW.expected_run_version OR OLD.member_count<>NEW.member_count OR OLD.total_bytes<>NEW.total_bytes OR OLD.created_at<>NEW.created_at OR NOT (OLD.status='prepared' AND NEW.status='admitted' AND OLD.admitted_at IS NULL AND NEW.admitted_at IS NOT NULL) BEGIN SELECT RAISE(ABORT,'prototype QA packets are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_packet_member_ownership_guard BEFORE INSERT ON feature_workspace_prototype_qa_packet_members FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_qa_packets p JOIN feature_workspaces w ON w.id=p.workspace_row_id WHERE p.id=NEW.qa_packet_row_id AND EXISTS (SELECT 1 FROM feature_workspace_discovery_artifacts a WHERE a.id=NEW.artifact_row_id AND a.workspace_row_id=w.id AND a.sha256=NEW.sha256 AND a.media_type=NEW.media_type AND a.size_bytes=NEW.size_bytes)) BEGIN SELECT RAISE(ABORT,'prototype QA packet member artifact ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_packet_member_immutable BEFORE UPDATE ON feature_workspace_prototype_qa_packet_members BEGIN SELECT RAISE(ABORT,'prototype QA packet members are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_evidence_ownership_guard BEFORE INSERT ON feature_workspace_prototype_qa_evidence FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_qa_packets p JOIN feature_workspaces w ON w.id=p.workspace_row_id WHERE p.id=NEW.qa_packet_row_id AND EXISTS (SELECT 1 FROM feature_workspace_discovery_artifacts a WHERE a.id=NEW.artifact_row_id AND a.workspace_row_id=w.id AND a.sha256=NEW.sha256 AND a.media_type=NEW.media_type AND a.size_bytes=NEW.size_bytes)) BEGIN SELECT RAISE(ABORT,'prototype QA evidence artifact ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_evidence_immutable BEFORE UPDATE ON feature_workspace_prototype_qa_evidence BEGIN SELECT RAISE(ABORT,'prototype QA evidence is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_admission_ownership_guard BEFORE INSERT ON feature_workspace_prototype_qa_admissions FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_qa_packets p WHERE p.id=NEW.qa_packet_row_id AND p.status='prepared' AND NEW.admitted_member_count=(SELECT COUNT(*) FROM feature_workspace_prototype_qa_evidence WHERE qa_packet_row_id=p.id) AND NEW.admitted_total_bytes=(SELECT COALESCE(SUM(size_bytes),0) FROM feature_workspace_prototype_qa_evidence WHERE qa_packet_row_id=p.id)) BEGIN SELECT RAISE(ABORT,'prototype QA admission requires exact prepared evidence'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_qa_admission_immutable BEFORE UPDATE ON feature_workspace_prototype_qa_admissions BEGIN SELECT RAISE(ABORT,'prototype QA admissions are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS prototype_qa_admission_immutable;
DROP TRIGGER IF EXISTS prototype_qa_admission_ownership_guard;
DROP TRIGGER IF EXISTS prototype_qa_evidence_immutable;
DROP TRIGGER IF EXISTS prototype_qa_evidence_ownership_guard;
DROP TRIGGER IF EXISTS prototype_qa_packet_member_immutable;
DROP TRIGGER IF EXISTS prototype_qa_packet_member_ownership_guard;
DROP TRIGGER IF EXISTS prototype_qa_packet_immutable;
DROP TRIGGER IF EXISTS prototype_qa_packet_ownership_guard;
DROP TRIGGER IF EXISTS prototype_cleanup_reconciliation_ownership_guard;
DROP TRIGGER IF EXISTS prototype_cleanup_reconciliation_immutable;
DROP INDEX IF EXISTS idx_prototype_qa_admissions_packet;
DROP INDEX IF EXISTS idx_prototype_qa_evidence_packet_sequence;
DROP INDEX IF EXISTS idx_prototype_qa_packet_members_packet_sequence;
DROP INDEX IF EXISTS idx_prototype_qa_packets_id;
DROP INDEX IF EXISTS idx_prototype_qa_packets_workspace_run;
DROP INDEX IF EXISTS idx_prototype_cleanup_reconciliations_id;
DROP INDEX IF EXISTS idx_prototype_cleanup_reconciliations_run;
DROP TABLE IF EXISTS feature_workspace_prototype_qa_admissions;
DROP TABLE IF EXISTS feature_workspace_prototype_qa_evidence;
DROP TABLE IF EXISTS feature_workspace_prototype_qa_packet_members;
DROP TABLE IF EXISTS feature_workspace_prototype_qa_packets;
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
    UNIQUE(run_row_id,run_version),
    CHECK (
        (from_state='proposed' AND to_state='approved' AND approval_row_id IS NOT NULL) OR
        (from_state='approved' AND to_state IN ('preparing','failed','closed') AND approval_row_id IS NULL) OR
        (from_state='preparing' AND to_state IN ('running','launch_uncertain','failed','closed') AND approval_row_id IS NULL) OR
        (from_state='launch_uncertain' AND to_state IN ('running','succeeded','failed','cancelled','timed_out','cleanup_required','closed') AND approval_row_id IS NULL) OR
        (from_state='running' AND to_state IN ('succeeded','failed','cancelled','timed_out','cleanup_required','closed') AND approval_row_id IS NULL) OR
        (from_state IN ('succeeded','failed','cancelled','timed_out') AND to_state IN ('cleanup_required','closed') AND approval_row_id IS NULL) OR
        (from_state='cleanup_required' AND to_state IN ('cleanup_required','closed') AND approval_row_id IS NULL)
    )
);
INSERT INTO feature_workspace_prototype_lifecycle_transitions SELECT * FROM feature_workspace_prototype_lifecycle_transitions_part3;
DROP TABLE feature_workspace_prototype_lifecycle_transitions_part3;
CREATE INDEX idx_prototype_transitions_run ON feature_workspace_prototype_lifecycle_transitions(run_row_id,id);
