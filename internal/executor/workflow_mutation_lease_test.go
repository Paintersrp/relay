package executor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	workflowpackages "relay/internal/app/packages"
	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/pipeline"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowMutationLeaseReleasesProvenPrelaunchFailure(t *testing.T) {
	fixture := newWorkflowFixture(t)
	fixture.service.adapterFactory = func(string) (ExecutorAdapter, error) {
		return nil, errors.New("adapter is unavailable before process launch")
	}
	if _, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "test-model",
	}); err == nil {
		t.Fatal("expected prelaunch failure")
	}
	if _, err := fixture.runs.GetActiveRunMutationLease(context.Background(), fixture.run.RunID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("active pre-mutation lease = %v", err)
	}
	leases, err := fixture.store.ListRepositoryBranchMutationLeases(context.Background(), fixture.run.RepoTarget, fixture.run.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].State != workflowstore.RepositoryBranchMutationLeaseStateReleased {
		t.Fatalf("prelaunch lease history = %#v", leases)
	}
}

func TestWorkflowMutationLeaseRetainsPostMutationFailure(t *testing.T) {
	fixture := newWorkflowFixture(t)
	dirty := false
	fixture.service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		if dirty {
			return workflowrepos.ExecutionPreflightResult{BlockerCode: "repository_dirty", BlockerText: "repository evidence is dirty"}
		}
		return workflowrepos.ExecutionPreflightResult{OK: true}
	}
	fixture.service.runner = func(_ context.Context, _ string, _ string, _ []string, _ string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
		identity := pipeline.ProcessIdentity{PID: 612, StartedAt: "post-mutation-failure", Platform: "linux"}
		if err := callbacks.OnProcessStarted(identity); err != nil {
			t.Fatal(err)
		}
		dirty = true
		now := time.Now()
		return pipeline.AgentCommandRunResult{
			ExitCode:            1,
			LaunchStarted:       true,
			LaunchDisposition:   pipeline.AgentLaunchOwned,
			ProcessIdentity:     identity,
			IdentityAvailable:   true,
			StartedAt:           now,
			FinishedAt:          now,
			TerminationVerified: true,
		}
	}

	if _, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "test-model",
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := fixture.runs.GetActiveRunMutationLease(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain || lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationFailed {
		t.Fatalf("post-mutation failure lease = %#v", lease)
	}
	if _, err := fixture.runs.ReleaseRunMutationLease(context.Background(), fixture.run.RunID, lease.LeaseID); !errors.Is(err, workflowruns.ErrMutationLeaseUncertain) {
		t.Fatalf("post-mutation failure released without reconciliation: %v", err)
	}
}

func TestWorkflowMutationLeaseRetainsCancellationUntilRepositoryReconciliation(t *testing.T) {
	fixture := newWorkflowFixture(t)
	clean := true
	fixture.service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		if clean {
			return workflowrepos.ExecutionPreflightResult{OK: true}
		}
		return workflowrepos.ExecutionPreflightResult{BlockerCode: "repository_dirty", BlockerText: "repository evidence is dirty"}
	}
	fixture.service.runner = func(_ context.Context, _ string, _ string, _ []string, _ string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
		identity := pipeline.ProcessIdentity{PID: 611, StartedAt: "lease-test", Platform: "linux"}
		if err := callbacks.OnProcessStarted(identity); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		return pipeline.AgentCommandRunResult{
			ExitCode:            1,
			LaunchStarted:       true,
			LaunchDisposition:   pipeline.AgentLaunchOwned,
			ProcessIdentity:     identity,
			IdentityAvailable:   true,
			StartedAt:           now,
			FinishedAt:          now,
			TerminationVerified: false,
		}
	}

	started, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := fixture.runs.GetActiveRunMutationLease(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain || lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationRequired {
		t.Fatalf("uncertain lease = %#v", lease)
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), started.Attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	var runtime workflowAttemptRuntime
	if err := json.Unmarshal([]byte(attempt.ResultJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.MutationLeaseID != lease.LeaseID || !runtime.SourceMutationStarted || !runtime.CleanupPending {
		t.Fatalf("durable lease runtime = %#v", runtime)
	}

	clean = false
	restarted := NewWorkflowExecutionService(fixture.store, nil, "lease-restart-test")
	restarted.preflight = fixture.service.preflight
	restarted.controller = absentProcessController{}
	if _, err := restarted.Cancel(context.Background(), fixture.run.RunID, attempt.AttemptID); err != nil {
		t.Fatal(err)
	}
	retained, err := fixture.runs.GetActiveRunMutationLease(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationFailed || retained.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyUncertain {
		t.Fatalf("cancelled uncertain lease was released or not marked failed: %#v", retained)
	}

	clean = true
	reconciled, err := restarted.ReconcileMutationLease(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled.Released || reconciled.Lease != nil || !reconciled.Preflight.OK {
		t.Fatalf("reconciled mutation lease = %#v", reconciled)
	}
	if _, err := fixture.runs.GetActiveRunMutationLease(context.Background(), fixture.run.RunID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("lease remained active after clean reconciliation: %v", err)
	}
}

func TestWorkflowMutationLeaseSerializesPackageAndLegacyRuns(t *testing.T) {
	ctx := context.Background()
	store, packageRun, _ := newPackageLinkedMutationLeaseFixture(t, ctx)
	runs, err := workflowruns.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := runs.CreateRun(ctx, workflowruns.CreateRunInput{
		FeatureSlug:      "legacy-lease-feature",
		RepoTarget:       "relay",
		Branch:           "main",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    mutationLeaseModelExecutionSpec("legacy-lease-feature"),
		RenderedMarkdown: []byte("# Legacy Executor Brief\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !packageRun.ExecutionPackageRowID.Valid || legacy.Run.ExecutionPackageRowID.Valid {
		t.Fatalf("package and legacy Run linkage = %#v / %#v", packageRun.ExecutionPackageRowID, legacy.Run.ExecutionPackageRowID)
	}

	service := NewWorkflowExecutionService(store, nil, "lease-cross-run-test")
	adapter := &captureAdapter{id: AdapterOpenCodeGo}
	service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		return workflowrepos.ExecutionPreflightResult{OK: true}
	}
	service.adapterFactory = func(string) (ExecutorAdapter, error) { return adapter, nil }
	service.invocationPreflight = func(ExecutorInvocation) ExecutorPreflightResult { return ExecutorPreflightResult{OK: true} }
	started := make(chan struct{})
	firstFinished := make(chan struct{})
	var launches atomic.Int32
	service.launch = func(fn func()) {
		if launches.Add(1) == 1 {
			go func() {
				defer close(firstFinished)
				fn()
			}()
			return
		}
		fn()
	}
	service.runner = func(ctx context.Context, _ string, _ string, _ []string, _ string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
		identity := pipeline.ProcessIdentity{PID: 712, StartedAt: "package-lease", Platform: "linux"}
		if err := callbacks.OnProcessStarted(identity); err != nil {
			t.Fatal(err)
		}
		if launches.Load() == 1 {
			close(started)
			<-ctx.Done()
			return pipeline.AgentCommandRunResult{ExitCode: -1, Error: ctx.Err().Error(), LaunchStarted: true, LaunchDisposition: pipeline.AgentLaunchOwned, ProcessIdentity: identity, IdentityAvailable: true, StartedAt: time.Now(), FinishedAt: time.Now(), TerminationVerified: true}
		}
		output := "STATUS: DONE\n"
		if callbacks.OnStdout != nil {
			callbacks.OnStdout([]byte(output))
		}
		return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: output, LaunchStarted: true, LaunchDisposition: pipeline.AgentLaunchOwned, ProcessIdentity: identity, IdentityAvailable: true, StartedAt: time.Now(), FinishedAt: time.Now(), TerminationVerified: true}
	}

	packageStarted, err := service.Start(ctx, WorkflowStartInput{RunID: packageRun.RunID, Adapter: "opencode_go", Model: "package-model"})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := service.Start(ctx, WorkflowStartInput{RunID: legacy.Run.RunID, Adapter: "opencode_go", Model: "legacy-model"}); !errors.Is(err, workflowruns.ErrMutationLeaseConflict) {
		t.Fatalf("legacy Run did not conflict with package-linked Run: %v", err)
	}
	if _, err := service.Cancel(ctx, packageRun.RunID, packageStarted.Attempt.AttemptID); err != nil {
		t.Fatal(err)
	}
	<-firstFinished
	if _, err := service.Start(ctx, WorkflowStartInput{RunID: legacy.Run.RunID, Adapter: "opencode_go", Model: "legacy-model"}); err != nil {
		t.Fatalf("legacy Run remained blocked after package-linked Run released its lease: %v", err)
	}
}

func newPackageLinkedMutationLeaseFixture(t *testing.T, ctx context.Context) (*workflowstore.Store, workflowstore.Run, []byte) {
	t.Helper()
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "source.txt"), []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	const (
		commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		tree   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	var selection workflowstore.DeliveryTicketSelection
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
			RepoTarget: "relay", LocalPath: repoPath, ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		}); err != nil {
			return err
		}
		vault, err := tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{VaultID: "vault-package-lease", RepoTarget: "relay", RelativePath: "repositories/relay.git"})
		if err != nil {
			return err
		}
		closure, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: "closure-package-lease", CommitOID: commit, TreeOID: tree,
			RefName: "refs/relay/closures/closure-package-lease", StartedAt: "2026-07-18T00:00:00.000000000Z",
		})
		if err != nil {
			return err
		}
		ready, err := tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: closure.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting,
			NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: "2026-07-18T00:00:01.000000000Z",
		})
		if err != nil {
			return err
		}
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: "project-package-lease", Name: "Package lease"})
		if err != nil {
			return err
		}
		workspace, err := tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: "workspace-package-lease", ProjectRowID: project.ID, FeatureSlug: "package-lease-feature"})
		if err != nil {
			return err
		}
		authority, err := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: "authority-package-lease", WorkspaceRowID: workspace.ID, RevisionNumber: 1,
			SourceClosureRowID: sql.NullInt64{Int64: ready.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		if _, err := tx.SetFeatureWorkspaceAuthorityRevision(ctx, authority.ID, workspace.WorkspaceID, workspace.Version); err != nil {
			return err
		}
		ticket, err := tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "P5-T3", WorkspaceRowID: workspace.ID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		revision, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "relay", Branch: "main", BaseCommit: commit,
			SourceClosureRowID: ready.ID, SourcePath: "tickets/P5-T3.delivery-ticket.json", Goal: "Serialize package and legacy execution.",
			Context: "Exercise the shared mutation lease.", TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		if _, err := tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revision.ID); err != nil {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID: "approval-package-lease", RevisionRowID: revision.ID, ApprovalKind: "delivery", ApprovalState: "approved",
			Rationale: "Approve exact package lease ticket.", SourceClosureRowID: ready.ID,
			AuthorityRevisionRowID: sql.NullInt64{Int64: authority.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selection, err = tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-package-lease", WorkspaceRowID: workspace.ID, State: "active",
			Rationale: "Select package lease ticket.", SourceClosureRowID: sql.NullInt64{Int64: ready.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: revision.ID, ApprovalRowID: approval.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	canonical := mutationLeaseModelExecutionSpec("package-lease-feature")
	brief := []byte("# Ticket Design Brief\n\n## Ticket Identity\n\n## Context\n\n## Design\n\n## Implementation Notes\n\n## Validation\n")
	packages, err := workflowpackages.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := packages.Prepare(ctx, workflowpackages.PrepareInput{
		SelectionID: selection.SelectionID,
		TicketDesignBriefs: []workflowpackages.ArtifactInput{{
			DisplayName: "package-lease-feature.ticket-P5-T3.r1.design-brief.md", ExpectedSHA256: mutationLeaseSHA256(brief), Bytes: brief,
		}},
		ExecutionSpec: workflowpackages.ArtifactInput{DisplayName: "package-lease-feature.execution-spec.json", ExpectedSHA256: mutationLeaseSHA256(canonical), Bytes: canonical},
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := packages.Approve(ctx, workflowpackages.ApproveInput{PackageID: prepared.Package.PackageID})
	if err != nil {
		t.Fatal(err)
	}
	return store, approved.Run, canonical
}

func mutationLeaseModelExecutionSpec(featureSlug string) []byte {
	return []byte(fmt.Sprintf(`{
  "schema_version": "2.0",
  "feature_slug": %q,
  "repo_target": "relay",
  "branch": "main",
  "base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "goal": "Exercise a model-owned lease fixture.",
  "context": "The source mutation must be serialized across legacy and package-linked Runs.",
  "scope": {"in_scope": ["Run one model-owned rename."], "out_of_scope": ["No unrelated behavior."]},
  "steps": [{"number": 1, "goal": "Model-owned rename.", "substeps": [{"number": 1, "instruction": "Rename through the model path.", "files": [{"path": "source.txt", "destination_path": "target.txt", "operation": "rename", "purpose": "Keep deterministic work empty.", "implementation": {"content": "target\n"}}], "completion_criteria": ["Model work is complete."]}], "completion_criteria": ["The model mutation is complete."]}],
  "validation": {"commands": [{"command": "go test ./internal/executor", "expected": "The focused executor tests pass."}]},
  "completion_criteria": ["The model mutation completes."]
}
`, featureSlug))
}

func mutationLeaseSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
