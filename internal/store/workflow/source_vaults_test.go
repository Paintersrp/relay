package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	relaydb "relay/internal/db"

	"github.com/pressly/goose/v3"
)

const sourceVaultTestTime = "2026-07-17T12:00:00.000000000Z"

func TestSourceVaultMigrationPreservesExistingWorkflowRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(relaydb.WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "workflow_migrations", 7); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version)
VALUES ('relay', '/tmp/relay', 'refs/heads/main', 3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO operation_packet_artifacts (
    artifact_id, kind, relative_path, media_type, sha256, size_bytes
) VALUES (?, 'operation_packet_document', ?, 'application/vnd.relay.operation-packet+json;version=1', ?, 2)`,
		"artifact-existing-packet",
		"operation-packets/existing/operation-packet.json",
		strings.Repeat("a", 64),
	); err != nil {
		t.Fatal(err)
	}
	var artifactRowID int64
	if err := db.QueryRow(`SELECT id FROM operation_packet_artifacts WHERE artifact_id = 'artifact-existing-packet'`).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO operation_packets (
    packet_id, packet_sha256, role, operation_id, surface_contract_id,
    project_id, created_at, packet_artifact_row_id
) VALUES (?, ?, 'planner', 'requirements_convergence', 'planner-v1', 'project-existing', ?, ?)`,
		"opkt-existing",
		strings.Repeat("a", 64),
		sourceVaultTestTime,
		artifactRowID,
	); err != nil {
		t.Fatal(err)
	}
	var packetRowID int64
	if err := db.QueryRow(`SELECT id FROM operation_packets WHERE packet_id = 'opkt-existing'`).Scan(&packetRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO operation_packet_retention_dependencies (
    packet_row_id, dependency_class, dependency_key, required, attached, retained, owner_identity
) VALUES (?, 'packet_document', ?, 1, 1, 1, ?)`,
		packetRowID,
		"artifact-existing-packet",
		"artifact-existing-packet",
	); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpTo(db, "workflow_migrations", 8); err != nil {
		t.Fatal(err)
	}
	for table := range map[string]struct{}{
		"source_vaults": {}, "source_vault_closures": {}, "source_vault_retentions": {},
	} {
		var count int64
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("new table %s count = %d, want 0", table, count)
		}
	}
	var repoPath, packetState, dependencyClass string
	if err := db.QueryRow(`SELECT local_path FROM repository_targets WHERE repo_target = 'relay'`).Scan(&repoPath); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT lifecycle_state FROM operation_packets WHERE packet_id = 'opkt-existing'`).Scan(&packetState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT dependency_class FROM operation_packet_retention_dependencies WHERE packet_row_id = ?`, packetRowID).Scan(&dependencyClass); err != nil {
		t.Fatal(err)
	}
	if repoPath != "/tmp/relay" || packetState != OperationPacketLifecycleActive || dependencyClass != OperationPacketDependencyPacketDocument {
		t.Fatalf("existing rows changed: path=%q packet=%q dependency=%q", repoPath, packetState, dependencyClass)
	}
}

func TestSourceVaultGenerationRetentionAndReleaseLifecycle(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	vault, first := seedReadySourceVaultClosure(t, ctx, store)

	owners := []string{
		SourceVaultOwnerOperationPacket,
		SourceVaultOwnerArtifact,
		SourceVaultOwnerWorkflowResult,
		SourceVaultOwnerAuditRecord,
	}
	retentions := make([]SourceVaultRetention, 0, len(owners))
	for _, ownerClass := range owners {
		var retention SourceVaultRetention
		if err := store.WithTx(ctx, func(tx *Tx) error {
			var err error
			retention, err = tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
				RetentionID:   NewSourceVaultRetentionID(),
				ClosureRowID:  first.ID,
				OwnerClass:    ownerClass,
				OwnerIdentity: ownerClass + "-owner",
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		retentions = append(retentions, retention)
	}
	if got, err := store.CountActiveSourceVaultRetentions(ctx, first.ID); err != nil || got != 4 {
		t.Fatalf("active retention count = %d, err=%v", got, err)
	}
	for index, retention := range retentions {
		if index == len(retentions)-1 {
			break
		}
		if err := store.WithTx(ctx, func(tx *Tx) error {
			_, err := tx.ReleaseSourceVaultRetention(ctx, ReleaseSourceVaultRetentionParams{RetentionID: retention.RetentionID, ReleasedAt: sourceVaultTestTime})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.BeginSourceVaultClosureRelease(ctx, first.ClosureID, sourceVaultTestTime)
		return err
	}); err == nil {
		t.Fatal("cleanup began while one retention remained active")
	}
	last := retentions[len(retentions)-1]
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.ReleaseSourceVaultRetention(ctx, ReleaseSourceVaultRetentionParams{RetentionID: last.RetentionID, ReleasedAt: sourceVaultTestTime})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		releasing, err := tx.BeginSourceVaultClosureRelease(ctx, first.ClosureID, sourceVaultTestTime)
		if err != nil {
			return err
		}
		_, err = tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
			ClosureID:     releasing.ClosureID,
			ExpectedState: SourceVaultClosureStateReleasing,
			NextState:     SourceVaultClosureStateReleased,
			TransitionAt:  sourceVaultTestTime,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var second SourceVaultClosureAcquisition
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		second, err = tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID,
			ClosureID:  NewSourceVaultClosureID(),
			CommitOID:  first.CommitOID,
			TreeOID:    first.TreeOID,
			RefName:    "refs/relay/closures/" + NewSourceVaultClosureID(),
			StartedAt:  sourceVaultTestTime,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if second.Closure.Generation != 2 || second.Closure.ClosureID == first.ClosureID || second.Disposition != SourceVaultClosureAcquisitionCreated {
		t.Fatalf("second acquisition = %#v", second)
	}
	generations, err := store.ListSourceVaultClosuresByIdentity(ctx, vault.ID, first.CommitOID, first.TreeOID)
	if err != nil {
		t.Fatal(err)
	}
	if len(generations) != 2 || generations[0].State != SourceVaultClosureStateReleased || generations[1].State != SourceVaultClosureStateImporting {
		t.Fatalf("generation history = %#v", generations)
	}
}

func TestSourceVaultConstraintsAndCompleteTransitionMatrix(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	_, closure := seedReadySourceVaultClosure(t, ctx, store)

	if _, err := store.DB().Exec(`UPDATE source_vault_closures SET state = 'released', released_at = ? WHERE closure_id = ?`, sourceVaultTestTime, closure.ClosureID); err == nil {
		t.Fatal("ready closure transitioned directly to released")
	}
	if _, err := store.DB().Exec(`UPDATE source_vault_closures SET failure_reason = 'interrupted_import' WHERE closure_id = ?`, closure.ClosureID); err == nil {
		t.Fatal("ready closure accepted failure reason")
	}
	if _, err := store.DB().Exec(`
INSERT INTO source_vault_retentions (retention_id, closure_row_id, owner_class, owner_identity)
VALUES ('retention-invalid', ?, 'unknown', 'owner')`, closure.ID); err == nil {
		t.Fatal("unsupported retention owner class was accepted")
	}
	if _, err := store.DB().Exec(`UPDATE source_vault_closures SET closure_id = 'closure-mutated' WHERE id = ?`, closure.ID); err == nil {
		t.Fatal("closure identity mutation was accepted")
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		unavailable, err := tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
			ClosureID:     closure.ClosureID,
			ExpectedState: SourceVaultClosureStateReady,
			NextState:     SourceVaultClosureStateUnavailable,
			FailureReason: sql.NullString{String: SourceVaultFailureRefMissing, Valid: true},
			TransitionAt:  sourceVaultTestTime,
		})
		if err != nil {
			return err
		}
		if unavailable.FailureReason.String != SourceVaultFailureRefMissing {
			t.Fatalf("failure reason = %#v", unavailable.FailureReason)
		}
		acquired, err := tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{
			VaultRowID: closure.VaultRowID, ClosureID: NewSourceVaultClosureID(), CommitOID: closure.CommitOID,
			TreeOID: closure.TreeOID, RefName: "refs/relay/closures/unused", StartedAt: sourceVaultTestTime,
		})
		if err != nil {
			return err
		}
		if acquired.Disposition != SourceVaultClosureAcquisitionRetry || acquired.Closure.Generation != closure.Generation {
			t.Fatalf("unavailable retry = %#v", acquired)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSourceVaultSingleStoreAcquisitionRemainsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, CreateRepositoryTargetParams{RepoTarget: "relay", LocalPath: "/repo", ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var vault SourceVault
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		vault, err = tx.GetOrCreateSourceVault(ctx, CreateSourceVaultParams{VaultID: NewSourceVaultID(), RepoTarget: "relay", RelativePath: "repositories/relay.git"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	const workers = 8
	results := make(chan SourceVaultClosureAcquisition, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result SourceVaultClosureAcquisition
			err := store.WithTx(ctx, func(tx *Tx) error {
				var err error
				closureID := NewSourceVaultClosureID()
				result, err = tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{
					VaultRowID: vault.ID, ClosureID: closureID, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40),
					RefName: "refs/relay/closures/" + closureID, StartedAt: sourceVaultTestTime,
				})
				return err
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	closureIDs := map[string]struct{}{}
	for result := range results {
		closureIDs[result.Closure.ClosureID] = struct{}{}
	}
	if len(closureIDs) != 1 {
		t.Fatalf("concurrent closure IDs = %v", closureIDs)
	}
}

func seedReadySourceVaultClosure(t *testing.T, ctx context.Context, store *Store) (SourceVault, SourceVaultClosure) {
	t.Helper()
	var vault SourceVault
	var closure SourceVaultClosure
	if err := store.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.CreateRepositoryTargetWithConfiguration(ctx, CreateRepositoryTargetParams{
			RepoTarget: "relay", LocalPath: "/repo", ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		}); err != nil {
			return err
		}
		var err error
		vault, err = tx.GetOrCreateSourceVault(ctx, CreateSourceVaultParams{
			VaultID: NewSourceVaultID(), RepoTarget: "relay", RelativePath: "repositories/relay.git",
		})
		if err != nil {
			return err
		}
		closureID := NewSourceVaultClosureID()
		acquired, err := tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: closureID, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40),
			RefName: "refs/relay/closures/" + closureID, StartedAt: sourceVaultTestTime,
		})
		if err != nil {
			return err
		}
		closure, err = tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
			ClosureID: acquired.Closure.ClosureID, ExpectedState: SourceVaultClosureStateImporting,
			NextState: SourceVaultClosureStateReady, TransitionAt: sourceVaultTestTime,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return vault, closure
}

func TestSourceVaultConcurrentReleaseAcrossIndependentStoresIsIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "workflow.sqlite")
	firstStore, err := Open(databasePath, filepath.Join(root, "artifacts-one"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstStore.Close() })
	_, closure := seedReadySourceVaultClosure(t, ctx, firstStore)
	var retention SourceVaultRetention
	if err := firstStore.WithTx(ctx, func(tx *Tx) error {
		var createErr error
		retention, createErr = tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
			RetentionID: NewSourceVaultRetentionID(), ClosureRowID: closure.ID,
			OwnerClass: SourceVaultOwnerArtifact, OwnerIdentity: "artifact-concurrent-release",
		})
		return createErr
	}); err != nil {
		t.Fatal(err)
	}

	secondStore, err := Open(databasePath, filepath.Join(root, "artifacts-two"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })

	start := make(chan struct{})
	type releaseResult struct {
		retention SourceVaultRetention
		err       error
	}
	results := make(chan releaseResult, 2)
	for index, store := range []*Store{firstStore, secondStore} {
		index, store := index, store
		go func() {
			<-start
			var released SourceVaultRetention
			err := store.WithTx(ctx, func(tx *Tx) error {
				var releaseErr error
				released, releaseErr = tx.ReleaseSourceVaultRetention(ctx, ReleaseSourceVaultRetentionParams{
					RetentionID: retention.RetentionID,
					ReleasedAt:  fmt.Sprintf("2026-07-17T12:00:0%d.000000000Z", index),
				})
				return releaseErr
			})
			results <- releaseResult{retention: released, err: err}
		}()
	}
	close(start)
	first := <-results
	second := <-results
	for _, result := range []releaseResult{first, second} {
		if result.err != nil {
			t.Fatalf("concurrent release failed: %v", result.err)
		}
		if result.retention.RetentionID != retention.RetentionID || result.retention.State != SourceVaultRetentionStateReleased || !result.retention.ReleasedAt.Valid {
			t.Fatalf("concurrent released retention = %#v", result.retention)
		}
	}
	if first.retention.ReleasedAt.String != second.retention.ReleasedAt.String {
		t.Fatalf("concurrent retries changed immutable release time: first=%q second=%q", first.retention.ReleasedAt.String, second.retention.ReleasedAt.String)
	}
	stored, err := firstStore.GetSourceVaultRetentionByRetentionID(ctx, retention.RetentionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ReleasedAt.String != first.retention.ReleasedAt.String {
		t.Fatalf("stored release time = %q, want %q", stored.ReleasedAt.String, first.retention.ReleasedAt.String)
	}
}

func TestSourceVaultReleasedRetentionIsImmutable(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	_, closure := seedReadySourceVaultClosure(t, ctx, store)
	var retention SourceVaultRetention
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		retention, err = tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
			RetentionID: NewSourceVaultRetentionID(), ClosureRowID: closure.ID,
			OwnerClass: SourceVaultOwnerArtifact, OwnerIdentity: "artifact-owner",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.ReleaseSourceVaultRetention(ctx, ReleaseSourceVaultRetentionParams{RetentionID: retention.RetentionID, ReleasedAt: sourceVaultTestTime})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
			RetentionID: NewSourceVaultRetentionID(), ClosureRowID: closure.ID,
			OwnerClass: SourceVaultOwnerArtifact, OwnerIdentity: "artifact-owner",
		})
		return err
	}); err == nil || !strings.Contains(err.Error(), "cannot be reactivated") {
		t.Fatalf("released retention recreate error = %v", err)
	}
	if _, err := store.DB().Exec(`DELETE FROM source_vault_retentions WHERE retention_id = ?`, retention.RetentionID); err == nil {
		t.Fatal("released retention history was deleted")
	}
}

func TestSourceVaultUnknownRepositoryAndInvalidFailureReasonFail(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.GetOrCreateSourceVault(ctx, CreateSourceVaultParams{VaultID: NewSourceVaultID(), RepoTarget: "missing", RelativePath: "repositories/missing.git"})
		return err
	}); err == nil {
		t.Fatal("unknown repository acquired a source vault")
	}
	_, closure := seedReadySourceVaultClosure(t, ctx, store)
	if _, err := store.DB().Exec(`
UPDATE source_vault_closures
SET state = 'unavailable', failure_reason = 'unknown_reason'
WHERE closure_id = ?`, closure.ClosureID); err == nil {
		t.Fatal("invalid source vault failure reason was accepted")
	}
}

func TestSourceVaultFailureReasonVocabularyAndLifecycleColumns(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, CreateRepositoryTargetParams{
			RepoTarget: "relay", LocalPath: "/repo", ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var vault SourceVault
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		vault, err = tx.GetOrCreateSourceVault(ctx, CreateSourceVaultParams{VaultID: NewSourceVaultID(), RepoTarget: "relay", RelativePath: "repositories/reasons.git"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reasons := []string{
		SourceVaultFailureInterruptedImport,
		SourceVaultFailureSourceCommitMissing,
		SourceVaultFailureSourceCommitTypeMismatch,
		SourceVaultFailureSourceTreeMissing,
		SourceVaultFailureSourceTreeTypeMismatch,
		SourceVaultFailureSourceTreeMismatch,
		SourceVaultFailureSourceGitStartFailed,
		SourceVaultFailurePackGenerationFailed,
		SourceVaultFailureVaultMissing,
		SourceVaultFailureVaultInvalid,
		SourceVaultFailureVaultGitStartFailed,
		SourceVaultFailurePackIndexFailed,
		SourceVaultFailureVaultCommitMissing,
		SourceVaultFailureVaultCommitTypeMismatch,
		SourceVaultFailureVaultTreeMissing,
		SourceVaultFailureVaultTreeTypeMismatch,
		SourceVaultFailureVaultTreeMismatch,
		SourceVaultFailureRefCreateFailed,
		SourceVaultFailureRefMissing,
		SourceVaultFailureRefMismatch,
		SourceVaultFailureRefDeleteFailed,
		SourceVaultFailurePostImportVerification,
		SourceVaultFailureOperationCancelled,
		SourceVaultFailureReleaseOwnerConflict,
		SourceVaultFailureReleaseInterrupted,
	}
	for index, reason := range reasons {
		closureID := NewSourceVaultClosureID()
		commitOID := fmt.Sprintf("%040x", index+1)
		treeOID := fmt.Sprintf("%040x", index+1001)
		if err := store.WithTx(ctx, func(tx *Tx) error {
			acquired, err := tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{
				VaultRowID: vault.ID, ClosureID: closureID, CommitOID: commitOID, TreeOID: treeOID,
				RefName: "refs/relay/closures/" + closureID, StartedAt: sourceVaultTestTime,
			})
			if err != nil {
				return err
			}
			unavailable, err := tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
				ClosureID: acquired.Closure.ClosureID, ExpectedState: SourceVaultClosureStateImporting,
				NextState:     SourceVaultClosureStateUnavailable,
				FailureReason: sql.NullString{String: reason, Valid: true}, TransitionAt: sourceVaultTestTime,
			})
			if err != nil {
				return err
			}
			if unavailable.FailureReason.String != reason || unavailable.VerifiedAt.Valid || unavailable.ReleasedAt.Valid {
				t.Fatalf("unavailable closure for %q = %#v", reason, unavailable)
			}
			return nil
		}); err != nil {
			t.Fatalf("reason %q: %v", reason, err)
		}
	}
}

func TestSourceVaultRetentionIdempotencyAndActiveOwnerUniqueness(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	vault, first := seedReadySourceVaultClosure(t, ctx, store)
	var firstRetention SourceVaultRetention
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		firstRetention, err = tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
			RetentionID: NewSourceVaultRetentionID(), ClosureRowID: first.ID,
			OwnerClass: SourceVaultOwnerArtifact, OwnerIdentity: "artifact-shared",
		})
		if err != nil {
			return err
		}
		retry, err := tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
			RetentionID: NewSourceVaultRetentionID(), ClosureRowID: first.ID,
			OwnerClass: SourceVaultOwnerArtifact, OwnerIdentity: "artifact-shared",
		})
		if err != nil {
			return err
		}
		if retry.RetentionID != firstRetention.RetentionID {
			t.Fatalf("retain retry changed identity: first=%#v retry=%#v", firstRetention, retry)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var second SourceVaultClosure
	if err := store.WithTx(ctx, func(tx *Tx) error {
		closureID := NewSourceVaultClosureID()
		acquired, err := tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: closureID, CommitOID: strings.Repeat("c", 40), TreeOID: strings.Repeat("d", 40),
			RefName: "refs/relay/closures/" + closureID, StartedAt: sourceVaultTestTime,
		})
		if err != nil {
			return err
		}
		second, err = tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
			ClosureID: acquired.Closure.ClosureID, ExpectedState: SourceVaultClosureStateImporting,
			NextState: SourceVaultClosureStateReady, TransitionAt: sourceVaultTestTime,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
			RetentionID: NewSourceVaultRetentionID(), ClosureRowID: second.ID,
			OwnerClass: SourceVaultOwnerArtifact, OwnerIdentity: "artifact-shared",
		})
		return err
	}); err == nil {
		t.Fatal("one active owner identity retained two generations")
	}

	var firstRelease SourceVaultRetention
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		firstRelease, err = tx.ReleaseSourceVaultRetention(ctx, ReleaseSourceVaultRetentionParams{RetentionID: firstRetention.RetentionID, ReleasedAt: sourceVaultTestTime})
		if err != nil {
			return err
		}
		retry, err := tx.ReleaseSourceVaultRetention(ctx, ReleaseSourceVaultRetentionParams{RetentionID: firstRetention.RetentionID, ReleasedAt: sourceVaultTestTime})
		if err != nil {
			return err
		}
		if retry.RetentionID != firstRelease.RetentionID || retry.State != SourceVaultRetentionStateReleased {
			t.Fatalf("release retry = %#v", retry)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
			RetentionID: NewSourceVaultRetentionID(), ClosureRowID: second.ID,
			OwnerClass: SourceVaultOwnerArtifact, OwnerIdentity: "artifact-shared",
		})
		return err
	}); err != nil {
		t.Fatalf("released historical owner blocked a later generation: %v", err)
	}
}

func TestSourceVaultClosureCompareAndSwapHasOneWinner(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	_, closure := seedReadySourceVaultClosure(t, ctx, store)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, reason := range []string{SourceVaultFailureRefMissing, SourceVaultFailureVaultMissing} {
		wg.Add(1)
		go func(reason string) {
			defer wg.Done()
			results <- store.WithTx(ctx, func(tx *Tx) error {
				_, err := tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
					ClosureID: closure.ClosureID, ExpectedState: SourceVaultClosureStateReady,
					NextState:     SourceVaultClosureStateUnavailable,
					FailureReason: sql.NullString{String: reason, Valid: true}, TransitionAt: sourceVaultTestTime,
				})
				return err
			})
		}(reason)
	}
	wg.Wait()
	close(results)
	var successes, failures int
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("CAS results: successes=%d failures=%d", successes, failures)
	}
}

func TestSourceVaultNoRowsErrorsRemainDeterministic(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	if _, err := store.GetSourceVaultClosureByClosureID(ctx, "closure-missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing closure error = %v", err)
	}
}
