-- +goose Up
ALTER TABLE runs ADD COLUMN plan_row_id INTEGER REFERENCES plans(id) ON DELETE SET NULL;
ALTER TABLE runs ADD COLUMN plan_pass_row_id INTEGER REFERENCES plan_passes(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_runs_plan_row_id ON runs(plan_row_id);
CREATE INDEX IF NOT EXISTS idx_runs_plan_pass_row_id ON runs(plan_pass_row_id);

-- +goose Down
DROP INDEX IF EXISTS idx_runs_plan_pass_row_id;
DROP INDEX IF EXISTS idx_runs_plan_row_id;
ALTER TABLE runs DROP COLUMN plan_pass_row_id;
ALTER TABLE runs DROP COLUMN plan_row_id;
