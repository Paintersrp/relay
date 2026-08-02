-- +goose Up
ALTER TABLE feature_workspaces ADD COLUMN discovery_capability_enabled INTEGER NOT NULL DEFAULT 0 CHECK (discovery_capability_enabled IN (0, 1));
ALTER TABLE feature_workspaces ADD COLUMN current_discovery_revision_row_id INTEGER;

CREATE TABLE feature_workspace_discovery_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    discovery_artifact_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    relative_path TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (discovery_artifact_id GLOB 'discovery-artifact-*' AND trim(discovery_artifact_id) = discovery_artifact_id),
    CHECK (relative_path GLOB 'feature-discovery/*/*/*' AND trim(relative_path) = relative_path)
);

CREATE TABLE feature_workspace_integrated_discovery_revisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    discovery_revision_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    revision_number INTEGER NOT NULL CHECK (revision_number >= 1),
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    predecessor_revision_row_id INTEGER REFERENCES feature_workspace_integrated_discovery_revisions(id) ON DELETE RESTRICT,
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, revision_number),
    CHECK (discovery_revision_id GLOB 'discovery-revision-*' AND trim(discovery_revision_id) = discovery_revision_id),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);

CREATE TABLE feature_workspace_discovery_work_item_metadata (
    ticket_row_id INTEGER PRIMARY KEY REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    work_item_kind TEXT NOT NULL,
    route_material INTEGER NOT NULL DEFAULT 0 CHECK (route_material IN (0, 1)),
    legacy_adopted INTEGER NOT NULL DEFAULT 0 CHECK (legacy_adopted IN (0, 1)),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (work_item_kind <> '' AND trim(work_item_kind) = work_item_kind)
);

CREATE TABLE feature_workspace_discovery_integration_consequences (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    integration_consequence_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    ticket_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    resolution_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_ticket_resolutions(id) ON DELETE RESTRICT,
    consequence_kind TEXT NOT NULL CHECK (consequence_kind IN ('integrated', 'no_material_change', 'superseded')),
    produced_revision_row_id INTEGER UNIQUE REFERENCES feature_workspace_integrated_discovery_revisions(id) ON DELETE RESTRICT,
    replacement_ticket_row_id INTEGER REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    evidence_basis TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (integration_consequence_id GLOB 'integration-*' AND trim(integration_consequence_id) = integration_consequence_id),
    CHECK (evidence_basis <> '' AND trim(evidence_basis) = evidence_basis),
    CHECK ((consequence_kind = 'integrated' AND produced_revision_row_id IS NOT NULL AND replacement_ticket_row_id IS NULL) OR
           (consequence_kind = 'no_material_change' AND produced_revision_row_id IS NULL AND replacement_ticket_row_id IS NULL) OR
           (consequence_kind = 'superseded' AND produced_revision_row_id IS NULL AND replacement_ticket_row_id IS NOT NULL))
);

CREATE INDEX idx_discovery_artifacts_workspace ON feature_workspace_discovery_artifacts(workspace_row_id, id);
CREATE INDEX idx_discovery_revisions_workspace ON feature_workspace_integrated_discovery_revisions(workspace_row_id, revision_number, id);
CREATE INDEX idx_discovery_consequences_ticket ON feature_workspace_discovery_integration_consequences(ticket_row_id, id);

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_current_discovery_guard
BEFORE UPDATE OF current_discovery_revision_row_id ON feature_workspaces
FOR EACH ROW WHEN NEW.current_discovery_revision_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_integrated_discovery_revisions
    WHERE id = NEW.current_discovery_revision_row_id AND workspace_row_id = NEW.id
)
BEGIN SELECT RAISE(ABORT, 'current discovery revision does not belong to workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_revision_artifact_workspace_guard
BEFORE INSERT ON feature_workspace_integrated_discovery_revisions
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_artifacts WHERE id = NEW.artifact_row_id AND workspace_row_id = NEW.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'discovery revision artifact does not belong to workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_consequence_workspace_guard
BEFORE INSERT ON feature_workspace_discovery_integration_consequences
FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_tickets WHERE id = NEW.ticket_row_id AND workspace_row_id = NEW.workspace_row_id)
 OR NOT EXISTS (SELECT 1 FROM feature_workspace_ticket_resolutions AS r JOIN feature_workspace_discovery_tickets AS t ON t.id = r.ticket_row_id WHERE r.id = NEW.resolution_row_id AND r.ticket_row_id = NEW.ticket_row_id AND t.workspace_row_id = NEW.workspace_row_id)
 OR (NEW.replacement_ticket_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_tickets WHERE id = NEW.replacement_ticket_row_id AND workspace_row_id = NEW.workspace_row_id))
 OR (NEW.produced_revision_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM feature_workspace_integrated_discovery_revisions WHERE id = NEW.produced_revision_row_id AND workspace_row_id = NEW.workspace_row_id))
BEGIN SELECT RAISE(ABORT, 'discovery integration references must share a workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_artifact_update_immutable BEFORE UPDATE ON feature_workspace_discovery_artifacts FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery artifacts are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_revision_update_immutable BEFORE UPDATE ON feature_workspace_integrated_discovery_revisions FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery revisions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER discovery_consequence_update_immutable BEFORE UPDATE ON feature_workspace_discovery_integration_consequences FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery integration consequences are immutable history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS discovery_consequence_update_immutable;
DROP TRIGGER IF EXISTS discovery_revision_update_immutable;
DROP TRIGGER IF EXISTS discovery_artifact_update_immutable;
DROP TRIGGER IF EXISTS discovery_consequence_workspace_guard;
DROP TRIGGER IF EXISTS discovery_revision_artifact_workspace_guard;
DROP TRIGGER IF EXISTS feature_workspace_current_discovery_guard;
DROP TABLE IF EXISTS feature_workspace_discovery_integration_consequences;
DROP TABLE IF EXISTS feature_workspace_discovery_work_item_metadata;
DROP TABLE IF EXISTS feature_workspace_integrated_discovery_revisions;
DROP TABLE IF EXISTS feature_workspace_discovery_artifacts;
