package workflowstore

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	workflowartifacts "relay/internal/artifacts/workflow"
)

func TestCommittedOperationPacketPublicationFreezesDependenciesAndMutationResult(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	batch, packetFile := sealedPacketPublication(t, store, "publication-remediation-immutable")
	packet, mutation := commitPublicationFixture(t, ctx, store, batch, "opkt-publication-remediation-immutable", packetFile)

	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyInputArtifact,
			DependencyKey: "artifact-late", Required: true, Attached: true, Retained: true,
			OwnerIdentity: sql.NullString{String: "artifact-late", Valid: true},
		})
		return err
	}); err == nil {
		t.Fatal("committed publication accepted a new dependency")
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.UpdateOperationPacketDependencyAvailability(ctx, UpdateOperationPacketDependencyAvailabilityParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument,
			DependencyKey: "artifact-" + packet.PacketID, Attached: false, Retained: false,
		})
		return err
	}); err == nil {
		t.Fatal("committed publication dependency was updated")
	}
	if _, err := store.DB().Exec(`DELETE FROM operation_packet_retention_dependencies WHERE packet_row_id = ?`, packet.ID); err == nil {
		t.Fatal("committed publication dependency was deleted")
	}
	if _, err := store.DB().Exec(`UPDATE mcp_mutation_results SET result_sha256 = ? WHERE id = ?`, strings.Repeat("f", 64), mutation.ID); err == nil {
		t.Fatal("committed publication mutation result was updated")
	}
	if _, err := store.DB().Exec(`DELETE FROM mcp_mutation_results WHERE id = ?`, mutation.ID); err == nil {
		t.Fatal("committed publication mutation result was deleted")
	}

	dependency, err := store.GetOperationPacketRetentionDependency(ctx, packet.ID, OperationPacketDependencyPacketDocument, "artifact-"+packet.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if !dependency.Required || !dependency.Attached || !dependency.Retained || !dependency.OwnerIdentity.Valid || dependency.OwnerIdentity.String != dependency.DependencyKey {
		t.Fatalf("committed dependency changed: %#v", dependency)
	}
}

func commitPublicationFixture(t *testing.T, ctx context.Context, store *Store, batch *workflowartifacts.PublicationBatch, packetID string, packetFile workflowartifacts.File) (OperationPacket, MCPMutationResult) {
	t.Helper()
	var packet OperationPacket
	var mutation MCPMutationResult
	err := store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		var artifact OperationPacketArtifact
		packet, artifact, mutation = createPublicationPacketRows(t, ctx, tx, batch.PublicationID(), packetID, packetFile)
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
		_, err := tx.CreateOperationPacketPublication(ctx, CreateOperationPacketPublicationParams{
			PublicationID: batch.PublicationID(), PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID,
			MutationResultRowID: mutation.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(),
			ExpectedBindingCount: 1, ExpectedDependencyCount: 1,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return packet, mutation
}

func TestLegacyOperationPacketDependencyRemainsMutable(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	var packet OperationPacket
	if err := store.WithTx(ctx, func(tx *Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(ctx, CreateOperationPacketArtifactParams{
			ArtifactID: "artifact-legacy-mutable", Kind: "operation_packet_document",
			RelativePath: "operation-packets/opkt-legacy-mutable/operation-packet.json",
			MediaType:    "application/vnd.relay.operation-packet+json;version=1",
			SHA256:       strings.Repeat("a", 64), SizeBytes: 2,
		})
		if err != nil {
			return err
		}
		packet, err = tx.CreateOperationPacket(ctx, CreateOperationPacketParams{
			PacketID: "opkt-legacy-mutable", PacketSHA256: artifact.SHA256,
			SchemaVersion: OperationPacketSchemaVersion, Role: "planner", OperationID: "planner.plan",
			SurfaceContractID: "planner-plan.v1", ProjectID: "project-test",
			ReadinessState: OperationPacketReadinessReady, CreatedAt: "2026-07-18T00:00:00.000000000Z",
			PacketArtifactRowID: artifact.ID,
		})
		if err != nil {
			return err
		}
		_, err = tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyInputArtifact,
			DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true,
			OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.UpdateOperationPacketDependencyAvailability(ctx, UpdateOperationPacketDependencyAvailabilityParams{
			PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyInputArtifact,
			DependencyKey: "artifact-legacy-mutable", Attached: false, Retained: false,
		})
		return err
	}); err != nil {
		t.Fatalf("legacy dependency update failed: %v", err)
	}
}

func TestOperationPacketPublicationRejectsMutationResultSurfaceMismatch(t *testing.T) {
	ctx := context.Background()
	store := openPublicationStore(t)
	batch, packetFile := sealedPacketPublication(t, store, "publication-remediation-result-mismatch")
	err := store.CommitOperationPacketPublication(ctx, batch, func(tx *Tx) error {
		packet, artifact, _ := createPublicationPacketRows(t, ctx, tx, batch.PublicationID(), "opkt-publication-remediation-result-mismatch", packetFile)
		mismatched, err := tx.CreateMCPMutationResult(ctx, CreateMCPMutationResultParams{
			SurfaceContractID: "planner-execution.v1", ToolName: "create_run", MutationID: "mutation-result-mismatch",
			SurfaceManifestSHA256: strings.Repeat("a", 64), SemanticIdentityVersion: "relay.semantic.create-run.v1",
			SemanticRequestSHA256: strings.Repeat("b", 64), ResultKind: "create_run_result",
			ResultIdentityJSON: `{"run_id":"run-mismatch"}`, ResultSHA256: strings.Repeat("c", 64),
		})
		if err != nil {
			return err
		}
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
		_, err = tx.CreateOperationPacketPublication(ctx, CreateOperationPacketPublicationParams{
			PublicationID: batch.PublicationID(), PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID,
			MutationResultRowID: mismatched.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(),
			ExpectedBindingCount: 1, ExpectedDependencyCount: 1,
		})
		return err
	})
	if err == nil {
		t.Fatal("publication accepted a mutation result from another surface")
	}
}
