package operations

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCoordinatedPacketReadsFailClosedWhenRequiredAuthorityIsUnavailable(t *testing.T) {
	ctx := context.Background()
	publication, store := openAuthorityPublicationRemediationService(t, ctx)
	input := authorityPublicationCreateInput(t, "mutation-corrupt", "opkt-corrupt", "artifact-corrupt", "planner.plan", "planner-plan.v1")
	created, err := publication.Publish(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.GetOperationPacketArtifact(ctx, created.Packet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath))); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, created.Packet.PacketID); ErrorCode(err) != CodePacketArtifactMismatch {
		t.Fatalf("get error = %v code=%q", err, ErrorCode(err))
	}
}

func TestLifecycleErrorCodesRemainBounded(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{CodeCompleteLifecycleRequired, "complete operation packet lifecycle is required"},
		{CodeRepositoryAuthorityUnavailable, "operation packet repository authority is unavailable"},
	}
	for _, test := range cases {
		err := (&Error{Code: test.code}).Error()
		if err != test.want {
			t.Fatalf("code %q = %q, want %q", test.code, err, test.want)
		}
	}
}
