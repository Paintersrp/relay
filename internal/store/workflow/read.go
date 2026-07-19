package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	DefaultWorkflowListLimit = 50
	MaxWorkflowListLimit     = 100
	MaxWorkflowAttemptLimit  = 50
)

type PlanListQuery struct {
	Status       string
	ProjectRowID sql.NullInt64
	Limit        int
}

type RunListQuery struct {
	Status        string
	PlanRowID     sql.NullInt64
	PlanPassRowID sql.NullInt64
	Limit         int
}

func normalizeWorkflowListLimit(value int) int {
	if value <= 0 {
		return DefaultWorkflowListLimit
	}
	if value > MaxWorkflowListLimit {
		return MaxWorkflowListLimit
	}
	return value
}

func (s *Store) ListRepositoryTargets(ctx context.Context) ([]RepositoryTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT repo_target, local_path, created_at, updated_at
FROM repository_targets
ORDER BY repo_target COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]RepositoryTarget, 0)
	for rows.Next() {
		var value RepositoryTarget
		if err := rows.Scan(&value.RepoTarget, &value.LocalPath, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListPlans(ctx context.Context, query PlanListQuery) ([]Plan, error) {
	var sqlText strings.Builder
	sqlText.WriteString(`
SELECT id, project_row_id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at
FROM plans`)
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 3)
	if strings.TrimSpace(query.Status) != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, strings.TrimSpace(query.Status))
	}
	if query.ProjectRowID.Valid {
		conditions = append(conditions, "project_row_id = ?")
		args = append(args, query.ProjectRowID.Int64)
	}
	if len(conditions) > 0 {
		sqlText.WriteString(" WHERE ")
		sqlText.WriteString(strings.Join(conditions, " AND "))
	}
	sqlText.WriteString(" ORDER BY id DESC LIMIT ?")
	args = append(args, normalizeWorkflowListLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]Plan, 0)
	for rows.Next() {
		value, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListPlanRepositoryTargets(ctx context.Context, planRowID int64) ([]PlanRepositoryTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, plan_row_id, sequence, repo_target, branch, planning_base_commit, created_at
FROM plan_repository_targets
WHERE plan_row_id = ?
ORDER BY sequence`, planRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]PlanRepositoryTarget, 0)
	for rows.Next() {
		var value PlanRepositoryTarget
		if err := rows.Scan(
			&value.ID,
			&value.PlanRowID,
			&value.Sequence,
			&value.RepoTarget,
			&value.Branch,
			&value.PlanningBaseCommit,
			&value.CreatedAt,
		); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListPlanPassDependencies(ctx context.Context, planRowID int64) ([]PlanPassDependency, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT dependency.pass_row_id, dependency.depends_on_pass_row_id, dependency.created_at
FROM plan_pass_dependencies dependency
JOIN plan_passes pass ON pass.id = dependency.pass_row_id
JOIN plan_passes required_pass ON required_pass.id = dependency.depends_on_pass_row_id
WHERE pass.plan_row_id = ?
ORDER BY pass.pass_number, required_pass.pass_number`, planRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]PlanPassDependency, 0)
	for rows.Next() {
		var value PlanPassDependency
		if err := rows.Scan(&value.PassRowID, &value.DependsOnPassRowID, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListRuns(ctx context.Context, query RunListQuery) ([]Run, error) {
	var sqlText strings.Builder
	sqlText.WriteString(`
SELECT id, run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id, remediates_run_row_id,
       status, branch, base_commit, canonical_sha256, created_at, updated_at, completed_at,
       execution_package_row_id, package_approval_row_id
FROM runs`)
	conditions := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if strings.TrimSpace(query.Status) != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, strings.TrimSpace(query.Status))
	}
	if query.PlanRowID.Valid {
		conditions = append(conditions, "plan_row_id = ?")
		args = append(args, query.PlanRowID.Int64)
	}
	if query.PlanPassRowID.Valid {
		conditions = append(conditions, "plan_pass_row_id = ?")
		args = append(args, query.PlanPassRowID.Int64)
	}
	if len(conditions) > 0 {
		sqlText.WriteString(" WHERE ")
		sqlText.WriteString(strings.Join(conditions, " AND "))
	}
	sqlText.WriteString(" ORDER BY id DESC LIMIT ?")
	args = append(args, normalizeWorkflowListLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]Run, 0)
	for rows.Next() {
		value, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListRecentExecutionAttemptsByRun(ctx context.Context, runRowID int64, limit int) ([]ExecutionAttempt, error) {
	if limit <= 0 || limit > MaxWorkflowAttemptLimit {
		limit = MaxWorkflowAttemptLimit
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
       created_at, started_at, finished_at, cancellation_requested_at
FROM (
    SELECT id, attempt_id, run_row_id, attempt_number, adapter, model, status, result_json,
           created_at, started_at, finished_at, cancellation_requested_at
    FROM execution_attempts
    WHERE run_row_id = ?
    ORDER BY attempt_number DESC
    LIMIT ?
)
ORDER BY attempt_number`, runRowID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]ExecutionAttempt, 0)
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

func scanPlan(row rowScanner) (Plan, error) {
	var value Plan
	err := row.Scan(
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

func scanRun(row rowScanner) (Run, error) {
	var value Run
	err := row.Scan(
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
		&value.PackageApprovalRowID,
	)
	return value, err
}

func ValidateWorkflowListStatus(value string, allowed ...string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("unsupported status %q", value)
}
