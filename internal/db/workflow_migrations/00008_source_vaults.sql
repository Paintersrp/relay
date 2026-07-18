-- +goose Up
CREATE TABLE source_vaults (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vault_id TEXT NOT NULL UNIQUE,
    repo_target TEXT NOT NULL COLLATE NOCASE UNIQUE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    relative_path TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (vault_id GLOB 'vault-*' AND trim(vault_id) = vault_id),
    CHECK (
        relative_path <> ''
        AND trim(relative_path) = relative_path
        AND relative_path NOT LIKE '/%'
        AND relative_path NOT LIKE '%\\%'
        AND relative_path NOT LIKE './%'
        AND relative_path NOT LIKE '%/./%'
        AND relative_path NOT LIKE '%/.'
        AND relative_path NOT LIKE '../%'
        AND relative_path NOT LIKE '%/../%'
        AND relative_path NOT LIKE '%/..'
        AND relative_path NOT LIKE '%//%'
        AND relative_path NOT LIKE '%/'
    )
);

CREATE TABLE source_vault_closures (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    closure_id TEXT NOT NULL UNIQUE,
    vault_row_id INTEGER NOT NULL REFERENCES source_vaults(id) ON DELETE RESTRICT,
    commit_oid TEXT NOT NULL CHECK (length(commit_oid) = 40 AND commit_oid NOT GLOB '*[^0-9a-f]*'),
    tree_oid TEXT NOT NULL CHECK (length(tree_oid) = 40 AND tree_oid NOT GLOB '*[^0-9a-f]*'),
    generation INTEGER NOT NULL CHECK (generation >= 1),
    ref_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('importing', 'ready', 'unavailable', 'releasing', 'released')),
    failure_reason TEXT CHECK (failure_reason IN (
        'interrupted_import',
        'source_commit_missing',
        'source_commit_type_mismatch',
        'source_tree_missing',
        'source_tree_type_mismatch',
        'source_tree_mismatch',
        'source_git_start_failed',
        'pack_generation_failed',
        'vault_missing',
        'vault_invalid',
        'vault_git_start_failed',
        'pack_index_failed',
        'vault_commit_missing',
        'vault_commit_type_mismatch',
        'vault_tree_missing',
        'vault_tree_type_mismatch',
        'vault_tree_mismatch',
        'ref_create_failed',
        'ref_missing',
        'ref_mismatch',
        'ref_delete_failed',
        'post_import_verification_failed',
        'operation_cancelled',
        'release_owner_conflict',
        'release_interrupted'
    )),
    import_started_at TEXT NOT NULL,
    verified_at TEXT,
    released_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (vault_row_id, commit_oid, tree_oid, generation),
    UNIQUE (vault_row_id, ref_name),
    CHECK (closure_id GLOB 'closure-*' AND trim(closure_id) = closure_id),
    CHECK (ref_name GLOB 'refs/relay/closures/*' AND trim(ref_name) = ref_name),
    CHECK ((state = 'unavailable' AND failure_reason IS NOT NULL) OR (state <> 'unavailable' AND failure_reason IS NULL)),
    CHECK ((state = 'ready' AND verified_at IS NOT NULL) OR (state <> 'ready' AND verified_at IS NULL)),
    CHECK ((state = 'released' AND released_at IS NOT NULL) OR (state <> 'released' AND released_at IS NULL))
);

CREATE TABLE source_vault_retentions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    retention_id TEXT NOT NULL UNIQUE,
    closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    owner_class TEXT NOT NULL CHECK (owner_class IN ('operation_packet', 'artifact', 'workflow_result', 'audit_record')),
    owner_identity TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'released')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    released_at TEXT,
    UNIQUE (closure_row_id, owner_class, owner_identity),
    CHECK (retention_id GLOB 'retention-*' AND trim(retention_id) = retention_id),
    CHECK (owner_identity <> '' AND trim(owner_identity) = owner_identity AND length(owner_identity) <= 512),
    CHECK ((state = 'active' AND released_at IS NULL) OR (state = 'released' AND released_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_source_vault_closures_current_logical
ON source_vault_closures(vault_row_id, commit_oid, tree_oid)
WHERE state <> 'released';

CREATE UNIQUE INDEX idx_source_vault_retentions_active_owner
ON source_vault_retentions(owner_class, owner_identity)
WHERE state = 'active';

CREATE INDEX idx_source_vault_closures_reconcile
ON source_vault_closures(state, vault_row_id, id);

CREATE INDEX idx_source_vault_retentions_closure_state
ON source_vault_retentions(closure_row_id, state, id);

-- +goose StatementBegin
CREATE TRIGGER source_vault_identity_immutable
BEFORE UPDATE OF vault_id, repo_target, relative_path, created_at ON source_vaults
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'source vault identity is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_vault_delete_guard
BEFORE DELETE ON source_vaults
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'source vaults are retained authority');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_vault_closure_identity_immutable
BEFORE UPDATE OF closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, created_at ON source_vault_closures
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'source vault closure identity is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_vault_closure_transition_guard
BEFORE UPDATE OF state, failure_reason, verified_at, released_at, import_started_at ON source_vault_closures
FOR EACH ROW WHEN NOT (
    (OLD.state = 'importing' AND NEW.state IN ('ready', 'unavailable') AND NEW.import_started_at = OLD.import_started_at) OR
    (OLD.state = 'unavailable' AND NEW.state = 'importing' AND NEW.import_started_at <> OLD.import_started_at) OR
    (OLD.state = 'unavailable' AND NEW.state = 'releasing' AND NEW.import_started_at = OLD.import_started_at) OR
    (OLD.state = 'ready' AND NEW.state IN ('unavailable', 'releasing') AND NEW.import_started_at = OLD.import_started_at) OR
    (OLD.state = 'releasing' AND NEW.state IN ('unavailable', 'released') AND NEW.import_started_at = OLD.import_started_at)
) BEGIN
    SELECT RAISE(ABORT, 'invalid source vault closure transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_vault_closure_delete_guard
BEFORE DELETE ON source_vault_closures
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'source vault closures are immutable history');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_vault_retention_identity_immutable
BEFORE UPDATE OF retention_id, closure_row_id, owner_class, owner_identity, created_at ON source_vault_retentions
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'source vault retention identity is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_vault_retention_transition_guard
BEFORE UPDATE OF state, released_at ON source_vault_retentions
FOR EACH ROW WHEN NOT (OLD.state = 'active' AND NEW.state = 'released') BEGIN
    SELECT RAISE(ABORT, 'invalid source vault retention transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_vault_retention_delete_guard
BEFORE DELETE ON source_vault_retentions
FOR EACH ROW BEGIN
    SELECT RAISE(ABORT, 'source vault retentions are immutable history');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS source_vault_retention_delete_guard;
DROP TRIGGER IF EXISTS source_vault_retention_transition_guard;
DROP TRIGGER IF EXISTS source_vault_retention_identity_immutable;
DROP TRIGGER IF EXISTS source_vault_closure_delete_guard;
DROP TRIGGER IF EXISTS source_vault_closure_transition_guard;
DROP TRIGGER IF EXISTS source_vault_closure_identity_immutable;
DROP TRIGGER IF EXISTS source_vault_delete_guard;
DROP TRIGGER IF EXISTS source_vault_identity_immutable;
DROP INDEX IF EXISTS idx_source_vault_retentions_closure_state;
DROP INDEX IF EXISTS idx_source_vault_closures_reconcile;
DROP INDEX IF EXISTS idx_source_vault_retentions_active_owner;
DROP INDEX IF EXISTS idx_source_vault_closures_current_logical;
DROP TABLE IF EXISTS source_vault_retentions;
DROP TABLE IF EXISTS source_vault_closures;
DROP TABLE IF EXISTS source_vaults;
