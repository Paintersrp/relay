-- +goose Up
-- A package-linked Run is dispatched exactly once. Legacy Runs retain retry
-- behavior because they do not carry an execution package binding.
DROP TRIGGER IF EXISTS execution_attempt_insert_guard;

-- Package-linked attempts may be durably prepared while their Run is still
-- setup_ready. Existing dispatch behavior remains valid when it has already
-- entered executing, and legacy retries retain their original guard.
-- +goose StatementBegin
CREATE TRIGGER execution_attempt_insert_guard
BEFORE INSERT ON execution_attempts
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM runs
    WHERE id = NEW.run_row_id
      AND (
          (execution_package_row_id IS NULL AND status = 'executing') OR
          (execution_package_row_id IS NOT NULL AND status IN ('setup_ready', 'executing'))
      )
) OR NEW.attempt_number <> COALESCE((
    SELECT MAX(attempt_number) + 1 FROM execution_attempts WHERE run_row_id = NEW.run_row_id
), 1)
BEGIN SELECT RAISE(ABORT, 'execution attempt requires an eligible Run and next attempt number'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER execution_attempt_package_run_singleton
BEFORE INSERT ON execution_attempts
FOR EACH ROW WHEN EXISTS (
    SELECT 1
    FROM runs
    WHERE id = NEW.run_row_id
      AND execution_package_row_id IS NOT NULL
      AND EXISTS (
          SELECT 1
          FROM execution_attempts
          WHERE run_row_id = NEW.run_row_id
      )
)
BEGIN SELECT RAISE(ABORT, 'package-linked Run may have only one execution attempt'); END;
-- +goose StatementEnd

CREATE UNIQUE INDEX idx_artifacts_one_adaptive_execution_input_per_attempt
ON artifacts(execution_attempt_row_id)
WHERE owner_type = 'execution_attempt'
  AND kind = 'adaptive_execution_input';

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_one_adaptive_execution_input_per_attempt;
DROP TRIGGER IF EXISTS execution_attempt_package_run_singleton;
DROP TRIGGER IF EXISTS execution_attempt_insert_guard;

-- +goose StatementBegin
CREATE TRIGGER execution_attempt_insert_guard
BEFORE INSERT ON execution_attempts
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM runs WHERE id = NEW.run_row_id AND status = 'executing'
) OR NEW.attempt_number <> COALESCE((
    SELECT MAX(attempt_number) + 1 FROM execution_attempts WHERE run_row_id = NEW.run_row_id
), 1)
BEGIN SELECT RAISE(ABORT, 'execution attempt requires an executing run and next attempt number'); END;
-- +goose StatementEnd
