-- +goose Up
CREATE TABLE source_index_generations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    generation_id TEXT NOT NULL UNIQUE CHECK (length(generation_id) = 64 AND generation_id NOT GLOB '*[^0-9a-f]*'),
    identity_version TEXT NOT NULL CHECK (identity_version = 'relay.source-index-generation-identity.v1'),
    vault_id TEXT NOT NULL CHECK (vault_id <> '' AND trim(vault_id) = vault_id),
    commit_oid TEXT NOT NULL CHECK (length(commit_oid) = 40 AND commit_oid NOT GLOB '*[^0-9a-f]*'),
    tree_oid TEXT NOT NULL CHECK (length(tree_oid) = 40 AND tree_oid NOT GLOB '*[^0-9a-f]*'),
    engine TEXT NOT NULL CHECK (engine = 'zoekt'),
    engine_revision TEXT NOT NULL CHECK (engine_revision = '2b2ce2e398e6bee68d67143f567b6c6199340c7f'),
    build_contract_version TEXT NOT NULL CHECK (build_contract_version = 'relay.source-index-build.v1'),
    build_options_sha256 TEXT NOT NULL CHECK (length(build_options_sha256) = 64 AND build_options_sha256 NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('pending', 'building', 'ready', 'failed', 'retired')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    generation_manifest_sha256 TEXT CHECK (generation_manifest_sha256 IS NULL OR (length(generation_manifest_sha256) = 64 AND generation_manifest_sha256 NOT GLOB '*[^0-9a-f]*')),
    coverage_manifest_sha256 TEXT CHECK (coverage_manifest_sha256 IS NULL OR (length(coverage_manifest_sha256) = 64 AND coverage_manifest_sha256 NOT GLOB '*[^0-9a-f]*')),
    artifact_manifest_sha256 TEXT CHECK (artifact_manifest_sha256 IS NULL OR (length(artifact_manifest_sha256) = 64 AND artifact_manifest_sha256 NOT GLOB '*[^0-9a-f]*')),
    failure_code TEXT CHECK (failure_code IS NULL OR (length(CAST(failure_code AS BLOB)) <= 128 AND trim(failure_code) <> '')),
    failure_message TEXT CHECK (failure_message IS NULL OR (length(CAST(failure_message AS BLOB)) <= 4096 AND trim(failure_message) <> '')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    building_started_at TEXT,
    ready_at TEXT,
    failed_at TEXT,
    retired_at TEXT,
    UNIQUE (identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256),
    CHECK (
        (state = 'pending' AND attempt_count >= 0 AND generation_manifest_sha256 IS NULL AND coverage_manifest_sha256 IS NULL AND artifact_manifest_sha256 IS NULL AND failure_code IS NULL AND failure_message IS NULL AND building_started_at IS NULL AND ready_at IS NULL AND failed_at IS NULL AND retired_at IS NULL)
        OR (state = 'building' AND building_started_at IS NOT NULL AND generation_manifest_sha256 IS NULL AND coverage_manifest_sha256 IS NULL AND artifact_manifest_sha256 IS NULL AND failure_code IS NULL AND failure_message IS NULL AND ready_at IS NULL AND failed_at IS NULL AND retired_at IS NULL)
        OR (state = 'ready' AND building_started_at IS NOT NULL AND ready_at IS NOT NULL AND generation_manifest_sha256 IS NOT NULL AND coverage_manifest_sha256 IS NOT NULL AND artifact_manifest_sha256 IS NOT NULL AND failure_code IS NULL AND failure_message IS NULL AND failed_at IS NULL AND retired_at IS NULL)
        OR (state = 'failed' AND building_started_at IS NOT NULL AND failed_at IS NOT NULL AND failure_code IS NOT NULL AND failure_message IS NOT NULL AND generation_manifest_sha256 IS NULL AND coverage_manifest_sha256 IS NULL AND artifact_manifest_sha256 IS NULL AND ready_at IS NULL AND retired_at IS NULL)
        OR (state = 'retired' AND retired_at IS NOT NULL AND (
            (building_started_at IS NULL AND ready_at IS NULL AND failed_at IS NULL AND generation_manifest_sha256 IS NULL AND coverage_manifest_sha256 IS NULL AND artifact_manifest_sha256 IS NULL AND failure_code IS NULL AND failure_message IS NULL)
            OR (building_started_at IS NOT NULL AND ready_at IS NOT NULL AND failed_at IS NULL AND generation_manifest_sha256 IS NOT NULL AND coverage_manifest_sha256 IS NOT NULL AND artifact_manifest_sha256 IS NOT NULL AND failure_code IS NULL AND failure_message IS NULL)
            OR (building_started_at IS NOT NULL AND ready_at IS NULL AND failed_at IS NOT NULL AND generation_manifest_sha256 IS NULL AND coverage_manifest_sha256 IS NULL AND artifact_manifest_sha256 IS NULL AND failure_code IS NOT NULL AND failure_message IS NOT NULL)
        ))
    )
);

-- +goose StatementBegin
CREATE TRIGGER source_index_generation_identity_immutable
BEFORE UPDATE OF generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, created_at ON source_index_generations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'source index generation identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER source_index_generation_transition_guard
BEFORE UPDATE ON source_index_generations
FOR EACH ROW WHEN NOT (
    (OLD.state = 'pending' AND NEW.state = 'building' AND NEW.attempt_count = OLD.attempt_count + 1)
    OR (OLD.state = 'pending' AND NEW.state = 'retired' AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'building' AND NEW.state IN ('ready', 'failed') AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'failed' AND NEW.state IN ('pending', 'retired') AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'ready' AND NEW.state = 'retired' AND NEW.attempt_count = OLD.attempt_count)
) BEGIN SELECT RAISE(ABORT, 'source index generation lifecycle transition is invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER source_index_generation_delete_guard
BEFORE DELETE ON source_index_generations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'source index generations are retained lifecycle history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS source_index_generation_delete_guard;
DROP TRIGGER IF EXISTS source_index_generation_transition_guard;
DROP TRIGGER IF EXISTS source_index_generation_identity_immutable;
DROP TABLE IF EXISTS source_index_generations;
