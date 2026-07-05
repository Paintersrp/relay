package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	return path + separator + "_journal_mode=WAL&_pragma=foreign_keys(1)"
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

func (s *Store) GetPlanByPlanID(ctx context.Context, planID string) (Plan, error) {
	return getPlanByPlanID(ctx, s.db, planID)
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

func (s *Store) GetAuditDecisionByDecisionID(ctx context.Context, decisionID string) (AuditDecision, error) {
	return getAuditDecisionByDecisionID(ctx, s.db, decisionID)
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

func getRepositoryTarget(ctx context.Context, queryer rowQueryer, repoTarget string) (RepositoryTarget, error) {
	var value RepositoryTarget
	err := queryer.QueryRowContext(ctx, `
SELECT repo_target, local_path, created_at, updated_at
FROM repository_targets
WHERE repo_target = ? COLLATE NOCASE`, repoTarget).Scan(
		&value.RepoTarget,
		&value.LocalPath,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	return value, err
}

func getPlanByPlanID(ctx context.Context, queryer rowQueryer, planID string) (Plan, error) {
	var value Plan
	err := queryer.QueryRowContext(ctx, `
SELECT id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at
FROM plans
WHERE plan_id = ?`, planID).Scan(
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

func getArtifactByArtifactID(ctx context.Context, queryer rowQueryer, artifactID string) (Artifact, error) {
	return scanArtifact(queryer.QueryRowContext(ctx, `
SELECT id, artifact_id, owner_type, plan_row_id, run_row_id, execution_attempt_row_id,
       kind, relative_path, media_type, sha256, size_bytes, created_at
FROM artifacts
WHERE artifact_id = ?`, artifactID))
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
	if ownerColumn != "plan_row_id" && ownerColumn != "run_row_id" {
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
