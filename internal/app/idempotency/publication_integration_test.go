package idempotency

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/mcp/semanticidentity"
	workflowstore "relay/internal/store/workflow"
)

func TestRecordSuccessInTxCommitsAndRollsBackWithPublicationTransaction(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	service := mustService(t, store)

	success := validSubmitInput(t, "mutation-publication-success", validSubmitRequest("feature.plan.json"))
	batch, packetFile := publicationBatch(t, store, "publication-idempotency-success")
	var packet workflowstore.OperationPacket
	err := store.CommitOperationPacketPublication(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(ctx, workflowstore.CreateOperationPacketArtifactParams{
			ArtifactID: "artifact-publication-idempotency-success", Kind: packetFile.Kind,
			RelativePath: packetFile.RelativePath, MediaType: packetFile.MediaType,
			SHA256: packetFile.SHA256, SizeBytes: packetFile.SizeBytes,
		})
		if err != nil {
			return err
		}
		stored, err := service.RecordSuccessInTx(ctx, tx, success, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			packet, err = tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
				PacketID: "opkt-publication-idempotency-success", PacketSHA256: artifact.SHA256,
				SchemaVersion: workflowstore.OperationPacketSchemaVersion, Role: "planner",
				OperationID: "planner.plan", SurfaceContractID: "planner-plan.v1", ProjectID: "project-test",
				ReadinessState: workflowstore.OperationPacketReadinessReady,
				CreatedAt:      "2026-07-18T00:00:00.000000000Z", PacketArtifactRowID: artifact.ID,
				CoordinatedPublicationID: sql.NullString{String: batch.PublicationID(), Valid: true},
			})
			if err != nil {
				return nil, err
			}
			return validSubmitResult("plan-publication-success"), nil
		})
		if err != nil || stored.ResultKind != semanticidentity.ResultKindSubmitPlan {
			return err
		}
		result, err := tx.GetMCPMutationResult(ctx, storeKey(success.Key))
		if err != nil {
			return err
		}
		if _, err := tx.AttachOperationPacketDependency(ctx, workflowstore.AttachOperationPacketDependencyParams{
			PacketRowID: packet.ID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument,
			DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true,
			OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := tx.CreateOperationPacketArtifactBinding(ctx, workflowstore.CreateOperationPacketArtifactBindingParams{
			PublicationID: batch.PublicationID(), PacketRowID: packet.ID, Sequence: 0,
			DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID,
			PacketArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true},
		}); err != nil {
			return err
		}
		_, err = tx.CreateOperationPacketPublication(ctx, workflowstore.CreateOperationPacketPublicationParams{
			PublicationID: batch.PublicationID(), PacketRowID: packet.ID, PacketArtifactRowID: artifact.ID,
			MutationResultRowID: result.ID, Namespace: batch.Namespace(), ManifestSHA256: batch.ManifestSHA256(),
			ExpectedBindingCount: 1, ExpectedDependencyCount: 1,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOperationPacketPublicationByPacketID(ctx, packet.PacketID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(packetFile.RelativePath))); err != nil {
		t.Fatal(err)
	}

	rollback := validSubmitInput(t, "mutation-publication-rollback", validSubmitRequest("rollback.plan.json"))
	rollbackBatch, rollbackFile := publicationBatch(t, store, "publication-idempotency-rollback")
	err = store.CommitOperationPacketPublication(ctx, rollbackBatch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(ctx, workflowstore.CreateOperationPacketArtifactParams{
			ArtifactID: "artifact-publication-idempotency-rollback", Kind: rollbackFile.Kind,
			RelativePath: rollbackFile.RelativePath, MediaType: rollbackFile.MediaType,
			SHA256: rollbackFile.SHA256, SizeBytes: rollbackFile.SizeBytes,
		})
		if err != nil {
			return err
		}
		_, err = service.RecordSuccessInTx(ctx, tx, rollback, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			_, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
				PacketID: "opkt-publication-idempotency-rollback", PacketSHA256: artifact.SHA256,
				SchemaVersion: workflowstore.OperationPacketSchemaVersion, Role: "planner",
				OperationID: "planner.plan", SurfaceContractID: "planner-plan.v1", ProjectID: "project-test",
				ReadinessState: workflowstore.OperationPacketReadinessReady,
				CreatedAt:      "2026-07-18T00:00:01.000000000Z", PacketArtifactRowID: artifact.ID,
				CoordinatedPublicationID: sql.NullString{String: rollbackBatch.PublicationID(), Valid: true},
			})
			if err != nil {
				return nil, err
			}
			return semanticidentity.CreateRunResult{}, nil
		})
		return err
	})
	if !HasCode(err, ErrorInvalidResultIdentity) {
		t.Fatalf("rollback error = %v", err)
	}
	if _, err := store.GetOperationPacketByPacketID(ctx, "opkt-publication-idempotency-rollback"); err == nil {
		t.Fatal("packet survived publication rollback")
	}
	if _, ok, err := store.GetMCPMutationResultOptional(ctx, storeKey(rollback.Key)); err != nil || ok {
		t.Fatalf("mutation row = %v, %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(rollbackFile.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("publication artifact survived rollback: %v", err)
	}
}

func TestPublicationConcurrentWinnerResolutionRunsAfterArtifactRollback(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	service := mustService(t, store)
	input := validSubmitInput(t, "mutation-publication-winner", validSubmitRequest("feature.plan.json"))
	if _, _, err := service.RecordSuccess(ctx, input, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
		return validSubmitResult("plan-winner"), nil
	}); err != nil {
		t.Fatal(err)
	}
	batch, file := publicationBatch(t, store, "publication-idempotency-loser")
	err := store.CommitOperationPacketPublication(ctx, batch, func(tx *workflowstore.Tx) error {
		_, err := service.RecordSuccessInTx(ctx, tx, input, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			t.Fatal("concurrent loser mutation callback ran")
			return nil, nil
		})
		return err
	})
	if !IsConcurrentWinner(err) {
		t.Fatalf("winner error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(file.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("loser publication survived rollback: %v", err)
	}
	result, replay, err := service.ResolveAfterRollback(ctx, input, err)
	if err != nil || !replay || result.ResultKind != semanticidentity.ResultKindSubmitPlan {
		t.Fatalf("winner recovery = %#v, %v, %v", result, replay, err)
	}
}

func publicationBatch(t *testing.T, store *workflowstore.Store, publicationID string) (*workflowartifacts.PublicationBatch, workflowartifacts.File) {
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
