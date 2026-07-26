-- +goose Up
-- An adaptive Run owns at most one immutable effective Executor Brief.
CREATE UNIQUE INDEX idx_artifacts_one_effective_executor_brief_per_run
ON artifacts(run_row_id)
WHERE owner_type = 'run' AND kind = 'effective_executor_brief';

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_one_effective_executor_brief_per_run;
