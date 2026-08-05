package features

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestPrototypeExecutionRequiresExactApprovalAndNeverLaunches(t *testing.T) {
	ctx, store, service, workspace, ticket, _ := prototypeFixture(t)
	productionCounts := map[string]int{}
	for _, table := range []string{"execution_packages", "runs", "execution_attempts", "repository_branch_mutation_leases", "audit_decisions", "audit_remediation_seeds", "feature_workspace_completion_decisions"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		productionCounts[table] = count
	}
	proposalBytes := []byte("prototype proposal exact bytes\n")
	proposal, err := service.PreparePrototypeProposal(ctx, PreparePrototypeProposalInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, Proposal: proposalBytes, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	authorization, run, err := service.PreparePrototypeExecution(ctx, PreparePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ProposalID: proposal.ProposalID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, SourceClosureID: "closure-prototype", RepoTarget: "prototype-relay", BaseCommit: strings.Repeat("a", 40), Adapter: "adapter", Model: "model", Variants: []string{"baseline"}, EvidenceObligations: []string{"result-envelope"}, Limits: map[string]any{"seconds": 60}})
	if err != nil {
		t.Fatalf("prepare execution: %v; workspace=%q workItem=%q proposal=%q versions=%d/%d", err, workspace.WorkspaceID, ticket.DiscoveryTicketID, proposal.ProposalID, workspace.Version, ticket.Version)
	}
	if run.LifecycleState != "proposed" || run.Version != 1 {
		t.Fatalf("new run = %#v", run)
	}
	before, err := service.ReadPrototypeExecution(ctx, workspace.WorkspaceID, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Approval != nil || before.LaunchClaim != nil || before.Run.ExternalProcessIdentity.Valid || before.Run.LifecycleState != "proposed" {
		t.Fatalf("unapproved read = %#v", before)
	}
	if _, _, err := service.ApprovePrototypeExecution(ctx, ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: "approve-one", OperatorConfirmationEvidence: "confirmed", ExpectedRunVersion: run.Version + 1}); !errors.Is(err, ErrPrototypeRunStale) {
		t.Fatalf("stale approval = %v", err)
	}
	still, err := service.ReadPrototypeExecution(ctx, workspace.WorkspaceID, run.PrototypeRunID)
	if err != nil || still.Run.LifecycleState != "proposed" || still.Approval != nil {
		t.Fatalf("stale approval mutated = %#v, %v", still, err)
	}
	approval, approved, err := service.ApprovePrototypeExecution(ctx, ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: "approve-one", OperatorConfirmationEvidence: "confirmed", ExpectedRunVersion: run.Version})
	if err != nil || approval.RunRowID != run.ID || approved.LifecycleState != "approved" || approved.Version != 2 {
		t.Fatalf("approval = %#v %#v %v", approval, approved, err)
	}
	replayed, replayRun, err := service.ApprovePrototypeExecution(ctx, ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: "approve-one", OperatorConfirmationEvidence: "confirmed", ExpectedRunVersion: run.Version})
	if err != nil || replayed.ID != approval.ID || replayRun.Version != approved.Version {
		t.Fatalf("idempotent approval = %#v %#v %v", replayed, replayRun, err)
	}
	if _, _, err := service.ApprovePrototypeExecution(ctx, ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: "approve-one", OperatorConfirmationEvidence: "changed-confirmation", ExpectedRunVersion: run.Version}); !errors.Is(err, ErrPrototypeApprovalConflicting) {
		t.Fatalf("same identity semantic conflict = %v", err)
	}
	if _, _, err := service.ApprovePrototypeExecution(ctx, ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: "approve-two", OperatorConfirmationEvidence: "confirmed", ExpectedRunVersion: approved.Version}); !errors.Is(err, ErrPrototypeApprovalConflicting) {
		t.Fatalf("conflicting approval = %v", err)
	}
	for table, beforeCount := range productionCounts {
		var afterCount int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&afterCount); err != nil || afterCount != beforeCount {
			t.Fatalf("production %s = %d, want %d: %v", table, afterCount, beforeCount, err)
		}
	}
}

func TestPrototypeExecutionRejectsDisabledCapabilityAndOwnership(t *testing.T) {
	ctx, store, service, workspace, ticket, _ := prototypeFixture(t)
	workspace, err := service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PreparePrototypeProposal(ctx, PreparePrototypeProposalInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, Proposal: []byte("x"), MediaType: "text/plain"}); !errors.Is(err, ErrPrototypeCapabilityDisabled) {
		t.Fatalf("disabled = %v", err)
	}
	if _, err := store.GetPrototypeProposal(ctx, "prototype-proposal-none"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unexpected proposal read = %v", err)
	}
}

func prototypeFixture(t *testing.T) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.SourceVaultClosure) {
	t.Helper()
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-prototype", "prototype")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("# discovery\n")
	_, workspace, err = service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	var vaultID int64
	if err = store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		ticket, e = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-prototype", WorkspaceRowID: workspace.ID, TicketKey: "prototype", Subject: "prototype"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('prototype-relay','C:/prototype','refs/heads/main',1)`); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id,repo_target,relative_path) VALUES ('vault-prototype','prototype-relay','vaults/prototype') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO source_vault_closures (closure_id,vault_row_id,commit_oid,tree_oid,generation,ref_name,state,import_started_at,verified_at) VALUES ('closure-prototype',?,?,?,1,'refs/relay/closures/prototype','ready','2026-08-05T00:00:00Z','2026-08-05T00:00:01Z')`, vaultID, strings.Repeat("a", 40), strings.Repeat("b", 40)); err != nil {
		t.Fatal(err)
	}
	closure, err := store.GetSourceVaultClosureByClosureID(ctx, "closure-prototype")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, service, workspace, ticket, closure
}

func preparedPrototype(t *testing.T) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.PrototypeProposal, workflowstore.PrototypeAuthorization, workflowstore.PrototypeRun) {
	t.Helper()
	ctx, store, service, workspace, ticket, _ := prototypeFixture(t)
	proposal, err := service.PreparePrototypeProposal(ctx, PreparePrototypeProposalInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, Proposal: []byte("proposal\n"), MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	authorization, run, err := service.PreparePrototypeExecution(ctx, PreparePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ProposalID: proposal.ProposalID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, SourceClosureID: "closure-prototype", RepoTarget: "prototype-relay", BaseCommit: strings.Repeat("a", 40), Adapter: "adapter", Model: "model", Variants: []string{"baseline"}, EvidenceObligations: []string{"result-envelope"}, Limits: map[string]any{"seconds": 60}})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, service, workspace, ticket, proposal, authorization, run
}

func approvalInput(workspace workflowstore.FeatureWorkspace, proposal workflowstore.PrototypeProposal, authorization workflowstore.PrototypeAuthorization, run workflowstore.PrototypeRun) ApprovePrototypeExecutionInput {
	return ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: "approve-drift", OperatorConfirmationEvidence: "confirmed", ExpectedRunVersion: run.Version}
}

func TestPrototypeExecutionBindsInvocationAuthorizationAndRun(t *testing.T) {
	ctx, store, _, workspace, _, _, authorization, run := preparedPrototype(t)
	if authorization.ProposedRunID != run.PrototypeRunID {
		t.Fatalf("authorization run = %q, run = %q", authorization.ProposedRunID, run.PrototypeRunID)
	}
	var relativePath string
	if err := store.DB().QueryRowContext(ctx, `SELECT relative_path FROM feature_workspace_discovery_artifacts WHERE id = ?`, authorization.InvocationArtifactRowID).Scan(&relativePath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ ProposedRunID string }
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ProposedRunID != run.PrototypeRunID {
		t.Fatalf("invocation run = %q, want %q", envelope.ProposedRunID, run.PrototypeRunID)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM feature_workspace_prototype_runs WHERE id = ?`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO feature_workspace_prototype_runs (prototype_run_id, authorization_row_id, workspace_row_id, work_item_row_id) VALUES (?, ?, ?, ?)`, "prototype-run-mismatch", authorization.ID, workspace.ID, run.WorkItemRowID); err == nil {
		t.Fatal("mismatched authorization/run insertion succeeded")
	}
}

func TestPrototypeExecutionApprovalDriftDoesNotMutate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, context.Context, *workflowstore.Store, workflowstore.FeatureWorkspace, workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.PrototypeAuthorization)
		want   error
	}{
		{"workspace version", func(t *testing.T, ctx context.Context, s *workflowstore.Store, w workflowstore.FeatureWorkspace, _ workflowstore.FeatureWorkspaceDiscoveryTicket, _ workflowstore.PrototypeAuthorization) {
			if _, err := s.DB().ExecContext(ctx, `UPDATE feature_workspaces SET version = version + 1 WHERE id = ?`, w.ID); err != nil {
				t.Fatal(err)
			}
		}, ErrPrototypeWorkspaceStale},
		{"work item version", func(t *testing.T, ctx context.Context, s *workflowstore.Store, _ workflowstore.FeatureWorkspace, ticket workflowstore.FeatureWorkspaceDiscoveryTicket, _ workflowstore.PrototypeAuthorization) {
			if _, err := s.DB().ExecContext(ctx, `UPDATE feature_workspace_discovery_tickets SET version = version + 1 WHERE id = ?`, ticket.ID); err != nil {
				t.Fatal(err)
			}
		}, ErrPrototypeWorkItemStale},
		{"source closure", func(t *testing.T, ctx context.Context, s *workflowstore.Store, _ workflowstore.FeatureWorkspace, _ workflowstore.FeatureWorkspaceDiscoveryTicket, a workflowstore.PrototypeAuthorization) {
			if _, err := s.DB().ExecContext(ctx, `UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'operation_cancelled', verified_at = NULL WHERE id = ?`, a.SourceClosureRowID); err != nil {
				t.Fatal(err)
			}
		}, ErrPrototypeSourceDivergence},
		{"invocation digest", func(t *testing.T, _ context.Context, _ *workflowstore.Store, _ workflowstore.FeatureWorkspace, _ workflowstore.FeatureWorkspaceDiscoveryTicket, _ workflowstore.PrototypeAuthorization) {
		}, ErrPrototypeAuthorizationMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, store, service, workspace, ticket, proposal, authorization, run := preparedPrototype(t)
			tc.mutate(t, ctx, store, workspace, ticket, authorization)
			in := approvalInput(workspace, proposal, authorization, run)
			if tc.name == "invocation digest" {
				in.InvocationSHA256 = strings.Repeat("f", 64)
			}
			if _, _, err := service.ApprovePrototypeExecution(ctx, in); !errors.Is(err, tc.want) {
				t.Fatalf("approval = %v, want %v", err, tc.want)
			}
			var approvals int
			if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_prototype_approvals WHERE run_row_id = ?`, run.ID).Scan(&approvals); err != nil {
				t.Fatal(err)
			}
			current, err := store.GetPrototypeRun(ctx, run.PrototypeRunID)
			if err != nil {
				t.Fatal(err)
			}
			if approvals != 0 || current.LifecycleState != "proposed" || current.Version != run.Version {
				t.Fatalf("mutation survived: approvals=%d run=%#v", approvals, current)
			}
		})
	}
}

func TestPrototypeExecutionPreparationPreservesTypedErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*testing.T, context.Context, *workflowstore.Store, workflowstore.FeatureWorkspace, workflowstore.FeatureWorkspaceDiscoveryTicket)
		prepare bool
		want    error
	}{
		{"workspace stale", func(t *testing.T, ctx context.Context, s *workflowstore.Store, w workflowstore.FeatureWorkspace, _ workflowstore.FeatureWorkspaceDiscoveryTicket) {
			_, err := s.DB().ExecContext(ctx, `UPDATE feature_workspaces SET version = version + 1 WHERE id = ?`, w.ID)
			if err != nil {
				t.Fatal(err)
			}
		}, false, ErrPrototypeWorkspaceStale},
		{"work item stale", func(t *testing.T, ctx context.Context, s *workflowstore.Store, _ workflowstore.FeatureWorkspace, ticket workflowstore.FeatureWorkspaceDiscoveryTicket) {
			_, err := s.DB().ExecContext(ctx, `UPDATE feature_workspace_discovery_tickets SET version = version + 1 WHERE id = ?`, ticket.ID)
			if err != nil {
				t.Fatal(err)
			}
		}, false, ErrPrototypeWorkItemStale},
		{"source divergence", func(t *testing.T, ctx context.Context, s *workflowstore.Store, _ workflowstore.FeatureWorkspace, _ workflowstore.FeatureWorkspaceDiscoveryTicket) {
			_, err := s.DB().ExecContext(ctx, `UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'operation_cancelled', verified_at = NULL WHERE closure_id = 'closure-prototype'`)
			if err != nil {
				t.Fatal(err)
			}
		}, true, ErrPrototypeSourceDivergence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, store, service, workspace, ticket, _ := prototypeFixture(t)
			proposal, err := service.PreparePrototypeProposal(ctx, PreparePrototypeProposalInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, Proposal: []byte("x"), MediaType: "text/plain"})
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, ctx, store, workspace, ticket)
			if tc.prepare {
				_, _, err = service.PreparePrototypeExecution(ctx, PreparePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ProposalID: proposal.ProposalID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, SourceClosureID: "closure-prototype", RepoTarget: "prototype-relay", BaseCommit: strings.Repeat("a", 40), Adapter: "adapter", Model: "model"})
			} else {
				_, err = service.PreparePrototypeProposal(ctx, PreparePrototypeProposalInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, Proposal: []byte("y"), MediaType: "text/plain"})
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPrototypeExecutionLifecycleVocabularyAndPartOneTransition(t *testing.T) {
	ctx, store, service, workspace, _, proposal, authorization, run := preparedPrototype(t)
	states := []string{"proposed", "approved", "preparing", "launch_uncertain", "running", "succeeded", "failed", "cancelled", "timed_out", "cleanup_required", "closed"}
	for _, state := range states {
		if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state = ? WHERE id = ?`, state, run.ID); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state = 'unknown' WHERE id = ?`, run.ID); err == nil {
		t.Fatal("unknown lifecycle accepted")
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state = 'proposed' WHERE id = ?`, run.ID); err != nil {
		t.Fatal(err)
	}
	approval, approved, err := service.ApprovePrototypeExecution(ctx, approvalInput(workspace, proposal, authorization, run))
	if err != nil || approval.ID == 0 || approved.LifecycleState != "approved" {
		t.Fatalf("part 1 transition = %#v %#v %v", approval, approved, err)
	}
	var transitions int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_prototype_lifecycle_transitions WHERE run_row_id = ? AND from_state = 'proposed' AND to_state = 'approved'`, run.ID).Scan(&transitions); err != nil || transitions != 1 {
		t.Fatalf("transitions = %d, %v", transitions, err)
	}
}

func TestPrototypeExecutionArtifactCompensationAfterPromotion(t *testing.T) {
	ctx, store, service, workspace, ticket, _ := prototypeFixture(t)
	before, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	restore := store.SetArtifactBatchPrepareCommitHookForTest(func() error { return errors.New("injected after promotion") })
	defer restore()
	if _, err := service.PreparePrototypeProposal(ctx, PreparePrototypeProposalInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, Proposal: []byte("compensate"), MediaType: "text/plain"}); !errors.Is(err, ErrPrototypeArtifactPersistence) {
		t.Fatalf("prepare = %v", err)
	}
	for _, table := range []string{"feature_workspace_prototype_proposals", "feature_workspace_prototype_authorizations"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count = %d, %v", table, count, err)
		}
	}
	after, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.CurrentDiscoveryRevisionRowID != before.CurrentDiscoveryRevisionRowID {
		t.Fatalf("workspace changed: before=%#v after=%#v", before, after)
	}
	entries, err := os.ReadDir(filepath.Join(store.ArtifactStore().Root(), ".staging"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging remains: %#v", entries)
	}
	var artifacts int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_artifacts`).Scan(&artifacts); err != nil || artifacts != 1 {
		t.Fatalf("discovery artifacts = %d, %v", artifacts, err)
	}
}
