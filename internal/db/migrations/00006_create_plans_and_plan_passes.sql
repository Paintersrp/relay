-- +goose Up
CREATE TABLE plans (
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
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE plan_passes (
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
    UNIQUE(plan_row_id, pass_id),
    UNIQUE(plan_row_id, sequence)
);

CREATE INDEX idx_plans_status ON plans(status);
CREATE INDEX idx_plan_passes_plan_row_id ON plan_passes(plan_row_id);
CREATE INDEX idx_plan_passes_status ON plan_passes(status);

-- +goose Down
DROP TABLE IF EXISTS plan_passes;
DROP TABLE IF EXISTS plans;
