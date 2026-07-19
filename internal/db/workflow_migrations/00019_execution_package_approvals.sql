-- +goose Up
-- An explicit package approval is the sole durable authorization that a
-- selected execution package may produce one setup-ready Run. Evidence is
-- canonical nonempty operator confirmation (1-4096 characters after trimming).
-- One package may only be approved once.
CREATE TABLE execution_package_approvals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_id TEXT NOT NULL UNIQUE,
    package_row_id INTEGER NOT NULL UNIQUE REFERENCES execution_packages(id) ON DELETE RESTRICT,
    package_sha256 TEXT NOT NULL CHECK (length(package_sha256) = 64 AND package_sha256 NOT GLOB '*[^0-9a-f]*'),
    operator_confirmation_evidence TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (approval_id GLOB 'pkg-approval-*' AND trim(approval_id) = approval_id),
    CHECK (operator_confirmation_evidence = trim(operator_confirmation_evidence) AND length(operator_confirmation_evidence) BETWEEN 1 AND 4096)
);

-- +goose StatementBegin
CREATE TRIGGER execution_package_approval_update_immutable
BEFORE UPDATE ON execution_package_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution package approvals are immutable history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER execution_package_approval_delete_guard
BEFORE DELETE ON execution_package_approvals
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'execution package approvals are retained history'); END;
-- +goose StatementEnd

ALTER TABLE runs ADD COLUMN package_approval_row_id INTEGER REFERENCES execution_package_approvals(id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX idx_runs_one_package_approval
ON runs(package_approval_row_id)
WHERE package_approval_row_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER run_package_approval_insert_guard
BEFORE INSERT ON runs
FOR EACH ROW WHEN NEW.package_approval_row_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM execution_package_approvals AS approval
    JOIN execution_packages AS package ON package.id = approval.package_row_id
    WHERE approval.id = NEW.package_approval_row_id
      AND approval.package_sha256 = package.package_sha256
      AND NEW.execution_package_row_id = approval.package_row_id
)
BEGIN SELECT RAISE(ABORT, 'Run package approval must match the linked execution package and sha'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER run_package_approval_update_guard
BEFORE UPDATE OF package_approval_row_id ON runs
FOR EACH ROW WHEN OLD.package_approval_row_id IS NOT NULL
    OR NEW.package_approval_row_id IS NULL
    OR NOT EXISTS (
        SELECT 1
        FROM execution_package_approvals AS approval
        JOIN execution_packages AS package ON package.id = approval.package_row_id
        WHERE approval.id = NEW.package_approval_row_id
          AND approval.package_sha256 = package.package_sha256
          AND NEW.execution_package_row_id = approval.package_row_id
    )
BEGIN SELECT RAISE(ABORT, 'Run package approval link is immutable and must match the exact package sha'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS run_package_approval_update_guard;
DROP TRIGGER IF EXISTS run_package_approval_insert_guard;
DROP INDEX IF EXISTS idx_runs_one_package_approval;
ALTER TABLE runs DROP COLUMN package_approval_row_id;
DROP TRIGGER IF EXISTS execution_package_approval_delete_guard;
DROP TRIGGER IF EXISTS execution_package_approval_update_immutable;
DROP TABLE IF EXISTS execution_package_approvals;
