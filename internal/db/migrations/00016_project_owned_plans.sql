-- +goose Up
PRAGMA foreign_keys = OFF;

-- Insert fallback legacy project ONLY when existing plans need it
INSERT INTO projects (project_id, name, description, status, default_repository_id)
SELECT 'legacy-default', 'Legacy Default', 'Backfilled project for managed plans that existed before project-owned plan persistence.', 'active', ''
WHERE EXISTS (
    SELECT 1 FROM plans WHERE COALESCE(
        NULLIF(json_extract(project_context_json, '$.primary_project'), ''),
        NULLIF(json_extract(plan_meta_json, '$.project_id'), ''),
        NULLIF(json_extract(plan_meta_json, '$.project_context.primary_project'), '')
    ) IS NULL OR COALESCE(
        NULLIF(json_extract(project_context_json, '$.primary_project'), ''),
        NULLIF(json_extract(plan_meta_json, '$.project_id'), ''),
        NULLIF(json_extract(plan_meta_json, '$.project_context.primary_project'), '')
    ) NOT IN (SELECT project_id FROM projects)
);

-- Rebuild plans table with non-nullable project ownership fields
CREATE TABLE plans_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id TEXT NOT NULL UNIQUE,
    schema_version TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    goal TEXT NOT NULL DEFAULT '',
    repo_target TEXT NOT NULL DEFAULT '',
    branch_context TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'complete', 'abandoned')),
    source_intent_summary TEXT NOT NULL DEFAULT '',
    source_artifact_path TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    plan_meta_json TEXT NOT NULL DEFAULT '{}',
    project_context_json TEXT NOT NULL DEFAULT '{}',
    mcp_capability_profile_json TEXT NOT NULL DEFAULT '{}',
    global_context_rules_json TEXT NOT NULL DEFAULT '{}',
    submission_note TEXT NOT NULL DEFAULT '',
    raw_plan_json TEXT NOT NULL DEFAULT '',
    project_row_id INTEGER NOT NULL REFERENCES projects(id),
    project_id TEXT NOT NULL
);

-- Backfill data using the legacy resolution order
INSERT INTO plans_new (
    id, plan_id, schema_version, title, goal, repo_target, branch_context, status,
    source_intent_summary, source_artifact_path, created_at, updated_at,
    plan_meta_json, project_context_json, mcp_capability_profile_json, global_context_rules_json,
    submission_note, raw_plan_json,
    project_id,
    project_row_id
)
SELECT
    id, plan_id, schema_version, title, goal, repo_target, branch_context, status,
    source_intent_summary, source_artifact_path, created_at, updated_at,
    plan_meta_json, project_context_json, mcp_capability_profile_json, global_context_rules_json,
    submission_note, raw_plan_json,
    COALESCE(
      (SELECT p.project_id FROM projects p WHERE p.project_id = COALESCE(
          NULLIF(json_extract(project_context_json, '$.primary_project'), ''),
          NULLIF(json_extract(plan_meta_json, '$.project_id'), ''),
          NULLIF(json_extract(plan_meta_json, '$.project_context.primary_project'), '')
      )),
      'legacy-default'
    ) AS project_id,
    COALESCE(
      (SELECT p.id FROM projects p WHERE p.project_id = COALESCE(
          NULLIF(json_extract(project_context_json, '$.primary_project'), ''),
          NULLIF(json_extract(plan_meta_json, '$.project_id'), ''),
          NULLIF(json_extract(plan_meta_json, '$.project_context.primary_project'), '')
      )),
      (SELECT id FROM projects WHERE project_id = 'legacy-default')
    ) AS project_row_id
FROM plans;

DROP TABLE plans;
ALTER TABLE plans_new RENAME TO plans;

-- Rebuild plan_passes table to support expanded status values in CHECK constraint
CREATE TABLE plan_passes_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_row_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    pass_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    name TEXT NOT NULL DEFAULT '',
    goal TEXT NOT NULL DEFAULT '',
    intended_execution_scope_json TEXT NOT NULL DEFAULT '[]',
    non_goals_json TEXT NOT NULL DEFAULT '[]',
    dependencies_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'ready_for_planner', 'handoff_ready', 'run_created', 'in_progress', 'audit_ready', 'completed', 'revision_required', 'blocked', 'skipped')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    pass_type TEXT NOT NULL DEFAULT '',
    context_plan_json TEXT NOT NULL DEFAULT '{}',
    source_snapshot_requirements_json TEXT NOT NULL DEFAULT '{}',
    handoff_readiness_criteria_json TEXT NOT NULL DEFAULT '[]',
    risk_level TEXT NOT NULL DEFAULT '',
    context_budget_json TEXT NOT NULL DEFAULT '{}',
    raw_pass_json TEXT NOT NULL DEFAULT '{}',
    UNIQUE(plan_row_id, pass_id),
    UNIQUE(plan_row_id, sequence)
);

INSERT INTO plan_passes_new (
    id, plan_row_id, pass_id, sequence, name, goal,
    intended_execution_scope_json, non_goals_json, dependencies_json,
    status, created_at, updated_at, pass_type, context_plan_json,
    source_snapshot_requirements_json, handoff_readiness_criteria_json,
    risk_level, context_budget_json, raw_pass_json
)
SELECT
    id, plan_row_id, pass_id, sequence, name, goal,
    intended_execution_scope_json, non_goals_json, dependencies_json,
    status, created_at, updated_at, pass_type, context_plan_json,
    source_snapshot_requirements_json, handoff_readiness_criteria_json,
    risk_level, context_budget_json, raw_pass_json
FROM plan_passes;

DROP TABLE plan_passes;
ALTER TABLE plan_passes_new RENAME TO plan_passes;

-- Recreate index on plans status
CREATE INDEX idx_plans_status ON plans(status);

-- Create new project-scoped indexes on plans
CREATE INDEX idx_plans_project_row_id ON plans(project_row_id);
CREATE INDEX idx_plans_project_id ON plans(project_id);
CREATE INDEX idx_plans_project_row_id_plan_id ON plans(project_row_id, plan_id);

-- Recreate indexes on plan_passes
CREATE INDEX idx_plan_passes_plan_row_id ON plan_passes(plan_row_id);
CREATE INDEX idx_plan_passes_status ON plan_passes(status);
CREATE INDEX idx_plan_passes_plan_row_id_status ON plan_passes(plan_row_id, status);

PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;

CREATE TABLE plans_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id TEXT NOT NULL UNIQUE,
    schema_version TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    goal TEXT NOT NULL DEFAULT '',
    repo_target TEXT NOT NULL DEFAULT '',
    branch_context TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'complete', 'abandoned')),
    source_intent_summary TEXT NOT NULL DEFAULT '',
    source_artifact_path TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    plan_meta_json TEXT NOT NULL DEFAULT '{}',
    project_context_json TEXT NOT NULL DEFAULT '{}',
    mcp_capability_profile_json TEXT NOT NULL DEFAULT '{}',
    global_context_rules_json TEXT NOT NULL DEFAULT '{}',
    submission_note TEXT NOT NULL DEFAULT '',
    raw_plan_json TEXT NOT NULL DEFAULT ''
);

INSERT INTO plans_old (
    id, plan_id, schema_version, title, goal, repo_target, branch_context, status,
    source_intent_summary, source_artifact_path, created_at, updated_at,
    plan_meta_json, project_context_json, mcp_capability_profile_json, global_context_rules_json,
    submission_note, raw_plan_json
)
SELECT
    id, plan_id, schema_version, title, goal, repo_target, branch_context, status,
    source_intent_summary, source_artifact_path, created_at, updated_at,
    plan_meta_json, project_context_json, mcp_capability_profile_json, global_context_rules_json,
    submission_note, raw_plan_json
FROM plans;

DROP TABLE plans;
ALTER TABLE plans_old RENAME TO plans;

CREATE TABLE plan_passes_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_row_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    pass_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    name TEXT NOT NULL DEFAULT '',
    goal TEXT NOT NULL DEFAULT '',
    intended_execution_scope_json TEXT NOT NULL DEFAULT '[]',
    non_goals_json TEXT NOT NULL DEFAULT '[]',
    dependencies_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'in_progress', 'completed', 'skipped')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    pass_type TEXT NOT NULL DEFAULT '',
    context_plan_json TEXT NOT NULL DEFAULT '{}',
    source_snapshot_requirements_json TEXT NOT NULL DEFAULT '{}',
    handoff_readiness_criteria_json TEXT NOT NULL DEFAULT '[]',
    risk_level TEXT NOT NULL DEFAULT '',
    context_budget_json TEXT NOT NULL DEFAULT '{}',
    raw_pass_json TEXT NOT NULL DEFAULT '{}',
    UNIQUE(plan_row_id, pass_id),
    UNIQUE(plan_row_id, sequence)
);

INSERT INTO plan_passes_old (
    id, plan_row_id, pass_id, sequence, name, goal,
    intended_execution_scope_json, non_goals_json, dependencies_json,
    status, created_at, updated_at, pass_type, context_plan_json,
    source_snapshot_requirements_json, handoff_readiness_criteria_json,
    risk_level, context_budget_json, raw_pass_json
)
SELECT
    id, plan_row_id, pass_id, sequence, name, goal,
    intended_execution_scope_json, non_goals_json, dependencies_json,
    status, created_at, updated_at, pass_type, context_plan_json,
    source_snapshot_requirements_json, handoff_readiness_criteria_json,
    risk_level, context_budget_json, raw_pass_json
FROM plan_passes;

DROP TABLE plan_passes;
ALTER TABLE plan_passes_old RENAME TO plan_passes;

CREATE INDEX idx_plans_status ON plans(status);
CREATE INDEX idx_plan_passes_plan_row_id ON plan_passes(plan_row_id);
CREATE INDEX idx_plan_passes_status ON plan_passes(status);

PRAGMA foreign_keys = ON;
