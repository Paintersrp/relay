package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestRequiredPacketDependenciesRemainAddressableAcrossTerminalLifecycle(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	sha := strings.Repeat("a", 64)
	var packet OperationPacket
	var documentArtifact OperationPacketArtifact
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		documentArtifact, err = tx.CreateOperationPacketArtifact(ctx, CreateOperationPacketArtifactParams{ArtifactID: "artifact-authority-document", Kind: "operation_packet_document", RelativePath: "operation-packets/opkt-authority/operation-packet.json", MediaType: "application/vnd.relay.operation-packet+json;version=1", SHA256: sha, SizeBytes: 2})
		if err != nil {
			return err
		}
		packet, err = tx.CreateOperationPacket(ctx, CreateOperationPacketParams{PacketID: "opkt-authority", PacketSHA256: sha, SchemaVersion: OperationPacketSchemaVersion, Role: "planner", OperationID: "planner.requirements", SurfaceContractID: "planner-authoring.v1", ProjectID: "project-authority", ReadinessState: OperationPacketReadinessReady, CreatedAt: "2026-07-15T16:04:05.123456789Z", PacketArtifactRowID: documentArtifact.ID})
		if err != nil {
			return err
		}
		for _, dependency := range []AttachOperationPacketDependencyParams{{PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: documentArtifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: documentArtifact.ArtifactID, Valid: true}}, {PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyInputArtifact, DependencyKey: "artifact-input", Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: "artifact-input", Valid: true}}} {
			if _, err := tx.AttachOperationPacketDependency(ctx, dependency); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	dependencies, err := store.ListOperationPacketRetentionDependencies(ctx, packet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 2 || dependencies[0].DependencyClass != OperationPacketDependencyInputArtifact || dependencies[1].DependencyClass != OperationPacketDependencyPacketDocument {
		t.Fatalf("dependency order = %+v", dependencies)
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CloseOperationPacket(ctx, CloseOperationPacketParams{PacketID: packet.PacketID, ClosedAt: "2026-07-15T16:04:06.123456789Z"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	retained, err := store.ListOperationPacketRetentionDependencies(ctx, packet.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range retained {
		if !dependency.Required || !dependency.Attached || !dependency.Retained || !dependency.OwnerIdentity.Valid {
			t.Fatalf("terminal dependency changed: %+v", dependency)
		}
	}
}

func TestConcurrentCloseCompareAndSwapHasOneWinner(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	sha := strings.Repeat("b", 64)
	var packet OperationPacket
	if err := store.WithTx(ctx, func(tx *Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(ctx, CreateOperationPacketArtifactParams{ArtifactID: "artifact-close-race", Kind: "operation_packet_document", RelativePath: "operation-packets/opkt-close-race/operation-packet.json", MediaType: "application/vnd.relay.operation-packet+json;version=1", SHA256: sha, SizeBytes: 2})
		if err != nil {
			return err
		}
		packet, err = tx.CreateOperationPacket(ctx, CreateOperationPacketParams{PacketID: "opkt-close-race", PacketSHA256: sha, SchemaVersion: OperationPacketSchemaVersion, Role: "planner", OperationID: "planner.requirements", SurfaceContractID: "planner-authoring.v1", ProjectID: "project-race", ReadinessState: OperationPacketReadinessReady, CreatedAt: "2026-07-15T16:04:05.123456789Z", PacketArtifactRowID: artifact.ID})
		if err != nil {
			return err
		}
		_, err = tx.AttachOperationPacketDependency(ctx, AttachOperationPacketDependencyParams{PacketRowID: packet.ID, DependencyClass: OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results <- store.WithTx(ctx, func(tx *Tx) error {
				_, err := tx.CloseOperationPacket(ctx, CloseOperationPacketParams{PacketID: packet.PacketID, ClosedAt: "2026-07-15T16:04:0" + string(rune('6'+index)) + ".123456789Z"})
				return err
			})
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	misses := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, sql.ErrNoRows):
			misses++
		default:
			t.Fatalf("unexpected close result: %v", err)
		}
	}
	if successes != 1 || misses != 1 {
		t.Fatalf("close race successes=%d misses=%d", successes, misses)
	}
}
