package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestBeginAdaptiveDispatchAdmission(t *testing.T) {
	ctx := context.Background()
	fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	prepared, err := newAdaptiveAttemptService(t, fixture).Prepare(ctx, AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAdaptiveDispatchAdmissionService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Begin(ctx, AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
	if err != nil {
		t.Fatal(err)
	}
	if !first.NewlyAdmitted || first.Run == nil || first.Run.Status != workflowstore.RunStatusExecuting || first.Attempt == nil || first.Attempt.Status != workflowstore.AttemptStatusRunning || first.Lease == nil || first.Lease.State != workflowstore.RepositoryBranchMutationLeaseStateActive {
		t.Fatalf("first admission = %#v", first)
	}
	var runtime adaptiveDispatchRuntime
	if err := json.Unmarshal([]byte(first.Attempt.ResultJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.MutationLeaseID != first.Lease.LeaseID || runtime.SourceMutationStarted || runtime.EffectiveBriefArtifactID != first.EffectiveBriefArtifact.ArtifactID || runtime.EffectiveBriefSHA256 != first.EffectiveBriefArtifact.SHA256 || runtime.EffectiveBriefMode != first.Mode {
		t.Fatalf("runtime = %#v", runtime)
	}
	second, err := service.Begin(ctx, AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
	if err != nil {
		t.Fatal(err)
	}
	if second.NewlyAdmitted || second.Lease == nil || second.Lease.LeaseID != first.Lease.LeaseID {
		t.Fatalf("second admission = %#v", second)
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(ctx, fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil || len(leases) != 1 {
		t.Fatalf("leases = %#v err=%v", leases, err)
	}
}

func TestBeginAdaptiveDispatchAdmissionModes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		operations bool
		coverage   string
		outcome    DeterministicOutcomeInput
		mode       EffectiveExecutorBriefMode
	}{
		{name: "operations absent", outcome: DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}}, mode: EffectiveExecutorBriefAdaptiveNoOperations},
		{name: "partial preflight failure", operations: true, coverage: "partial", outcome: failedOutcomeInput("partial"), mode: EffectiveExecutorBriefAdaptivePreflightFailed},
		{name: "complete preflight failure", operations: true, coverage: "complete", outcome: failedOutcomeInput("complete"), mode: EffectiveExecutorBriefAdaptivePreflightFailed},
		{name: "partial deterministic application", operations: true, coverage: "partial", outcome: appliedOutcomeInput("partial"), mode: EffectiveExecutorBriefAdaptiveAfterPartialApplication},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := adaptiveAttemptFixture(t, tc.operations, tc.coverage, tc.outcome)
			prepared, err := newAdaptiveAttemptService(t, fixture).Prepare(ctx, AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewAdaptiveDispatchAdmissionService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			if tc.mode == EffectiveExecutorBriefAdaptiveAfterPartialApplication {
				seedAdaptivePartialLease(t, fixture, "lease-admission-partial")
			}
			result, err := service.Begin(ctx, AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
			if err != nil {
				t.Fatal(err)
			}
			if !result.AdaptiveDispatchRequired || !result.NewlyAdmitted || result.Mode != tc.mode || result.Run == nil || result.Run.ID != fixture.run.ID || result.Attempt == nil || result.Attempt.ID != prepared.Attempt.ID || result.Lease == nil || result.EffectiveBriefArtifact == nil || result.InputArtifact == nil {
				t.Fatalf("admission = %#v", result)
			}
			if result.EffectiveBriefArtifact.ArtifactID == "" || len(result.EffectiveBriefBytes) == 0 || len(result.InputBytes) == 0 {
				t.Fatalf("admission omitted verified artifacts: %#v", result)
			}
			var runtime adaptiveDispatchRuntime
			if err := json.Unmarshal([]byte(result.Attempt.ResultJSON), &runtime); err != nil {
				t.Fatal(err)
			}
			expectedMutationStarted, valid := adaptiveSourceMutationStarted(tc.mode)
			if !valid || runtime.MutationLeaseID != result.Lease.LeaseID || runtime.EffectiveBriefArtifactID != result.EffectiveBriefArtifact.ArtifactID || runtime.EffectiveBriefSHA256 != result.EffectiveBriefArtifact.SHA256 || runtime.EffectiveBriefMode != tc.mode || runtime.SourceMutationStarted != expectedMutationStarted {
				t.Fatalf("runtime = %#v", runtime)
			}
			if tc.mode == EffectiveExecutorBriefAdaptiveAfterPartialApplication && result.Lease.LeaseID != "lease-admission-partial" {
				t.Fatalf("partial admission replaced lease: %#v", result.Lease)
			}
		})
	}
}

func seedAdaptivePartialLease(t *testing.T, fixture *executionAssignmentFixture, leaseID string) {
	t.Helper()
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryBranchMutationLease(context.Background(), workflowstore.CreateRepositoryBranchMutationLeaseParams{
			LeaseID: leaseID, RepoTarget: fixture.run.RepoTarget, Branch: fixture.run.Branch,
			OwnerKind: "run_execution", OwnerIdentity: fixture.run.RunID,
			UncertaintyState:    workflowstore.RepositoryBranchMutationLeaseCertaintyCertain,
			ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBeginAdaptiveDispatchAdmissionCompleteAndConcurrent(t *testing.T) {
	ctx := context.Background()
	complete := adaptiveAttemptFixture(t, true, "complete", appliedOutcomeInput("complete"))
	service, err := NewAdaptiveDispatchAdmissionService(complete.store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Begin(ctx, AdaptiveDispatchAdmissionInput{RunID: complete.run.RunID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != EffectiveExecutorBriefDeterministicComplete || result.AdaptiveDispatchRequired || result.NewlyAdmitted || result.Run != nil || result.Attempt != nil || result.Lease != nil || result.EffectiveBriefArtifact != nil || result.InputArtifact != nil || len(result.EffectiveBriefBytes) != 0 || len(result.InputBytes) != 0 {
		t.Fatalf("complete result = %#v", result)
	}

	fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	prepared, err := newAdaptiveAttemptService(t, fixture).Prepare(ctx, AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := NewAdaptiveDispatchAdmissionService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan AdaptiveDispatchAdmissionResult, 2)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := parallel.Begin(ctx, AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID})
			results <- result
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errs)
	admitted := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.Run == nil || result.Attempt == nil || result.Lease == nil || result.Attempt.ID != prepared.Attempt.ID {
			t.Fatalf("concurrent result = %#v", result)
		}
		if result.NewlyAdmitted {
			admitted++
		}
		if result.Lease.LeaseID == "" {
			t.Fatal("concurrent result omitted lease identity")
		}
	}
	if admitted != 1 {
		t.Fatalf("new admissions = %d", admitted)
	}
	attempts, err := fixture.store.ListExecutionAttemptsByRun(ctx, fixture.run.ID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != workflowstore.AttemptStatusRunning {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
	run, err := fixture.store.GetRunByRunID(ctx, fixture.run.RunID)
	if err != nil || run.Status != workflowstore.RunStatusExecuting {
		t.Fatalf("Run = %#v err=%v", run, err)
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(ctx, fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil || len(leases) != 1 {
		t.Fatalf("leases = %#v err=%v", leases, err)
	}
}

func TestBeginAdaptiveDispatchAdmissionCompleteRejectsDurableDispatchState(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func(t *testing.T, fixture *executionAssignmentFixture)
	}{
		{name: "pre-existing attempt", seed: func(t *testing.T, f *executionAssignmentFixture) {
			t.Helper()
			if err := f.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
				_, err := tx.CreateExecutionAttempt(context.Background(), workflowstore.CreateExecutionAttemptParams{AttemptID: "attempt-complete-conflict", RunRowID: f.run.ID, AttemptNumber: 1, Adapter: "codex", Model: "model"})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "active Run lease", seed: func(t *testing.T, f *executionAssignmentFixture) {
			t.Helper()
			if err := f.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
				_, err := tx.CreateRepositoryBranchMutationLease(context.Background(), workflowstore.CreateRepositoryBranchMutationLeaseParams{LeaseID: "lease-complete-conflict", RepoTarget: f.run.RepoTarget, Branch: f.run.Branch, OwnerKind: "run_execution", OwnerIdentity: f.run.RunID, UncertaintyState: workflowstore.RepositoryBranchMutationLeaseCertaintyCertain, ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := adaptiveAttemptFixture(t, true, "complete", appliedOutcomeInput("complete"))
			tc.seed(t, fixture)
			service, err := NewAdaptiveDispatchAdmissionService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Begin(context.Background(), AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID}); !errors.Is(err, ErrAdaptiveDispatchAdmissionConflict) {
				t.Fatalf("complete admission error = %v", err)
			}
			run, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
			if err != nil || run.Status != workflowstore.RunStatusSetupReady {
				t.Fatalf("Run = %#v err=%v", run, err)
			}
		})
	}
}

func TestBeginAdaptiveDispatchAdmissionRejectsWrongAttempt(t *testing.T) {
	fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	prepared, err := newAdaptiveAttemptService(t, fixture).Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAdaptiveDispatchAdmissionService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(context.Background(), AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID + "-wrong"}); !errors.Is(err, ErrAdaptiveDispatchAdmissionConflict) {
		t.Fatalf("wrong attempt error = %v", err)
	}
}

func TestBeginAdaptiveDispatchAdmissionRejectsTamperingAndLeaseConflicts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, fixture *executionAssignmentFixture, prepared AdaptiveExecutionAttemptResult, service *AdaptiveDispatchAdmissionService)
	}{
		{name: "adaptive input bytes changed", mutate: func(t *testing.T, f *executionAssignmentFixture, prepared AdaptiveExecutionAttemptResult, _ *AdaptiveDispatchAdmissionService) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(f.store.ArtifactStore().Root(), filepath.FromSlash(prepared.InputArtifact.RelativePath)), []byte("tampered"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "effective Brief bytes changed", mutate: func(t *testing.T, f *executionAssignmentFixture, _ AdaptiveExecutionAttemptResult, service *AdaptiveDispatchAdmissionService) {
			t.Helper()
			brief, err := service.briefs.Load(context.Background(), f.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.store.ArtifactStore().Root(), filepath.FromSlash(brief.Artifact.RelativePath)), []byte("tampered"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "attempt becomes terminal", mutate: func(t *testing.T, f *executionAssignmentFixture, prepared AdaptiveExecutionAttemptResult, _ *AdaptiveDispatchAdmissionService) {
			t.Helper()
			if _, err := f.store.DB().Exec(`UPDATE execution_attempts SET status = 'failed', started_at = '2000-01-01T00:00:00Z', finished_at = '2000-01-01T00:00:01Z' WHERE id = ?`, prepared.Attempt.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "runtime lease differs", mutate: func(t *testing.T, f *executionAssignmentFixture, prepared AdaptiveExecutionAttemptResult, service *AdaptiveDispatchAdmissionService) {
			t.Helper()
			first, err := service.Begin(context.Background(), AdaptiveDispatchAdmissionInput{RunID: f.run.RunID, AttemptID: prepared.Attempt.AttemptID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.DB().Exec(`UPDATE execution_attempts SET result_json = ? WHERE id = ?`, `{"mutation_lease_id":"lease-different","source_mutation_started":false,"effective_brief_artifact_id":"`+first.EffectiveBriefArtifact.ArtifactID+`","effective_brief_sha256":"`+first.EffectiveBriefArtifact.SHA256+`","effective_brief_mode":"`+string(first.Mode)+`"}`, first.Attempt.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "lease becomes uncertain", mutate: func(t *testing.T, f *executionAssignmentFixture, prepared AdaptiveExecutionAttemptResult, service *AdaptiveDispatchAdmissionService) {
			t.Helper()
			first, err := service.Begin(context.Background(), AdaptiveDispatchAdmissionInput{RunID: f.run.RunID, AttemptID: prepared.Attempt.AttemptID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.DB().Exec(`UPDATE repository_branch_mutation_leases SET uncertainty_state = 'uncertain', uncertainty_reason = 'test', reconciliation_state = 'required' WHERE lease_id = ?`, first.Lease.LeaseID); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
			prepared, err := newAdaptiveAttemptService(t, fixture).Prepare(context.Background(), AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewAdaptiveDispatchAdmissionService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, fixture, prepared, service)
			if _, err := service.Begin(context.Background(), AdaptiveDispatchAdmissionInput{RunID: fixture.run.RunID, AttemptID: prepared.Attempt.AttemptID}); !errors.Is(err, ErrAdaptiveDispatchAdmissionConflict) {
				t.Fatalf("admission error = %v", err)
			}
		})
	}
}

func TestLoadAdaptiveExecutionAttempt(t *testing.T) {
	ctx := context.Background()
	fixture := adaptiveAttemptFixture(t, false, "", DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}})
	prepared, err := newAdaptiveAttemptService(t, fixture).Prepare(ctx, AdaptiveExecutionAttemptInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	loader := newAdaptiveAttemptService(t, fixture)
	loaded, err := loader.Load(ctx, fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Attempt == nil || loaded.InputArtifact == nil || loaded.Attempt.ID != prepared.Attempt.ID || loaded.InputArtifact.ID != prepared.InputArtifact.ID {
		t.Fatalf("loaded attempt = %#v", loaded)
	}
	loaded.InputBytes[0] = '!'
	reloaded, err := loader.Load(ctx, fixture.run.RunID)
	if err != nil || reloaded.InputBytes[0] == '!' {
		t.Fatalf("reloaded = %#v err=%v", reloaded, err)
	}
}
