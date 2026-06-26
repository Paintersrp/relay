-- +goose Up
CREATE TABLE IF NOT EXISTS plan_review_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_row_id INTEGER NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    drift_review_mode TEXT NOT NULL DEFAULT 'manual' CHECK (drift_review_mode IN ('disabled', 'manual', 'automatic', 'external')),
    model_tier TEXT NOT NULL DEFAULT 'standard' CHECK (model_tier IN ('economy', 'standard', 'high_assurance', 'auto_escalate')),
    manual_model_call_warning TEXT NOT NULL DEFAULT 'Running an internal drift review may call a configured model provider. Confirm before continuing.',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_plan_review_settings_project_id ON plan_review_settings(project_id);

-- +goose Down
DROP TABLE IF EXISTS plan_review_settings;
