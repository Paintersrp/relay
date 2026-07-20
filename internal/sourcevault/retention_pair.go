package sourcevault

import (
	"context"
	workflowstore "relay/internal/store/workflow"
)

func (m *Manager) withActiveRetentionEdges(ctx context.Context, before, after workflowstore.OperationPacketVaultRelationship, use func(string, workflowstore.SourceVaultClosure, workflowstore.SourceVaultClosure) error) error {
	if m == nil || use == nil {
		return &Error{Code: CodeInvalidRequest}
	}
	br, err := m.store.GetSourceVaultRetentionByRowID(ctx, before.RetentionRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	ar, err := m.store.GetSourceVaultRetentionByRowID(ctx, after.RetentionRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	if !retentionMatchesRelationship(br, before) || !retentionMatchesRelationship(ar, after) {
		return &Error{Code: CodeVaultUnavailable}
	}
	bc, err := m.store.GetSourceVaultClosureByRowID(ctx, before.ClosureRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	ac, err := m.store.GetSourceVaultClosureByRowID(ctx, after.ClosureRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	if !closureMatchesRelationship(bc, before) || !closureMatchesRelationship(ac, after) || bc.VaultRowID != ac.VaultRowID {
		return &Error{Code: CodeVaultUnavailable}
	}
	vault, err := m.store.GetSourceVaultByRowID(ctx, bc.VaultRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	path, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, path); err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.VerifyVaultClosure(ctx, path, bc.CommitOID, bc.TreeOID, bc.RefName); err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	if ac.ID != bc.ID {
		if err := m.git.VerifyVaultClosure(ctx, path, ac.CommitOID, ac.TreeOID, ac.RefName); err != nil {
			return managerError(ctx, err, CodeVaultUnavailable)
		}
	}
	return use(path, bc, ac)
}
func retentionMatchesRelationship(r workflowstore.SourceVaultRetention, v workflowstore.OperationPacketVaultRelationship) bool {
	return r.State == workflowstore.SourceVaultRetentionStateActive && r.OwnerClass == workflowstore.SourceVaultOwnerOperationPacket && r.OwnerIdentity == v.OwnerIdentity && r.ClosureRowID == v.ClosureRowID
}
func closureMatchesRelationship(c workflowstore.SourceVaultClosure, v workflowstore.OperationPacketVaultRelationship) bool {
	return c.State == workflowstore.SourceVaultClosureStateReady && c.VaultRowID == v.VaultRowID && c.CommitOID == v.CommitOID && c.TreeOID == v.TreeOID
}
