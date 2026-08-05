-- +goose Up
ALTER TABLE feature_workspace_integrated_discovery_revisions
ADD COLUMN settled_destination TEXT CHECK (settled_destination IS NULL OR settled_destination IN (
    'no_delivery_work', 'direct_delivery_ticket', 'requirements', 'shared_design',
    'requirements_then_shared_design', 'existing_route_continuation'
));
ALTER TABLE feature_workspace_integrated_discovery_revisions
ADD COLUMN continuation_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE feature_workspace_discovery_destination_assessments
ADD COLUMN currentness TEXT NOT NULL DEFAULT 'not_closed' CHECK (currentness IN (
    'current', 'historical', 'not_closed', 'legacy_unbound', 'integrity_failed'
));
ALTER TABLE feature_workspace_discovery_reopen_events
ADD COLUMN cause_artifact_row_id INTEGER REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT;

-- Existing rows predate exact reopen-cause retention. New rows must bind the
-- retained cause artifact owned by the same workspace.
-- +goose StatementBegin
CREATE TRIGGER discovery_reopen_cause_guard
BEFORE INSERT ON feature_workspace_discovery_reopen_events
FOR EACH ROW WHEN NEW.cause_artifact_row_id IS NULL OR NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_artifacts
    WHERE id = NEW.cause_artifact_row_id AND workspace_row_id = NEW.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'discovery reopen cause must be retained in its workspace'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER discovery_adoption_delete_guard BEFORE DELETE ON feature_workspace_discovery_adoptions FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery adoptions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_assessment_delete_guard BEFORE DELETE ON feature_workspace_discovery_destination_assessments FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery assessments are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_packet_delete_guard BEFORE DELETE ON feature_workspace_discovery_closure_packets FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery packets are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_packet_member_delete_guard BEFORE DELETE ON feature_workspace_discovery_closure_packet_members FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery packet members are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_reopen_delete_guard BEFORE DELETE ON feature_workspace_discovery_reopen_events FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery reopen events are immutable history'); END;
-- +goose StatementEnd

-- The current packet and current revision must move together and identify the
-- same exact closing basis whenever a packet is selected.
DROP TRIGGER feature_workspace_current_discovery_packet_guard;
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_current_discovery_packet_guard
BEFORE UPDATE OF current_discovery_closure_packet_row_id, current_discovery_revision_row_id ON feature_workspaces
FOR EACH ROW WHEN NEW.current_discovery_closure_packet_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_closure_packets
    WHERE id = NEW.current_discovery_closure_packet_row_id
      AND workspace_row_id = NEW.id
      AND closing_revision_row_id = NEW.current_discovery_revision_row_id
)
BEGIN SELECT RAISE(ABORT, 'current discovery closure packet does not close the current workspace revision'); END;
-- +goose StatementEnd

-- Packet-governed frontier records cannot change underneath a current packet.
-- +goose StatementBegin
CREATE TRIGGER discovery_closed_ticket_mutation_guard
BEFORE UPDATE ON feature_workspace_discovery_tickets
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM feature_workspaces
    WHERE id = OLD.workspace_row_id AND current_discovery_closure_packet_row_id IS NOT NULL
)
BEGIN SELECT RAISE(ABORT, 'closed discovery work items require explicit reopening'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_closed_resolution_guard
BEFORE INSERT ON feature_workspace_ticket_resolutions
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM feature_workspace_discovery_tickets AS ticket
    JOIN feature_workspaces AS workspace ON workspace.id = ticket.workspace_row_id
    WHERE ticket.id = NEW.ticket_row_id AND workspace.current_discovery_closure_packet_row_id IS NOT NULL
)
BEGIN SELECT RAISE(ABORT, 'closed discovery resolutions require explicit reopening'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_closed_consequence_guard
BEFORE INSERT ON feature_workspace_discovery_integration_consequences
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM feature_workspaces
    WHERE id = NEW.workspace_row_id AND current_discovery_closure_packet_row_id IS NOT NULL
)
BEGIN SELECT RAISE(ABORT, 'closed discovery integration requires explicit reopening'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS feature_workspace_current_discovery_packet_guard;
DROP TRIGGER IF EXISTS discovery_closed_consequence_guard;
DROP TRIGGER IF EXISTS discovery_closed_resolution_guard;
DROP TRIGGER IF EXISTS discovery_closed_ticket_mutation_guard;
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_current_discovery_packet_guard
BEFORE UPDATE OF current_discovery_closure_packet_row_id ON feature_workspaces
FOR EACH ROW WHEN NEW.current_discovery_closure_packet_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_closure_packets
    WHERE id = NEW.current_discovery_closure_packet_row_id AND workspace_row_id = NEW.id
)
BEGIN SELECT RAISE(ABORT, 'current discovery closure packet does not belong to workspace'); END;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS discovery_reopen_delete_guard;
DROP TRIGGER IF EXISTS discovery_packet_member_delete_guard;
DROP TRIGGER IF EXISTS discovery_packet_delete_guard;
DROP TRIGGER IF EXISTS discovery_assessment_delete_guard;
DROP TRIGGER IF EXISTS discovery_adoption_delete_guard;
DROP TRIGGER IF EXISTS discovery_reopen_cause_guard;
