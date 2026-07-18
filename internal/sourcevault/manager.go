package sourcevault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

type Manager struct {
	store *workflowstore.Store
	git   gitClient
	locks sync.Map
}

type vaultMutex struct {
	mu sync.Mutex
}

func Open(ctx context.Context, root string, store *workflowstore.Store) (*Manager, error) {
	if store == nil {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	repositories, err := store.ListRepositoryTargetsWithConfiguration(ctx)
	if err != nil {
		return nil, managerError(ctx, err, CodeDatabaseFailure)
	}
	git, err := newCommandGit(ctx, root, repositories)
	if err != nil {
		return nil, managerError(ctx, err, CodeInvalidRequest)
	}
	manager := &Manager{store: store, git: git}
	if err := manager.Reconcile(ctx); err != nil {
		return nil, managerError(ctx, err, CodeInternal)
	}
	return manager, nil
}

func newManager(store *workflowstore.Store, git gitClient) (*Manager, error) {
	if store == nil || git == nil {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	return &Manager{store: store, git: git}, nil
}

func (m *Manager) currentRepositoryAuthority(
	ctx context.Context,
	revision workflowrepos.ResolvedRevision,
) (workflowstore.RepositoryTarget, error) {
	current, err := m.store.GetRepositoryTarget(ctx, revision.RepositoryTarget.RepoTarget)
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.RepositoryTarget{}, &Error{Code: CodeRepositoryMismatch}
	}
	if err != nil {
		return workflowstore.RepositoryTarget{}, err
	}
	if err := validateRepositoryAuthority(current, revision); err != nil {
		return workflowstore.RepositoryTarget{}, err
	}
	return current, nil
}

func validateRepositoryAuthority(
	current workflowstore.RepositoryTarget,
	revision workflowrepos.ResolvedRevision,
) error {
	if current.RepoTarget != revision.RepositoryTarget.RepoTarget ||
		current.LocalPath != revision.RepositoryTarget.LocalPath {
		return &Error{Code: CodeRepositoryMismatch}
	}
	switch revision.RevisionSource {
	case workflowrepos.RevisionSourceConfiguredWorkingBranch:
		if !current.ConfiguredBranchRef.Valid ||
			current.ConfiguredBranchRef.String != revision.ConfiguredWorkingBranchRef ||
			current.ConfigurationVersion != revision.RepositoryTargetConfigurationVersion {
			return &Error{Code: CodeStaleConfiguredAuthority}
		}
	case workflowrepos.RevisionSourceExplicitCommit:
		// Explicit commit authority is intentionally independent of mutable branch configuration.
	default:
		return &Error{Code: CodeInvalidRequest}
	}
	return nil
}

const sourceVaultAcquisitionAttempts = 4

func (m *Manager) acquireImportAuthority(
	ctx context.Context,
	revision workflowrepos.ResolvedRevision,
	startedAt string,
) (workflowstore.SourceVault, workflowstore.SourceVaultClosureAcquisition, error) {
	candidateVaultID := workflowstore.NewSourceVaultID()
	candidateClosureID := workflowstore.NewSourceVaultClosureID()
	candidateRelativePath := filepath.ToSlash(filepath.Join("repositories", candidateVaultID+".git"))
	candidateRefName := "refs/relay/closures/" + candidateClosureID
	var lastErr error

	for attempt := 0; attempt < sourceVaultAcquisitionAttempts; attempt++ {
		vault, acquisition, err := m.tryAcquireImportAuthority(
			ctx,
			revision,
			startedAt,
			candidateVaultID,
			candidateRelativePath,
			candidateClosureID,
			candidateRefName,
		)
		if err == nil {
			return vault, acquisition, nil
		}
		lastErr = err
		var stable *Error
		if errors.As(err, &stable) {
			return workflowstore.SourceVault{}, workflowstore.SourceVaultClosureAcquisition{}, stable
		}
		if ctx.Err() != nil {
			return workflowstore.SourceVault{}, workflowstore.SourceVaultClosureAcquisition{}, ctx.Err()
		}

		winnerVault, winner, winnerErr := m.currentImportAuthority(ctx, revision)
		if winnerErr == nil {
			return winnerVault, winner, nil
		}
		if !errors.Is(winnerErr, sql.ErrNoRows) {
			lastErr = winnerErr
		}
		if attempt+1 == sourceVaultAcquisitionAttempts {
			break
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return workflowstore.SourceVault{}, workflowstore.SourceVaultClosureAcquisition{}, ctx.Err()
		case <-timer.C:
		}
	}
	return workflowstore.SourceVault{}, workflowstore.SourceVaultClosureAcquisition{}, lastErr
}

func (m *Manager) tryAcquireImportAuthority(
	ctx context.Context,
	revision workflowrepos.ResolvedRevision,
	startedAt string,
	candidateVaultID string,
	candidateRelativePath string,
	candidateClosureID string,
	candidateRefName string,
) (workflowstore.SourceVault, workflowstore.SourceVaultClosureAcquisition, error) {
	var vault workflowstore.SourceVault
	var acquisition workflowstore.SourceVaultClosureAcquisition
	err := m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetRepositoryTarget(ctx, revision.RepositoryTarget.RepoTarget)
		if errors.Is(err, sql.ErrNoRows) {
			return &Error{Code: CodeRepositoryMismatch}
		}
		if err != nil {
			return fmt.Errorf("load registered repository target: %w", err)
		}
		if err := validateRepositoryAuthority(current, revision); err != nil {
			return err
		}
		vault, err = tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{
			VaultID:      candidateVaultID,
			RepoTarget:   current.RepoTarget,
			RelativePath: candidateRelativePath,
		})
		if err != nil {
			return err
		}
		acquisition, err = tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID,
			ClosureID:  candidateClosureID,
			CommitOID:  revision.CommitOID,
			TreeOID:    revision.TreeOID,
			RefName:    candidateRefName,
			StartedAt:  startedAt,
		})
		return err
	})
	return vault, acquisition, err
}

func (m *Manager) currentImportAuthority(
	ctx context.Context,
	revision workflowrepos.ResolvedRevision,
) (workflowstore.SourceVault, workflowstore.SourceVaultClosureAcquisition, error) {
	var vault workflowstore.SourceVault
	var acquisition workflowstore.SourceVaultClosureAcquisition
	err := m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetRepositoryTarget(ctx, revision.RepositoryTarget.RepoTarget)
		if errors.Is(err, sql.ErrNoRows) {
			return &Error{Code: CodeRepositoryMismatch}
		}
		if err != nil {
			return fmt.Errorf("load registered repository target: %w", err)
		}
		if err := validateRepositoryAuthority(current, revision); err != nil {
			return err
		}
		vault, err = tx.GetSourceVaultByRepositoryTarget(ctx, current.RepoTarget)
		if err != nil {
			return err
		}
		closure, err := tx.GetCurrentSourceVaultClosureByIdentity(
			ctx,
			vault.ID,
			revision.CommitOID,
			revision.TreeOID,
		)
		if err != nil {
			return err
		}
		switch closure.State {
		case workflowstore.SourceVaultClosureStateReady:
			acquisition = workflowstore.SourceVaultClosureAcquisition{
				Closure:     closure,
				Disposition: workflowstore.SourceVaultClosureAcquisitionReady,
			}
		case workflowstore.SourceVaultClosureStateImporting:
			acquisition = workflowstore.SourceVaultClosureAcquisition{
				Closure:     closure,
				Disposition: workflowstore.SourceVaultClosureAcquisitionImporting,
			}
		case workflowstore.SourceVaultClosureStateReleasing:
			acquisition = workflowstore.SourceVaultClosureAcquisition{
				Closure:     closure,
				Disposition: workflowstore.SourceVaultClosureAcquisitionReleasing,
			}
		case workflowstore.SourceVaultClosureStateUnavailable:
			return sql.ErrNoRows
		default:
			return workflowstore.ErrSourceVaultStateConflict
		}
		return nil
	})
	return vault, acquisition, err
}

func (m *Manager) ImportClosure(ctx context.Context, request ImportRequest) (ImportResult, error) {
	if err := validateResolvedRevision(request.Revision); err != nil {
		return ImportResult{}, err
	}

	current, err := m.currentRepositoryAuthority(ctx, request.Revision)
	if err != nil {
		return ImportResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	sourceExists, err := m.git.ValidateRepositorySeparation(ctx, current.LocalPath)
	if err != nil {
		return ImportResult{}, managerError(ctx, err, CodeUnsafeVaultRoot)
	}

	startedAt := canonicalTime(time.Now())
	var vault workflowstore.SourceVault
	var acquisition workflowstore.SourceVaultClosureAcquisition
	if sourceExists {
		vault, acquisition, err = m.acquireImportAuthority(ctx, request.Revision, startedAt)
	} else {
		vault, acquisition, err = m.currentImportAuthority(ctx, request.Revision)
		if errors.Is(err, sql.ErrNoRows) {
			return ImportResult{}, &Error{
				Code:          CodeSourceObjectUnavailable,
				FailureReason: workflowstore.SourceVaultFailureSourceCommitMissing,
			}
		}
	}
	if err != nil {
		return ImportResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}

	switch acquisition.Disposition {
	case workflowstore.SourceVaultClosureAcquisitionImporting:
		return ImportResult{}, &Error{Code: CodeImportInProgress}
	case workflowstore.SourceVaultClosureAcquisitionReleasing:
		return ImportResult{}, &Error{Code: CodeReleaseInProgress}
	}

	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}

	if acquisition.Disposition == workflowstore.SourceVaultClosureAcquisitionReady {
		if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
			return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailureVaultInvalid)
		}
		if err := m.git.VerifyVaultClosure(ctx, vaultPath, acquisition.Closure.CommitOID, acquisition.Closure.TreeOID, acquisition.Closure.RefName); err != nil {
			return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailurePostImportVerification)
		}
		return importResult(vault, acquisition.Closure), nil
	}

	createdRef := false
	if err := m.git.EnsureVault(ctx, vaultPath); err != nil {
		return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	if err := m.git.VerifySource(ctx, request.Revision.RepositoryTarget.LocalPath, acquisition.Closure.CommitOID, acquisition.Closure.TreeOID); err != nil {
		return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailureSourceCommitMissing)
	}
	if err := m.git.ImportClosure(ctx, request.Revision.RepositoryTarget.LocalPath, vaultPath, acquisition.Closure.CommitOID); err != nil {
		return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailurePackGenerationFailed)
	}
	refOID, exists, err := m.git.ReadRef(ctx, vaultPath, acquisition.Closure.RefName)
	if err != nil {
		return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailureRefCreateFailed)
	}
	if exists && refOID != acquisition.Closure.CommitOID {
		return ImportResult{}, m.failClosure(ctx, acquisition.Closure, &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}, workflowstore.SourceVaultFailureRefMismatch)
	}
	if !exists {
		if err := m.git.CreateRef(ctx, vaultPath, acquisition.Closure.RefName, acquisition.Closure.CommitOID); err != nil {
			return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailureRefCreateFailed)
		}
		createdRef = true
	}
	if err := m.git.VerifyVaultClosure(ctx, vaultPath, acquisition.Closure.CommitOID, acquisition.Closure.TreeOID, acquisition.Closure.RefName); err != nil {
		if createdRef {
			m.removeOwnedRef(ctx, vaultPath, acquisition.Closure)
		}
		return ImportResult{}, m.failClosure(ctx, acquisition.Closure, err, workflowstore.SourceVaultFailurePostImportVerification)
	}

	var ready workflowstore.SourceVaultClosure
	err = m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var transitionErr error
		ready, transitionErr = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID:     acquisition.Closure.ClosureID,
			ExpectedState: workflowstore.SourceVaultClosureStateImporting,
			NextState:     workflowstore.SourceVaultClosureStateReady,
			TransitionAt:  canonicalTime(time.Now()),
		})
		return transitionErr
	})
	if err != nil {
		if createdRef {
			m.removeOwnedRef(ctx, vaultPath, acquisition.Closure)
		}
		return ImportResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	return importResult(vault, ready), nil
}

func (m *Manager) RetainClosure(ctx context.Context, request RetainRequest) (workflowstore.SourceVaultRetention, error) {
	if strings.TrimSpace(request.ClosureID) != request.ClosureID || request.ClosureID == "" || !validOwnerClass(request.OwnerClass) || strings.TrimSpace(request.OwnerIdentity) != request.OwnerIdentity || request.OwnerIdentity == "" || len(request.OwnerIdentity) > 512 {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeInvalidRequest}
	}
	closure, err := m.store.GetSourceVaultClosureByClosureID(ctx, request.ClosureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	vault, err := m.sourceVaultForClosure(ctx, closure)
	if err != nil {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return workflowstore.SourceVaultRetention{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	if err := m.git.VerifyVaultClosure(ctx, vaultPath, closure.CommitOID, closure.TreeOID, closure.RefName); err != nil {
		return workflowstore.SourceVaultRetention{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailurePostImportVerification)
	}

	var retention workflowstore.SourceVaultRetention
	err = m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var createErr error
		retention, createErr = tx.CreateOrGetSourceVaultRetention(ctx, workflowstore.CreateSourceVaultRetentionParams{
			RetentionID:   workflowstore.NewSourceVaultRetentionID(),
			ClosureRowID:  closure.ID,
			OwnerClass:    request.OwnerClass,
			OwnerIdentity: request.OwnerIdentity,
		})
		return createErr
	})
	if err != nil {
		winner, winnerErr := m.store.GetActiveSourceVaultRetentionByOwner(ctx, request.OwnerClass, request.OwnerIdentity)
		if winnerErr == nil {
			if winner.ClosureRowID == closure.ID {
				return winner, nil
			}
			return workflowstore.SourceVaultRetention{}, &Error{Code: CodeRetentionConflict}
		}
		if !errors.Is(winnerErr, sql.ErrNoRows) {
			return workflowstore.SourceVaultRetention{}, managerError(ctx, winnerErr, CodeDatabaseFailure)
		}
		return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	return retention, nil
}

func (m *Manager) ReleaseRetention(ctx context.Context, retentionID string) (workflowstore.SourceVaultRetention, error) {
	if strings.TrimSpace(retentionID) != retentionID || retentionID == "" {
		return workflowstore.SourceVaultRetention{}, &Error{Code: CodeInvalidRequest}
	}
	var retention workflowstore.SourceVaultRetention
	err := m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var releaseErr error
		retention, releaseErr = tx.ReleaseSourceVaultRetention(ctx, workflowstore.ReleaseSourceVaultRetentionParams{
			RetentionID: retentionID,
			ReleasedAt:  canonicalTime(time.Now()),
		})
		return releaseErr
	})
	if err == nil {
		return retention, nil
	}
	winner, readErr := m.store.GetSourceVaultRetentionByRetentionID(ctx, retentionID)
	if readErr == nil && winner.State == workflowstore.SourceVaultRetentionStateReleased {
		return winner, nil
	}
	if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
		return workflowstore.SourceVaultRetention{}, managerError(ctx, readErr, CodeDatabaseFailure)
	}
	return workflowstore.SourceVaultRetention{}, managerError(ctx, err, CodeDatabaseFailure)
}

func (m *Manager) CleanupClosure(ctx context.Context, closureID string) (workflowstore.SourceVaultClosure, error) {
	if strings.TrimSpace(closureID) != closureID || closureID == "" {
		return workflowstore.SourceVaultClosure{}, &Error{Code: CodeInvalidRequest}
	}
	var releasing workflowstore.SourceVaultClosure
	err := m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var beginErr error
		releasing, beginErr = tx.BeginSourceVaultClosureRelease(ctx, closureID, canonicalTime(time.Now()))
		return beginErr
	})
	if err != nil {
		return workflowstore.SourceVaultClosure{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	vault, err := m.sourceVaultForClosure(ctx, releasing)
	if err != nil {
		return workflowstore.SourceVaultClosure{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()

	current, err := m.store.GetSourceVaultClosureByClosureID(ctx, releasing.ClosureID)
	if err != nil {
		return workflowstore.SourceVaultClosure{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if current.State != workflowstore.SourceVaultClosureStateReleasing {
		return workflowstore.SourceVaultClosure{}, &Error{Code: CodeStateConflict}
	}
	active, err := m.store.CountActiveSourceVaultRetentions(ctx, current.ID)
	if err != nil {
		return workflowstore.SourceVaultClosure{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if active != 0 {
		return workflowstore.SourceVaultClosure{}, m.failClosure(ctx, current, &gitFailure{reason: workflowstore.SourceVaultFailureReleaseOwnerConflict, code: CodeRetentionConflict}, workflowstore.SourceVaultFailureReleaseOwnerConflict)
	}
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return workflowstore.SourceVaultClosure{}, m.failClosure(ctx, current, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return workflowstore.SourceVaultClosure{}, m.failClosure(ctx, current, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	refOID, exists, err := m.git.ReadRef(ctx, vaultPath, current.RefName)
	if err != nil {
		return workflowstore.SourceVaultClosure{}, m.failClosure(ctx, current, err, workflowstore.SourceVaultFailureRefDeleteFailed)
	}
	if exists && refOID != current.CommitOID {
		return workflowstore.SourceVaultClosure{}, m.failClosure(ctx, current, &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}, workflowstore.SourceVaultFailureRefMismatch)
	}
	if exists {
		if err := m.git.DeleteRef(ctx, vaultPath, current.RefName, current.CommitOID); err != nil {
			return workflowstore.SourceVaultClosure{}, m.failClosure(ctx, current, err, workflowstore.SourceVaultFailureRefDeleteFailed)
		}
	}
	_, stillExists, err := m.git.ReadRef(ctx, vaultPath, current.RefName)
	if err != nil || stillExists {
		return workflowstore.SourceVaultClosure{}, m.failClosure(ctx, current, err, workflowstore.SourceVaultFailureRefDeleteFailed)
	}

	var released workflowstore.SourceVaultClosure
	err = m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var transitionErr error
		released, transitionErr = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID:     current.ClosureID,
			ExpectedState: workflowstore.SourceVaultClosureStateReleasing,
			NextState:     workflowstore.SourceVaultClosureStateReleased,
			TransitionAt:  canonicalTime(time.Now()),
		})
		return transitionErr
	})
	if err != nil {
		return workflowstore.SourceVaultClosure{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if err := m.git.GarbageCollect(ctx, vaultPath); err != nil {
		return released, managerError(ctx, err, CodeVaultUnavailable)
	}
	return released, nil
}

func (m *Manager) ReadObject(ctx context.Context, request ReadObjectRequest) (ReadObjectResult, error) {
	if strings.TrimSpace(request.ClosureID) != request.ClosureID || request.ClosureID == "" || !validOID(request.ObjectOID) || !validObjectType(request.ExpectedType) || request.MaxBytes <= 0 || request.MaxBytes > MaxObjectReadBytes {
		return ReadObjectResult{}, &Error{Code: CodeInvalidRequest}
	}
	closure, err := m.store.GetSourceVaultClosureByClosureID(ctx, request.ClosureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return ReadObjectResult{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return ReadObjectResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	active, err := m.store.CountActiveSourceVaultRetentions(ctx, closure.ID)
	if err != nil {
		return ReadObjectResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if active == 0 {
		return ReadObjectResult{}, &Error{Code: CodeVaultUnavailable}
	}
	vault, err := m.sourceVaultForClosure(ctx, closure)
	if err != nil {
		return ReadObjectResult{}, managerError(ctx, err, CodeDatabaseFailure)
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

func (m *Manager) failClosure(ctx context.Context, closure workflowstore.SourceVaultClosure, cause error, fallback string) error {
	reason := failureReason(ctx, cause, fallback)
	if closure.State != workflowstore.SourceVaultClosureStateReleased && closure.State != workflowstore.SourceVaultClosureStateUnavailable {
		if err := m.markUnavailable(ctx, closure, reason); err != nil {
			return err
		}
	}
	return typedFailure(ctx, cause, reason)
}

func (m *Manager) markUnavailable(ctx context.Context, closure workflowstore.SourceVaultClosure, reason string) error {
	withoutCancel := context.WithoutCancel(ctx)
	var transitioned workflowstore.SourceVaultClosure
	err := m.store.WithTx(withoutCancel, func(tx *workflowstore.Tx) error {
		var transitionErr error
		transitioned, transitionErr = tx.TransitionSourceVaultClosure(withoutCancel, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID:     closure.ClosureID,
			ExpectedState: closure.State,
			NextState:     workflowstore.SourceVaultClosureStateUnavailable,
			FailureReason: sql.NullString{String: reason, Valid: true},
			TransitionAt:  canonicalTime(time.Now()),
		})
		return transitionErr
	})
	if err == nil {
		_ = transitioned
		return nil
	}
	winner, readErr := m.store.GetSourceVaultClosureByClosureID(withoutCancel, closure.ClosureID)
	if readErr == nil && winner.State == workflowstore.SourceVaultClosureStateUnavailable && winner.FailureReason.Valid && winner.FailureReason.String == reason {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
		return managerError(ctx, readErr, CodeDatabaseFailure)
	}
	return managerError(ctx, err, CodeDatabaseFailure)
}

func (m *Manager) removeOwnedRef(ctx context.Context, vaultPath string, closure workflowstore.SourceVaultClosure) {
	oid, exists, err := m.git.ReadRef(context.WithoutCancel(ctx), vaultPath, closure.RefName)
	if err == nil && exists && oid == closure.CommitOID {
		_ = m.git.DeleteRef(context.WithoutCancel(ctx), vaultPath, closure.RefName, closure.CommitOID)
	}
}

func (m *Manager) sourceVaultForClosure(ctx context.Context, closure workflowstore.SourceVaultClosure) (workflowstore.SourceVault, error) {
	var vault workflowstore.SourceVault
	err := m.store.DB().QueryRowContext(ctx, `
SELECT id, vault_id, repo_target, relative_path, created_at, updated_at
FROM source_vaults
WHERE id = ?`, closure.VaultRowID).Scan(
		&vault.ID,
		&vault.VaultID,
		&vault.RepoTarget,
		&vault.RelativePath,
		&vault.CreatedAt,
		&vault.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.SourceVault{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return workflowstore.SourceVault{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	return vault, nil
}

func (m *Manager) lockVault(vaultID string) func() {
	value, _ := m.locks.LoadOrStore(vaultID, &vaultMutex{})
	lock := value.(*vaultMutex)
	lock.mu.Lock()
	return lock.mu.Unlock
}

func validateResolvedRevision(revision workflowrepos.ResolvedRevision) error {
	if revision.RepositoryTarget.RepoTarget == "" || strings.TrimSpace(revision.RepositoryTarget.RepoTarget) != revision.RepositoryTarget.RepoTarget || revision.RepositoryTarget.LocalPath == "" || strings.TrimSpace(revision.RepositoryTarget.LocalPath) != revision.RepositoryTarget.LocalPath || revision.RepositoryTargetConfigurationVersion < 1 || !validOID(revision.CommitOID) || !validOID(revision.TreeOID) {
		return &Error{Code: CodeInvalidRequest}
	}
	switch revision.RevisionSource {
	case workflowrepos.RevisionSourceConfiguredWorkingBranch:
		if revision.ConfiguredWorkingBranchRef == "" || strings.TrimSpace(revision.ConfiguredWorkingBranchRef) != revision.ConfiguredWorkingBranchRef || !strings.HasPrefix(revision.ConfiguredWorkingBranchRef, "refs/heads/") {
			return &Error{Code: CodeInvalidRequest}
		}
	case workflowrepos.RevisionSourceExplicitCommit:
		if revision.ConfiguredWorkingBranchRef != "" {
			return &Error{Code: CodeInvalidRequest}
		}
	default:
		return &Error{Code: CodeInvalidRequest}
	}
	return nil
}

func validOwnerClass(value string) bool {
	switch value {
	case workflowstore.SourceVaultOwnerOperationPacket,
		workflowstore.SourceVaultOwnerArtifact,
		workflowstore.SourceVaultOwnerWorkflowResult,
		workflowstore.SourceVaultOwnerAuditRecord:
		return true
	default:
		return false
	}
}

func validObjectType(value string) bool {
	return value == "commit" || value == "tree" || value == "blob"
}

func failureReason(ctx context.Context, err error, fallback string) string {
	if ctx.Err() != nil {
		return workflowstore.SourceVaultFailureOperationCancelled
	}
	var failure *gitFailure
	if errors.As(err, &failure) && failure.reason != "" {
		return failure.reason
	}
	if fallback != "" {
		return fallback
	}
	return workflowstore.SourceVaultFailurePostImportVerification
}

func typedFailure(ctx context.Context, err error, reason string) error {
	if reason == workflowstore.SourceVaultFailureOperationCancelled || ctx.Err() != nil {
		return &Error{Code: CodeOperationCancelled, FailureReason: workflowstore.SourceVaultFailureOperationCancelled}
	}
	var failure *gitFailure
	if errors.As(err, &failure) && failure.code != "" {
		return &Error{Code: failure.code, FailureReason: reason}
	}
	return &Error{Code: CodeVaultUnavailable, FailureReason: reason}
}

func managerError(ctx context.Context, err error, fallbackCode string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeOperationCancelled}
	}
	var stable *Error
	if errors.As(err, &stable) {
		return &Error{Code: stable.Code, FailureReason: stable.FailureReason}
	}
	var failure *gitFailure
	if errors.As(err, &failure) {
		code := failure.code
		if code == "" || code == CodeInternal {
			code = fallbackCode
		}
		if code == "" {
			code = CodeInternal
		}
		return &Error{Code: code, FailureReason: failure.reason}
	}
	switch {
	case errors.Is(err, workflowstore.ErrSourceVaultRetentionConflict):
		return &Error{Code: CodeRetentionConflict}
	case errors.Is(err, workflowstore.ErrSourceVaultCleanupBlocked):
		return &Error{Code: CodeCleanupBlocked}
	case errors.Is(err, workflowstore.ErrSourceVaultStateConflict):
		return &Error{Code: CodeStateConflict}
	case errors.Is(err, sql.ErrNoRows):
		return &Error{Code: CodeStateConflict}
	}
	if fallbackCode == "" {
		fallbackCode = CodeDatabaseFailure
	}
	return &Error{Code: fallbackCode}
}

func importResult(vault workflowstore.SourceVault, closure workflowstore.SourceVaultClosure) ImportResult {
	return ImportResult{
		Vault:     vault,
		Closure:   closure,
		CommitOID: closure.CommitOID,
		TreeOID:   closure.TreeOID,
		RefName:   closure.RefName,
		Ready:     closure.State == workflowstore.SourceVaultClosureStateReady,
	}
}

func canonicalTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
