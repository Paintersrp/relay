package workflowstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	workflowartifacts "relay/internal/artifacts/workflow"
)

func TestCommitOperationPacketPublicationEnforcesLastMarkerClosure(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	batch, packetFile := sealedPacketPublication(t, store, "publication-complete")
	var publication OperationPacketPublication
	err := store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		packet, artifact, mutation := createPublicationPacketRows(t, ctx, tx, "publication-complete", "opkt-publication-complete", packetFile)
		if _, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument,
			DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true,
			OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := tx.CreateOperationPacketArtifactBinding(ctx, CreateOperationPacketArtifactBindingParams{
			PublicationID: "publication-complete", PacketRowID: packet.ID, Sequence: 0,
			DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID,
			PacketArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true},
		}); err != nil {
			return err
		}
		var err error
		publication, err = tx.CreateOperationPacketPublication(ctx, CreateOperationPacketPublicationParams{
			PublicationID: "publication-complete", PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID,
			MutationResultRowID: mutation.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(),
			ExpectedBindingCount: 1, ExpectedDependencyCount: 1,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if publication.State != OperationPacketPublicationStateCommitted {
		t.Fatalf("publication = %#v", publication)
	}
	integrity, err := store.GetOperationPacketPublicationIntegrity(ctx, "publication-complete")
	if err != nil {
		t.Fatal(err)
	}
	if integrity.Packet.CoordinatedPublicationID.String != "publication-complete" || len(integrity.Bindings) != 1 || len(integrity.Dependencies) != 1 {
		t.Fatalf("integrity = %#v", integrity)
	}
}

func TestCommitOperationPacketPublicationRejectsMissingMarkerAtCommit(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	batch, packetFile := sealedPacketPublication(t, store, "publication-no-marker")
	err := store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		packet, artifact, _ := createPublicationPacketRows(t, ctx, tx, "publication-no-marker", "opkt-publication-no-marker", packetFile)
		if _, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument,
			DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true,
			OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true},
		}); err != nil {
			return err
		}
		_, err := tx.CreateOperationPacketArtifactBinding(ctx, CreateOperationPacketArtifactBindingParams{
			PublicationID: "publication-no-marker", PacketRowID: packet.ID, Sequence: 0,
			DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID,
			PacketArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true},
		})
		return err
	})
	if err == nil {
		t.Fatal("coordinated packet committed without publication marker")
	}
	if _, err := store.GetOperationPacketByPacketID(ctx, "opkt-publication-no-marker"); err == nil {
		t.Fatal("packet survived deferred-constraint rollback")
	}
	if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), "operation-packet-publications", "publication-no-marker")); !os.IsNotExist(err) {
		t.Fatalf("promoted directory survived failed commit: %v", err)
	}
}

func TestOperationPacketPublicationRejectsEarlyMarkerAndPostMarkerChild(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	batch, packetFile := sealedPacketPublication(t, store, "publication-order")
	err := store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		packet, artifact, mutation := createPublicationPacketRows(t, ctx, tx, "publication-order", "opkt-publication-order", packetFile)
		_, err := tx.CreateOperationPacketPublication(ctx, CreateOperationPacketPublicationParams{
			PublicationID: "publication-order", PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID,
			MutationResultRowID: mutation.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(),
			ExpectedBindingCount: 1, ExpectedDependencyCount: 1,
		})
		return err
	})
	if err == nil {
		t.Fatal("publication marker inserted before closure completion")
	}

	batch, packetFile = sealedPacketPublication(t, store, "publication-post-marker")
	err = store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		packet, artifact, mutation := createPublicationPacketRows(t, ctx, tx, "publication-post-marker", "opkt-publication-post-marker", packetFile)
		if _, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument,
			DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true,
			OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := tx.CreateOperationPacketArtifactBinding(ctx, CreateOperationPacketArtifactBindingParams{
			PublicationID: "publication-post-marker", PacketRowID: packet.ID, Sequence: 0,
			DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID,
			PacketArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := tx.CreateOperationPacketPublication(ctx, CreateOperationPacketPublicationParams{
			PublicationID: "publication-post-marker", PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID,
			MutationResultRowID: mutation.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(),
			ExpectedBindingCount: 1, ExpectedDependencyCount: 1,
		}); err != nil {
			return err
		}
		_, err := tx.CreateOperationPacketRetainedArtifact(ctx, CreateOperationPacketRetainedArtifactParams{
			PublicationID: "publication-post-marker", ArtifactID: "artifact-late", Kind: OperationPacketRetainedArtifactInlineInput,
			RelativePath: "operation-packet-publications/publication-post-marker/inputs/late.txt", MediaType: "text/plain",
			SHA256: digestString([]byte("late")), SizeBytes: 4,
		})
		return err
	})
	if err == nil {
		t.Fatal("publication child inserted after commit marker")
	}
}

func openPublicationStore(t *testing.T) *Store {
	t.Helper()
	directory := t.TempDir()
	store, err := Open(filepath.Join(directory, "workflow.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func sealedPacketPublication(t *testing.T, store *Store, publicationID string) (*workflowartifacts.PublicationBatch, workflowartifacts.File) {
	t.Helper()
	batch, err := store.ArtifactStore().BeginPublication(publicationID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("operation_packet_document", "operation-packet.json", "application/vnd.relay.operation-packet+json;version=1", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Seal(workflowartifacts.PublicationExpectations{BindingCount: 1, DependencyCount: 1}); err != nil {
		t.Fatal(err)
	}
	return batch, file
}

func createPublicationPacketRows(t *testing.T, ctx context.Context, tx *Tx, publicationID, packetID string, packetFile workflowartifacts.File) (OperationPacket, OperationPacketArtifact, MCPMutationResult) {
	t.Helper()
	artifact, err := tx.CreateOperationPacketArtifact(ctx, CreateOperationPacketArtifactParams{
		ArtifactID: "artifact-" + packetID, Kind: packetFile.Kind, RelativePath: packetFile.RelativePath,
		MediaType: packetFile.MediaType, SHA256: packetFile.SHA256, SizeBytes: packetFile.SizeBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := tx.CreateOperationPacket(ctx, CreateOperationPacketParams{
		PacketID: packetID, PacketSHA256: packetFile.SHA256, SchemaVersion: OperationPacketSchemaVersion,
		Role: "planner", OperationID: "submit_plan", SurfaceContractID: "planner-plan.v1", ProjectID: "project-test",
		ReadinessState: OperationPacketReadinessReady, CreatedAt: "2026-07-18T00:00:00.000000000Z",
		PacketArtifactRowID: artifact.ID, CoordinatedPublicationID: sql.NullString{String: publicationID, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := tx.CreateMCPMutationResult(ctx, CreateMCPMutationResultParams{
		SurfaceContractID: "planner-plan.v1", ToolName: "submit_plan", MutationID: "mutation-" + packetID,
		SurfaceManifestSHA256: digestString([]byte("manifest")), SemanticIdentityVersion: "v1",
		SemanticRequestSHA256: digestString([]byte("request")), ResultKind: "submit_plan_result",
		ResultIdentityJSON: `{"planId":"plan-test"}`, ResultSHA256: digestString([]byte(`{"planId":"plan-test"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return packet, artifact, mutation
}

func digestString(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func TestOperationPacketPublicationCommitsMultipleVaultEdges(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	batch, err := store.ArtifactStore().BeginPublication("publication-vault-edges")
	if err != nil {
		t.Fatal(err)
	}
	packetFile, err := batch.Stage("operation_packet_document", "operation-packet.json", "application/vnd.relay.operation-packet+json;version=1", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Seal(workflowartifacts.PublicationExpectations{BindingCount: 1, DependencyCount: 3, VaultRelationshipCount: 2}); err != nil {
		t.Fatal(err)
	}
	err = store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		packet, artifact, mutation := createPublicationPacketRows(t, ctx, tx, "publication-vault-edges", "opkt-publication-vault-edges", packetFile)
		if _, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument,
			DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true,
			OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := tx.CreateOperationPacketArtifactBinding(ctx, CreateOperationPacketArtifactBindingParams{
			PublicationID: batch.PublicationID(), PacketRowID: packet.ID, Sequence: 0,
			DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID,
			PacketArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := tx.CreateRepositoryTarget(ctx, "relay", t.TempDir()); err != nil {
			return err
		}
		vault, err := tx.GetOrCreateSourceVault(ctx, CreateSourceVaultParams{
			VaultID: "vault-publication-edges", RepoTarget: "relay", RelativePath: "repositories/vault-publication-edges.git",
		})
		if err != nil {
			return err
		}
		edges := []struct {
			closureID       string
			commitOID       string
			treeOID         string
			dependencyClass string
			dependencyKey   string
		}{
			{"closure-publication-repository", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", OperationPacketDependencyRepositoryVault, "repository:relay"},
			{"closure-publication-anchor", "3333333333333333333333333333333333333333", "4444444444444444444444444444444444444444", OperationPacketDependencyGitPathObject, "anchor:reviewed-base"},
		}
		for index, edge := range edges {
			acquisition, err := tx.AcquireSourceVaultClosure(ctx, AcquireSourceVaultClosureParams{
				VaultRowID: vault.ID, ClosureID: edge.closureID, CommitOID: edge.commitOID, TreeOID: edge.treeOID,
				RefName: "refs/relay/closures/" + edge.closureID, StartedAt: "2026-07-18T00:00:0" + strconv.Itoa(index+1) + ".000000000Z",
			})
			if err != nil {
				return err
			}
			closure, err := tx.TransitionSourceVaultClosure(ctx, TransitionSourceVaultClosureParams{
				ClosureID: acquisition.Closure.ClosureID, ExpectedState: SourceVaultClosureStateImporting,
				NextState: SourceVaultClosureStateReady, TransitionAt: "2026-07-18T00:00:1" + strconv.Itoa(index+1) + ".000000000Z",
			})
			if err != nil {
				return err
			}
			owner, err := SourceVaultRetentionOwnerIdentity(packet.PacketID, edge.dependencyClass, edge.dependencyKey)
			if err != nil {
				return err
			}
			retention, err := tx.CreateOrGetSourceVaultRetention(ctx, CreateSourceVaultRetentionParams{
				RetentionID: "retention-publication-edge-" + strconv.Itoa(index+1), ClosureRowID: closure.ID,
				OwnerClass: SourceVaultOwnerOperationPacket, OwnerIdentity: owner,
			})
			if err != nil {
				return err
			}
			if _, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{
				PacketRowID: packet.ID, DependencyClass: edge.dependencyClass, DependencyKey: edge.dependencyKey,
				Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: owner, Valid: true},
			}); err != nil {
				return err
			}
			if _, err := tx.CreateOperationPacketVaultRelationship(ctx, CreateOperationPacketVaultRelationshipParams{
				PublicationID: batch.PublicationID(), PacketRowID: packet.ID,
				DependencyClass: edge.dependencyClass, DependencyKey: edge.dependencyKey, OwnerIdentity: owner,
				RetentionRowID: retention.ID, ClosureRowID: closure.ID, VaultRowID: vault.ID,
				CommitOID: closure.CommitOID, TreeOID: closure.TreeOID,
			}); err != nil {
				return err
			}
		}
		_, err = tx.CreateOperationPacketPublication(ctx, CreateOperationPacketPublicationParams{
			PublicationID: batch.PublicationID(), PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID,
			MutationResultRowID: mutation.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(),
			ExpectedBindingCount: 1, ExpectedDependencyCount: 3, ExpectedVaultRelationshipCount: 2,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	integrity, err := store.GetOperationPacketPublicationIntegrity(ctx, batch.PublicationID())
	if err != nil {
		t.Fatal(err)
	}
	if len(integrity.VaultRelationships) != 2 || integrity.VaultRelationships[0].OwnerIdentity == integrity.VaultRelationships[1].OwnerIdentity {
		t.Fatalf("vault relationships = %#v", integrity.VaultRelationships)
	}
}
