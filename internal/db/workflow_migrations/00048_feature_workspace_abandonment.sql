-- +goose Up
-- +goose NO TRANSACTION
-- Feature-owned abandonment records the same immutable closing decision as
-- completion: the only schema change is the decision CHECK expansion. Every
-- existing guard trigger, packet trigger, immutability trigger, reopen table,
-- and history invariant is preserved unchanged.
PRAGMA foreign_keys=off;

DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_guard;
DROP TRIGGER IF EXISTS discovery_completion_packet_guard;

CREATE TABLE feature_workspace_completion_decisions_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    completion_decision_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    discovery_closure_packet_row_id INTEGER REFERENCES feature_workspace_discovery_closure_packets(id) ON DELETE RESTRICT,
    decision TEXT NOT NULL CHECK (decision IN ('completed', 'abandoned')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (completion_decision_id GLOB 'completion-*' AND trim(completion_decision_id) = completion_decision_id)
);

INSERT INTO feature_workspace_completion_decisions_next (
    id, completion_decision_id, workspace_row_id, authority_revision_row_id,
    source_closure_row_id, discovery_closure_packet_row_id, decision, created_at
)
SELECT id, completion_decision_id, workspace_row_id, authority_revision_row_id,
       source_closure_row_id, discovery_closure_packet_row_id, decision, created_at
FROM feature_workspace_completion_decisions;

DROP TABLE feature_workspace_completion_decisions;
ALTER TABLE feature_workspace_completion_decisions_next RENAME TO feature_workspace_completion_decisions;

CREATE INDEX idx_feature_workspace_completion_decisions_workspace
ON feature_workspace_completion_decisions(workspace_row_id, created_at, id);

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
CREATE TRIGGER discovery_completion_packet_guard
BEFORE INSERT ON feature_workspace_completion_decisions
FOR EACH ROW WHEN EXISTS (SELECT 1 FROM feature_workspace_discovery_adoptions WHERE workspace_row_id = NEW.workspace_row_id)
 AND NOT EXISTS (
    SELECT 1 FROM feature_workspaces AS workspace
    JOIN feature_workspace_discovery_closure_packets AS packet ON packet.id = NEW.discovery_closure_packet_row_id
    WHERE workspace.id = NEW.workspace_row_id
      AND workspace.current_discovery_closure_packet_row_id = packet.id
      AND packet.workspace_row_id = workspace.id
      AND packet.closing_revision_row_id = workspace.current_discovery_revision_row_id
 )
BEGIN SELECT RAISE(ABORT, 'adopted workspace completion requires its current discovery closure packet'); END;
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

PRAGMA foreign_keys=on;

-- +goose Down
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_completion_reopening_guard;
DROP TRIGGER IF EXISTS discovery_completion_packet_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_completion_decision_guard;
DROP INDEX IF EXISTS idx_feature_workspace_completion_decisions_workspace;
DROP TABLE IF EXISTS feature_workspace_completion_decisions;
