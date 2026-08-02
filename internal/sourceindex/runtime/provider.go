package sourceindexruntime

import (
	"context"
	"errors"
	"fmt"

	"relay/internal/app/operations"
	"relay/internal/sourcegateway"
	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type identityAuthority interface {
	ResolveSourceIndexIdentity(context.Context, workflowstore.OperationPacketVaultRelationship) (sourceindex.GenerationIdentity, error)
}

var openGenerationReader = reader.Open

func (m *Manager) OpenSearchIndex(ctx context.Context, authority operations.SourceReadAuthority) (sourcegateway.SearchIndexHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	available := m.started && !m.stopping
	m.mu.Unlock()
	if !available {
		return nil, reader.ErrGenerationUnavailable
	}
	resolver, ok := m.authority.(identityAuthority)
	if !ok {
		return nil, reader.ErrGenerationUnavailable
	}
	identity, err := resolver.ResolveSourceIndexIdentity(ctx, authority.Relationship)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if sourcevault.ErrorCode(err) == sourcevault.CodeVaultUnavailable {
			return nil, reader.ErrGenerationUnavailable
		}
		return nil, err
	}
	row, _, err := m.store.CreateOrResolveSourceIndexGeneration(ctx, workflowstore.CreateOrResolveSourceIndexGenerationParams{Identity: identity})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, mapGenerationResolutionError(err)
	}
	if row.State != workflowstore.SourceIndexGenerationReady {
		if row.State == workflowstore.SourceIndexGenerationPending {
			m.enqueue(row.GenerationID)
		} else if row.State == workflowstore.SourceIndexGenerationFailed && transientFailures[row.FailureCode] || row.State == workflowstore.SourceIndexGenerationRetired {
			m.wakeReconciliation()
		}
		return nil, reader.ErrGenerationUnavailable
	}
	l := m.lock(row.GenerationID)
	l.mu.RLock()
	if l.retiring {
		l.mu.RUnlock()
		return nil, reader.ErrGenerationUnavailable
	}
	m.mu.Lock()
	available = m.started && !m.stopping
	m.mu.Unlock()
	if !available {
		l.mu.RUnlock()
		return nil, reader.ErrGenerationUnavailable
	}
	active, err := m.store.IsSourceIndexAuthorityActive(ctx, identity)
	if err != nil {
		l.mu.RUnlock()
		return nil, err
	}
	if !active {
		l.mu.RUnlock()
		return nil, reader.ErrGenerationUnavailable
	}
	r, err := openGenerationReader(ctx, m.store, reader.Config{IndexRoot: m.config.IndexRoot, ProtectedStorage: m.config.ProtectedStorage}, identity)
	if err != nil {
		l.mu.RUnlock()
		if errors.Is(err, reader.ErrGenerationIntegrity) {
			m.wakeReconciliation()
		}
		return nil, err
	}
	m.mu.Lock()
	stopping := !m.started || m.stopping
	if !stopping {
		h := &handle{reader: r, timeout: m.config.QueryTimeout, release: l.mu.RUnlock}
		m.mu.Unlock()
		return h, nil
	}
	m.mu.Unlock()
	_ = r.Close()
	l.mu.RUnlock()
	return nil, reader.ErrGenerationUnavailable
}

func mapGenerationResolutionError(err error) error {
	switch {
	case errors.Is(err, workflowstore.ErrInvalidSourceIndexGeneration), errors.Is(err, workflowstore.ErrSourceIndexGenerationIntegrity):
		return fmt.Errorf("%w: generation", reader.ErrGenerationIntegrity)
	case errors.Is(err, workflowstore.ErrSourceIndexGenerationNotFound):
		return reader.ErrGenerationUnavailable
	default:
		return err
	}
}

var _ sourcegateway.SearchIndexProvider = (*Manager)(nil)
