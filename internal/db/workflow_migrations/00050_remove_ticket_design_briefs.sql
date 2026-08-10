-- +goose Up
-- +goose NO TRANSACTION
-- The Ticket Design Brief is no longer an authority surface: the selected
-- approved Delivery Ticket v2 revision is the sole ticket semantic authority.
-- Remove the Brief tables, the selection current-Brief pointer, and the
-- design_brief_sha256 column from execution_packages. Historical Brief rows are
-- removed with their schema; no code path may authorize package state from
-- Brief residue.
PRAGMA foreign_keys=off;

DROP TRIGGER IF EXISTS ticket_design_brief_approval_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_approval_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_approval_binding_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_basis_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_current_brief_insert_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_current_brief_guard;

DROP INDEX IF EXISTS idx_ticket_design_briefs_workspace;
DROP INDEX IF EXISTS idx_ticket_design_briefs_selection_attempt;
DROP INDEX IF EXISTS idx_ticket_design_brief_approvals_brief;

DROP TABLE IF EXISTS ticket_design_brief_approvals;
DROP TABLE IF EXISTS ticket_design_briefs;

-- The selection current-Brief pointer referenced the removed table and is dead
-- schema; drop it so the selection shape has no Brief residue.
ALTER TABLE delivery_ticket_selections
    DROP COLUMN current_ticket_design_brief_row_id;

-- Rebuild execution_packages without design_brief_sha256. Row IDs are
-- preserved so every foreign-key reference (members, approval bindings,
-- approvals, runs) remains exact; only the active shape changes. Entity
-- triggers referencing execution_packages are dropped first (and recreated
-- below), and legacy_alter_table keeps the RENAME from re-verifying the
-- remaining audit/run triggers whose bodies reference execution_packages
-- while the old table is transiently absent.
DROP TRIGGER IF EXISTS execution_package_delete_guard;
DROP TRIGGER IF EXISTS execution_package_update_immutable;
DROP TRIGGER IF EXISTS execution_package_input_guard;
DROP TRIGGER IF EXISTS execution_package_member_guard;
DROP TRIGGER IF EXISTS execution_package_approval_binding_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_consumption_guard;
DROP TRIGGER IF EXISTS run_execution_package_insert_guard;
DROP TRIGGER IF EXISTS run_execution_package_update_guard;
DROP INDEX IF EXISTS idx_execution_packages_workspace;
DROP INDEX IF EXISTS idx_execution_packages_source_authority;

CREATE TABLE execution_packages_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    package_id TEXT NOT NULL UNIQUE,
    selection_row_id INTEGER NOT NULL UNIQUE REFERENCES delivery_ticket_selections(id) ON DELETE RESTRICT,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    repo_target TEXT NOT NULL COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL CHECK (length(base_commit) = 40 AND base_commit NOT GLOB '*[^0-9a-f]*'),
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    package_sha256 TEXT NOT NULL CHECK (length(package_sha256) = 64 AND package_sha256 NOT GLOB '*[^0-9a-f]*'),
    authority_sha256 TEXT NOT NULL CHECK (length(authority_sha256) = 64 AND authority_sha256 NOT GLOB '*[^0-9a-f]*'),
    source_sha256 TEXT NOT NULL CHECK (length(source_sha256) = 64 AND source_sha256 NOT GLOB '*[^0-9a-f]*'),
    deterministic_operations_sha256 TEXT CHECK (deterministic_operations_sha256 IS NULL OR (length(deterministic_operations_sha256) = 64 AND deterministic_operations_sha256 NOT GLOB '*[^0-9a-f]*')),
    deterministic_operations_coverage TEXT CHECK (deterministic_operations_coverage IS NULL OR deterministic_operations_coverage IN ('complete', 'partial')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK ((deterministic_operations_sha256 IS NULL AND deterministic_operations_coverage IS NULL) OR (deterministic_operations_sha256 IS NOT NULL AND deterministic_operations_coverage IS NOT NULL)),
    CHECK (package_id GLOB 'package-*' AND trim(package_id) = package_id),
    CHECK (branch <> '' AND trim(branch) = branch)
);

INSERT INTO execution_packages_next (
    id, package_id, selection_row_id, workspace_row_id, repo_target, branch,
    base_commit, source_closure_row_id, authority_revision_row_id,
    package_sha256, authority_sha256, source_sha256,
    deterministic_operations_sha256, deterministic_operations_coverage, created_at
)
SELECT id, package_id, selection_row_id, workspace_row_id, repo_target, branch,
       base_commit, source_closure_row_id, authority_revision_row_id,
       package_sha256, authority_sha256, source_sha256,
       deterministic_operations_sha256, deterministic_operations_coverage, created_at
FROM execution_packages;

PRAGMA legacy_alter_table=ON;

DROP TABLE execution_packages;
ALTER TABLE execution_packages_next RENAME TO execution_packages;

PRAGMA legacy_alter_table=OFF;

CREATE INDEX idx_execution_packages_workspace ON execution_packages(workspace_row_id, created_at, id);
CREATE INDEX idx_execution_packages_source_authority ON execution_packages(source_closure_row_id, authority_revision_row_id, id);

-- +goose StatementBegin
CREATE TRIGGER execution_package_input_guard
BEFORE INSERT ON execution_packages
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_ticket_selections AS selection
    JOIN feature_workspaces AS workspace ON workspace.id = selection.workspace_row_id
    JOIN feature_workspace_authority_revisions AS authority ON authority.id = NEW.authority_revision_row_id
    JOIN source_vault_closures AS closure ON closure.id = NEW.source_closure_row_id
    JOIN source_vaults AS vault ON vault.id = closure.vault_row_id
    JOIN repository_targets AS target ON target.repo_target = NEW.repo_target COLLATE NOCASE
    WHERE selection.id = NEW.selection_row_id
      AND selection.state = 'active'
      AND selection.workspace_row_id = NEW.workspace_row_id
      AND selection.source_closure_row_id = NEW.source_closure_row_id
      AND authority.workspace_row_id = workspace.id
      AND authority.source_closure_row_id = NEW.source_closure_row_id
      AND closure.state = 'ready'
      AND vault.repo_target = NEW.repo_target COLLATE NOCASE
      AND closure.commit_oid = NEW.base_commit
      AND target.configured_branch_ref = 'refs/heads/' || NEW.branch
      AND EXISTS (
          SELECT 1 FROM delivery_ticket_selection_members AS member
          WHERE member.selection_row_id = selection.id
      )
)
BEGIN SELECT RAISE(ABORT, 'execution package must bind the active selection source and authority basis'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER execution_package_update_immutable
BEFORE UPDATE ON execution_packages
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution packages are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER execution_package_delete_guard
BEFORE DELETE ON execution_packages
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution packages are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER execution_package_member_guard
BEFORE INSERT ON execution_package_members
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM execution_packages AS package
    JOIN delivery_ticket_selection_members AS selection_member ON selection_member.id = NEW.selection_member_row_id
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.revision_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
    WHERE package.id = NEW.package_row_id
      AND selection_member.selection_row_id = package.selection_row_id
      AND selection_member.revision_row_id = revision.id
      AND ticket.workspace_row_id = package.workspace_row_id
      AND revision.repo_target = package.repo_target COLLATE NOCASE
      AND revision.branch = package.branch
      AND revision.base_commit = package.base_commit
      AND revision.source_closure_row_id = package.source_closure_row_id
)
BEGIN SELECT RAISE(ABORT, 'execution package member must be an exact selection revision on the package basis'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER execution_package_approval_binding_guard
BEFORE INSERT ON execution_package_approval_bindings
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM execution_packages AS package
    JOIN execution_package_members AS package_member ON package_member.id = NEW.package_member_row_id
    JOIN delivery_ticket_selection_members AS selection_member ON selection_member.id = package_member.selection_member_row_id
    JOIN delivery_ticket_revision_approvals AS approval ON approval.id = NEW.approval_row_id
    JOIN feature_workspace_authority_revisions AS authority ON authority.id = NEW.authority_revision_row_id
    WHERE package.id = NEW.package_row_id
      AND package_member.package_row_id = package.id
      AND approval.id = selection_member.approval_row_id
      AND approval.revision_row_id = package_member.revision_row_id
      AND approval.approval_kind = 'delivery'
      AND approval.approval_state = 'approved'
      AND approval.source_closure_row_id = package.source_closure_row_id
      AND approval.authority_revision_row_id = package.authority_revision_row_id
      AND NEW.source_closure_row_id = package.source_closure_row_id
      AND NEW.authority_revision_row_id = package.authority_revision_row_id
      AND authority.workspace_row_id = package.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'execution package approval must bind the selected approval compoundly to package authority and source'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_consumption_guard
BEFORE UPDATE OF state ON delivery_ticket_selections
FOR EACH ROW WHEN NEW.state = 'consumed' AND NOT (
    EXISTS (
        SELECT 1
        FROM execution_packages AS package
        WHERE package.selection_row_id = OLD.id
    )
    AND NOT EXISTS (
        SELECT 1
        FROM delivery_ticket_selection_members AS selection_member
        WHERE selection_member.selection_row_id = OLD.id
          AND NOT EXISTS (
              SELECT 1
              FROM execution_package_members AS package_member
              JOIN execution_packages AS package ON package.id = package_member.package_row_id
              WHERE package.selection_row_id = OLD.id
                AND package_member.selection_member_row_id = selection_member.id
          )
    )
    AND NOT EXISTS (
        SELECT 1
        FROM execution_package_members AS package_member
        JOIN execution_packages AS package ON package.id = package_member.package_row_id
        WHERE package.selection_row_id = OLD.id
          AND NOT EXISTS (
              SELECT 1
              FROM execution_package_approval_bindings AS binding
              WHERE binding.package_row_id = package.id
                AND binding.package_member_row_id = package_member.id
          )
    )
)
BEGIN SELECT RAISE(ABORT, 'delivery ticket selection consumption requires one complete execution package'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER run_execution_package_insert_guard
BEFORE INSERT ON runs
FOR EACH ROW WHEN NEW.execution_package_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM execution_packages AS package
    WHERE package.id = NEW.execution_package_row_id
      AND package.repo_target = NEW.repo_target COLLATE NOCASE
      AND package.branch = NEW.branch
      AND package.base_commit = NEW.base_commit
)
BEGIN SELECT RAISE(ABORT, 'Run package link must match repository branch and base commit'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER run_execution_package_update_guard
BEFORE UPDATE OF execution_package_row_id ON runs
FOR EACH ROW WHEN OLD.execution_package_row_id IS NOT NULL
    OR NEW.execution_package_row_id IS NULL
    OR NOT EXISTS (
        SELECT 1
        FROM execution_packages AS package
        WHERE package.id = NEW.execution_package_row_id
          AND package.repo_target = NEW.repo_target COLLATE NOCASE
          AND package.branch = NEW.branch
          AND package.base_commit = NEW.base_commit
    )
BEGIN SELECT RAISE(ABORT, 'Run package link is immutable and must match repository branch and base commit'); END;
-- +goose StatementEnd

PRAGMA foreign_keys=on;

-- +goose Down
-- The Brief surface is not restored: the selected approved Delivery Ticket is
-- the sole ticket semantic authority and historical Brief schema cannot
-- authorize packages.
SELECT 1;
