-- +goose Up
DROP TRIGGER source_index_generation_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER source_index_generation_transition_guard
BEFORE UPDATE ON source_index_generations
FOR EACH ROW WHEN NOT (
    (OLD.state = 'pending' AND NEW.state = 'building' AND NEW.attempt_count = OLD.attempt_count + 1)
    OR (OLD.state = 'pending' AND NEW.state = 'retired' AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'building' AND NEW.state IN ('ready', 'failed') AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'failed' AND NEW.state IN ('pending', 'retired') AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'ready' AND NEW.state = 'retired' AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'retired' AND NEW.state = 'pending' AND NEW.attempt_count = OLD.attempt_count)
) BEGIN SELECT RAISE(ABORT, 'source index generation lifecycle transition is invalid'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS source_index_generation_transition_guard;

-- +goose StatementBegin
CREATE TRIGGER source_index_generation_transition_guard
BEFORE UPDATE ON source_index_generations
FOR EACH ROW WHEN NOT (
    (OLD.state = 'pending' AND NEW.state = 'building' AND NEW.attempt_count = OLD.attempt_count + 1)
    OR (OLD.state = 'pending' AND NEW.state = 'retired' AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'building' AND NEW.state IN ('ready', 'failed') AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'failed' AND NEW.state IN ('pending', 'retired') AND NEW.attempt_count = OLD.attempt_count)
    OR (OLD.state = 'ready' AND NEW.state = 'retired' AND NEW.attempt_count = OLD.attempt_count)
) BEGIN SELECT RAISE(ABORT, 'source index generation lifecycle transition is invalid'); END;
-- +goose StatementEnd
