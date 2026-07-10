package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type ProjectListQuery struct {
	Status string
	Limit  int
}

func (s *Store) GetProjectByProjectID(ctx context.Context, projectID string) (Project, error) {
	return getProjectByProjectID(ctx, s.db, projectID)
}

func (s *Store) GetProjectByRowID(ctx context.Context, rowID int64) (Project, error) {
	return getProjectByRowID(ctx, s.db, rowID)
}

func (s *Store) ListProjects(ctx context.Context, query ProjectListQuery) ([]Project, error) {
	var sqlText strings.Builder
	sqlText.WriteString(`
SELECT id, project_id, name, description, status, created_at, updated_at
FROM projects`)
	args := make([]any, 0, 2)
	if strings.TrimSpace(query.Status) != "" {
		sqlText.WriteString(" WHERE status = ?")
		args = append(args, strings.TrimSpace(query.Status))
	}
	sqlText.WriteString(" ORDER BY id DESC LIMIT ?")
	args = append(args, normalizeWorkflowListLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]Project, 0)
	for rows.Next() {
		value, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListProjectRepositoryTargets(ctx context.Context, projectRowID int64, limit int) ([]ProjectRepositoryTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT project_row_id, repo_target, created_at
FROM project_repository_targets
WHERE project_row_id = ?
ORDER BY repo_target COLLATE NOCASE
LIMIT ?`, projectRowID, normalizeWorkflowListLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]ProjectRepositoryTarget, 0)
	for rows.Next() {
		var value ProjectRepositoryTarget
		if err := rows.Scan(&value.ProjectRowID, &value.RepoTarget, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListProjectNotes(ctx context.Context, projectRowID int64, limit int) ([]ProjectNote, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, note_id, project_row_id, title, body, status, created_at, updated_at
FROM project_notes
WHERE project_row_id = ?
ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END, id DESC
LIMIT ?`, projectRowID, normalizeWorkflowListLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]ProjectNote, 0)
	for rows.Next() {
		value, err := scanProjectNote(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetProjectNoteByNoteID(ctx context.Context, noteID string) (ProjectNote, error) {
	return getProjectNoteByNoteID(ctx, s.db, noteID)
}

func (tx *Tx) GetProjectByProjectID(ctx context.Context, projectID string) (Project, error) {
	return getProjectByProjectID(ctx, tx.tx, projectID)
}

func (tx *Tx) GetProjectByRowID(ctx context.Context, rowID int64) (Project, error) {
	return getProjectByRowID(ctx, tx.tx, rowID)
}

func (tx *Tx) CreateProject(ctx context.Context, params CreateProjectParams) (Project, error) {
	return scanProject(tx.tx.QueryRowContext(ctx, `
INSERT INTO projects (project_id, name, description, status)
VALUES (?, ?, ?, 'active')
RETURNING id, project_id, name, description, status, created_at, updated_at`,
		params.ProjectID,
		params.Name,
		params.Description,
	))
}

func (tx *Tx) UpdateProject(ctx context.Context, projectID, name, description string) (Project, error) {
	return scanProject(tx.tx.QueryRowContext(ctx, `
UPDATE projects
SET name = ?, description = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE project_id = ?
RETURNING id, project_id, name, description, status, created_at, updated_at`,
		name,
		description,
		projectID,
	))
}

func (tx *Tx) TransitionProjectStatus(ctx context.Context, projectID, expectedStatus, nextStatus string) (Project, error) {
	return scanProject(tx.tx.QueryRowContext(ctx, `
UPDATE projects
SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE project_id = ? AND status = ?
RETURNING id, project_id, name, description, status, created_at, updated_at`,
		nextStatus,
		projectID,
		expectedStatus,
	))
}

func (tx *Tx) AttachProjectRepository(ctx context.Context, projectRowID int64, repoTarget string) (ProjectRepositoryTarget, error) {
	if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO project_repository_targets (project_row_id, repo_target)
VALUES (?, ?)
ON CONFLICT(project_row_id, repo_target) DO NOTHING`, projectRowID, repoTarget); err != nil {
		return ProjectRepositoryTarget{}, err
	}
	return getProjectRepositoryTarget(ctx, tx.tx, projectRowID, repoTarget)
}

func (tx *Tx) DetachProjectRepository(ctx context.Context, projectRowID int64, repoTarget string) error {
	result, err := tx.tx.ExecContext(ctx, `
DELETE FROM project_repository_targets
WHERE project_row_id = ? AND repo_target = ?`, projectRowID, repoTarget)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (tx *Tx) CreateProjectNote(ctx context.Context, params CreateProjectNoteParams) (ProjectNote, error) {
	return scanProjectNote(tx.tx.QueryRowContext(ctx, `
INSERT INTO project_notes (note_id, project_row_id, title, body, status)
VALUES (?, ?, ?, ?, 'open')
RETURNING id, note_id, project_row_id, title, body, status, created_at, updated_at`,
		params.NoteID,
		params.ProjectRowID,
		params.Title,
		params.Body,
	))
}

func (tx *Tx) GetProjectNoteByNoteID(ctx context.Context, noteID string) (ProjectNote, error) {
	return getProjectNoteByNoteID(ctx, tx.tx, noteID)
}

func (tx *Tx) UpdateProjectNote(ctx context.Context, params UpdateProjectNoteParams) (ProjectNote, error) {
	return scanProjectNote(tx.tx.QueryRowContext(ctx, `
UPDATE project_notes
SET title = ?, body = ?, status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE note_id = ? AND project_row_id = ?
RETURNING id, note_id, project_row_id, title, body, status, created_at, updated_at`,
		params.Title,
		params.Body,
		params.Status,
		params.NoteID,
		params.ProjectRowID,
	))
}

func (tx *Tx) DeleteProjectNote(ctx context.Context, projectRowID int64, noteID string) error {
	result, err := tx.tx.ExecContext(ctx, `
DELETE FROM project_notes
WHERE project_row_id = ? AND note_id = ?`, projectRowID, noteID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (tx *Tx) MovePlanToProject(ctx context.Context, planID string, projectRowID int64) (Plan, error) {
	return scanPlan(tx.tx.QueryRowContext(ctx, `
UPDATE plans
SET project_row_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE plan_id = ?
RETURNING id, project_row_id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at`,
		projectRowID,
		planID,
	))
}

func getProjectByProjectID(ctx context.Context, queryer rowQueryer, projectID string) (Project, error) {
	return scanProject(queryer.QueryRowContext(ctx, `
SELECT id, project_id, name, description, status, created_at, updated_at
FROM projects
WHERE project_id = ?`, projectID))
}

func getProjectByRowID(ctx context.Context, queryer rowQueryer, rowID int64) (Project, error) {
	return scanProject(queryer.QueryRowContext(ctx, `
SELECT id, project_id, name, description, status, created_at, updated_at
FROM projects
WHERE id = ?`, rowID))
}

func getProjectRepositoryTarget(ctx context.Context, queryer rowQueryer, projectRowID int64, repoTarget string) (ProjectRepositoryTarget, error) {
	var value ProjectRepositoryTarget
	err := queryer.QueryRowContext(ctx, `
SELECT project_row_id, repo_target, created_at
FROM project_repository_targets
WHERE project_row_id = ? AND repo_target = ? COLLATE NOCASE`, projectRowID, repoTarget).Scan(
		&value.ProjectRowID,
		&value.RepoTarget,
		&value.CreatedAt,
	)
	return value, err
}

func getProjectNoteByNoteID(ctx context.Context, queryer rowQueryer, noteID string) (ProjectNote, error) {
	return scanProjectNote(queryer.QueryRowContext(ctx, `
SELECT id, note_id, project_row_id, title, body, status, created_at, updated_at
FROM project_notes
WHERE note_id = ?`, noteID))
}

func scanProject(row rowScanner) (Project, error) {
	var value Project
	err := row.Scan(
		&value.ID,
		&value.ProjectID,
		&value.Name,
		&value.Description,
		&value.Status,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	return value, err
}

func scanProjectNote(row rowScanner) (ProjectNote, error) {
	var value ProjectNote
	err := row.Scan(
		&value.ID,
		&value.NoteID,
		&value.ProjectRowID,
		&value.Title,
		&value.Body,
		&value.Status,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	return value, err
}

func validateProjectStatus(value string) error {
	switch value {
	case ProjectStatusActive, ProjectStatusArchived:
		return nil
	default:
		return fmt.Errorf("unsupported Project status %q", value)
	}
}
