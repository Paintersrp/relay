package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type countingStore struct {
	*workflowstore.Store
	withTxCalls atomic.Int64
}

func (s *countingStore) WithTx(ctx context.Context, fn func(*workflowstore.Tx) error) error {
	s.withTxCalls.Add(1)
	return s.Store.WithTx(ctx, fn)
}

func TestRecordSuccessInTxUsesCallerTransactionWithoutNestedTransaction(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	wrapped := &countingStore{Store: store}
	service := mustService(t, wrapped)
	input := validSubmitInput(t, "mutation-in-tx", validSubmitRequest("feature.plan.json"))

	var callbacks atomic.Int64
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		result, err := service.RecordSuccessInTx(ctx, tx, input, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			callbacks.Add(1)
			if _, err := tx.CreateRepositoryTarget(ctx, "transaction-owned", t.TempDir()); err != nil {
				return nil, err
			}
			return validSubmitResult("plan-in-tx"), nil
		})
		if err != nil || result.ResultKind != semanticidentity.ResultKindSubmitPlan {
			t.Fatalf("result = %#v, err=%v", result, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if wrapped.withTxCalls.Load() != 0 {
		t.Fatalf("RecordSuccessInTx began %d nested transactions", wrapped.withTxCalls.Load())
	}
	if callbacks.Load() != 1 {
		t.Fatalf("callbacks = %d", callbacks.Load())
	}
	if _, err := store.GetRepositoryTarget(ctx, "transaction-owned"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.GetMCPMutationResultOptional(ctx, storeKey(input.Key)); err != nil || !ok {
		t.Fatalf("mutation row = %v, %v", ok, err)
	}
}

func TestRecordSuccessInTxRechecksEqualAndConflictingWinners(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	service := mustService(t, store)
	request := validSubmitRequest("feature.plan.json")
	input := validSubmitInput(t, "mutation-winner", request)
	if _, _, err := service.RecordSuccess(ctx, input, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
		return validSubmitResult("plan-winner"), nil
	}); err != nil {
		t.Fatal(err)
	}

	var callbacks atomic.Int64
	err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := service.RecordSuccessInTx(ctx, tx, input, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			callbacks.Add(1)
			return validSubmitResult("duplicate"), nil
		})
		return err
	})
	if !IsConcurrentWinner(err) {
		t.Fatalf("equal winner error = %v", err)
	}
	result, replay, err := service.ResolveAfterRollback(ctx, input, err)
	if err != nil || !replay || result.ResultKind != semanticidentity.ResultKindSubmitPlan {
		t.Fatalf("equal winner recovery = %#v, %v, %v", result, replay, err)
	}

	conflict := validSubmitInput(t, "mutation-winner", validSubmitRequest("other.plan.json"))
	err = store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := service.RecordSuccessInTx(ctx, tx, conflict, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			callbacks.Add(1)
			return validSubmitResult("conflict"), nil
		})
		return err
	})
	if !HasCode(err, ErrorMutationConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	if callbacks.Load() != 0 {
		t.Fatalf("winner callbacks = %d", callbacks.Load())
	}
}

func TestCommitArtifactBatchCommitsAndRollsBackDomainAndMutationTogether(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	artifactRoot := filepath.Join(dir, "artifacts")
	store, err := workflowstore.Open(filepath.Join(dir, "workflow.db"), artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := mustService(t, store)

	successInput := validSubmitInput(t, "mutation-artifact-success", validSubmitRequest("feature.plan.json"))
	batch, err := store.ArtifactStore().Begin("plans/plan-success")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("canonical_plan", "feature.plan.json", "application/json", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		_, err := service.RecordSuccessInTx(ctx, tx, successInput, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			if _, err := tx.CreateRepositoryTarget(ctx, "artifact-success", t.TempDir()); err != nil {
				return nil, err
			}
			return validSubmitResult("plan-artifact"), nil
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(artifactRoot, filepath.FromSlash(file.RelativePath))); err != nil {
		t.Fatalf("artifact not committed: %v", err)
	}
	if _, err := store.GetRepositoryTarget(ctx, "artifact-success"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.GetMCPMutationResultOptional(ctx, storeKey(successInput.Key)); err != nil || !ok {
		t.Fatalf("mutation row = %v, %v", ok, err)
	}

	rollbackInput := validSubmitInput(t, "mutation-artifact-rollback", validSubmitRequest("rollback.plan.json"))
	rollbackBatch, err := store.ArtifactStore().Begin("plans/plan-rollback")
	if err != nil {
		t.Fatal(err)
	}
	rollbackFile, err := rollbackBatch.Stage("canonical_plan", "rollback.plan.json", "application/json", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.CommitArtifactBatch(ctx, rollbackBatch, func(tx *workflowstore.Tx) error {
		_, err := service.RecordSuccessInTx(ctx, tx, rollbackInput, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			if _, err := tx.CreateRepositoryTarget(ctx, "artifact-rollback", t.TempDir()); err != nil {
				return nil, err
			}
			return semanticidentity.CreateRunResult{}, nil
		})
		return err
	})
	if !HasCode(err, ErrorInvalidResultIdentity) {
		t.Fatalf("rollback error = %v", err)
	}
	if _, err := store.GetRepositoryTarget(ctx, "artifact-rollback"); err == nil {
		t.Fatal("domain row survived coordinated rollback")
	}
	if _, ok, err := store.GetMCPMutationResultOptional(ctx, storeKey(rollbackInput.Key)); err != nil || ok {
		t.Fatalf("rollback mutation row = %v, %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(artifactRoot, filepath.FromSlash(rollbackFile.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("artifact survived coordinated rollback: %v", err)
	}
}

func TestResolveReplayConflictRestartAndResponseWriteRecovery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "workflow.db")
	artifactRoot := filepath.Join(dir, "artifacts")
	store, err := workflowstore.Open(dbPath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	service := mustService(t, store)
	input := validSubmitInput(t, "mutation-replay", validSubmitRequest("feature.plan.json"))

	resolution, err := service.Resolve(ctx, input.Key, input.Fingerprint)
	if err != nil || resolution.Kind != ResolutionMiss {
		t.Fatalf("initial resolution = %#v, %v", resolution, err)
	}
	var callbacks atomic.Int64
	stored, replay, err := service.RecordSuccess(ctx, input, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
		callbacks.Add(1)
		return validSubmitResult("plan-replay"), nil
	})
	if err != nil || replay || stored.ResultKind != semanticidentity.ResultKindSubmitPlan {
		t.Fatalf("record = %#v, %v, %v", stored, replay, err)
	}

	// A response serialization or socket write failure occurs after this commit.
	// The retry resolves before any preparation callback or acquisition can run.
	var preparationCalls atomic.Int64
	resolution, err = service.Resolve(ctx, input.Key, input.Fingerprint)
	if resolution.Kind == ResolutionMiss {
		preparationCalls.Add(1)
	}
	if err != nil || resolution.Kind != ResolutionReplay {
		t.Fatalf("replay = %#v, %v", resolution, err)
	}
	if callbacks.Load() != 1 || preparationCalls.Load() != 0 {
		t.Fatalf("callbacks=%d preparation=%d", callbacks.Load(), preparationCalls.Load())
	}

	conflict := validSubmitInput(t, "mutation-replay", validSubmitRequest("other.plan.json"))
	resolution, err = service.Resolve(ctx, conflict.Key, conflict.Fingerprint)
	if err != nil || resolution.Kind != ResolutionConflict || len(resolution.Result.ResultIdentityJSON) != 0 {
		t.Fatalf("conflict = %#v, %v", resolution, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = workflowstore.Open(dbPath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = mustService(t, store)
	resolution, err = service.Resolve(ctx, input.Key, input.Fingerprint)
	if err != nil || resolution.Kind != ResolutionReplay {
		t.Fatalf("restart replay = %#v, %v", resolution, err)
	}
	decoded, ok := resolution.Result.ResultIdentity.(semanticidentity.SubmitPlanResult)
	if !ok || decoded.PlanID != "plan-replay" {
		t.Fatalf("typed replay = %#v", resolution.Result.ResultIdentity)
	}
}

func TestServiceOwnedTransactionRollbackAndConcurrentWinnerBehavior(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	service := mustService(t, store)
	input := validSubmitInput(t, "mutation-rollback", validSubmitRequest("feature.plan.json"))

	_, _, err := service.RecordSuccess(ctx, input, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
		if _, err := tx.CreateRepositoryTarget(ctx, "rollback-target", t.TempDir()); err != nil {
			return nil, err
		}
		return semanticidentity.CreateRunResult{}, nil
	})
	if !HasCode(err, ErrorInvalidResultIdentity) {
		t.Fatalf("invalid result error = %v", err)
	}
	if _, err := store.GetRepositoryTarget(ctx, "rollback-target"); err == nil {
		t.Fatal("domain mutation survived result failure")
	}

	var callbacks atomic.Int64
	equalInput := validSubmitInput(t, "mutation-concurrent", validSubmitRequest("feature.plan.json"))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := service.RecordSuccess(ctx, equalInput, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
				callbacks.Add(1)
				return validSubmitResult("plan-concurrent"), nil
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if callbacks.Load() != 1 {
		t.Fatalf("equal callbacks = %d", callbacks.Load())
	}

	first := validSubmitInput(t, "mutation-conflict", validSubmitRequest("first.plan.json"))
	second := validSubmitInput(t, "mutation-conflict", validSubmitRequest("second.plan.json"))
	results := make(chan error, 2)
	callbacks.Store(0)
	for index, value := range []RecordSuccessInput{first, second} {
		index, value := index, value
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := service.RecordSuccess(ctx, value, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
				callbacks.Add(1)
				return validSubmitResult("plan-" + string(rune('a'+index))), nil
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var success, conflictCount int
	for err := range results {
		if err == nil {
			success++
		} else if HasCode(err, ErrorMutationConflict) {
			conflictCount++
		} else {
			t.Fatalf("unexpected conflict result: %v", err)
		}
	}
	if success != 1 || conflictCount != 1 || callbacks.Load() != 1 {
		t.Fatalf("success=%d conflict=%d callbacks=%d", success, conflictCount, callbacks.Load())
	}
}

func TestResolveAfterRollbackUsesCommittedWinner(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	service := mustService(t, store)
	input := validSubmitInput(t, "mutation-recover", validSubmitRequest("feature.plan.json"))
	if _, _, err := service.RecordSuccess(ctx, input, func(context.Context, *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
		return validSubmitResult("plan-recover"), nil
	}); err != nil {
		t.Fatal(err)
	}
	result, replay, err := service.ResolveAfterRollback(ctx, input, appError(ErrorConcurrentWinner))
	if err != nil || !replay || result.ResultKind != semanticidentity.ResultKindSubmitPlan {
		t.Fatalf("recover = %#v, %v, %v", result, replay, err)
	}
	conflict := input
	conflict.Fingerprint = mustFingerprint(t, validSubmitRequest("other.plan.json"))
	_, _, err = service.ResolveAfterRollback(ctx, conflict, appError(ErrorConcurrentWinner))
	if !HasCode(err, ErrorMutationConflict) {
		t.Fatalf("conflicting winner = %v", err)
	}
}

type corruptStore struct {
	row workflowstore.MCPMutationResult
}

func (s corruptStore) GetMCPMutationResultOptional(context.Context, workflowstore.MCPMutationKey) (workflowstore.MCPMutationResult, bool, error) {
	return s.row, true, nil
}

func (corruptStore) WithTx(context.Context, func(*workflowstore.Tx) error) error {
	return errors.New("not used")
}

func TestServiceRejectsKeyFingerprintAndStoredResultCorruption(t *testing.T) {
	input := validSubmitInput(t, "mutation-corrupt", validSubmitRequest("feature.plan.json"))
	service := mustService(t, corruptStore{})

	_, err := service.Resolve(context.Background(), MutationKey{SurfaceContractID: "unknown.v1", Tool: registry.MutationToolSubmitPlan, MutationID: "mutation-1"}, input.Fingerprint)
	if !HasCode(err, ErrorUnknownSurfaceContract) {
		t.Fatalf("unknown surface = %v", err)
	}
	_, err = service.Resolve(context.Background(), MutationKey{SurfaceContractID: "planner-authoring.v1", Tool: registry.MutationToolSubmitPlan, MutationID: "mutation-1"}, input.Fingerprint)
	if !HasCode(err, ErrorUnknownMutationTool) {
		t.Fatalf("unknown tool = %v", err)
	}
	_, err = service.Resolve(context.Background(), MutationKey{SurfaceContractID: "planner-plan.v1", Tool: registry.MutationToolSubmitPlan, MutationID: "bad space"}, input.Fingerprint)
	if !HasCode(err, ErrorInvalidMutationID) {
		t.Fatalf("invalid mutation id = %v", err)
	}
	_, err = service.Resolve(context.Background(), input.Key, semanticidentity.Fingerprint{})
	if !HasCode(err, ErrorInvalidSemanticIdentity) {
		t.Fatalf("forged fingerprint = %v", err)
	}

	manifest, _ := registry.SurfaceManifestSHA256("planner-plan.v1")
	validResult, _ := semanticidentity.EncodeResultIdentity("planner-plan.v1", registry.MutationToolSubmitPlan, validSubmitResult("plan-1"))
	row := workflowstore.MCPMutationResult{
		SurfaceContractID:       string(input.Key.SurfaceContractID),
		ToolName:                string(input.Key.Tool),
		MutationID:              input.Key.MutationID,
		SurfaceManifestSHA256:   manifest,
		SemanticIdentityVersion: input.Fingerprint.SemanticIdentityVersion(),
		SemanticRequestSHA256:   input.Fingerprint.SemanticRequestSHA256(),
		ResultKind:              string(validResult.Kind),
		ResultIdentityJSON:      string(validResult.JSON),
		ResultSHA256:            strings.Repeat("0", 64),
		CommittedAt:             "2026-07-16T00:00:00Z",
	}
	service = mustService(t, corruptStore{row: row})
	_, err = service.Resolve(context.Background(), input.Key, input.Fingerprint)
	if !HasCode(err, ErrorCorruptStoredResult) {
		t.Fatalf("bad digest = %v", err)
	}

	row.ResultSHA256 = validResult.SHA256
	row.ResultKind = string(semanticidentity.ResultKindCreateRun)
	service = mustService(t, corruptStore{row: row})
	_, err = service.Resolve(context.Background(), input.Key, input.Fingerprint)
	if !HasCode(err, ErrorCorruptStoredResult) {
		t.Fatalf("wrong kind = %v", err)
	}

	row.ResultKind = string(validResult.Kind)
	row.ResultIdentityJSON = `{"plan_id":"plan-1","artifact_body":"forbidden"}`
	row.ResultSHA256 = sha256Hex([]byte(row.ResultIdentityJSON))
	service = mustService(t, corruptStore{row: row})
	_, err = service.Resolve(context.Background(), input.Key, input.Fingerprint)
	if !HasCode(err, ErrorCorruptStoredResult) {
		t.Fatalf("forbidden body = %v", err)
	}

	row.ResultIdentityJSON = `{"value":"` + strings.Repeat("x", semanticidentity.MaxResultIdentityBytes) + `"}`
	row.ResultSHA256 = sha256Hex([]byte(row.ResultIdentityJSON))
	service = mustService(t, corruptStore{row: row})
	_, err = service.Resolve(context.Background(), input.Key, input.Fingerprint)
	if !HasCode(err, ErrorCorruptStoredResult) {
		t.Fatalf("oversized body = %v", err)
	}
}

func openStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func validSubmitInput(t *testing.T, mutationID string, request semanticidentity.SubmitPlan) RecordSuccessInput {
	t.Helper()
	manifest, ok := registry.SurfaceManifestSHA256(request.SurfaceContract)
	if !ok {
		t.Fatal("missing manifest")
	}
	return RecordSuccessInput{
		Key: MutationKey{
			SurfaceContractID: request.SurfaceContract,
			Tool:              registry.MutationToolSubmitPlan,
			MutationID:        mutationID,
		},
		SurfaceManifestSHA256: manifest,
		Fingerprint:           mustFingerprint(t, request),
	}
}

func validSubmitRequest(name string) semanticidentity.SubmitPlan {
	sha := strings.Repeat("a", 64)
	return semanticidentity.SubmitPlan{CanonicalArtifactMutation: semanticidentity.CanonicalArtifactMutation{
		SurfaceContract:  "planner-plan.v1",
		ExpectedPacketID: "packet-1",
		ArtifactName:     name,
		MediaType:        "application/json",
		ExpectedSHA256:   sha,
		SensitiveDataClearance: registry.SensitiveDataClearance{
			PolicyVersion: registry.SensitiveDataClearancePolicyVersion,
			SubjectSHA256: sha,
			Confirmed:     true,
		},
	}}
}

func validSubmitResult(planID string) semanticidentity.SubmitPlanResult {
	return semanticidentity.SubmitPlanResult{
		PlanID: planID, ArtifactID: "artifact-1", ArtifactSHA256: strings.Repeat("a", 64),
		ProjectID: "project-1", SubmissionID: "submission-1", WorkflowState: "active", Complete: true,
	}
}

func mustFingerprint(t *testing.T, identity semanticidentity.RequestIdentity) semanticidentity.Fingerprint {
	t.Helper()
	value, err := semanticidentity.BuildFingerprint(identity)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustService(t *testing.T, store Store) *Service {
	t.Helper()
	service, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
