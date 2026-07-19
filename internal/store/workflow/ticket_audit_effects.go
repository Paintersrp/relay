package workflowstore

import (
	"context"
	"database/sql"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

// Ticket-route audit records are exposed through the store boundary so audit,
// ticket, and feature owners can apply one immutable decision atomically.
type (
	AuditPacketTicketObligation         = workflowgenerated.AuditPacketTicketObligation
	AuditTicketRevisionDecision         = workflowgenerated.AuditTicketRevisionDecision
	AuditRemediationSeed                = workflowgenerated.AuditRemediationSeed
	AuditRemediationSeedFinding         = workflowgenerated.AuditRemediationSeedFinding
	AuditRemediationSeedReopening       = workflowgenerated.AuditRemediationSeedReopening
	DeliveryTicketRevisionSatisfaction  = workflowgenerated.DeliveryTicketRevisionSatisfaction
	FeatureWorkspaceCompletionDecision  = workflowgenerated.FeatureWorkspaceCompletionDecision
	FeatureWorkspaceCompletionReopening = workflowgenerated.FeatureWorkspaceCompletionReopening

	CreateAuditPacketTicketObligationParams         = workflowgenerated.CreateAuditPacketTicketObligationParams
	CreateAuditTicketRevisionDecisionParams         = workflowgenerated.CreateAuditTicketRevisionDecisionParams
	CreateAuditRemediationSeedParams                = workflowgenerated.CreateAuditRemediationSeedParams
	CreateAuditRemediationSeedFindingParams         = workflowgenerated.CreateAuditRemediationSeedFindingParams
	CreateAuditRemediationSeedReopeningParams       = workflowgenerated.CreateAuditRemediationSeedReopeningParams
	CreateDeliveryTicketRevisionSatisfactionParams  = workflowgenerated.CreateDeliveryTicketRevisionSatisfactionParams
	CreateFeatureWorkspaceCompletionDecisionParams  = workflowgenerated.CreateFeatureWorkspaceCompletionDecisionParams
	CreateFeatureWorkspaceCompletionReopeningParams = workflowgenerated.CreateFeatureWorkspaceCompletionReopeningParams
)

func (s *Store) ListAuditPacketTicketObligations(ctx context.Context, packetRowID int64) ([]AuditPacketTicketObligation, error) {
	return workflowgenerated.New(s.db).ListAuditPacketTicketObligations(ctx, packetRowID)
}

func (s *Store) GetAuditPacketTicketObligationByRowID(ctx context.Context, obligationRowID int64) (AuditPacketTicketObligation, error) {
	return getAuditPacketTicketObligationByRowID(ctx, s.db, obligationRowID)
}

func (s *Store) GetAuditRemediationSeedBySeedID(ctx context.Context, seedID string) (AuditRemediationSeed, error) {
	return workflowgenerated.New(s.db).GetAuditRemediationSeedBySeedID(ctx, seedID)
}

func (s *Store) GetAuditRemediationSeedReopening(ctx context.Context, seedRowID int64) (AuditRemediationSeedReopening, error) {
	return workflowgenerated.New(s.db).GetAuditRemediationSeedReopening(ctx, seedRowID)
}

func (s *Store) ListAuditRemediationSeedsByWorkspace(ctx context.Context, workspaceRowID int64) ([]AuditRemediationSeed, error) {
	return workflowgenerated.New(s.db).ListAuditRemediationSeedsByWorkspace(ctx, workspaceRowID)
}

func (s *Store) ListAuditRemediationSeedFindings(ctx context.Context, seedRowID int64) ([]AuditRemediationSeedFinding, error) {
	return workflowgenerated.New(s.db).ListAuditRemediationSeedFindings(ctx, seedRowID)
}

func (s *Store) GetDeliveryTicketRevisionSatisfaction(ctx context.Context, revisionRowID int64) (DeliveryTicketRevisionSatisfaction, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketRevisionSatisfaction(ctx, revisionRowID)
}

func (s *Store) GetCurrentFeatureWorkspaceCompletionDecision(ctx context.Context, workspaceRowID int64) (FeatureWorkspaceCompletionDecision, error) {
	return workflowgenerated.New(s.db).GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspaceRowID)
}

func (tx *Tx) ListAuditPacketTicketObligations(ctx context.Context, packetRowID int64) ([]AuditPacketTicketObligation, error) {
	return workflowgenerated.New(tx.tx).ListAuditPacketTicketObligations(ctx, packetRowID)
}

func (tx *Tx) GetAuditPacketTicketObligationByRowID(ctx context.Context, obligationRowID int64) (AuditPacketTicketObligation, error) {
	return getAuditPacketTicketObligationByRowID(ctx, tx.tx, obligationRowID)
}

func (tx *Tx) CreateAuditPacketTicketObligation(ctx context.Context, params CreateAuditPacketTicketObligationParams) (AuditPacketTicketObligation, error) {
	return workflowgenerated.New(tx.tx).CreateAuditPacketTicketObligation(ctx, params)
}

func (tx *Tx) CreateAuditTicketRevisionDecision(ctx context.Context, params CreateAuditTicketRevisionDecisionParams) (AuditTicketRevisionDecision, error) {
	return workflowgenerated.New(tx.tx).CreateAuditTicketRevisionDecision(ctx, params)
}

func (tx *Tx) GetAuditTicketRevisionDecisionByRowID(ctx context.Context, decisionRowID int64) (AuditTicketRevisionDecision, error) {
	var value AuditTicketRevisionDecision
	err := tx.tx.QueryRowContext(ctx, `
SELECT id, audit_decision_row_id, audit_packet_ticket_obligation_row_id, created_at
FROM audit_ticket_revision_decisions
WHERE id = ?`, decisionRowID).Scan(
		&value.ID, &value.AuditDecisionRowID, &value.AuditPacketTicketObligationRowID, &value.CreatedAt,
	)
	return value, err
}

func (tx *Tx) CreateDeliveryTicketRevisionSatisfaction(ctx context.Context, params CreateDeliveryTicketRevisionSatisfactionParams) (DeliveryTicketRevisionSatisfaction, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionSatisfaction(ctx, params)
}

func (tx *Tx) CreateAuditRemediationSeed(ctx context.Context, params CreateAuditRemediationSeedParams) (AuditRemediationSeed, error) {
	return workflowgenerated.New(tx.tx).CreateAuditRemediationSeed(ctx, params)
}

func (tx *Tx) CreateAuditRemediationSeedFinding(ctx context.Context, params CreateAuditRemediationSeedFindingParams) (AuditRemediationSeedFinding, error) {
	return workflowgenerated.New(tx.tx).CreateAuditRemediationSeedFinding(ctx, params)
}

func (tx *Tx) GetAuditRemediationSeedBySeedID(ctx context.Context, seedID string) (AuditRemediationSeed, error) {
	return workflowgenerated.New(tx.tx).GetAuditRemediationSeedBySeedID(ctx, seedID)
}

func (tx *Tx) GetAuditRemediationSeedReopening(ctx context.Context, seedRowID int64) (AuditRemediationSeedReopening, error) {
	return workflowgenerated.New(tx.tx).GetAuditRemediationSeedReopening(ctx, seedRowID)
}

func (tx *Tx) ListAuditRemediationSeedsByWorkspace(ctx context.Context, workspaceRowID int64) ([]AuditRemediationSeed, error) {
	return workflowgenerated.New(tx.tx).ListAuditRemediationSeedsByWorkspace(ctx, workspaceRowID)
}

func (tx *Tx) ListAuditRemediationSeedFindings(ctx context.Context, seedRowID int64) ([]AuditRemediationSeedFinding, error) {
	return workflowgenerated.New(tx.tx).ListAuditRemediationSeedFindings(ctx, seedRowID)
}

func (tx *Tx) CreateAuditRemediationSeedReopening(ctx context.Context, params CreateAuditRemediationSeedReopeningParams) (AuditRemediationSeedReopening, error) {
	return workflowgenerated.New(tx.tx).CreateAuditRemediationSeedReopening(ctx, params)
}

func (tx *Tx) GetDeliveryTicketRevisionSatisfaction(ctx context.Context, revisionRowID int64) (DeliveryTicketRevisionSatisfaction, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryTicketRevisionSatisfaction(ctx, revisionRowID)
}

func (tx *Tx) GetCurrentFeatureWorkspaceCompletionDecision(ctx context.Context, workspaceRowID int64) (FeatureWorkspaceCompletionDecision, error) {
	return workflowgenerated.New(tx.tx).GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspaceRowID)
}

func (tx *Tx) CreateFeatureWorkspaceCompletionDecision(ctx context.Context, params CreateFeatureWorkspaceCompletionDecisionParams) (FeatureWorkspaceCompletionDecision, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceCompletionDecision(ctx, params)
}

func (tx *Tx) CreateFeatureWorkspaceCompletionReopening(ctx context.Context, params CreateFeatureWorkspaceCompletionReopeningParams) (FeatureWorkspaceCompletionReopening, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceCompletionReopening(ctx, params)
}

func getAuditPacketTicketObligationByRowID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, obligationRowID int64) (AuditPacketTicketObligation, error) {
	var value AuditPacketTicketObligation
	err := queryer.QueryRowContext(ctx, `
SELECT id, audit_packet_row_id, execution_package_row_id, execution_package_member_row_id,
       delivery_ticket_row_id, delivery_ticket_revision_row_id, authority_revision_row_id,
       source_closure_row_id, created_at
FROM audit_packet_ticket_obligations
WHERE id = ?`, obligationRowID).Scan(
		&value.ID, &value.AuditPacketRowID, &value.ExecutionPackageRowID, &value.ExecutionPackageMemberRowID,
		&value.DeliveryTicketRowID, &value.DeliveryTicketRevisionRowID, &value.AuthorityRevisionRowID,
		&value.SourceClosureRowID, &value.CreatedAt,
	)
	return value, err
}
