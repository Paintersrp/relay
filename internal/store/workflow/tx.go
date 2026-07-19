package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Tx struct {
	tx *sql.Tx
}

func (tx *Tx) CreateOperationPacketArtifact(ctx context.Context, params CreateOperationPacketArtifactParams) (OperationPacketArtifact, error) {
	return scanOperationPacketArtifact(tx.tx.QueryRowContext(ctx, `
INSERT INTO operation_packet_artifacts (
    artifact_id,
    kind,
    relative_path,
    media_type,
    sha256,
    size_bytes
)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING `+operationPacketArtifactColumns,
		params.ArtifactID,
		params.Kind,
		params.RelativePath,
		params.MediaType,
		params.SHA256,
		params.SizeBytes,
	))
}

func (tx *Tx) CreateOperationPacket(ctx context.Context, params CreateOperationPacketParams) (OperationPacket, error) {
	return scanOperationPacket(tx.tx.QueryRowContext(ctx, `
INSERT INTO operation_packets (
    packet_id, packet_sha256, schema_version, role, operation_id,
    surface_contract_id, project_id, readiness_state, prior_packet_row_id,
    coordinated_publication_id, created_at, packet_artifact_row_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING `+operationPacketColumns,
		params.PacketID, params.PacketSHA256, params.SchemaVersion, params.Role,
		params.OperationID, params.SurfaceContractID, params.ProjectID,
		params.ReadinessState, params.PriorPacketRowID, params.CoordinatedPublicationID,
		params.CreatedAt, params.PacketArtifactRowID,
	))
}

func (tx *Tx) GetOperationPacketByPacketID(ctx context.Context, packetID string) (OperationPacket, error) {
	return getOperationPacketByPacketID(ctx, tx.tx, packetID)
}

func (tx *Tx) GetOperationPacketByRowID(ctx context.Context, rowID int64) (OperationPacket, error) {
	return getOperationPacketByRowID(ctx, tx.tx, rowID)
}

func (tx *Tx) GetOperationPacketArtifact(ctx context.Context, packetRowID int64) (OperationPacketArtifact, error) {
	return getOperationPacketArtifact(ctx, tx.tx, packetRowID)
}

func (tx *Tx) GetOperationPacketReplacement(ctx context.Context, packetRowID int64) (OperationPacketReplacement, error) {
	return getOperationPacketReplacement(ctx, tx.tx, packetRowID)
}

func (tx *Tx) AttachOperationPacketDependency(ctx context.Context, params AttachOperationPacketDependencyParams) (OperationPacketRetentionDependency, error) {
	return scanOperationPacketDependency(tx.tx.QueryRowContext(ctx, `
INSERT INTO operation_packet_retention_dependencies (
    packet_row_id, dependency_class, dependency_key, required, attached, retained, owner_identity
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING `+operationPacketDependencyColumns,
		params.PacketRowID, params.DependencyClass, params.DependencyKey,
		operationPacketDependencyBool(params.Required), operationPacketDependencyBool(params.Attached),
		operationPacketDependencyBool(params.Retained), params.OwnerIdentity,
	))
}

func (tx *Tx) UpdateOperationPacketDependencyAvailability(ctx context.Context, params UpdateOperationPacketDependencyAvailabilityParams) (OperationPacketRetentionDependency, error) {
	return scanOperationPacketDependency(tx.tx.QueryRowContext(ctx, `
UPDATE operation_packet_retention_dependencies
SET attached = ?, retained = ?, owner_identity = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE packet_row_id = ? AND dependency_class = ? AND dependency_key = ?
RETURNING `+operationPacketDependencyColumns,
		operationPacketDependencyBool(params.Attached), operationPacketDependencyBool(params.Retained), params.OwnerIdentity,
		params.PacketRowID, params.DependencyClass, params.DependencyKey,
	))
}

func (tx *Tx) SupersedeOperationPacket(ctx context.Context, params SupersedeOperationPacketParams) (OperationPacket, error) {
	return scanOperationPacket(tx.tx.QueryRowContext(ctx, `
UPDATE operation_packets
SET lifecycle_state = 'superseded', replacement_packet_row_id = ?, superseded_at = ?
WHERE packet_id = ? AND readiness_state = 'ready' AND lifecycle_state = 'active'
  AND replacement_packet_row_id IS NULL AND superseded_at IS NULL AND closed_at IS NULL
RETURNING `+operationPacketColumns,
		params.ReplacementPacketRowID, params.SupersededAt, params.PacketID,
	))
}

func (tx *Tx) CloseOperationPacket(ctx context.Context, params CloseOperationPacketParams) (OperationPacket, error) {
	return scanOperationPacket(tx.tx.QueryRowContext(ctx, `
UPDATE operation_packets
SET lifecycle_state = 'closed', closed_at = ?
WHERE packet_id = ? AND readiness_state = 'ready' AND lifecycle_state = 'active'
  AND replacement_packet_row_id IS NULL AND superseded_at IS NULL AND closed_at IS NULL
RETURNING `+operationPacketColumns,
		params.ClosedAt, params.PacketID,
	))
}

func (tx *Tx) ListOperationPacketRetentionDependencies(ctx context.Context, packetRowID int64) ([]OperationPacketRetentionDependency, error) {
	return listOperationPacketRetentionDependencies(ctx, tx.tx, packetRowID)
}

func (tx *Tx) GetOperationPacketRetentionDependency(ctx context.Context, packetRowID int64, dependencyClass, dependencyKey string) (OperationPacketRetentionDependency, error) {
	return getOperationPacketRetentionDependency(ctx, tx.tx, packetRowID, dependencyClass, dependencyKey)
}

func (tx *Tx) CreateRepositoryTarget(ctx context.Context, repoTarget, localPath string) (RepositoryTarget, error) {
	return tx.CreateRepositoryTargetWithConfiguration(ctx, CreateRepositoryTargetParams{
		RepoTarget: repoTarget,
		LocalPath:  localPath,
	})
}

func (tx *Tx) CreateRepositoryTargetWithConfiguration(
	ctx context.Context,
	params CreateRepositoryTargetParams,
) (RepositoryTarget, error) {
	return scanRepositoryTarget(tx.tx.QueryRowContext(ctx, `
INSERT INTO repository_targets (
    repo_target,
    local_path,
    configured_branch_ref,
    configuration_version
)
VALUES (?, ?, ?, 1)
RETURNING `+repositoryTargetColumns,
		params.RepoTarget,
		params.LocalPath,
		params.ConfiguredBranchRef,
	))
}

func (tx *Tx) ConfigureRepositoryTarget(
	ctx context.Context,
	params ConfigureRepositoryTargetParams,
) (RepositoryTarget, error) {
	if params.ExpectedConfigurationVersion < 1 {
		return RepositoryTarget{}, fmt.Errorf("expected repository configuration version must be positive")
	}
	if params.ConfiguredBranchRef == "" {
		return RepositoryTarget{}, fmt.Errorf("configured branch ref is required")
	}
	return scanRepositoryTarget(tx.tx.QueryRowContext(ctx, `
UPDATE repository_targets
SET
    configured_branch_ref = ?,
    configuration_version = configuration_version + 1,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE repo_target = ? COLLATE NOCASE
  AND configuration_version = ?
RETURNING `+repositoryTargetColumns,
		params.ConfiguredBranchRef,
		params.RepoTarget,
		params.ExpectedConfigurationVersion,
	))
}

func (tx *Tx) GetRepositoryTarget(ctx context.Context, repoTarget string) (RepositoryTarget, error) {
	return getRepositoryTarget(ctx, tx.tx, repoTarget)
}

func (tx *Tx) GetRepositoryTargetByLocalPath(ctx context.Context, localPath string) (RepositoryTarget, error) {
	return getRepositoryTargetByLocalPath(ctx, tx.tx, localPath)
}

func (tx *Tx) CreatePlan(ctx context.Context, params CreatePlanParams) (Plan, error) {
	var value Plan
	err := tx.tx.QueryRowContext(ctx, `
INSERT INTO plans (project_row_id, plan_id, feature_slug, status, canonical_sha256)
VALUES (?, ?, ?, 'active', ?)
RETURNING id, project_row_id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at`,
		params.ProjectRowID,
		params.PlanID,
		params.FeatureSlug,
		params.CanonicalSHA256,
	).Scan(
		&value.ID,
		&value.ProjectRowID,
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
RETURNING id, project_row_id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at`, planRowID).Scan(
		&value.ID,
		&value.ProjectRowID,
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
          created_at, updated_at, completed_at, execution_package_row_id`,
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
		&value.ExecutionPackageRowID,
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
          created_at, updated_at, completed_at, execution_package_row_id`,
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
		&value.ExecutionPackageRowID,
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

func (tx *Tx) RequestExecutionAttemptCancellation(ctx context.Context, runRowID int64, attemptID string) (ExecutionAttempt, error) {
	var value ExecutionAttempt
	err := tx.tx.QueryRowContext(ctx, `
UPDATE execution_attempts
SET cancellation_requested_at = COALESCE(cancellation_requested_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE attempt_id = ? AND run_row_id = ? AND status IN ('pending', 'running')
RETURNING id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
          created_at, started_at, finished_at, cancellation_requested_at`,
		attemptID,
		runRowID,
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

func (tx *Tx) GetLatestSucceededExecutionAttempt(ctx context.Context, runRowID int64) (ExecutionAttempt, error) {
	return getLatestSucceededExecutionAttempt(ctx, tx.tx, runRowID)
}

func (tx *Tx) GetLatestSucceededExecutionAttemptOptional(ctx context.Context, runRowID int64) (ExecutionAttempt, bool, error) {
	attempt, err := getLatestSucceededExecutionAttempt(ctx, tx.tx, runRowID)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionAttempt{}, false, nil
	}
	if err != nil {
		return ExecutionAttempt{}, false, err
	}
	return attempt, true, nil
}

func (tx *Tx) ListArtifactsByRun(ctx context.Context, runRowID int64) ([]Artifact, error) {
	return listArtifacts(ctx, tx.tx, "run_row_id", runRowID)
}

func (tx *Tx) ListArtifactsByExecutionAttempt(ctx context.Context, attemptRowID int64) ([]Artifact, error) {
	return listArtifacts(ctx, tx.tx, "execution_attempt_row_id", attemptRowID)
}

func (tx *Tx) CreateAuditPacket(ctx context.Context, params CreateAuditPacketParams) (AuditPacket, error) {
	actorKind := params.ImplementationActorKind
	if actorKind == "" && params.ExecutionAttemptRowID.Valid {
		actorKind = ImplementationActorExecutor
	}
	return scanAuditPacket(tx.tx.QueryRowContext(ctx, `
INSERT INTO audit_packets (
    audit_packet_id,
    run_row_id,
    implementation_actor_kind,
    execution_attempt_row_id,
    artifact_row_id,
    base_commit,
    audited_commit,
    packet_sha256,
    status
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'current')
RETURNING id, audit_packet_id, run_row_id, implementation_actor_kind, execution_attempt_row_id, artifact_row_id,
          base_commit, audited_commit, packet_sha256, status, stale_reason,
          created_at, superseded_at`,
		params.AuditPacketID,
		params.RunRowID,
		actorKind,
		params.ExecutionAttemptRowID,
		params.ArtifactRowID,
		params.BaseCommit,
		params.AuditedCommit,
		params.PacketSHA256,
	))
}

func (tx *Tx) GetAuditPacketByPacketID(ctx context.Context, packetID string) (AuditPacket, error) {
	return getAuditPacketByPacketID(ctx, tx.tx, packetID)
}

func (tx *Tx) GetCurrentAuditPacketByRun(ctx context.Context, runRowID int64) (AuditPacket, error) {
	return getCurrentAuditPacketByRun(ctx, tx.tx, runRowID)
}

func (tx *Tx) MarkCurrentAuditPacketsStale(ctx context.Context, runRowID int64, reason string) error {
	if reason == "" {
		return fmt.Errorf("audit packet stale reason is required")
	}
	_, err := tx.tx.ExecContext(ctx, `
UPDATE audit_packets
SET
    status = 'stale',
    stale_reason = ?,
    superseded_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_row_id = ? AND status = 'current'`, reason, runRowID)
	return err
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

func (tx *Tx) GetArtifactByRowID(ctx context.Context, rowID int64) (Artifact, error) {
	return getArtifactByRowID(ctx, tx.tx, rowID)
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
func (tx *Tx) GetOrCreateSourceVault(ctx context.Context, params CreateSourceVaultParams) (SourceVault, error) {
	if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO source_vaults (vault_id, repo_target, relative_path)
VALUES (?, ?, ?)
ON CONFLICT(repo_target) DO NOTHING`, params.VaultID, params.RepoTarget, params.RelativePath); err != nil {
		return SourceVault{}, err
	}
	return tx.GetSourceVaultByRepositoryTarget(ctx, params.RepoTarget)
}

func (tx *Tx) AcquireSourceVaultClosure(
	ctx context.Context,
	params AcquireSourceVaultClosureParams,
) (SourceVaultClosureAcquisition, error) {
	current, err := scanSourceVaultClosure(tx.tx.QueryRowContext(ctx, `
SELECT `+sourceVaultClosureColumns+`
FROM source_vault_closures
WHERE vault_row_id = ? AND commit_oid = ? AND tree_oid = ? AND state <> 'released'
LIMIT 1`, params.VaultRowID, params.CommitOID, params.TreeOID))
	if err == nil {
		switch current.State {
		case SourceVaultClosureStateReady:
			return SourceVaultClosureAcquisition{Closure: current, Disposition: SourceVaultClosureAcquisitionReady}, nil
		case SourceVaultClosureStateImporting:
			return SourceVaultClosureAcquisition{Closure: current, Disposition: SourceVaultClosureAcquisitionImporting}, nil
		case SourceVaultClosureStateReleasing:
			return SourceVaultClosureAcquisition{Closure: current, Disposition: SourceVaultClosureAcquisitionReleasing}, nil
		case SourceVaultClosureStateUnavailable:
			retried, updateErr := scanSourceVaultClosure(tx.tx.QueryRowContext(ctx, `
UPDATE source_vault_closures
SET state = 'importing', failure_reason = NULL, verified_at = NULL,
    import_started_at = CASE WHEN import_started_at = ? THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') ELSE ? END,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE closure_id = ? AND state = 'unavailable'
RETURNING `+sourceVaultClosureColumns, params.StartedAt, params.StartedAt, current.ClosureID))
			if updateErr != nil {
				return SourceVaultClosureAcquisition{}, sourceVaultNoRows(updateErr, "retry source vault closure")
			}
			return SourceVaultClosureAcquisition{Closure: retried, Disposition: SourceVaultClosureAcquisitionRetry}, nil
		default:
			return SourceVaultClosureAcquisition{}, fmt.Errorf("unsupported current source vault closure state %q", current.State)
		}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SourceVaultClosureAcquisition{}, err
	}

	var generation int64
	if err := tx.tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(generation), 0) + 1
FROM source_vault_closures
WHERE vault_row_id = ? AND commit_oid = ? AND tree_oid = ?`,
		params.VaultRowID, params.CommitOID, params.TreeOID,
	).Scan(&generation); err != nil {
		return SourceVaultClosureAcquisition{}, err
	}
	created, err := scanSourceVaultClosure(tx.tx.QueryRowContext(ctx, `
INSERT INTO source_vault_closures (
    closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name,
    state, import_started_at
)
VALUES (?, ?, ?, ?, ?, ?, 'importing', ?)
RETURNING `+sourceVaultClosureColumns,
		params.ClosureID,
		params.VaultRowID,
		params.CommitOID,
		params.TreeOID,
		generation,
		params.RefName,
		params.StartedAt,
	))
	if err != nil {
		return SourceVaultClosureAcquisition{}, err
	}
	return SourceVaultClosureAcquisition{Closure: created, Disposition: SourceVaultClosureAcquisitionCreated}, nil
}

func (tx *Tx) TransitionSourceVaultClosure(
	ctx context.Context,
	params TransitionSourceVaultClosureParams,
) (SourceVaultClosure, error) {
	verifiedAt := sql.NullString{}
	releasedAt := sql.NullString{}
	if params.NextState == SourceVaultClosureStateReady {
		verifiedAt = sql.NullString{String: params.TransitionAt, Valid: true}
	}
	if params.NextState == SourceVaultClosureStateReleased {
		releasedAt = sql.NullString{String: params.TransitionAt, Valid: true}
	}
	value, err := scanSourceVaultClosure(tx.tx.QueryRowContext(ctx, `
UPDATE source_vault_closures
SET state = ?, failure_reason = ?, verified_at = ?, released_at = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE closure_id = ? AND state = ?
RETURNING `+sourceVaultClosureColumns,
		params.NextState,
		params.FailureReason,
		verifiedAt,
		releasedAt,
		params.ClosureID,
		params.ExpectedState,
	))
	return value, sourceVaultNoRows(err, "transition source vault closure")
}

func (tx *Tx) CreateOrGetSourceVaultRetention(
	ctx context.Context,
	params CreateSourceVaultRetentionParams,
) (SourceVaultRetention, error) {
	closure, err := tx.GetSourceVaultClosureByRowID(ctx, params.ClosureRowID)
	if err != nil {
		return SourceVaultRetention{}, err
	}
	if closure.State != SourceVaultClosureStateReady {
		return SourceVaultRetention{}, fmt.Errorf("source vault closure is not ready")
	}

	existing, err := tx.GetSourceVaultRetentionByOwnerEdge(ctx, params.ClosureRowID, params.OwnerClass, params.OwnerIdentity)
	if err == nil {
		if existing.State == SourceVaultRetentionStateActive {
			return existing, nil
		}
		return SourceVaultRetention{}, fmt.Errorf("%w: released source vault retention cannot be reactivated", ErrSourceVaultRetentionConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SourceVaultRetention{}, err
	}

	activeOwner, err := tx.GetActiveSourceVaultRetentionByOwner(ctx, params.OwnerClass, params.OwnerIdentity)
	if err == nil {
		if activeOwner.ClosureRowID == params.ClosureRowID {
			return activeOwner, nil
		}
		return SourceVaultRetention{}, fmt.Errorf("%w: active owner identity targets another source vault closure", ErrSourceVaultRetentionConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SourceVaultRetention{}, err
	}

	_, insertErr := tx.tx.ExecContext(ctx, `
INSERT INTO source_vault_retentions (
    retention_id, closure_row_id, owner_class, owner_identity, state
)
VALUES (?, ?, ?, ?, 'active')`,
		params.RetentionID,
		params.ClosureRowID,
		params.OwnerClass,
		params.OwnerIdentity,
	)
	if insertErr == nil {
		return tx.GetSourceVaultRetentionByOwnerEdge(ctx, params.ClosureRowID, params.OwnerClass, params.OwnerIdentity)
	}

	existing, exactErr := tx.GetSourceVaultRetentionByOwnerEdge(ctx, params.ClosureRowID, params.OwnerClass, params.OwnerIdentity)
	if exactErr == nil {
		if existing.State == SourceVaultRetentionStateActive {
			return existing, nil
		}
		return SourceVaultRetention{}, fmt.Errorf("%w: released source vault retention cannot be reactivated", ErrSourceVaultRetentionConflict)
	}
	if !errors.Is(exactErr, sql.ErrNoRows) {
		return SourceVaultRetention{}, exactErr
	}
	activeOwner, ownerErr := tx.GetActiveSourceVaultRetentionByOwner(ctx, params.OwnerClass, params.OwnerIdentity)
	if ownerErr == nil {
		if activeOwner.ClosureRowID == params.ClosureRowID {
			return activeOwner, nil
		}
		return SourceVaultRetention{}, fmt.Errorf("%w: active owner identity targets another source vault closure", ErrSourceVaultRetentionConflict)
	}
	if !errors.Is(ownerErr, sql.ErrNoRows) {
		return SourceVaultRetention{}, ownerErr
	}
	return SourceVaultRetention{}, insertErr
}

func (tx *Tx) ReleaseSourceVaultRetention(
	ctx context.Context,
	params ReleaseSourceVaultRetentionParams,
) (SourceVaultRetention, error) {
	value, err := scanSourceVaultRetention(tx.tx.QueryRowContext(ctx, `
UPDATE source_vault_retentions
SET state = 'released', released_at = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE retention_id = ? AND state = 'active'
RETURNING `+sourceVaultRetentionColumns,
		params.ReleasedAt,
		params.RetentionID,
	))
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SourceVaultRetention{}, err
	}
	winner, readErr := tx.GetSourceVaultRetentionByRetentionID(ctx, params.RetentionID)
	if readErr != nil {
		return SourceVaultRetention{}, readErr
	}
	if winner.State == SourceVaultRetentionStateReleased {
		return winner, nil
	}
	return SourceVaultRetention{}, fmt.Errorf("%w: release source vault retention", ErrSourceVaultStateConflict)
}

func (tx *Tx) BeginSourceVaultClosureRelease(ctx context.Context, closureID, transitionAt string) (SourceVaultClosure, error) {
	closure, err := tx.GetSourceVaultClosureByClosureID(ctx, closureID)
	if err != nil {
		return SourceVaultClosure{}, err
	}
	if closure.State != SourceVaultClosureStateReady && closure.State != SourceVaultClosureStateUnavailable {
		return SourceVaultClosure{}, fmt.Errorf("%w: source vault closure is not cleanup eligible", ErrSourceVaultCleanupBlocked)
	}
	count, err := tx.CountActiveSourceVaultRetentions(ctx, closure.ID)
	if err != nil {
		return SourceVaultClosure{}, err
	}
	if count != 0 {
		return SourceVaultClosure{}, fmt.Errorf("%w: source vault closure still has active retentions", ErrSourceVaultCleanupBlocked)
	}
	return tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
		ClosureID:     closureID,
		ExpectedState: closure.State,
		NextState:     SourceVaultClosureStateReleasing,
		TransitionAt:  transitionAt,
	})
}
