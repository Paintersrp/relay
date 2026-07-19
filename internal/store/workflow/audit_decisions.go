package workflowstore

import (
	"context"
	"database/sql"
	"fmt"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

// RecheckDecisionApprovalConsistency verifies that every ticket revision
// decision under a decision row carries the exact package approval identity
// and approved SHA from its obligation, and that the approval matches the
// Run, package, and SHA fields transactionally.
func (tx *Tx) RecheckDecisionApprovalConsistency(ctx context.Context, decisionRowID int64) error {
	decisions, err := workflowgenerated.New(tx.tx).ListAuditTicketRevisionDecisions(ctx, decisionRowID)
	if err != nil {
		return fmt.Errorf("recheck ticket revision decisions: %w", err)
	}
	if len(decisions) == 0 {
		return nil
	}
	var decision AuditDecision
	if err := tx.tx.QueryRowContext(ctx, `
SELECT id, run_row_id
FROM audit_decisions
WHERE id = ?`, decisionRowID).Scan(&decision.ID, &decision.RunRowID); err != nil {
		return fmt.Errorf("recheck audit decision: %w", err)
	}
	for _, revisionDecision := range decisions {
		var obligation struct {
			PackageApprovalRowID  int64
			ApprovedPackageSha256 string
		}
		if err := tx.tx.QueryRowContext(ctx, `
SELECT COALESCE(package_approval_row_id, 0), COALESCE(approved_package_sha256, '')
FROM audit_packet_ticket_obligations
WHERE id = ?`, revisionDecision.AuditPacketTicketObligationRowID).Scan(
			&obligation.PackageApprovalRowID,
			&obligation.ApprovedPackageSha256,
		); err != nil {
			return fmt.Errorf("recheck obligation: %w", err)
		}
		if !revisionDecision.PackageApprovalRowID.Valid || revisionDecision.ApprovedPackageSha256.String == "" {
			return fmt.Errorf("ticket revision decision %d has no package approval identity", revisionDecision.ID)
		}
		if revisionDecision.PackageApprovalRowID.Int64 != obligation.PackageApprovalRowID {
			return fmt.Errorf("ticket revision decision %d approval does not match obligation", revisionDecision.ID)
		}
		if revisionDecision.ApprovedPackageSha256.String != obligation.ApprovedPackageSha256 {
			return fmt.Errorf("ticket revision decision %d approved SHA does not match obligation", revisionDecision.ID)
		}
	}
	return nil
}

// RecheckSatisfactionApprovalConsistency verifies that accepted ticket
// satisfactions reference decisions with valid approval identity.
func (tx *Tx) RecheckSatisfactionApprovalConsistency(ctx context.Context, satisfactionRowID int64) error {
	var decisionRowID, revisionRowID int64
	if err := tx.tx.QueryRowContext(ctx, `
SELECT dts.audit_ticket_revision_decision_row_id, dts.delivery_ticket_revision_row_id
FROM delivery_ticket_revision_satisfactions AS dts
WHERE dts.id = ?`, satisfactionRowID).Scan(&decisionRowID, &revisionRowID); err != nil {
		return fmt.Errorf("recheck satisfaction: %w", err)
	}
	var decision struct {
		PackageApprovalRowID  sql.NullInt64
		ApprovedPackageSha256 sql.NullString
	}
	if err := tx.tx.QueryRowContext(ctx, `
SELECT package_approval_row_id, approved_package_sha256
FROM audit_ticket_revision_decisions
WHERE id = ?`, decisionRowID).Scan(&decision.PackageApprovalRowID, &decision.ApprovedPackageSha256); err != nil {
		return fmt.Errorf("recheck decision: %w", err)
	}
	if !decision.PackageApprovalRowID.Valid {
		return fmt.Errorf("satisfaction %d references decision without package approval identity", satisfactionRowID)
	}
	return nil
}
