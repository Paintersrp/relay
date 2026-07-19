package workflowstore

import (
	"bytes"
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
	"testing"

	workflowartifacts "relay/internal/artifacts/workflow"
)

func TestSourcePathSelectorRoundTripsLongArbitraryBytesAndConverges(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	packet, relationship := seedSelectorAuthority(t, ctx, store)
	path := append([]byte(strings.Repeat("a", 9000)), 0xff, 0xfe)
	params := CreateOrGetSourcePathSelectorParams{SelectorID: "spath-" + strings.Repeat("1", 64), PacketRowID: packet.ID, PacketID: packet.PacketID, SurfaceContractID: packet.SurfaceContractID, OperationID: packet.OperationID, ProjectID: packet.ProjectID, RepositoryKey: "relay", PublicationID: relationship.PublicationID, VaultRelationshipRowID: relationship.ID, CommitOID: relationship.CommitOID, TreeOID: relationship.TreeOID, PathID: strings.Repeat("2", 64), PathBytes: path}
	const workers = 8
	results := make(chan SourcePathSelector, workers)
	failures := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			value, err := store.CreateOrGetSourcePathSelector(ctx, params)
			if err != nil {
				failures <- err
				return
			}
			results <- value
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var first SourcePathSelector
	count := 0
	for value := range results {
		if count == 0 {
			first = value
		}
		if value.ID != first.ID || !bytes.Equal(value.PathBytes, path) {
			t.Fatalf("selector = %#v first=%#v", value, first)
		}
		count++
	}
	if count != workers {
		t.Fatalf("result count = %d", count)
	}
	loaded, err := store.GetSourcePathSelector(ctx, params.SelectorID)
	if err != nil || !bytes.Equal(loaded.PathBytes, path) || loaded.PathByteLength != int64(len(path)) {
		t.Fatalf("loaded = %#v err=%v", loaded, err)
	}
	conflict := params
	conflict.PathBytes = append([]byte(nil), path...)
	conflict.PathBytes[len(conflict.PathBytes)-1] ^= 1
	if _, err := store.CreateOrGetSourcePathSelector(ctx, conflict); err == nil {
		t.Fatal("conflicting selector bytes were accepted")
	}
}

func seedSelectorAuthority(t *testing.T, ctx context.Context, store *Store) (OperationPacket, OperationPacketVaultRelationship) {
	t.Helper()
	publicationID := "publication-selector"
	batch, err := store.ArtifactStore().BeginPublication(publicationID)
	if err != nil {
		t.Fatal(err)
	}
	packetFile, err := batch.Stage("operation_packet_document", "operation-packet.json", "application/vnd.relay.operation-packet+json;version=1", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Seal(workflowartifacts.PublicationExpectations{BindingCount: 1, DependencyCount: 2, VaultRelationshipCount: 1}); err != nil {
		t.Fatal(err)
	}
	var packet OperationPacket
	var relationship OperationPacketVaultRelationship
	err = store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		created, artifact, mutation := createPublicationPacketRows(t, ctx, tx, publicationID, "opkt-selector", packetFile)
		packet = created
		if _, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true}}); err != nil {
			return err
		}
		if _, err := tx.CreateOperationPacketArtifactBinding(ctx, CreateOperationPacketArtifactBindingParams{PublicationID: publicationID, PacketRowID: packet.ID, Sequence: 0, DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, PacketArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true}}); err != nil {
			return err
		}
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", t.TempDir()); err != nil {
			return err
		}
		vault, err := tx.GetOrCreateSourceVault(ctx, CreateSourceVaultParams{VaultID: "vault-selector", RepoTarget: "relay", RelativePath: "repositories/vault-selector.git"})
		if err != nil {
			return err
		}
		closureID := "closure-selector"
		acquisition, err := tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{VaultRowID: vault.ID, ClosureID: closureID, CommitOID: strings.Repeat("3", 40), TreeOID: strings.Repeat("4", 40), RefName: "refs/relay/closures/" + closureID, StartedAt: "2026-07-19T00:00:00.000000000Z"})
		if err != nil {
			return err
		}
		closure, err := tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{ClosureID: closureID, ExpectedState: SourceVaultClosureStateImporting, NextState: SourceVaultClosureStateReady, TransitionAt: "2026-07-19T00:00:01.000000000Z"})
		if err != nil {
			return err
		}
		if acquisition.Closure.ID != closure.ID {
			return sql.ErrNoRows
		}
		dependencyKey := "repository:relay:primary"
		owner, err := SourceVaultRetentionOwnerIdentity(packet.PacketID, OperationPacketDependencyRepositoryVault, dependencyKey)
		if err != nil {
			return err
		}
		retention, err := tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{RetentionID: "retention-selector-" + strconv.FormatInt(closure.ID, 10), ClosureRowID: closure.ID, OwnerClass: SourceVaultOwnerOperationPacket, OwnerIdentity: owner})
		if err != nil {
			return err
		}
		if _, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyRepositoryVault, DependencyKey: dependencyKey, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: owner, Valid: true}}); err != nil {
			return err
		}
		relationship, err = tx.CreateOperationPacketVaultRelationship(ctx, CreateOperationPacketVaultRelationshipParams{PublicationID: publicationID, PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyRepositoryVault, DependencyKey: dependencyKey, OwnerIdentity: owner, RetentionRowID: retention.ID, ClosureRowID: closure.ID, VaultRowID: vault.ID, CommitOID: closure.CommitOID, TreeOID: closure.TreeOID})
		if err != nil {
			return err
		}
		_, err = tx.CreateOperationPacketPublication(ctx, CreateOperationPacketPublicationParams{PublicationID: publicationID, PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID, MutationResultRowID: mutation.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(), ExpectedBindingCount: 1, ExpectedDependencyCount: 2, ExpectedVaultRelationshipCount: 1})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return packet, relationship
}
