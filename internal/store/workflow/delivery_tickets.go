package workflowstore

import (
	"context"
	"database/sql"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

type (
	DeliveryTicket                   = workflowgenerated.DeliveryTicket
	DeliveryTicketRevision           = workflowgenerated.DeliveryTicketRevision
	DeliveryTicketRevisionMember     = workflowgenerated.DeliveryTicketRevisionMember
	DeliveryTicketRevisionDependency = workflowgenerated.DeliveryTicketRevisionDependency
	DeliveryTicketRevisionApproval   = workflowgenerated.DeliveryTicketRevisionApproval
	DeliveryTicketSelection          = workflowgenerated.DeliveryTicketSelection
	DeliveryTicketSelectionMember    = workflowgenerated.DeliveryTicketSelectionMember

	CreateDeliveryTicketParams                   = workflowgenerated.CreateDeliveryTicketParams
	CreateDeliveryTicketRevisionParams           = workflowgenerated.CreateDeliveryTicketRevisionParams
	CreateDeliveryTicketRevisionMemberParams     = workflowgenerated.CreateDeliveryTicketRevisionMemberParams
	CreateDeliveryTicketRevisionDependencyParams = workflowgenerated.CreateDeliveryTicketRevisionDependencyParams
	CreateDeliveryTicketRevisionApprovalParams   = workflowgenerated.CreateDeliveryTicketRevisionApprovalParams
	CreateDeliveryTicketSelectionParams          = workflowgenerated.CreateDeliveryTicketSelectionParams
	CreateDeliveryTicketSelectionMemberParams    = workflowgenerated.CreateDeliveryTicketSelectionMemberParams
)

func (s *Store) GetDeliveryTicketByTicketID(ctx context.Context, ticketID string) (DeliveryTicket, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketByTicketID(ctx, ticketID)
}

func (s *Store) GetDeliveryTicketByRowID(ctx context.Context, rowID int64) (DeliveryTicket, error) {
	var value DeliveryTicket
	err := s.db.QueryRowContext(ctx, `
SELECT id, ticket_id, workspace_row_id, external_priority, current_revision_row_id, created_at, updated_at
FROM delivery_tickets
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.TicketID, &value.WorkspaceRowID, &value.ExternalPriority,
		&value.CurrentRevisionRowID, &value.CreatedAt, &value.UpdatedAt,
	)
	return value, err
}

func (s *Store) ListDeliveryTicketsByWorkspace(ctx context.Context, workspaceRowID int64) ([]DeliveryTicket, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketsByWorkspace(ctx, workspaceRowID)
}

func (s *Store) GetDeliveryTicketRevisionByRowID(ctx context.Context, revisionRowID int64) (DeliveryTicketRevision, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketRevisionByRowID(ctx, revisionRowID)
}

func (s *Store) ListDeliveryTicketRevisions(ctx context.Context, ticketRowID int64) ([]DeliveryTicketRevision, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketRevisions(ctx, ticketRowID)
}

func (s *Store) ListDeliveryTicketRevisionMembers(ctx context.Context, revisionRowID int64) ([]DeliveryTicketRevisionMember, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketRevisionMembers(ctx, revisionRowID)
}

func (s *Store) ListDeliveryTicketRevisionDependencies(ctx context.Context, revisionRowID int64) ([]DeliveryTicketRevisionDependency, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketRevisionDependencies(ctx, revisionRowID)
}

func (s *Store) ListDeliveryTicketRevisionApprovals(ctx context.Context, revisionRowID int64) ([]DeliveryTicketRevisionApproval, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketRevisionApprovals(ctx, revisionRowID)
}

func (s *Store) GetDeliveryTicketSelectionBySelectionID(ctx context.Context, selectionID string) (DeliveryTicketSelection, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketSelectionBySelectionID(ctx, selectionID)
}

func (s *Store) ListDeliveryTicketSelectionsByWorkspace(ctx context.Context, workspaceRowID int64) ([]DeliveryTicketSelection, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketSelectionsByWorkspace(ctx, workspaceRowID)
}

func (s *Store) ListDeliveryTicketSelectionMembers(ctx context.Context, selectionRowID int64) ([]DeliveryTicketSelectionMember, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketSelectionMembers(ctx, selectionRowID)
}

func (tx *Tx) CreateDeliveryTicket(ctx context.Context, params CreateDeliveryTicketParams) (DeliveryTicket, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicket(ctx, params)
}

func (tx *Tx) GetDeliveryTicketByTicketID(ctx context.Context, ticketID string) (DeliveryTicket, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryTicketByTicketID(ctx, ticketID)
}

func (tx *Tx) GetDeliveryTicketByRowID(ctx context.Context, rowID int64) (DeliveryTicket, error) {
	var value DeliveryTicket
	err := tx.tx.QueryRowContext(ctx, `
SELECT id, ticket_id, workspace_row_id, external_priority, current_revision_row_id, created_at, updated_at
FROM delivery_tickets
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.TicketID, &value.WorkspaceRowID, &value.ExternalPriority,
		&value.CurrentRevisionRowID, &value.CreatedAt, &value.UpdatedAt,
	)
	return value, err
}

func (tx *Tx) GetDeliveryTicketRevisionByRowID(ctx context.Context, revisionRowID int64) (DeliveryTicketRevision, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryTicketRevisionByRowID(ctx, revisionRowID)
}

func (tx *Tx) ListDeliveryTicketRevisions(ctx context.Context, ticketRowID int64) ([]DeliveryTicketRevision, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryTicketRevisions(ctx, ticketRowID)
}

func (tx *Tx) ListDeliveryTicketRevisionApprovals(ctx context.Context, revisionRowID int64) ([]DeliveryTicketRevisionApproval, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryTicketRevisionApprovals(ctx, revisionRowID)
}

func (tx *Tx) UpdateDeliveryTicketExternalPriority(ctx context.Context, ticketID string, externalPriority int64) (DeliveryTicket, error) {
	return workflowgenerated.New(tx.tx).UpdateDeliveryTicketExternalPriority(ctx, workflowgenerated.UpdateDeliveryTicketExternalPriorityParams{
		TicketID: ticketID, ExternalPriority: externalPriority,
	})
}

func (tx *Tx) CreateDeliveryTicketRevision(ctx context.Context, params CreateDeliveryTicketRevisionParams) (DeliveryTicketRevision, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketRevision(ctx, params)
}

func (tx *Tx) SetDeliveryTicketCurrentRevision(ctx context.Context, ticketID string, revisionRowID int64) (DeliveryTicket, error) {
	return workflowgenerated.New(tx.tx).SetDeliveryTicketCurrentRevision(ctx, workflowgenerated.SetDeliveryTicketCurrentRevisionParams{
		TicketID: ticketID, CurrentRevisionRowID: sql.NullInt64{Int64: revisionRowID, Valid: true},
	})
}

func (tx *Tx) CreateDeliveryTicketRevisionMember(ctx context.Context, params CreateDeliveryTicketRevisionMemberParams) (DeliveryTicketRevisionMember, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionMember(ctx, params)
}

func (tx *Tx) CreateDeliveryTicketRevisionDependency(ctx context.Context, params CreateDeliveryTicketRevisionDependencyParams) (DeliveryTicketRevisionDependency, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionDependency(ctx, params)
}

func (tx *Tx) CreateDeliveryTicketRevisionApproval(ctx context.Context, params CreateDeliveryTicketRevisionApprovalParams) (DeliveryTicketRevisionApproval, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionApproval(ctx, params)
}

func (tx *Tx) CreateDeliveryTicketSelection(ctx context.Context, params CreateDeliveryTicketSelectionParams) (DeliveryTicketSelection, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketSelection(ctx, params)
}

func (tx *Tx) TransitionDeliveryTicketSelection(ctx context.Context, selectionID, nextState string) (DeliveryTicketSelection, error) {
	return workflowgenerated.New(tx.tx).TransitionDeliveryTicketSelection(ctx, workflowgenerated.TransitionDeliveryTicketSelectionParams{
		SelectionID: selectionID, State: nextState,
	})
}

func (tx *Tx) CreateDeliveryTicketSelectionMember(ctx context.Context, params CreateDeliveryTicketSelectionMemberParams) (DeliveryTicketSelectionMember, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketSelectionMember(ctx, params)
}
