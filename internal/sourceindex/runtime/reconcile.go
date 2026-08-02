package sourceindexruntime

import (
	"context"
	"errors"
	"os"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/fsatomic"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

var transientFailures = map[string]bool{"cancelled": true, "source_unavailable": true, "indexer_start_failed": true, "publication_failed": true}

var verifyPublishedGeneration = reader.VerifyPublishedGeneration
var removeOwnedGeneration = fsatomic.RemoveOwnedGeneration
var removeAllOwnedGenerationAttempts = fsatomic.RemoveAllOwnedGenerationAttempts

func (m *Manager) reconcile(ctx context.Context, startup bool) error {
	authorities, err := m.store.ListActiveSourceIndexAuthorities(ctx)
	if err != nil {
		return err
	}
	digest, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		return err
	}
	active := make(map[string]bool, len(authorities))
	for _, a := range authorities {
		identity, err := sourceindex.NewGenerationIdentity(a.VaultID, a.CommitOID, a.TreeOID, digest)
		if err != nil {
			return err
		}
		row, _, err := m.store.CreateOrResolveSourceIndexGeneration(ctx, workflowstore.CreateOrResolveSourceIndexGenerationParams{Identity: identity})
		if err != nil {
			return err
		}
		active[row.GenerationID] = true
	}
	m.mu.Lock()
	m.active = active
	m.mu.Unlock()
	rows, err := m.store.ListSourceIndexGenerations(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		// The list above is only a wakeup snapshot. Every lifecycle decision uses
		// the exact authoritative identity query below.
		isActive, err := m.store.IsSourceIndexAuthorityActive(ctx, row.Identity)
		if err != nil {
			return err
		}
		if err := m.reconcileGeneration(ctx, row, isActive, startup); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) reconcileGeneration(ctx context.Context, row workflowstore.SourceIndexGeneration, active bool, _ bool) error {
	l := m.lock(row.GenerationID)
	for {
		l.mu.Lock()
		current, err := m.store.GetSourceIndexGeneration(ctx, row.GenerationID)
		if err != nil {
			l.mu.Unlock()
			return err
		}
		active, err = m.store.IsSourceIndexAuthorityActive(ctx, current.Identity)
		if err != nil {
			l.mu.Unlock()
			return err
		}
		m.mu.Lock()
		local, owned := m.builds[current.GenerationID]
		m.mu.Unlock()
		if current.State == workflowstore.SourceIndexGenerationBuilding && owned && !active {
			l.mu.Unlock()
			local.cancel()
			select {
			case <-local.done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if current.State == workflowstore.SourceIndexGenerationBuilding && owned && active {
			l.mu.Unlock()
			return nil
		}
		err = m.reconcileGenerationLocked(ctx, current, active)
		l.mu.Unlock()
		return err
	}
}

// reconcileGenerationLocked performs every generation mutation while the
// caller owns that generation's write lock.
func (m *Manager) reconcileGenerationLocked(ctx context.Context, row workflowstore.SourceIndexGeneration, active bool) error {
	if !active {
		if row.State == workflowstore.SourceIndexGenerationBuilding {
			m.mu.Lock()
			_, owned := m.builds[row.GenerationID]
			m.mu.Unlock()
			if owned {
				return nil
			}
			if err := m.recoverBuildingLocked(ctx, row, false); err != nil {
				return err
			}
			updated, err := m.store.GetSourceIndexGeneration(ctx, row.GenerationID)
			if err != nil {
				return err
			}
			row = updated
		}
		if row.State != workflowstore.SourceIndexGenerationRetired {
			if _, err := m.store.RetireSourceIndexGeneration(ctx, row.GenerationID); err != nil {
				return err
			}
			m.logger.Info("source_index_generation_retired", "generation_id", row.GenerationID)
		}
		return m.removeUnlocked(row.GenerationID)
	}

	switch row.State {
	case workflowstore.SourceIndexGenerationPending:
		m.enqueue(row.GenerationID)
	case workflowstore.SourceIndexGenerationBuilding:
		m.mu.Lock()
		_, owned := m.builds[row.GenerationID]
		m.mu.Unlock()
		if !owned {
			return m.recoverBuildingLocked(ctx, row, true)
		}
	case workflowstore.SourceIndexGenerationReady:
		d, err := verifyPublishedGeneration(ctx, reader.Config{IndexRoot: m.config.IndexRoot, ProtectedStorage: m.config.ProtectedStorage}, row.Identity)
		if err == nil && d.GenerationManifestSHA256 == row.GenerationManifestSHA256 && d.CoverageManifestSHA256 == row.CoverageManifestSHA256 && d.ArtifactManifestSHA256 == row.ArtifactManifestSHA256 {
			return nil
		}
		return m.rebuildLocked(ctx, row)
	case workflowstore.SourceIndexGenerationFailed:
		if transientFailures[row.FailureCode] && row.AttemptCount < 3 {
			if _, err := m.store.RetrySourceIndexGeneration(ctx, row.GenerationID); err != nil {
				return err
			}
			m.enqueue(row.GenerationID)
		}
	case workflowstore.SourceIndexGenerationRetired:
		if err := m.removeUnlocked(row.GenerationID); err != nil {
			return err
		}
		if _, err := m.store.ReactivateSourceIndexGeneration(ctx, row.GenerationID); err != nil {
			return err
		}
		m.enqueue(row.GenerationID)
	}
	return nil
}

func (m *Manager) recoverBuildingLocked(ctx context.Context, row workflowstore.SourceIndexGeneration, active bool) error {
	if row.State != workflowstore.SourceIndexGenerationBuilding {
		return errors.New("source-index recovery requires a building generation")
	}
	var err error
	active, err = m.store.IsSourceIndexAuthorityActive(ctx, row.Identity)
	if err != nil {
		return err
	}
	d, verifyErr := verifyPublishedGeneration(ctx, reader.Config{IndexRoot: m.config.IndexRoot, ProtectedStorage: m.config.ProtectedStorage}, row.Identity)
	if verifyErr == nil {
		row, err := m.store.MarkSourceIndexGenerationReady(ctx, workflowstore.MarkSourceIndexGenerationReadyParams{GenerationID: row.GenerationID, GenerationManifestSHA256: d.GenerationManifestSHA256, CoverageManifestSHA256: d.CoverageManifestSHA256, ArtifactManifestSHA256: d.ArtifactManifestSHA256})
		if err != nil {
			return err
		}
		active, err = m.store.IsSourceIndexAuthorityActive(ctx, row.Identity)
		if err != nil {
			return err
		}
		if !active {
			if _, err := m.store.RetireSourceIndexGeneration(ctx, row.GenerationID); err != nil {
				return err
			}
			return m.removeUnlocked(row.GenerationID)
		}
		return nil
	}
	if err := m.removeUnlocked(row.GenerationID); err != nil {
		return err
	}
	row, err = m.store.MarkSourceIndexGenerationFailed(ctx, workflowstore.MarkSourceIndexGenerationFailedParams{GenerationID: row.GenerationID, FailureCode: "interrupted", FailureMessage: "source-index build was interrupted"})
	if err != nil {
		return err
	}
	active, err = m.store.IsSourceIndexAuthorityActive(ctx, row.Identity)
	if err != nil {
		return err
	}
	if active {
		if _, err := m.store.RetrySourceIndexGeneration(ctx, row.GenerationID); err != nil {
			return err
		}
		m.enqueue(row.GenerationID)
		return nil
	}
	if _, err := m.store.RetireSourceIndexGeneration(ctx, row.GenerationID); err != nil {
		return err
	}
	return nil
}

func (m *Manager) rebuildLocked(ctx context.Context, row workflowstore.SourceIndexGeneration) error {
	l := m.lock(row.GenerationID)
	l.retiring = true
	defer func() { l.retiring = false }()
	if _, err := m.store.RetireSourceIndexGeneration(ctx, row.GenerationID); err != nil {
		return err
	}
	if err := m.removeUnlocked(row.GenerationID); err != nil {
		return err
	}
	if _, err := m.store.ReactivateSourceIndexGeneration(ctx, row.GenerationID); err != nil {
		return err
	}
	m.enqueue(row.GenerationID)
	return nil
}

func (m *Manager) remove(id string) error {
	l := m.lock(id)
	l.mu.Lock()
	defer l.mu.Unlock()
	return m.removeUnlocked(id)
}

// removeUnlocked may only be called by code that demonstrably owns the
// generation write lock.
func (m *Manager) removeUnlocked(id string) error {
	if err := removeOwnedGeneration(m.config.IndexRoot, id); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := removeAllOwnedGenerationAttempts(m.config.IndexRoot, id); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
