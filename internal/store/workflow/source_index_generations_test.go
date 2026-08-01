package workflowstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/sourceindex"
)

func TestSourceIndexGenerationLifecycleAndConvergence(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	identity := testSourceIndexIdentity(t, "vault-lifecycle", "a", "b")
	created, wasCreated, err := store.CreateOrResolveSourceIndexGeneration(ctx, CreateOrResolveSourceIndexGenerationParams{Identity: identity})
	if err != nil || !wasCreated || created.State != SourceIndexGenerationPending || created.AttemptCount != 0 {
		t.Fatalf("created = %#v, %t, %v", created, wasCreated, err)
	}
	existing, wasCreated, err := store.CreateOrResolveSourceIndexGeneration(ctx, CreateOrResolveSourceIndexGenerationParams{Identity: identity})
	if err != nil || wasCreated || existing.ID != created.ID || existing.State != SourceIndexGenerationPending {
		t.Fatalf("existing = %#v, %t, %v", existing, wasCreated, err)
	}
	building, err := store.BeginSourceIndexGenerationBuild(ctx, created.GenerationID)
	if err != nil || building.State != SourceIndexGenerationBuilding || building.AttemptCount != 1 || building.BuildingStartedAt == "" {
		t.Fatalf("building = %#v, %v", building, err)
	}
	if _, err := store.BeginSourceIndexGenerationBuild(ctx, created.GenerationID); !errors.Is(err, ErrSourceIndexGenerationLifecycleConflict) {
		t.Fatalf("second begin error = %v", err)
	}
	ready, err := store.MarkSourceIndexGenerationReady(ctx, MarkSourceIndexGenerationReadyParams{GenerationID: created.GenerationID, GenerationManifestSHA256: strings.Repeat("1", 64), CoverageManifestSHA256: strings.Repeat("2", 64), ArtifactManifestSHA256: strings.Repeat("3", 64)})
	if err != nil || ready.State != SourceIndexGenerationReady || ready.AttemptCount != 1 || ready.ReadyAt == "" {
		t.Fatalf("ready = %#v, %v", ready, err)
	}
	retired, err := store.RetireSourceIndexGeneration(ctx, created.GenerationID)
	if err != nil || retired.State != SourceIndexGenerationRetired || retired.RetiredAt == "" || retired.GenerationManifestSHA256 != ready.GenerationManifestSHA256 {
		t.Fatalf("retired = %#v, %v", retired, err)
	}
	if _, err := store.RetrySourceIndexGeneration(ctx, created.GenerationID); !errors.Is(err, ErrSourceIndexGenerationLifecycleConflict) {
		t.Fatalf("retry retired error = %v", err)
	}
}

func TestSourceIndexGenerationFailureRetryAndReads(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	identity := testSourceIndexIdentity(t, "vault-retry", "c", "d")
	generation, _, err := store.CreateOrResolveSourceIndexGeneration(ctx, CreateOrResolveSourceIndexGenerationParams{Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginSourceIndexGenerationBuild(ctx, generation.GenerationID); err != nil {
		t.Fatal(err)
	}
	failed, err := store.MarkSourceIndexGenerationFailed(ctx, MarkSourceIndexGenerationFailedParams{GenerationID: generation.GenerationID, FailureCode: "build_failed", FailureMessage: "the builder rejected this generation"})
	if err != nil || failed.State != SourceIndexGenerationFailed || failed.FailedAt == "" {
		t.Fatalf("failed = %#v, %v", failed, err)
	}
	pending, err := store.RetrySourceIndexGeneration(ctx, generation.GenerationID)
	if err != nil || pending.State != SourceIndexGenerationPending || pending.AttemptCount != 1 || pending.FailureCode != "" || pending.BuildingStartedAt != "" || pending.FailedAt != "" {
		t.Fatalf("pending = %#v, %v", pending, err)
	}
	if _, err := store.BeginSourceIndexGenerationBuild(ctx, generation.GenerationID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkSourceIndexGenerationReady(ctx, MarkSourceIndexGenerationReadyParams{GenerationID: generation.GenerationID, GenerationManifestSHA256: strings.Repeat("4", 64), CoverageManifestSHA256: strings.Repeat("5", 64), ArtifactManifestSHA256: strings.Repeat("6", 64)}); err != nil {
		t.Fatal(err)
	}
	byIdentity, err := store.GetSourceIndexGenerationByIdentity(ctx, identity)
	if err != nil || byIdentity.AttemptCount != 2 || byIdentity.State != SourceIndexGenerationReady {
		t.Fatalf("by identity = %#v, %v", byIdentity, err)
	}
	listed, err := store.ListSourceIndexGenerationsByState(ctx, SourceIndexGenerationReady)
	if err != nil || len(listed) != 1 || listed[0].ID != generation.ID {
		t.Fatalf("listed = %#v, %v", listed, err)
	}
	if _, err := store.MarkSourceIndexGenerationReady(ctx, MarkSourceIndexGenerationReadyParams{GenerationID: generation.GenerationID}); !errors.Is(err, ErrInvalidSourceIndexGeneration) {
		t.Fatalf("invalid ready error = %v", err)
	}
	if _, err := store.GetSourceIndexGeneration(ctx, "bad"); !errors.Is(err, ErrInvalidSourceIndexGeneration) {
		t.Fatalf("invalid get error = %v", err)
	}
	if _, err := store.GetSourceIndexGeneration(ctx, strings.Repeat("f", 64)); !errors.Is(err, ErrSourceIndexGenerationNotFound) {
		t.Fatalf("missing get error = %v", err)
	}
}

func TestSourceIndexGenerationDatabaseGuards(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	identity := testSourceIndexIdentity(t, "vault-guards", "e", "f")
	generation, _, err := store.CreateOrResolveSourceIndexGeneration(ctx, CreateOrResolveSourceIndexGenerationParams{Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE source_index_generations SET vault_id = 'different' WHERE id = ?`,
		`DELETE FROM source_index_generations WHERE id = ?`,
		`UPDATE source_index_generations SET state = 'ready' WHERE id = ?`,
		`UPDATE source_index_generations SET attempt_count = 1 WHERE id = ?`,
	} {
		if _, err := store.DB().ExecContext(ctx, query, generation.ID); err == nil {
			t.Fatalf("database accepted %s", query)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO source_index_generations (generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`, strings.Repeat("9", 64), identity.Version, "vault-corrupt", identity.CommitOID, identity.TreeOID, identity.Engine, identity.EngineRevision, identity.BuildContractVersion, identity.BuildOptionsSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSourceIndexGeneration(ctx, strings.Repeat("9", 64)); !errors.Is(err, ErrSourceIndexGenerationIntegrity) {
		t.Fatalf("malformed row error = %v", err)
	}
}

func TestSourceIndexGenerationReopens(t *testing.T) {
	ctx := context.Background()
	store, root := openWorkflowTestStore(t)
	identity := testSourceIndexIdentity(t, "vault-reopen", "7", "8")
	generation, _, err := store.CreateOrResolveSourceIndexGeneration(ctx, CreateOrResolveSourceIndexGenerationParams{Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginSourceIndexGenerationBuild(ctx, generation.GenerationID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkSourceIndexGenerationReady(ctx, MarkSourceIndexGenerationReadyParams{GenerationID: generation.GenerationID, GenerationManifestSHA256: strings.Repeat("1", 64), CoverageManifestSHA256: strings.Repeat("2", 64), ArtifactManifestSHA256: strings.Repeat("3", 64)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetireSourceIndexGeneration(ctx, generation.GenerationID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.GetSourceIndexGeneration(ctx, generation.GenerationID)
	if err != nil || loaded.Identity != identity || loaded.State != SourceIndexGenerationRetired || loaded.AttemptCount != 1 || loaded.GenerationManifestSHA256 != strings.Repeat("1", 64) || loaded.RetiredAt == "" {
		t.Fatalf("reopened = %#v, %v", loaded, err)
	}
}

func testSourceIndexIdentity(t *testing.T, vaultID, commit, tree string) sourceindex.GenerationIdentity {
	t.Helper()
	digest, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity(vaultID, strings.Repeat(commit, 40), strings.Repeat(tree, 40), digest)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
