package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	workflowgenerated "relay/internal/store/workflowgenerated"
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
type CutoverGatewayConfiguration struct {
	ActivationRowID     int64
	ConfigurationSHA256 string
	RelayRepository     string
	RelayCommitOID      string
	StandingRepository  string
	StandingCommitOID   string
	Routes              []CutoverGatewayRoute
	Mappings            []CutoverGatewayMapping
	StandingAuthorities []CutoverGatewayStandingAuthority
	DependencyOutcomes  []CutoverGatewayDependencyOutcome
}

type CutoverGatewayRoute struct {
	Sequence           int64
	RoutePath          string
	Role               string
	SurfaceContractID  string
	ManifestSHA256     string
	AuthorityCommitOID string
	AuthorityBlobOID   string
}

type CutoverGatewayMapping struct {
	Sequence             int64
	MappingID            string
	RoutePath            string
	ListenerIdentity     string
	UpstreamIdentity     string
	HealthEvidenceSHA256 string
	TraceEvidenceSHA256  string
}

type CutoverGatewayStandingAuthority struct {
	Role          string
	Repository    string
	CommitOID     string
	Path          string
	BlobOID       string
	ContentSHA256 string
}

type CutoverGatewayDependencyOutcome struct {
	Sequence       int64
	TicketID       string
	TicketRevision int64
	Outcome        string
	EvidenceSHA256 string
}

var (
	ErrCutoverNotFound              = errors.New("cutover activation not found")
	ErrCutoverAlreadyActive         = errors.New("a cutover activation is already active")
	ErrCutoverNotPrepared           = errors.New("cutover activation is not in prepared state")
	ErrCutoverStateConflict         = errors.New("cutover state transition conflict")
	ErrCutoverBoundaryQualification = errors.New("Run does not qualify for cutover boundary crossing")
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
func (tx *Tx) CreateCutoverGatewayConfiguration(ctx context.Context, activationRowID int64, value CutoverGatewayConfiguration) error {
	if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_configurations (
    activation_row_id, configuration_sha256, relay_repository, relay_commit_oid,
    standing_repository, standing_commit_oid
) VALUES (?, ?, ?, ?, ?, ?)`,
		activationRowID, value.ConfigurationSHA256, value.RelayRepository, value.RelayCommitOID,
		value.StandingRepository, value.StandingCommitOID); err != nil {
		return err
	}
	for _, route := range value.Routes {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_routes (
    activation_row_id, sequence, route_path, role, surface_contract_id,
    manifest_sha256, authority_commit_oid, authority_blob_oid
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			activationRowID, route.Sequence, route.RoutePath, route.Role, route.SurfaceContractID,
			route.ManifestSHA256, route.AuthorityCommitOID, route.AuthorityBlobOID); err != nil {
			return err
		}
	}
	for _, mapping := range value.Mappings {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_mappings (
    activation_row_id, sequence, mapping_id, route_path, listener_identity,
    upstream_identity, health_evidence_sha256, trace_evidence_sha256
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			activationRowID, mapping.Sequence, mapping.MappingID, mapping.RoutePath,
			mapping.ListenerIdentity, mapping.UpstreamIdentity,
			mapping.HealthEvidenceSHA256, mapping.TraceEvidenceSHA256); err != nil {
			return err
		}
	}
	for _, authority := range value.StandingAuthorities {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_standing_authorities (
    activation_row_id, role, repository, commit_oid, path, blob_oid, content_sha256
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			activationRowID, authority.Role, authority.Repository, authority.CommitOID,
			authority.Path, authority.BlobOID, authority.ContentSHA256); err != nil {
			return err
		}
	}
	for _, dependency := range value.DependencyOutcomes {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_dependency_outcomes (
    activation_row_id, sequence, ticket_id, ticket_revision, outcome, evidence_sha256
) VALUES (?, ?, ?, ?, ?, ?)`,
			activationRowID, dependency.Sequence, dependency.TicketID,
			dependency.TicketRevision, dependency.Outcome, dependency.EvidenceSHA256); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadCutoverGatewayConfiguration(ctx context.Context, activationRowID int64) (CutoverGatewayConfiguration, error) {
	return loadCutoverGatewayConfiguration(ctx, s.db, activationRowID)
}

func (tx *Tx) LoadCutoverGatewayConfiguration(ctx context.Context, activationRowID int64) (CutoverGatewayConfiguration, error) {
	return loadCutoverGatewayConfiguration(ctx, tx.tx, activationRowID)
}

type cutoverConfigurationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadCutoverGatewayConfiguration(ctx context.Context, queryer cutoverConfigurationQueryer, activationRowID int64) (CutoverGatewayConfiguration, error) {
	result := CutoverGatewayConfiguration{ActivationRowID: activationRowID}
	if err := queryer.QueryRowContext(ctx, `
SELECT configuration_sha256, relay_repository, relay_commit_oid, standing_repository, standing_commit_oid
FROM cutover_gateway_configurations
WHERE activation_row_id = ?`, activationRowID).Scan(
		&result.ConfigurationSHA256,
		&result.RelayRepository,
		&result.RelayCommitOID,
		&result.StandingRepository,
		&result.StandingCommitOID,
	); err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	routeRows, err := queryer.QueryContext(ctx, `
SELECT sequence, route_path, role, surface_contract_id, manifest_sha256, authority_commit_oid, authority_blob_oid
FROM cutover_gateway_routes WHERE activation_row_id = ? ORDER BY sequence`, activationRowID)
	if err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	defer routeRows.Close()
	for routeRows.Next() {
		var value CutoverGatewayRoute
		if err := routeRows.Scan(&value.Sequence, &value.RoutePath, &value.Role, &value.SurfaceContractID, &value.ManifestSHA256, &value.AuthorityCommitOID, &value.AuthorityBlobOID); err != nil {
			return CutoverGatewayConfiguration{}, err
		}
		result.Routes = append(result.Routes, value)
	}
	if err := routeRows.Err(); err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	mappingRows, err := queryer.QueryContext(ctx, `
SELECT sequence, mapping_id, route_path, listener_identity, upstream_identity, health_evidence_sha256, trace_evidence_sha256
FROM cutover_gateway_mappings WHERE activation_row_id = ? ORDER BY sequence`, activationRowID)
	if err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	defer mappingRows.Close()
	for mappingRows.Next() {
		var value CutoverGatewayMapping
		if err := mappingRows.Scan(&value.Sequence, &value.MappingID, &value.RoutePath, &value.ListenerIdentity, &value.UpstreamIdentity, &value.HealthEvidenceSHA256, &value.TraceEvidenceSHA256); err != nil {
			return CutoverGatewayConfiguration{}, err
		}
		result.Mappings = append(result.Mappings, value)
	}
	if err := mappingRows.Err(); err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	standingRows, err := queryer.QueryContext(ctx, `
SELECT role, repository, commit_oid, path, blob_oid, content_sha256
FROM cutover_gateway_standing_authorities WHERE activation_row_id = ? ORDER BY role`, activationRowID)
	if err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	defer standingRows.Close()
	for standingRows.Next() {
		var value CutoverGatewayStandingAuthority
		if err := standingRows.Scan(&value.Role, &value.Repository, &value.CommitOID, &value.Path, &value.BlobOID, &value.ContentSHA256); err != nil {
			return CutoverGatewayConfiguration{}, err
		}
		result.StandingAuthorities = append(result.StandingAuthorities, value)
	}
	if err := standingRows.Err(); err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	dependencyRows, err := queryer.QueryContext(ctx, `
SELECT sequence, ticket_id, ticket_revision, outcome, evidence_sha256
FROM cutover_gateway_dependency_outcomes WHERE activation_row_id = ? ORDER BY sequence`, activationRowID)
	if err != nil {
		return CutoverGatewayConfiguration{}, err
	}
	defer dependencyRows.Close()
	for dependencyRows.Next() {
		var value CutoverGatewayDependencyOutcome
		if err := dependencyRows.Scan(&value.Sequence, &value.TicketID, &value.TicketRevision, &value.Outcome, &value.EvidenceSHA256); err != nil {
			return CutoverGatewayConfiguration{}, err
		}
		result.DependencyOutcomes = append(result.DependencyOutcomes, value)
	}
	return result, dependencyRows.Err()
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

func (tx *Tx) ConditionalCrossCutoverExecutionBoundary(ctx context.Context, activationID string, runRowID int64) (CutoverActivation, error) {
	value, err := workflowgenerated.New(tx.tx).CrossCutoverBoundary(ctx, workflowgenerated.CrossCutoverBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: runRowID, Valid: true},
		CutoverActivationID:       activationID,
		ID:                        runRowID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return CutoverActivation{}, ErrCutoverStateConflict
	}
	return toCutoverActivation(value), err
}

func (tx *Tx) AttemptCrossCutoverBoundaryForRun(ctx context.Context, runRowID int64, runExecutionPackageRowID sql.NullInt64) error {
	current, err := workflowgenerated.New(tx.tx).GetCurrentCutoverActivation(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.ExecutionBoundaryStatus == "crossed" {
		return nil
	}
	if !runExecutionPackageRowID.Valid {
		return ErrCutoverBoundaryQualification
	}
	_, err = tx.ConditionalCrossCutoverExecutionBoundary(ctx, current.CutoverActivationID, runRowID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrCutoverStateConflict) {
		return err
	}
	reloaded, reloadErr := workflowgenerated.New(tx.tx).GetCurrentCutoverActivation(ctx)
	if errors.Is(reloadErr, sql.ErrNoRows) {
		return nil
	}
	if reloadErr != nil {
		return reloadErr
	}
	if reloaded.ExecutionBoundaryStatus == "crossed" {
		return nil
	}
	return ErrCutoverBoundaryQualification
}

func toCutoverActivation(g workflowgenerated.CutoverActivation) CutoverActivation {
	return CutoverActivation{
		ID:                                g.ID,
		CutoverActivationID:               g.CutoverActivationID,
		WorkspaceRowID:                    g.WorkspaceRowID,
		TransitionPlanTicketRevisionRowID: g.TransitionPlanTicketRevisionRowID,
		TransitionPlanTicketID:            g.TransitionPlanTicketID,
		TransitionPlanTicketRevision:      g.TransitionPlanTicketRevision,
		TransitionPlanAuthorityLayerRowID: g.TransitionPlanAuthorityLayerRowID,
		TransitionPlanSHA256:              g.TransitionPlanSha256,
		AuthorityRevisionRowID:            g.AuthorityRevisionRowID,
		AuthorityRevisionID:               g.AuthorityRevisionID,
		AuthorityRevisionNumber:           g.AuthorityRevisionNumber,
		AuthoritySHA256:                   g.AuthoritySha256,
		RollbackEligibility:               g.RollbackEligibility,
		ActivationStatus:                  g.ActivationStatus,
		ActivatedAt:                       g.ActivatedAt,
		ExecutionBoundaryStatus:           g.ExecutionBoundaryStatus,
		FirstNewExecutionRunRowID:         g.FirstNewExecutionRunRowID,
		FirstNewExecutionAt:               g.FirstNewExecutionAt,
		RollbackStatus:                    g.RollbackStatus,
		RollForwardStatus:                 g.RollForwardStatus,
		RolledBackAt:                      g.RolledBackAt,
		CreatedAt:                         g.CreatedAt,
	}
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
