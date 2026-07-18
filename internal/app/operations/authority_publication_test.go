package operations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func TestAuthorityPublicationReconcileRemovesResidueAndPreservesLegacyRetention(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(directory, "workflow.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	vaults, err := sourcevault.Open(ctx, filepath.Join(directory, "source-vaults"), store)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAuthorityPublicationService(store, vaults)
	if err != nil {
		t.Fatal(err)
	}

	var legacy workflowstore.SourceVaultRetention
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", filepath.Join(directory, "registered-source")); err != nil {
			return err
		}
		vault, err := tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{
			VaultID: "vault-legacy-packet-owner", RepoTarget: "relay", RelativePath: "repositories/vault-legacy-packet-owner.git",
		})
		if err != nil {
			return err
		}
		acquisition, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: "closure-legacy-packet-owner",
			CommitOID: "1111111111111111111111111111111111111111", TreeOID: "2222222222222222222222222222222222222222",
			RefName: "refs/relay/closures/closure-legacy-packet-owner", StartedAt: "2026-07-18T00:00:00.000000000Z",
		})
		if err != nil {
			return err
		}
		closure, err := tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: acquisition.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting,
			NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: "2026-07-18T00:00:01.000000000Z",
		})
		if err != nil {
			return err
		}
		legacy, err = tx.CreateOrGetSourceVaultRetention(ctx, workflowstore.CreateSourceVaultRetentionParams{
			RetentionID: "retention-legacy-packet-owner", ClosureRowID: closure.ID,
			OwnerClass: workflowstore.SourceVaultOwnerOperationPacket, OwnerIdentity: "opkt-legacy-owner",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	staging, err := store.ArtifactStore().BeginPublication("publication-staging-residue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staging.Stage("operation_packet_document", "operation-packet.json", "application/json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	orphan, err := store.ArtifactStore().BeginPublication("publication-final-residue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orphan.Stage("operation_packet_document", "operation-packet.json", "application/json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := orphan.Seal(workflowPublicationExpectations()); err != nil {
		t.Fatal(err)
	}
	if err := orphan.Promote(); err != nil {
		t.Fatal(err)
	}

	if err := service.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), ".publication-staging", "publication-staging-residue")); !os.IsNotExist(err) {
		t.Fatalf("staging residue survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), "operation-packet-publications", "publication-final-residue")); !os.IsNotExist(err) {
		t.Fatalf("final residue survived: %v", err)
	}
	current, err := store.GetSourceVaultRetentionByRetentionID(ctx, legacy.RetentionID)
	if err != nil || current.State != workflowstore.SourceVaultRetentionStateActive || current.OwnerIdentity != legacy.OwnerIdentity {
		t.Fatalf("legacy retention = %#v, %v", current, err)
	}
	if err := service.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSourceVaultRetentionByRetentionID(ctx, legacy.RetentionID)
	if err != nil || current.State != workflowstore.SourceVaultRetentionStateActive {
		t.Fatalf("legacy retention after repeat = %#v, %v", current, err)
	}
	_ = staging.Rollback()
	_ = orphan.Rollback()
}

func workflowPublicationExpectations() workflowartifacts.PublicationExpectations {
	return workflowartifacts.PublicationExpectations{BindingCount: 1, DependencyCount: 1}
}
