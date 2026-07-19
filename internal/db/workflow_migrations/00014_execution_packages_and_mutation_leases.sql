-- +goose Up
-- A selection becomes immutable package input only when it is consumed. Rebuild
-- the small selection tables so the consumed state remains durable history.
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_transition_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_identity_immutable;
DROP INDEX IF EXISTS idx_delivery_ticket_selection_members_selection;
DROP INDEX IF EXISTS idx_delivery_ticket_selections_one_active_workspace;

CREATE TABLE delivery_ticket_selections_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    selection_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    state TEXT NOT NULL CHECK (state IN ('active', 'consumed', 'superseded', 'cancelled')),
    rationale TEXT NOT NULL,
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (selection_id GLOB 'selection-*' AND trim(selection_id) = selection_id),
    CHECK (rationale <> '' AND trim(rationale) <> '')
);

INSERT INTO delivery_ticket_selections_next (
    id, selection_id, workspace_row_id, state, rationale, source_closure_row_id, created_at, updated_at
)
SELECT id, selection_id, workspace_row_id, state, rationale, source_closure_row_id, created_at, updated_at
FROM delivery_ticket_selections;

CREATE TABLE delivery_ticket_selection_members_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    selection_row_id INTEGER NOT NULL REFERENCES delivery_ticket_selections_next(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    approval_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revision_approvals(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (selection_row_id, sequence),
    UNIQUE (selection_row_id, revision_row_id)
);

INSERT INTO delivery_ticket_selection_members_next (
    id, selection_row_id, sequence, revision_row_id, approval_row_id, created_at
)
SELECT id, selection_row_id, sequence, revision_row_id, approval_row_id, created_at
FROM delivery_ticket_selection_members;

DROP TABLE delivery_ticket_selection_members;
DROP TABLE delivery_ticket_selections;
ALTER TABLE delivery_ticket_selections_next RENAME TO delivery_ticket_selections;
ALTER TABLE delivery_ticket_selection_members_next RENAME TO delivery_ticket_selection_members;

CREATE UNIQUE INDEX idx_delivery_ticket_selections_one_active_workspace
ON delivery_ticket_selections(workspace_row_id)
WHERE state = 'active';

CREATE INDEX idx_delivery_ticket_selection_members_selection
ON delivery_ticket_selection_members(selection_row_id, sequence, id);

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_identity_immutable
BEFORE UPDATE OF selection_id, workspace_row_id, rationale, source_closure_row_id, created_at ON delivery_ticket_selections
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selection identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_transition_guard
BEFORE UPDATE OF state ON delivery_ticket_selections
FOR EACH ROW WHEN NOT (OLD.state = 'active' AND NEW.state IN ('consumed', 'superseded', 'cancelled'))
BEGIN SELECT RAISE(ABORT, 'invalid delivery ticket selection transition'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_delete_guard
BEFORE DELETE ON delivery_ticket_selections
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selections are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_member_guard
BEFORE INSERT ON delivery_ticket_selection_members
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_ticket_selections AS selection
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.revision_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
    JOIN delivery_ticket_revision_approvals AS approval ON approval.id = NEW.approval_row_id
    WHERE selection.id = NEW.selection_row_id
      AND ticket.workspace_row_id = selection.workspace_row_id
      AND approval.revision_row_id = revision.id
      AND approval.approval_state = 'approved'
)
BEGIN SELECT RAISE(ABORT, 'selection member must bind an approved revision in the selection workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_member_update_immutable
BEFORE UPDATE ON delivery_ticket_selection_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selection members are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_member_delete_guard
BEFORE DELETE ON delivery_ticket_selection_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selection members are immutable history'); END;
-- +goose StatementEnd

CREATE TABLE execution_packages (
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
    design_brief_sha256 TEXT NOT NULL CHECK (length(design_brief_sha256) = 64 AND design_brief_sha256 NOT GLOB '*[^0-9a-f]*'),
    execution_spec_sha256 TEXT NOT NULL CHECK (length(execution_spec_sha256) = 64 AND execution_spec_sha256 NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (package_id GLOB 'package-*' AND trim(package_id) = package_id),
    CHECK (branch <> '' AND trim(branch) = branch)
);

CREATE TABLE execution_package_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    package_row_id INTEGER NOT NULL REFERENCES execution_packages(id) ON DELETE RESTRICT,
    selection_member_row_id INTEGER NOT NULL REFERENCES delivery_ticket_selection_members(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    member_sha256 TEXT NOT NULL CHECK (length(member_sha256) = 64 AND member_sha256 NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (package_row_id, sequence),
    UNIQUE (package_row_id, selection_member_row_id),
    UNIQUE (package_row_id, revision_row_id)
);

CREATE TABLE execution_package_approval_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    package_row_id INTEGER NOT NULL REFERENCES execution_packages(id) ON DELETE RESTRICT,
    package_member_row_id INTEGER NOT NULL UNIQUE REFERENCES execution_package_members(id) ON DELETE RESTRICT,
    approval_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revision_approvals(id) ON DELETE RESTRICT,
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    approval_basis_sha256 TEXT NOT NULL CHECK (length(approval_basis_sha256) = 64 AND approval_basis_sha256 NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (package_row_id, approval_row_id)
);

CREATE INDEX idx_execution_packages_workspace ON execution_packages(workspace_row_id, created_at, id);
CREATE INDEX idx_execution_packages_source_authority ON execution_packages(source_closure_row_id, authority_revision_row_id, id);
CREATE INDEX idx_execution_package_members_package ON execution_package_members(package_row_id, sequence, id);
CREATE INDEX idx_execution_package_approval_bindings_package ON execution_package_approval_bindings(package_row_id, package_member_row_id, id);

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
CREATE TRIGGER execution_package_member_update_immutable
BEFORE UPDATE ON execution_package_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution package members are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER execution_package_member_delete_guard
BEFORE DELETE ON execution_package_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution package members are retained history'); END;
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
CREATE TRIGGER execution_package_approval_binding_update_immutable
BEFORE UPDATE ON execution_package_approval_bindings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution package approval bindings are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER execution_package_approval_binding_delete_guard
BEFORE DELETE ON execution_package_approval_bindings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution package approval bindings are retained history'); END;
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

ALTER TABLE runs
ADD COLUMN execution_package_row_id INTEGER REFERENCES execution_packages(id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX idx_runs_one_execution_package
ON runs(execution_package_row_id)
WHERE execution_package_row_id IS NOT NULL;

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

CREATE TABLE repository_branch_mutation_leases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    lease_id TEXT NOT NULL UNIQUE,
    repo_target TEXT NOT NULL COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    branch TEXT NOT NULL,
    owner_kind TEXT NOT NULL,
    owner_identity TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'released')),
    uncertainty_state TEXT NOT NULL CHECK (uncertainty_state IN ('certain', 'uncertain')),
    uncertainty_reason TEXT,
    reconciliation_state TEXT NOT NULL CHECK (reconciliation_state IN ('not_required', 'required', 'in_progress', 'reconciled', 'failed')),
    reconciliation_note TEXT,
    acquired_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    released_at TEXT,
    reconciliation_started_at TEXT,
    reconciled_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (lease_id GLOB 'lease-*' AND trim(lease_id) = lease_id),
    CHECK (branch <> '' AND trim(branch) = branch),
    CHECK (owner_kind <> '' AND trim(owner_kind) = owner_kind),
    CHECK (owner_identity <> '' AND trim(owner_identity) = owner_identity),
    CHECK ((state = 'active' AND released_at IS NULL) OR (state = 'released' AND released_at IS NOT NULL)),
    CHECK ((uncertainty_state = 'certain' AND uncertainty_reason IS NULL) OR (uncertainty_state = 'uncertain' AND uncertainty_reason IS NOT NULL AND trim(uncertainty_reason) <> '')),
    CHECK (
        (reconciliation_state IN ('not_required', 'required') AND reconciliation_started_at IS NULL AND reconciled_at IS NULL) OR
        (reconciliation_state = 'in_progress' AND reconciliation_started_at IS NOT NULL AND reconciled_at IS NULL) OR
        (reconciliation_state IN ('reconciled', 'failed') AND reconciliation_started_at IS NOT NULL AND reconciled_at IS NOT NULL)
    ),
    CHECK (reconciliation_note IS NULL OR trim(reconciliation_note) <> '')
);

CREATE UNIQUE INDEX idx_repository_branch_mutation_leases_one_active
ON repository_branch_mutation_leases(repo_target COLLATE NOCASE, branch)
WHERE state = 'active';

CREATE INDEX idx_repository_branch_mutation_leases_owner
ON repository_branch_mutation_leases(owner_kind, owner_identity, created_at, id);

-- +goose StatementBegin
CREATE TRIGGER repository_branch_mutation_lease_identity_immutable
BEFORE UPDATE OF lease_id, repo_target, branch, owner_kind, owner_identity, acquired_at, created_at ON repository_branch_mutation_leases
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'repository branch mutation lease identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER repository_branch_mutation_lease_transition_guard
BEFORE UPDATE OF state ON repository_branch_mutation_leases
FOR EACH ROW WHEN NEW.state <> OLD.state AND NOT (OLD.state = 'active' AND NEW.state = 'released')
BEGIN SELECT RAISE(ABORT, 'invalid repository branch mutation lease transition'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER repository_branch_mutation_lease_delete_guard
BEFORE DELETE ON repository_branch_mutation_leases
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'repository branch mutation leases are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS repository_branch_mutation_lease_delete_guard;
DROP TRIGGER IF EXISTS repository_branch_mutation_lease_transition_guard;
DROP TRIGGER IF EXISTS repository_branch_mutation_lease_identity_immutable;
DROP INDEX IF EXISTS idx_repository_branch_mutation_leases_owner;
DROP INDEX IF EXISTS idx_repository_branch_mutation_leases_one_active;
DROP TABLE IF EXISTS repository_branch_mutation_leases;

DROP TRIGGER IF EXISTS run_execution_package_update_guard;
DROP TRIGGER IF EXISTS run_execution_package_insert_guard;
DROP INDEX IF EXISTS idx_runs_one_execution_package;
ALTER TABLE runs DROP COLUMN execution_package_row_id;

DROP TRIGGER IF EXISTS delivery_ticket_selection_consumption_guard;
DROP TRIGGER IF EXISTS execution_package_approval_binding_delete_guard;
DROP TRIGGER IF EXISTS execution_package_approval_binding_update_immutable;
DROP TRIGGER IF EXISTS execution_package_approval_binding_guard;
DROP TRIGGER IF EXISTS execution_package_member_delete_guard;
DROP TRIGGER IF EXISTS execution_package_member_update_immutable;
DROP TRIGGER IF EXISTS execution_package_member_guard;
DROP TRIGGER IF EXISTS execution_package_delete_guard;
DROP TRIGGER IF EXISTS execution_package_update_immutable;
DROP TRIGGER IF EXISTS execution_package_input_guard;
DROP INDEX IF EXISTS idx_execution_package_approval_bindings_package;
DROP INDEX IF EXISTS idx_execution_package_members_package;
DROP INDEX IF EXISTS idx_execution_packages_source_authority;
DROP INDEX IF EXISTS idx_execution_packages_workspace;
DROP TABLE IF EXISTS execution_package_approval_bindings;
DROP TABLE IF EXISTS execution_package_members;
DROP TABLE IF EXISTS execution_packages;

DROP TRIGGER IF EXISTS delivery_ticket_selection_member_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_transition_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_identity_immutable;
DROP INDEX IF EXISTS idx_delivery_ticket_selection_members_selection;
DROP INDEX IF EXISTS idx_delivery_ticket_selections_one_active_workspace;

CREATE TABLE delivery_ticket_selections_previous (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    selection_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    state TEXT NOT NULL CHECK (state IN ('active', 'superseded', 'cancelled')),
    rationale TEXT NOT NULL,
    source_closure_row_id INTEGER REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (selection_id GLOB 'selection-*' AND trim(selection_id) = selection_id),
    CHECK (rationale <> '' AND trim(rationale) <> '')
);

INSERT INTO delivery_ticket_selections_previous (
    id, selection_id, workspace_row_id, state, rationale, source_closure_row_id, created_at, updated_at
)
SELECT id, selection_id, workspace_row_id, state, rationale, source_closure_row_id, created_at, updated_at
FROM delivery_ticket_selections;

CREATE TABLE delivery_ticket_selection_members_previous (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    selection_row_id INTEGER NOT NULL REFERENCES delivery_ticket_selections_previous(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    approval_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revision_approvals(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (selection_row_id, sequence),
    UNIQUE (selection_row_id, revision_row_id)
);

INSERT INTO delivery_ticket_selection_members_previous (
    id, selection_row_id, sequence, revision_row_id, approval_row_id, created_at
)
SELECT id, selection_row_id, sequence, revision_row_id, approval_row_id, created_at
FROM delivery_ticket_selection_members;

DROP TABLE delivery_ticket_selection_members;
DROP TABLE delivery_ticket_selections;
ALTER TABLE delivery_ticket_selections_previous RENAME TO delivery_ticket_selections;
ALTER TABLE delivery_ticket_selection_members_previous RENAME TO delivery_ticket_selection_members;

CREATE UNIQUE INDEX idx_delivery_ticket_selections_one_active_workspace
ON delivery_ticket_selections(workspace_row_id)
WHERE state = 'active';

CREATE INDEX idx_delivery_ticket_selection_members_selection
ON delivery_ticket_selection_members(selection_row_id, sequence, id);

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_identity_immutable
BEFORE UPDATE OF selection_id, workspace_row_id, rationale, source_closure_row_id, created_at ON delivery_ticket_selections
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selection identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_transition_guard
BEFORE UPDATE OF state ON delivery_ticket_selections
FOR EACH ROW WHEN NOT (OLD.state = 'active' AND NEW.state IN ('superseded', 'cancelled'))
BEGIN SELECT RAISE(ABORT, 'invalid delivery ticket selection transition'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_delete_guard
BEFORE DELETE ON delivery_ticket_selections
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selections are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_member_guard
BEFORE INSERT ON delivery_ticket_selection_members
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_ticket_selections AS selection
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.revision_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
    JOIN delivery_ticket_revision_approvals AS approval ON approval.id = NEW.approval_row_id
    WHERE selection.id = NEW.selection_row_id
      AND ticket.workspace_row_id = selection.workspace_row_id
      AND approval.revision_row_id = revision.id
      AND approval.approval_state = 'approved'
)
BEGIN SELECT RAISE(ABORT, 'selection member must bind an approved revision in the selection workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_member_update_immutable
BEFORE UPDATE ON delivery_ticket_selection_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selection members are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_selection_member_delete_guard
BEFORE DELETE ON delivery_ticket_selection_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selection members are retained history'); END;
-- +goose StatementEnd
