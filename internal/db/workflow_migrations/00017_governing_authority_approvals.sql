-- +goose Up
CREATE TABLE governing_artifact_approvals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    artifact_row_id INTEGER REFERENCES artifacts(id) ON DELETE RESTRICT,
    retained_artifact_row_id INTEGER REFERENCES operation_packet_retained_artifacts(id) ON DELETE RESTRICT,
    family TEXT NOT NULL CHECK (family IN ('requirements', 'design', 'transition_plan')),
    artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
    operator_confirmation_evidence TEXT NOT NULL DEFAULT '',
    invalidated_by_approval_row_id INTEGER REFERENCES governing_artifact_approvals(id) ON DELETE RESTRICT,
    superseded_by_approval_row_id INTEGER REFERENCES governing_artifact_approvals(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (approval_id GLOB 'ga-approval-*' AND trim(approval_id) = approval_id),
    CHECK ((artifact_row_id IS NOT NULL AND retained_artifact_row_id IS NULL) OR (artifact_row_id IS NULL AND retained_artifact_row_id IS NOT NULL)),
    CHECK (operator_confirmation_evidence = trim(operator_confirmation_evidence) AND length(operator_confirmation_evidence) <= 4096)
);

ALTER TABLE feature_workspace_authority_layers ADD COLUMN approval_row_id INTEGER;

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_authority_layer_approval_guard
BEFORE INSERT ON feature_workspace_authority_layers
FOR EACH ROW WHEN NEW.approval_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM governing_artifact_approvals AS approval
    JOIN feature_workspace_authority_revisions AS revision ON revision.id = NEW.authority_revision_row_id
    WHERE approval.id = NEW.approval_row_id
      AND approval.workspace_row_id = revision.workspace_row_id
      AND approval.family = CASE WHEN NEW.layer_kind = 'plan' THEN 'transition_plan' ELSE NEW.layer_kind END
      AND approval.artifact_sha256 = NEW.artifact_sha256
      AND ((approval.artifact_row_id IS NOT NULL AND approval.artifact_row_id = NEW.artifact_row_id)
           OR (approval.retained_artifact_row_id IS NOT NULL AND approval.retained_artifact_row_id = NEW.retained_artifact_row_id))
      AND approval.invalidated_by_approval_row_id IS NULL
      AND approval.superseded_by_approval_row_id IS NULL
)
BEGIN SELECT RAISE(ABORT, 'authority layer approval does not match exact workspace, artifact, family, and sha256'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_approval_update_immutable
BEFORE UPDATE ON governing_artifact_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'governing artifact approvals are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_approval_delete_guard
BEFORE DELETE ON governing_artifact_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'governing artifact approvals are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_exact_approval_artifact_guard
BEFORE INSERT ON governing_artifact_approvals
FOR EACH ROW WHEN (NEW.artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM artifacts WHERE id = NEW.artifact_row_id AND sha256 = NEW.artifact_sha256)) OR (NEW.retained_artifact_row_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM operation_packet_retained_artifacts WHERE id = NEW.retained_artifact_row_id AND sha256 = NEW.artifact_sha256))
BEGIN SELECT RAISE(ABORT, 'governing artifact approval reference does not match sha256'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_approval_same_workspace_invalidation
BEFORE INSERT ON governing_artifact_approvals
FOR EACH ROW WHEN NEW.invalidated_by_approval_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM governing_artifact_approvals AS target
    WHERE target.id = NEW.invalidated_by_approval_row_id AND target.workspace_row_id = NEW.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'governing artifact approval invalidation target must share the workspace'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER feature_workspace_approval_same_workspace_supersession
BEFORE INSERT ON governing_artifact_approvals
FOR EACH ROW WHEN NEW.superseded_by_approval_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM governing_artifact_approvals AS target
    WHERE target.id = NEW.superseded_by_approval_row_id AND target.workspace_row_id = NEW.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'governing artifact approval supersession target must share the workspace'); END;
-- +goose StatementEnd

CREATE INDEX idx_governing_artifact_approvals_workspace ON governing_artifact_approvals(workspace_row_id, family, artifact_sha256, id);

-- +goose Down
DROP TRIGGER IF EXISTS feature_workspace_approval_same_workspace_supersession;
DROP TRIGGER IF EXISTS feature_workspace_approval_same_workspace_invalidation;
DROP TRIGGER IF EXISTS feature_workspace_exact_approval_artifact_guard;
DROP TRIGGER IF EXISTS feature_workspace_approval_delete_guard;
DROP TRIGGER IF EXISTS feature_workspace_approval_update_immutable;
DROP TRIGGER IF EXISTS feature_workspace_authority_layer_approval_guard;
DROP INDEX IF EXISTS idx_governing_artifact_approvals_workspace;
ALTER TABLE feature_workspace_authority_layers DROP COLUMN approval_row_id;
DROP TABLE IF EXISTS governing_artifact_approvals;
