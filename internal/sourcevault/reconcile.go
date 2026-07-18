package sourcevault

import (
	"context"
	"fmt"
	"strings"
	"time"

	workflowstore "relay/internal/store/workflow"
)

func (m *Manager) Reconcile(ctx context.Context) error {
	closures, err := m.store.ListSourceVaultClosuresForReconciliation(ctx)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	if err := validateCurrentGenerations(closures); err != nil {
		return managerError(ctx, err, CodeStateConflict)
	}
	for _, closure := range closures {
		if err := m.reconcileClosure(ctx, closure); err != nil {
			return managerError(ctx, err, CodeInternal)
		}
	}
	return nil
}

func (m *Manager) reconcileClosure(ctx context.Context, closure workflowstore.SourceVaultClosure) error {
	vault, err := m.sourceVaultForClosure(ctx, closure)
	if err != nil {
		return err
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, pathErr := m.git.VaultPath(vault.RelativePath)
	if pathErr != nil {
		if closure.State == workflowstore.SourceVaultClosureStateReleased {
			return pathErr
		}
		return m.failClosure(ctx, closure, pathErr, workflowstore.SourceVaultFailureVaultInvalid)
	}

	switch closure.State {
	case workflowstore.SourceVaultClosureStateImporting:
		return m.reconcileImporting(ctx, vaultPath, closure)
	case workflowstore.SourceVaultClosureStateReady:
		if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
			return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
		}
		if err := m.git.VerifyVaultClosure(ctx, vaultPath, closure.CommitOID, closure.TreeOID, closure.RefName); err != nil {
			return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailurePostImportVerification)
		}
		return nil
	case workflowstore.SourceVaultClosureStateUnavailable:
		if !closure.FailureReason.Valid {
			return &Error{Code: CodeStateConflict}
		}
		return nil
	case workflowstore.SourceVaultClosureStateReleasing:
		return m.reconcileReleasing(ctx, vaultPath, closure)
	case workflowstore.SourceVaultClosureStateReleased:
		return m.reconcileReleased(ctx, vaultPath, closure)
	default:
		return &Error{Code: CodeStateConflict}
	}
}

func (m *Manager) reconcileImporting(ctx context.Context, vaultPath string, closure workflowstore.SourceVaultClosure) error {
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	oid, exists, err := m.git.ReadRef(ctx, vaultPath, closure.RefName)
	if err != nil {
		return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureInterruptedImport)
	}
	if exists && oid != closure.CommitOID {
		return m.failClosure(ctx, closure, &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}, workflowstore.SourceVaultFailureRefMismatch)
	}
	if exists {
		if err := m.git.DeleteRef(ctx, vaultPath, closure.RefName, closure.CommitOID); err != nil {
			return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureRefDeleteFailed)
		}
		_, stillExists, readErr := m.git.ReadRef(ctx, vaultPath, closure.RefName)
		if readErr != nil || stillExists {
			return m.failClosure(ctx, closure, readErr, workflowstore.SourceVaultFailureRefDeleteFailed)
		}
	}
	return m.markUnavailable(ctx, closure, workflowstore.SourceVaultFailureInterruptedImport)
}

func (m *Manager) reconcileReleasing(ctx context.Context, vaultPath string, closure workflowstore.SourceVaultClosure) error {
	active, err := m.store.CountActiveSourceVaultRetentions(ctx, closure.ID)
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	if active != 0 {
		return m.failClosure(ctx, closure, &gitFailure{reason: workflowstore.SourceVaultFailureReleaseOwnerConflict, code: CodeRetentionConflict}, workflowstore.SourceVaultFailureReleaseOwnerConflict)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	oid, exists, err := m.git.ReadRef(ctx, vaultPath, closure.RefName)
	if err != nil {
		return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureReleaseInterrupted)
	}
	if exists && oid != closure.CommitOID {
		return m.failClosure(ctx, closure, &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}, workflowstore.SourceVaultFailureRefMismatch)
	}
	if exists {
		if err := m.git.DeleteRef(ctx, vaultPath, closure.RefName, closure.CommitOID); err != nil {
			return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureRefDeleteFailed)
		}
	}
	_, stillExists, err := m.git.ReadRef(ctx, vaultPath, closure.RefName)
	if err != nil || stillExists {
		return m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureRefDeleteFailed)
	}
	var released workflowstore.SourceVaultClosure
	err = m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var transitionErr error
		released, transitionErr = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID:     closure.ClosureID,
			ExpectedState: workflowstore.SourceVaultClosureStateReleasing,
			NextState:     workflowstore.SourceVaultClosureStateReleased,
			TransitionAt:  canonicalTime(time.Now()),
		})
		return transitionErr
	})
	if err != nil {
		return managerError(ctx, err, CodeDatabaseFailure)
	}
	_ = released
	return nil
}

func (m *Manager) reconcileReleased(ctx context.Context, vaultPath string, closure workflowstore.SourceVaultClosure) error {
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	oid, exists, err := m.git.ReadRef(ctx, vaultPath, closure.RefName)
	if err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	if !exists {
		return nil
	}
	if oid != closure.CommitOID {
		return &Error{Code: CodeObjectMismatch, FailureReason: workflowstore.SourceVaultFailureRefMismatch}
	}
	if err := m.git.DeleteRef(ctx, vaultPath, closure.RefName, closure.CommitOID); err != nil {
		return managerError(ctx, err, CodeVaultUnavailable)
	}
	return nil
}

func validateCurrentGenerations(closures []workflowstore.SourceVaultClosure) error {
	current := make(map[string]string)
	for _, closure := range closures {
		if closure.State == workflowstore.SourceVaultClosureStateReleased {
			continue
		}
		key := strings.Join([]string{
			fmt.Sprint(closure.VaultRowID),
			closure.CommitOID,
			closure.TreeOID,
		}, "\x00")
		if _, ok := current[key]; ok {
			return &Error{Code: CodeStateConflict}
		}
		current[key] = closure.ClosureID
	}
	return nil
}
