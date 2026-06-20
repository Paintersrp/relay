-- +goose Up
-- +goose StatementBegin
ALTER TABLE runs ADD COLUMN executor_adapter TEXT NOT NULL DEFAULT 'opencode_go';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN executor_adapter;
-- +goose StatementEnd
