package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type CutoverActivation struct {
	ID                                int64
	CutoverActivationID               string
	WorkspaceRowID                    int64
	TransitionPlanTicketRevisionRowID int64
	TransitionPlanTicketID            string
	TransitionPlanTicketRevision      int64
	TransitionPlanAuthorityLayerRowID int64
	TransitionPlanSHA256              string
	AuthorityRevisionRowID            int64
	AuthorityRevisionID               string
	AuthorityRevisionNumber           int64
	AuthoritySHA256                   string
	RollbackEligibility               string
	ActivationStatus                  string
	ActivatedAt                       sql.NullString
	ExecutionBoundaryStatus           string
	FirstNewExecutionRunRowID         sql.NullInt64
	FirstNewExecutionAt               sql.NullString
	RollbackStatus                    string
	RollForwardStatus                 string
	RolledBackAt                      sql.NullString
	CreatedAt                         string
}

type CutoverActivationPrerequisite struct {
	ID              int64
	ActivationRowID int64
	Sequence        int64
	Prerequisite    string
	Evidence        string
	CreatedAt       string
}

type CutoverActivationObligation struct {
	ID              int64
	ActivationRowID int64
	ObligationKind  string
	Sequence        int64
	Obligation      string
	Evidence        string
	CreatedAt       string
}

type CutoverRollForwardCriterion struct {
	ID                  int64
	ActivationRowID     int64
	Sequence            int64
	CompletionCriterion string
	CreatedAt           string
}

type CutoverRollForwardEvidence struct {
	ID              int64
	ActivationRowID int64
	CriterionRowID  int64
	Evidence        string
	CreatedAt       string
}

type CutoverCurrentState struct {
	SingletonID     int64
	ActivationRowID sql.NullInt64
	CreatedAt       string
	UpdatedAt       string
}

var (
	ErrCutoverNotFound      = errors.New("cutover activation not found")
	ErrCutoverAlreadyActive = errors.New("a cutover activation is already active")
	ErrCutoverNotPrepared   = errors.New("cutover activation is not in prepared state")
	ErrCutoverStateConflict = errors.New("cutover state transition conflict")
)

const cutoverActivationColumns = `
id, cutover_activation_id, workspace_row_id, transition_plan_ticket_revision_row_id,
transition_plan_ticket_id, transition_plan_ticket_revision, transition_plan_authority_layer_row_id,
transition_plan_sha256, authority_revision_row_id, authority_revision_id,
authority_revision_number, authority_sha256, rollback_eligibility,
activation_status, activated_at, execution_boundary_status,
first_new_execution_run_row_id, first_new_execution_at,
rollback_status, roll_forward_status, rolled_back_at, created_at`

func scanCutoverActivation(row rowScanner) (CutoverActivation, error) {
	var value CutoverActivation
	err := row.Scan(
		&value.ID, &value.CutoverActivationID, &value.WorkspaceRowID,
		&value.TransitionPlanTicketRevisionRowID, &value.TransitionPlanTicketID,
		&value.TransitionPlanTicketRevision, &value.TransitionPlanAuthorityLayerRowID,
		&value.TransitionPlanSHA256, &value.AuthorityRevisionRowID, &value.AuthorityRevisionID,
		&value.AuthorityRevisionNumber, &value.AuthoritySHA256, &value.RollbackEligibility,
		&value.ActivationStatus, &value.ActivatedAt, &value.ExecutionBoundaryStatus,
		&value.FirstNewExecutionRunRowID, &value.FirstNewExecutionAt,
		&value.RollbackStatus, &value.RollForwardStatus, &value.RolledBackAt, &value.CreatedAt,
	)
	return value, err
}

func (s *Store) GetCutoverActivationByID(ctx context.Context, activationID string) (CutoverActivation, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+cutoverActivationColumns+`
FROM cutover_activations
WHERE cutover_activation_id = ?`, activationID)
	value, err := scanCutoverActivation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CutoverActivation{}, fmt.Errorf("%w: %s", ErrCutoverNotFound, activationID)
	}
	return value, err
}

func (s *Store) GetCurrentCutoverActivation(ctx context.Context) (CutoverActivation, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+cutoverActivationColumns+`
FROM cutover_activations
WHERE id = (SELECT activation_row_id FROM cutover_current_states WHERE singleton_id = 1)`)
	value, err := scanCutoverActivation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CutoverActivation{}, false, nil
	}
	if err != nil {
		return CutoverActivation{}, false, err
	}
	return value, true, nil
}

func (s *Store) ListCutoverActivationPrerequisites(ctx context.Context, activationRowID int64) ([]CutoverActivationPrerequisite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, activation_row_id, sequence, prerequisite, evidence, created_at
FROM cutover_activation_prerequisite_evidence
WHERE activation_row_id = ?
ORDER BY sequence`, activationRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []CutoverActivationPrerequisite
	for rows.Next() {
		var value CutoverActivationPrerequisite
		if err := rows.Scan(&value.ID, &value.ActivationRowID, &value.Sequence, &value.Prerequisite, &value.Evidence, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListCutoverActivationObligations(ctx context.Context, activationRowID int64) ([]CutoverActivationObligation, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, activation_row_id, obligation_kind, sequence, obligation, evidence, created_at
FROM cutover_activation_obligation_evidence
WHERE activation_row_id = ?
ORDER BY obligation_kind, sequence`, activationRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []CutoverActivationObligation
	for rows.Next() {
		var value CutoverActivationObligation
		if err := rows.Scan(&value.ID, &value.ActivationRowID, &value.ObligationKind, &value.Sequence, &value.Obligation, &value.Evidence, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListCutoverRollForwardCriteria(ctx context.Context, activationRowID int64) ([]CutoverRollForwardCriterion, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, activation_row_id, sequence, completion_criterion, created_at
FROM cutover_roll_forward_criteria
WHERE activation_row_id = ?
ORDER BY sequence`, activationRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []CutoverRollForwardCriterion
	for rows.Next() {
		var value CutoverRollForwardCriterion
		if err := rows.Scan(&value.ID, &value.ActivationRowID, &value.Sequence, &value.CompletionCriterion, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListCutoverRollForwardEvidence(ctx context.Context, activationRowID int64) ([]CutoverRollForwardEvidence, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, activation_row_id, criterion_row_id, evidence, created_at
FROM cutover_roll_forward_evidence
WHERE activation_row_id = ?
ORDER BY criterion_row_id`, activationRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []CutoverRollForwardEvidence
	for rows.Next() {
		var value CutoverRollForwardEvidence
		if err := rows.Scan(&value.ID, &value.ActivationRowID, &value.CriterionRowID, &value.Evidence, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListAllCutoverActivations(ctx context.Context) ([]CutoverActivation, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cutoverActivationColumns+`
FROM cutover_activations
ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []CutoverActivation
	for rows.Next() {
		value, err := scanCutoverActivation(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (tx *Tx) CreateCutoverActivation(ctx context.Context, activationID string, workspaceRowID int64, transitionPlanTicketRevisionRowID int64, transitionPlanTicketID string, transitionPlanTicketRevision int64, transitionPlanAuthorityLayerRowID int64, transitionPlanSHA256 string, authorityRevisionRowID int64, authorityRevisionID string, authorityRevisionNumber int64, authoritySHA256 string, rollbackEligibility string) (CutoverActivation, error) {
	value, err := scanCutoverActivation(tx.tx.QueryRowContext(ctx, `
INSERT INTO cutover_activations (
    cutover_activation_id, workspace_row_id, transition_plan_ticket_revision_row_id,
    transition_plan_ticket_id, transition_plan_ticket_revision, transition_plan_authority_layer_row_id,
    transition_plan_sha256, authority_revision_row_id, authority_revision_id,
    authority_revision_number, authority_sha256, rollback_eligibility
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING `+cutoverActivationColumns,
		activationID, workspaceRowID, transitionPlanTicketRevisionRowID,
		transitionPlanTicketID, transitionPlanTicketRevision, transitionPlanAuthorityLayerRowID,
		transitionPlanSHA256, authorityRevisionRowID, authorityRevisionID,
		authorityRevisionNumber, authoritySHA256, rollbackEligibility,
	))
	return value, err
}

func (tx *Tx) ActivateCutover(ctx context.Context, activationID string, activatedAt string, rollbackEligibility string) (CutoverActivation, error) {
	rollbackStatus := "available"
	if rollbackEligibility == "not_eligible" {
		rollbackStatus = "not_eligible"
	}
	value, err := scanCutoverActivation(tx.tx.QueryRowContext(ctx, `
UPDATE cutover_activations SET
    activation_status = 'active',
    activated_at = ?,
    rollback_status = ?
WHERE cutover_activation_id = ? AND activation_status = 'prepared'
RETURNING `+cutoverActivationColumns,
		activatedAt, rollbackStatus, activationID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return CutoverActivation{}, ErrCutoverNotPrepared
	}
	return value, err
}

func (tx *Tx) SetCutoverCurrentState(ctx context.Context, activationRowID int64) error {
	result, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_current_states (singleton_id, activation_row_id)
VALUES (1, ?)
ON CONFLICT(singleton_id) DO UPDATE SET activation_row_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		activationRowID, activationRowID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("failed to set cutover current state")
	}
	return nil
}

func (tx *Tx) CrossCutoverExecutionBoundary(ctx context.Context, activationID string, runRowID int64, crossedAt string) (CutoverActivation, error) {
	value, err := scanCutoverActivation(tx.tx.QueryRowContext(ctx, `
UPDATE cutover_activations SET
    execution_boundary_status = 'crossed',
    first_new_execution_run_row_id = ?,
    first_new_execution_at = ?,
    rollback_status = 'forbidden',
    roll_forward_status = 'required'
WHERE cutover_activation_id = ? AND activation_status = 'active' AND execution_boundary_status = 'open'
RETURNING `+cutoverActivationColumns,
		runRowID, crossedAt, activationID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return CutoverActivation{}, ErrCutoverStateConflict
	}
	return value, err
}

func (tx *Tx) CompleteCutoverRollForward(ctx context.Context, activationID string) (CutoverActivation, error) {
	value, err := scanCutoverActivation(tx.tx.QueryRowContext(ctx, `
UPDATE cutover_activations SET
    roll_forward_status = 'completed'
WHERE cutover_activation_id = ? AND activation_status = 'active' AND roll_forward_status = 'required'
RETURNING `+cutoverActivationColumns,
		activationID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return CutoverActivation{}, ErrCutoverStateConflict
	}
	return value, err
}

func (tx *Tx) RollbackCutover(ctx context.Context, activationID string, rolledBackAt string) (CutoverActivation, error) {
	value, err := scanCutoverActivation(tx.tx.QueryRowContext(ctx, `
UPDATE cutover_activations SET
    activation_status = 'rolled_back',
    rollback_status = 'rolled_back',
    roll_forward_status = 'not_required',
    rolled_back_at = ?
WHERE cutover_activation_id = ? AND activation_status = 'active' AND execution_boundary_status = 'open' AND rollback_eligibility = 'eligible' AND rollback_status = 'available'
RETURNING `+cutoverActivationColumns,
		rolledBackAt, activationID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return CutoverActivation{}, ErrCutoverStateConflict
	}
	return value, err
}

func (tx *Tx) ClearCutoverCurrentState(ctx context.Context) error {
	_, err := tx.tx.ExecContext(ctx, `
UPDATE cutover_current_states SET activation_row_id = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE singleton_id = 1 AND EXISTS (SELECT 1 FROM cutover_activations WHERE id = activation_row_id AND activation_status = 'rolled_back')`)
	return err
}

func (tx *Tx) CreateCutoverPrerequisite(ctx context.Context, activationRowID int64, sequence int64, prerequisite string, evidence string) (CutoverActivationPrerequisite, error) {
	row := tx.tx.QueryRowContext(ctx, `
INSERT INTO cutover_activation_prerequisite_evidence (activation_row_id, sequence, prerequisite, evidence)
VALUES (?, ?, ?, ?)
RETURNING id, activation_row_id, sequence, prerequisite, evidence, created_at`,
		activationRowID, sequence, prerequisite, evidence)
	var value CutoverActivationPrerequisite
	err := row.Scan(&value.ID, &value.ActivationRowID, &value.Sequence, &value.Prerequisite, &value.Evidence, &value.CreatedAt)
	return value, err
}

func (tx *Tx) CreateCutoverObligation(ctx context.Context, activationRowID int64, obligationKind string, sequence int64, obligation string, evidence string) (CutoverActivationObligation, error) {
	row := tx.tx.QueryRowContext(ctx, `
INSERT INTO cutover_activation_obligation_evidence (activation_row_id, obligation_kind, sequence, obligation, evidence)
VALUES (?, ?, ?, ?, ?)
RETURNING id, activation_row_id, obligation_kind, sequence, obligation, evidence, created_at`,
		activationRowID, obligationKind, sequence, obligation, evidence)
	var value CutoverActivationObligation
	err := row.Scan(&value.ID, &value.ActivationRowID, &value.ObligationKind, &value.Sequence, &value.Obligation, &value.Evidence, &value.CreatedAt)
	return value, err
}

func (tx *Tx) CreateCutoverRollForwardCriterion(ctx context.Context, activationRowID int64, sequence int64, criterion string) (CutoverRollForwardCriterion, error) {
	row := tx.tx.QueryRowContext(ctx, `
INSERT INTO cutover_roll_forward_criteria (activation_row_id, sequence, completion_criterion)
VALUES (?, ?, ?)
RETURNING id, activation_row_id, sequence, completion_criterion, created_at`,
		activationRowID, sequence, criterion)
	var value CutoverRollForwardCriterion
	err := row.Scan(&value.ID, &value.ActivationRowID, &value.Sequence, &value.CompletionCriterion, &value.CreatedAt)
	return value, err
}

func (tx *Tx) CreateCutoverRollForwardEvidence(ctx context.Context, activationRowID int64, criterionRowID int64, evidence string) (CutoverRollForwardEvidence, error) {
	row := tx.tx.QueryRowContext(ctx, `
INSERT INTO cutover_roll_forward_evidence (activation_row_id, criterion_row_id, evidence)
VALUES (?, ?, ?)
RETURNING id, activation_row_id, criterion_row_id, evidence, created_at`,
		activationRowID, criterionRowID, evidence)
	var value CutoverRollForwardEvidence
	err := row.Scan(&value.ID, &value.ActivationRowID, &value.CriterionRowID, &value.Evidence, &value.CreatedAt)
	return value, err
}
