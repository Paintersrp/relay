package workflowstore

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestOperationPacketArtifactLifecycleAndDependencyPersistence(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	dataSHA := strings.Repeat("a", 64)
	createdAt := "2026-07-15T16:04:05.123456789Z"
	var packet OperationPacket
	err := store.WithTx(ctx, func(tx *Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(ctx, CreateOperationPacketArtifactParams{ArtifactID: "artifact-opkt", Kind: "operation_packet_document", RelativePath: "operation-packets/opkt-test/operation-packet.json", MediaType: "application/vnd.relay.operation-packet+json;version=1", SHA256: dataSHA, SizeBytes: 2})
		if err != nil {
			return err
		}
		packet, err = tx.CreateOperationPacket(ctx, CreateOperationPacketParams{PacketID: "opkt-test", PacketSHA256: dataSHA, SchemaVersion: OperationPacketSchemaVersion, Role: "planner", OperationID: "planner.requirements", SurfaceContractID: "planner-authoring.v1", ProjectID: "project-test", ReadinessState: OperationPacketReadinessReady, CreatedAt: createdAt, PacketArtifactRowID: artifact.ID})
		if err != nil {
			return err
		}
		_, err = tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetOperationPacketByPacketID(ctx, packet.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PacketSHA256 != dataSHA || got.LifecycleState != OperationPacketLifecycleActive {
		t.Fatalf("unexpected packet: %+v", got)
	}
	dependency, err := store.GetOperationPacketRetentionDependency(ctx, got.ID, OperationPacketDependencyPacketDocument, "artifact-opkt")
	if err != nil {
		t.Fatal(err)
	}
	if !dependency.Required || !dependency.Attached || !dependency.Retained {
		t.Fatalf("unexpected dependency: %+v", dependency)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CloseOperationPacket(ctx, CloseOperationPacketParams{PacketID: packet.PacketID, ClosedAt: "2026-07-15T16:04:06.123456789Z"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	closed, err := store.GetOperationPacketByPacketID(ctx, packet.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.LifecycleState != OperationPacketLifecycleClosed || !closed.ClosedAt.Valid {
		t.Fatalf("unexpected closed packet: %+v", closed)
	}
}
