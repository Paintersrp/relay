-- +goose Up
CREATE TABLE program_prepared_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    prepared_member_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    execution_package_row_id INTEGER NOT NULL UNIQUE REFERENCES execution_packages(id) ON DELETE RESTRICT,
    run_row_id INTEGER NOT NULL UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    ticket_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    assignment_artifact_row_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
    repo_target TEXT NOT NULL,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('prepared', 'cancelled', 'dispatched')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (prepared_member_id GLOB 'program-member-*' AND trim(prepared_member_id) = prepared_member_id)
);
CREATE INDEX idx_program_prepared_members_workspace ON program_prepared_members(workspace_row_id, state, id);

CREATE TABLE program_dispatches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    dispatch_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    repo_target TEXT NOT NULL,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'dispatched' CHECK (status IN ('dispatched', 'reported')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (dispatch_id GLOB 'dispatch-*' AND trim(dispatch_id) = dispatch_id)
);
CREATE TABLE program_dispatch_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    dispatch_row_id INTEGER NOT NULL REFERENCES program_dispatches(id) ON DELETE RESTRICT,
    prepared_member_row_id INTEGER NOT NULL UNIQUE REFERENCES program_prepared_members(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    UNIQUE (dispatch_row_id, sequence)
);
CREATE TABLE program_dispatch_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    dispatch_member_row_id INTEGER NOT NULL UNIQUE REFERENCES program_dispatch_members(id) ON DELETE RESTRICT,
    outcome TEXT NOT NULL CHECK (outcome IN ('done', 'blocked')),
    branch TEXT,
    branch_head_sha TEXT,
    blocker TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK ((outcome = 'done' AND branch <> '' AND trim(branch) = branch AND length(branch_head_sha) = 40 AND branch_head_sha NOT GLOB '*[^0-9a-f]*' AND blocker IS NULL) OR
           (outcome = 'blocked' AND branch IS NULL AND branch_head_sha IS NULL AND blocker <> '' AND trim(blocker) = blocker))
);
CREATE TABLE program_execution_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    dispatch_row_id INTEGER NOT NULL UNIQUE REFERENCES program_dispatches(id) ON DELETE RESTRICT,
    later_integration_risks TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (later_integration_risks = trim(later_integration_risks))
);

-- +goose StatementBegin
CREATE TRIGGER program_prepared_member_identity_immutable BEFORE UPDATE OF prepared_member_id, workspace_row_id, execution_package_row_id, run_row_id, ticket_revision_row_id, assignment_artifact_row_id, repo_target, branch, base_commit, created_at ON program_prepared_members FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program prepared member identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_prepared_member_transition_guard BEFORE UPDATE OF state ON program_prepared_members FOR EACH ROW WHEN NOT (OLD.state = 'prepared' AND NEW.state IN ('cancelled', 'dispatched')) BEGIN SELECT RAISE(ABORT, 'invalid program prepared member transition'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_prepared_member_delete_guard BEFORE DELETE ON program_prepared_members FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program prepared members are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_dispatch_identity_immutable BEFORE UPDATE ON program_dispatches FOR EACH ROW WHEN NOT (OLD.status = 'dispatched' AND NEW.status = 'reported' AND OLD.dispatch_id = NEW.dispatch_id AND OLD.workspace_row_id = NEW.workspace_row_id AND OLD.repo_target = NEW.repo_target AND OLD.branch = NEW.branch AND OLD.base_commit = NEW.base_commit AND OLD.created_at = NEW.created_at) BEGIN SELECT RAISE(ABORT, 'program dispatch identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_dispatch_delete_guard BEFORE DELETE ON program_dispatches FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program dispatches are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_dispatch_member_update_immutable BEFORE UPDATE ON program_dispatch_members FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program dispatch members are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_dispatch_member_delete_guard BEFORE DELETE ON program_dispatch_members FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program dispatch members are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_dispatch_result_update_immutable BEFORE UPDATE ON program_dispatch_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program dispatch results are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_dispatch_result_delete_guard BEFORE DELETE ON program_dispatch_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program dispatch results are retained history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_execution_result_update_immutable BEFORE UPDATE ON program_execution_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program execution results are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER program_execution_result_delete_guard BEFORE DELETE ON program_execution_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'program execution results are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS program_execution_results;
DROP TABLE IF EXISTS program_dispatch_results;
DROP TABLE IF EXISTS program_dispatch_members;
DROP TABLE IF EXISTS program_dispatches;
DROP TABLE IF EXISTS program_prepared_members;
