-- +goose Up
-- Review is a transient, read-only result. It is deliberately not a workflow
-- record and cannot become durable approval authority.
SELECT 1;

-- +goose Down
SELECT 1;
