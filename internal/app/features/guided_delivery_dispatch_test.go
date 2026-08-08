package features

import (
	"context"
	"errors"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

// guidedDeliveryTicketFixture closes a direct-delivery workspace, records
// completion, and produces one approved current Delivery Ticket so the guided
// journey reaches the selection frontier.
func guidedDeliveryTicketFixture(t *testing.T) (context.Context, *Service, workflowstore.FeatureWorkspace) {
	t.Helper()
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	ticketService, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	candidateBytes := deliveryTicketCandidateBytes("P3-TX", workspace.FeatureSlug, "candidate-production", strings.Repeat("a", 40))
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryTicket,
		Filename: "discovery-proof.ticket-P3-TX.r1.delivery-ticket.json", Bytes: candidateBytes,
		SHA256: digestForPlanningTest(candidateBytes), RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidateApproval, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256,
		ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: candidateBytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "guided delivery fixture", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ticketService.PromoteApprovedDeliveryTicketCandidate(ctx, workflowtickets.CandidateProductionInput{
		CandidateID: candidate.Candidate.CandidateID, ApprovalID: candidateApproval.Approval.ApprovalID,
		ExpectedVersion: workspace.Version, ExternalPriority: 7, CreatedIdentity: "planner",
	}); err != nil {
		t.Fatal(err)
	}
	return ctx, service, workspace
}

// guidedFakePackageOwner is the packages-owner contract fake used to prove the
// guided dispatch delegates the approve action with server-resolved inputs.
type guidedFakePackageOwner struct {
	state    guidedapp.PackageState
	approved guidedapp.PackageState
	calls    []guidedapp.ApprovePackageInput
}

func (f *guidedFakePackageOwner) ReadWorkspacePackageState(context.Context, string) (guidedapp.PackageState, error) {
	return f.state, nil
}

func (f *guidedFakePackageOwner) ApproveCurrentPackage(_ context.Context, in guidedapp.ApprovePackageInput) error {
	f.calls = append(f.calls, in)
	f.state = f.approved
	return nil
}

// guidedFakeAuditOwner is the audits-owner contract fake used by guided
// dispatch tests; it reports neutral audit and remediation states unless the
// test overrides them.
type guidedFakeAuditOwner struct {
	runAudit    guidedapp.RunAuditState
	remediation guidedapp.RemediationState
}

func (f *guidedFakeAuditOwner) ReadRunAuditState(context.Context, string) (guidedapp.RunAuditState, error) {
	return f.runAudit, nil
}

func (f *guidedFakeAuditOwner) ReadWorkspaceRemediationState(context.Context, string) (guidedapp.RemediationState, error) {
	return f.remediation, nil
}

func TestGuidedSelectDispatchResolvesFrontierHeadServerSide(t *testing.T) {
	ctx, service, workspace := guidedDeliveryTicketFixture(t)
	// The refreshed projection after selection composes the package owner
	// state; bind a contract fake reporting no package yet.
	fake := &guidedFakePackageOwner{state: guidedapp.PackageState{State: "none"}}
	service.SetGuidedPackageOwnerForTest(fake)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})

	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if before.PrimaryAction.Action != GuidedActionSelectDeliveryTicket || !before.PrimaryAction.Enabled || !before.PrimaryAction.RequiresConfirmation || len(before.Delivery.Frontier) != 1 {
		t.Fatalf("frontier projection primary=%+v delivery=%+v", before.PrimaryAction, before.Delivery)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: before.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("selection without confirmation error = %v", err)
	}
	after, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: before.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if after.Projection.Delivery.SelectionState != "active" || after.Projection.PrimaryAction.Action != GuidedActionPreparePackage {
		t.Fatalf("after selection delivery=%+v primary=%+v", after.Projection.Delivery, after.Projection.PrimaryAction)
	}
}

func TestGuidedApprovePackageDispatchDelegatesOwnerAndRefreshesProjection(t *testing.T) {
	ctx, service, workspace := guidedDeliveryTicketFixture(t)
	fake := &guidedFakePackageOwner{state: guidedapp.PackageState{State: "none"}}
	service.SetGuidedPackageOwnerForTest(fake)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	selected, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionSelectDeliveryTicket), ExpectedVersion: workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Projection.Delivery.SelectionState != "active" || selected.Projection.PrimaryAction.Action != GuidedActionPreparePackage {
		t.Fatalf("after selection delivery=%+v primary=%+v", selected.Projection.Delivery, selected.Projection.PrimaryAction)
	}
	fake.state = guidedapp.PackageState{State: "prepared", PackageID: "package-guided", PackageSHA256: strings.Repeat("a", 64)}
	fake.approved = guidedapp.PackageState{State: "approved", PackageID: "package-guided", PackageSHA256: strings.Repeat("a", 64),
		RunID: "run-guided", RunStatus: "setup_ready", RunRepoTarget: "candidate-production", RunBranch: "main", RunBaseCommit: strings.Repeat("b", 40)}
	// The refreshed projection composes the run audit read, so the run the
	// package owner reports must exist in the store.
	insertGuidedRun(t, ctx, service, fake.approved)
	before, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if before.PrimaryAction.Action != GuidedActionApprovePackage || !before.PrimaryAction.Enabled || !before.PrimaryAction.RequiresConfirmation || before.Delivery.PackageState != "prepared" {
		t.Fatalf("prepared projection primary=%+v delivery=%+v", before.PrimaryAction, before.Delivery)
	}
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePackage), ExpectedVersion: before.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("package approval without confirmation error = %v", err)
	}
	after, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionApprovePackage), ExpectedVersion: before.Workspace.Version, Confirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || fake.calls[0].WorkspaceID != workspace.WorkspaceID || fake.calls[0].Evidence != guidedApprovalEvidence {
		t.Fatalf("guided approve delegation = %+v", fake.calls)
	}
	if after.Projection.Delivery.PackageState != "approved" || after.Projection.Delivery.RunID != "run-guided" {
		t.Fatalf("after approval delivery=%+v", after.Projection.Delivery)
	}
	if after.Projection.PrimaryAction.Action != GuidedActionLaunchRun {
		t.Fatalf("after approval primary=%+v", after.Projection.PrimaryAction)
	}

	launch, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionLaunchRun), ExpectedVersion: after.Projection.Workspace.Version})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Handoff == nil || launch.Handoff.Transfer == nil || launch.Handoff.Transfer.Run == nil || launch.Handoff.Transfer.Run.RunID != "run-guided" || launch.Handoff.Transfer.Run.Status != "setup_ready" || launch.Handoff.Transfer.Run.PackageID != "package-guided" {
		t.Fatalf("launch handoff=%+v", launch.Handoff)
	}
}

// insertGuidedRun inserts a package Run matching the state the package owner
// contract fake reports, so the refreshed projection can compose the run audit
// read.
func insertGuidedRun(t *testing.T, ctx context.Context, service *Service, state guidedapp.PackageState) {
	t.Helper()
	if _, err := service.store.DB().ExecContext(ctx, `
INSERT INTO runs (run_id, feature_slug, repo_target, status, branch, base_commit)
VALUES (?, 'discovery-proof', ?, 'created', ?, ?)`, state.RunID, state.RunRepoTarget, state.RunBranch, state.RunBaseCommit); err != nil {
		t.Fatal(err)
	}
	if err := service.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionRun(ctx, state.RunID, "created", "setup_ready")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGuidedReopenDiscoveryExecutesOwnerMutationAndRefreshes(t *testing.T) {
	ctx, _, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationNoDeliveryWork)
	fakeAudit := &guidedFakeAuditOwner{}
	service.SetGuidedAuditOwnerForTest(fakeAudit)

	projection, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.PrimaryAction.Action != GuidedActionReopenDiscovery || !projection.PrimaryAction.Enabled || !projection.PrimaryAction.RequiresConfirmation || projection.Discovery.ReopenState != "none" {
		t.Fatalf("completion recorded projection primary=%+v discovery=%+v", projection.PrimaryAction, projection.Discovery)
	}

	// Reopen is a confirmed primary mutation: without confirmation it is
	// rejected before any owner mutation.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReopenDiscovery), ExpectedVersion: projection.Workspace.Version}); !errors.Is(err, ErrFeatureCompletionConfirmation) {
		t.Fatalf("reopen without confirmation error = %v", err)
	}

	// With confirmation but without the operator-authored replacement content
	// the reopen owner rejects the consequence; no mutation occurs.
	if _, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReopenDiscovery), ExpectedVersion: projection.Workspace.Version, Confirmation: true}); !errors.Is(err, ErrInvalidDiscoveryConsequence) {
		t.Fatalf("reopen without replacement error = %v", err)
	}

	replacement := []byte("# guided reopened discovery\n")
	result, err := service.ExecuteGuidedAction(ctx, GuidedActionInput{
		WorkspaceID: workspace.WorkspaceID, Action: string(GuidedActionReopenDiscovery),
		ExpectedVersion: projection.Workspace.Version, Confirmation: true,
		Cause: "guided reopen", Markdown: replacement,
		Destination: DiscoveryDestinationNoDeliveryWork,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handoff != nil {
		t.Fatalf("reopen emitted a handoff instead of a refreshed mutation projection: %+v", result.Handoff)
	}
	if result.Projection.Workspace.Version <= projection.Workspace.Version {
		t.Fatalf("reopen did not advance the workspace version: %d -> %d", projection.Workspace.Version, result.Projection.Workspace.Version)
	}
	if result.Projection.Discovery.ReopenState != "reopened" || result.Projection.Discovery.State != "active" {
		t.Fatalf("reopened projection discovery=%+v", result.Projection.Discovery)
	}
	// The reopened workspace has no current closure packet until it is closed
	// again, so the refreshed projection reports the source-backed recovery
	// requirement rather than a fabricated continuation.
	if result.Projection.Recovery.State != "required" || result.Projection.Recovery.Category != "close_current_discovery" {
		t.Fatalf("reopened projection recovery=%+v", result.Projection.Recovery)
	}
	// Normal-entry proof: the guided request carried no digest, so the server
	// derived the replacement SHA-256 from the submitted markdown and the
	// reopen owner stored exactly that revision.
	frontier, err := service.ReadIntegratedDiscoveryFrontier(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if frontier.Current.Artifact.SHA256 != discoveryTestDigest(replacement) || string(frontier.Current.Markdown) != string(replacement) {
		t.Fatalf("guided reopen stored a replacement with an unexpected server-derived digest: sha=%q markdown=%q", frontier.Current.Artifact.SHA256, frontier.Current.Markdown)
	}
}
