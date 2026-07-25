-- +goose Up
CREATE TABLE operation_packet_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL DEFAULT 'operation_packet_document' CHECK (kind = 'operation_packet_document'),
    relative_path TEXT NOT NULL UNIQUE,
    media_type TEXT NOT NULL DEFAULT 'application/vnd.relay.operation-packet+json;version=1' CHECK (media_type = 'application/vnd.relay.operation-packet+json;version=1'),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (artifact_id GLOB 'artifact-*' AND trim(artifact_id) = artifact_id),
    CHECK (relative_path <> '' AND trim(relative_path) = relative_path)
);

CREATE TABLE operation_packets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    packet_id TEXT NOT NULL UNIQUE,
    packet_sha256 TEXT NOT NULL CHECK (length(packet_sha256) = 64 AND packet_sha256 NOT GLOB '*[^0-9a-f]*'),
    schema_version TEXT NOT NULL DEFAULT 'relay.operation-packet.v1' CHECK (schema_version = 'relay.operation-packet.v1'),
    role TEXT NOT NULL CHECK (role IN ('planner', 'auditor')),
    operation_id TEXT NOT NULL CHECK (operation_id <> '' AND trim(operation_id) = operation_id),
    surface_contract_id TEXT NOT NULL CHECK (surface_contract_id <> '' AND trim(surface_contract_id) = surface_contract_id),
    project_id TEXT NOT NULL CHECK (project_id <> '' AND trim(project_id) = project_id),
    readiness_state TEXT NOT NULL DEFAULT 'ready' CHECK (readiness_state = 'ready'),
    lifecycle_state TEXT NOT NULL DEFAULT 'active' CHECK (lifecycle_state IN ('active', 'superseded', 'closed')),
    prior_packet_row_id INTEGER UNIQUE REFERENCES operation_packets(id) ON DELETE RESTRICT,
    replacement_packet_row_id INTEGER UNIQUE REFERENCES operation_packets(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL CHECK (length(created_at) = 30 AND created_at GLOB '????-??-??T??:??:??.?????????Z' AND created_at NOT GLOB '*[^0-9TZ:.-]*'),
    superseded_at TEXT,
    closed_at TEXT,
    packet_artifact_row_id INTEGER NOT NULL UNIQUE REFERENCES operation_packet_artifacts(id) ON DELETE RESTRICT,
    CHECK (packet_id GLOB 'opkt-*' AND trim(packet_id) = packet_id),
    CHECK (prior_packet_row_id IS NULL OR prior_packet_row_id <> id),
    CHECK (replacement_packet_row_id IS NULL OR replacement_packet_row_id <> id),
    CHECK (superseded_at IS NULL OR (length(superseded_at) = 30 AND superseded_at GLOB '????-??-??T??:??:??.?????????Z' AND superseded_at NOT GLOB '*[^0-9TZ:.-]*')),
    CHECK (closed_at IS NULL OR (length(closed_at) = 30 AND closed_at GLOB '????-??-??T??:??:??.?????????Z' AND closed_at NOT GLOB '*[^0-9TZ:.-]*')),
    CHECK ((lifecycle_state = 'active' AND replacement_packet_row_id IS NULL AND superseded_at IS NULL AND closed_at IS NULL) OR (lifecycle_state = 'superseded' AND replacement_packet_row_id IS NOT NULL AND superseded_at IS NOT NULL AND closed_at IS NULL) OR (lifecycle_state = 'closed' AND replacement_packet_row_id IS NULL AND superseded_at IS NULL AND closed_at IS NOT NULL))
);

CREATE TABLE operation_packet_retention_dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    packet_row_id INTEGER NOT NULL REFERENCES operation_packets(id) ON DELETE RESTRICT,
    dependency_class TEXT NOT NULL CHECK (dependency_class IN ('packet_document', 'input_artifact', 'workflow_snapshot', 'repository_vault', 'git_path_object', 'manifest_member', 'run_artifact')),
    dependency_key TEXT NOT NULL CHECK (dependency_key <> '' AND trim(dependency_key) = dependency_key AND length(dependency_key) <= 512),
    required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
    attached INTEGER NOT NULL DEFAULT 0 CHECK (attached IN (0, 1)),
    retained INTEGER NOT NULL DEFAULT 0 CHECK (retained IN (0, 1)),
    owner_identity TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (packet_row_id, dependency_class, dependency_key),
    CHECK (owner_identity IS NULL OR (trim(owner_identity) = owner_identity AND length(owner_identity) BETWEEN 1 AND 512)),
    CHECK ((attached = 0 AND retained = 0 AND owner_identity IS NULL) OR (attached = 1 AND owner_identity IS NOT NULL AND trim(owner_identity) <> '')),
    CHECK (dependency_class <> 'packet_document' OR (required = 1 AND attached = 1 AND retained = 1))
);

CREATE INDEX idx_operation_packets_project_lifecycle ON operation_packets(project_id, lifecycle_state, created_at, id);
CREATE INDEX idx_operation_packets_prior ON operation_packets(prior_packet_row_id);
CREATE INDEX idx_operation_packet_dependencies_packet_class ON operation_packet_retention_dependencies(packet_row_id, dependency_class, dependency_key);

-- +goose StatementBegin
CREATE TRIGGER operation_packet_artifact_immutable_update BEFORE UPDATE ON operation_packet_artifacts FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'operation packet artifacts are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_artifact_delete_guard BEFORE DELETE ON operation_packet_artifacts FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'operation packet artifacts are retained authority'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_insert_artifact_guard BEFORE INSERT ON operation_packets FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM operation_packet_artifacts AS artifact WHERE artifact.id = NEW.packet_artifact_row_id AND artifact.sha256 = NEW.packet_sha256 AND artifact.kind = 'operation_packet_document' AND artifact.media_type = 'application/vnd.relay.operation-packet+json;version=1') BEGIN SELECT RAISE(ABORT, 'operation packet artifact identity mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_insert_prior_guard BEFORE INSERT ON operation_packets FOR EACH ROW WHEN NEW.prior_packet_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM operation_packets AS prior WHERE prior.id = NEW.prior_packet_row_id AND prior.lifecycle_state = 'active' AND prior.replacement_packet_row_id IS NULL AND prior.role = NEW.role AND prior.operation_id = NEW.operation_id AND prior.surface_contract_id = NEW.surface_contract_id AND prior.project_id = NEW.project_id) BEGIN SELECT RAISE(ABORT, 'replacement prior packet is not an active matching lineage'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_immutable_update BEFORE UPDATE OF packet_id, packet_sha256, schema_version, role, operation_id, surface_contract_id, project_id, readiness_state, prior_packet_row_id, created_at, packet_artifact_row_id ON operation_packets FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'operation packet identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_lifecycle_transition_guard BEFORE UPDATE OF lifecycle_state, replacement_packet_row_id, superseded_at, closed_at ON operation_packets FOR EACH ROW WHEN NOT (OLD.lifecycle_state = 'active' AND NEW.lifecycle_state = 'superseded' AND NEW.replacement_packet_row_id IS NOT NULL AND NEW.superseded_at IS NOT NULL AND NEW.closed_at IS NULL AND EXISTS (SELECT 1 FROM operation_packets AS replacement WHERE replacement.id = NEW.replacement_packet_row_id AND replacement.prior_packet_row_id = OLD.id AND replacement.lifecycle_state = 'active' AND replacement.role = OLD.role AND replacement.operation_id = OLD.operation_id AND replacement.surface_contract_id = OLD.surface_contract_id AND replacement.project_id = OLD.project_id)) AND NOT (OLD.lifecycle_state = 'active' AND NEW.lifecycle_state = 'closed' AND NEW.replacement_packet_row_id IS NULL AND NEW.superseded_at IS NULL AND NEW.closed_at IS NOT NULL) BEGIN SELECT RAISE(ABORT, 'invalid operation packet lifecycle transition'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_delete_guard BEFORE DELETE ON operation_packets FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'operation packets are immutable retained authority'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_dependency_document_guard BEFORE INSERT ON operation_packet_retention_dependencies FOR EACH ROW WHEN NEW.dependency_class = 'packet_document' AND NOT EXISTS (SELECT 1 FROM operation_packets AS packet JOIN operation_packet_artifacts AS artifact ON artifact.id = packet.packet_artifact_row_id WHERE packet.id = NEW.packet_row_id AND NEW.owner_identity = artifact.artifact_id AND NEW.dependency_key = artifact.artifact_id AND NEW.required = 1 AND NEW.attached = 1 AND NEW.retained = 1) BEGIN SELECT RAISE(ABORT, 'packet document dependency does not match packet artifact'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_dependency_identity_immutable BEFORE UPDATE OF packet_row_id, dependency_class, dependency_key, required, created_at ON operation_packet_retention_dependencies FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'operation packet dependency identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_dependency_delete_guard BEFORE DELETE ON operation_packet_retention_dependencies FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'operation packet dependencies are retained authority'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS operation_packet_dependency_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_dependency_identity_immutable;
DROP TRIGGER IF EXISTS operation_packet_dependency_document_guard;
DROP TRIGGER IF EXISTS operation_packet_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_lifecycle_transition_guard;
DROP TRIGGER IF EXISTS operation_packet_immutable_update;
DROP TRIGGER IF EXISTS operation_packet_insert_prior_guard;
DROP TRIGGER IF EXISTS operation_packet_insert_artifact_guard;
DROP TRIGGER IF EXISTS operation_packet_artifact_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_artifact_immutable_update;
DROP INDEX IF EXISTS idx_operation_packet_dependencies_packet_class;
DROP INDEX IF EXISTS idx_operation_packets_prior;
DROP INDEX IF EXISTS idx_operation_packets_project_lifecycle;
DROP TABLE IF EXISTS operation_packet_retention_dependencies;
DROP TABLE IF EXISTS operation_packets;
DROP TABLE IF EXISTS operation_packet_artifacts;
