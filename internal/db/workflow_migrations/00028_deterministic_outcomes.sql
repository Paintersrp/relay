-- +goose Up
-- A Run records at most one immutable deterministic preflight/application outcome.
CREATE UNIQUE INDEX idx_artifacts_one_deterministic_outcome_per_run
ON artifacts(run_row_id)
WHERE owner_type = 'run' AND kind = 'deterministic_outcome';

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_one_deterministic_outcome_per_run;
