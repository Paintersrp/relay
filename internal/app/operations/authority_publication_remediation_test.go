package operations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/idempotency"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func TestAuthorityPublicationRejectsUnboundWinnerAndReturnsCommittedWinner(t *testing.T) {
	ctx := context.Background()
	service, store := openAuthorityPublicationRemediationService(t, ctx)

	unbound := authorityPublicationRemediationInput(t, "mutation-unbound-winner", "opkt-unbound-winner", "artifact-unbound-winner", "planner.plan", "planner-plan.v1")
	mutationService, err := idempotency.New(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mutationService.RecordSuccess(ctx, unbound.Idempotency, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
		return authorityPublicationSubmitResult("plan-unbound"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, unbound); ErrorCode(err) != CodeAuthorityPublicationConflict {
		t.Fatalf("unbound winner error = %v code=%q", err, ErrorCode(err))
	}
	if _, err := store.GetOperationPacketPublicationIntegrityByMutationKey(ctx, publicationMutationKey(unbound)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unbound winner publication lookup = %v", err)
	}

	first := authorityPublicationRemediationInput(t, "mutation-bound-winner", "opkt-bound-winner", "artifact-bound-winner-first", "planner.plan", "planner-plan.v1")
	committed, err := service.Publish(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	retry := authorityPublicationRemediationInput(t, "mutation-bound-winner", "opkt-bound-winner", "artifact-bound-winner-retry", "planner.plan", "planner-plan.v1")
	winner, err := service.Publish(ctx, retry)
	if err != nil {
		t.Fatal(err)
	}
	if !winner.Replay || winner.Publication.PublicationID != committed.Publication.PublicationID || winner.Packet.PacketID != committed.Packet.PacketID || winner.Mutation.ResultSHA256 != committed.Mutation.ResultSHA256 {
		t.Fatalf("committed winner = %#v, first = %#v", winner, committed)
	}
}

func TestAuthorityPublicationRejectsPacketMutationAuthorityMismatch(t *testing.T) {
	ctx := context.Background()
	service, _ := openAuthorityPublicationRemediationService(t, ctx)
	for _, tc := range []struct {
		name      string
		operation registry.OperationID
		surface   registry.SurfaceContractID
	}{
		{name: "cross surface", operation: "planner.one_shot_execution_spec", surface: "planner-execution.v1"},
		{name: "operation action mismatch", operation: "planner.requirements", surface: "planner-plan.v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := authorityPublicationRemediationInput(t, "mutation-"+strings.ReplaceAll(tc.name, " ", "-"), "opkt-"+strings.ReplaceAll(tc.name, " ", "-"), "artifact-"+strings.ReplaceAll(tc.name, " ", "-"), tc.operation, tc.surface)
			if _, err := service.Publish(ctx, input); ErrorCode(err) != CodeAuthorityPublicationConflict {
				t.Fatalf("authority mismatch error = %v code=%q", err, ErrorCode(err))
			}
		})
	}
}

func TestAuthorityPublicationSanitizesCallbackPromotionAndCommitFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("callback diagnostics", func(t *testing.T) {
		service, _ := openAuthorityPublicationRemediationService(t, ctx)
		input := authorityPublicationRemediationInput(t, "mutation-callback-diagnostic", "opkt-callback-diagnostic", "artifact-callback-diagnostic", "planner.plan", "planner-plan.v1")
		input.Mutation = func(context.Context, *workflowstore.Tx, PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			return workflowstore.OperationPacket{}, nil, errors.New("password=do-not-render C:\\private\\workflow.sqlite")
		}
		result, err := service.Publish(ctx, input)
		assertSanitizedAuthorityPublicationFailure(t, result, err, "password=", "workflow.sqlite")
	})

	t.Run("promotion diagnostics", func(t *testing.T) {
		service, _ := openAuthorityPublicationRemediationService(t, ctx)
		input := authorityPublicationRemediationInput(t, "mutation-promotion-diagnostic", "opkt-promotion-diagnostic", "artifact-promotion-diagnostic", "planner.plan", "planner-plan.v1")
		original := input.Mutation
		input.Mutation = func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			if err := os.MkdirAll(filepath.Dir(mutation.PacketArtifact.AbsolutePath), 0o700); err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			return original(ctx, tx, mutation)
		}
		result, err := service.Publish(ctx, input)
		assertSanitizedAuthorityPublicationFailure(t, result, err, "operation-packet-publications", filepath.Clean(input.PacketArtifactID))
	})

	t.Run("commit diagnostics and promoted rollback", func(t *testing.T) {
		service, store := openAuthorityPublicationRemediationService(t, ctx)
		input := authorityPublicationRemediationInput(t, "mutation-commit-diagnostic", "opkt-commit-diagnostic", "artifact-commit-diagnostic", "planner.plan", "planner-plan.v1")
		original := input.Mutation
		var publicationID string
		input.Mutation = func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			publicationID = mutation.PublicationID
			packet, identity, err := original(ctx, tx, mutation)
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			_, err = tx.CreateOperationPacketRetainedArtifact(ctx, workflowstore.CreateOperationPacketRetainedArtifactParams{
				PublicationID: "publication-missing-deferred-parent", ArtifactID: "artifact-missing-deferred-parent",
				Kind:         workflowstore.OperationPacketRetainedArtifactInlineInput,
				RelativePath: "operation-packet-publications/publication-missing-deferred-parent/inputs/value.txt",
				MediaType:    "text/plain", SHA256: strings.Repeat("b", 64), SizeBytes: 1,
			})
			return packet, identity, err
		}
		result, err := service.Publish(ctx, input)
		assertSanitizedAuthorityPublicationFailure(t, result, err, "FOREIGN KEY", "publication-missing-deferred-parent")
		if _, err := os.Stat(filepath.Join(store.ArtifactStore().Root(), "operation-packet-publications", publicationID)); !os.IsNotExist(err) {
			t.Fatalf("promoted loser survived failed commit: %v", err)
		}
	})
}

func openAuthorityPublicationRemediationService(t *testing.T, ctx context.Context) (*AuthorityPublicationService, *workflowstore.Store) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	vaults, err := sourcevault.Open(ctx, filepath.Join(root, "source-vaults"), store)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAuthorityPublicationService(store, vaults)
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func authorityPublicationRemediationInput(t *testing.T, mutationID, packetID, packetArtifactID string, operationID registry.OperationID, packetSurface registry.SurfaceContractID) AuthorityPublicationInput {
	t.Helper()
	artifactSHA := strings.Repeat("a", 64)
	request := semanticidentity.SubmitPlan{CanonicalArtifactMutation: semanticidentity.CanonicalArtifactMutation{
		SurfaceContract: "planner-plan.v1", ExpectedPacketID: packetID,
		ArtifactName: "feature.plan.json", MediaType: "application/json", ExpectedSHA256: artifactSHA,
		SensitiveDataClearance: registry.SensitiveDataClearance{
			PolicyVersion: registry.SensitiveDataClearancePolicyVersion,
			SubjectSHA256: artifactSHA, Confirmed: true,
		},
	}}
	fingerprint, err := semanticidentity.BuildFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	manifest, ok := registry.SurfaceManifestSHA256(request.SurfaceContract)
	if !ok {
		t.Fatal("planner Plan surface manifest is unavailable")
	}
	return AuthorityPublicationInput{
		PacketID: packetID, PacketArtifactID: packetArtifactID,
		PacketMediaType: "application/vnd.relay.operation-packet+json;version=1",
		PacketBytes:     []byte(`{"packet_id":"` + packetID + `"}` + "\n"),
		Idempotency: idempotency.RecordSuccessInput{
			Key: idempotency.MutationKey{
				SurfaceContractID: request.SurfaceContract,
				Tool:              registry.MutationToolSubmitPlan,
				MutationID:        mutationID,
			},
			SurfaceManifestSHA256: manifest,
			Fingerprint:           fingerprint,
		},
		Mutation: func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			packet, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
				PacketID: packetID, PacketSHA256: mutation.PacketArtifact.SHA256,
				SchemaVersion: workflowstore.OperationPacketSchemaVersion, Role: "planner", OperationID: string(operationID), SurfaceContractID: string(packetSurface), ProjectID: "project-test",
				ReadinessState:           workflowstore.OperationPacketReadinessReady,
				CoordinatedPublicationID: sql.NullString{String: mutation.PublicationID, Valid: true},
				CreatedAt:                "2026-07-18T00:00:00.000000000Z", PacketArtifactRowID: mutation.PacketArtifactRowID,
			})
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			return packet, authorityPublicationSubmitResult("plan-" + packetID), nil
		},
	}
}

func authorityPublicationSubmitResult(planID string) semanticidentity.SubmitPlanResult {
	return semanticidentity.SubmitPlanResult{
		PlanID: planID, ArtifactID: "artifact-plan-result", ArtifactSHA256: strings.Repeat("a", 64),
		ProjectID: "project-test", SubmissionID: "submission-test", WorkflowState: "active", Complete: true,
	}
}

func publicationMutationKey(input AuthorityPublicationInput) workflowstore.MCPMutationKey {
	return workflowstore.MCPMutationKey{
		SurfaceContractID: string(input.Idempotency.Key.SurfaceContractID),
		ToolName:          string(input.Idempotency.Key.Tool),
		MutationID:        input.Idempotency.Key.MutationID,
	}
}

func assertSanitizedAuthorityPublicationFailure(t *testing.T, _ AuthorityPublicationResult, err error, forbidden ...string) {
	t.Helper()
	if ErrorCode(err) != CodeAuthorityPublicationFailure {
		t.Fatalf("publication failure = %v code=%q", err, ErrorCode(err))
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(err.Error(), value) {
			t.Fatalf("publication error exposed %q: %v", value, err)
		}
	}
}
