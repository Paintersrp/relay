package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	workflowartifacts "relay/internal/artifacts/workflow"
	relaydb "relay/internal/db"

	_ "modernc.org/sqlite"
)

type Store struct {
	db        *sql.DB
	artifacts *workflowartifacts.Store
}

func Open(dbPath, artifactRoot string) (*Store, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("workflow database path is required")
	}
	if !strings.HasPrefix(dbPath, "file:") && dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, fmt.Errorf("create workflow database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open workflow database: %w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = db.Close()
		}
	}()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping workflow database: %w", err)
	}
	if err := relaydb.AutoMigrateWorkflow(db); err != nil {
		return nil, fmt.Errorf("migrate workflow database: %w", err)
	}
	db.SetMaxOpenConns(1)

	artifactStore, err := workflowartifacts.New(artifactRoot)
	if err != nil {
		return nil, err
	}
	closeOnError = false
	return &Store{db: db, artifacts: artifactStore}, nil
}

func New(db *sql.DB, artifactStore *workflowartifacts.Store) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("workflow database is required")
	}
	if artifactStore == nil {
		return nil, fmt.Errorf("workflow artifact store is required")
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enable workflow foreign keys: %w", err)
	}
	return &Store{db: db, artifacts: artifactStore}, nil
}

func sqliteDSN(path string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "_journal_mode=WAL&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) ArtifactStore() *workflowartifacts.Store {
	return s.artifacts
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) (err error) {
	if fn == nil {
		return fmt.Errorf("workflow transaction callback is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin workflow transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback workflow transaction: %w", rollbackErr))
		}
	}()
	if err := fn(&Tx{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *Store) CommitArtifactBatch(ctx context.Context, batch *workflowartifacts.Batch, fn func(*Tx) error) (err error) {
	if batch == nil {
		return fmt.Errorf("artifact batch is required")
	}
	if fn == nil {
		_ = batch.Rollback()
		return fmt.Errorf("workflow transaction callback is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		_ = batch.Rollback()
		return fmt.Errorf("begin workflow transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback workflow transaction: %w", rollbackErr))
		}
		if artifactErr := batch.Rollback(); artifactErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback workflow artifacts: %w", artifactErr))
		}
	}()

	if err := fn(&Tx{tx: tx}); err != nil {
		return err
	}
	if err := batch.Promote(); err != nil {
		return fmt.Errorf("promote workflow artifacts: %w", err)
	}
	if err := batch.PrepareCommit(); err != nil {
		return fmt.Errorf("prepare workflow artifacts: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow transaction: %w", err)
	}
	batch.Commit()
	committed = true
	return nil
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type rowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Store) GetRepositoryTarget(ctx context.Context, repoTarget string) (RepositoryTarget, error) {
	return getRepositoryTarget(ctx, s.db, repoTarget)
}

func (s *Store) GetRepositoryTargetByLocalPath(ctx context.Context, localPath string) (RepositoryTarget, error) {
	return getRepositoryTargetByLocalPath(ctx, s.db, localPath)
}
func (s *Store) GetSourceVaultByVaultID(ctx context.Context, vaultID string) (SourceVault, error) {
	return getSourceVaultByVaultID(ctx, s.db, vaultID)
}

func (s *Store) GetSourceVaultByRepositoryTarget(ctx context.Context, repoTarget string) (SourceVault, error) {
	return getSourceVaultByRepositoryTarget(ctx, s.db, repoTarget)
}

func (s *Store) GetSourceVaultClosureByClosureID(ctx context.Context, closureID string) (SourceVaultClosure, error) {
	return getSourceVaultClosureByClosureID(ctx, s.db, closureID)
}

func (s *Store) GetCurrentSourceVaultClosureByIdentity(
	ctx context.Context,
	vaultRowID int64,
	commitOID string,
	treeOID string,
) (SourceVaultClosure, error) {
	return getCurrentSourceVaultClosureByIdentity(ctx, s.db, vaultRowID, commitOID, treeOID)
}

func (s *Store) ListSourceVaultClosuresByIdentity(
	ctx context.Context,
	vaultRowID int64,
	commitOID string,
	treeOID string,
) ([]SourceVaultClosure, error) {
	return listSourceVaultClosuresByIdentity(ctx, s.db, vaultRowID, commitOID, treeOID)
}

func (s *Store) ListSourceVaultClosuresForReconciliation(ctx context.Context) ([]SourceVaultClosure, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+sourceVaultClosureColumns+`
FROM source_vault_closures
ORDER BY vault_row_id, commit_oid, tree_oid, generation, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSourceVaultClosures(rows)
}

func (s *Store) GetSourceVaultRetentionByRetentionID(ctx context.Context, retentionID string) (SourceVaultRetention, error) {
	return getSourceVaultRetentionByRetentionID(ctx, s.db, retentionID)
}

func (s *Store) GetActiveSourceVaultRetentionByOwner(
	ctx context.Context,
	ownerClass string,
	ownerIdentity string,
) (SourceVaultRetention, error) {
	return getActiveSourceVaultRetentionByOwner(ctx, s.db, ownerClass, ownerIdentity)
}

func (s *Store) ListSourceVaultRetentions(ctx context.Context, closureRowID int64) ([]SourceVaultRetention, error) {
	return listSourceVaultRetentions(ctx, s.db, closureRowID)
}

func (s *Store) CountActiveSourceVaultRetentions(ctx context.Context, closureRowID int64) (int64, error) {
	return countActiveSourceVaultRetentions(ctx, s.db, closureRowID)
}

func (s *Store) ListRepositoryTargetsWithConfiguration(ctx context.Context) ([]RepositoryTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+repositoryTargetColumns+`
FROM repository_targets
ORDER BY repo_target COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]RepositoryTarget, 0)
	for rows.Next() {
		value, err := scanRepositoryTarget(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetPlanByPlanID(ctx context.Context, planID string) (Plan, error) {
	return getPlanByPlanID(ctx, s.db, planID)
}

func (s *Store) GetPlanByRowID(ctx context.Context, rowID int64) (Plan, error) {
	return getPlanByRowID(ctx, s.db, rowID)
}

func (s *Store) GetPlanPassByPassID(ctx context.Context, passID string) (PlanPass, error) {
	return getPlanPassByPassID(ctx, s.db, passID)
}

func (s *Store) GetPlanPassByRowID(ctx context.Context, rowID int64) (PlanPass, error) {
	return getPlanPassByRowID(ctx, s.db, rowID)
}

func (s *Store) GetPlanPassByPlanAndNumber(ctx context.Context, planRowID, passNumber int64) (PlanPass, error) {
	return getPlanPassByPlanAndNumber(ctx, s.db, planRowID, passNumber)
}

func (s *Store) GetRunByRunID(ctx context.Context, runID string) (Run, error) {
	return getRunByRunID(ctx, s.db, runID)
}

func (s *Store) GetRunByRowID(ctx context.Context, rowID int64) (Run, error) {
	return getRunByRowID(ctx, s.db, rowID)
}

func (s *Store) GetExecutionAttemptByAttemptID(ctx context.Context, attemptID string) (ExecutionAttempt, error) {
	return getExecutionAttemptByAttemptID(ctx, s.db, attemptID)
}

func (s *Store) GetArtifactByArtifactID(ctx context.Context, artifactID string) (Artifact, error) {
	return getArtifactByArtifactID(ctx, s.db, artifactID)
}

func (s *Store) GetArtifactByRowID(ctx context.Context, rowID int64) (Artifact, error) {
	return getArtifactByRowID(ctx, s.db, rowID)
}

func (s *Store) GetCurrentAuditPacketByRun(ctx context.Context, runRowID int64) (AuditPacket, error) {
	return getCurrentAuditPacketByRun(ctx, s.db, runRowID)
}

func (s *Store) GetLatestAuditPacketByRun(ctx context.Context, runRowID int64) (AuditPacket, error) {
	return getLatestAuditPacketByRun(ctx, s.db, runRowID)
}

func (s *Store) GetAuditDecisionByDecisionID(ctx context.Context, decisionID string) (AuditDecision, error) {
	return getAuditDecisionByDecisionID(ctx, s.db, decisionID)
}

func (s *Store) GetAuditDecisionByRun(ctx context.Context, runRowID int64) (AuditDecision, error) {
	return getAuditDecisionByRun(ctx, s.db, runRowID)
}

func (s *Store) ListPlanPasses(ctx context.Context, planRowID int64) ([]PlanPass, error) {
	return listPlanPasses(ctx, s.db, planRowID)
}

func (s *Store) ListArtifactsByPlan(ctx context.Context, planRowID int64) ([]Artifact, error) {
	return listArtifacts(ctx, s.db, "plan_row_id", planRowID)
}

func (s *Store) ListArtifactsByRun(ctx context.Context, runRowID int64) ([]Artifact, error) {
	return listArtifacts(ctx, s.db, "run_row_id", runRowID)
}

func (s *Store) ListArtifactsByExecutionAttempt(ctx context.Context, attemptRowID int64) ([]Artifact, error) {
	return listArtifacts(ctx, s.db, "execution_attempt_row_id", attemptRowID)
}

func (s *Store) GetLatestSucceededExecutionAttempt(ctx context.Context, runRowID int64) (ExecutionAttempt, error) {
	return getLatestSucceededExecutionAttempt(ctx, s.db, runRowID)
}

func (s *Store) GetLatestSucceededExecutionAttemptOptional(ctx context.Context, runRowID int64) (ExecutionAttempt, bool, error) {
	attempt, err := getLatestSucceededExecutionAttempt(ctx, s.db, runRowID)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionAttempt{}, false, nil
	}
	if err != nil {
		return ExecutionAttempt{}, false, err
	}
	return attempt, true, nil
}

func (s *Store) MarkCurrentAuditPacketsStale(ctx context.Context, runRowID int64, reason string) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return tx.MarkCurrentAuditPacketsStale(ctx, runRowID, reason)
	})
}

func (s *Store) ListExecutionAttemptsByRun(ctx context.Context, runRowID int64) ([]ExecutionAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
       created_at, started_at, finished_at, cancellation_requested_at
FROM execution_attempts
WHERE run_row_id = ?
ORDER BY attempt_number`, runRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []ExecutionAttempt
	for rows.Next() {
		var value ExecutionAttempt
		if err := rows.Scan(
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
		); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

const repositoryTargetColumns = `
repo_target,
local_path,
configured_branch_ref,
configuration_version,
created_at,
updated_at`

func getRepositoryTarget(ctx context.Context, queryer rowQueryer, repoTarget string) (RepositoryTarget, error) {
	return scanRepositoryTarget(queryer.QueryRowContext(ctx, `
SELECT `+repositoryTargetColumns+`
FROM repository_targets
WHERE repo_target = ? COLLATE NOCASE`, repoTarget))
}

func getRepositoryTargetByLocalPath(
	ctx context.Context,
	queryer rowQueryer,
	localPath string,
) (RepositoryTarget, error) {
	query := `
SELECT ` + repositoryTargetColumns + `
FROM repository_targets
WHERE local_path = ?`
	if runtime.GOOS == "windows" {
		query += " COLLATE NOCASE"
	}
	return scanRepositoryTarget(queryer.QueryRowContext(ctx, query, localPath))
}

func getPlanByRowID(ctx context.Context, queryer rowQueryer, rowID int64) (Plan, error) {
	var value Plan
	err := queryer.QueryRowContext(ctx, `
SELECT id, project_row_id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at
FROM plans
WHERE id = ?`, rowID).Scan(
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

func getPlanByPlanID(ctx context.Context, queryer rowQueryer, planID string) (Plan, error) {
	var value Plan
	err := queryer.QueryRowContext(ctx, `
SELECT id, project_row_id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at
FROM plans
WHERE plan_id = ?`, planID).Scan(
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

func getPlanPassByPassID(ctx context.Context, queryer rowQueryer, passID string) (PlanPass, error) {
	return scanPlanPass(queryer.QueryRowContext(ctx, `
SELECT id, pass_id, plan_row_id, pass_number, name, repo_target, status, created_at, updated_at, started_at, completed_at
FROM plan_passes
WHERE pass_id = ?`, passID))
}

func getPlanPassByRowID(ctx context.Context, queryer rowQueryer, rowID int64) (PlanPass, error) {
	return scanPlanPass(queryer.QueryRowContext(ctx, `
SELECT id, pass_id, plan_row_id, pass_number, name, repo_target, status, created_at, updated_at, started_at, completed_at
FROM plan_passes
WHERE id = ?`, rowID))
}

func getPlanPassByPlanAndNumber(ctx context.Context, queryer rowQueryer, planRowID, passNumber int64) (PlanPass, error) {
	return scanPlanPass(queryer.QueryRowContext(ctx, `
SELECT id, pass_id, plan_row_id, pass_number, name, repo_target, status, created_at, updated_at, started_at, completed_at
FROM plan_passes
WHERE plan_row_id = ? AND pass_number = ?`, planRowID, passNumber))
}

func getRunByRunID(ctx context.Context, queryer rowQueryer, runID string) (Run, error) {
	var value Run
	err := queryer.QueryRowContext(ctx, `
SELECT id, run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id, remediates_run_row_id,
       status, branch, base_commit, canonical_sha256, created_at, updated_at, completed_at
FROM runs
WHERE run_id = ?`, runID).Scan(
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

func getRunByRowID(ctx context.Context, queryer rowQueryer, rowID int64) (Run, error) {
	var value Run
	err := queryer.QueryRowContext(ctx, `
SELECT id, run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id, remediates_run_row_id,
       status, branch, base_commit, canonical_sha256, created_at, updated_at, completed_at
FROM runs
WHERE id = ?`, rowID).Scan(
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

func getExecutionAttemptByAttemptID(ctx context.Context, queryer rowQueryer, attemptID string) (ExecutionAttempt, error) {
	var value ExecutionAttempt
	err := queryer.QueryRowContext(ctx, `
SELECT id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
       created_at, started_at, finished_at, cancellation_requested_at
FROM execution_attempts
WHERE attempt_id = ?`, attemptID).Scan(
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

func getArtifactByRowID(ctx context.Context, queryer rowQueryer, rowID int64) (Artifact, error) {
	return scanArtifact(queryer.QueryRowContext(ctx, `
SELECT id, artifact_id, owner_type, plan_row_id, run_row_id, execution_attempt_row_id,
       kind, relative_path, media_type, sha256, size_bytes, created_at
FROM artifacts
WHERE id = ?`, rowID))
}

func getArtifactByArtifactID(ctx context.Context, queryer rowQueryer, artifactID string) (Artifact, error) {
	return scanArtifact(queryer.QueryRowContext(ctx, `
SELECT id, artifact_id, owner_type, plan_row_id, run_row_id, execution_attempt_row_id,
       kind, relative_path, media_type, sha256, size_bytes, created_at
FROM artifacts
WHERE artifact_id = ?`, artifactID))
}

func getLatestSucceededExecutionAttempt(ctx context.Context, queryer rowQueryer, runRowID int64) (ExecutionAttempt, error) {
	var value ExecutionAttempt
	err := queryer.QueryRowContext(ctx, `
SELECT id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
       created_at, started_at, finished_at, cancellation_requested_at
FROM execution_attempts
WHERE run_row_id = ? AND status = 'succeeded'
ORDER BY attempt_number DESC
LIMIT 1`, runRowID).Scan(
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

func getAuditPacketByPacketID(ctx context.Context, queryer rowQueryer, packetID string) (AuditPacket, error) {
	return scanAuditPacket(queryer.QueryRowContext(ctx, `
	SELECT id, audit_packet_id, run_row_id, implementation_actor_kind, execution_attempt_row_id, artifact_row_id,
       base_commit, audited_commit, packet_sha256, status, stale_reason,
       created_at, superseded_at
FROM audit_packets
WHERE audit_packet_id = ?`, packetID))
}

func getCurrentAuditPacketByRun(ctx context.Context, queryer rowQueryer, runRowID int64) (AuditPacket, error) {
	return scanAuditPacket(queryer.QueryRowContext(ctx, `
	SELECT id, audit_packet_id, run_row_id, implementation_actor_kind, execution_attempt_row_id, artifact_row_id,
       base_commit, audited_commit, packet_sha256, status, stale_reason,
       created_at, superseded_at
FROM audit_packets
WHERE run_row_id = ? AND status = 'current'
LIMIT 1`, runRowID))
}

func getLatestAuditPacketByRun(ctx context.Context, queryer rowQueryer, runRowID int64) (AuditPacket, error) {
	return scanAuditPacket(queryer.QueryRowContext(ctx, `
	SELECT id, audit_packet_id, run_row_id, implementation_actor_kind, execution_attempt_row_id, artifact_row_id,
       base_commit, audited_commit, packet_sha256, status, stale_reason,
       created_at, superseded_at
FROM audit_packets
WHERE run_row_id = ?
ORDER BY id DESC
LIMIT 1`, runRowID))
}

func getAuditDecisionByRun(ctx context.Context, queryer rowQueryer, runRowID int64) (AuditDecision, error) {
	var value AuditDecision
	err := queryer.QueryRowContext(ctx, `
SELECT id, audit_decision_id, run_row_id, audit_packet_artifact_row_id,
       audited_commit, packet_sha256, decision, rationale, created_at
FROM audit_decisions
WHERE run_row_id = ?
ORDER BY id DESC
LIMIT 1`, runRowID).Scan(
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

func getAuditDecisionByDecisionID(ctx context.Context, queryer rowQueryer, decisionID string) (AuditDecision, error) {
	var value AuditDecision
	err := queryer.QueryRowContext(ctx, `
SELECT id, audit_decision_id, run_row_id, audit_packet_artifact_row_id,
       audited_commit, packet_sha256, decision, rationale, created_at
FROM audit_decisions
WHERE audit_decision_id = ?`, decisionID).Scan(
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

func listPlanPasses(ctx context.Context, queryer rowsQueryer, planRowID int64) ([]PlanPass, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, pass_id, plan_row_id, pass_number, name, repo_target, status, created_at, updated_at, started_at, completed_at
FROM plan_passes
WHERE plan_row_id = ?
ORDER BY pass_number`, planRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []PlanPass
	for rows.Next() {
		value, err := scanPlanPass(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func listArtifacts(ctx context.Context, queryer rowsQueryer, ownerColumn string, ownerRowID int64) ([]Artifact, error) {
	if ownerColumn != "plan_row_id" && ownerColumn != "run_row_id" && ownerColumn != "execution_attempt_row_id" {
		return nil, fmt.Errorf("unsupported artifact owner column %q", ownerColumn)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT id, artifact_id, owner_type, plan_row_id, run_row_id, execution_attempt_row_id,
       kind, relative_path, media_type, sha256, size_bytes, created_at
FROM artifacts
WHERE `+ownerColumn+` = ?
ORDER BY created_at, id`, ownerRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []Artifact
	for rows.Next() {
		value, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanRepositoryTarget(row rowScanner) (RepositoryTarget, error) {
	var value RepositoryTarget
	err := row.Scan(
		&value.RepoTarget,
		&value.LocalPath,
		&value.ConfiguredBranchRef,
		&value.ConfigurationVersion,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	return value, err
}

func scanAuditPacket(row rowScanner) (AuditPacket, error) {
	var value AuditPacket
	err := row.Scan(
		&value.ID,
		&value.AuditPacketID,
		&value.RunRowID,
		&value.ImplementationActorKind,
		&value.ExecutionAttemptRowID,
		&value.ArtifactRowID,
		&value.BaseCommit,
		&value.AuditedCommit,
		&value.PacketSHA256,
		&value.Status,
		&value.StaleReason,
		&value.CreatedAt,
		&value.SupersededAt,
	)
	return value, err
}

func scanPlanPass(row rowScanner) (PlanPass, error) {
	var value PlanPass
	err := row.Scan(
		&value.ID,
		&value.PassID,
		&value.PlanRowID,
		&value.PassNumber,
		&value.Name,
		&value.RepoTarget,
		&value.Status,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.StartedAt,
		&value.CompletedAt,
	)
	return value, err
}

func scanArtifact(row rowScanner) (Artifact, error) {
	var value Artifact
	err := row.Scan(
		&value.ID,
		&value.ArtifactID,
		&value.OwnerType,
		&value.PlanRowID,
		&value.RunRowID,
		&value.ExecutionAttemptRowID,
		&value.Kind,
		&value.RelativePath,
		&value.MediaType,
		&value.SHA256,
		&value.SizeBytes,
		&value.CreatedAt,
	)
	return value, err
}
