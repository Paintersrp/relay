-- name: CreateRepositoryTarget :one
INSERT INTO repository_targets (repo_target, local_path)
VALUES (?, ?)
RETURNING *;

-- name: GetRepositoryTarget :one
SELECT *
FROM repository_targets
WHERE repo_target = ? COLLATE NOCASE;

-- name: ListRepositoryTargets :many
SELECT *
FROM repository_targets
ORDER BY repo_target COLLATE NOCASE;

-- name: CreatePlan :one
INSERT INTO plans (plan_id, feature_slug, status, canonical_sha256)
VALUES (?, ?, 'active', ?)
RETURNING *;

-- name: GetPlanByPlanID :one
SELECT *
FROM plans
WHERE plan_id = ?;

-- name: CreatePlanRepositoryTarget :one
INSERT INTO plan_repository_targets (
    plan_row_id,
    sequence,
    repo_target,
    branch,
    planning_base_commit
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListPlanRepositoryTargets :many
SELECT *
FROM plan_repository_targets
WHERE plan_row_id = ?
ORDER BY sequence;

-- name: CreatePlanPass :one
INSERT INTO plan_passes (
    pass_id,
    plan_row_id,
    pass_number,
    name,
    repo_target,
    status
)
VALUES (?, ?, ?, ?, ?, 'planned')
RETURNING *;

-- name: GetPlanPassByRowID :one
SELECT *
FROM plan_passes
WHERE id = ?;

-- name: GetPlanPassByPassID :one
SELECT *
FROM plan_passes
WHERE pass_id = ?;

-- name: GetPlanPassByPlanAndNumber :one
SELECT *
FROM plan_passes
WHERE plan_row_id = ? AND pass_number = ?;

-- name: ListPlanPasses :many
SELECT *
FROM plan_passes
WHERE plan_row_id = ?
ORDER BY pass_number;

-- name: CreatePlanPassDependency :exec
INSERT INTO plan_pass_dependencies (pass_row_id, depends_on_pass_row_id)
VALUES (?, ?);

-- name: ListPlanPassDependencies :many
SELECT *
FROM plan_pass_dependencies
WHERE pass_row_id = ?
ORDER BY depends_on_pass_row_id;

-- name: CountIncompletePlanPasses :one
SELECT COUNT(*)
FROM plan_passes
WHERE plan_row_id = ? AND status <> 'completed';

-- name: TransitionPlanPassStatus :one
UPDATE plan_passes
SET
    status = ?,
    started_at = CASE
        WHEN ? = 'in_progress' THEN COALESCE(started_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE started_at
    END,
    completed_at = CASE
        WHEN ? = 'completed' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE pass_id = ? AND status = ?
RETURNING *;

-- name: CompletePlan :one
UPDATE plans
SET
    status = 'completed',
    completed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ? AND status = 'active'
RETURNING *;

-- name: CreateRun :one
INSERT INTO runs (
    run_id,
    feature_slug,
    repo_target,
    plan_row_id,
    plan_pass_row_id,
    remediates_run_row_id,
    status,
    branch,
    base_commit,
    canonical_sha256
)
VALUES (?, ?, ?, ?, ?, ?, 'created', ?, ?, ?)
RETURNING *;

-- name: GetRunByRowID :one
SELECT *
FROM runs
WHERE id = ?;

-- name: GetRunByRunID :one
SELECT *
FROM runs
WHERE run_id = ?;

-- name: ListRunsByPlanPass :many
SELECT *
FROM runs
WHERE plan_pass_row_id = ?
ORDER BY created_at, id;

-- name: TransitionRunStatus :one
UPDATE runs
SET
    status = ?,
    completed_at = CASE
        WHEN ? = 'completed' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_id = ? AND status = ?
RETURNING *;

-- name: NextExecutionAttemptNumber :one
SELECT COALESCE(MAX(attempt_number), 0) + 1
FROM execution_attempts
WHERE run_row_id = ?;

-- name: CreateExecutionAttempt :one
INSERT INTO execution_attempts (
    attempt_id,
    run_row_id,
    attempt_number,
    adapter,
    model,
    status
)
VALUES (?, ?, ?, ?, ?, 'pending')
RETURNING *;

-- name: GetExecutionAttemptByAttemptID :one
SELECT *
FROM execution_attempts
WHERE attempt_id = ?;

-- name: GetLatestExecutionAttemptByRun :one
SELECT *
FROM execution_attempts
WHERE run_row_id = ?
ORDER BY attempt_number DESC
LIMIT 1;

-- name: TransitionExecutionAttemptStatus :one
UPDATE execution_attempts
SET
    status = sqlc.arg(status),
    result_json = ?,
    started_at = CASE
        WHEN sqlc.arg(status) IN ('running', 'cancelled') THEN COALESCE(started_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE started_at
    END,
    finished_at = CASE
        WHEN sqlc.arg(status) IN ('succeeded', 'failed', 'cancelled', 'timed_out') THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    cancellation_requested_at = CASE
        WHEN sqlc.arg(status) = 'cancelled' THEN COALESCE(cancellation_requested_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE cancellation_requested_at
    END
WHERE attempt_id = ? AND status = ?
RETURNING *;

-- name: CreateArtifact :one
INSERT INTO artifacts (
    artifact_id,
    owner_type,
    plan_row_id,
    run_row_id,
    execution_attempt_row_id,
    kind,
    relative_path,
    media_type,
    sha256,
    size_bytes
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetArtifactByArtifactID :one
SELECT *
FROM artifacts
WHERE artifact_id = ?;

-- name: ListArtifactsByPlan :many
SELECT *
FROM artifacts
WHERE plan_row_id = ?
ORDER BY created_at, id;

-- name: ListArtifactsByRun :many
SELECT *
FROM artifacts
WHERE run_row_id = ?
ORDER BY created_at, id;

-- name: ListArtifactsByExecutionAttempt :many
SELECT *
FROM artifacts
WHERE execution_attempt_row_id = ?
ORDER BY created_at, id;

-- name: CreateAuditDecision :one
INSERT INTO audit_decisions (
    audit_decision_id,
    run_row_id,
    audit_packet_artifact_row_id,
    audited_commit,
    packet_sha256,
    decision,
    rationale
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAuditDecisionByDecisionID :one
SELECT *
FROM audit_decisions
WHERE audit_decision_id = ?;
