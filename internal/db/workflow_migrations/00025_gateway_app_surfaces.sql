-- +goose Up
-- Preserve route-level cutover evidence while adding the immutable public MCP
-- app-surface layer. Existing configurations remain historical seven-route
-- records; new preparations must populate these three-surface tables.
CREATE TABLE cutover_gateway_app_surfaces (
    activation_row_id INTEGER NOT NULL REFERENCES cutover_gateway_configurations(activation_row_id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 3),
    app_surface TEXT NOT NULL CHECK (app_surface IN ('wayfinder', 'planner', 'auditor')),
    public_path TEXT NOT NULL CHECK (public_path GLOB '/mcp/*' AND trim(public_path) = public_path),
    manifest_sha256 TEXT NOT NULL
        CHECK (length(manifest_sha256) = 64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
    PRIMARY KEY (activation_row_id, sequence),
    UNIQUE (activation_row_id, app_surface),
    UNIQUE (activation_row_id, public_path),
    UNIQUE (activation_row_id, app_surface, public_path)
);

CREATE TABLE cutover_gateway_route_memberships (
    activation_row_id INTEGER NOT NULL REFERENCES cutover_gateway_configurations(activation_row_id) ON DELETE RESTRICT,
    route_path TEXT NOT NULL,
    app_surface TEXT NOT NULL CHECK (app_surface IN ('wayfinder', 'planner', 'auditor')),
    PRIMARY KEY (activation_row_id, route_path),
    FOREIGN KEY (activation_row_id, route_path)
        REFERENCES cutover_gateway_routes(activation_row_id, route_path) ON DELETE RESTRICT,
    FOREIGN KEY (activation_row_id, app_surface)
        REFERENCES cutover_gateway_app_surfaces(activation_row_id, app_surface) ON DELETE RESTRICT
);

CREATE TABLE cutover_gateway_app_surface_mappings (
    activation_row_id INTEGER NOT NULL REFERENCES cutover_gateway_configurations(activation_row_id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 3),
    mapping_id TEXT NOT NULL CHECK (mapping_id IN ('wayfinder', 'planner', 'auditor')),
    app_surface TEXT NOT NULL CHECK (app_surface IN ('wayfinder', 'planner', 'auditor')),
    public_path TEXT NOT NULL CHECK (public_path GLOB '/mcp/*' AND trim(public_path) = public_path),
    listener_identity TEXT NOT NULL CHECK (listener_identity <> '' AND trim(listener_identity) = listener_identity),
    upstream_identity TEXT NOT NULL CHECK (upstream_identity <> '' AND trim(upstream_identity) = upstream_identity),
    health_evidence_sha256 TEXT NOT NULL
        CHECK (length(health_evidence_sha256) = 64 AND health_evidence_sha256 NOT GLOB '*[^0-9a-f]*'),
    trace_evidence_sha256 TEXT NOT NULL
        CHECK (length(trace_evidence_sha256) = 64 AND trace_evidence_sha256 NOT GLOB '*[^0-9a-f]*'),
    PRIMARY KEY (activation_row_id, sequence),
    UNIQUE (activation_row_id, mapping_id),
    UNIQUE (activation_row_id, app_surface),
    UNIQUE (activation_row_id, public_path),
    FOREIGN KEY (activation_row_id, app_surface, public_path)
        REFERENCES cutover_gateway_app_surfaces(activation_row_id, app_surface, public_path) ON DELETE RESTRICT
);

CREATE TRIGGER cutover_gateway_app_surfaces_immutable
BEFORE UPDATE ON cutover_gateway_app_surfaces
BEGIN SELECT RAISE(ABORT, 'cutover gateway app surfaces are immutable'); END;

CREATE TRIGGER cutover_gateway_route_memberships_immutable
BEFORE UPDATE ON cutover_gateway_route_memberships
BEGIN SELECT RAISE(ABORT, 'cutover gateway route memberships are immutable'); END;

CREATE TRIGGER cutover_gateway_app_surface_mappings_immutable
BEFORE UPDATE ON cutover_gateway_app_surface_mappings
BEGIN SELECT RAISE(ABORT, 'cutover gateway app surface mappings are immutable'); END;

CREATE TRIGGER cutover_gateway_app_surfaces_delete_guard
BEFORE DELETE ON cutover_gateway_app_surfaces
BEGIN SELECT RAISE(ABORT, 'cutover gateway app surfaces are immutable'); END;

CREATE TRIGGER cutover_gateway_route_memberships_delete_guard
BEFORE DELETE ON cutover_gateway_route_memberships
BEGIN SELECT RAISE(ABORT, 'cutover gateway route memberships are immutable'); END;

CREATE TRIGGER cutover_gateway_app_surface_mappings_delete_guard
BEFORE DELETE ON cutover_gateway_app_surface_mappings
BEGIN SELECT RAISE(ABORT, 'cutover gateway app surface mappings are immutable'); END;

-- +goose Down
DROP TRIGGER IF EXISTS cutover_gateway_app_surface_mappings_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_route_memberships_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_app_surfaces_delete_guard;
DROP TRIGGER IF EXISTS cutover_gateway_app_surface_mappings_immutable;
DROP TRIGGER IF EXISTS cutover_gateway_route_memberships_immutable;
DROP TRIGGER IF EXISTS cutover_gateway_app_surfaces_immutable;
DROP TABLE IF EXISTS cutover_gateway_app_surface_mappings;
DROP TABLE IF EXISTS cutover_gateway_route_memberships;
DROP TABLE IF EXISTS cutover_gateway_app_surfaces;
