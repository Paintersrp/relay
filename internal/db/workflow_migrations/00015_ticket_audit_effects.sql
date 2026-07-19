-- +goose Up
-- Ticket-route audit effects retain the exact package basis carried by a packet.
-- They are intentionally separate from the legacy packet and decision rows so
-- ordinary Run history remains valid and readable.
CREATE TABLE audit_packet_ticket_obligations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_packet_row_id INTEGER NOT NULL REFERENCES audit_packets(id) ON DELETE RESTRICT,
    execution_package_row_id INTEGER NOT NULL REFERENCES execution_packages(id) ON DELETE RESTRICT,
    execution_package_member_row_id INTEGER NOT NULL REFERENCES execution_package_members(id) ON DELETE RESTRICT,
    delivery_ticket_row_id INTEGER NOT NULL REFERENCES delivery_tickets(id) ON DELETE RESTRICT,
    delivery_ticket_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (audit_packet_row_id, execution_package_member_row_id),
    UNIQUE (audit_packet_row_id, delivery_ticket_revision_row_id)
);

CREATE TABLE audit_ticket_revision_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_decision_row_id INTEGER NOT NULL REFERENCES audit_decisions(id) ON DELETE RESTRICT,
    audit_packet_ticket_obligation_row_id INTEGER NOT NULL REFERENCES audit_packet_ticket_obligations(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (audit_decision_row_id, audit_packet_ticket_obligation_row_id)
);

CREATE TABLE delivery_ticket_revision_satisfactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    delivery_ticket_revision_row_id INTEGER NOT NULL UNIQUE REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    audit_ticket_revision_decision_row_id INTEGER NOT NULL UNIQUE REFERENCES audit_ticket_revision_decisions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE audit_remediation_seeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    remediation_seed_id TEXT NOT NULL UNIQUE,
    audit_ticket_revision_decision_row_id INTEGER NOT NULL UNIQUE REFERENCES audit_ticket_revision_decisions(id) ON DELETE RESTRICT,
    audit_packet_row_id INTEGER NOT NULL REFERENCES audit_packets(id) ON DELETE RESTRICT,
    execution_package_row_id INTEGER NOT NULL REFERENCES execution_packages(id) ON DELETE RESTRICT,
    audited_commit TEXT NOT NULL CHECK (length(audited_commit) = 40 AND audited_commit NOT GLOB '*[^0-9a-f]*'),
    decision_rationale TEXT NOT NULL CHECK (decision_rationale <> '' AND trim(decision_rationale) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (remediation_seed_id GLOB 'remediation-*' AND trim(remediation_seed_id) = remediation_seed_id)
);

CREATE TABLE audit_remediation_seed_findings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    remediation_seed_row_id INTEGER NOT NULL REFERENCES audit_remediation_seeds(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    upstream_classification TEXT NOT NULL CHECK (upstream_classification IN ('executor_implementation', 'execution_spec', 'both')),
    summary TEXT NOT NULL CHECK (summary <> '' AND trim(summary) <> ''),
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) <> ''),
    required_remediation TEXT NOT NULL CHECK (required_remediation <> '' AND trim(required_remediation) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (remediation_seed_row_id, sequence)
);

CREATE TABLE audit_remediation_seed_reopenings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    remediation_seed_row_id INTEGER NOT NULL UNIQUE REFERENCES audit_remediation_seeds(id) ON DELETE RESTRICT,
    reopening_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    reopening_kind TEXT NOT NULL CHECK (reopening_kind IN ('replacement_ticket_revision', 'remediation_ticket')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE feature_workspace_completion_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    completion_decision_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    decision TEXT NOT NULL CHECK (decision = 'completed'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (completion_decision_id GLOB 'completion-*' AND trim(completion_decision_id) = completion_decision_id)
);

CREATE TABLE feature_workspace_completion_reopenings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    completion_decision_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_completion_decisions(id) ON DELETE RESTRICT,
    reopening_kind TEXT NOT NULL CHECK (reopening_kind IN ('ticket_revision', 'authority_revision', 'remediation_seed')),
    reopening_ticket_revision_row_id INTEGER REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    reopening_authority_revision_row_id INTEGER REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    reopening_remediation_seed_row_id INTEGER REFERENCES audit_remediation_seeds(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (
        (reopening_kind = 'ticket_revision' AND reopening_ticket_revision_row_id IS NOT NULL AND reopening_authority_revision_row_id IS NULL AND reopening_remediation_seed_row_id IS NULL) OR
        (reopening_kind = 'authority_revision' AND reopening_ticket_revision_row_id IS NULL AND reopening_authority_revision_row_id IS NOT NULL AND reopening_remediation_seed_row_id IS NULL) OR
        (reopening_kind = 'remediation_seed' AND reopening_ticket_revision_row_id IS NULL AND reopening_authority_revision_row_id IS NULL AND reopening_remediation_seed_row_id IS NOT NULL)
    )
);

CREATE INDEX idx_audit_packet_ticket_obligations_packet
ON audit_packet_ticket_obligations(audit_packet_row_id, execution_package_member_row_id, id);
CREATE INDEX idx_audit_ticket_revision_decisions_decision
ON audit_ticket_revision_decisions(audit_decision_row_id, id);
CREATE INDEX idx_delivery_ticket_revision_satisfactions_revision
ON delivery_ticket_revision_satisfactions(delivery_ticket_revision_row_id, id);
CREATE INDEX idx_audit_remediation_seeds_decision
ON audit_remediation_seeds(audit_ticket_revision_decision_row_id, id);
CREATE INDEX idx_audit_remediation_seed_reopenings_revision
ON audit_remediation_seed_reopenings(reopening_revision_row_id, id);
CREATE INDEX idx_feature_workspace_completion_decisions_workspace
ON feature_workspace_completion_decisions(workspace_row_id, created_at, id);

-- +goose StatementBegin
CREATE TRIGGER audit_packet_identity_immutable
BEFORE UPDATE OF audit_packet_id, run_row_id, implementation_actor_kind, execution_attempt_row_id,
    artifact_row_id, base_commit, audited_commit, packet_sha256, created_at ON audit_packets
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'audit packet identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_packet_lifecycle_guard
BEFORE UPDATE OF status, stale_reason, superseded_at ON audit_packets
FOR EACH ROW WHEN NOT (
    OLD.status = 'current' AND NEW.status = 'stale' AND NEW.stale_reason <> ''
    AND NEW.superseded_at IS NOT NULL
)
BEGIN SELECT RAISE(ABORT, 'audit packet lifecycle may only become stale'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_packet_delete_guard
BEFORE DELETE ON audit_packets
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'audit packets are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_decision_update_immutable
BEFORE UPDATE ON audit_decisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'audit decisions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_decision_delete_guard
BEFORE DELETE ON audit_decisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'audit decisions are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_packet_ticket_obligation_guard
BEFORE INSERT ON audit_packet_ticket_obligations
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM audit_packets AS packet
    JOIN runs AS run ON run.id = packet.run_row_id
    JOIN execution_packages AS package ON package.id = NEW.execution_package_row_id
    JOIN execution_package_members AS package_member ON package_member.id = NEW.execution_package_member_row_id
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.delivery_ticket_revision_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = NEW.delivery_ticket_row_id
    WHERE packet.id = NEW.audit_packet_row_id
      AND run.execution_package_row_id = package.id
      AND package_member.package_row_id = package.id
      AND package_member.revision_row_id = revision.id
      AND revision.delivery_ticket_row_id = ticket.id
      AND ticket.workspace_row_id = package.workspace_row_id
      AND package.authority_revision_row_id = NEW.authority_revision_row_id
      AND package.source_closure_row_id = NEW.source_closure_row_id
      AND revision.source_closure_row_id = package.source_closure_row_id
      AND run.repo_target = package.repo_target COLLATE NOCASE
      AND run.branch = package.branch
      AND run.base_commit = package.base_commit
      AND packet.base_commit = package.base_commit
)
BEGIN SELECT RAISE(ABORT, 'audit packet ticket obligation must bind the Run package member and exact authority source basis'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_packet_ticket_obligation_update_immutable
BEFORE UPDATE ON audit_packet_ticket_obligations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'audit packet ticket obligations are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_packet_ticket_obligation_delete_guard
BEFORE DELETE ON audit_packet_ticket_obligations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'audit packet ticket obligations are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_ticket_revision_decision_guard
BEFORE INSERT ON audit_ticket_revision_decisions
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM audit_decisions AS decision
    JOIN audit_packet_ticket_obligations AS obligation ON obligation.id = NEW.audit_packet_ticket_obligation_row_id
    JOIN audit_packets AS packet ON packet.id = obligation.audit_packet_row_id
    WHERE decision.id = NEW.audit_decision_row_id
      AND decision.run_row_id = packet.run_row_id
      AND decision.audit_packet_artifact_row_id = packet.artifact_row_id
      AND decision.audited_commit = packet.audited_commit
      AND decision.packet_sha256 = packet.packet_sha256
)
BEGIN SELECT RAISE(ABORT, 'ticket revision decision must bind the exact decision packet obligation'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_ticket_revision_decision_update_immutable
BEFORE UPDATE ON audit_ticket_revision_decisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'ticket revision decisions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_ticket_revision_decision_delete_guard
BEFORE DELETE ON audit_ticket_revision_decisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'ticket revision decisions are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_revision_satisfaction_guard
BEFORE INSERT ON delivery_ticket_revision_satisfactions
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM audit_ticket_revision_decisions AS revision_decision
    JOIN audit_decisions AS decision ON decision.id = revision_decision.audit_decision_row_id
    JOIN audit_packet_ticket_obligations AS obligation ON obligation.id = revision_decision.audit_packet_ticket_obligation_row_id
    JOIN audit_packets AS packet ON packet.id = obligation.audit_packet_row_id
    JOIN execution_packages AS package ON package.id = obligation.execution_package_row_id
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.delivery_ticket_revision_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
    JOIN delivery_ticket_selections AS selection ON selection.id = package.selection_row_id
    JOIN feature_workspaces AS workspace ON workspace.id = package.workspace_row_id
    JOIN feature_workspace_authority_revisions AS authority ON authority.id = package.authority_revision_row_id
    JOIN source_vault_closures AS closure ON closure.id = package.source_closure_row_id
    WHERE revision_decision.id = NEW.audit_ticket_revision_decision_row_id
      AND decision.decision = 'accepted'
      AND obligation.delivery_ticket_revision_row_id = revision.id
      AND ticket.current_revision_row_id = revision.id
      AND revision.cancellation_reason IS NULL
      AND packet.status = 'current'
      AND selection.state = 'consumed'
      AND workspace.current_authority_revision_row_id = package.authority_revision_row_id
      AND authority.workspace_row_id = workspace.id
      AND authority.source_closure_row_id = package.source_closure_row_id
      AND closure.state = 'ready'
      AND NOT EXISTS (
          SELECT 1
          FROM execution_package_members AS required_member
          WHERE required_member.package_row_id = package.id
            AND NOT EXISTS (
                SELECT 1
                FROM audit_packet_ticket_obligations AS required_obligation
                WHERE required_obligation.audit_packet_row_id = packet.id
                  AND required_obligation.execution_package_member_row_id = required_member.id
            )
      )
      AND NOT EXISTS (
          SELECT 1
          FROM execution_package_members AS required_member
          JOIN delivery_ticket_revisions AS required_revision ON required_revision.id = required_member.revision_row_id
          JOIN delivery_tickets AS required_ticket ON required_ticket.id = required_revision.delivery_ticket_row_id
          WHERE required_member.package_row_id = package.id
            AND (required_ticket.current_revision_row_id <> required_revision.id OR required_revision.cancellation_reason IS NOT NULL)
      )
)
BEGIN SELECT RAISE(ABORT, 'ticket satisfaction requires every current active package obligation and accepted exact revision'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_revision_satisfaction_update_immutable
BEFORE UPDATE ON delivery_ticket_revision_satisfactions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket satisfactions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_revision_satisfaction_delete_guard
BEFORE DELETE ON delivery_ticket_revision_satisfactions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket satisfactions are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_guard
BEFORE INSERT ON audit_remediation_seeds
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM audit_ticket_revision_decisions AS revision_decision
    JOIN audit_decisions AS decision ON decision.id = revision_decision.audit_decision_row_id
    JOIN audit_packet_ticket_obligations AS obligation ON obligation.id = revision_decision.audit_packet_ticket_obligation_row_id
    WHERE revision_decision.id = NEW.audit_ticket_revision_decision_row_id
      AND decision.decision = 'needs_revision'
      AND obligation.audit_packet_row_id = NEW.audit_packet_row_id
      AND obligation.execution_package_row_id = NEW.execution_package_row_id
      AND decision.audited_commit = NEW.audited_commit
      AND decision.rationale = NEW.decision_rationale
)
BEGIN SELECT RAISE(ABORT, 'remediation seed must preserve one needs-revision decision exact packet package and rationale'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_update_immutable
BEFORE UPDATE ON audit_remediation_seeds
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seeds are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_delete_guard
BEFORE DELETE ON audit_remediation_seeds
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seeds are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_finding_update_immutable
BEFORE UPDATE ON audit_remediation_seed_findings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed findings are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_finding_delete_guard
BEFORE DELETE ON audit_remediation_seed_findings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed findings are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_reopening_guard
BEFORE INSERT ON audit_remediation_seed_reopenings
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM audit_remediation_seeds AS seed
    JOIN audit_ticket_revision_decisions AS revision_decision ON revision_decision.id = seed.audit_ticket_revision_decision_row_id
    JOIN audit_packet_ticket_obligations AS obligation ON obligation.id = revision_decision.audit_packet_ticket_obligation_row_id
    JOIN delivery_ticket_revisions AS audited_revision ON audited_revision.id = obligation.delivery_ticket_revision_row_id
    JOIN delivery_tickets AS audited_ticket ON audited_ticket.id = audited_revision.delivery_ticket_row_id
    JOIN delivery_ticket_revisions AS reopening_revision ON reopening_revision.id = NEW.reopening_revision_row_id
    JOIN delivery_tickets AS reopening_ticket ON reopening_ticket.id = reopening_revision.delivery_ticket_row_id
    WHERE seed.id = NEW.remediation_seed_row_id
      AND reopening_ticket.workspace_row_id = audited_ticket.workspace_row_id
      AND reopening_ticket.current_revision_row_id = reopening_revision.id
      AND reopening_revision.cancellation_reason IS NULL
      AND reopening_revision.id <> audited_revision.id
      AND (
          (NEW.reopening_kind = 'replacement_ticket_revision'
              AND reopening_ticket.id = audited_ticket.id
              AND reopening_revision.replaces_revision_row_id = audited_revision.id) OR
          (NEW.reopening_kind = 'remediation_ticket'
              AND reopening_ticket.id <> audited_ticket.id)
      )
)
BEGIN SELECT RAISE(ABORT, 'remediation reopening must link one current active replacement or remediation ticket revision'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_reopening_update_immutable
BEFORE UPDATE ON audit_remediation_seed_reopenings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed reopenings are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_reopening_delete_guard
BEFORE DELETE ON audit_remediation_seed_reopenings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed reopenings are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_completion_decision_guard
BEFORE INSERT ON feature_workspace_completion_decisions
FOR EACH ROW WHEN NOT (
    EXISTS (
        SELECT 1
        FROM feature_workspaces AS workspace
        JOIN feature_workspace_authority_revisions AS authority ON authority.id = NEW.authority_revision_row_id
        JOIN source_vault_closures AS closure ON closure.id = NEW.source_closure_row_id
        WHERE workspace.id = NEW.workspace_row_id
          AND workspace.current_authority_revision_row_id = authority.id
          AND authority.workspace_row_id = workspace.id
          AND authority.source_closure_row_id = closure.id
          AND closure.state = 'ready'
    )
    AND NOT EXISTS (
        SELECT 1
        FROM delivery_tickets AS ticket
        JOIN delivery_ticket_revisions AS revision ON revision.id = ticket.current_revision_row_id
        LEFT JOIN delivery_ticket_revision_satisfactions AS satisfaction ON satisfaction.delivery_ticket_revision_row_id = revision.id
        WHERE ticket.workspace_row_id = NEW.workspace_row_id
          AND revision.cancellation_reason IS NULL
          AND satisfaction.id IS NULL
    )
    AND NOT EXISTS (
        SELECT 1
        FROM audit_remediation_seeds AS seed
        JOIN audit_ticket_revision_decisions AS revision_decision ON revision_decision.id = seed.audit_ticket_revision_decision_row_id
        JOIN audit_packet_ticket_obligations AS obligation ON obligation.id = revision_decision.audit_packet_ticket_obligation_row_id
        JOIN delivery_tickets AS ticket ON ticket.id = obligation.delivery_ticket_row_id
        WHERE ticket.workspace_row_id = NEW.workspace_row_id
          AND NOT EXISTS (
              SELECT 1
              FROM audit_remediation_seed_reopenings AS reopening
              JOIN delivery_ticket_revisions AS reopening_revision ON reopening_revision.id = reopening.reopening_revision_row_id
              JOIN delivery_tickets AS reopening_ticket ON reopening_ticket.id = reopening_revision.delivery_ticket_row_id
              WHERE reopening.remediation_seed_row_id = seed.id
                AND reopening_ticket.current_revision_row_id = reopening_revision.id
                AND reopening_revision.cancellation_reason IS NULL
          )
    )
    AND NOT EXISTS (
        SELECT 1
        FROM feature_workspace_completion_decisions AS prior
        WHERE prior.workspace_row_id = NEW.workspace_row_id
          AND NOT EXISTS (
              SELECT 1 FROM feature_workspace_completion_reopenings AS reopening
              WHERE reopening.completion_decision_row_id = prior.id
          )
    )
)
BEGIN SELECT RAISE(ABORT, 'feature completion requires explicit current satisfied tickets and no pending remediation'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_completion_decision_update_immutable
BEFORE UPDATE ON feature_workspace_completion_decisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature completion decisions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_completion_decision_delete_guard
BEFORE DELETE ON feature_workspace_completion_decisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature completion decisions are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_completion_reopening_guard
BEFORE INSERT ON feature_workspace_completion_reopenings
FOR EACH ROW WHEN NOT (
    (NEW.reopening_kind = 'ticket_revision' AND EXISTS (
        SELECT 1
        FROM feature_workspace_completion_decisions AS completion
        JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.reopening_ticket_revision_row_id
        JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
        WHERE completion.id = NEW.completion_decision_row_id
          AND ticket.workspace_row_id = completion.workspace_row_id
          AND ticket.current_revision_row_id = revision.id
          AND revision.cancellation_reason IS NULL
    )) OR
    (NEW.reopening_kind = 'authority_revision' AND EXISTS (
        SELECT 1
        FROM feature_workspace_completion_decisions AS completion
        JOIN feature_workspace_authority_revisions AS authority ON authority.id = NEW.reopening_authority_revision_row_id
        JOIN feature_workspaces AS workspace ON workspace.id = completion.workspace_row_id
        WHERE completion.id = NEW.completion_decision_row_id
          AND authority.workspace_row_id = completion.workspace_row_id
          AND workspace.current_authority_revision_row_id = authority.id
    )) OR
    (NEW.reopening_kind = 'remediation_seed' AND EXISTS (
        SELECT 1
        FROM feature_workspace_completion_decisions AS completion
        JOIN audit_remediation_seeds AS seed ON seed.id = NEW.reopening_remediation_seed_row_id
        JOIN audit_ticket_revision_decisions AS revision_decision ON revision_decision.id = seed.audit_ticket_revision_decision_row_id
        JOIN audit_packet_ticket_obligations AS obligation ON obligation.id = revision_decision.audit_packet_ticket_obligation_row_id
        JOIN delivery_tickets AS ticket ON ticket.id = obligation.delivery_ticket_row_id
        WHERE completion.id = NEW.completion_decision_row_id
          AND ticket.workspace_row_id = completion.workspace_row_id
    ))
)
BEGIN SELECT RAISE(ABORT, 'feature completion reopening must link a current workspace ticket authority or remediation seed'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_completion_reopening_update_immutable
BEFORE UPDATE ON feature_workspace_completion_reopenings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature completion reopenings are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_completion_reopening_delete_guard
BEFORE DELETE ON feature_workspace_completion_reopenings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature completion reopenings are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_guard;
DROP TRIGGER IF EXISTS audit_remediation_seed_reopening_delete_guard;
DROP TRIGGER IF EXISTS audit_remediation_seed_reopening_update_immutable;
DROP TRIGGER IF EXISTS audit_remediation_seed_reopening_guard;
DROP TRIGGER IF EXISTS audit_remediation_seed_finding_delete_guard;
DROP TRIGGER IF EXISTS audit_remediation_seed_finding_update_immutable;
DROP TRIGGER IF EXISTS audit_remediation_seed_delete_guard;
DROP TRIGGER IF EXISTS audit_remediation_seed_update_immutable;
DROP TRIGGER IF EXISTS audit_remediation_seed_guard;
DROP TRIGGER IF EXISTS delivery_ticket_revision_satisfaction_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_revision_satisfaction_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_revision_satisfaction_guard;
DROP TRIGGER IF EXISTS audit_ticket_revision_decision_delete_guard;
DROP TRIGGER IF EXISTS audit_ticket_revision_decision_update_immutable;
DROP TRIGGER IF EXISTS audit_ticket_revision_decision_guard;
DROP TRIGGER IF EXISTS audit_packet_ticket_obligation_delete_guard;
DROP TRIGGER IF EXISTS audit_packet_ticket_obligation_update_immutable;
DROP TRIGGER IF EXISTS audit_packet_ticket_obligation_guard;
DROP TRIGGER IF EXISTS audit_decision_delete_guard;
DROP TRIGGER IF EXISTS audit_decision_update_immutable;
DROP TRIGGER IF EXISTS audit_packet_delete_guard;
DROP TRIGGER IF EXISTS audit_packet_lifecycle_guard;
DROP TRIGGER IF EXISTS audit_packet_identity_immutable;
DROP INDEX IF EXISTS idx_feature_workspace_completion_decisions_workspace;
DROP INDEX IF EXISTS idx_audit_remediation_seed_reopenings_revision;
DROP INDEX IF EXISTS idx_audit_remediation_seeds_decision;
DROP INDEX IF EXISTS idx_delivery_ticket_revision_satisfactions_revision;
DROP INDEX IF EXISTS idx_audit_ticket_revision_decisions_decision;
DROP INDEX IF EXISTS idx_audit_packet_ticket_obligations_packet;
DROP TABLE IF EXISTS feature_workspace_completion_reopenings;
DROP TABLE IF EXISTS feature_workspace_completion_decisions;
DROP TABLE IF EXISTS audit_remediation_seed_reopenings;
DROP TABLE IF EXISTS audit_remediation_seed_findings;
DROP TABLE IF EXISTS audit_remediation_seeds;
DROP TABLE IF EXISTS delivery_ticket_revision_satisfactions;
DROP TABLE IF EXISTS audit_ticket_revision_decisions;
DROP TABLE IF EXISTS audit_packet_ticket_obligations;
