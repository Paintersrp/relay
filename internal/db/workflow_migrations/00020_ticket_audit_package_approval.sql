-- +goose Up
-- Add package_approval_row_id and approved_package_sha256 to ticket-aware audit
-- packet obligations and decision-effect basis rows. Add foreign keys and
-- consistency triggers requiring the approval to belong to the same package and
-- SHA. Preserve nullable compatibility only for legacy non-ticket audit rows.

ALTER TABLE audit_packet_ticket_obligations ADD COLUMN package_approval_row_id INTEGER REFERENCES execution_package_approvals(id) ON DELETE RESTRICT;
ALTER TABLE audit_packet_ticket_obligations ADD COLUMN approved_package_sha256 TEXT CHECK (approved_package_sha256 IS NULL OR (length(approved_package_sha256) = 64 AND approved_package_sha256 NOT GLOB '*[^0-9a-f]*'));

ALTER TABLE audit_ticket_revision_decisions ADD COLUMN package_approval_row_id INTEGER REFERENCES execution_package_approvals(id) ON DELETE RESTRICT;
ALTER TABLE audit_ticket_revision_decisions ADD COLUMN approved_package_sha256 TEXT CHECK (approved_package_sha256 IS NULL OR (length(approved_package_sha256) = 64 AND approved_package_sha256 NOT GLOB '*[^0-9a-f]*'));

-- +goose StatementBegin
CREATE TRIGGER audit_packet_ticket_obligation_approval_guard
BEFORE INSERT ON audit_packet_ticket_obligations
FOR EACH ROW WHEN NEW.package_approval_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM execution_package_approvals AS approval
    JOIN execution_packages AS package ON package.id = approval.package_row_id
    JOIN runs AS run ON run.id = (
        SELECT packet.run_row_id FROM audit_packets AS packet WHERE packet.id = NEW.audit_packet_row_id
    )
    WHERE approval.id = NEW.package_approval_row_id
      AND approval.package_row_id = NEW.execution_package_row_id
      AND approval.package_sha256 = NEW.approved_package_sha256
      AND approval.package_sha256 = package.package_sha256
      AND run.package_approval_row_id = approval.id
)
BEGIN SELECT RAISE(ABORT, 'audit packet ticket obligation must bind exact Run package approval with matching SHA'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_ticket_revision_decision_approval_guard
BEFORE INSERT ON audit_ticket_revision_decisions
FOR EACH ROW WHEN NEW.package_approval_row_id IS NOT NULL AND EXISTS (
    SELECT 1
    FROM audit_packet_ticket_obligations AS obligation
    JOIN audit_packets AS packet ON packet.id = obligation.audit_packet_row_id
    JOIN runs AS run ON run.id = packet.run_row_id
    WHERE obligation.id = NEW.audit_packet_ticket_obligation_row_id
      AND obligation.package_approval_row_id IS NOT NULL
)
AND NOT EXISTS (
    SELECT 1
    FROM audit_packet_ticket_obligations AS obligation
    JOIN audit_packets AS packet ON packet.id = obligation.audit_packet_row_id
    JOIN runs AS run ON run.id = packet.run_row_id
    JOIN execution_package_approvals AS approval ON approval.id = NEW.package_approval_row_id
    WHERE obligation.id = NEW.audit_packet_ticket_obligation_row_id
      AND obligation.package_approval_row_id = NEW.package_approval_row_id
      AND obligation.approved_package_sha256 = NEW.approved_package_sha256
      AND run.package_approval_row_id = approval.id
)
BEGIN SELECT RAISE(ABORT, 'ticket revision decision must carry the exact obligation package approval and SHA'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS audit_ticket_revision_decision_approval_guard;
DROP TRIGGER IF EXISTS audit_packet_ticket_obligation_approval_guard;
ALTER TABLE audit_ticket_revision_decisions DROP COLUMN approved_package_sha256;
ALTER TABLE audit_ticket_revision_decisions DROP COLUMN package_approval_row_id;
ALTER TABLE audit_packet_ticket_obligations DROP COLUMN approved_package_sha256;
ALTER TABLE audit_packet_ticket_obligations DROP COLUMN package_approval_row_id;
