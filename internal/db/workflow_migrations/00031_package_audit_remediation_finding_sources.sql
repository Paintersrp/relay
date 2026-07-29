-- +goose Up
DROP TRIGGER IF EXISTS audit_remediation_seed_finding_update_immutable;
DROP TRIGGER IF EXISTS audit_remediation_seed_finding_delete_guard;

CREATE TABLE audit_remediation_seed_findings_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    remediation_seed_row_id INTEGER NOT NULL REFERENCES audit_remediation_seeds(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    upstream_classification TEXT NOT NULL CHECK (upstream_classification IN ('implementation', 'governing_package', 'both')),
    summary TEXT NOT NULL CHECK (summary <> '' AND trim(summary) <> ''),
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) <> ''),
    required_remediation TEXT NOT NULL CHECK (required_remediation <> '' AND trim(required_remediation) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (remediation_seed_row_id, sequence)
);
INSERT INTO audit_remediation_seed_findings_new (id, remediation_seed_row_id, sequence, upstream_classification, summary, evidence, required_remediation, created_at)
SELECT id, remediation_seed_row_id, sequence, upstream_classification, summary, evidence, required_remediation, created_at
FROM audit_remediation_seed_findings;
DROP TABLE audit_remediation_seed_findings;
ALTER TABLE audit_remediation_seed_findings_new RENAME TO audit_remediation_seed_findings;

-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_finding_update_immutable
BEFORE UPDATE ON audit_remediation_seed_findings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed findings are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_finding_delete_guard
BEFORE DELETE ON audit_remediation_seed_findings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed findings are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS audit_remediation_seed_finding_update_immutable;
DROP TRIGGER IF EXISTS audit_remediation_seed_finding_delete_guard;

CREATE TABLE audit_remediation_seed_findings_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    remediation_seed_row_id INTEGER NOT NULL REFERENCES audit_remediation_seeds(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    upstream_classification TEXT NOT NULL CHECK (upstream_classification IN ('executor_implementation', 'execution_spec', 'both')),
    summary TEXT NOT NULL CHECK (summary <> '' AND trim(summary) <> ''),
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) <> ''),
    required_remediation TEXT NOT NULL CHECK (required_remediation <> '' AND trim(required_remediation) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (remediation_seed_row_id, sequence)
);
INSERT INTO audit_remediation_seed_findings_old (id, remediation_seed_row_id, sequence, upstream_classification, summary, evidence, required_remediation, created_at)
SELECT id, remediation_seed_row_id, sequence, upstream_classification, summary, evidence, required_remediation, created_at
FROM audit_remediation_seed_findings;
DROP TABLE audit_remediation_seed_findings;
ALTER TABLE audit_remediation_seed_findings_old RENAME TO audit_remediation_seed_findings;

-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_finding_update_immutable
BEFORE UPDATE ON audit_remediation_seed_findings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed findings are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_remediation_seed_finding_delete_guard
BEFORE DELETE ON audit_remediation_seed_findings
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'remediation seed findings are retained history'); END;
-- +goose StatementEnd
