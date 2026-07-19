package workflowstore

import (
	"context"
	"database/sql"
	"fmt"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

// RecheckPacketApprovalConsistency verifies that the package approval
// identity and approved SHA carried by every obligation for the packet
// still matches the Run's current approval, the execution package, and
// the package's own SHA. Call this inside a transaction right before
// commit when write-read ordering matters.
func (tx *Tx) RecheckPacketApprovalConsistency(ctx context.Context, packetRowID int64) error {
	obligations, err := workflowgenerated.New(tx.tx).ListAuditPacketTicketObligations(ctx, packetRowID)
	if err != nil {
		return fmt.Errorf("recheck ticket obligations: %w", err)
	}
	if len(obligations) == 0 {
		return nil
	}
	var packet AuditPacket
	if err := tx.tx.QueryRowContext(ctx, `
SELECT id, run_row_id
FROM audit_packets
WHERE id = ?`, packetRowID).Scan(&packet.ID, &packet.RunRowID); err != nil {
		return fmt.Errorf("recheck audit packet: %w", err)
	}
	var run struct {
		PackageApprovalRowID  sql.NullInt64
		ExecutionPackageRowID sql.NullInt64
	}
	if err := tx.tx.QueryRowContext(ctx, `
SELECT package_approval_row_id, execution_package_row_id
FROM runs
WHERE id = ?`, packet.RunRowID).Scan(&run.PackageApprovalRowID, &run.ExecutionPackageRowID); err != nil {
		return fmt.Errorf("recheck run: %w", err)
	}
	if !run.PackageApprovalRowID.Valid {
		return fmt.Errorf("run has no package approval")
	}
	for _, obligation := range obligations {
		if !obligation.PackageApprovalRowID.Valid || obligation.ApprovedPackageSha256.String == "" {
			return fmt.Errorf("ticket obligation %d has no package approval identity", obligation.ID)
		}
		if obligation.PackageApprovalRowID.Int64 != run.PackageApprovalRowID.Int64 {
			return fmt.Errorf("ticket obligation %d approval does not match the Run", obligation.ID)
		}
		var packageSha256 string
		if err := tx.tx.QueryRowContext(ctx, `
SELECT package_sha256
FROM execution_packages
WHERE id = ?`, obligation.ExecutionPackageRowID).Scan(&packageSha256); err != nil {
			return fmt.Errorf("recheck execution package: %w", err)
		}
		if obligation.ApprovedPackageSha256.String != packageSha256 {
			return fmt.Errorf("ticket obligation %d approved SHA does not match the execution package", obligation.ID)
		}
	}
	return nil
}
