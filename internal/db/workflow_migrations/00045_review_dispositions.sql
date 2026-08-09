-- +goose Up
-- Review dispositions are transient completion data, never persisted.
SELECT 1;

-- +goose Down
SELECT 1;
