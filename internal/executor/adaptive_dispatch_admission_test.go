package executor

import (
	"context"
	"encoding/json"
	"errors"
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
		if result.NewlyAdmitted {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("new admissions = %d", admitted)
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
