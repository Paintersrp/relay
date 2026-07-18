package sourcevault

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

type PreparedRetention struct {
	PacketID        string
	DependencyClass string
	DependencyKey   string
	OwnerIdentity   string
	Vault           workflowstore.SourceVault
	Closure         workflowstore.SourceVaultClosure
}

func (m *Manager) PrepareRetention(ctx context.Context, request RetainRequest) (PreparedRetention, error) {
	ownerIdentity, err := workflowstore.SourceVaultRetentionOwnerIdentity(request.PacketID, request.DependencyClass, request.DependencyKey)
	if err != nil || !validPacketRetentionDependencyClass(request.DependencyClass) || request.OwnerClass != workflowstore.SourceVaultOwnerOperationPacket || request.OwnerIdentity != ownerIdentity {
		return PreparedRetention{}, &Error{Code: CodeInvalidRequest}
	}
	closure, err := m.store.GetSourceVaultClosureByClosureID(ctx, request.ClosureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return PreparedRetention{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return PreparedRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	vault, err := m.sourceVaultForClosure(ctx, closure)
	if err != nil {
		return PreparedRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return PreparedRetention{}, managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return PreparedRetention{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	if err := m.git.VerifyVaultClosure(ctx, vaultPath, closure.CommitOID, closure.TreeOID, closure.RefName); err != nil {
		return PreparedRetention{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailurePostImportVerification)
	}
	return PreparedRetention{
		PacketID:        request.PacketID,
		DependencyClass: request.DependencyClass,
		DependencyKey:   request.DependencyKey,
		OwnerIdentity:   ownerIdentity,
		Vault:           vault,
		Closure:         closure,
	}, nil
}

func (m *Manager) RetainPreparedInTx(ctx context.Context, tx *workflowstore.Tx, prepared PreparedRetention) (workflowstore.SourceVaultRetention, error) {
	if tx == nil {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeInvalidRequest}
	}
	ownerIdentity, err := workflowstore.SourceVaultRetentionOwnerIdentity(prepared.PacketID, prepared.DependencyClass, prepared.DependencyKey)
	if err != nil || ownerIdentity != prepared.OwnerIdentity {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeInvalidRequest}
	}
	closure, err := tx.GetSourceVaultClosureByClosureID(ctx, prepared.Closure.ClosureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if closure.ID != prepared.Closure.ID || closure.VaultRowID != prepared.Vault.ID || closure.CommitOID != prepared.Closure.CommitOID || closure.TreeOID != prepared.Closure.TreeOID || closure.Generation != prepared.Closure.Generation || closure.RefName != prepared.Closure.RefName {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeVaultUnavailable}
	}
	vault, err := tx.GetSourceVaultByVaultID(ctx, prepared.Vault.VaultID)
	if err != nil || vault.ID != prepared.Vault.ID || vault.RepoTarget != prepared.Vault.RepoTarget || vault.RelativePath != prepared.Vault.RelativePath {
		if err == nil {
			err = workflowstore.ErrSourceVaultStateConflict
		}
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	retention, err := tx.CreateOrGetSourceVaultRetention(ctx, workflowstore.CreateSourceVaultRetentionParams{
		RetentionID:   workflowstore.NewSourceVaultRetentionID(),
		ClosureRowID:  closure.ID,
		OwnerClass:    workflowstore.SourceVaultOwnerOperationPacket,
		OwnerIdentity: ownerIdentity,
	})
	if err == nil {
		if retention.ClosureRowID != closure.ID || retention.OwnerClass != workflowstore.SourceVaultOwnerOperationPacket || retention.OwnerIdentity != ownerIdentity || retention.State != workflowstore.SourceVaultRetentionStateActive {
			return workflowstore.SourceVaultRetention{}, &Error{Code: CodeRetentionConflict}
		}
		return retention, nil
	}
	winner, winnerErr := tx.GetActiveSourceVaultRetentionByOwner(ctx, workflowstore.SourceVaultOwnerOperationPacket, ownerIdentity)
	if winnerErr == nil {
		if winner.ClosureRowID == closure.ID {
			return winner, nil
		}
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeRetentionConflict}
	}
	if !errors.Is(winnerErr, sql.ErrNoRows) {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, winnerErr, CodeDatabaseFailure)
	}
	if errors.Is(err, workflowstore.ErrSourceVaultRetentionConflict) {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeRetentionConflict}
	}
	return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
}

// PrepareInvestigationRetention verifies an exact closure before the caller
// creates its immutable investigation evidence. It returns no vault path or
// object access capability.
func (m *Manager) PrepareInvestigationRetention(ctx context.Context, closureID, investigationID string) (PreparedInvestigationRetention, error) {
	if strings.TrimSpace(closureID) != closureID || closureID == "" || strings.TrimSpace(investigationID) != investigationID || investigationID == "" || len(investigationID) > 512 {
		return PreparedInvestigationRetention{}, &Error{Code: CodeInvalidRequest}
	}
	closure, err := m.store.GetSourceVaultClosureByClosureID(ctx, closureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return PreparedInvestigationRetention{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return PreparedInvestigationRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	vault, err := m.sourceVaultForClosure(ctx, closure)
	if err != nil {
		return PreparedInvestigationRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return PreparedInvestigationRetention{}, managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return PreparedInvestigationRetention{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	if err := m.git.VerifyVaultClosure(ctx, vaultPath, closure.CommitOID, closure.TreeOID, closure.RefName); err != nil {
		return PreparedInvestigationRetention{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailurePostImportVerification)
	}
	return PreparedInvestigationRetention{OwnerIdentity: investigationID, Vault: vault, Closure: closure}, nil
}

// RetainPreparedInvestigationInTx creates the retention edge in the same
// transaction as its durable investigation row.
func (m *Manager) RetainPreparedInvestigationInTx(ctx context.Context, tx *workflowstore.Tx, prepared PreparedInvestigationRetention) (workflowstore.SourceVaultRetention, error) {
	if tx == nil || strings.TrimSpace(prepared.OwnerIdentity) != prepared.OwnerIdentity || prepared.OwnerIdentity == "" {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeInvalidRequest}
	}
	closure, err := tx.GetSourceVaultClosureByClosureID(ctx, prepared.Closure.ClosureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if closure.ID != prepared.Closure.ID || closure.VaultRowID != prepared.Vault.ID || closure.CommitOID != prepared.Closure.CommitOID || closure.TreeOID != prepared.Closure.TreeOID || closure.Generation != prepared.Closure.Generation || closure.RefName != prepared.Closure.RefName {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeVaultUnavailable}
	}
	vault, err := tx.GetSourceVaultByVaultID(ctx, prepared.Vault.VaultID)
	if err != nil || vault.ID != prepared.Vault.ID || vault.RepoTarget != prepared.Vault.RepoTarget || vault.RelativePath != prepared.Vault.RelativePath {
		if err == nil {
			err = workflowstore.ErrSourceVaultStateConflict
		}
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	retention, err := tx.CreateOrGetSourceVaultRetention(ctx, workflowstore.CreateSourceVaultRetentionParams{
		RetentionID:   workflowstore.NewSourceVaultRetentionID(),
		ClosureRowID:  closure.ID,
		OwnerClass:    workflowstore.SourceVaultOwnerArtifact,
		OwnerIdentity: prepared.OwnerIdentity,
	})
	if err == nil {
		if retention.ClosureRowID != closure.ID || retention.OwnerClass != workflowstore.SourceVaultOwnerArtifact || retention.OwnerIdentity != prepared.OwnerIdentity || retention.State != workflowstore.SourceVaultRetentionStateActive {
			return workflowstore.SourceVaultRetention{}, &Error{Code: CodeRetentionConflict}
		}
		return retention, nil
	}
	winner, winnerErr := tx.GetActiveSourceVaultRetentionByOwner(ctx, workflowstore.SourceVaultOwnerArtifact, prepared.OwnerIdentity)
	if winnerErr == nil {
		if winner.ClosureRowID == closure.ID {
			return winner, nil
		}
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeRetentionConflict}
	}
	if !errors.Is(winnerErr, sql.ErrNoRows) {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, winnerErr, CodeDatabaseFailure)
	}
	if errors.Is(err, workflowstore.ErrSourceVaultRetentionConflict) {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeRetentionConflict}
	}
	return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
}

func (m *Manager) VerifyActiveRetentionEdge(ctx context.Context, relationship workflowstore.OperationPacketVaultRelationship) error {
	retention, err := m.store.GetSourceVaultRetentionByRowID(ctx, relationship.RetentionRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	if retention.State != workflowstore.SourceVaultRetentionStateActive || retention.OwnerClass != workflowstore.SourceVaultOwnerOperationPacket || retention.OwnerIdentity != relationship.OwnerIdentity || retention.ClosureRowID != relationship.ClosureRowID {
		return &Error{Code: CodeVaultUnavailable}
	}
	closure, err := m.store.GetSourceVaultClosureByRowID(ctx, relationship.ClosureRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	if closure.State != workflowstore.SourceVaultClosureStateReady || closure.VaultRowID != relationship.VaultRowID || closure.CommitOID != relationship.CommitOID || closure.TreeOID != relationship.TreeOID {
		return &Error{Code: CodeVaultUnavailable}
	}
	vault, err := m.store.GetSourceVaultByRowID(ctx, relationship.VaultRowID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	if vault.ID != closure.VaultRowID {
		return &Error{Code: CodeVaultUnavailable}
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.VerifyVaultClosure(ctx, vaultPath, closure.CommitOID, closure.TreeOID, closure.RefName); err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	return nil
}

func validPacketRetentionDependencyClass(value string) bool {
	switch value {
	case workflowstore.OperationPacketDependencyRepositoryVault,
		workflowstore.OperationPacketDependencyGitPathObject,
		workflowstore.OperationPacketDependencyManifestMember:
		return true
	default:
		return false
	}
}
