package sourceindexruntime

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/fsatomic"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

var transientFailures = map[string]bool{"cancelled": true, "source_unavailable": true, "indexer_start_failed": true, "publication_failed": true}

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
		identity, e := sourceindex.NewGenerationIdentity(a.VaultID, a.CommitOID, a.TreeOID, digest)
		if e != nil {
			return e
		}
		row, _, e := m.store.CreateOrResolveSourceIndexGeneration(ctx, workflowstore.CreateOrResolveSourceIndexGenerationParams{Identity: identity})
		if e != nil {
			return e
		}
		active[row.GenerationID] = true
	}
	m.mu.Lock()
	m.active = active
	repairs := m.repair
	m.repair = map[string]bool{}
	m.mu.Unlock()
	rows, err := m.store.ListSourceIndexGenerations(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if repairs[row.GenerationID] {
			active[row.GenerationID] = true
		}
		if err := m.reconcileGeneration(ctx, row, startup); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) reconcileGeneration(ctx context.Context, row workflowstore.SourceIndexGeneration, startup bool) error {
	m.mu.Lock()
	active := m.active[row.GenerationID]
	m.mu.Unlock()
	if !active {
		if row.State == workflowstore.SourceIndexGenerationBuilding {
			return nil
		}
		if row.State != workflowstore.SourceIndexGenerationRetired {
			if _, err := m.store.RetireSourceIndexGeneration(ctx, row.GenerationID); err != nil {
				return err
			}
			slog.Info("source_index_generation_retired", "generation_id", row.GenerationID)
		}
		return m.remove(row.GenerationID)
	}
	switch row.State {
	case workflowstore.SourceIndexGenerationPending:
		m.enqueue(row.GenerationID)
	case workflowstore.SourceIndexGenerationBuilding:
		if startup {
			return m.recoverBuilding(ctx, row)
		}
	case workflowstore.SourceIndexGenerationReady:
		d, err := reader.VerifyPublishedGeneration(ctx, reader.Config{IndexRoot: m.config.IndexRoot, ProtectedStorage: m.config.ProtectedStorage}, row.Identity)
		if err == nil && d.GenerationManifestSHA256 == row.GenerationManifestSHA256 && d.CoverageManifestSHA256 == row.CoverageManifestSHA256 && d.ArtifactManifestSHA256 == row.ArtifactManifestSHA256 {
			return nil
		}
		return m.rebuild(ctx, row)
	case workflowstore.SourceIndexGenerationFailed:
		if transientFailures[row.FailureCode] && row.AttemptCount < 3 {
			if _, err := m.store.RetrySourceIndexGeneration(ctx, row.GenerationID); err != nil {
				return err
			}
			m.enqueue(row.GenerationID)
		}
	case workflowstore.SourceIndexGenerationRetired:
		if err := m.remove(row.GenerationID); err != nil {
			return err
		}
		if _, err := m.store.ReactivateSourceIndexGeneration(ctx, row.GenerationID); err != nil {
			return err
		}
		m.enqueue(row.GenerationID)
	}
	return nil
}

func (m *Manager) recoverBuilding(ctx context.Context, row workflowstore.SourceIndexGeneration) error {
	d, err := reader.VerifyPublishedGeneration(ctx, reader.Config{IndexRoot: m.config.IndexRoot, ProtectedStorage: m.config.ProtectedStorage}, row.Identity)
	if err == nil {
		_, err = m.store.MarkSourceIndexGenerationReady(ctx, workflowstore.MarkSourceIndexGenerationReadyParams{GenerationID: row.GenerationID, GenerationManifestSHA256: d.GenerationManifestSHA256, CoverageManifestSHA256: d.CoverageManifestSHA256, ArtifactManifestSHA256: d.ArtifactManifestSHA256})
		return err
	}
	_ = m.remove(row.GenerationID)
	if _, err = m.store.MarkSourceIndexGenerationFailed(ctx, workflowstore.MarkSourceIndexGenerationFailedParams{GenerationID: row.GenerationID, FailureCode: "interrupted", FailureMessage: "source-index build was interrupted"}); err != nil {
		return err
	}
	if _, err = m.store.RetrySourceIndexGeneration(ctx, row.GenerationID); err != nil {
		return err
	}
	m.enqueue(row.GenerationID)
	return nil
}
func (m *Manager) rebuild(ctx context.Context, row workflowstore.SourceIndexGeneration) error {
	l := m.lock(row.GenerationID)
	l.mu.Lock()
	defer l.mu.Unlock()
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
func (m *Manager) removeUnlocked(id string) error {
	if err := fsatomic.RemoveOwnedGeneration(m.config.IndexRoot, id); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
