package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
)

type Tx struct {
	tx *sql.Tx
}

func (tx *Tx) CreateRepositoryTarget(ctx context.Context, repoTarget, localPath string) (RepositoryTarget, error) {
	var value RepositoryTarget
	err := tx.tx.QueryRowContext(ctx, `
INSERT INTO repository_targets (repo_target, local_path)
VALUES (?, ?)
RETURNING repo_target, local_path, created_at, updated_at`, repoTarget, localPath).Scan(
		&value.RepoTarget,
		&value.LocalPath,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	return value, err
}

func (tx *Tx) GetRepositoryTarget(ctx context.Context, repoTarget string) (RepositoryTarget, error) {
	return getRepositoryTarget(ctx, tx.tx, repoTarget)
}

func (tx *Tx) CreatePlan(ctx context.Context, params CreatePlanParams) (Plan, error) {
	var value Plan
	err := tx.tx.QueryRowContext(ctx, `
INSERT INTO plans (plan_id, feature_slug, status, canonical_sha256)
VALUES (?, ?, 'active', ?)
RETURNING id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at`,
		params.PlanID,
		params.FeatureSlug,
		params.CanonicalSHA256,
	).Scan(
		&value.ID,
		&value.PlanID,
		&value.FeatureSlug,
		&value.Status,
		&value.CanonicalSHA256,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.CompletedAt,
	)
	return value, err
}

func (tx *Tx) GetPlanByPlanID(ctx context.Context, planID string) (Plan, error) {
	return getPlanByPlanID(ctx, tx.tx, planID)
}

func (tx *Tx) CreatePlanRepositoryTarget(ctx context.Context, params CreatePlanRepositoryTargetParams) (PlanRepositoryTarget, error) {
	var value PlanRepositoryTarget
	err := tx.tx.QueryRowContext(ctx, `
INSERT INTO plan_repository_targets (plan_row_id, sequence, repo_target, branch, planning_base_commit)
VALUES (?, ?, ?, ?, ?)
RETURNING id, plan_row_id, sequence, repo_target, branch, planning_base_commit, created_at`,
		params.PlanRowID,
		params.Sequence,
		params.RepoTarget,
		params.Branch,
		params.PlanningBaseCommit,
	).Scan(
		&value.ID,
		&value.PlanRowID,
		&value.Sequence,
		&value.RepoTarget,
		&value.Branch,
		&value.PlanningBaseCommit,
		&value.CreatedAt,
	)
	return value, err
}

func (tx *Tx) CreatePlanPass(ctx context.Context, params CreatePlanPassParams) (PlanPass, error) {
	return scanPlanPass(tx.tx.QueryRowContext(ctx, `
INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target, status)
VALUES (?, ?, ?, ?, ?, 'planned')
RETURNING id, pass_id, plan_row_id, pass_number, name, repo_target, status, created_at, updated_at, started_at, completed_at`,
		params.PassID,
		params.PlanRowID,
		params.PassNumber,
		params.Name,
		params.RepoTarget,
	))
}

func (tx *Tx) CreatePlanPassDependency(ctx context.Context, passRowID, dependsOnPassRowID int64) error {
	_, err := tx.tx.ExecContext(ctx, `
INSERT INTO plan_pass_dependencies (pass_row_id, depends_on_pass_row_id)
VALUES (?, ?)`, passRowID, dependsOnPassRowID)
	return err
}

func (tx *Tx) GetPlanPassByPassID(ctx context.Context, passID string) (PlanPass, error) {
	return getPlanPassByPassID(ctx, tx.tx, passID)
}

func (tx *Tx) GetPlanPassByRowID(ctx context.Context, rowID int64) (PlanPass, error) {
	return getPlanPassByRowID(ctx, tx.tx, rowID)
}

func (tx *Tx) GetPlanPassByPlanAndNumber(ctx context.Context, planRowID, passNumber int64) (PlanPass, error) {
	return getPlanPassByPlanAndNumber(ctx, tx.tx, planRowID, passNumber)
}

func (tx *Tx) TransitionPlanPass(ctx context.Context, passID, expectedStatus, nextStatus string) (PlanPass, error) {
	return scanPlanPass(tx.tx.QueryRowContext(ctx, `
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
RETURNING id, pass_id, plan_row_id, pass_number, name, repo_target, status, created_at, updated_at, started_at, completed_at`,
		nextStatus,
		nextStatus,
		nextStatus,
		passID,
		expectedStatus,
	))
}

func (tx *Tx) CountIncompletePlanPasses(ctx context.Context, planRowID int64) (int64, error) {
	var count int64
	err := tx.tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM plan_passes
WHERE plan_row_id = ? AND status <> 'completed'`, planRowID).Scan(&count)
	return count, err
}

func (tx *Tx) CompletePlan(ctx context.Context, planRowID int64) (Plan, error) {
	var value Plan
	err := tx.tx.QueryRowContext(ctx, `
UPDATE plans
SET
    status = 'completed',
    completed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ? AND status = 'active'
RETURNING id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at`, planRowID).Scan(
		&value.ID,
		&value.PlanID,
		&value.FeatureSlug,
		&value.Status,
		&value.CanonicalSHA256,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.CompletedAt,
	)
	return value, err
}

func (tx *Tx) CreateRun(ctx context.Context, params CreateRunParams) (Run, error) {
	var value Run
	err := tx.tx.QueryRowContext(ctx, `
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
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id,
          remediates_run_row_id, status, branch, base_commit, canonical_sha256,
          created_at, updated_at, completed_at`,
		params.RunID,
		params.FeatureSlug,
		params.RepoTarget,
		params.PlanRowID,
		params.PlanPassRowID,
		params.RemediatesRunRowID,
		params.Status,
		params.Branch,
		params.BaseCommit,
		params.CanonicalSHA256,
	).Scan(
		&value.ID,
		&value.RunID,
		&value.FeatureSlug,
		&value.RepoTarget,
		&value.PlanRowID,
		&value.PlanPassRowID,
		&value.RemediatesRunRowID,
		&value.Status,
		&value.Branch,
		&value.BaseCommit,
		&value.CanonicalSHA256,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.CompletedAt,
	)
	return value, err
}

func (tx *Tx) GetRunByRunID(ctx context.Context, runID string) (Run, error) {
	return getRunByRunID(ctx, tx.tx, runID)
}

func (tx *Tx) GetRunByRowID(ctx context.Context, rowID int64) (Run, error) {
	return getRunByRowID(ctx, tx.tx, rowID)
}

func (tx *Tx) TransitionRun(ctx context.Context, runID, expectedStatus, nextStatus string) (Run, error) {
	var value Run
	err := tx.tx.QueryRowContext(ctx, `
UPDATE runs
SET
    status = ?,
    completed_at = CASE
        WHEN ? = 'completed' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_id = ? AND status = ?
RETURNING id, run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id,
          remediates_run_row_id, status, branch, base_commit, canonical_sha256,
          created_at, updated_at, completed_at`,
		nextStatus,
		nextStatus,
		runID,
		expectedStatus,
	).Scan(
		&value.ID,
		&value.RunID,
		&value.FeatureSlug,
		&value.RepoTarget,
		&value.PlanRowID,
		&value.PlanPassRowID,
		&value.RemediatesRunRowID,
		&value.Status,
		&value.Branch,
		&value.BaseCommit,
		&value.CanonicalSHA256,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.CompletedAt,
	)
	return value, err
}

func (tx *Tx) NextExecutionAttemptNumber(ctx context.Context, runRowID int64) (int64, error) {
	var number int64
	err := tx.tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(attempt_number), 0) + 1
FROM execution_attempts
WHERE run_row_id = ?`, runRowID).Scan(&number)
	return number, err
}

func (tx *Tx) CreateExecutionAttempt(ctx context.Context, params CreateExecutionAttemptParams) (ExecutionAttempt, error) {
	var value ExecutionAttempt
	err := tx.tx.QueryRowContext(ctx, `
INSERT INTO execution_attempts (attempt_id, run_row_id, attempt_number, adapter, model, status)
VALUES (?, ?, ?, ?, ?, 'pending')
RETURNING id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
          created_at, started_at, finished_at, cancellation_requested_at`,
		params.AttemptID,
		params.RunRowID,
		params.AttemptNumber,
		params.Adapter,
		params.Model,
	).Scan(
		&value.ID,
		&value.AttemptID,
		&value.RunRowID,
		&value.AttemptNumber,
		&value.Adapter,
		&value.Model,
		&value.Status,
		&value.ResultJSON,
		&value.CreatedAt,
		&value.StartedAt,
		&value.FinishedAt,
		&value.CancellationRequestedAt,
	)
	return value, err
}

func (tx *Tx) TransitionExecutionAttempt(ctx context.Context, attemptID, expectedStatus, nextStatus, resultJSON string) (ExecutionAttempt, error) {
	if resultJSON == "" {
		resultJSON = "{}"
	}
	var value ExecutionAttempt
	err := tx.tx.QueryRowContext(ctx, `
UPDATE execution_attempts
SET
    status = ?,
    result_json = ?,
    started_at = CASE
        WHEN ? IN ('running', 'succeeded', 'failed', 'cancelled', 'timed_out') THEN COALESCE(started_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE started_at
    END,
    finished_at = CASE
        WHEN ? IN ('succeeded', 'failed', 'cancelled', 'timed_out') THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    cancellation_requested_at = CASE
        WHEN ? = 'cancelled' THEN COALESCE(cancellation_requested_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE cancellation_requested_at
    END
WHERE attempt_id = ? AND status = ?
RETURNING id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
          created_at, started_at, finished_at, cancellation_requested_at`,
		nextStatus,
		resultJSON,
		nextStatus,
		nextStatus,
		nextStatus,
		attemptID,
		expectedStatus,
	).Scan(
		&value.ID,
		&value.AttemptID,
		&value.RunRowID,
		&value.AttemptNumber,
		&value.Adapter,
		&value.Model,
		&value.Status,
		&value.ResultJSON,
		&value.CreatedAt,
		&value.StartedAt,
		&value.FinishedAt,
		&value.CancellationRequestedAt,
	)
	return value, err
}

func (tx *Tx) UpdateExecutionAttemptResult(ctx context.Context, attemptID, expectedStatus, resultJSON string) (ExecutionAttempt, error) {
	var value ExecutionAttempt
	err := tx.tx.QueryRowContext(ctx, `
UPDATE execution_attempts
SET result_json = ?
WHERE attempt_id = ? AND status = ?
RETURNING id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
          created_at, started_at, finished_at, cancellation_requested_at`,
		resultJSON,
		attemptID,
		expectedStatus,
	).Scan(
		&value.ID,
		&value.AttemptID,
		&value.RunRowID,
		&value.AttemptNumber,
		&value.Adapter,
		&value.Model,
		&value.Status,
		&value.ResultJSON,
		&value.CreatedAt,
		&value.StartedAt,
		&value.FinishedAt,
		&value.CancellationRequestedAt,
	)
	return value, err
}

func (tx *Tx) RequestExecutionAttemptCancellation(ctx context.Context, attemptID string) (ExecutionAttempt, error) {
	var value ExecutionAttempt
	err := tx.tx.QueryRowContext(ctx, `
UPDATE execution_attempts
SET cancellation_requested_at = COALESCE(cancellation_requested_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE attempt_id = ? AND status IN ('pending', 'running')
RETURNING id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
          created_at, started_at, finished_at, cancellation_requested_at`,
		attemptID,
	).Scan(
		&value.ID,
		&value.AttemptID,
		&value.RunRowID,
		&value.AttemptNumber,
		&value.Adapter,
		&value.Model,
		&value.Status,
		&value.ResultJSON,
		&value.CreatedAt,
		&value.StartedAt,
		&value.FinishedAt,
		&value.CancellationRequestedAt,
	)
	return value, err
}

func (tx *Tx) CreateArtifact(ctx context.Context, params CreateArtifactParams) (Artifact, error) {
	return scanArtifact(tx.tx.QueryRowContext(ctx, `
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
RETURNING id, artifact_id, owner_type, plan_row_id, run_row_id, execution_attempt_row_id,
          kind, relative_path, media_type, sha256, size_bytes, created_at`,
		params.ArtifactID,
		params.OwnerType,
		params.PlanRowID,
		params.RunRowID,
		params.ExecutionAttemptRowID,
		params.Kind,
		params.RelativePath,
		params.MediaType,
		params.SHA256,
		params.SizeBytes,
	))
}

func (tx *Tx) GetArtifactByArtifactID(ctx context.Context, artifactID string) (Artifact, error) {
	return getArtifactByArtifactID(ctx, tx.tx, artifactID)
}

func (tx *Tx) GetExecutionAttemptByAttemptID(ctx context.Context, attemptID string) (ExecutionAttempt, error) {
	return getExecutionAttemptByAttemptID(ctx, tx.tx, attemptID)
}

func (tx *Tx) CreateAuditDecision(ctx context.Context, params CreateAuditDecisionParams) (AuditDecision, error) {
	var value AuditDecision
	err := tx.tx.QueryRowContext(ctx, `
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
RETURNING id, audit_decision_id, run_row_id, audit_packet_artifact_row_id,
          audited_commit, packet_sha256, decision, rationale, created_at`,
		params.AuditDecisionID,
		params.RunRowID,
		params.AuditPacketArtifactRowID,
		params.AuditedCommit,
		params.PacketSHA256,
		params.Decision,
		params.Rationale,
	).Scan(
		&value.ID,
		&value.AuditDecisionID,
		&value.RunRowID,
		&value.AuditPacketArtifactRowID,
		&value.AuditedCommit,
		&value.PacketSHA256,
		&value.Decision,
		&value.Rationale,
		&value.CreatedAt,
	)
	return value, err
}

func noRowsAsConflict(err error, operation string) error {
	if err == nil {
		return nil
	}
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s did not match the expected current state", operation)
	}
	return err
}
