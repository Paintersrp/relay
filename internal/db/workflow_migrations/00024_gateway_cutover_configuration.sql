-- +goose Up
-- Persist one exact seven-route gateway configuration beneath the existing
-- cutover activation lifecycle row. Configuration rows are immutable evidence;
-- activation, rollback, boundary, and roll-forward state remain in
-- cutover_activations.
CREATE TABLE cutover_gateway_configurations (
    activation_row_id INTEGER PRIMARY KEY REFERENCES cutover_activations(id) ON DELETE RESTRICT,
    configuration_sha256 TEXT NOT NULL UNIQUE
        CHECK (length(configuration_sha256) = 64 AND configuration_sha256 NOT GLOB '*[^0-9a-f]*'),
    relay_repository TEXT NOT NULL CHECK (relay_repository <> '' AND trim(relay_repository) = relay_repository),
    relay_commit_oid TEXT NOT NULL
        CHECK (length(relay_commit_oid) = 40 AND relay_commit_oid NOT GLOB '*[^0-9a-f]*'),
    standing_repository TEXT NOT NULL CHECK (standing_repository <> '' AND trim(standing_repository) = standing_repository),
    standing_commit_oid TEXT NOT NULL
        CHECK (length(standing_commit_oid) = 40 AND standing_commit_oid NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE cutover_gateway_routes (
    activation_row_id INTEGER NOT NULL REFERENCES cutover_gateway_configurations(activation_row_id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 7),
    route_path TEXT NOT NULL CHECK (route_path GLOB '/mcp/v1/*' AND trim(route_path) = route_path),
    role TEXT NOT NULL CHECK (role IN ('wayfinder', 'planner', 'auditor')),
    surface_contract_id TEXT NOT NULL CHECK (surface_contract_id <> '' AND trim(surface_contract_id) = surface_contract_id),
    manifest_sha256 TEXT NOT NULL
        CHECK (length(manifest_sha256) = 64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
    authority_commit_oid TEXT NOT NULL
        CHECK (length(authority_commit_oid) = 40 AND authority_commit_oid NOT GLOB '*[^0-9a-f]*'),
    authority_blob_oid TEXT NOT NULL
        CHECK (length(authority_blob_oid) = 40 AND authority_blob_oid NOT GLOB '*[^0-9a-f]*'),
    PRIMARY KEY (activation_row_id, sequence),
    UNIQUE (activation_row_id, route_path)
);

CREATE TABLE cutover_gateway_mappings (
    activation_row_id INTEGER NOT NULL REFERENCES cutover_gateway_configurations(activation_row_id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 7),
    mapping_id TEXT NOT NULL CHECK (mapping_id <> '' AND trim(mapping_id) = mapping_id),
    route_path TEXT NOT NULL,
    listener_identity TEXT NOT NULL CHECK (listener_identity <> '' AND trim(listener_identity) = listener_identity),
    upstream_identity TEXT NOT NULL CHECK (upstream_identity <> '' AND trim(upstream_identity) = upstream_identity),
    health_evidence_sha256 TEXT NOT NULL
        CHECK (length(health_evidence_sha256) = 64 AND health_evidence_sha256 NOT GLOB '*[^0-9a-f]*'),
    trace_evidence_sha256 TEXT NOT NULL
        CHECK (length(trace_evidence_sha256) = 64 AND trace_evidence_sha256 NOT GLOB '*[^0-9a-f]*'),
    PRIMARY KEY (activation_row_id, sequence),
    UNIQUE (activation_row_id, mapping_id),
    UNIQUE (activation_row_id, route_path),
    FOREIGN KEY (activation_row_id, route_path)
        REFERENCES cutover_gateway_routes(activation_row_id, route_path) ON DELETE RESTRICT
);

CREATE TABLE cutover_gateway_standing_authorities (
    activation_row_id INTEGER NOT NULL REFERENCES cutover_gateway_configurations(activation_row_id) ON DELETE RESTRICT,
    role TEXT NOT NULL CHECK (role IN ('wayfinder', 'planner', 'auditor')),
    repository TEXT NOT NULL CHECK (repository <> '' AND trim(repository) = repository),
    commit_oid TEXT NOT NULL
        CHECK (length(commit_oid) = 40 AND commit_oid NOT GLOB '*[^0-9a-f]*'),
    path TEXT NOT NULL CHECK (path <> '' AND trim(path) = path),
    blob_oid TEXT NOT NULL
        CHECK (length(blob_oid) = 40 AND blob_oid NOT GLOB '*[^0-9a-f]*'),
    content_sha256 TEXT NOT NULL
        CHECK (length(content_sha256) = 64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
    PRIMARY KEY (activation_row_id, role)
);

CREATE TABLE cutover_gateway_dependency_outcomes (
    activation_row_id INTEGER NOT NULL REFERENCES cutover_gateway_configurations(activation_row_id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 3),
    ticket_id TEXT NOT NULL CHECK (ticket_id <> '' AND trim(ticket_id) = ticket_id),
    ticket_revision INTEGER NOT NULL CHECK (ticket_revision >= 1),
    outcome TEXT NOT NULL CHECK (outcome = 'completed_accepted'),
    evidence_sha256 TEXT NOT NULL
        CHECK (length(evidence_sha256) = 64 AND evidence_sha256 NOT GLOB '*[^0-9a-f]*'),
    PRIMARY KEY (activation_row_id, sequence),
    UNIQUE (activation_row_id, ticket_id, ticket_revision)
);

CREATE TRIGGER cutover_gateway_configuration_immutable
BEFORE UPDATE ON cutover_gateway_configurations
BEGIN SELECT RAISE(ABORT, 'cutover gateway configuration is immutable'); END;

CREATE TRIGGER cutover_gateway_routes_immutable
BEFORE UPDATE ON cutover_gateway_routes
BEGIN SELECT RAISE(ABORT, 'cutover gateway routes are immutable'); END;

CREATE TRIGGER cutover_gateway_mappings_immutable
BEFORE UPDATE ON cutover_gateway_mappings
BEGIN SELECT RAISE(ABORT, 'cutover gateway mappings are immutable'); END;

CREATE TRIGGER cutover_gateway_standing_authorities_immutable
BEFORE UPDATE ON cutover_gateway_standing_authorities
BEGIN SELECT RAISE(ABORT, 'cutover gateway standing authorities are immutable'); END;

CREATE TRIGGER cutover_gateway_dependency_outcomes_immutable
BEFORE UPDATE ON cutover_gateway_dependency_outcomes
BEGIN SELECT RAISE(ABORT, 'cutover gateway dependency outcomes are immutable'); END;

CREATE TRIGGER cutover_gateway_configuration_delete_guard
BEFORE DELETE ON cutover_gateway_configurations
BEGIN SELECT RAISE(ABORT, 'cutover gateway configuration is immutable'); END;

CREATE TRIGGER cutover_gateway_routes_delete_guard
BEFORE DELETE ON cutover_gateway_routes
BEGIN SELECT RAISE(ABORT, 'cutover gateway routes are immutable'); END;

CREATE TRIGGER cutover_gateway_mappings_delete_guard
BEFORE DELETE ON cutover_gateway_mappings
BEGIN SELECT RAISE(ABORT, 'cutover gateway mappings are immutable'); END;

CREATE TRIGGER cutover_gateway_standing_authorities_delete_guard
BEFORE DELETE ON cutover_gateway_standing_authorities
BEGIN SELECT RAISE(ABORT, 'cutover gateway standing authorities are immutable'); END;

CREATE TRIGGER cutover_gateway_dependency_outcomes_delete_guard
BEFORE DELETE ON cutover_gateway_dependency_outcomes
BEGIN SELECT RAISE(ABORT, 'cutover gateway dependency outcomes are immutable'); END;

-- +goose Down
DROP TRIGGER IF EXISTS cutover_gateway_dependency_outcomes_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_standing_authorities_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_mappings_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_routes_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_configuration_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_dependency_outcomes_immutable;
DROP TRIGGER IF EXISTS cutover_gateway_standing_authorities_immutable;
DROP TRIGGER IF EXISTS cutover_gateway_mappings_immutable;
DROP TRIGGER IF EXISTS cutover_gateway_routes_immutable;
DROP TRIGGER IF EXISTS cutover_gateway_configuration_immutable;
DROP TABLE IF EXISTS cutover_gateway_dependency_outcomes;
DROP TABLE IF EXISTS cutover_gateway_standing_authorities;
DROP TABLE IF EXISTS cutover_gateway_mappings;
DROP TABLE IF EXISTS cutover_gateway_routes;
DROP TABLE IF EXISTS cutover_gateway_configurations;

