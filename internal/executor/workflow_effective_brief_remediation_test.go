package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"relay/internal/applier"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

type tamperingEffectiveBriefAdapter struct{}

func (tamperingEffectiveBriefAdapter) ID() AdapterID { return AdapterOpenCodeGo }

func (tamperingEffectiveBriefAdapter) BuildInvocation(request ExecutorAdapterRequest) (ExecutorInvocation, error) {
	tampered := request.BriefContent + "tampered"
	return ExecutorInvocation{
		Adapter:     AdapterOpenCodeGo,
		Binary:      "fake-agent",
		WorkDir:     request.RepoPath,
		Stdin:       tampered,
		StdinSource: request.BriefPath,
		StdinBytes:  len([]byte(tampered)),
		Model:       request.SelectedModel,
		Agent:       string(AdapterOpenCodeGo),
		Preview:     "fake-agent < " + request.BriefPath,
	}, nil
}

func (tamperingEffectiveBriefAdapter) NormalizeResult(string) NormalizedExecutorResult {
	return NormalizedExecutorResult{}
}

func TestVerifyInvocationUsesEffectiveBriefAdapterTransports(t *testing.T) {
	selected := testEffectiveBriefInput(t)
	for _, adapter := range []AdapterID{AdapterOpenCodeGo, AdapterCodex, AdapterKiroCLI} {
		t.Run(string(adapter), func(t *testing.T) {
			invocation := ExecutorInvocation{
				Adapter:     adapter,
				Stdin:       string(selected.Content),
				StdinSource: selected.Path,
				StdinBytes:  len(selected.Content),
			}
			if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err != nil {
				t.Fatal(err)
			}
		})
	}

	antigravity := &AntigravityAdapter{Config: AntigravityAdapterConfig{Binary: "antigravity", ApproveFlag: "none"}}
	invocation, err := antigravity.BuildInvocation(ExecutorAdapterRequest{
		RepoPath:     t.TempDir(),
		BriefContent: string(selected.Content),
		BriefPath:    selected.Path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err != nil {
		t.Fatal(err)
	}
	invocation.StdinSource = filepath.Join(t.TempDir(), "alternate-brief.md")
	if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err == nil {
		t.Fatal("expected alternate path-based stdin source to be rejected")
	}
}

func TestWorkflowHybridPrelaunchFailuresPreserveEffectiveBriefIdentity(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*workflowFixture)
	}{
		{
			name: "adapter construction",
			configure: func(fixture *workflowFixture) {
				fixture.service.adapterFactory = func(string) (ExecutorAdapter, error) {
					return nil, errors.New("adapter unavailable")
				}
			},
		},
		{
			name: "invocation verification",
			configure: func(fixture *workflowFixture) {
				fixture.service.adapterFactory = func(string) (ExecutorAdapter, error) {
					return tamperingEffectiveBriefAdapter{}, nil
				}
			},
		},
		{
			name: "adapter preflight",
			configure: func(fixture *workflowFixture) {
				fixture.service.invocationPreflight = func(ExecutorInvocation) ExecutorPreflightResult {
					return ExecutorPreflightResult{OK: false, BlockerText: "preflight blocked"}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			run := createRunWithCanonicalProjectionSpec(t, fixture, "hybrid-prelaunch-identity")
			fixture.service.applier = hybridPartialApplier
			launched := false
			fixture.service.launch = func(func()) { launched = true }
			tt.configure(fixture)

			_, err := fixture.service.Start(context.Background(), WorkflowStartInput{
				RunID:   run.RunID,
				Adapter: "opencode_go",
				Model:   "attempt-model",
			})
			if err == nil {
				t.Fatal("expected hybrid prelaunch failure")
			}
			if launched {
				t.Fatal("executor process was launched")
			}
			attempts, err := fixture.store.ListExecutionAttemptsByRun(context.Background(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 {
				t.Fatalf("attempts = %d, want 1", len(attempts))
			}
			attempt := attempts[0]
			if attempt.Status != workflowstore.AttemptStatusFailed {
				t.Fatalf("attempt status = %q", attempt.Status)
			}
			artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			var residual workflowstore.Artifact
			for _, artifact := range artifacts {
				if artifact.Kind == "executor_residual_brief" {
					residual = artifact
				}
			}
			if residual.ArtifactID == "" {
				t.Fatal("residual effective brief artifact is missing")
			}
			var state workflowAttemptRuntime
			if err := json.Unmarshal([]byte(attempt.ResultJSON), &state); err != nil {
				t.Fatal(err)
			}
			if state.EffectiveBriefMode != string(speccompiler.EffectiveBriefResidual) || state.EffectiveBriefArtifactID != residual.ArtifactID || state.EffectiveBriefSHA256 != residual.SHA256 {
				t.Fatalf("effective identity = %+v, residual = %+v", state, residual)
			}
			if state.ProcessIdentity != "" || !state.TerminationVerified {
				t.Fatalf("prelaunch process state = %+v", state)
			}
		})
	}
}

func testEffectiveBriefInput(t *testing.T) effectiveBriefInput {
	t.Helper()
	content := []byte("# Executor Brief\n\nUse the selected effective input.\n")
	digest := sha256.Sum256(content)
	return effectiveBriefInput{
		Mode:    speccompiler.EffectiveBriefResidual,
		Content: content,
		Artifact: workflowstore.Artifact{
			ArtifactID: "artifact-effective-brief",
			SHA256:     hex.EncodeToString(digest[:]),
			SizeBytes:  int64(len(content)),
		},
		Path: filepath.Join(t.TempDir(), "executor-residual-brief.md"),
	}
}

func hybridPartialApplier(context.Context, applier.Input) (applier.Result, error) {
	return applier.Result{
		Outcome:      applier.OutcomePartial,
		ActorKind:    applier.ActorKindApplier,
		ChangedFiles: []string{"deterministic.txt"},
		Partition: applier.Partition{
			DeterministicPathChains: []string{"chain.1.1.file.1"},
			ResidualPathChains:      []string{"chain.1.2.file.1"},
			DeterministicFileWork:   []string{"1.1.file.1"},
			ResidualFileWork:        []string{"1.2.file.1"},
			ProtectedPaths:          []string{"deterministic.txt"},
			CoveredFileWork:         []string{"1.1.file.1", "1.2.file.1"},
		},
		Ledger: applier.Ledger{Entries: []applier.LedgerEntry{
			{PathChainRef: "chain.1.1.file.1", FileWorkRefs: []string{"1.1.file.1"}, Disposition: applier.DispositionDeterministic, Outcome: applier.OperationApplied, ChangedPaths: []string{"deterministic.txt"}},
			{PathChainRef: "chain.1.2.file.1", FileWorkRefs: []string{"1.2.file.1"}, Disposition: applier.DispositionResidual, Outcome: applier.OperationResidual},
		}},
		ImplementationResult: applier.ImplementationResult{
			Outcome:               applier.OutcomePartial,
			ActorKind:             applier.ActorKindApplier,
			CompletedPathChains:   []string{"chain.1.1.file.1"},
			ResidualPathChains:    []string{"chain.1.2.file.1"},
			CompletedFileWork:     []string{"1.1.file.1"},
			ResidualFileWork:      []string{"1.2.file.1"},
			ChangedFiles:          []string{"deterministic.txt"},
			ProtectedPaths:        []string{"deterministic.txt"},
			ModelExecutorRequired: true,
		},
	}, nil
}

func TestWorkflowHybridEvidencePreservesResidualIdentityAndValidation(t *testing.T) {
	fixture := newWorkflowFixture(t)
	run := createRunWithCanonicalProjectionSpec(t, fixture, "hybrid-evidence")
	fixture.service.applier = hybridPartialApplier
	fixture.service.runner = successfulRunner

	result, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID:   run.RunID,
		Adapter: "opencode_go",
		Model:   "attempt-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), result.Attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	var residual workflowstore.Artifact
	var evidenceArtifact workflowstore.Artifact
	for _, artifact := range artifacts {
		switch artifact.Kind {
		case "executor_residual_brief":
			residual = artifact
		case "execution_evidence":
			evidenceArtifact = artifact
		}
	}
	if residual.ArtifactID == "" || evidenceArtifact.ArtifactID == "" {
		t.Fatalf("attempt artifacts = %+v", artifacts)
	}
	data, err := os.ReadFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(evidenceArtifact.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	var evidence workflowExecutionEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.EffectiveBriefMode != "residual" || evidence.EffectiveBriefArtifactID != residual.ArtifactID || evidence.EffectiveBriefSHA256 != residual.SHA256 {
		t.Fatalf("execution evidence identity = %+v, residual = %+v", evidence, residual)
	}
	if len(evidence.ValidationResults) != 1 || evidence.ValidationResults[0].Command != "go test ./internal/executor" || evidence.ValidationResults[0].Status != workflowValidationPassed {
		t.Fatalf("validation evidence = %+v", evidence.ValidationResults)
	}
}
