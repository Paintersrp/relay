-- +goose Up
ALTER TABLE feature_workspaces ADD COLUMN current_discovery_closure_packet_row_id INTEGER;

CREATE TABLE feature_workspace_discovery_adoptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    adoption_id TEXT NOT NULL UNIQUE,
    operator_identity TEXT NOT NULL,
    adopted_workspace_version INTEGER NOT NULL CHECK (adopted_workspace_version >= 1),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (adoption_id GLOB 'discovery-adoption-*' AND trim(adoption_id) = adoption_id),
    CHECK (operator_identity <> '' AND trim(operator_identity) = operator_identity)
);

CREATE TABLE feature_workspace_discovery_destination_assessments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    assessment_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    discovery_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_integrated_discovery_revisions(id) ON DELETE RESTRICT,
    workspace_version INTEGER NOT NULL CHECK (workspace_version >= 1),
    discovery_state TEXT NOT NULL CHECK (discovery_state IN ('not_started', 'active', 'blocked', 'closed')),
    destination TEXT CHECK (destination IN ('no_delivery_work', 'direct_delivery_ticket', 'requirements', 'shared_design', 'requirements_then_shared_design', 'existing_route_continuation')),
    rationale TEXT NOT NULL,
    blockers_json TEXT NOT NULL,
    restoration_actions_json TEXT NOT NULL,
    continuation_json TEXT NOT NULL,
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (assessment_id GLOB 'discovery-assessment-*' AND trim(assessment_id) = assessment_id),
    CHECK (rationale <> '' AND created_identity <> '')
);

CREATE TABLE feature_workspace_discovery_closure_packets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    closure_packet_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    closing_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_integrated_discovery_revisions(id) ON DELETE RESTRICT,
    destination TEXT NOT NULL CHECK (destination IN ('no_delivery_work', 'direct_delivery_ticket', 'requirements', 'shared_design', 'requirements_then_shared_design', 'existing_route_continuation')),
    manifest_artifact_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    manifest_sha256 TEXT NOT NULL CHECK (length(manifest_sha256) = 64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
    manifest_size_bytes INTEGER NOT NULL CHECK (manifest_size_bytes >= 0),
    manifest_media_type TEXT NOT NULL CHECK (manifest_media_type = 'application/vnd.relay.feature-discovery-closure+json'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, closing_revision_row_id),
    CHECK (closure_packet_id GLOB 'discovery-packet-*' AND trim(closure_packet_id) = closure_packet_id)
);

CREATE TABLE feature_workspace_discovery_closure_packet_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    closure_packet_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_closure_packets(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    owner_family TEXT NOT NULL,
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    source_identity TEXT NOT NULL,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    media_type TEXT NOT NULL,
    semantic_role TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (closure_packet_row_id, sequence),
    UNIQUE (closure_packet_row_id, semantic_role, artifact_row_id)
);

CREATE TABLE feature_workspace_discovery_reopen_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reopen_event_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    closure_packet_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_closure_packets(id) ON DELETE RESTRICT,
    replacement_revision_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_integrated_discovery_revisions(id) ON DELETE RESTRICT,
    cause_text TEXT NOT NULL,
    confirmed_operator_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (reopen_event_id GLOB 'discovery-reopen-*' AND trim(reopen_event_id) = reopen_event_id),
    CHECK (cause_text <> '' AND confirmed_operator_identity <> '')
);

ALTER TABLE feature_workspace_completion_decisions ADD COLUMN discovery_closure_packet_row_id INTEGER REFERENCES feature_workspace_discovery_closure_packets(id) ON DELETE RESTRICT;

CREATE INDEX idx_discovery_assessments_workspace ON feature_workspace_discovery_destination_assessments(workspace_row_id, id);
CREATE INDEX idx_discovery_packets_workspace ON feature_workspace_discovery_closure_packets(workspace_row_id, id);
CREATE INDEX idx_discovery_packet_members_packet ON feature_workspace_discovery_closure_packet_members(closure_packet_row_id, sequence, id);
CREATE INDEX idx_discovery_reopen_events_workspace ON feature_workspace_discovery_reopen_events(workspace_row_id, id);

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_current_discovery_packet_guard
BEFORE UPDATE OF current_discovery_closure_packet_row_id ON feature_workspaces
FOR EACH ROW WHEN NEW.current_discovery_closure_packet_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_closure_packets
    WHERE id = NEW.current_discovery_closure_packet_row_id AND workspace_row_id = NEW.id
)
BEGIN SELECT RAISE(ABORT, 'current discovery closure packet does not belong to workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_assessment_workspace_guard
BEFORE INSERT ON feature_workspace_discovery_destination_assessments
FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_integrated_discovery_revisions WHERE id = NEW.discovery_revision_row_id AND workspace_row_id = NEW.workspace_row_id)
BEGIN SELECT RAISE(ABORT, 'discovery assessment revision does not belong to workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_packet_workspace_guard
BEFORE INSERT ON feature_workspace_discovery_closure_packets
FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_integrated_discovery_revisions WHERE id = NEW.closing_revision_row_id AND workspace_row_id = NEW.workspace_row_id)
 OR NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_artifacts WHERE id = NEW.manifest_artifact_row_id AND workspace_row_id = NEW.workspace_row_id AND sha256 = NEW.manifest_sha256 AND size_bytes = NEW.manifest_size_bytes AND media_type = NEW.manifest_media_type)
BEGIN SELECT RAISE(ABORT, 'discovery packet references must share a workspace and exact manifest'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_packet_member_workspace_guard
BEFORE INSERT ON feature_workspace_discovery_closure_packet_members
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_closure_packets AS packet
    JOIN feature_workspace_discovery_artifacts AS artifact ON artifact.id = NEW.artifact_row_id
    WHERE packet.id = NEW.closure_packet_row_id AND packet.workspace_row_id = artifact.workspace_row_id
      AND artifact.sha256 = NEW.sha256 AND artifact.size_bytes = NEW.size_bytes AND artifact.media_type = NEW.media_type
)
BEGIN SELECT RAISE(ABORT, 'discovery packet member must be an exact workspace artifact'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_reopen_workspace_guard
BEFORE INSERT ON feature_workspace_discovery_reopen_events
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_closure_packets AS packet
    JOIN feature_workspace_integrated_discovery_revisions AS revision ON revision.id = NEW.replacement_revision_row_id
    WHERE packet.id = NEW.closure_packet_row_id AND packet.workspace_row_id = NEW.workspace_row_id AND revision.workspace_row_id = NEW.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'discovery reopen references must share a workspace'); END;
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
CREATE TRIGGER discovery_adoption_update_immutable BEFORE UPDATE ON feature_workspace_discovery_adoptions FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery adoptions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_assessment_update_immutable BEFORE UPDATE ON feature_workspace_discovery_destination_assessments FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery assessments are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_packet_update_immutable BEFORE UPDATE ON feature_workspace_discovery_closure_packets FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery packets are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_packet_member_update_immutable BEFORE UPDATE ON feature_workspace_discovery_closure_packet_members FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery packet members are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_reopen_update_immutable BEFORE UPDATE ON feature_workspace_discovery_reopen_events FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery reopen events are immutable history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS discovery_reopen_update_immutable;
DROP TRIGGER IF EXISTS discovery_packet_member_update_immutable;
DROP TRIGGER IF EXISTS discovery_packet_update_immutable;
DROP TRIGGER IF EXISTS discovery_assessment_update_immutable;
DROP TRIGGER IF EXISTS discovery_adoption_update_immutable;
DROP TRIGGER IF EXISTS discovery_reopen_workspace_guard;
DROP TRIGGER IF EXISTS discovery_completion_packet_guard;
DROP TRIGGER IF EXISTS discovery_packet_member_workspace_guard;
DROP TRIGGER IF EXISTS discovery_packet_workspace_guard;
DROP TRIGGER IF EXISTS discovery_assessment_workspace_guard;
DROP TRIGGER IF EXISTS feature_workspace_current_discovery_packet_guard;
DROP INDEX IF EXISTS idx_discovery_reopen_events_workspace;
DROP INDEX IF EXISTS idx_discovery_packet_members_packet;
DROP INDEX IF EXISTS idx_discovery_packets_workspace;
DROP INDEX IF EXISTS idx_discovery_assessments_workspace;
DROP TABLE IF EXISTS feature_workspace_discovery_reopen_events;
DROP TABLE IF EXISTS feature_workspace_discovery_closure_packet_members;
DROP TABLE IF EXISTS feature_workspace_discovery_closure_packets;
DROP TABLE IF EXISTS feature_workspace_discovery_destination_assessments;
DROP TABLE IF EXISTS feature_workspace_discovery_adoptions;
