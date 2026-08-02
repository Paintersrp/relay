package sourceindexruntime

import (
	"context"
	"errors"

	"relay/internal/app/operations"
	"relay/internal/sourcegateway"
	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

type identityAuthority interface {
	ResolveSourceIndexIdentity(context.Context, workflowstore.OperationPacketVaultRelationship) (sourceindex.GenerationIdentity, error)
}

func (m *Manager) OpenSearchIndex(ctx context.Context, authority operations.SourceReadAuthority) (sourcegateway.SearchIndexHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
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
		return nil, reader.ErrGenerationUnavailable
	}
	row, _, err := m.store.CreateOrResolveSourceIndexGeneration(ctx, workflowstore.CreateOrResolveSourceIndexGenerationParams{Identity: identity})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, reader.ErrGenerationUnavailable
	}
	if row.State != workflowstore.SourceIndexGenerationReady {
		if row.State == workflowstore.SourceIndexGenerationPending {
			m.enqueue(row.GenerationID)
		} else if row.State == workflowstore.SourceIndexGenerationFailed && transientFailures[row.FailureCode] || row.State == workflowstore.SourceIndexGenerationRetired {
			m.mu.Lock()
			m.repair[row.GenerationID] = true
			m.mu.Unlock()
		}
		return nil, reader.ErrGenerationUnavailable
	}
	l := m.lock(row.GenerationID)
	l.mu.RLock()
	if l.retiring {
		l.mu.RUnlock()
		return nil, reader.ErrGenerationUnavailable
	}
	r, err := reader.Open(ctx, m.store, reader.Config{IndexRoot: m.config.IndexRoot, ProtectedStorage: m.config.ProtectedStorage}, identity)
	if err != nil {
		l.mu.RUnlock()
		if errors.Is(err, reader.ErrGenerationIntegrity) {
			m.mu.Lock()
			m.repair[row.GenerationID] = true
			m.mu.Unlock()
		}
		return nil, err
	}
	return &handle{reader: r, timeout: m.config.QueryTimeout, release: l.mu.RUnlock}, nil
}

var _ sourcegateway.SearchIndexProvider = (*Manager)(nil)
