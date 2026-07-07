-- Seed script for data/workflow/relay-workflow.sqlite
-- Run with: sqlite3 data/workflow/relay-workflow.sqlite < scripts/seed-workflow-db.sql
--
-- The workflow schema has strict CHECK constraints and INSERT/UPDATE triggers
-- that enforce valid state machines, so rows are inserted in dependency order
-- and statuses are transitioned via UPDATE rather than inserted in a terminal state.

PRAGMA foreign_keys = ON;

-- ============================================================
-- repository_targets
-- ============================================================
INSERT INTO repository_targets (repo_target, local_path) VALUES
    ('relay',        'D:/Code/relay'),
    ('relay-infra',  'D:/Code/relay-infra');

-- ============================================================
-- projects and lightweight Project organization
-- ============================================================
INSERT INTO projects (project_id, name, description) VALUES
    ('project-00000000-0000-0000-0000-000000000001', 'Relay', 'Primary Relay workflow work.'),
    ('project-00000000-0000-0000-0000-000000000002', 'Relay Infrastructure', 'Infrastructure and store refactor work.');

INSERT INTO project_repository_targets (project_row_id, repo_target)
    SELECT id, 'relay' FROM projects WHERE project_id = 'project-00000000-0000-0000-0000-000000000001';

INSERT INTO project_repository_targets (project_row_id, repo_target)
    SELECT id, 'relay' FROM projects WHERE project_id = 'project-00000000-0000-0000-0000-000000000002';

INSERT INTO project_repository_targets (project_row_id, repo_target)
    SELECT id, 'relay-infra' FROM projects WHERE project_id = 'project-00000000-0000-0000-0000-000000000002';

INSERT INTO project_notes (note_id, project_row_id, title, body)
    SELECT 'note-00000000-0000-0000-0000-000000000001', id, 'Future cleanup', 'Review remaining legacy cleanup after the canonical workflow pivot.'
    FROM projects WHERE project_id = 'project-00000000-0000-0000-0000-000000000001';

-- ============================================================
-- plans  (must start 'active'; completed_at must be NULL)
-- ============================================================
INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256)
    SELECT id,
           'plan-00000000-0000-0000-0000-000000000001',
           'feat/simplification',
           'aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899'
    FROM projects WHERE project_id = 'project-00000000-0000-0000-0000-000000000001';

INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256)
    SELECT id,
           'plan-00000000-0000-0000-0000-000000000002',
           'feat/refactor-store',
           '1122334455667788990011223344556677889900112233445566778899001122'
    FROM projects WHERE project_id = 'project-00000000-0000-0000-0000-000000000002';

-- ============================================================
-- plan_repository_targets
-- ============================================================
-- Plan 1 targets: relay (primary)
INSERT INTO plan_repository_targets (plan_row_id, sequence, repo_target, branch, planning_base_commit)
    SELECT id, 1, 'relay', 'feat/simplification',
           'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef'
    FROM plans WHERE plan_id = 'plan-00000000-0000-0000-0000-000000000001';

-- Plan 2 targets: relay (primary) + relay-infra (secondary)
INSERT INTO plan_repository_targets (plan_row_id, sequence, repo_target, branch, planning_base_commit)
    SELECT id, 1, 'relay', 'feat/refactor-store',
           'cafebabecafebabecafebabecafebabecafebabe'
    FROM plans WHERE plan_id = 'plan-00000000-0000-0000-0000-000000000002';

INSERT INTO plan_repository_targets (plan_row_id, sequence, repo_target, branch, planning_base_commit)
    SELECT id, 2, 'relay-infra', 'feat/refactor-store',
           'cafebabecafebabecafebabecafebabecafebabe'
    FROM plans WHERE plan_id = 'plan-00000000-0000-0000-0000-000000000002';

-- ============================================================
-- plan_passes  (must start 'planned')
-- ============================================================
-- Plan 1 — two passes on relay
INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target, status)
    SELECT 'pass-00000000-0000-0000-0001-000000000001',
           id, 1, 'Simplify store layer', 'relay', 'planned'
    FROM plans WHERE plan_id = 'plan-00000000-0000-0000-0000-000000000001';

INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target, status)
    SELECT 'pass-00000000-0000-0000-0001-000000000002',
           id, 2, 'Wire up simplified API handlers', 'relay', 'planned'
    FROM plans WHERE plan_id = 'plan-00000000-0000-0000-0000-000000000001';

-- Plan 2 — one pass per repo
INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target, status)
    SELECT 'pass-00000000-0000-0000-0002-000000000001',
           id, 1, 'Refactor relay store', 'relay', 'planned'
    FROM plans WHERE plan_id = 'plan-00000000-0000-0000-0000-000000000002';

INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target, status)
    SELECT 'pass-00000000-0000-0000-0002-000000000002',
           id, 2, 'Refactor relay-infra store', 'relay-infra', 'planned'
    FROM plans WHERE plan_id = 'plan-00000000-0000-0000-0000-000000000002';

-- ============================================================
-- plan_pass_dependencies
-- Plan 1: pass 2 depends on pass 1
-- Plan 2: pass 2 depends on pass 1
-- ============================================================
INSERT INTO plan_pass_dependencies (pass_row_id, depends_on_pass_row_id)
    SELECT p2.id, p1.id
    FROM plan_passes p1
    JOIN plan_passes p2
        ON  p1.plan_row_id = p2.plan_row_id
        AND p1.pass_number  = 1
        AND p2.pass_number  = 2
    WHERE p1.pass_id IN (
        'pass-00000000-0000-0000-0001-000000000001',
        'pass-00000000-0000-0000-0002-000000000001'
    );

-- ============================================================
-- Advance plan 1 / pass 1 to in_progress so a run can be created.
-- Trigger: planned → in_progress requires plan to be active and
--          no outstanding dependencies (pass 1 has none).
-- ============================================================
UPDATE plan_passes
SET status     = 'in_progress',
    started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE pass_id = 'pass-00000000-0000-0000-0001-000000000001'
  AND status  = 'planned';

-- ============================================================
-- runs
-- A managed run requires plan status = 'active' and the
-- linked pass status = 'in_progress'.
-- ============================================================
INSERT INTO runs (
    run_id, feature_slug, repo_target,
    plan_row_id, plan_pass_row_id,
    remediates_run_row_id,
    status, branch, base_commit, canonical_sha256
)
SELECT
    'run-00000000-0000-0000-0001-000000000001',
    'feat/simplification',
    'relay',
    pl.id,
    pp.id,
    NULL,
    'created',
    'feat/simplification',
    'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef',
    'fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210'
FROM plans    pl
JOIN plan_passes pp ON pp.plan_row_id = pl.id
WHERE pl.plan_id  = 'plan-00000000-0000-0000-0000-000000000001'
  AND pp.pass_id  = 'pass-00000000-0000-0000-0001-000000000001';

-- An unmanaged (standalone) run — no plan or pass association.
INSERT INTO runs (
    run_id, feature_slug, repo_target,
    plan_row_id, plan_pass_row_id,
    remediates_run_row_id,
    status, branch, base_commit, canonical_sha256
) VALUES (
    'run-00000000-0000-0000-0000-000000000099',
    'feat/hotfix-logging',
    'relay',
    NULL, NULL, NULL,
    'created',
    'feat/hotfix-logging',
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
);

-- ============================================================
-- Advance managed run to executing (created → setup_ready → executing)
-- ============================================================
UPDATE runs
SET status     = 'setup_ready',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_id = 'run-00000000-0000-0000-0001-000000000001'
  AND status = 'created';

UPDATE runs
SET status     = 'executing',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_id = 'run-00000000-0000-0000-0001-000000000001'
  AND status = 'setup_ready';

-- ============================================================
-- execution_attempts
-- Trigger: run must be 'executing'; attempt_number must be MAX+1.
-- ============================================================
INSERT INTO execution_attempts (
    attempt_id, run_row_id, attempt_number, adapter, model, status
)
SELECT
    'attempt-00000000-0000-0000-0001-000000000001',
    id,
    1,
    'opencode',
    'opencode-go/deepseek-v4-pro',
    'pending'
FROM runs WHERE run_id = 'run-00000000-0000-0000-0001-000000000001';

-- Advance attempt to running → succeeded
UPDATE execution_attempts
SET status     = 'running',
    started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE attempt_id = 'attempt-00000000-0000-0000-0001-000000000001'
  AND status = 'pending';

UPDATE execution_attempts
SET status      = 'succeeded',
    finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    result_json = '{"exit_code":0,"summary":"All changes applied cleanly."}'
WHERE attempt_id = 'attempt-00000000-0000-0000-0001-000000000001'
  AND status = 'running';

-- ============================================================
-- Advance run to validating → audit_ready
-- ============================================================
UPDATE runs
SET status     = 'validating',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_id = 'run-00000000-0000-0000-0001-000000000001'
  AND status = 'executing';

UPDATE runs
SET status     = 'audit_ready',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_id = 'run-00000000-0000-0000-0001-000000000001'
  AND status = 'validating';

-- ============================================================
-- artifacts  (audit_packet artifact owned by the run)
-- ============================================================
INSERT INTO artifacts (
    artifact_id, owner_type,
    plan_row_id, run_row_id, execution_attempt_row_id,
    kind, relative_path, media_type, sha256, size_bytes
)
SELECT
    'artifact-00000000-0000-0000-0001-000000000001',
    'run',
    NULL,
    r.id,
    NULL,
    'audit_packet',
    'workflow/artifacts/run-00000000-0000-0000-0001-000000000001/audit_packet.json',
    'application/json',
    'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
    2048
FROM runs r
WHERE r.run_id = 'run-00000000-0000-0000-0001-000000000001';

-- A plan-owned artifact (e.g. the compiled plan document)
INSERT INTO artifacts (
    artifact_id, owner_type,
    plan_row_id, run_row_id, execution_attempt_row_id,
    kind, relative_path, media_type, sha256, size_bytes
)
SELECT
    'artifact-00000000-0000-0000-0001-000000000002',
    'plan',
    pl.id,
    NULL,
    NULL,
    'plan_document',
    'workflow/artifacts/plan-00000000-0000-0000-0000-000000000001/plan.json',
    'application/json',
    'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
    4096
FROM plans pl
WHERE pl.plan_id = 'plan-00000000-0000-0000-0000-000000000001';

-- An execution-attempt-owned artifact (stdout log)
INSERT INTO artifacts (
    artifact_id, owner_type,
    plan_row_id, run_row_id, execution_attempt_row_id,
    kind, relative_path, media_type, sha256, size_bytes
)
SELECT
    'artifact-00000000-0000-0000-0001-000000000003',
    'execution_attempt',
    NULL,
    NULL,
    ea.id,
    'stdout_log',
    'workflow/artifacts/attempt-00000000-0000-0000-0001-000000000001/stdout.log',
    'text/plain',
    'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',
    8192
FROM execution_attempts ea
WHERE ea.attempt_id = 'attempt-00000000-0000-0000-0001-000000000001';

-- ============================================================
-- audit_packets
-- Trigger: run must be 'audit_ready'; artifact must be run-owned
--          audit_packet kind; packet_sha256 must match artifact sha256.
-- ============================================================
INSERT INTO audit_packets (
    audit_packet_id, run_row_id, execution_attempt_row_id, artifact_row_id,
    base_commit, audited_commit, packet_sha256, status
)
SELECT
    'packet-00000000-0000-0000-0001-000000000001',
    r.id,
    ea.id,
    a.id,
    'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef',
    'feedfacedeadbeedfeedfacedeadbeeffeedface',
    'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
    'current'
FROM runs r
JOIN execution_attempts ea ON ea.run_row_id = r.id
JOIN artifacts          a  ON a.run_row_id  = r.id AND a.kind = 'audit_packet'
WHERE r.run_id = 'run-00000000-0000-0000-0001-000000000001';

-- ============================================================
-- audit_decisions
-- Trigger: run must be 'audit_ready'; packet must be 'current'
--          and packet_sha256 must match; audited_commit must match.
-- ============================================================
INSERT INTO audit_decisions (
    audit_decision_id, run_row_id, audit_packet_artifact_row_id,
    audited_commit, packet_sha256, decision, rationale
)
SELECT
    'audit-00000000-0000-0000-0001-000000000001',
    r.id,
    a.id,
    'feedfacedeadbeedfeedfacedeadbeeffeedface',
    'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
    'accepted',
    'All changes look correct. Simplification is clean and tests pass.'
FROM runs r
JOIN artifacts a ON a.run_row_id = r.id AND a.kind = 'audit_packet'
WHERE r.run_id = 'run-00000000-0000-0000-0001-000000000001';
