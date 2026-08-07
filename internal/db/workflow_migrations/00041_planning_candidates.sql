-- +goose Up
-- Planning candidates are immutable, review-neutral inputs. Exact bytes live in
-- the existing immutable workflow artifact store and are bound by digest/size.
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

CREATE TABLE planning_candidate_approvals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_id TEXT NOT NULL UNIQUE,
    candidate_row_id INTEGER NOT NULL UNIQUE REFERENCES planning_candidates(id) ON DELETE RESTRICT,
    candidate_artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64 AND candidate_sha256 NOT GLOB '*[^0-9a-f]*'),
    candidate_size_bytes INTEGER NOT NULL CHECK (candidate_size_bytes >= 0),
    operator_confirmation_evidence TEXT NOT NULL,
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (approval_id GLOB 'candidate-approval-*' AND trim(approval_id) = approval_id),
    CHECK (operator_confirmation_evidence = trim(operator_confirmation_evidence) AND length(operator_confirmation_evidence) BETWEEN 1 AND 4096),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);

CREATE TABLE delivery_ticket_production_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    production_link_id TEXT NOT NULL UNIQUE,
    delivery_ticket_row_id INTEGER NOT NULL REFERENCES delivery_tickets(id) ON DELETE RESTRICT,
    candidate_row_id INTEGER NOT NULL REFERENCES planning_candidates(id) ON DELETE RESTRICT,
    candidate_artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64 AND candidate_sha256 NOT GLOB '*[^0-9a-f]*'),
    candidate_size_bytes INTEGER NOT NULL CHECK (candidate_size_bytes >= 0),
    canonical_json_artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    canonical_json_sha256 TEXT NOT NULL CHECK (length(canonical_json_sha256) = 64 AND canonical_json_sha256 NOT GLOB '*[^0-9a-f]*'),
    canonical_json_size_bytes INTEGER NOT NULL CHECK (canonical_json_size_bytes >= 0),
    rendered_markdown_artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    rendered_markdown_sha256 TEXT NOT NULL CHECK (length(rendered_markdown_sha256) = 64 AND rendered_markdown_sha256 NOT GLOB '*[^0-9a-f]*'),
    rendered_markdown_size_bytes INTEGER NOT NULL CHECK (rendered_markdown_size_bytes >= 0),
    produced_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    produced_revision_identity TEXT NOT NULL,
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (production_link_id GLOB 'production-link-*' AND trim(production_link_id) = production_link_id),
    CHECK (produced_revision_identity <> '' AND trim(produced_revision_identity) = produced_revision_identity),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);

CREATE INDEX idx_planning_candidates_workspace ON planning_candidates(workspace_row_id, created_at, id);
CREATE INDEX idx_planning_candidate_approvals_candidate ON planning_candidate_approvals(candidate_row_id, id);
CREATE INDEX idx_delivery_ticket_production_links_ticket ON delivery_ticket_production_links(delivery_ticket_row_id, created_at, id);
CREATE INDEX idx_delivery_ticket_production_links_candidate ON delivery_ticket_production_links(candidate_row_id, id);

-- A candidate must bind the workspace's current discovery closure and, when
-- supplied, its current authority revision. This prevents stale basis claims
-- without changing the legacy workspace rows during migration.
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

-- Approval stores an exact candidate snapshot. It is not a review record and
-- contains no lifecycle state or downstream promotion authority.
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
CREATE TRIGGER planning_candidate_approval_update_immutable
BEFORE UPDATE ON planning_candidate_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'candidate approvals are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_approval_delete_guard
BEFORE DELETE ON planning_candidate_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'candidate approvals are retained history'); END;
-- +goose StatementEnd

-- Production linkage independently binds all input/output artifacts and the
-- produced revision identity. It deliberately has no approval foreign key.
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
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_production_link_update_immutable
BEFORE UPDATE ON delivery_ticket_production_links
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket production links are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_production_link_delete_guard
BEFORE DELETE ON delivery_ticket_production_links
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket production links are retained history'); END;
-- +goose StatementEnd

-- Candidate artifacts are Feature-owned immutable inputs. Authority layers may
-- bind one directly without pretending it is a run/plan artifact or a retained
-- publication artifact. Rebuild the legacy table because SQLite cannot alter a
-- CHECK constraint in place.
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_approval_guard;
DROP TRIGGER IF EXISTS feature_workspace_exact_authority_layer_artifact_guard;
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_update_immutable;
DROP INDEX IF EXISTS idx_feature_workspace_authority_layers_revision;
ALTER TABLE feature_workspace_authority_layers RENAME TO feature_workspace_authority_layers_legacy;
CREATE TABLE feature_workspace_authority_layers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    layer_kind TEXT NOT NULL CHECK (layer_kind IN ('requirements', 'design', 'plan', 'execution_spec', 'audit_decision')),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    artifact_row_id INTEGER REFERENCES artifacts(id) ON DELETE RESTRICT,
    retained_artifact_row_id INTEGER REFERENCES operation_packet_retained_artifacts(id) ON DELETE RESTRICT,
    candidate_artifact_row_id INTEGER REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    approval_row_id INTEGER REFERENCES governing_artifact_approvals(id) ON DELETE RESTRICT,
    UNIQUE (authority_revision_row_id, sequence),
    UNIQUE (authority_revision_row_id, layer_kind),
    CHECK ((artifact_row_id IS NOT NULL AND retained_artifact_row_id IS NULL AND candidate_artifact_row_id IS NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NOT NULL AND candidate_artifact_row_id IS NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NULL AND candidate_artifact_row_id IS NOT NULL))
);
INSERT INTO feature_workspace_authority_layers (id, authority_revision_row_id, layer_kind, sequence, artifact_row_id, retained_artifact_row_id, artifact_sha256, source_closure_row_id, created_at, approval_row_id)
SELECT id, authority_revision_row_id, layer_kind, sequence, artifact_row_id, retained_artifact_row_id, artifact_sha256, source_closure_row_id, created_at, approval_row_id
FROM feature_workspace_authority_layers_legacy;
DROP TABLE feature_workspace_authority_layers_legacy;
CREATE INDEX idx_feature_workspace_authority_layers_revision ON feature_workspace_authority_layers(authority_revision_row_id, sequence, id);
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_exact_authority_layer_artifact_guard
BEFORE INSERT ON feature_workspace_authority_layers
FOR EACH ROW WHEN (NEW.artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM artifacts WHERE id = NEW.artifact_row_id AND sha256 = NEW.artifact_sha256)) OR (NEW.retained_artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM operation_packet_retained_artifacts WHERE id = NEW.retained_artifact_row_id AND sha256 = NEW.artifact_sha256)) OR (NEW.candidate_artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_artifacts AS candidate WHERE candidate.id = NEW.candidate_artifact_row_id AND candidate.sha256 = NEW.artifact_sha256))
BEGIN SELECT RAISE(ABORT, 'authority layer artifact identity does not match'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_layer_approval_guard
BEFORE INSERT ON feature_workspace_authority_layers
FOR EACH ROW WHEN NEW.approval_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM governing_artifact_approvals AS approval
    JOIN feature_workspace_authority_revisions AS revision ON revision.id = NEW.authority_revision_row_id
    WHERE approval.id = NEW.approval_row_id AND approval.workspace_row_id = revision.workspace_row_id
      AND approval.family = CASE WHEN NEW.layer_kind = 'plan' THEN 'transition_plan' ELSE NEW.layer_kind END
      AND approval.artifact_sha256 = NEW.artifact_sha256
      AND ((approval.artifact_row_id IS NOT NULL AND approval.artifact_row_id = NEW.artifact_row_id) OR (approval.retained_artifact_row_id IS NOT NULL AND approval.retained_artifact_row_id = NEW.retained_artifact_row_id))
      AND approval.invalidated_by_approval_row_id IS NULL AND approval.superseded_by_approval_row_id IS NULL
)
BEGIN SELECT RAISE(ABORT, 'authority layer approval does not match exact workspace, artifact, family, and sha256'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_layer_update_immutable BEFORE UPDATE ON feature_workspace_authority_layers FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'authority layers are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_layer_delete_guard BEFORE DELETE ON feature_workspace_authority_layers FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'authority layers are immutable history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_approval_guard;
DROP TRIGGER IF EXISTS feature_workspace_exact_authority_layer_artifact_guard;

DROP TRIGGER IF EXISTS delivery_ticket_production_link_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_production_link_binding_guard;
DROP TRIGGER IF EXISTS planning_candidate_approval_delete_guard;
DROP TRIGGER IF EXISTS planning_candidate_approval_update_immutable;
DROP TRIGGER IF EXISTS planning_candidate_approval_binding_guard;
DROP TRIGGER IF EXISTS planning_candidate_delete_guard;
DROP TRIGGER IF EXISTS planning_candidate_update_immutable;
DROP TRIGGER IF EXISTS planning_candidate_basis_guard;
DROP INDEX IF EXISTS idx_delivery_ticket_production_links_candidate;
DROP INDEX IF EXISTS idx_delivery_ticket_production_links_ticket;
DROP INDEX IF EXISTS idx_planning_candidate_approvals_candidate;
DROP INDEX IF EXISTS idx_planning_candidates_workspace;
DROP TABLE IF EXISTS delivery_ticket_production_links;
DROP TABLE IF EXISTS planning_candidate_approvals;
DROP TABLE IF EXISTS planning_candidates;
