package workflowstore

import (
	"context"
	"database/sql"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

// The generated types remain an implementation detail of the store. These
// aliases make the feature-workspace transaction surface available to the
// application owners without exposing generated queries directly.
type (
	FeatureWorkspace                  = workflowgenerated.FeatureWorkspace
	FeatureWorkspaceAdmittedInput     = workflowgenerated.FeatureWorkspaceAdmittedInput
	FeatureWorkspaceDestination       = workflowgenerated.FeatureWorkspaceDestination
	FeatureWorkspaceDiscoveryTicket   = workflowgenerated.FeatureWorkspaceDiscoveryTicket
	FeatureWorkspaceRouteState        = workflowgenerated.FeatureWorkspaceRouteState
	FeatureWorkspaceTicketDependency  = workflowgenerated.FeatureWorkspaceTicketDependency
	FeatureWorkspaceTicketResolution  = workflowgenerated.FeatureWorkspaceTicketResolution
	FeatureWorkspaceAuthorityRevision = workflowgenerated.FeatureWorkspaceAuthorityRevision
	FeatureWorkspaceAuthorityLayer    = workflowgenerated.FeatureWorkspaceAuthorityLayer

	CreateFeatureWorkspaceParams                  = workflowgenerated.CreateFeatureWorkspaceParams
	CreateFeatureWorkspaceAdmittedInputParams     = workflowgenerated.CreateFeatureWorkspaceAdmittedInputParams
	CreateFeatureWorkspaceDestinationParams       = workflowgenerated.CreateFeatureWorkspaceDestinationParams
	CreateFeatureWorkspaceDiscoveryTicketParams   = workflowgenerated.CreateFeatureWorkspaceDiscoveryTicketParams
	CreateFeatureWorkspaceRouteStateParams        = workflowgenerated.CreateFeatureWorkspaceRouteStateParams
	CreateFeatureWorkspaceTicketDependencyParams  = workflowgenerated.CreateFeatureWorkspaceTicketDependencyParams
	CreateFeatureWorkspaceTicketResolutionParams  = workflowgenerated.CreateFeatureWorkspaceTicketResolutionParams
	CreateFeatureWorkspaceAuthorityRevisionParams = workflowgenerated.CreateFeatureWorkspaceAuthorityRevisionParams
	CreateFeatureWorkspaceAuthorityLayerParams    = workflowgenerated.CreateFeatureWorkspaceAuthorityLayerParams
)

func (s *Store) GetFeatureWorkspaceByWorkspaceID(ctx context.Context, workspaceID string) (FeatureWorkspace, error) {
	return workflowgenerated.New(s.db).GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
}

func (s *Store) GetFeatureWorkspaceByRowID(ctx context.Context, rowID int64) (FeatureWorkspace, error) {
	return getFeatureWorkspaceByRowID(ctx, s.db, rowID)
}

func (s *Store) GetFeatureWorkspaceAuthorityRevisionByRowID(ctx context.Context, rowID int64) (FeatureWorkspaceAuthorityRevision, error) {
	return getFeatureWorkspaceAuthorityRevisionByRowID(ctx, s.db, rowID)
}

func (s *Store) ListFeatureWorkspaceAdmittedInputs(ctx context.Context, workspaceRowID int64) ([]FeatureWorkspaceAdmittedInput, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceAdmittedInputs(ctx, workspaceRowID)
}

func (s *Store) ListFeatureWorkspaceDestinations(ctx context.Context, workspaceRowID int64) ([]FeatureWorkspaceDestination, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceDestinations(ctx, workspaceRowID)
}

func (s *Store) ListFeatureWorkspaceDiscoveryTickets(ctx context.Context, workspaceRowID int64) ([]FeatureWorkspaceDiscoveryTicket, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceDiscoveryTickets(ctx, workspaceRowID)
}

func (s *Store) ListFeatureWorkspaceRouteStates(ctx context.Context, workspaceRowID int64) ([]FeatureWorkspaceRouteState, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceRouteStates(ctx, workspaceRowID)
}

func (s *Store) ListFeatureWorkspaceInvestigations(ctx context.Context, workspaceRowID int64) ([]FeatureWorkspaceInvestigation, error) {
	values, err := workflowgenerated.New(s.db).ListFeatureWorkspaceInvestigations(ctx, workspaceRowID)
	result := make([]FeatureWorkspaceInvestigation, len(values))
	for index, value := range values {
		result[index] = featureWorkspaceInvestigationFromGenerated(value)
	}
	return result, err
}

func (s *Store) ListFeatureWorkspaceAuthorityRevisions(ctx context.Context, workspaceRowID int64) ([]FeatureWorkspaceAuthorityRevision, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceAuthorityRevisions(ctx, workspaceRowID)
}

func (s *Store) ListFeatureWorkspaceAuthorityLayers(ctx context.Context, revisionRowID int64) ([]FeatureWorkspaceAuthorityLayer, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceAuthorityLayers(ctx, revisionRowID)
}

func (s *Store) ListFeatureWorkspaceTicketDependencies(ctx context.Context, ticketRowID int64) ([]FeatureWorkspaceTicketDependency, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceTicketDependencies(ctx, ticketRowID)
}

func (s *Store) ListFeatureWorkspaceTicketResolutions(ctx context.Context, ticketRowID int64) ([]FeatureWorkspaceTicketResolution, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceTicketResolutions(ctx, ticketRowID)
}

func (tx *Tx) GetFeatureWorkspaceByWorkspaceID(ctx context.Context, workspaceID string) (FeatureWorkspace, error) {
	return workflowgenerated.New(tx.tx).GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
}

func (tx *Tx) GetFeatureWorkspaceByRowID(ctx context.Context, rowID int64) (FeatureWorkspace, error) {
	return getFeatureWorkspaceByRowID(ctx, tx.tx, rowID)
}

func (tx *Tx) GetFeatureWorkspaceAuthorityRevisionByRowID(ctx context.Context, rowID int64) (FeatureWorkspaceAuthorityRevision, error) {
	return getFeatureWorkspaceAuthorityRevisionByRowID(ctx, tx.tx, rowID)
}

func (tx *Tx) GetFeatureWorkspaceDiscoveryTicketByID(ctx context.Context, ticketID string) (FeatureWorkspaceDiscoveryTicket, error) {
	return workflowgenerated.New(tx.tx).GetFeatureWorkspaceDiscoveryTicketByID(ctx, ticketID)
}

func (tx *Tx) ListFeatureWorkspaceAuthorityRevisions(ctx context.Context, workspaceRowID int64) ([]FeatureWorkspaceAuthorityRevision, error) {
	return workflowgenerated.New(tx.tx).ListFeatureWorkspaceAuthorityRevisions(ctx, workspaceRowID)
}

func (tx *Tx) ListFeatureWorkspaceAuthorityLayers(ctx context.Context, revisionRowID int64) ([]FeatureWorkspaceAuthorityLayer, error) {
	return workflowgenerated.New(tx.tx).ListFeatureWorkspaceAuthorityLayers(ctx, revisionRowID)
}

func (tx *Tx) CreateFeatureWorkspace(ctx context.Context, params CreateFeatureWorkspaceParams) (FeatureWorkspace, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspace(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceAdmittedInput(ctx context.Context, params CreateFeatureWorkspaceAdmittedInputParams) (FeatureWorkspaceAdmittedInput, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceAdmittedInput(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceDestination(ctx context.Context, params CreateFeatureWorkspaceDestinationParams) (FeatureWorkspaceDestination, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceDestination(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceDiscoveryTicket(ctx context.Context, params CreateFeatureWorkspaceDiscoveryTicketParams) (FeatureWorkspaceDiscoveryTicket, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceDiscoveryTicket(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceTicketDependency(ctx context.Context, params CreateFeatureWorkspaceTicketDependencyParams) error {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceTicketDependency(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceTicketResolution(ctx context.Context, params CreateFeatureWorkspaceTicketResolutionParams) (FeatureWorkspaceTicketResolution, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceTicketResolution(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceRouteState(ctx context.Context, params CreateFeatureWorkspaceRouteStateParams) (FeatureWorkspaceRouteState, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceRouteState(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceAuthorityRevision(ctx context.Context, params CreateFeatureWorkspaceAuthorityRevisionParams) (FeatureWorkspaceAuthorityRevision, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceAuthorityRevision(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceAuthorityLayer(ctx context.Context, params CreateFeatureWorkspaceAuthorityLayerParams) (FeatureWorkspaceAuthorityLayer, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceAuthorityLayer(ctx, params)
}

func (tx *Tx) AdvanceFeatureWorkspaceRouteState(ctx context.Context, routeRowID int64, workspaceState, workspaceID string, expectedVersion int64) (FeatureWorkspace, error) {
	return workflowgenerated.New(tx.tx).AdvanceFeatureWorkspaceRouteState(ctx, workflowgenerated.AdvanceFeatureWorkspaceRouteStateParams{
		CurrentRouteStateRowID: sql.NullInt64{Int64: routeRowID, Valid: true}, State: workspaceState, WorkspaceID: workspaceID, Version: expectedVersion,
	})
}

func (tx *Tx) SetFeatureWorkspaceAuthorityRevision(ctx context.Context, revisionRowID int64, workspaceID string, expectedVersion int64) (FeatureWorkspace, error) {
	return workflowgenerated.New(tx.tx).SetFeatureWorkspaceAuthorityRevision(ctx, workflowgenerated.SetFeatureWorkspaceAuthorityRevisionParams{
		CurrentAuthorityRevisionRowID: sql.NullInt64{Int64: revisionRowID, Valid: true}, WorkspaceID: workspaceID, Version: expectedVersion,
	})
}

func (tx *Tx) TransitionFeatureWorkspaceDiscoveryTicket(ctx context.Context, ticketID, expectedState, nextState string, expectedVersion int64) (FeatureWorkspaceDiscoveryTicket, error) {
	return workflowgenerated.New(tx.tx).TransitionFeatureWorkspaceDiscoveryTicket(ctx, workflowgenerated.TransitionFeatureWorkspaceDiscoveryTicketParams{
		State: nextState, DiscoveryTicketID: ticketID, State_2: expectedState, Version: expectedVersion,
	})
}

// BumpFeatureWorkspaceVersion provides optimistic concurrency for immutable
// child-history writes that do not otherwise update the workspace row.
func (tx *Tx) BumpFeatureWorkspaceVersion(ctx context.Context, workspaceID string, expectedVersion int64) (FeatureWorkspace, error) {
	var value FeatureWorkspace
	err := tx.tx.QueryRowContext(ctx, `
UPDATE feature_workspaces
SET version = version + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE workspace_id = ? AND version = ?
RETURNING id, workspace_id, project_row_id, feature_slug, state, version,
          current_route_state_row_id, current_authority_revision_row_id, created_at, updated_at`, workspaceID, expectedVersion).Scan(
		&value.ID, &value.WorkspaceID, &value.ProjectRowID, &value.FeatureSlug, &value.State, &value.Version,
		&value.CurrentRouteStateRowID, &value.CurrentAuthorityRevisionRowID, &value.CreatedAt, &value.UpdatedAt,
	)
	return value, err
}

type featureWorkspaceQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getFeatureWorkspaceByRowID(ctx context.Context, queryer featureWorkspaceQueryer, rowID int64) (FeatureWorkspace, error) {
	var value FeatureWorkspace
	err := queryer.QueryRowContext(ctx, `
SELECT id, workspace_id, project_row_id, feature_slug, state, version,
       current_route_state_row_id, current_authority_revision_row_id, created_at, updated_at
FROM feature_workspaces
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.WorkspaceID, &value.ProjectRowID, &value.FeatureSlug, &value.State, &value.Version,
		&value.CurrentRouteStateRowID, &value.CurrentAuthorityRevisionRowID, &value.CreatedAt, &value.UpdatedAt,
	)
	return value, err
}

func getFeatureWorkspaceAuthorityRevisionByRowID(ctx context.Context, queryer featureWorkspaceQueryer, rowID int64) (FeatureWorkspaceAuthorityRevision, error) {
	var value FeatureWorkspaceAuthorityRevision
	err := queryer.QueryRowContext(ctx, `
SELECT id, authority_revision_id, workspace_row_id, revision_number, source_closure_row_id, created_at
FROM feature_workspace_authority_revisions
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.AuthorityRevisionID, &value.WorkspaceRowID, &value.RevisionNumber,
		&value.SourceClosureRowID, &value.CreatedAt,
	)
	return value, err
}
