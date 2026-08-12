-- +goose Up
-- Program integration runtime: durable eligibility for accepted isolated audit
-- decisions of Program Dispatch members, the immutable Relay-generated
-- Integration Assignment transport, the admitted external Merge result, and
-- the recorded Relay verification pass or failure. This surface is runtime
-- transport only: it carries no Delivery Plan identity or authority, no
-- authored plan, no approval, no second lifecycle, and no source merge.
CREATE TABLE program_integration_eligibilities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    eligibility_id TEXT NOT NULL UNIQUE,
    dispatch_member_row_id INTEGER NOT NULL UNIQUE REFERENCES program_dispatch_members(id) ON DELETE RESTRICT,
    audit_ticket_revision_decision_row_id INTEGER NOT NULL UNIQUE REFERENCES audit_ticket_revision_decisions(id) ON DELETE RESTRICT,
    delivery_ticket_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    audited_commit TEXT NOT NULL CHECK (length(audited_commit) = 40 AND audited_commit NOT GLOB '*[^0-9a-f]*'),
    pushed_branch TEXT NOT NULL CHECK (pushed_branch <> '' AND trim(pushed_branch) = pushed_branch),
    execution_package_row_id INTEGER NOT NULL REFERENCES execution_packages(id) ON DELETE RESTRICT,
    assignment_artifact_row_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (eligibility_id GLOB 'integration-eligibility-*' AND trim(eligibility_id) = eligibility_id)
);
CREATE INDEX idx_program_integration_eligibilities_dispatch
ON program_integration_eligibilities(dispatch_member_row_id, id);

CREATE TABLE program_integration_assignments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    assignment_id TEXT NOT NULL UNIQUE,
    dispatch_row_id INTEGER NOT NULL REFERENCES program_dispatches(id) ON DELETE RESTRICT,
    repo_target TEXT NOT NULL,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL,
    content TEXT NOT NULL,
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL DEFAULT 'generated' CHECK (status IN ('generated', 'admitted', 'verified', 'failed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (assignment_id GLOB 'integration-assignment-*' AND trim(assignment_id) = assignment_id)
);
CREATE INDEX idx_program_integration_assignments_dispatch
ON program_integration_assignments(dispatch_row_id, id);

CREATE TABLE program_integration_assignment_constituents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    assignment_row_id INTEGER NOT NULL REFERENCES program_integration_assignments(id) ON DELETE RESTRICT,
    eligibility_row_id INTEGER NOT NULL REFERENCES program_integration_eligibilities(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    UNIQUE (assignment_row_id, sequence)
);

CREATE TABLE program_integration_merge_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    result_id TEXT NOT NULL UNIQUE,
    assignment_row_id INTEGER NOT NULL UNIQUE REFERENCES program_integration_assignments(id) ON DELETE RESTRICT,
    integrated_commit TEXT NOT NULL CHECK (length(integrated_commit) = 40 AND integrated_commit NOT GLOB '*[^0-9a-f]*'),
    preservation_identity TEXT NOT NULL CHECK (preservation_identity <> '' AND trim(preservation_identity) = preservation_identity),
    conflict_evidence TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (result_id GLOB 'integration-merge-result-*' AND trim(result_id) = result_id),
    CHECK (conflict_evidence IS NULL OR (conflict_evidence <> '' AND trim(conflict_evidence) = conflict_evidence))
);

CREATE TABLE program_integration_validation_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    merge_result_row_id INTEGER NOT NULL REFERENCES program_integration_merge_results(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    constituent_sequence INTEGER NOT NULL CHECK (constituent_sequence >= 1),
    command TEXT NOT NULL CHECK (command <> '' AND trim(command) = command),
    expected TEXT NOT NULL CHECK (expected <> '' AND trim(expected) = expected),
    status TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) = evidence),
    UNIQUE (merge_result_row_id, sequence)
);

CREATE TABLE program_integration_evidence_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    merge_result_row_id INTEGER NOT NULL REFERENCES program_integration_merge_results(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    constituent_sequence INTEGER NOT NULL CHECK (constituent_sequence >= 1),
    obligation TEXT NOT NULL CHECK (obligation <> '' AND trim(obligation) = obligation),
    kind TEXT NOT NULL CHECK (kind IN ('proof_obligation', 'black_box_outcome')),
    status TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) = evidence),
    UNIQUE (merge_result_row_id, sequence)
);

CREATE TABLE program_integration_verifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    verification_id TEXT NOT NULL UNIQUE,
    assignment_row_id INTEGER NOT NULL UNIQUE REFERENCES program_integration_assignments(id) ON DELETE RESTRICT,
    merge_result_row_id INTEGER NOT NULL UNIQUE REFERENCES program_integration_merge_results(id) ON DELETE RESTRICT,
    outcome TEXT NOT NULL CHECK (outcome IN ('passed', 'failed')),
    failure_reason TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (verification_id GLOB 'integration-verification-*' AND trim(verification_id) = verification_id),
    CHECK ((outcome = 'passed' AND failure_reason IS NULL) OR (outcome = 'failed' AND failure_reason <> '' AND trim(failure_reason) = failure_reason))
);

CREATE TABLE program_integration_completions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    verification_row_id INTEGER NOT NULL REFERENCES program_integration_verifications(id) ON DELETE RESTRICT,
    assignment_constituent_row_id INTEGER NOT NULL UNIQUE REFERENCES program_integration_assignment_constituents(id) ON DELETE RESTRICT,
    satisfaction_row_id INTEGER NOT NULL UNIQUE REFERENCES delivery_ticket_revision_satisfactions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose StatementBegin
CREATE TRIGGER program_integration_eligibility_immutable BEFORE UPDATE ON program_integration_eligibilities FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration eligibility is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_eligibility_delete_guard BEFORE DELETE ON program_integration_eligibilities FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration eligibilities are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_assignment_identity_immutable BEFORE UPDATE OF assignment_id, dispatch_row_id, repo_target, branch, base_commit, content, content_sha256, created_at ON program_integration_assignments FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration assignment identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_assignment_transition_guard BEFORE UPDATE OF status ON program_integration_assignments FOR EACH ROW WHEN NOT ((OLD.status = 'generated' AND NEW.status = 'admitted') OR (OLD.status = 'admitted' AND NEW.status IN ('verified', 'failed'))) BEGIN SELECT RAISE(ABORT, 'invalid program integration assignment transition'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_assignment_delete_guard BEFORE DELETE ON program_integration_assignments FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration assignments are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_assignment_constituent_immutable BEFORE UPDATE ON program_integration_assignment_constituents FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration assignment constituents are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_assignment_constituent_delete_guard BEFORE DELETE ON program_integration_assignment_constituents FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration assignment constituents are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_merge_result_immutable BEFORE UPDATE ON program_integration_merge_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration merge results are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_merge_result_delete_guard BEFORE DELETE ON program_integration_merge_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration merge results are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_validation_outcome_immutable BEFORE UPDATE ON program_integration_validation_outcomes FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration validation outcomes are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_validation_outcome_delete_guard BEFORE DELETE ON program_integration_validation_outcomes FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration validation outcomes are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_evidence_outcome_immutable BEFORE UPDATE ON program_integration_evidence_outcomes FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration evidence outcomes are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_evidence_outcome_delete_guard BEFORE DELETE ON program_integration_evidence_outcomes FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration evidence outcomes are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_verification_immutable BEFORE UPDATE ON program_integration_verifications FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration verifications are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_verification_delete_guard BEFORE DELETE ON program_integration_verifications FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration verifications are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_completion_immutable BEFORE UPDATE ON program_integration_completions FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration completions are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_integration_completion_delete_guard BEFORE DELETE ON program_integration_completions FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program integration completions are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS program_integration_completions;
DROP TABLE IF EXISTS program_integration_verifications;
DROP TABLE IF EXISTS program_integration_evidence_outcomes;
DROP TABLE IF EXISTS program_integration_validation_outcomes;
DROP TABLE IF EXISTS program_integration_merge_results;
DROP TABLE IF EXISTS program_integration_assignment_constituents;
DROP TABLE IF EXISTS program_integration_assignments;
DROP TABLE IF EXISTS program_integration_eligibilities;
