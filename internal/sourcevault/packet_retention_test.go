package sourcevault

import (
	"context"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestRetainPreparedInTxSupportsMultiplePacketEdgesAndConflicts(t *testing.T) {
	ctx := context.Background()
	store := openPacketRetentionStore(t)
	vault, first, second := createReadyPacketRetentionClosures(t, ctx, store)
	manager := &Manager{store: store}

	firstOwner, err := workflowstore.SourceVaultRetentionOwnerIdentity("opkt-multiple", workflowstore.OperationPacketDependencyRepositoryVault, "repository:relay")
	if err != nil {
		t.Fatal(err)
	}
	secondOwner, err := workflowstore.SourceVaultRetentionOwnerIdentity("opkt-multiple", workflowstore.OperationPacketDependencyGitPathObject, "anchor:base")
	if err != nil {
		t.Fatal(err)
	}
	var firstRetention, secondRetention workflowstore.SourceVaultRetention
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		firstRetention, err = manager.RetainPreparedInTx(ctx, tx, PreparedRetention{
			PacketID: "opkt-multiple", DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault,
			DependencyKey: "repository:relay", OwnerIdentity: firstOwner, Vault: vault, Closure: first,
		})
		if err != nil {
			return err
		}
		secondRetention, err = manager.RetainPreparedInTx(ctx, tx, PreparedRetention{
			PacketID: "opkt-multiple", DependencyClass: workflowstore.OperationPacketDependencyGitPathObject,
			DependencyKey: "anchor:base", OwnerIdentity: secondOwner, Vault: vault, Closure: second,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if firstRetention.OwnerIdentity == secondRetention.OwnerIdentity || firstRetention.ClosureRowID == secondRetention.ClosureRowID {
		t.Fatalf("retentions = %#v / %#v", firstRetention, secondRetention)
	}

	var retry workflowstore.SourceVaultRetention
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		retry, err = manager.RetainPreparedInTx(ctx, tx, PreparedRetention{
			PacketID: "opkt-multiple", DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault,
			DependencyKey: "repository:relay", OwnerIdentity: firstOwner, Vault: vault, Closure: first,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if retry.ID != firstRetention.ID {
		t.Fatalf("retry retention = %#v, want %#v", retry, firstRetention)
	}

	err = store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := manager.RetainPreparedInTx(ctx, tx, PreparedRetention{
			PacketID: "opkt-multiple", DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault,
			DependencyKey: "repository:relay", OwnerIdentity: firstOwner, Vault: vault, Closure: second,
		})
		return err
	})
	if ErrorCode(err) != CodeRetentionConflict {
		t.Fatalf("contradictory edge error = %v", err)
	}

	if _, err := manager.ReleaseRetention(ctx, firstRetention.RetentionID); err != nil {
		t.Fatal(err)
	}
	active, err := store.GetSourceVaultRetentionByRetentionID(ctx, secondRetention.RetentionID)
	if err != nil || active.State != workflowstore.SourceVaultRetentionStateActive {
		t.Fatalf("sibling retention = %#v, %v", active, err)
	}
}

func TestRetainPreparedInTxRollsBackWithCallerTransaction(t *testing.T) {
	ctx := context.Background()
	store := openPacketRetentionStore(t)
	vault, first, _ := createReadyPacketRetentionClosures(t, ctx, store)
	manager := &Manager{store: store}
	owner, err := workflowstore.SourceVaultRetentionOwnerIdentity("opkt-rollback", workflowstore.OperationPacketDependencyRepositoryVault, "repository:relay")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := &Error{Code: CodeStateConflict}
	err = store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := manager.RetainPreparedInTx(ctx, tx, PreparedRetention{
			PacketID: "opkt-rollback", DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault,
			DependencyKey: "repository:relay", OwnerIdentity: owner, Vault: vault, Closure: first,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("rollback error = %v", err)
	}
	if _, err := store.GetActiveSourceVaultRetentionByOwner(ctx, workflowstore.SourceVaultOwnerOperationPacket, owner); err == nil {
		t.Fatal("retention survived caller rollback")
	}
}

func TestRetainPreparedInvestigationInTxRollsBackWithCallerTransaction(t *testing.T) {
	ctx := context.Background()
	store := openPacketRetentionStore(t)
	vault, first, _ := createReadyPacketRetentionClosures(t, ctx, store)
	manager := &Manager{store: store}
	sentinel := &Error{Code: CodeStateConflict}
	err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := manager.RetainPreparedInvestigationInTx(ctx, tx, PreparedInvestigationRetention{
			OwnerIdentity: "investigation-rollback", Vault: vault, Closure: first,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("rollback error = %v", err)
	}
	if _, err := store.GetActiveSourceVaultRetentionByOwner(ctx, workflowstore.SourceVaultOwnerArtifact, "investigation-rollback"); err == nil {
		t.Fatal("investigation retention survived caller rollback")
	}
}

func openPacketRetentionStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	directory := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(directory, "workflow.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createReadyPacketRetentionClosures(t *testing.T, ctx context.Context, store *workflowstore.Store) (workflowstore.SourceVault, workflowstore.SourceVaultClosure, workflowstore.SourceVaultClosure) {
	t.Helper()
	var vault workflowstore.SourceVault
	var first, second workflowstore.SourceVaultClosure
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", t.TempDir()); err != nil {
			return err
		}
		var err error
		vault, err = tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{
			VaultID: "vault-packet-retention", RepoTarget: "relay", RelativePath: "repositories/vault-packet-retention.git",
		})
		if err != nil {
			return err
		}
		firstAcquisition, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: "closure-packet-first", CommitOID: "1111111111111111111111111111111111111111",
			TreeOID: "2222222222222222222222222222222222222222", RefName: "refs/relay/closures/closure-packet-first",
			StartedAt: "2026-07-18T00:00:00.000000000Z",
		})
		if err != nil {
			return err
		}
		first, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: firstAcquisition.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting,
			NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: "2026-07-18T00:00:01.000000000Z",
		})
		if err != nil {
			return err
		}
		secondAcquisition, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: "closure-packet-second", CommitOID: "3333333333333333333333333333333333333333",
			TreeOID: "4444444444444444444444444444444444444444", RefName: "refs/relay/closures/closure-packet-second",
			StartedAt: "2026-07-18T00:00:02.000000000Z",
		})
		if err != nil {
			return err
		}
		second, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: secondAcquisition.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting,
			NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: "2026-07-18T00:00:03.000000000Z",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return vault, first, second
}

func TestRetainPreparedInTxSupportsMultipleAnchorEdgesForOnePacket(t *testing.T) {
	ctx := context.Background()
	store := openPacketRetentionStore(t)
	vault, first, second := createReadyPacketRetentionClosures(t, ctx, store)
	manager := &Manager{store: store}
	firstOwner, err := workflowstore.SourceVaultRetentionOwnerIdentity("opkt-anchor-pair", workflowstore.OperationPacketDependencyGitPathObject, "anchor:base")
	if err != nil {
		t.Fatal(err)
	}
	secondOwner, err := workflowstore.SourceVaultRetentionOwnerIdentity("opkt-anchor-pair", workflowstore.OperationPacketDependencyGitPathObject, "anchor:head")
	if err != nil {
		t.Fatal(err)
	}
	var firstRetention, secondRetention workflowstore.SourceVaultRetention
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		firstRetention, err = manager.RetainPreparedInTx(ctx, tx, PreparedRetention{
			PacketID: "opkt-anchor-pair", DependencyClass: workflowstore.OperationPacketDependencyGitPathObject,
			DependencyKey: "anchor:base", OwnerIdentity: firstOwner, Vault: vault, Closure: first,
		})
		if err != nil {
			return err
		}
		secondRetention, err = manager.RetainPreparedInTx(ctx, tx, PreparedRetention{
			PacketID: "opkt-anchor-pair", DependencyClass: workflowstore.OperationPacketDependencyGitPathObject,
			DependencyKey: "anchor:head", OwnerIdentity: secondOwner, Vault: vault, Closure: second,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if firstRetention.OwnerIdentity == secondRetention.OwnerIdentity || firstRetention.ClosureRowID == secondRetention.ClosureRowID {
		t.Fatalf("anchor retentions = %#v / %#v", firstRetention, secondRetention)
	}
}
