package operations

import (
	"context"
	"testing"

	"relay/internal/app/idempotency"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
)

func TestAuthorityPublicationResolveClassifiesMissReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	service, _ := openAuthorityPublicationRemediationService(t, ctx)
	input := authorityPublicationCreateInput(t, "mutation-resolve", "opkt-resolve", "artifact-resolve", "planner.requirements", "planner-authoring.v1")
	miss, err := service.Resolve(ctx, AuthorityPublicationResolveInput{RequestIdentity: input.RequestIdentity, Idempotency: input.Idempotency})
	if err != nil || miss.Kind != AuthorityPublicationResolutionMiss {
		t.Fatalf("miss = %#v err=%v", miss, err)
	}
	committed, err := service.Publish(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Resolve(ctx, AuthorityPublicationResolveInput{RequestIdentity: input.RequestIdentity, Idempotency: input.Idempotency})
	if err != nil || replay.Kind != AuthorityPublicationResolutionReplay || replay.Result.Packet.PacketID != committed.Packet.PacketID || replay.Result.Mutation.ResultSHA256 != committed.Mutation.ResultSHA256 {
		t.Fatalf("replay = %#v err=%v", replay, err)
	}
	other := semanticidentity.CreateOperationPacket{SurfaceContract: "planner-authoring.v1", OperationID: "planner.design", ProjectID: "project-test"}
	fingerprint, err := semanticidentity.BuildFingerprint(other)
	if err != nil {
		t.Fatal(err)
	}
	conflictInput := AuthorityPublicationResolveInput{RequestIdentity: other, Idempotency: idempotency.RecordSuccessInput{
		Key:                   idempotency.MutationKey{SurfaceContractID: "planner-authoring.v1", Tool: registry.MutationToolCreateOperationPacket, MutationID: "mutation-resolve"},
		SurfaceManifestSHA256: input.Idempotency.SurfaceManifestSHA256,
		Fingerprint:           fingerprint,
	}}
	if _, err := service.Resolve(ctx, conflictInput); !idempotency.HasCode(err, idempotency.ErrorMutationConflict) {
		t.Fatalf("conflict = %v", err)
	}
}
