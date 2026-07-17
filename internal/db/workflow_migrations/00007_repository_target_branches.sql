-- +goose Up
ALTER TABLE repository_targets
ADD COLUMN configured_branch_ref TEXT
CHECK (
    configured_branch_ref IS NULL OR (
        configured_branch_ref <> ''
        AND trim(configured_branch_ref) = configured_branch_ref
        AND length(configured_branch_ref) <= 1024
        AND configured_branch_ref GLOB 'refs/heads/*'
    )
);

ALTER TABLE repository_targets
ADD COLUMN configuration_version INTEGER NOT NULL DEFAULT 1
CHECK (configuration_version >= 1);

-- +goose Down
ALTER TABLE repository_targets DROP COLUMN configuration_version;
ALTER TABLE repository_targets DROP COLUMN configured_branch_ref;

