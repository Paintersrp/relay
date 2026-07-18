-- +goose Up
CREATE TABLE operation_packet_publications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    publication_id TEXT NOT NULL UNIQUE,
    packet_row_id INTEGER NOT NULL UNIQUE REFERENCES operation_packets(id) ON DELETE RESTRICT,
    packet_artifact_row_id INTEGER NOT NULL UNIQUE REFERENCES operation_packet_artifacts(id) ON DELETE RESTRICT,
    mutation_result_row_id INTEGER NOT NULL UNIQUE REFERENCES mcp_mutation_results(id) ON DELETE RESTRICT,
    namespace TEXT NOT NULL UNIQUE,
    manifest_sha256 TEXT NOT NULL,
    expected_retained_artifact_count INTEGER NOT NULL CHECK (expected_retained_artifact_count >= 0),
    expected_binding_count INTEGER NOT NULL CHECK (expected_binding_count >= 1),
    expected_dependency_count INTEGER NOT NULL CHECK (expected_dependency_count >= 1),
    expected_vault_relationship_count INTEGER NOT NULL CHECK (expected_vault_relationship_count >= 0),
    state TEXT NOT NULL DEFAULT 'committed' CHECK (state = 'committed'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (publication_id GLOB 'publication-*' AND trim(publication_id) = publication_id),
    CHECK (namespace = 'operation-packet-publications/' || publication_id),
    CHECK (length(manifest_sha256) = 64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*')
);

ALTER TABLE operation_packets
ADD COLUMN coordinated_publication_id TEXT
REFERENCES operation_packet_publications(publication_id) ON DELETE RESTRICT
DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE operation_packet_retained_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    publication_id TEXT NOT NULL REFERENCES operation_packet_publications(publication_id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    artifact_id TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL CHECK (kind IN ('direct_uploaded_input', 'inline_input', 'workflow_snapshot')),
    relative_path TEXT NOT NULL UNIQUE,
    media_type TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (artifact_id GLOB 'artifact-*' AND trim(artifact_id) = artifact_id),
    CHECK (relative_path GLOB 'operation-packet-publications/' || publication_id || '/*'),
    CHECK (relative_path NOT LIKE '%\\%' AND relative_path NOT LIKE '%/../%' AND relative_path NOT LIKE '%/./%' AND relative_path NOT LIKE '%//%'),
    CHECK (media_type <> '' AND trim(media_type) = media_type AND length(media_type) <= 255),
    CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    UNIQUE (publication_id, artifact_id)
);

CREATE TABLE operation_packet_artifact_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    publication_id TEXT NOT NULL REFERENCES operation_packet_publications(publication_id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    packet_row_id INTEGER NOT NULL REFERENCES operation_packets(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    dependency_class TEXT NOT NULL CHECK (dependency_class IN ('packet_document', 'input_artifact', 'workflow_snapshot', 'run_artifact')),
    dependency_key TEXT NOT NULL,
    packet_artifact_row_id INTEGER REFERENCES operation_packet_artifacts(id) ON DELETE RESTRICT,
    retained_artifact_row_id INTEGER REFERENCES operation_packet_retained_artifacts(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (dependency_key <> '' AND trim(dependency_key) = dependency_key AND length(dependency_key) <= 512),
    CHECK ((packet_artifact_row_id IS NOT NULL AND retained_artifact_row_id IS NULL) OR (packet_artifact_row_id IS NULL AND retained_artifact_row_id IS NOT NULL)),
    CHECK ((dependency_class = 'packet_document' AND packet_artifact_row_id IS NOT NULL) OR (dependency_class <> 'packet_document' AND retained_artifact_row_id IS NOT NULL)),
    UNIQUE (publication_id, sequence),
    UNIQUE (publication_id, dependency_class, dependency_key)
);

CREATE TABLE operation_packet_vault_relationships (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    publication_id TEXT NOT NULL REFERENCES operation_packet_publications(publication_id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    packet_row_id INTEGER NOT NULL REFERENCES operation_packets(id) ON DELETE RESTRICT,
    dependency_class TEXT NOT NULL CHECK (dependency_class IN ('repository_vault', 'git_path_object', 'manifest_member')),
    dependency_key TEXT NOT NULL,
    owner_identity TEXT NOT NULL,
    retention_row_id INTEGER NOT NULL UNIQUE REFERENCES source_vault_retentions(id) ON DELETE RESTRICT,
    closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    vault_row_id INTEGER NOT NULL REFERENCES source_vaults(id) ON DELETE RESTRICT,
    commit_oid TEXT NOT NULL,
    tree_oid TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (dependency_key <> '' AND trim(dependency_key) = dependency_key AND length(dependency_key) <= 512),
    CHECK (owner_identity GLOB 'opkt-edge:*' AND length(owner_identity) = 74 AND substr(owner_identity, 11) NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(commit_oid) = 40 AND commit_oid NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(tree_oid) = 40 AND tree_oid NOT GLOB '*[^0-9a-f]*'),
    UNIQUE (publication_id, dependency_class, dependency_key),
    UNIQUE (packet_row_id, dependency_class, dependency_key),
    UNIQUE (owner_identity)
);

CREATE INDEX idx_operation_packet_publications_packet ON operation_packet_publications(packet_row_id);
CREATE INDEX idx_operation_packet_retained_artifacts_publication ON operation_packet_retained_artifacts(publication_id, id);
CREATE INDEX idx_operation_packet_artifact_bindings_publication ON operation_packet_artifact_bindings(publication_id, sequence, id);
CREATE INDEX idx_operation_packet_vault_relationships_publication ON operation_packet_vault_relationships(publication_id, dependency_class, dependency_key, id);

-- +goose StatementBegin
CREATE TRIGGER operation_packet_coordinated_publication_immutable
BEFORE UPDATE OF coordinated_publication_id ON operation_packets
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'operation packet coordinated publication identity is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_retained_artifact_insert_guard
BEFORE INSERT ON operation_packet_retained_artifacts
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = NEW.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication is already committed');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_retained_artifact_mutation_guard
BEFORE UPDATE ON operation_packet_retained_artifacts
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = OLD.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet retained artifacts are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_retained_artifact_delete_guard
BEFORE DELETE ON operation_packet_retained_artifacts
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = OLD.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet retained artifacts are retained authority');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_artifact_binding_insert_guard
BEFORE INSERT ON operation_packet_artifact_bindings
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = NEW.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication is already committed');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_artifact_binding_mutation_guard
BEFORE UPDATE ON operation_packet_artifact_bindings
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = OLD.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet artifact bindings are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_artifact_binding_delete_guard
BEFORE DELETE ON operation_packet_artifact_bindings
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = OLD.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet artifact bindings are retained authority');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_vault_relationship_insert_guard
BEFORE INSERT ON operation_packet_vault_relationships
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = NEW.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication is already committed');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_vault_relationship_mutation_guard
BEFORE UPDATE ON operation_packet_vault_relationships
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = OLD.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet vault relationships are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_vault_relationship_delete_guard
BEFORE DELETE ON operation_packet_vault_relationships
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM operation_packet_publications WHERE publication_id = OLD.publication_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet vault relationships are retained authority');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_closure_guard
BEFORE INSERT ON operation_packet_publications
FOR EACH ROW WHEN
    NOT EXISTS (
        SELECT 1
        FROM operation_packets AS packet
        JOIN operation_packet_artifacts AS artifact ON artifact.id = packet.packet_artifact_row_id
        WHERE packet.id = NEW.packet_row_id
          AND packet.coordinated_publication_id = NEW.publication_id
          AND packet.packet_artifact_row_id = NEW.packet_artifact_row_id
          AND packet.packet_sha256 = artifact.sha256
    )
    OR NOT EXISTS (SELECT 1 FROM mcp_mutation_results WHERE id = NEW.mutation_result_row_id)
    OR NEW.expected_retained_artifact_count <> (SELECT COUNT(*) FROM operation_packet_retained_artifacts WHERE publication_id = NEW.publication_id)
    OR NEW.expected_binding_count <> (SELECT COUNT(*) FROM operation_packet_artifact_bindings WHERE publication_id = NEW.publication_id AND packet_row_id = NEW.packet_row_id)
    OR NEW.expected_dependency_count <> (SELECT COUNT(*) FROM operation_packet_retention_dependencies WHERE packet_row_id = NEW.packet_row_id)
    OR NEW.expected_vault_relationship_count <> (SELECT COUNT(*) FROM operation_packet_vault_relationships WHERE publication_id = NEW.publication_id AND packet_row_id = NEW.packet_row_id)
    OR 1 <> (
        SELECT COUNT(*)
        FROM operation_packet_artifact_bindings
        WHERE publication_id = NEW.publication_id
          AND packet_row_id = NEW.packet_row_id
          AND dependency_class = 'packet_document'
          AND packet_artifact_row_id = NEW.packet_artifact_row_id
          AND retained_artifact_row_id IS NULL
    )
    OR EXISTS (
        SELECT 1
        FROM operation_packet_retained_artifacts AS retained
        WHERE retained.publication_id = NEW.publication_id
          AND NOT EXISTS (
              SELECT 1 FROM operation_packet_artifact_bindings AS binding
              WHERE binding.publication_id = NEW.publication_id
                AND binding.retained_artifact_row_id = retained.id
          )
    )
    OR EXISTS (
        SELECT 1
        FROM operation_packet_artifact_bindings AS binding
        LEFT JOIN operation_packet_retained_artifacts AS retained ON retained.id = binding.retained_artifact_row_id
        LEFT JOIN operation_packet_retention_dependencies AS dependency
          ON dependency.packet_row_id = NEW.packet_row_id
         AND dependency.dependency_class = binding.dependency_class
         AND dependency.dependency_key = binding.dependency_key
        LEFT JOIN operation_packet_artifacts AS packet_artifact ON packet_artifact.id = binding.packet_artifact_row_id
        WHERE binding.publication_id = NEW.publication_id
          AND (
              binding.packet_row_id <> NEW.packet_row_id
              OR dependency.id IS NULL
              OR dependency.required <> 1
              OR dependency.attached <> 1
              OR dependency.retained <> 1
              OR dependency.owner_identity IS NULL
              OR dependency.owner_identity <> COALESCE(packet_artifact.artifact_id, retained.artifact_id)
              OR (binding.retained_artifact_row_id IS NOT NULL AND retained.publication_id <> NEW.publication_id)
          )
    )
    OR EXISTS (
        SELECT 1
        FROM operation_packet_vault_relationships AS relationship
        LEFT JOIN operation_packet_retention_dependencies AS dependency
          ON dependency.packet_row_id = NEW.packet_row_id
         AND dependency.dependency_class = relationship.dependency_class
         AND dependency.dependency_key = relationship.dependency_key
        LEFT JOIN source_vault_retentions AS retention ON retention.id = relationship.retention_row_id
        LEFT JOIN source_vault_closures AS closure ON closure.id = relationship.closure_row_id
        LEFT JOIN source_vaults AS vault ON vault.id = relationship.vault_row_id
        WHERE relationship.publication_id = NEW.publication_id
          AND (
              relationship.packet_row_id <> NEW.packet_row_id
              OR dependency.id IS NULL
              OR dependency.required <> 1
              OR dependency.attached <> 1
              OR dependency.retained <> 1
              OR dependency.owner_identity <> relationship.owner_identity
              OR retention.id IS NULL
              OR retention.owner_class <> 'operation_packet'
              OR retention.owner_identity <> relationship.owner_identity
              OR retention.state <> 'active'
              OR retention.closure_row_id <> relationship.closure_row_id
              OR closure.id IS NULL
              OR closure.state <> 'ready'
              OR closure.vault_row_id <> relationship.vault_row_id
              OR closure.commit_oid <> relationship.commit_oid
              OR closure.tree_oid <> relationship.tree_oid
              OR vault.id IS NULL
          )
    )
    OR EXISTS (
        SELECT 1
        FROM operation_packet_retention_dependencies AS dependency
        WHERE dependency.packet_row_id = NEW.packet_row_id
          AND NOT EXISTS (
              SELECT 1 FROM operation_packet_artifact_bindings AS binding
              WHERE binding.publication_id = NEW.publication_id
                AND binding.dependency_class = dependency.dependency_class
                AND binding.dependency_key = dependency.dependency_key
          )
          AND NOT EXISTS (
              SELECT 1 FROM operation_packet_vault_relationships AS relationship
              WHERE relationship.publication_id = NEW.publication_id
                AND relationship.dependency_class = dependency.dependency_class
                AND relationship.dependency_key = dependency.dependency_key
          )
    )
    OR EXISTS (
        SELECT 1
        FROM operation_packet_artifact_bindings AS binding
        JOIN operation_packet_vault_relationships AS relationship
          ON relationship.publication_id = binding.publication_id
         AND relationship.dependency_class = binding.dependency_class
         AND relationship.dependency_key = binding.dependency_key
        WHERE binding.publication_id = NEW.publication_id
    )
BEGIN
    SELECT RAISE(ABORT, 'operation packet publication closure is incomplete');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_immutable_update
BEFORE UPDATE ON operation_packet_publications
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'operation packet publications are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_delete_guard
BEFORE DELETE ON operation_packet_publications
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'operation packet publications are retained authority');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS operation_packet_publication_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_immutable_update;
DROP TRIGGER IF EXISTS operation_packet_publication_closure_guard;
DROP TRIGGER IF EXISTS operation_packet_vault_relationship_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_vault_relationship_mutation_guard;
DROP TRIGGER IF EXISTS operation_packet_vault_relationship_insert_guard;
DROP TRIGGER IF EXISTS operation_packet_artifact_binding_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_artifact_binding_mutation_guard;
DROP TRIGGER IF EXISTS operation_packet_artifact_binding_insert_guard;
DROP TRIGGER IF EXISTS operation_packet_retained_artifact_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_retained_artifact_mutation_guard;
DROP TRIGGER IF EXISTS operation_packet_retained_artifact_insert_guard;
DROP TRIGGER IF EXISTS operation_packet_coordinated_publication_immutable;
DROP INDEX IF EXISTS idx_operation_packet_vault_relationships_publication;
DROP INDEX IF EXISTS idx_operation_packet_artifact_bindings_publication;
DROP INDEX IF EXISTS idx_operation_packet_retained_artifacts_publication;
DROP INDEX IF EXISTS idx_operation_packet_publications_packet;
DROP TABLE IF EXISTS operation_packet_vault_relationships;
DROP TABLE IF EXISTS operation_packet_artifact_bindings;
DROP TABLE IF EXISTS operation_packet_retained_artifacts;
ALTER TABLE operation_packets DROP COLUMN coordinated_publication_id;
DROP TABLE IF EXISTS operation_packet_publications;

