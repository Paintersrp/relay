-- +goose Up
CREATE TABLE repository_targets (
    repo_target TEXT PRIMARY KEY COLLATE NOCASE,
    local_path TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (repo_target <> '' AND trim(repo_target) = repo_target),
    CHECK (local_path <> '' AND trim(local_path) = local_path)
);

CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (project_id GLOB 'project-*' AND trim(project_id) = project_id),
    CHECK (name <> '' AND trim(name) <> '')
);

CREATE TABLE project_repository_targets (
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    repo_target TEXT NOT NULL COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (project_row_id, repo_target)
);

CREATE TABLE project_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    note_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (note_id GLOB 'note-*' AND trim(note_id) = note_id),
    CHECK (title <> '' AND trim(title) <> ''),
    CHECK (body <> '' AND trim(body) <> '')
);

CREATE TABLE plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    plan_id TEXT NOT NULL UNIQUE,
    feature_slug TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed')),
    canonical_sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at TEXT,
    CHECK (plan_id GLOB 'plan-*' AND trim(plan_id) = plan_id),
    CHECK (feature_slug <> '' AND trim(feature_slug) = feature_slug),
    CHECK (length(canonical_sha256) = 64 AND canonical_sha256 NOT GLOB '*[^0-9a-f]*'),
    CHECK (
        (status = 'active' AND completed_at IS NULL) OR
        (status = 'completed' AND completed_at IS NOT NULL)
    )
);

CREATE TABLE plan_repository_targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_row_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    repo_target TEXT NOT NULL REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    branch TEXT NOT NULL,
    planning_base_commit TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (plan_row_id, sequence),
    UNIQUE (plan_row_id, repo_target),
    CHECK (branch <> '' AND trim(branch) = branch),
    CHECK (length(planning_base_commit) = 40 AND planning_base_commit NOT GLOB '*[^0-9a-f]*')
);

CREATE TABLE plan_passes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pass_id TEXT NOT NULL UNIQUE,
    plan_row_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    pass_number INTEGER NOT NULL CHECK (pass_number >= 1),
    name TEXT NOT NULL,
    repo_target TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'in_progress', 'completed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at TEXT,
    completed_at TEXT,
    UNIQUE (plan_row_id, pass_number),
    UNIQUE (id, plan_row_id, repo_target),
    FOREIGN KEY (plan_row_id, repo_target)
        REFERENCES plan_repository_targets(plan_row_id, repo_target)
        ON DELETE RESTRICT,
    CHECK (pass_id GLOB 'pass-*' AND trim(pass_id) = pass_id),
    CHECK (name <> '' AND trim(name) <> ''),
    CHECK (
        (status = 'planned' AND started_at IS NULL AND completed_at IS NULL) OR
        (status = 'in_progress' AND started_at IS NOT NULL AND completed_at IS NULL) OR
        (status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL)
    )
);

CREATE TABLE plan_pass_dependencies (
    pass_row_id INTEGER NOT NULL REFERENCES plan_passes(id) ON DELETE CASCADE,
    depends_on_pass_row_id INTEGER NOT NULL REFERENCES plan_passes(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (pass_row_id, depends_on_pass_row_id),
    CHECK (pass_row_id <> depends_on_pass_row_id)
);

CREATE TABLE runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL UNIQUE,
    feature_slug TEXT NOT NULL,
    repo_target TEXT NOT NULL REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    plan_row_id INTEGER REFERENCES plans(id) ON DELETE RESTRICT,
    plan_pass_row_id INTEGER REFERENCES plan_passes(id) ON DELETE RESTRICT,
    remediates_run_row_id INTEGER REFERENCES runs(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'created' CHECK (status IN (
        'created',
        'setup_ready',
        'executing',
        'execution_failed',
        'cancelled',
        'validating',
        'validation_failed',
        'audit_ready',
        'needs_revision',
        'completed'
    )),
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL,
    canonical_sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at TEXT,
    FOREIGN KEY (plan_pass_row_id, plan_row_id, repo_target)
        REFERENCES plan_passes(id, plan_row_id, repo_target)
        ON DELETE RESTRICT,
    CHECK (run_id GLOB 'run-*' AND trim(run_id) = run_id),
    CHECK (feature_slug <> '' AND trim(feature_slug) = feature_slug),
    CHECK (branch <> '' AND trim(branch) = branch),
    CHECK (length(base_commit) = 40 AND base_commit NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(canonical_sha256) = 64 AND canonical_sha256 NOT GLOB '*[^0-9a-f]*'),
    CHECK (
        (plan_row_id IS NULL AND plan_pass_row_id IS NULL) OR
        (plan_row_id IS NOT NULL AND plan_pass_row_id IS NOT NULL)
    ),
    CHECK (remediates_run_row_id IS NULL OR remediates_run_row_id <> id),
    CHECK (
        (status = 'completed' AND completed_at IS NOT NULL) OR
        (status <> 'completed' AND completed_at IS NULL)
    )
);

CREATE TABLE execution_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
    adapter TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending',
        'running',
        'succeeded',
        'failed',
        'cancelled',
        'timed_out'
    )),
    result_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at TEXT,
    finished_at TEXT,
    cancellation_requested_at TEXT,
    UNIQUE (run_row_id, attempt_number),
    CHECK (attempt_id GLOB 'attempt-*' AND trim(attempt_id) = attempt_id),
    CHECK (adapter <> '' AND trim(adapter) = adapter),
    CHECK (model <> '' AND trim(model) = model),
    CHECK (json_valid(result_json)),
    CHECK (
        (status = 'pending' AND started_at IS NULL AND finished_at IS NULL) OR
        (status = 'running' AND started_at IS NOT NULL AND finished_at IS NULL) OR
        (status IN ('succeeded', 'failed', 'cancelled', 'timed_out') AND started_at IS NOT NULL AND finished_at IS NOT NULL)
    )
);

CREATE TABLE artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id TEXT NOT NULL UNIQUE,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('plan', 'run', 'execution_attempt')),
    plan_row_id INTEGER REFERENCES plans(id) ON DELETE CASCADE,
    run_row_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
    execution_attempt_row_id INTEGER REFERENCES execution_attempts(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    relative_path TEXT NOT NULL UNIQUE,
    media_type TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (artifact_id GLOB 'artifact-*' AND trim(artifact_id) = artifact_id),
    CHECK (kind <> '' AND trim(kind) = kind),
    CHECK (relative_path <> '' AND trim(relative_path) = relative_path),
    CHECK (media_type <> '' AND trim(media_type) = media_type),
    CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    CHECK (
        (CASE WHEN plan_row_id IS NOT NULL THEN 1 ELSE 0 END) +
        (CASE WHEN run_row_id IS NOT NULL THEN 1 ELSE 0 END) +
        (CASE WHEN execution_attempt_row_id IS NOT NULL THEN 1 ELSE 0 END) = 1
    ),
    CHECK (
        (owner_type = 'plan' AND plan_row_id IS NOT NULL) OR
        (owner_type = 'run' AND run_row_id IS NOT NULL) OR
        (owner_type = 'execution_attempt' AND execution_attempt_row_id IS NOT NULL)
    )
);

CREATE TABLE audit_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_decision_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
    audit_packet_artifact_row_id INTEGER NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
    audited_commit TEXT NOT NULL,
    packet_sha256 TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('accepted', 'needs_revision')),
    rationale TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (run_row_id, packet_sha256),
    CHECK (audit_decision_id GLOB 'audit-*' AND trim(audit_decision_id) = audit_decision_id),
    CHECK (length(audited_commit) = 40 AND audited_commit NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(packet_sha256) = 64 AND packet_sha256 NOT GLOB '*[^0-9a-f]*')
);

-- +goose StatementBegin
CREATE TRIGGER plan_pass_dependencies_same_plan_and_order
BEFORE INSERT ON plan_pass_dependencies
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM plan_passes AS pass
    JOIN plan_passes AS dependency ON dependency.id = NEW.depends_on_pass_row_id
    WHERE pass.id = NEW.pass_row_id
      AND dependency.plan_row_id = pass.plan_row_id
      AND dependency.pass_number < pass.pass_number
)
BEGIN
    SELECT RAISE(ABORT, 'dependency must reference an earlier pass in the same plan');
END;
-- +goose StatementEnd


-- +goose StatementBegin
CREATE TRIGGER project_delete_guard
BEFORE DELETE ON projects
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'Projects cannot be deleted; archive the Project instead');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER plan_project_insert_guard
BEFORE INSERT ON plans
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM projects WHERE id = NEW.project_row_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'new Plans require an active Project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER plan_project_move_guard
BEFORE UPDATE OF project_row_id ON plans
FOR EACH ROW
WHEN NEW.project_row_id <> OLD.project_row_id AND NOT EXISTS (
    SELECT 1 FROM projects WHERE id = NEW.project_row_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'Plans may move only to an active Project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER plan_initial_status_guard
BEFORE INSERT ON plans
FOR EACH ROW
WHEN NEW.status <> 'active'
BEGIN
    SELECT RAISE(ABORT, 'new Plans must start active');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER pass_initial_status_guard
BEFORE INSERT ON plan_passes
FOR EACH ROW
WHEN NEW.status <> 'planned'
BEGIN
    SELECT RAISE(ABORT, 'new passes must start planned');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER plan_status_transition_guard
BEFORE UPDATE OF status ON plans
FOR EACH ROW
WHEN NEW.status <> OLD.status AND (
    OLD.status <> 'active' OR
    NEW.status <> 'completed' OR
    NOT EXISTS (SELECT 1 FROM plan_passes WHERE plan_row_id = OLD.id) OR
    EXISTS (SELECT 1 FROM plan_passes WHERE plan_row_id = OLD.id AND status <> 'completed')
)
BEGIN
    SELECT RAISE(ABORT, 'invalid plan status transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER pass_status_transition_guard
BEFORE UPDATE OF status ON plan_passes
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'planned' AND NEW.status = 'in_progress'
        AND EXISTS (SELECT 1 FROM plans WHERE id = OLD.plan_row_id AND status = 'active')
        AND NOT EXISTS (
            SELECT 1
            FROM plan_pass_dependencies AS dependency
            JOIN plan_passes AS required_pass ON required_pass.id = dependency.depends_on_pass_row_id
            WHERE dependency.pass_row_id = OLD.id AND required_pass.status <> 'completed'
        )) OR
    (OLD.status = 'in_progress' AND NEW.status = 'completed')
)
BEGIN
    SELECT RAISE(ABORT, 'invalid pass status transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER run_initial_status_guard
BEFORE INSERT ON runs
FOR EACH ROW
WHEN NEW.status <> 'created'
BEGIN
    SELECT RAISE(ABORT, 'new runs must start in created');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER managed_run_requires_active_plan
BEFORE INSERT ON runs
FOR EACH ROW
WHEN NEW.plan_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM plans
    JOIN plan_passes ON plan_passes.id = NEW.plan_pass_row_id
    WHERE plans.id = NEW.plan_row_id
      AND plans.status = 'active'
      AND plan_passes.plan_row_id = plans.id
      AND plan_passes.repo_target = NEW.repo_target
      AND plan_passes.status = 'in_progress'
)
BEGIN
    SELECT RAISE(ABORT, 'managed run requires an active Plan and in-progress pass');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER remediation_run_guard
BEFORE INSERT ON runs
FOR EACH ROW
WHEN NEW.remediates_run_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM runs AS original
    WHERE original.id = NEW.remediates_run_row_id
      AND original.status = 'needs_revision'
      AND original.repo_target = NEW.repo_target
      AND original.plan_row_id IS NEW.plan_row_id
      AND original.plan_pass_row_id IS NEW.plan_pass_row_id
)
BEGIN
    SELECT RAISE(ABORT, 'remediation run must match a needs_revision run');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER run_status_transition_guard
BEFORE UPDATE OF status ON runs
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'created' AND NEW.status = 'setup_ready') OR
    (OLD.status = 'setup_ready' AND NEW.status IN ('executing', 'cancelled')) OR
    (OLD.status = 'executing' AND NEW.status IN ('execution_failed', 'cancelled', 'validating')) OR
    (OLD.status = 'execution_failed' AND NEW.status = 'validating') OR
    (OLD.status = 'validating' AND NEW.status IN ('validation_failed', 'audit_ready')) OR
    (OLD.status = 'validation_failed' AND NEW.status = 'needs_revision') OR
    (OLD.status = 'audit_ready' AND NEW.status IN ('needs_revision', 'completed'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid run status transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER execution_attempt_initial_status_guard
BEFORE INSERT ON execution_attempts
FOR EACH ROW
WHEN NEW.status <> 'pending'
BEGIN
    SELECT RAISE(ABORT, 'new execution attempts must start pending');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER execution_attempt_insert_guard
BEFORE INSERT ON execution_attempts
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM runs WHERE id = NEW.run_row_id AND status = 'executing'
) OR NEW.attempt_number <> COALESCE((
    SELECT MAX(attempt_number) + 1 FROM execution_attempts WHERE run_row_id = NEW.run_row_id
), 1)
BEGIN
    SELECT RAISE(ABORT, 'execution attempt requires an executing run and next attempt number');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER execution_attempt_status_transition_guard
BEFORE UPDATE OF status ON execution_attempts
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'pending' AND NEW.status IN ('running', 'cancelled')) OR
    (OLD.status = 'running' AND NEW.status IN ('succeeded', 'failed', 'cancelled', 'timed_out'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid execution attempt status transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_decision_guard
BEFORE INSERT ON audit_decisions
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM runs
    JOIN artifacts ON artifacts.id = NEW.audit_packet_artifact_row_id
    WHERE runs.id = NEW.run_row_id
      AND runs.status = 'audit_ready'
      AND artifacts.owner_type = 'run'
      AND artifacts.run_row_id = runs.id
      AND artifacts.kind = 'audit_packet'
      AND artifacts.sha256 = NEW.packet_sha256
)
BEGIN
    SELECT RAISE(ABORT, 'audit decision requires the matching run-owned audit packet');
END;
-- +goose StatementEnd

CREATE INDEX idx_projects_status ON projects(status, id);
CREATE INDEX idx_project_repository_targets_project ON project_repository_targets(project_row_id, repo_target);
CREATE INDEX idx_project_notes_project ON project_notes(project_row_id, status, id);
CREATE INDEX idx_plans_project ON plans(project_row_id, id);
CREATE INDEX idx_plan_repository_targets_plan ON plan_repository_targets(plan_row_id, sequence);
CREATE INDEX idx_plan_passes_plan ON plan_passes(plan_row_id, pass_number);
CREATE INDEX idx_plan_passes_status ON plan_passes(status);
CREATE INDEX idx_plan_pass_dependencies_pass ON plan_pass_dependencies(pass_row_id);
CREATE INDEX idx_runs_plan_pass ON runs(plan_pass_row_id, created_at);
CREATE INDEX idx_runs_remediates ON runs(remediates_run_row_id);
CREATE INDEX idx_runs_status ON runs(status);
CREATE INDEX idx_execution_attempts_run ON execution_attempts(run_row_id, attempt_number);
CREATE INDEX idx_artifacts_plan ON artifacts(plan_row_id, created_at);
CREATE INDEX idx_artifacts_run ON artifacts(run_row_id, created_at);
CREATE INDEX idx_artifacts_attempt ON artifacts(execution_attempt_row_id, created_at);
CREATE INDEX idx_audit_decisions_run ON audit_decisions(run_row_id, created_at);

-- +goose Down
DROP TRIGGER IF EXISTS plan_project_move_guard;
DROP TRIGGER IF EXISTS plan_project_insert_guard;
DROP TRIGGER IF EXISTS project_delete_guard;
DROP TRIGGER IF EXISTS audit_decision_guard;
DROP TRIGGER IF EXISTS execution_attempt_status_transition_guard;
DROP TRIGGER IF EXISTS execution_attempt_insert_guard;
DROP TRIGGER IF EXISTS execution_attempt_initial_status_guard;
DROP TRIGGER IF EXISTS run_status_transition_guard;
DROP TRIGGER IF EXISTS remediation_run_guard;
DROP TRIGGER IF EXISTS managed_run_requires_active_plan;
DROP TRIGGER IF EXISTS run_initial_status_guard;
DROP TRIGGER IF EXISTS pass_status_transition_guard;
DROP TRIGGER IF EXISTS plan_status_transition_guard;
DROP TRIGGER IF EXISTS pass_initial_status_guard;
DROP TRIGGER IF EXISTS plan_initial_status_guard;
DROP TRIGGER IF EXISTS plan_pass_dependencies_same_plan_and_order;
DROP TABLE IF EXISTS audit_decisions;
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS execution_attempts;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS plan_pass_dependencies;
DROP TABLE IF EXISTS plan_passes;
DROP TABLE IF EXISTS plan_repository_targets;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS project_notes;
DROP TABLE IF EXISTS project_repository_targets;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS repository_targets;
