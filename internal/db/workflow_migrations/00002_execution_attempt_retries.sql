-- +goose Up
DROP TRIGGER IF EXISTS run_status_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER run_status_transition_guard
BEFORE UPDATE OF status ON runs
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'created' AND NEW.status = 'setup_ready') OR
    (OLD.status = 'setup_ready' AND NEW.status IN ('executing', 'cancelled')) OR
    (OLD.status = 'executing' AND NEW.status IN ('execution_failed', 'cancelled', 'validating')) OR
    (OLD.status = 'execution_failed' AND NEW.status IN ('executing', 'validating')) OR
    (OLD.status = 'cancelled' AND NEW.status = 'executing') OR
    (OLD.status = 'validating' AND NEW.status IN ('validation_failed', 'audit_ready')) OR
    (OLD.status = 'validation_failed' AND NEW.status = 'needs_revision') OR
    (OLD.status = 'audit_ready' AND NEW.status IN ('needs_revision', 'completed'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid run status transition');
END;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS execution_attempt_status_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER execution_attempt_status_transition_guard
BEFORE UPDATE OF status ON execution_attempts
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'pending' AND NEW.status IN ('running', 'failed', 'cancelled', 'timed_out')) OR
    (OLD.status = 'running' AND NEW.status IN ('succeeded', 'failed', 'cancelled', 'timed_out'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid execution attempt status transition');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS execution_attempt_status_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER execution_attempt_status_transition_guard
BEFORE UPDATE OF status ON execution_attempts
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'pending' AND NEW.status IN ('running', 'cancelled')) OR
    (OLD.status = 'running' AND NEW.status IN ('succeeded', 'failed', 'cancelled', 'timed_out'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid execution attempt status transition');
END;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS run_status_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER run_status_transition_guard
BEFORE UPDATE OF status ON runs
FOR EACH ROW
WHEN NEW.status <> OLD.status AND NOT (
    (OLD.status = 'created' AND NEW.status = 'setup_ready') OR
    (OLD.status = 'setup_ready' AND NEW.status IN ('executing', 'cancelled')) OR
    (OLD.status = 'executing' AND NEW.status IN ('execution_failed', 'cancelled', 'validating')) OR
    (OLD.status = 'execution_failed' AND NEW.status = 'validating') OR
    (OLD.status = 'validating' AND NEW.status IN ('validation_failed', 'audit_ready')) OR
    (OLD.status = 'validation_failed' AND NEW.status = 'needs_revision') OR
    (OLD.status = 'audit_ready' AND NEW.status IN ('needs_revision', 'completed'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid run status transition');
END;
-- +goose StatementEnd
