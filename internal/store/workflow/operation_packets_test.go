package workflowstore

import (
	"context"
	"database/sql"
	"errors"
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

func TestDiscoveryClosureMemberTransactionRollbackLeavesNoAuthoritativePacket(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	var projectID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-discovery-rollback', 'Discovery rollback') RETURNING id`).Scan(&projectID); err != nil { t.Fatal(err) }
	var workspace FeatureWorkspace
	if err := store.WithTx(ctx, func(tx *Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, "project-discovery-rollback")
		if err != nil { return err }
		workspace, err = tx.CreateFeatureWorkspace(ctx, CreateFeatureWorkspaceParams{WorkspaceID: "workspace-discovery-rollback", ProjectRowID: project.ID, FeatureSlug: "discovery-rollback"})
		return err
	}); err != nil { t.Fatal(err) }
	rollback := errors.New("injected rollback")
	if err := store.WithTx(ctx, func(tx *Tx) error {
		artifact, err := tx.CreateDiscoveryArtifact(ctx, DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-rollback", WorkspaceRowID: workspace.ID, RelativePath: "feature-discovery/workspace-discovery-rollback/discovery-artifact-rollback/discovery.md", SHA256: strings.Repeat("a", 64), MediaType: "text/markdown", SizeBytes: 1})
		if err != nil { return err }
		revision, err := tx.CreateIntegratedDiscoveryRevision(ctx, IntegratedDiscoveryRevision{DiscoveryRevisionID: "discovery-revision-rollback", WorkspaceRowID: workspace.ID, RevisionNumber: 1, ArtifactRowID: artifact.ID, CreatedIdentity: "operator", SettledDestination: sql.NullString{String: "requirements", Valid: true}})
		if err != nil { return err }
		manifest, err := tx.CreateDiscoveryArtifact(ctx, DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-rollback-manifest", WorkspaceRowID: workspace.ID, RelativePath: "feature-discovery/workspace-discovery-rollback/discovery-artifact-rollback/closure.json", SHA256: strings.Repeat("b", 64), MediaType: "application/vnd.relay.feature-discovery-closure+json", SizeBytes: 1})
		if err != nil { return err }
		packet, err := tx.CreateDiscoveryClosurePacket(ctx, DiscoveryClosurePacket{ClosurePacketID: "discovery-packet-rollback", WorkspaceRowID: workspace.ID, ClosingRevisionRowID: revision.ID, Destination: "requirements", ManifestArtifactRowID: manifest.ID, ManifestSha256: manifest.SHA256, ManifestSizeBytes: manifest.SizeBytes, ManifestMediaType: manifest.MediaType})
		if err != nil { return err }
		if _, err = tx.CreateDiscoveryClosurePacketMember(ctx, DiscoveryClosurePacketMember{ClosurePacketRowID: packet.ID, Sequence: 1, OwnerFamily: "integrated_discovery", ArtifactRowID: artifact.ID, SourceIdentity: revision.DiscoveryRevisionID, Sha256: artifact.SHA256, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType, SemanticRole: "closing_revision"}); err != nil { return err }
		return rollback
	}); !errors.Is(err, rollback) { t.Fatalf("transaction error = %v", err) }
	for _, table := range []string{"feature_workspace_discovery_closure_packets", "feature_workspace_discovery_closure_packet_members", "feature_workspace_integrated_discovery_revisions"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE workspace_row_id = ?`, workspace.ID).Scan(&count); err != nil {
			if table == "feature_workspace_discovery_closure_packet_members" { err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_closure_packet_members`).Scan(&count) }
			if err != nil { t.Fatal(err) }
		}
		if count != 0 { t.Fatalf("%s rows = %d", table, count) }
	}
}
