package operations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/idempotency"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func TestAuthorityPublicationCreateRefreshAndReplayUseLifecycleAuthority(t *testing.T) {
	ctx := context.Background()
	service, store := openAuthorityPublicationRemediationService(t, ctx)

	create := authorityPublicationCreateInput(t, "mutation-create", "opkt-create", "artifact-create", "planner.plan", "planner-plan.v1")
	created, err := service.Publish(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	createRetry := authorityPublicationCreateInput(t, "mutation-create", "opkt-create-loser", "artifact-create-retry", "planner.plan", "planner-plan.v1")
	createWinner, err := service.Publish(ctx, createRetry)
	if err != nil {
		t.Fatal(err)
	}
	if !createWinner.Replay || createWinner.Publication.PublicationID != created.Publication.PublicationID || createWinner.Packet.PacketID != created.Packet.PacketID || createWinner.Mutation.ResultSHA256 != created.Mutation.ResultSHA256 {
		t.Fatalf("create winner = %#v, first = %#v", createWinner, created)
	}
	if _, err := store.GetOperationPacketByPacketID(ctx, createRetry.PacketID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("losing create packet lookup = %v", err)
	}

	refresh := authorityPublicationRefreshInput(t, created.Packet.PacketID, "mutation-refresh", "opkt-refresh", "artifact-refresh", "planner.plan", "planner-plan.v1")
	refreshed, err := service.Publish(ctx, refresh)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := store.GetOperationPacketByPacketID(ctx, created.Packet.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if prior.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded || !prior.ReplacementPacketRowID.Valid || prior.ReplacementPacketRowID.Int64 != refreshed.Packet.ID || !refreshed.Packet.PriorPacketRowID.Valid || refreshed.Packet.PriorPacketRowID.Int64 != prior.ID {
		t.Fatalf("refresh lineage prior=%#v replacement=%#v", prior, refreshed.Packet)
	}
	refreshRetry := authorityPublicationRefreshInput(t, created.Packet.PacketID, "mutation-refresh", "opkt-refresh-loser", "artifact-refresh-retry", "planner.plan", "planner-plan.v1")
	refreshWinner, err := service.Publish(ctx, refreshRetry)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshWinner.Replay || refreshWinner.Publication.PublicationID != refreshed.Publication.PublicationID || refreshWinner.Packet.PacketID != refreshed.Packet.PacketID || refreshWinner.Mutation.ResultSHA256 != refreshed.Mutation.ResultSHA256 {
		t.Fatalf("refresh winner = %#v, first = %#v", refreshWinner, refreshed)
	}
	if _, err := store.GetOperationPacketByPacketID(ctx, refreshRetry.PacketID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("losing refresh packet lookup = %v", err)
	}
}

func TestAuthorityPublicationRejectsUnboundAndMismatchedLifecycleResults(t *testing.T) {
	ctx := context.Background()

	t.Run("unbound create winner", func(t *testing.T) {
		service, store := openAuthorityPublicationRemediationService(t, ctx)
		input := authorityPublicationCreateInput(t, "mutation-unbound", "opkt-unbound", "artifact-unbound", "planner.plan", "planner-plan.v1")
		mutationService, err := idempotency.New(store)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := mutationService.RecordSuccess(ctx, input.Idempotency, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			return standaloneCreateResult(input), nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Publish(ctx, input); ErrorCode(err) != CodeAuthorityPublicationConflict {
			t.Fatalf("unbound winner error = %v code=%q", err, ErrorCode(err))
		}
	})

	t.Run("create result names another packet", func(t *testing.T) {
		service, _ := openAuthorityPublicationRemediationService(t, ctx)
		input := authorityPublicationCreateInput(t, "mutation-wrong-create", "opkt-wrong-create", "artifact-wrong-create", "planner.plan", "planner-plan.v1")
		original := input.Mutation
		input.Mutation = func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			packet, identity, err := original(ctx, tx, mutation)
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			result := identity.(semanticidentity.CreateOperationPacketResult)
			result.Packet.Summary.PacketID = "opkt-other"
			return packet, result, nil
		}
		if _, err := service.Publish(ctx, input); ErrorCode(err) != CodeAuthorityPublicationConflict {
			t.Fatalf("mismatched create result error = %v code=%q", err, ErrorCode(err))
		}
	})

	t.Run("refresh result names another replacement", func(t *testing.T) {
		service, _ := openAuthorityPublicationRemediationService(t, ctx)
		created, err := service.Publish(ctx, authorityPublicationCreateInput(t, "mutation-prior", "opkt-prior", "artifact-prior", "planner.plan", "planner-plan.v1"))
		if err != nil {
			t.Fatal(err)
		}
		input := authorityPublicationRefreshInput(t, created.Packet.PacketID, "mutation-wrong-refresh", "opkt-wrong-refresh", "artifact-wrong-refresh", "planner.plan", "planner-plan.v1")
		original := input.Mutation
		input.Mutation = func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			packet, identity, err := original(ctx, tx, mutation)
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			result := identity.(semanticidentity.RefreshOperationPacketResult)
			result.PriorPacket.ReplacementPacket.PacketID = "opkt-other"
			return packet, result, nil
		}
		if _, err := service.Publish(ctx, input); ErrorCode(err) != CodeAuthorityPublicationConflict {
			t.Fatalf("mismatched refresh result error = %v code=%q", err, ErrorCode(err))
		}
	})
}

func TestAuthorityPublicationRejectsNonPublicationMutationTools(t *testing.T) {
	ctx := context.Background()
	service, _ := openAuthorityPublicationRemediationService(t, ctx)
	for _, input := range []AuthorityPublicationInput{
		authorityPublicationSubmitPlanInput(t, "mutation-submit-plan"),
		authorityPublicationCloseInput(t, "mutation-close"),
	} {
		if _, err := service.Publish(ctx, input); ErrorCode(err) != CodeAuthorityPublicationConflict {
			t.Fatalf("tool %q error = %v code=%q", input.Idempotency.Key.Tool, err, ErrorCode(err))
		}
	}
}

func TestAuthorityPublicationRejectsPacketOperationAuthorityMismatch(t *testing.T) {
	ctx := context.Background()
	service, _ := openAuthorityPublicationRemediationService(t, ctx)
	input := authorityPublicationCreateInput(t, "mutation-operation-mismatch", "opkt-operation-mismatch", "artifact-operation-mismatch", "planner.plan", "planner-plan.v1")
	input.Mutation = func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
		packet, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
			PacketID: input.PacketID, PacketSHA256: mutation.PacketArtifact.SHA256,
			SchemaVersion: workflowstore.OperationPacketSchemaVersion, Role: "planner", OperationID: "planner.requirements",
			SurfaceContractID: "planner-plan.v1", ProjectID: "project-test", ReadinessState: workflowstore.OperationPacketReadinessReady,
			CoordinatedPublicationID: sql.NullString{String: mutation.PublicationID, Valid: true}, CreatedAt: remediationCreateTime,
			PacketArtifactRowID: mutation.PacketArtifactRowID,
		})
		if err != nil {
			return workflowstore.OperationPacket{}, nil, err
		}
		return packet, semanticidentity.CreateOperationPacketResult{
			Packet:                packetViewIdentity(packet, mutation.PacketArtifact, input.PacketArtifactID, nil),
			SurfaceManifestSHA256: input.Idempotency.SurfaceManifestSHA256,
			Complete:              true,
		}, nil
	}
	if _, err := service.Publish(ctx, input); ErrorCode(err) != CodeAuthorityPublicationConflict {
		t.Fatalf("operation mismatch error = %v code=%q", err, ErrorCode(err))
	}
}

func TestAuthorityPublicationSanitizesCallbackPromotionAndCommitFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("callback diagnostics", func(t *testing.T) {
		service, _ := openAuthorityPublicationRemediationService(t, ctx)
		input := authorityPublicationCreateInput(t, "mutation-callback-diagnostic", "opkt-callback-diagnostic", "artifact-callback-diagnostic", "planner.plan", "planner-plan.v1")
		input.Mutation = func(context.Context, *workflowstore.Tx, PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			return workflowstore.OperationPacket{}, nil, errors.New("password=do-not-render C:\\private\\workflow.sqlite")
		}
		result, err := service.Publish(ctx, input)
		assertSanitizedAuthorityPublicationFailure(t, result, err, "password=", "workflow.sqlite")
	})

	t.Run("promotion diagnostics", func(t *testing.T) {
		service, _ := openAuthorityPublicationRemediationService(t, ctx)
		input := authorityPublicationCreateInput(t, "mutation-promotion-diagnostic", "opkt-promotion-diagnostic", "artifact-promotion-diagnostic", "planner.plan", "planner-plan.v1")
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
		input := authorityPublicationCreateInput(t, "mutation-commit-diagnostic", "opkt-commit-diagnostic", "artifact-commit-diagnostic", "planner.plan", "planner-plan.v1")
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

const (
	remediationCreateTime  = "2026-07-18T00:00:00.000000000Z"
	remediationRefreshTime = "2026-07-18T00:00:01.000000000Z"
)

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

func authorityPublicationCreateInput(t *testing.T, mutationID, packetID, packetArtifactID string, operationID registry.OperationID, surface registry.SurfaceContractID) AuthorityPublicationInput {
	t.Helper()
	request := semanticidentity.CreateOperationPacket{SurfaceContract: surface, OperationID: operationID, ProjectID: "project-test"}
	fingerprint, err := semanticidentity.BuildFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	manifest, ok := registry.SurfaceManifestSHA256(surface)
	if !ok {
		t.Fatal("surface manifest is unavailable")
	}
	input := AuthorityPublicationInput{
		PacketID: packetID, PacketArtifactID: packetArtifactID, RequestIdentity: request,
		PacketMediaType: "application/vnd.relay.operation-packet+json;version=1",
		PacketBytes:     []byte(`{"packet_id":"` + packetID + `"}` + "\n"),
		Idempotency: idempotency.RecordSuccessInput{
			Key:                   idempotency.MutationKey{SurfaceContractID: surface, Tool: registry.MutationToolCreateOperationPacket, MutationID: mutationID},
			SurfaceManifestSHA256: manifest, Fingerprint: fingerprint,
		},
	}
	input.Mutation = func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
		packet, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
			PacketID: packetID, PacketSHA256: mutation.PacketArtifact.SHA256,
			SchemaVersion: workflowstore.OperationPacketSchemaVersion, Role: "planner", OperationID: string(operationID),
			SurfaceContractID: string(surface), ProjectID: "project-test", ReadinessState: workflowstore.OperationPacketReadinessReady,
			CoordinatedPublicationID: sql.NullString{String: mutation.PublicationID, Valid: true}, CreatedAt: remediationCreateTime,
			PacketArtifactRowID: mutation.PacketArtifactRowID,
		})
		if err != nil {
			return workflowstore.OperationPacket{}, nil, err
		}
		return packet, semanticidentity.CreateOperationPacketResult{
			Packet: packetViewIdentity(packet, mutation.PacketArtifact, input.PacketArtifactID, nil), SurfaceManifestSHA256: manifest, Complete: true,
		}, nil
	}
	return input
}

func authorityPublicationRefreshInput(t *testing.T, priorPacketID, mutationID, packetID, packetArtifactID string, operationID registry.OperationID, surface registry.SurfaceContractID) AuthorityPublicationInput {
	t.Helper()
	request := semanticidentity.RefreshOperationPacket{SurfaceContract: surface, ExpectedPacketID: priorPacketID}
	fingerprint, err := semanticidentity.BuildFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	manifest, ok := registry.SurfaceManifestSHA256(surface)
	if !ok {
		t.Fatal("surface manifest is unavailable")
	}
	input := AuthorityPublicationInput{
		PacketID: packetID, PriorPacketID: priorPacketID, PacketArtifactID: packetArtifactID, RequestIdentity: request,
		PacketMediaType: "application/vnd.relay.operation-packet+json;version=1",
		PacketBytes:     []byte(`{"packet_id":"` + packetID + `"}` + "\n"),
		Idempotency: idempotency.RecordSuccessInput{
			Key:                   idempotency.MutationKey{SurfaceContractID: surface, Tool: registry.MutationToolRefreshOperationPacket, MutationID: mutationID},
			SurfaceManifestSHA256: manifest, Fingerprint: fingerprint,
		},
	}
	input.Mutation = func(ctx context.Context, tx *workflowstore.Tx, mutation PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
		prior, err := tx.GetOperationPacketByPacketID(ctx, priorPacketID)
		if err != nil {
			return workflowstore.OperationPacket{}, nil, err
		}
		replacement, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
			PacketID: packetID, PacketSHA256: mutation.PacketArtifact.SHA256,
			SchemaVersion: workflowstore.OperationPacketSchemaVersion, Role: prior.Role, OperationID: prior.OperationID,
			SurfaceContractID: prior.SurfaceContractID, ProjectID: prior.ProjectID, ReadinessState: workflowstore.OperationPacketReadinessReady,
			PriorPacketRowID:         sql.NullInt64{Int64: prior.ID, Valid: true},
			CoordinatedPublicationID: sql.NullString{String: mutation.PublicationID, Valid: true}, CreatedAt: remediationRefreshTime,
			PacketArtifactRowID: mutation.PacketArtifactRowID,
		})
		if err != nil {
			return workflowstore.OperationPacket{}, nil, err
		}
		if _, err := tx.SupersedeOperationPacket(ctx, workflowstore.SupersedeOperationPacketParams{
			PacketID: prior.PacketID, ReplacementPacketRowID: replacement.ID, SupersededAt: remediationRefreshTime,
		}); err != nil {
			return workflowstore.OperationPacket{}, nil, err
		}
		prior, err = tx.GetOperationPacketByPacketID(ctx, priorPacketID)
		if err != nil {
			return workflowstore.OperationPacket{}, nil, err
		}
		return replacement, semanticidentity.RefreshOperationPacketResult{
			PriorPacket:           packetSummaryIdentity(prior, &replacement),
			Packet:                packetViewIdentity(replacement, mutation.PacketArtifact, input.PacketArtifactID, nil),
			SurfaceManifestSHA256: manifest, Complete: true,
		}, nil
	}
	return input
}

func authorityPublicationSubmitPlanInput(t *testing.T, mutationID string) AuthorityPublicationInput {
	t.Helper()
	sha := strings.Repeat("a", 64)
	request := semanticidentity.SubmitPlan{CanonicalArtifactMutation: semanticidentity.CanonicalArtifactMutation{
		SurfaceContract: "planner-plan.v1", ExpectedPacketID: "opkt-submit-plan", ArtifactName: "feature.plan.json",
		MediaType: "application/json", ExpectedSHA256: sha,
		SensitiveDataClearance: registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true},
	}}
	fingerprint, err := semanticidentity.BuildFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := registry.SurfaceManifestSHA256("planner-plan.v1")
	return AuthorityPublicationInput{
		PacketID: "opkt-submit-plan", RequestIdentity: request, PacketArtifactID: "artifact-submit-plan", PacketMediaType: "application/vnd.relay.operation-packet+json;version=1", PacketBytes: []byte("{}\n"),
		Idempotency: idempotency.RecordSuccessInput{Key: idempotency.MutationKey{SurfaceContractID: "planner-plan.v1", Tool: registry.MutationToolSubmitPlan, MutationID: mutationID}, SurfaceManifestSHA256: manifest, Fingerprint: fingerprint},
		Mutation: func(context.Context, *workflowstore.Tx, PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			return workflowstore.OperationPacket{}, nil, errors.New("non-publication mutation callback must not run")
		},
	}
}

func authorityPublicationCloseInput(t *testing.T, mutationID string) AuthorityPublicationInput {
	t.Helper()
	request := semanticidentity.CloseOperationPacket{SurfaceContract: "planner-plan.v1", ExpectedPacketID: "opkt-close"}
	fingerprint, err := semanticidentity.BuildFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := registry.SurfaceManifestSHA256("planner-plan.v1")
	return AuthorityPublicationInput{
		PacketID: "opkt-close", RequestIdentity: request, PacketArtifactID: "artifact-close", PacketMediaType: "application/vnd.relay.operation-packet+json;version=1", PacketBytes: []byte("{}\n"),
		Idempotency: idempotency.RecordSuccessInput{Key: idempotency.MutationKey{SurfaceContractID: "planner-plan.v1", Tool: registry.MutationToolCloseOperationPacket, MutationID: mutationID}, SurfaceManifestSHA256: manifest, Fingerprint: fingerprint},
		Mutation: func(context.Context, *workflowstore.Tx, PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			return workflowstore.OperationPacket{}, nil, errors.New("close mutation callback must not run")
		},
	}
}

func standaloneCreateResult(input AuthorityPublicationInput) semanticidentity.CreateOperationPacketResult {
	sum := sha256.Sum256(input.PacketBytes)
	sha := hex.EncodeToString(sum[:])
	return semanticidentity.CreateOperationPacketResult{
		Packet: semanticidentity.OperationPacketViewIdentity{
			Summary: semanticidentity.PacketSummaryIdentity{
				PacketID: input.PacketID, PacketSHA256: sha, SchemaVersion: workflowstore.OperationPacketSchemaVersion,
				Role: "planner", OperationID: "planner.plan", SurfaceContractID: "planner-plan.v1", ProjectID: "project-test",
				ReadinessState: workflowstore.OperationPacketReadinessReady, LifecycleState: workflowstore.OperationPacketLifecycleActive,
			},
			Document: semanticidentity.PacketDocumentIdentity{ArtifactID: input.PacketArtifactID, MediaType: input.PacketMediaType, SizeBytes: int64(len(input.PacketBytes)), SHA256: sha},
		},
		SurfaceManifestSHA256: input.Idempotency.SurfaceManifestSHA256, Complete: true,
	}
}

func packetViewIdentity(packet workflowstore.OperationPacket, artifact workflowartifacts.File, artifactID string, replacement *workflowstore.OperationPacket) semanticidentity.OperationPacketViewIdentity {
	return semanticidentity.OperationPacketViewIdentity{Summary: packetSummaryIdentity(packet, replacement), Document: semanticidentity.PacketDocumentIdentity{
		ArtifactID: artifactID, MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256,
	}}
}

func packetSummaryIdentity(packet workflowstore.OperationPacket, replacement *workflowstore.OperationPacket) semanticidentity.PacketSummaryIdentity {
	value := semanticidentity.PacketSummaryIdentity{
		PacketID: packet.PacketID, PacketSHA256: packet.PacketSHA256, SchemaVersion: packet.SchemaVersion,
		Role: packet.Role, OperationID: registry.OperationID(packet.OperationID), SurfaceContractID: registry.SurfaceContractID(packet.SurfaceContractID),
		ProjectID: packet.ProjectID, ReadinessState: packet.ReadinessState, LifecycleState: packet.LifecycleState,
	}
	if packet.SupersededAt.Valid {
		copy := packet.SupersededAt.String
		value.SupersededAt = &copy
	}
	if packet.ClosedAt.Valid {
		copy := packet.ClosedAt.String
		value.ClosedAt = &copy
	}
	if replacement != nil {
		value.ReplacementPacket = &semanticidentity.ReplacementPacketIdentity{
			PacketID: replacement.PacketID, PacketSHA256: replacement.PacketSHA256, Role: replacement.Role,
			OperationID: registry.OperationID(replacement.OperationID), SurfaceContractID: registry.SurfaceContractID(replacement.SurfaceContractID),
		}
	}
	return value
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
