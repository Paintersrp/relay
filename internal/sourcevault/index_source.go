package sourcevault

import (
	"context"
	"database/sql"
	"sync"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/supervisor"
	workflowstore "relay/internal/store/workflow"
)

// ResolveSourceIndexIdentity resolves the exact retained source-vault closure
// for an index identity. It never selects a different generation of the same
// commit or tree and requires the closure to remain actively retained.
func (m *Manager) ResolveSourceIndexIdentity(ctx context.Context, relationship workflowstore.OperationPacketVaultRelationship) (sourceindex.GenerationIdentity, error) {
	if m == nil || m.store == nil {
		return sourceindex.GenerationIdentity{}, &Error{Code: CodeVaultUnavailable}
	}
	var identity sourceindex.GenerationIdentity
	err := m.withActiveRetentionEdge(ctx, relationship, func(_ string, closure workflowstore.SourceVaultClosure) error {
		vault, err := m.store.GetSourceVaultByRowID(ctx, relationship.VaultRowID)
		if err != nil {
			return err
		}
		if vault.ID != closure.VaultRowID || closure.ID != relationship.ClosureRowID || closure.CommitOID != relationship.CommitOID || closure.TreeOID != relationship.TreeOID {
			return &Error{Code: CodeVaultUnavailable}
		}
		digest, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
		if err != nil {
			return err
		}
		identity, err = sourceindex.NewGenerationIdentity(vault.VaultID, closure.CommitOID, closure.TreeOID, digest)
		return err
	})
	if err != nil {
		return sourceindex.GenerationIdentity{}, err
	}
	return identity, nil
}

// AcquireSourceIndexLease exposes an already-retained closure to the index
// supervisor while holding the vault's maintenance lock. That lock is local to
// this Manager instance; it does not claim cross-manager or cross-process
// exclusion. It never imports or creates source authority.
func (m *Manager) AcquireSourceIndexLease(ctx context.Context, identity sourceindex.GenerationIdentity) (supervisor.SourceLease, error) {
	if m == nil {
		return nil, &Error{Code: CodeVaultUnavailable}
	}
	var vault workflowstore.SourceVault
	var closure workflowstore.SourceVaultClosure
	err := m.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		vault, err = tx.GetSourceVaultByVaultID(ctx, identity.VaultID)
		if err != nil {
			return err
		}
		closures, err := tx.ListSourceVaultClosuresByIdentity(ctx, vault.ID, identity.CommitOID, identity.TreeOID)
		if err != nil {
			return err
		}
		for _, candidate := range closures {
			if candidate.State == workflowstore.SourceVaultClosureStateReady {
				closure = candidate
				return nil
			}
		}
		return sql.ErrNoRows
	})
	if err != nil {
		return nil, managerError(ctx, err, CodeVaultUnavailable)
	}

	unlock := m.lockVault(vault.VaultID)
	path, err := m.indexLeasePath(ctx, vault, closure, identity)
	if err != nil {
		unlock()
		return nil, err
	}
	return &sourceIndexLease{path: path, close: unlock}, nil
}

func (m *Manager) indexLeasePath(ctx context.Context, vault workflowstore.SourceVault, expected workflowstore.SourceVaultClosure, identity sourceindex.GenerationIdentity) (string, error) {
	closure, err := m.store.GetSourceVaultClosureByRowID(ctx, expected.ID)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.VaultRowID != vault.ID || closure.CommitOID != identity.CommitOID || closure.TreeOID != identity.TreeOID {
		return "", &Error{Code: CodeVaultUnavailable}
	}
	active, err := m.store.CountActiveSourceVaultRetentions(ctx, closure.ID)
	if err != nil || active == 0 {
		return "", &Error{Code: CodeVaultUnavailable}
	}
	path, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return "", managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, path); err != nil {
		return "", managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.VerifyVaultClosure(ctx, path, identity.CommitOID, identity.TreeOID, closure.RefName); err != nil {
		return "", managerError(ctx, err, CodeVaultUnavailable)
	}
	return path, nil
}

type sourceIndexLease struct {
	path  string
	close func()
	once  sync.Once
}

func (l *sourceIndexLease) RepositoryPath() string { return l.path }
func (l *sourceIndexLease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(l.close)
	return nil
}

var _ supervisor.SourceAuthority = (*Manager)(nil)
