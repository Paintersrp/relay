-- +goose Up
-- A Run owns at most one immutable execution assignment.
CREATE UNIQUE INDEX idx_artifacts_one_execution_assignment_per_run
ON artifacts(run_row_id)
WHERE owner_type = 'run' AND kind = 'execution_assignment';

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_one_execution_assignment_per_run;
