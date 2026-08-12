package workflowstore

import (
	"context"
	"database/sql"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

// Generated Delivery Plan persistence types remain behind the workflow store
// boundary.
type (
	DeliveryPlan                = workflowgenerated.DeliveryPlan
	DeliveryPlanUnit            = workflowgenerated.DeliveryPlanUnit
	DeliveryPlanUnitDependency  = workflowgenerated.DeliveryPlanUnitDependency
	DeliveryTicketPlanUnitLink  = workflowgenerated.DeliveryTicketPlanUnitLink

	CreateDeliveryPlanParams                    = workflowgenerated.CreateDeliveryPlanParams
	CreateDeliveryPlanUnitParams                = workflowgenerated.CreateDeliveryPlanUnitParams
	CreateDeliveryPlanUnitDependencyParams      = workflowgenerated.CreateDeliveryPlanUnitDependencyParams
	CreateDeliveryTicketPlanUnitLinkParams      = workflowgenerated.CreateDeliveryTicketPlanUnitLinkParams
)

func (s *Store) GetDeliveryPlanByPlanID(ctx context.Context, planID string) (DeliveryPlan, error) {
	return workflowgenerated.New(s.db).GetDeliveryPlanByPlanID(ctx, planID)
}

func (s *Store) GetDeliveryPlanByRowID(ctx context.Context, rowID int64) (DeliveryPlan, error) {
	return workflowgenerated.New(s.db).GetDeliveryPlanByRowID(ctx, rowID)
}

func (s *Store) GetDeliveryPlanByCandidateRowID(ctx context.Context, candidateRowID int64) (DeliveryPlan, error) {
	return workflowgenerated.New(s.db).GetDeliveryPlanByCandidateRowID(ctx, candidateRowID)
}

func (s *Store) ListDeliveryPlansByWorkspace(ctx context.Context, workspaceRowID int64) ([]DeliveryPlan, error) {
	return workflowgenerated.New(s.db).ListDeliveryPlansByWorkspace(ctx, workspaceRowID)
}

func (s *Store) ListDeliveryPlanUnitsByPlan(ctx context.Context, planRowID int64) ([]DeliveryPlanUnit, error) {
	return workflowgenerated.New(s.db).ListDeliveryPlanUnitsByPlan(ctx, planRowID)
}

func (s *Store) ListDeliveryPlanUnitDependenciesByUnit(ctx context.Context, unitRowID int64) ([]DeliveryPlanUnitDependency, error) {
	return workflowgenerated.New(s.db).ListDeliveryPlanUnitDependenciesByUnit(ctx, unitRowID)
}

func (s *Store) GetDeliveryTicketPlanUnitLinkByUnitRowID(ctx context.Context, unitRowID int64) (DeliveryTicketPlanUnitLink, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketPlanUnitLinkByUnitRowID(ctx, unitRowID)
}

func (s *Store) GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx context.Context, ticketRowID int64) (DeliveryTicketPlanUnitLink, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx, ticketRowID)
}

func (s *Store) ListDeliveryTicketPlanUnitLinksByPlan(ctx context.Context, planRowID int64) ([]DeliveryTicketPlanUnitLink, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketPlanUnitLinksByPlan(ctx, planRowID)
}

func (tx *Tx) GetDeliveryPlanByPlanID(ctx context.Context, planID string) (DeliveryPlan, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryPlanByPlanID(ctx, planID)
}

func (tx *Tx) GetDeliveryPlanByRowID(ctx context.Context, rowID int64) (DeliveryPlan, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryPlanByRowID(ctx, rowID)
}

func (tx *Tx) GetDeliveryPlanByCandidateRowID(ctx context.Context, candidateRowID int64) (DeliveryPlan, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryPlanByCandidateRowID(ctx, candidateRowID)
}

func (tx *Tx) ListDeliveryPlansByWorkspace(ctx context.Context, workspaceRowID int64) ([]DeliveryPlan, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryPlansByWorkspace(ctx, workspaceRowID)
}

func (tx *Tx) CreateDeliveryPlan(ctx context.Context, params CreateDeliveryPlanParams) (DeliveryPlan, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryPlan(ctx, params)
}

func (tx *Tx) CreateDeliveryPlanUnit(ctx context.Context, params CreateDeliveryPlanUnitParams) (DeliveryPlanUnit, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryPlanUnit(ctx, params)
}

func (tx *Tx) CreateDeliveryPlanUnitDependency(ctx context.Context, params CreateDeliveryPlanUnitDependencyParams) (DeliveryPlanUnitDependency, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryPlanUnitDependency(ctx, params)
}

func (tx *Tx) ListDeliveryPlanUnitsByPlan(ctx context.Context, planRowID int64) ([]DeliveryPlanUnit, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryPlanUnitsByPlan(ctx, planRowID)
}

func (tx *Tx) ListDeliveryPlanUnitDependenciesByUnit(ctx context.Context, unitRowID int64) ([]DeliveryPlanUnitDependency, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryPlanUnitDependenciesByUnit(ctx, unitRowID)
}

func (tx *Tx) CreateDeliveryTicketPlanUnitLink(ctx context.Context, params CreateDeliveryTicketPlanUnitLinkParams) (DeliveryTicketPlanUnitLink, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketPlanUnitLink(ctx, params)
}

func (tx *Tx) GetDeliveryTicketPlanUnitLinkByUnitRowID(ctx context.Context, unitRowID int64) (DeliveryTicketPlanUnitLink, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryTicketPlanUnitLinkByUnitRowID(ctx, unitRowID)
}

func (tx *Tx) GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx context.Context, ticketRowID int64) (DeliveryTicketPlanUnitLink, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx, ticketRowID)
}

func (tx *Tx) ListDeliveryTicketPlanUnitLinksByPlan(ctx context.Context, planRowID int64) ([]DeliveryTicketPlanUnitLink, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryTicketPlanUnitLinksByPlan(ctx, planRowID)
}

// SetCurrentDeliveryPlan makes one approved Delivery Plan the workspace's
// current approved Plan. The workspace version must match exactly; the update
// records currentness only and never alters Ticket or package currentness.
func (tx *Tx) SetCurrentDeliveryPlan(ctx context.Context, planRowID int64, workspaceID string, expectedVersion int64) (FeatureWorkspace, error) {
	return workflowgenerated.New(tx.tx).SetFeatureWorkspaceCurrentDeliveryPlan(ctx, workflowgenerated.SetFeatureWorkspaceCurrentDeliveryPlanParams{
		CurrentDeliveryPlanRowID: sql.NullInt64{Int64: planRowID, Valid: true}, WorkspaceID: workspaceID, Version: expectedVersion,
	})
}

func (s *Store) SetCurrentDeliveryPlan(ctx context.Context, planRowID int64, workspaceID string, expectedVersion int64) (FeatureWorkspace, error) {
	return workflowgenerated.New(s.db).SetFeatureWorkspaceCurrentDeliveryPlan(ctx, workflowgenerated.SetFeatureWorkspaceCurrentDeliveryPlanParams{
		CurrentDeliveryPlanRowID: sql.NullInt64{Int64: planRowID, Valid: true}, WorkspaceID: workspaceID, Version: expectedVersion,
	})
}
