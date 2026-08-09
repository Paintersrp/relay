-- +goose Up
-- Ticket Design Briefs are durable, immutable authored inputs bound to the
-- current active Delivery Ticket selection. Authoring admits the exact brief
-- artifact; approval is a separate explicit confirmed owner mutation recorded
-- in ticket_design_brief_approvals. Read-only review never persists review
-- state, so the admissible authored brief is the review-ready basis.
CREATE TABLE ticket_design_briefs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    brief_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    selection_row_id INTEGER NOT NULL UNIQUE REFERENCES delivery_ticket_selections(id) ON DELETE RESTRICT,
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    filename TEXT NOT NULL,
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    artifact_size_bytes INTEGER NOT NULL CHECK (artifact_size_bytes >= 0),
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (brief_id GLOB 'brief-*' AND trim(brief_id) = brief_id),
    CHECK (filename <> '' AND trim(filename) = filename AND filename NOT LIKE '%/%' AND filename NOT LIKE '%\\%'),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);

CREATE TABLE ticket_design_brief_approvals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_id TEXT NOT NULL UNIQUE,
    brief_row_id INTEGER NOT NULL UNIQUE REFERENCES ticket_design_briefs(id) ON DELETE RESTRICT,
    brief_artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    brief_sha256 TEXT NOT NULL CHECK (length(brief_sha256) = 64 AND brief_sha256 NOT GLOB '*[^0-9a-f]*'),
    brief_size_bytes INTEGER NOT NULL CHECK (brief_size_bytes >= 0),
    operator_confirmation_evidence TEXT NOT NULL,
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (approval_id GLOB 'brief-approval-*' AND trim(approval_id) = approval_id),
    CHECK (operator_confirmation_evidence = trim(operator_confirmation_evidence) AND length(operator_confirmation_evidence) BETWEEN 1 AND 4096),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);

CREATE INDEX idx_ticket_design_briefs_workspace ON ticket_design_briefs(workspace_row_id, created_at, id);
CREATE INDEX idx_ticket_design_brief_approvals_brief ON ticket_design_brief_approvals(brief_row_id, id);

-- A brief must bind the workspace's current active selection, the selection's
-- exact current revision, and an exact workspace-owned artifact.
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_basis_guard
BEFORE INSERT ON ticket_design_briefs
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM feature_workspaces AS workspace
    JOIN delivery_ticket_selections AS selection ON selection.id = NEW.selection_row_id
    JOIN delivery_ticket_selection_members AS member ON member.selection_row_id = selection.id
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.revision_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
    JOIN feature_workspace_discovery_artifacts AS artifact ON artifact.id = NEW.artifact_row_id
    WHERE workspace.id = NEW.workspace_row_id
      AND selection.workspace_row_id = workspace.id
      AND selection.state = 'active'
      AND member.revision_row_id = NEW.revision_row_id
      AND ticket.workspace_row_id = workspace.id
      AND ticket.current_revision_row_id = NEW.revision_row_id
      AND revision.delivery_ticket_row_id = ticket.id
      AND artifact.workspace_row_id = workspace.id
      AND artifact.sha256 = NEW.artifact_sha256
      AND artifact.size_bytes = NEW.artifact_size_bytes
)
BEGIN SELECT RAISE(ABORT, 'ticket design brief basis or exact artifact mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_update_immutable
BEFORE UPDATE ON ticket_design_briefs
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'ticket design briefs are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_delete_guard
BEFORE DELETE ON ticket_design_briefs
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'ticket design briefs are retained history'); END;
-- +goose StatementEnd

-- Approval stores an exact brief snapshot. It is an explicit confirmed owner
-- mutation, not a review record and not lifecycle state.
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_approval_binding_guard
BEFORE INSERT ON ticket_design_brief_approvals
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM ticket_design_briefs AS brief
    JOIN feature_workspace_discovery_artifacts AS artifact ON artifact.id = NEW.brief_artifact_row_id
    WHERE brief.id = NEW.brief_row_id
      AND brief.artifact_row_id = NEW.brief_artifact_row_id
      AND brief.artifact_sha256 = NEW.brief_sha256
      AND brief.artifact_size_bytes = NEW.brief_size_bytes
      AND artifact.sha256 = NEW.brief_sha256
      AND artifact.size_bytes = NEW.brief_size_bytes
)
BEGIN SELECT RAISE(ABORT, 'brief approval must bind the exact brief artifact'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_approval_update_immutable
BEFORE UPDATE ON ticket_design_brief_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'brief approvals are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_approval_delete_guard
BEFORE DELETE ON ticket_design_brief_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'brief approvals are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS ticket_design_brief_approval_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_approval_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_approval_binding_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_basis_guard;
DROP INDEX IF EXISTS idx_ticket_design_brief_approvals_brief;
DROP INDEX IF EXISTS idx_ticket_design_briefs_workspace;
DROP TABLE IF EXISTS ticket_design_brief_approvals;
DROP TABLE IF EXISTS ticket_design_briefs;
