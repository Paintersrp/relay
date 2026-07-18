package sourcevault

import (
	"context"
	"database/sql"
	"errors"

	workflowstore "relay/internal/store/workflow"
)

type PreparedObjectReadRequest struct {
	Import       ImportResult
	ObjectOID    string
	ExpectedType string
	MaxBytes     int64
}

func (m *Manager) ReadPreparedObject(ctx context.Context, request PreparedObjectReadRequest) (ReadObjectResult, error) {
	if !request.Import.Ready || request.Import.Closure.ClosureID == "" || request.Import.Vault.VaultID == "" || !validOID(request.ObjectOID) || !validObjectType(request.ExpectedType) || request.MaxBytes <= 0 || request.MaxBytes > MaxObjectReadBytes {
		return ReadObjectResult{}, &Error{Code: CodeInvalidRequest}
	}
	closure, err := m.store.GetSourceVaultClosureByClosureID(ctx, request.Import.Closure.ClosureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return ReadObjectResult{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return ReadObjectResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if closure.ID != request.Import.Closure.ID || closure.VaultRowID != request.Import.Closure.VaultRowID || closure.CommitOID != request.Import.CommitOID || closure.TreeOID != request.Import.TreeOID || closure.RefName != request.Import.RefName || closure.Generation != request.Import.Closure.Generation {
		return ReadObjectResult{}, &Error{Code: CodeVaultUnavailable}
	}
	vault, err := m.sourceVaultForClosure(ctx, closure)
	if err != nil {
		return ReadObjectResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if vault.ID != request.Import.Vault.ID || vault.VaultID != request.Import.Vault.VaultID || vault.RepoTarget != request.Import.Vault.RepoTarget || vault.RelativePath != request.Import.Vault.RelativePath {
		return ReadObjectResult{}, &Error{Code: CodeVaultUnavailable}
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return ReadObjectResult{}, managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return ReadObjectResult{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	if err := m.git.VerifyVaultClosure(ctx, vaultPath, closure.CommitOID, closure.TreeOID, closure.RefName); err != nil {
		return ReadObjectResult{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailurePostImportVerification)
	}
	data, err := m.git.ReadObject(ctx, vaultPath, request.ObjectOID, request.ExpectedType, request.MaxBytes)
	if err != nil {
		return ReadObjectResult{}, managerError(ctx, err, CodeObjectUnavailable)
	}
	return ReadObjectResult{ObjectOID: request.ObjectOID, ObjectType: request.ExpectedType, Bytes: data}, nil
}
