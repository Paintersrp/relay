-- +goose Up
-- +goose NO TRANSACTION
-- The Delivery Plan is optional planning-stage authority between Shared Design
-- and normal Delivery Ticket authoring. It owns planned unit boundaries and
-- the planned semantic dependency topology only, is never a selected-package
-- member, and does not participate in execution or audit authority ordering.
-- The durable surface records the current approved Plan's exact bytes
-- (canonical JSON discovery artifact), its planned units and planned semantic
-- dependencies in Plan source order, and the plan-unit-to-authored-Ticket
-- association recorded when a planned unit is realized by a Delivery Ticket.
PRAGMA foreign_keys=off;

-- Extend the planning candidate family vocabulary with delivery_plan. All
-- triggers referencing planning_candidates are dropped before the rename so
-- their bodies are not rewritten to the legacy table, then recreated after the
-- rebuild; row IDs are preserved so approvals and production links remain
-- exact.
DROP TRIGGER IF EXISTS planning_candidate_basis_guard;
DROP TRIGGER IF EXISTS planning_candidate_update_immutable;
DROP TRIGGER IF EXISTS planning_candidate_delete_guard;
DROP TRIGGER IF EXISTS planning_candidate_approval_binding_guard;
DROP TRIGGER IF EXISTS delivery_ticket_production_link_binding_guard;
DROP INDEX IF EXISTS idx_planning_candidates_workspace;
-- legacy_alter_table keeps the RENAME from rewriting the remaining approvals
-- and production-link triggers and foreign keys to the legacy table while the
-- rebuilt planning_candidates table is transiently absent.
PRAGMA legacy_alter_table=ON;
ALTER TABLE planning_candidates RENAME TO planning_candidates_legacy;
PRAGMA legacy_alter_table=OFF;
CREATE TABLE planning_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    candidate_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    family TEXT NOT NULL CHECK (family IN ('requirements', 'shared_design', 'delivery_plan', 'delivery_ticket')),
    filename TEXT NOT NULL,
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    artifact_size_bytes INTEGER NOT NULL CHECK (artifact_size_bytes >= 0),
    discovery_closure_packet_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_closure_packets(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    repo_target TEXT NOT NULL COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL CHECK (length(base_commit) = 40 AND base_commit NOT GLOB '*[^0-9a-f]*'),
    destination TEXT NOT NULL CHECK (destination IN ('direct_delivery_ticket', 'requirements', 'shared_design', 'requirements_then_shared_design', 'existing_route_continuation')),
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (candidate_id GLOB 'candidate-*' AND trim(candidate_id) = candidate_id),
    CHECK (filename <> '' AND trim(filename) = filename AND filename NOT LIKE '%/%' AND filename NOT LIKE '%\\%'),
    CHECK (branch <> '' AND trim(branch) = branch),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);
INSERT INTO planning_candidates (id, candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, authority_revision_row_id, repo_target, branch, base_commit, destination, created_identity, created_at)
SELECT id, candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, authority_revision_row_id, repo_target, branch, base_commit, destination, created_identity, created_at
FROM planning_candidates_legacy;
DROP TABLE planning_candidates_legacy;
CREATE INDEX idx_planning_candidates_workspace ON planning_candidates(workspace_row_id, created_at, id);
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_basis_guard
BEFORE INSERT ON planning_candidates
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM feature_workspaces AS workspace
    JOIN feature_workspace_discovery_closure_packets AS packet
      ON packet.id = NEW.discovery_closure_packet_row_id
     AND packet.workspace_row_id = workspace.id
     AND workspace.current_discovery_closure_packet_row_id = packet.id
     AND packet.closing_revision_row_id = workspace.current_discovery_revision_row_id
    WHERE workspace.id = NEW.workspace_row_id
)
OR (NEW.authority_revision_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspaces AS workspace
    WHERE workspace.id = NEW.workspace_row_id
      AND workspace.current_authority_revision_row_id = NEW.authority_revision_row_id
))
OR NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_artifacts AS artifact
    WHERE artifact.id = NEW.artifact_row_id
      AND artifact.workspace_row_id = NEW.workspace_row_id
      AND artifact.sha256 = NEW.artifact_sha256
      AND artifact.size_bytes = NEW.artifact_size_bytes
)
BEGIN SELECT RAISE(ABORT, 'planning candidate basis or exact artifact mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_update_immutable
BEFORE UPDATE ON planning_candidates
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'planning candidates are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_delete_guard
BEFORE DELETE ON planning_candidates
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'planning candidates are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_approval_binding_guard
BEFORE INSERT ON planning_candidate_approvals
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM planning_candidates AS candidate
    JOIN feature_workspace_discovery_artifacts AS artifact ON artifact.id = NEW.candidate_artifact_row_id
    WHERE candidate.id = NEW.candidate_row_id
      AND candidate.artifact_row_id = NEW.candidate_artifact_row_id
      AND candidate.artifact_sha256 = NEW.candidate_sha256
      AND candidate.artifact_size_bytes = NEW.candidate_size_bytes
      AND artifact.workspace_row_id = candidate.workspace_row_id
      AND artifact.sha256 = NEW.candidate_sha256
      AND artifact.size_bytes = NEW.candidate_size_bytes
)
BEGIN SELECT RAISE(ABORT, 'candidate approval must bind the exact candidate artifact'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_production_link_binding_guard
BEFORE INSERT ON delivery_ticket_production_links
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_tickets AS ticket
    JOIN planning_candidates AS candidate ON candidate.id = NEW.candidate_row_id
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.produced_revision_row_id
    JOIN feature_workspace_discovery_artifacts AS candidate_artifact ON candidate_artifact.id = NEW.candidate_artifact_row_id
    JOIN feature_workspace_discovery_artifacts AS canonical_artifact ON canonical_artifact.id = NEW.canonical_json_artifact_row_id
    JOIN feature_workspace_discovery_artifacts AS markdown_artifact ON markdown_artifact.id = NEW.rendered_markdown_artifact_row_id
    WHERE ticket.id = NEW.delivery_ticket_row_id
      AND ticket.workspace_row_id = candidate.workspace_row_id
      AND revision.delivery_ticket_row_id = ticket.id
      AND candidate_artifact.workspace_row_id = ticket.workspace_row_id
      AND candidate_artifact.sha256 = NEW.candidate_sha256
      AND candidate_artifact.size_bytes = NEW.candidate_size_bytes
      AND candidate.artifact_row_id = NEW.candidate_artifact_row_id
      AND candidate.artifact_sha256 = NEW.candidate_sha256
      AND candidate.artifact_size_bytes = NEW.candidate_size_bytes
      AND canonical_artifact.workspace_row_id = ticket.workspace_row_id
      AND canonical_artifact.sha256 = NEW.canonical_json_sha256
      AND canonical_artifact.size_bytes = NEW.canonical_json_size_bytes
      AND canonical_artifact.media_type = 'application/json'
      AND markdown_artifact.workspace_row_id = ticket.workspace_row_id
      AND markdown_artifact.sha256 = NEW.rendered_markdown_sha256
      AND markdown_artifact.size_bytes = NEW.rendered_markdown_size_bytes
      AND markdown_artifact.media_type = 'text/markdown'
)
BEGIN SELECT RAISE(ABORT, 'production link ownership or exact artifact mismatch'); END;
-- +goose StatementEnd

-- A Delivery Plan is the complete exact-byte approved planning candidate
-- promoted to currentness. The plan row binds the immutable candidate, its
-- exact canonical artifact, and the workspace. Replacement is a new complete
-- Plan row promoted as current; prior Plan rows remain retained history and
-- never re-authorize planned Ticket authoring.
CREATE TABLE delivery_plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    candidate_row_id INTEGER NOT NULL REFERENCES planning_candidates(id) ON DELETE RESTRICT,
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    artifact_size_bytes INTEGER NOT NULL CHECK (artifact_size_bytes >= 0),
    feature_slug TEXT NOT NULL,
    goal TEXT NOT NULL,
    context TEXT NOT NULL,
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (plan_id GLOB 'delivery-plan-*' AND trim(plan_id) = plan_id),
    CHECK (feature_slug <> '' AND trim(feature_slug) = feature_slug),
    CHECK (goal <> '' AND trim(goal) = goal),
    CHECK (context <> '' AND trim(context) = context),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);

-- Planned units keep Plan source order; unit identity is unique within one
-- Plan exactly as the compiler enforces keyed planned-unit identity uniqueness.
CREATE TABLE delivery_plan_units (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_row_id INTEGER NOT NULL REFERENCES delivery_plans(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    unit_id TEXT NOT NULL,
    goal TEXT NOT NULL,
    UNIQUE (plan_row_id, sequence),
    UNIQUE (plan_row_id, unit_id),
    CHECK (unit_id <> '' AND trim(unit_id) = unit_id),
    CHECK (goal <> '' AND trim(goal) = goal)
);

-- Planned semantic dependencies keep the unit's depends_on source order and
-- always reference another planned unit of the same Plan.
CREATE TABLE delivery_plan_unit_dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    unit_row_id INTEGER NOT NULL REFERENCES delivery_plan_units(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    depends_on_unit_row_id INTEGER NOT NULL REFERENCES delivery_plan_units(id) ON DELETE RESTRICT,
    UNIQUE (unit_row_id, sequence),
    CHECK (unit_row_id <> depends_on_unit_row_id)
);

-- The plan-unit-to-authored-Ticket association records that a realized
-- Delivery Ticket binds one current planned unit of one Plan. Each planned
-- unit realizes exactly one Ticket and each Ticket realizes at most one
-- planned unit; the association is retained history after Plan replacement.
CREATE TABLE delivery_ticket_plan_unit_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    link_id TEXT NOT NULL UNIQUE,
    plan_row_id INTEGER NOT NULL REFERENCES delivery_plans(id) ON DELETE RESTRICT,
    unit_row_id INTEGER NOT NULL REFERENCES delivery_plan_units(id) ON DELETE RESTRICT,
    delivery_ticket_row_id INTEGER NOT NULL REFERENCES delivery_tickets(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (unit_row_id),
    UNIQUE (delivery_ticket_row_id),
    CHECK (link_id GLOB 'plan-unit-link-*' AND trim(link_id) = link_id)
);

ALTER TABLE feature_workspaces ADD COLUMN current_delivery_plan_row_id INTEGER REFERENCES delivery_plans(id) ON DELETE RESTRICT;

CREATE INDEX idx_delivery_plans_workspace ON delivery_plans(workspace_row_id, created_at, id);
CREATE INDEX idx_delivery_plan_units_plan ON delivery_plan_units(plan_row_id, sequence, id);
CREATE INDEX idx_delivery_plan_unit_dependencies_unit ON delivery_plan_unit_dependencies(unit_row_id, sequence, id);
CREATE INDEX idx_delivery_ticket_plan_unit_links_plan ON delivery_ticket_plan_unit_links(plan_row_id, id);
CREATE INDEX idx_delivery_ticket_plan_unit_links_ticket ON delivery_ticket_plan_unit_links(delivery_ticket_row_id, id);

-- +goose StatementBegin
CREATE TRIGGER delivery_plan_binding_guard
BEFORE INSERT ON delivery_plans
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM planning_candidates AS candidate
    JOIN feature_workspaces AS workspace ON workspace.id = candidate.workspace_row_id
    JOIN feature_workspace_discovery_artifacts AS artifact ON artifact.id = NEW.artifact_row_id
    WHERE candidate.id = NEW.candidate_row_id
      AND candidate.family = 'delivery_plan'
      AND candidate.workspace_row_id = NEW.workspace_row_id
      AND candidate.artifact_row_id = NEW.artifact_row_id
      AND candidate.artifact_sha256 = NEW.artifact_sha256
      AND candidate.artifact_size_bytes = NEW.artifact_size_bytes
      AND artifact.workspace_row_id = NEW.workspace_row_id
      AND artifact.sha256 = NEW.artifact_sha256
      AND artifact.size_bytes = NEW.artifact_size_bytes
)
BEGIN SELECT RAISE(ABORT, 'delivery plan must bind the exact approved delivery_plan candidate artifact'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_plan_update_immutable
BEFORE UPDATE ON delivery_plans
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery plans are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_plan_delete_guard
BEFORE DELETE ON delivery_plans
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery plans are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_plan_unit_update_immutable
BEFORE UPDATE ON delivery_plan_units
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery plan units are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_plan_unit_delete_guard
BEFORE DELETE ON delivery_plan_units
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery plan units are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_plan_unit_dependency_same_plan
BEFORE INSERT ON delivery_plan_unit_dependencies
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_plan_units AS unit
    JOIN delivery_plan_units AS dependency ON dependency.id = NEW.depends_on_unit_row_id
    WHERE unit.id = NEW.unit_row_id AND unit.plan_row_id = dependency.plan_row_id
)
BEGIN SELECT RAISE(ABORT, 'planned dependency must reference a planned unit of the same Plan'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_plan_unit_dependency_update_immutable
BEFORE UPDATE ON delivery_plan_unit_dependencies
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'planned dependencies are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_plan_unit_dependency_delete_guard
BEFORE DELETE ON delivery_plan_unit_dependencies
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'planned dependencies are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_plan_unit_link_binding_guard
BEFORE INSERT ON delivery_ticket_plan_unit_links
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_plans AS plan
    JOIN delivery_plan_units AS unit ON unit.id = NEW.unit_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = NEW.delivery_ticket_row_id
    WHERE plan.id = NEW.plan_row_id
      AND unit.plan_row_id = plan.id
      AND ticket.workspace_row_id = plan.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'plan unit link must bind a unit of the Plan and a Ticket of the workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_plan_unit_link_update_immutable
BEFORE UPDATE ON delivery_ticket_plan_unit_links
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket plan unit links are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_plan_unit_link_delete_guard
BEFORE DELETE ON delivery_ticket_plan_unit_links
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket plan unit links are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_current_delivery_plan_guard
BEFORE UPDATE OF current_delivery_plan_row_id ON feature_workspaces
FOR EACH ROW WHEN NEW.current_delivery_plan_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM delivery_plans
    WHERE id = NEW.current_delivery_plan_row_id AND workspace_row_id = NEW.id
)
BEGIN SELECT RAISE(ABORT, 'current delivery plan does not belong to workspace'); END;
-- +goose StatementEnd

PRAGMA foreign_keys=on;

-- +goose Down
-- Reverse the Delivery Plan surface: remove its triggers, tables, and the
-- workspace current-plan pointer, and restore the planning candidate family
-- vocabulary without delivery_plan. The rename below uses legacy_alter_table
-- so the approvals and production-link foreign keys and triggers keep naming
-- planning_candidates while the rebuilt table is transiently absent.
PRAGMA foreign_keys=off;

DROP TRIGGER IF EXISTS feature_workspace_current_delivery_plan_guard;
DROP TRIGGER IF EXISTS delivery_ticket_plan_unit_link_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_plan_unit_link_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_plan_unit_link_binding_guard;
DROP TRIGGER IF EXISTS delivery_plan_unit_dependency_delete_guard;
DROP TRIGGER IF EXISTS delivery_plan_unit_dependency_update_immutable;
DROP TRIGGER IF EXISTS delivery_plan_unit_dependency_same_plan;
DROP TRIGGER IF EXISTS delivery_plan_unit_delete_guard;
DROP TRIGGER IF EXISTS delivery_plan_unit_update_immutable;
DROP TRIGGER IF EXISTS delivery_plan_delete_guard;
DROP TRIGGER IF EXISTS delivery_plan_update_immutable;
DROP TRIGGER IF EXISTS delivery_plan_binding_guard;
DROP INDEX IF EXISTS idx_delivery_ticket_plan_unit_links_ticket;
DROP INDEX IF EXISTS idx_delivery_ticket_plan_unit_links_plan;
DROP INDEX IF EXISTS idx_delivery_plan_unit_dependencies_unit;
DROP INDEX IF EXISTS idx_delivery_plan_units_plan;
DROP INDEX IF EXISTS idx_delivery_plans_workspace;
DROP TABLE IF EXISTS delivery_ticket_plan_unit_links;
DROP TABLE IF EXISTS delivery_plan_unit_dependencies;
DROP TABLE IF EXISTS delivery_plan_units;
DROP TABLE IF EXISTS delivery_plans;
ALTER TABLE feature_workspaces DROP COLUMN current_delivery_plan_row_id;

DROP TRIGGER IF EXISTS planning_candidate_basis_guard;
DROP TRIGGER IF EXISTS planning_candidate_update_immutable;
DROP TRIGGER IF EXISTS planning_candidate_delete_guard;
DROP TRIGGER IF EXISTS planning_candidate_approval_binding_guard;
DROP TRIGGER IF EXISTS delivery_ticket_production_link_binding_guard;
DROP INDEX IF EXISTS idx_planning_candidates_workspace;
PRAGMA legacy_alter_table=ON;
ALTER TABLE planning_candidates RENAME TO planning_candidates_legacy;
PRAGMA legacy_alter_table=OFF;
CREATE TABLE planning_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    candidate_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    family TEXT NOT NULL CHECK (family IN ('requirements', 'shared_design', 'delivery_ticket')),
    filename TEXT NOT NULL,
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    artifact_size_bytes INTEGER NOT NULL CHECK (artifact_size_bytes >= 0),
    discovery_closure_packet_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_closure_packets(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    repo_target TEXT NOT NULL COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL CHECK (length(base_commit) = 40 AND base_commit NOT GLOB '*[^0-9a-f]*'),
    destination TEXT NOT NULL CHECK (destination IN ('direct_delivery_ticket', 'requirements', 'shared_design', 'requirements_then_shared_design', 'existing_route_continuation')),
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (candidate_id GLOB 'candidate-*' AND trim(candidate_id) = candidate_id),
    CHECK (filename <> '' AND trim(filename) = filename AND filename NOT LIKE '%/%' AND filename NOT LIKE '%\\%'),
    CHECK (branch <> '' AND trim(branch) = branch),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);
INSERT INTO planning_candidates (id, candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, authority_revision_row_id, repo_target, branch, base_commit, destination, created_identity, created_at)
SELECT id, candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, authority_revision_row_id, repo_target, branch, base_commit, destination, created_identity, created_at
FROM planning_candidates_legacy;
DROP TABLE planning_candidates_legacy;
CREATE INDEX idx_planning_candidates_workspace ON planning_candidates(workspace_row_id, created_at, id);
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_basis_guard
BEFORE INSERT ON planning_candidates
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM feature_workspaces AS workspace
    JOIN feature_workspace_discovery_closure_packets AS packet
      ON packet.id = NEW.discovery_closure_packet_row_id
     AND packet.workspace_row_id = workspace.id
     AND workspace.current_discovery_closure_packet_row_id = packet.id
     AND packet.closing_revision_row_id = workspace.current_discovery_revision_row_id
    WHERE workspace.id = NEW.workspace_row_id
)
OR (NEW.authority_revision_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM feature_workspaces AS workspace
    WHERE workspace.id = NEW.workspace_row_id
      AND workspace.current_authority_revision_row_id = NEW.authority_revision_row_id
))
OR NOT EXISTS (
    SELECT 1 FROM feature_workspace_discovery_artifacts AS artifact
    WHERE artifact.id = NEW.artifact_row_id
      AND artifact.workspace_row_id = NEW.workspace_row_id
      AND artifact.sha256 = NEW.artifact_sha256
      AND artifact.size_bytes = NEW.artifact_size_bytes
)
BEGIN SELECT RAISE(ABORT, 'planning candidate basis or exact artifact mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_update_immutable
BEFORE UPDATE ON planning_candidates
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'planning candidates are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_delete_guard
BEFORE DELETE ON planning_candidates
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'planning candidates are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_approval_binding_guard
BEFORE INSERT ON planning_candidate_approvals
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM planning_candidates AS candidate
    JOIN feature_workspace_discovery_artifacts AS artifact ON artifact.id = NEW.candidate_artifact_row_id
    WHERE candidate.id = NEW.candidate_row_id
      AND candidate.artifact_row_id = NEW.candidate_artifact_row_id
      AND candidate.artifact_sha256 = NEW.candidate_sha256
      AND candidate.artifact_size_bytes = NEW.candidate_size_bytes
      AND artifact.workspace_row_id = candidate.workspace_row_id
      AND artifact.sha256 = NEW.candidate_sha256
      AND artifact.size_bytes = NEW.candidate_size_bytes
)
BEGIN SELECT RAISE(ABORT, 'candidate approval must bind the exact candidate artifact'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_production_link_binding_guard
BEFORE INSERT ON delivery_ticket_production_links
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_tickets AS ticket
    JOIN planning_candidates AS candidate ON candidate.id = NEW.candidate_row_id
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.produced_revision_row_id
    JOIN feature_workspace_discovery_artifacts AS candidate_artifact ON candidate_artifact.id = NEW.candidate_artifact_row_id
    JOIN feature_workspace_discovery_artifacts AS canonical_artifact ON canonical_artifact.id = NEW.canonical_json_artifact_row_id
    JOIN feature_workspace_discovery_artifacts AS markdown_artifact ON markdown_artifact.id = NEW.rendered_markdown_artifact_row_id
    WHERE ticket.id = NEW.delivery_ticket_row_id
      AND ticket.workspace_row_id = candidate.workspace_row_id
      AND revision.delivery_ticket_row_id = ticket.id
      AND candidate_artifact.workspace_row_id = ticket.workspace_row_id
      AND candidate_artifact.sha256 = NEW.candidate_sha256
      AND candidate_artifact.size_bytes = NEW.candidate_size_bytes
      AND candidate.artifact_row_id = NEW.candidate_artifact_row_id
      AND candidate.artifact_sha256 = NEW.candidate_sha256
      AND candidate.artifact_size_bytes = NEW.candidate_size_bytes
      AND canonical_artifact.workspace_row_id = ticket.workspace_row_id
      AND canonical_artifact.sha256 = NEW.canonical_json_sha256
      AND canonical_artifact.size_bytes = NEW.canonical_json_size_bytes
      AND canonical_artifact.media_type = 'application/json'
      AND markdown_artifact.workspace_row_id = ticket.workspace_row_id
      AND markdown_artifact.sha256 = NEW.rendered_markdown_sha256
      AND markdown_artifact.size_bytes = NEW.rendered_markdown_size_bytes
      AND markdown_artifact.media_type = 'text/markdown'
)
BEGIN SELECT RAISE(ABORT, 'production link ownership or exact artifact mismatch'); END;
-- +goose StatementEnd

PRAGMA foreign_keys=on;
