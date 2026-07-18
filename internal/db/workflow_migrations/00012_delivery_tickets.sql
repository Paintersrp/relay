-- +goose Up
CREATE TABLE delivery_tickets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    external_priority INTEGER NOT NULL DEFAULT 0 CHECK (external_priority >= 0),
    current_revision_row_id INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (workspace_row_id, ticket_id),
    CHECK (ticket_id GLOB '[A-Z]*' AND ticket_id NOT GLOB '*[^A-Z0-9-]*' AND trim(ticket_id) = ticket_id)
);

CREATE TABLE delivery_ticket_revisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    delivery_ticket_row_id INTEGER NOT NULL REFERENCES delivery_tickets(id) ON DELETE RESTRICT,
    revision_number INTEGER NOT NULL CHECK (revision_number >= 1),
    replaces_revision_row_id INTEGER REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    cancellation_reason TEXT,
    repo_target TEXT NOT NULL COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL CHECK (length(base_commit) = 40 AND base_commit NOT GLOB '*[^0-9a-f]*'),
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    source_path TEXT NOT NULL,
    goal TEXT NOT NULL,
    context TEXT NOT NULL,
    transition_applicability TEXT NOT NULL CHECK (transition_applicability IN ('not_required', 'required')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (delivery_ticket_row_id, revision_number),
    UNIQUE (delivery_ticket_row_id, replaces_revision_row_id),
    CHECK (source_path <> '' AND trim(source_path) = source_path AND source_path NOT LIKE '/%' AND source_path NOT LIKE '%\\%' AND source_path NOT LIKE '../%' AND source_path NOT LIKE '%/../%' AND source_path NOT LIKE '%//%' AND source_path NOT LIKE '%/'),
    CHECK (goal <> '' AND trim(goal) = goal),
    CHECK (context <> '' AND trim(context) <> ''),
    CHECK (branch <> '' AND trim(branch) = branch),
    CHECK (cancellation_reason IS NULL OR (trim(cancellation_reason) = cancellation_reason AND cancellation_reason <> ''))
);

CREATE TABLE delivery_ticket_revision_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    member_kind TEXT NOT NULL CHECK (member_kind IN ('scope_in', 'scope_out', 'implementation_obligation', 'validation_intent', 'completion_criterion')),
    member_path TEXT,
    member_text TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (revision_row_id, sequence),
    CHECK (member_path IS NULL OR (member_path <> '' AND trim(member_path) = member_path AND member_path NOT LIKE '/%' AND member_path NOT LIKE '%\\%' AND member_path NOT LIKE '../%' AND member_path NOT LIKE '%/../%' AND member_path NOT LIKE '%//%' AND member_path NOT LIKE '%/')),
    CHECK (member_text <> '' AND trim(member_text) <> '')
);

CREATE TABLE delivery_ticket_revision_dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    depends_on_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    outcome TEXT NOT NULL CHECK (outcome IN ('satisfied', 'blocked', 'not_applicable')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (revision_row_id, sequence),
    UNIQUE (revision_row_id, depends_on_revision_row_id),
    CHECK (revision_row_id <> depends_on_revision_row_id)
);

CREATE TABLE delivery_ticket_revision_approvals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_id TEXT NOT NULL UNIQUE,
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    approval_kind TEXT NOT NULL CHECK (approval_kind IN ('delivery')),
    approval_state TEXT NOT NULL CHECK (approval_state IN ('approved', 'rejected')),
    rationale TEXT NOT NULL,
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (revision_row_id, approval_kind),
    CHECK (approval_id GLOB 'approval-*' AND trim(approval_id) = approval_id),
    CHECK (rationale <> '' AND trim(rationale) <> '')
);

CREATE TABLE delivery_ticket_selections (
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

CREATE TABLE delivery_ticket_selection_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    selection_row_id INTEGER NOT NULL REFERENCES delivery_ticket_selections(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    approval_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revision_approvals(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (selection_row_id, sequence),
    UNIQUE (selection_row_id, revision_row_id)
);

CREATE UNIQUE INDEX idx_delivery_ticket_selections_one_active_workspace
ON delivery_ticket_selections(workspace_row_id)
WHERE state = 'active';

CREATE INDEX idx_delivery_tickets_workspace ON delivery_tickets(workspace_row_id, external_priority DESC, id);
CREATE INDEX idx_delivery_ticket_revisions_ticket ON delivery_ticket_revisions(delivery_ticket_row_id, revision_number, id);
CREATE INDEX idx_delivery_ticket_members_revision ON delivery_ticket_revision_members(revision_row_id, sequence, id);
CREATE INDEX idx_delivery_ticket_dependencies_revision ON delivery_ticket_revision_dependencies(revision_row_id, sequence, id);
CREATE INDEX idx_delivery_ticket_approvals_revision ON delivery_ticket_revision_approvals(revision_row_id, id);
CREATE INDEX idx_delivery_ticket_selection_members_selection ON delivery_ticket_selection_members(selection_row_id, sequence, id);

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_identity_immutable
BEFORE UPDATE OF ticket_id, workspace_row_id, current_revision_row_id, created_at ON delivery_tickets
FOR EACH ROW WHEN NEW.ticket_id <> OLD.ticket_id OR NEW.workspace_row_id <> OLD.workspace_row_id OR NEW.created_at <> OLD.created_at
BEGIN SELECT RAISE(ABORT, 'delivery ticket identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_current_revision_guard
BEFORE UPDATE OF current_revision_row_id ON delivery_tickets
FOR EACH ROW WHEN NEW.current_revision_row_id IS NULL OR NOT EXISTS (
    SELECT 1 FROM delivery_ticket_revisions
    WHERE id = NEW.current_revision_row_id
      AND delivery_ticket_row_id = OLD.id
      AND revision_number = COALESCE((SELECT revision_number FROM delivery_ticket_revisions WHERE id = OLD.current_revision_row_id), 0) + 1
)
BEGIN SELECT RAISE(ABORT, 'delivery ticket current revision must advance exactly once'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_delete_guard
BEFORE DELETE ON delivery_tickets
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery tickets are retained authority'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_revision_insert_guard
BEFORE INSERT ON delivery_ticket_revisions
FOR EACH ROW WHEN NOT (
    (NEW.revision_number = 1 AND NEW.replaces_revision_row_id IS NULL AND NOT EXISTS (
        SELECT 1 FROM delivery_tickets WHERE id = NEW.delivery_ticket_row_id AND current_revision_row_id IS NOT NULL
    )) OR
    (NEW.revision_number > 1 AND EXISTS (
        SELECT 1 FROM delivery_ticket_revisions AS prior
        JOIN delivery_tickets AS ticket ON ticket.id = NEW.delivery_ticket_row_id
        WHERE prior.id = NEW.replaces_revision_row_id
          AND prior.delivery_ticket_row_id = NEW.delivery_ticket_row_id
          AND prior.revision_number = NEW.revision_number - 1
          AND ticket.current_revision_row_id = prior.id
    ))
)
BEGIN SELECT RAISE(ABORT, 'delivery ticket revision must replace the current prior revision'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_revision_source_guard
BEFORE INSERT ON delivery_ticket_revisions
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM source_vault_closures AS closure
    JOIN source_vaults AS vault ON vault.id = closure.vault_row_id
    JOIN repository_targets AS target ON target.repo_target = vault.repo_target
    WHERE closure.id = NEW.source_closure_row_id
      AND vault.repo_target = NEW.repo_target
      AND closure.commit_oid = NEW.base_commit
      AND target.configured_branch_ref = 'refs/heads/' || NEW.branch
)
BEGIN SELECT RAISE(ABORT, 'delivery ticket revision source basis does not match repository commit'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_revision_update_immutable
BEFORE UPDATE ON delivery_ticket_revisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket revisions are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_revision_delete_guard
BEFORE DELETE ON delivery_ticket_revisions
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket revisions are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_member_update_immutable
BEFORE UPDATE ON delivery_ticket_revision_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket revision members are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_member_delete_guard
BEFORE DELETE ON delivery_ticket_revision_members
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket revision members are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_dependency_workspace_guard
BEFORE INSERT ON delivery_ticket_revision_dependencies
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM delivery_ticket_revisions AS revision
    JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
    JOIN delivery_ticket_revisions AS dependency ON dependency.id = NEW.depends_on_revision_row_id
    JOIN delivery_tickets AS dependency_ticket ON dependency_ticket.id = dependency.delivery_ticket_row_id
    WHERE revision.id = NEW.revision_row_id AND ticket.workspace_row_id = dependency_ticket.workspace_row_id
)
BEGIN SELECT RAISE(ABORT, 'delivery ticket dependencies must share a workspace'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_dependency_update_immutable
BEFORE UPDATE ON delivery_ticket_revision_dependencies
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket revision dependencies are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_dependency_delete_guard
BEFORE DELETE ON delivery_ticket_revision_dependencies
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket revision dependencies are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_approval_update_immutable
BEFORE UPDATE ON delivery_ticket_revision_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket approvals are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER delivery_ticket_approval_delete_guard
BEFORE DELETE ON delivery_ticket_revision_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket approvals are immutable history'); END;
-- +goose StatementEnd

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
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'delivery ticket selection members are immutable history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_selection_member_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_transition_guard;
DROP TRIGGER IF EXISTS delivery_ticket_selection_identity_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_approval_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_approval_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_dependency_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_dependency_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_dependency_workspace_guard;
DROP TRIGGER IF EXISTS delivery_ticket_member_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_member_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_revision_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_revision_update_immutable;
DROP TRIGGER IF EXISTS delivery_ticket_revision_source_guard;
DROP TRIGGER IF EXISTS delivery_ticket_revision_insert_guard;
DROP TRIGGER IF EXISTS delivery_ticket_delete_guard;
DROP TRIGGER IF EXISTS delivery_ticket_current_revision_guard;
DROP TRIGGER IF EXISTS delivery_ticket_identity_immutable;
DROP INDEX IF EXISTS idx_delivery_ticket_selection_members_selection;
DROP INDEX IF EXISTS idx_delivery_ticket_approvals_revision;
DROP INDEX IF EXISTS idx_delivery_ticket_dependencies_revision;
DROP INDEX IF EXISTS idx_delivery_ticket_members_revision;
DROP INDEX IF EXISTS idx_delivery_ticket_revisions_ticket;
DROP INDEX IF EXISTS idx_delivery_tickets_workspace;
DROP INDEX IF EXISTS idx_delivery_ticket_selections_one_active_workspace;
DROP TABLE IF EXISTS delivery_ticket_selection_members;
DROP TABLE IF EXISTS delivery_ticket_selections;
DROP TABLE IF EXISTS delivery_ticket_revision_approvals;
DROP TABLE IF EXISTS delivery_ticket_revision_dependencies;
DROP TABLE IF EXISTS delivery_ticket_revision_members;
DROP TABLE IF EXISTS delivery_ticket_revisions;
DROP TABLE IF EXISTS delivery_tickets;
