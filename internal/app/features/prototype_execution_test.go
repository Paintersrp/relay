package features

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestPrototypeExecutionRequiresExactApprovalAndNeverLaunches(t *testing.T) {
	ctx, store, service, workspace, ticket, _ := prototypeFixture(t)
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
	if _, _, err := service.ApprovePrototypeExecution(ctx, ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: "approve-two", OperatorConfirmationEvidence: "confirmed", ExpectedRunVersion: approved.Version}); !errors.Is(err, ErrPrototypeApprovalConflicting) {
		t.Fatalf("conflicting approval = %v", err)
	}
	var production int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM runs`).Scan(&production); err != nil || production != 0 {
		t.Fatalf("production runs = %d, %v", production, err)
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
