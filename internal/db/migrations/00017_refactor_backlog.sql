-- +goose Up

-- Refactor discovery tasks are project-scoped analysis prompts, not pass-ready candidates.
CREATE TABLE refactor_discovery_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    prompt TEXT NOT NULL,
    target_scope_json TEXT NOT NULL,
    priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'completed', 'closed', 'superseded')),
    tags_json TEXT NOT NULL DEFAULT '[]',
    created_from TEXT NOT NULL DEFAULT 'manual',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    closed_reason TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL DEFAULT '',
    closed_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_row_id, task_id),
    CHECK (json_valid(tags_json) AND json_type(tags_json) = 'array'),
    CHECK (json_valid(metadata_json) AND json_type(metadata_json) = 'object'),
    CHECK (
        json_valid(target_scope_json)
        AND json_type(target_scope_json) = 'object'
        AND json_extract(target_scope_json, '$.kind') IN ('repository', 'subsystem', 'directory', 'file_set', 'plan', 'pass')
        AND json_type(target_scope_json, '$.values') = 'array'
        AND json_array_length(target_scope_json, '$.values') >= 1
    )
);

-- Refactor candidates are pass-shaped backlog entries. Pass-ready fields are enforced
-- at the store boundary and, where practical, with DB-level CHECK constraints.
CREATE TABLE refactor_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    candidate_id TEXT NOT NULL,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    problem_summary TEXT NOT NULL,
    current_behavior TEXT NOT NULL DEFAULT '',
    desired_behavior TEXT NOT NULL,
    rationale TEXT NOT NULL,
    proposed_pass_name TEXT NOT NULL,
    proposed_pass_goal TEXT NOT NULL,
    proposed_pass_scope_json TEXT NOT NULL,
    proposed_non_goals_json TEXT NOT NULL,
    target_files_json TEXT NOT NULL,
    validation_commands_json TEXT NOT NULL,
    audit_focus_json TEXT NOT NULL,
    constraints_json TEXT NOT NULL DEFAULT '[]',
    risk_level TEXT NOT NULL CHECK (risk_level IN ('low', 'medium', 'high')),
    status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'scheduled', 'scheduled_revision_required', 'completed', 'completed_with_warnings', 'deferred', 'rejected', 'superseded')),
    dependency_notes TEXT NOT NULL DEFAULT '',
    defer_reason TEXT NOT NULL DEFAULT '',
    deferred_until TEXT NOT NULL DEFAULT '',
    rejected_reason TEXT NOT NULL DEFAULT '',
    superseded_by_candidate_id TEXT NOT NULL DEFAULT '',
    superseded_reason TEXT NOT NULL DEFAULT '',
    scheduled_at TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_row_id, candidate_id),
    CHECK (json_valid(proposed_pass_scope_json) AND json_type(proposed_pass_scope_json) = 'array' AND json_array_length(proposed_pass_scope_json) >= 1),
    CHECK (json_valid(proposed_non_goals_json) AND json_type(proposed_non_goals_json) = 'array' AND json_array_length(proposed_non_goals_json) >= 1),
    CHECK (json_valid(target_files_json) AND json_type(target_files_json) = 'array' AND json_array_length(target_files_json) >= 1),
    CHECK (json_valid(validation_commands_json) AND json_type(validation_commands_json) = 'array' AND json_array_length(validation_commands_json) >= 1),
    CHECK (json_valid(audit_focus_json) AND json_type(audit_focus_json) = 'array' AND json_array_length(audit_focus_json) >= 1),
    CHECK (json_valid(constraints_json) AND json_type(constraints_json) = 'array'),
    CHECK (json_valid(metadata_json) AND json_type(metadata_json) = 'object')
);

-- Candidate-to-discovery provenance links.
CREATE TABLE refactor_candidate_discovery_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    link_id TEXT NOT NULL,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    candidate_row_id INTEGER NOT NULL REFERENCES refactor_candidates(id) ON DELETE CASCADE,
    discovery_task_row_id INTEGER NOT NULL REFERENCES refactor_discovery_tasks(id) ON DELETE CASCADE,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_row_id, link_id),
    UNIQUE(candidate_row_id, discovery_task_row_id)
);

-- Candidate dependency links. Persisted but not a planning/promotion engine in this pass.
CREATE TABLE refactor_candidate_dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    dependency_id TEXT NOT NULL,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    candidate_row_id INTEGER NOT NULL REFERENCES refactor_candidates(id) ON DELETE CASCADE,
    depends_on_candidate_row_id INTEGER NOT NULL REFERENCES refactor_candidates(id) ON DELETE CASCADE,
    dependency_type TEXT NOT NULL DEFAULT 'blocks',
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_row_id, dependency_id),
    UNIQUE(candidate_row_id, depends_on_candidate_row_id),
    CHECK (candidate_row_id != depends_on_candidate_row_id)
);

-- Candidate scheduling references. Row id references are nullable so later passes can
-- detect stale references without requiring plan/pass/run records to exist in this pass.
CREATE TABLE refactor_candidate_schedule_refs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    schedule_ref_id TEXT NOT NULL,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    candidate_row_id INTEGER NOT NULL REFERENCES refactor_candidates(id) ON DELETE CASCADE,
    schedule_kind TEXT NOT NULL CHECK (schedule_kind IN ('existing_plan_bonus_pass', 'generated_refactor_only_plan')),
    status TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'stale', 'completed', 'cancelled')),
    plan_row_id INTEGER,
    plan_pass_row_id INTEGER,
    run_row_id INTEGER,
    plan_id TEXT NOT NULL DEFAULT '',
    pass_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_row_id, schedule_ref_id)
);

-- Append-only candidate status/audit-relevant metadata events.
CREATE TABLE refactor_candidate_status_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    candidate_row_id INTEGER NOT NULL REFERENCES refactor_candidates(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN ('created', 'updated', 'deferred', 'rejected', 'superseded', 'scheduled', 'completed', 'completed_with_warnings', 'scheduled_revision_required', 'reopened')),
    from_status TEXT NOT NULL DEFAULT '',
    to_status TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    detail_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_row_id, event_id),
    CHECK (json_valid(detail_json) AND json_type(detail_json) = 'object')
);

CREATE INDEX idx_refactor_discovery_tasks_project_status
    ON refactor_discovery_tasks(project_row_id, status, updated_at DESC, id DESC);

CREATE INDEX idx_refactor_candidates_project_status
    ON refactor_candidates(project_row_id, status, updated_at DESC, id DESC);

CREATE INDEX idx_refactor_candidates_project_risk
    ON refactor_candidates(project_row_id, risk_level, updated_at DESC, id DESC);

CREATE INDEX idx_refactor_candidate_discovery_links_project_candidate
    ON refactor_candidate_discovery_links(project_row_id, candidate_row_id);

CREATE INDEX idx_refactor_candidate_discovery_links_project_task
    ON refactor_candidate_discovery_links(project_row_id, discovery_task_row_id);

CREATE INDEX idx_refactor_candidate_dependencies_project_candidate
    ON refactor_candidate_dependencies(project_row_id, candidate_row_id);

CREATE INDEX idx_refactor_candidate_schedule_refs_project_candidate_status
    ON refactor_candidate_schedule_refs(project_row_id, candidate_row_id, status);

CREATE INDEX idx_refactor_candidate_schedule_refs_project_plan_pass
    ON refactor_candidate_schedule_refs(project_row_id, plan_id, pass_id);

CREATE INDEX idx_refactor_candidate_status_events_project_candidate_created
    ON refactor_candidate_status_events(project_row_id, candidate_row_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_refactor_candidate_status_events_project_candidate_created;
DROP INDEX IF EXISTS idx_refactor_candidate_schedule_refs_project_plan_pass;
DROP INDEX IF EXISTS idx_refactor_candidate_schedule_refs_project_candidate_status;
DROP INDEX IF EXISTS idx_refactor_candidate_dependencies_project_candidate;
DROP INDEX IF EXISTS idx_refactor_candidate_discovery_links_project_task;
DROP INDEX IF EXISTS idx_refactor_candidate_discovery_links_project_candidate;
DROP INDEX IF EXISTS idx_refactor_candidates_project_risk;
DROP INDEX IF EXISTS idx_refactor_candidates_project_status;
DROP INDEX IF EXISTS idx_refactor_discovery_tasks_project_status;
DROP TABLE IF EXISTS refactor_candidate_status_events;
DROP TABLE IF EXISTS refactor_candidate_schedule_refs;
DROP TABLE IF EXISTS refactor_candidate_dependencies;
DROP TABLE IF EXISTS refactor_candidate_discovery_links;
DROP TABLE IF EXISTS refactor_candidates;
DROP TABLE IF EXISTS refactor_discovery_tasks;
