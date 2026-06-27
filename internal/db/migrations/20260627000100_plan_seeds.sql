-- +goose Up
CREATE TABLE IF NOT EXISTS plan_seeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    seed_id TEXT NOT NULL,
    project_row_id INTEGER NOT NULL,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    quick_context TEXT NOT NULL,
    constraints_json TEXT NOT NULL DEFAULT '[]',
    non_goals_json TEXT NOT NULL DEFAULT '[]',
    tags_json TEXT NOT NULL DEFAULT '[]',
    priority TEXT NOT NULL DEFAULT 'normal',
    status TEXT NOT NULL DEFAULT 'captured',
    source_type TEXT NOT NULL,
    source_label TEXT NOT NULL DEFAULT '',
    source_ref_id TEXT NOT NULL DEFAULT '',
    plan_attempt_id TEXT NOT NULL DEFAULT '',
    managed_plan_id TEXT NOT NULL DEFAULT '',
    planned_at TEXT NOT NULL DEFAULT '',
    defer_reason TEXT NOT NULL DEFAULT '',
    reject_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (project_row_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_row_id, seed_id),
    CHECK (status IN ('captured', 'planned', 'deferred', 'rejected')),
    CHECK (source_type IN ('manual', 'chat', 'mcp'))
);

CREATE INDEX IF NOT EXISTS idx_plan_seeds_project_status
    ON plan_seeds(project_row_id, status, updated_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_plan_seeds_project_updated
    ON plan_seeds(project_row_id, updated_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_plan_seeds_plan_attempt
    ON plan_seeds(project_row_id, plan_attempt_id);

CREATE INDEX IF NOT EXISTS idx_plan_seeds_managed_plan
    ON plan_seeds(project_row_id, managed_plan_id);

-- +goose Down
DROP TABLE IF EXISTS plan_seeds;
