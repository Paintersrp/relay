-- +goose Up
CREATE TABLE feature_workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    feature_slug TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    current_route_state_row_id INTEGER,
    current_authority_revision_row_id INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_row_id, feature_slug),
    CHECK (workspace_id GLOB 'workspace-*' AND trim(workspace_id) = workspace_id),
    CHECK (feature_slug <> '' AND trim(feature_slug) = feature_slug)
);

CREATE TABLE feature_workspace_admitted_inputs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    admitted_input_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    input_name TEXT NOT NULL,
    input_role TEXT NOT NULL CHECK (input_role IN ('candidate', 'governing', 'authority', 'evidence')),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('uploaded_file', 'relay_artifact', 'inline_text', 'workflow_record', 'committed_source')),
    artifact_row_id INTEGER REFERENCES artifacts(id) ON DELETE RESTRICT,
    retained_artifact_row_id INTEGER REFERENCES operation_packet_retained_artifacts(id) ON DELETE RESTRICT,
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT,
    source_reference TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, sequence),
    UNIQUE (workspace_row_id, input_name),
    CHECK (admitted_input_id GLOB 'input-*' AND trim(admitted_input_id) = admitted_input_id),
    CHECK (input_name <> '' AND trim(input_name) = input_name),
    CHECK (source_reference = trim(source_reference) AND length(source_reference) <= 512),
    CHECK (artifact_sha256 IS NULL OR (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*')),
    CHECK ((artifact_row_id IS NOT NULL AND retained_artifact_row_id IS NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NOT NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NULL AND source_kind = 'committed_source')),
    CHECK ((source_kind = 'committed_source' AND source_closure_row_id IS NOT NULL AND artifact_sha256 IS NULL) OR (source_kind <> 'committed_source' AND source_closure_row_id IS NULL AND artifact_sha256 IS NOT NULL))
);

CREATE TABLE feature_workspace_destinations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    destination_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    destination_kind TEXT NOT NULL CHECK (destination_kind IN ('destination', 'fog')),
    destination_key TEXT NOT NULL,
    repo_target TEXT COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, sequence),
    UNIQUE (workspace_row_id, destination_kind, destination_key),
    CHECK (destination_id GLOB 'destination-*' AND trim(destination_id) = destination_id),
    CHECK (destination_key <> '' AND trim(destination_key) = destination_key AND length(destination_key) <= 512)
);

CREATE TABLE feature_workspace_discovery_tickets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    discovery_ticket_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    ticket_key TEXT NOT NULL,
    subject TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'blocked', 'resolved', 'cancelled')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, ticket_key),
    CHECK (discovery_ticket_id GLOB 'discovery-*' AND trim(discovery_ticket_id) = discovery_ticket_id),
    CHECK (ticket_key <> '' AND trim(ticket_key) = ticket_key AND length(ticket_key) <= 128),
    CHECK (subject <> '' AND trim(subject) <> '' AND length(subject) <= 1024)
);

CREATE TABLE feature_workspace_ticket_dependencies (
    ticket_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    depends_on_ticket_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    dependency_kind TEXT NOT NULL CHECK (dependency_kind IN ('blocks', 'informs')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (ticket_row_id, depends_on_ticket_row_id),
    CHECK (ticket_row_id <> depends_on_ticket_row_id)
);

CREATE TABLE feature_workspace_ticket_resolutions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    resolution_id TEXT NOT NULL UNIQUE,
    ticket_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    resolution_kind TEXT NOT NULL CHECK (resolution_kind IN ('resolved', 'rejected', 'deferred')),
    artifact_row_id INTEGER REFERENCES artifacts(id) ON DELETE RESTRICT,
    retained_artifact_row_id INTEGER REFERENCES operation_packet_retained_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (ticket_row_id, sequence),
    CHECK (resolution_id GLOB 'resolution-*' AND trim(resolution_id) = resolution_id),
    CHECK ((artifact_row_id IS NOT NULL AND retained_artifact_row_id IS NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NOT NULL))
);

CREATE TABLE feature_workspace_route_states (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    route_state_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    workspace_version INTEGER NOT NULL CHECK (workspace_version >= 2),
    state TEXT NOT NULL CHECK (state IN ('discovery', 'ready', 'blocked', 'resolved', 'closed')),
    ticket_row_id INTEGER REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, sequence),
    UNIQUE (workspace_row_id, workspace_version),
    CHECK (route_state_id GLOB 'route-*' AND trim(route_state_id) = route_state_id)
);

CREATE TABLE feature_workspace_investigations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    investigation_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    ticket_row_id INTEGER REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    investigation_kind TEXT NOT NULL CHECK (investigation_kind IN ('source', 'artifact', 'dependency')),
    artifact_row_id INTEGER REFERENCES artifacts(id) ON DELETE RESTRICT,
    retained_artifact_row_id INTEGER REFERENCES operation_packet_retained_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, sequence),
    CHECK (investigation_id GLOB 'investigation-*' AND trim(investigation_id) = investigation_id),
    CHECK ((artifact_row_id IS NOT NULL AND retained_artifact_row_id IS NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NOT NULL))
);

CREATE TABLE feature_workspace_authority_revisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    authority_revision_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    revision_number INTEGER NOT NULL CHECK (revision_number >= 1),
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, revision_number),
    CHECK (authority_revision_id GLOB 'authority-*' AND trim(authority_revision_id) = authority_revision_id)
);

CREATE TABLE feature_workspace_authority_layers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    layer_kind TEXT NOT NULL CHECK (layer_kind IN ('requirements', 'design', 'plan', 'execution_spec', 'audit_decision')),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    artifact_row_id INTEGER REFERENCES artifacts(id) ON DELETE RESTRICT,
    retained_artifact_row_id INTEGER REFERENCES operation_packet_retained_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (authority_revision_row_id, sequence),
    UNIQUE (authority_revision_row_id, layer_kind),
    CHECK ((artifact_row_id IS NOT NULL AND retained_artifact_row_id IS NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NOT NULL))
);

CREATE INDEX idx_feature_workspaces_project ON feature_workspaces(project_row_id, feature_slug, id);
CREATE INDEX idx_feature_workspace_inputs_workspace ON feature_workspace_admitted_inputs(workspace_row_id, sequence, id);
CREATE INDEX idx_feature_workspace_destinations_workspace ON feature_workspace_destinations(workspace_row_id, sequence, id);
CREATE INDEX idx_feature_workspace_tickets_workspace ON feature_workspace_discovery_tickets(workspace_row_id, state, id);
CREATE INDEX idx_feature_workspace_ticket_dependencies_ticket ON feature_workspace_ticket_dependencies(ticket_row_id, depends_on_ticket_row_id);
CREATE INDEX idx_feature_workspace_resolutions_ticket ON feature_workspace_ticket_resolutions(ticket_row_id, sequence, id);
CREATE INDEX idx_feature_workspace_route_states_workspace ON feature_workspace_route_states(workspace_row_id, sequence, id);
CREATE INDEX idx_feature_workspace_investigations_workspace ON feature_workspace_investigations(workspace_row_id, sequence, id);
CREATE INDEX idx_feature_workspace_authority_revisions_workspace ON feature_workspace_authority_revisions(workspace_row_id, revision_number, id);
CREATE INDEX idx_feature_workspace_authority_layers_revision ON feature_workspace_authority_layers(authority_revision_row_id, sequence, id);

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_identity_immutable
BEFORE UPDATE OF workspace_id, project_row_id, feature_slug, created_at ON feature_workspaces
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature workspace identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_version_guard
BEFORE UPDATE ON feature_workspaces
FOR EACH ROW WHEN NEW.version <> OLD.version + 1
BEGIN SELECT RAISE(ABORT, 'feature workspace updates require the next version'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_current_route_guard
BEFORE UPDATE OF current_route_state_row_id ON feature_workspaces
FOR EACH ROW WHEN NEW.current_route_state_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_route_states
    WHERE id = NEW.current_route_state_row_id
      AND workspace_row_id = NEW.id
      AND workspace_version = NEW.version
)
BEGIN SELECT RAISE(ABORT, 'feature workspace route state does not match workspace version'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_current_authority_guard
BEFORE UPDATE OF current_authority_revision_row_id ON feature_workspaces
FOR EACH ROW WHEN NEW.current_authority_revision_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_authority_revisions
    WHERE id = NEW.current_authority_revision_row_id AND workspace_row_id = NEW.id
)
BEGIN SELECT RAISE(ABORT, 'feature workspace authority revision does not belong to workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_delete_guard
BEFORE DELETE ON feature_workspaces
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature workspaces are retained authority'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_exact_input_artifact_guard
BEFORE INSERT ON feature_workspace_admitted_inputs
FOR EACH ROW WHEN (NEW.artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM artifacts WHERE id = NEW.artifact_row_id AND sha256 = NEW.artifact_sha256)) OR (NEW.retained_artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM operation_packet_retained_artifacts WHERE id = NEW.retained_artifact_row_id AND sha256 = NEW.artifact_sha256))
BEGIN SELECT RAISE(ABORT, 'admitted input artifact reference does not match sha256'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_input_update_immutable
BEFORE UPDATE ON feature_workspace_admitted_inputs
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'admitted inputs are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_input_delete_guard
BEFORE DELETE ON feature_workspace_admitted_inputs
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'admitted inputs are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_destination_update_immutable
BEFORE UPDATE ON feature_workspace_destinations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature workspace destinations are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_destination_delete_guard
BEFORE DELETE ON feature_workspace_destinations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'feature workspace destinations are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_ticket_version_guard
BEFORE UPDATE ON feature_workspace_discovery_tickets
FOR EACH ROW WHEN NEW.version <> OLD.version + 1
BEGIN SELECT RAISE(ABORT, 'discovery ticket updates require the next version'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_ticket_identity_immutable
BEFORE UPDATE OF discovery_ticket_id, workspace_row_id, ticket_key, subject, created_at ON feature_workspace_discovery_tickets
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery ticket identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_ticket_dependency_same_workspace
BEFORE INSERT ON feature_workspace_ticket_dependencies
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_tickets AS ticket
    JOIN feature_workspace_discovery_tickets AS dependency ON dependency.id = NEW.depends_on_ticket_row_id
    WHERE ticket.id = NEW.ticket_row_id AND ticket.workspace_row_id = dependency.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'discovery ticket dependencies must share a workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_ticket_dependency_update_immutable
BEFORE UPDATE ON feature_workspace_ticket_dependencies
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery ticket dependencies are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_ticket_dependency_delete_guard
BEFORE DELETE ON feature_workspace_ticket_dependencies
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'discovery ticket dependencies are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_exact_resolution_artifact_guard
BEFORE INSERT ON feature_workspace_ticket_resolutions
FOR EACH ROW WHEN (NEW.artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM artifacts WHERE id = NEW.artifact_row_id AND sha256 = NEW.artifact_sha256)) OR (NEW.retained_artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM operation_packet_retained_artifacts WHERE id = NEW.retained_artifact_row_id AND sha256 = NEW.artifact_sha256))
BEGIN SELECT RAISE(ABORT, 'ticket resolution artifact reference does not match sha256'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_resolution_update_immutable
BEFORE UPDATE ON feature_workspace_ticket_resolutions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'ticket resolutions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_resolution_delete_guard
BEFORE DELETE ON feature_workspace_ticket_resolutions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'ticket resolutions are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_route_state_ticket_guard
BEFORE INSERT ON feature_workspace_route_states
FOR EACH ROW WHEN NEW.ticket_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_tickets WHERE id = NEW.ticket_row_id AND workspace_row_id = NEW.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'route state ticket does not belong to workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_route_state_version_guard
BEFORE INSERT ON feature_workspace_route_states
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM feature_workspaces WHERE id = NEW.workspace_row_id AND version + 1 = NEW.workspace_version
)
BEGIN SELECT RAISE(ABORT, 'route state must target the next workspace version'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_route_state_update_immutable
BEFORE UPDATE ON feature_workspace_route_states
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'route states are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_route_state_delete_guard
BEFORE DELETE ON feature_workspace_route_states
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'route states are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_investigation_ticket_guard
BEFORE INSERT ON feature_workspace_investigations
FOR EACH ROW WHEN NEW.ticket_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_tickets WHERE id = NEW.ticket_row_id AND workspace_row_id = NEW.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'investigation ticket does not belong to workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_exact_investigation_artifact_guard
BEFORE INSERT ON feature_workspace_investigations
FOR EACH ROW WHEN (NEW.artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM artifacts WHERE id = NEW.artifact_row_id AND sha256 = NEW.artifact_sha256)) OR (NEW.retained_artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM operation_packet_retained_artifacts WHERE id = NEW.retained_artifact_row_id AND sha256 = NEW.artifact_sha256))
BEGIN SELECT RAISE(ABORT, 'investigation artifact reference does not match sha256'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_investigation_update_immutable
BEFORE UPDATE ON feature_workspace_investigations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'investigations are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_investigation_delete_guard
BEFORE DELETE ON feature_workspace_investigations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'investigations are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_revision_update_immutable
BEFORE UPDATE ON feature_workspace_authority_revisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'authority revisions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_revision_delete_guard
BEFORE DELETE ON feature_workspace_authority_revisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'authority revisions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_exact_authority_layer_artifact_guard
BEFORE INSERT ON feature_workspace_authority_layers
FOR EACH ROW WHEN (NEW.artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM artifacts WHERE id = NEW.artifact_row_id AND sha256 = NEW.artifact_sha256)) OR (NEW.retained_artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM operation_packet_retained_artifacts WHERE id = NEW.retained_artifact_row_id AND sha256 = NEW.artifact_sha256))
BEGIN SELECT RAISE(ABORT, 'authority layer artifact reference does not match sha256'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_layer_update_immutable
BEFORE UPDATE ON feature_workspace_authority_layers
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'authority layers are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_layer_delete_guard
BEFORE DELETE ON feature_workspace_authority_layers
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'authority layers are immutable history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_exact_authority_layer_artifact_guard;
DROP TRIGGER IF EXISTS feature_workspace_authority_revision_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_authority_revision_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_investigation_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_investigation_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_exact_investigation_artifact_guard;
DROP TRIGGER IF EXISTS feature_workspace_investigation_ticket_guard;
DROP TRIGGER IF EXISTS feature_workspace_route_state_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_route_state_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_route_state_version_guard;
DROP TRIGGER IF EXISTS feature_workspace_route_state_ticket_guard;
DROP TRIGGER IF EXISTS feature_workspace_resolution_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_resolution_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_exact_resolution_artifact_guard;
DROP TRIGGER IF EXISTS feature_workspace_ticket_dependency_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_ticket_dependency_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_ticket_dependency_same_workspace;
DROP TRIGGER IF EXISTS feature_workspace_ticket_identity_immutable;
DROP TRIGGER IF EXISTS feature_workspace_ticket_version_guard;
DROP TRIGGER IF EXISTS feature_workspace_destination_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_destination_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_input_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_input_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_exact_input_artifact_guard;
DROP TRIGGER IF EXISTS feature_workspace_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_current_authority_guard;
DROP TRIGGER IF EXISTS feature_workspace_current_route_guard;
DROP TRIGGER IF EXISTS feature_workspace_version_guard;
DROP TRIGGER IF EXISTS feature_workspace_identity_immutable;
DROP INDEX IF EXISTS idx_feature_workspace_authority_layers_revision;
DROP INDEX IF EXISTS idx_feature_workspace_authority_revisions_workspace;
DROP INDEX IF EXISTS idx_feature_workspace_investigations_workspace;
DROP INDEX IF EXISTS idx_feature_workspace_route_states_workspace;
DROP INDEX IF EXISTS idx_feature_workspace_resolutions_ticket;
DROP INDEX IF EXISTS idx_feature_workspace_ticket_dependencies_ticket;
DROP INDEX IF EXISTS idx_feature_workspace_tickets_workspace;
DROP INDEX IF EXISTS idx_feature_workspace_destinations_workspace;
DROP INDEX IF EXISTS idx_feature_workspace_inputs_workspace;
DROP INDEX IF EXISTS idx_feature_workspaces_project;
DROP TABLE IF EXISTS feature_workspace_authority_layers;
DROP TABLE IF EXISTS feature_workspace_authority_revisions;
DROP TABLE IF EXISTS feature_workspace_investigations;
DROP TABLE IF EXISTS feature_workspace_route_states;
DROP TABLE IF EXISTS feature_workspace_ticket_resolutions;
DROP TABLE IF EXISTS feature_workspace_ticket_dependencies;
DROP TABLE IF EXISTS feature_workspace_discovery_tickets;
DROP TABLE IF EXISTS feature_workspace_destinations;
DROP TABLE IF EXISTS feature_workspace_admitted_inputs;
DROP TABLE IF EXISTS feature_workspaces;
