-- +goose Up
-- +goose NO TRANSACTION
-- A selection owns a current immutable Brief attempt. Replacements retain the
-- same source-backed selection and advance the attempt number instead of
-- recreating the selection.
PRAGMA foreign_keys=off;

DROP TRIGGER IF EXISTS ticket_design_brief_approval_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_approval_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_approval_binding_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_basis_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_review_binding_guard;

ALTER TABLE delivery_ticket_selections
    ADD COLUMN current_ticket_design_brief_row_id INTEGER REFERENCES ticket_design_briefs(id) ON DELETE RESTRICT;

CREATE TABLE ticket_design_briefs_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    brief_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    selection_row_id INTEGER NOT NULL REFERENCES delivery_ticket_selections(id) ON DELETE RESTRICT,
    attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    filename TEXT NOT NULL,
    artifact_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    artifact_size_bytes INTEGER NOT NULL CHECK (artifact_size_bytes >= 0),
    created_identity TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (selection_row_id, attempt_number),
    CHECK (brief_id GLOB 'brief-*' AND trim(brief_id) = brief_id),
    CHECK (filename <> '' AND trim(filename) = filename AND filename NOT LIKE '%/%' AND filename NOT LIKE '%\\%'),
    CHECK (created_identity <> '' AND trim(created_identity) = created_identity)
);

INSERT INTO ticket_design_briefs_next (
    id, brief_id, workspace_row_id, selection_row_id, attempt_number,
    revision_row_id, filename, artifact_row_id, artifact_sha256,
    artifact_size_bytes, created_identity, created_at
)
SELECT id, brief_id, workspace_row_id, selection_row_id, 1,
       revision_row_id, filename, artifact_row_id, artifact_sha256,
       artifact_size_bytes, created_identity, created_at
FROM ticket_design_briefs;

DROP TABLE ticket_design_briefs;
ALTER TABLE ticket_design_briefs_next RENAME TO ticket_design_briefs;

UPDATE delivery_ticket_selections
SET current_ticket_design_brief_row_id = (
    SELECT brief.id
    FROM ticket_design_briefs AS brief
    WHERE brief.selection_row_id = delivery_ticket_selections.id
    ORDER BY brief.attempt_number DESC, brief.id DESC
    LIMIT 1
);

CREATE INDEX idx_ticket_design_briefs_workspace ON ticket_design_briefs(workspace_row_id, created_at, id);
CREATE INDEX idx_ticket_design_briefs_selection_attempt ON ticket_design_briefs(selection_row_id, attempt_number, id);

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

-- The selection's current Brief pointer can only name an immutable attempt it
-- owns. The pointer is intentionally separate from selection state so source
-- invalidation retains inspectable Brief history without manufacturing a new
-- selection.
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_current_brief_insert_guard
BEFORE INSERT ON delivery_ticket_selections
FOR EACH ROW WHEN NEW.current_ticket_design_brief_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM ticket_design_briefs AS brief
    WHERE brief.id = NEW.current_ticket_design_brief_row_id
      AND brief.selection_row_id = NEW.id
)
BEGIN SELECT RAISE(ABORT, 'current ticket design brief must belong to its selection'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_current_brief_guard
BEFORE UPDATE OF current_ticket_design_brief_row_id ON delivery_ticket_selections
FOR EACH ROW WHEN NEW.current_ticket_design_brief_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM ticket_design_briefs AS brief
    WHERE brief.id = NEW.current_ticket_design_brief_row_id
      AND brief.selection_row_id = OLD.id
)
BEGIN SELECT RAISE(ABORT, 'current ticket design brief must belong to its selection'); END;
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

PRAGMA foreign_keys=on;

-- +goose Down
DROP TRIGGER IF EXISTS delivery_ticket_selection_current_brief_insert_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_current_brief_guard;
DROP INDEX IF EXISTS idx_ticket_design_briefs_selection_attempt;
