package operations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	featureapp "relay/internal/app/features"
	"relay/internal/mcp/semanticidentity"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type remediationLifecycleFixture struct {
	lifecycleFixture
	workspace        workflowstore.FeatureWorkspace
	closure          workflowstore.SourceVaultClosure
	authority        workflowstore.FeatureWorkspaceAuthorityRevision
	authorityLayers  []remediationAuthorityLayer
	ticket           workflowstore.DeliveryTicket
	revision         workflowstore.DeliveryTicketRevision
	executionPackage workflowstore.ExecutionPackage
	run              workflowstore.Run
	auditPacket      workflowstore.AuditPacket
	decision         workflowstore.AuditDecision
	seed             workflowstore.AuditRemediationSeed
	findings         []workflowstore.AuditRemediationSeedFinding
}

type remediationAuthorityLayer struct {
	kind     string
	bytes    []byte
	artifact workflowstore.Artifact
}

func newRemediationLifecycleFixture(t *testing.T) remediationLifecycleFixture {
	t.Helper()
	fixture := remediationLifecycleFixture{lifecycleFixture: openLifecycleFixture(t)}
	ctx := fixture.ctx
	commit := strings.TrimSpace(runLifecycleGit(t, fixture.projectRepo, "rev-parse", "HEAD"))
	vaults, err := sourcevault.Open(ctx, filepath.Join(filepath.Dir(fixture.store.ArtifactStore().Root()), "source-vaults"), fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := fixture.repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{RepoTarget: "project"})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := vaults.ImportClosure(ctx, sourcevault.ImportRequest{Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	fixture.closure = imported.Closure

	project, err := fixture.store.GetProjectByProjectID(ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		fixture.workspace, err = tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{
			WorkspaceID: "workspace-remediation", ProjectRowID: project.ID, FeatureSlug: "remediation",
		})
		if err != nil {
			return err
		}
		fixture.run, err = tx.CreateRun(ctx, workflowstore.CreateRunParams{
			RunID: "run-remediation", FeatureSlug: "remediation", RepoTarget: "project", Status: workflowstore.RunStatusCreated,
			Branch: "main", BaseCommit: commit, CanonicalSHA256: strings.Repeat("1", 64),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	featureService, err := featureapp.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.workspace, err = featureService.SetIntegratedDiscoveryCapability(ctx, fixture.workspace.WorkspaceID, fixture.workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, fixture.workspace, err = featureService.AdoptFeatureDiscoveryLifecycle(ctx, featureapp.AdoptFeatureDiscoveryLifecycleInput{
		WorkspaceID: fixture.workspace.WorkspaceID, ExpectedVersion: fixture.workspace.Version, OperatorIdentity: "operator",
	}); err != nil {
		t.Fatal(err)
	}
	discovery := []byte("# Remediation discovery\n")
	started, updatedWorkspace, err := featureService.StartIntegratedDiscovery(ctx, featureapp.StartIntegratedDiscoveryInput{
		WorkspaceID: fixture.workspace.WorkspaceID, ExpectedVersion: fixture.workspace.Version, Markdown: discovery,
		SHA256: lifecycleSHA(discovery), CreatedIdentity: "operator", Destination: featureapp.DiscoveryDestinationDirectDeliveryTicket,
	})
	if err != nil {
		t.Fatal(err)
	}
	closed, updatedWorkspace, err := featureService.CloseFeatureDiscovery(ctx, featureapp.CloseFeatureDiscoveryInput{
		WorkspaceID: updatedWorkspace.WorkspaceID, ExpectedVersion: updatedWorkspace.Version,
		ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: featureapp.DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Packet.WorkspaceRowID != updatedWorkspace.ID {
		t.Fatalf("discovery closure workspace = %d, want %d", closed.Packet.WorkspaceRowID, updatedWorkspace.ID)
	}
	fixture.workspace = updatedWorkspace

	fixture.authorityLayers = []remediationAuthorityLayer{
		{kind: "requirements", bytes: []byte("exact requirements bytes\n")},
		{kind: "design", bytes: []byte("exact design bytes\n")},
	}
	for index := range fixture.authorityLayers {
		fixture.authorityLayers[index].artifact = createRemediationArtifact(t, fixture.store, fixture.ctx, fixture.run.ID,
			fmt.Sprintf("authority-layer-%d", index+1), fixture.authorityLayers[index].kind, fixture.authorityLayers[index].bytes)
	}
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		fixture.authority, err = tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: "authority-remediation-1", WorkspaceRowID: fixture.workspace.ID, RevisionNumber: 1,
			SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		for index, layer := range fixture.authorityLayers {
			if _, err := tx.CreateFeatureWorkspaceAuthorityLayer(ctx, workflowstore.CreateFeatureWorkspaceAuthorityLayerParams{
				AuthorityRevisionRowID: fixture.authority.ID, LayerKind: layer.kind, Sequence: int64(index + 1),
				ArtifactRowID: sql.NullInt64{Int64: layer.artifact.ID, Valid: true}, ArtifactSha256: layer.artifact.SHA256,
				SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true},
			}); err != nil {
				return err
			}
		}
		_, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, fixture.authority.ID, fixture.workspace.WorkspaceID, fixture.workspace.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	packageSHA := strings.Repeat("2", 64)
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		fixture.ticket, err = tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "TICKET-REMEDIATION", WorkspaceRowID: fixture.workspace.ID, ExternalPriority: 10})
		if err != nil {
			return err
		}
		fixture.revision, err = tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: fixture.ticket.ID, RevisionNumber: 1, RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID,
			SourceClosureRowID: fixture.closure.ID, SourcePath: "tickets/remediation.delivery-ticket.json", Goal: "Remediate the audited package.",
			Context: "The ticket is an immutable audit obligation.", TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		if _, err = tx.SetDeliveryTicketCurrentRevision(ctx, fixture.ticket.TicketID, fixture.revision.ID); err != nil {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID: "approval-remediation", RevisionRowID: fixture.revision.ID, ApprovalKind: "delivery", ApprovalState: "approved",
			Rationale: "Approved for the exact remediation package.", SourceClosureRowID: fixture.closure.ID,
			AuthorityRevisionRowID: sql.NullInt64{Int64: fixture.authority.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selection, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-remediation", WorkspaceRowID: fixture.workspace.ID, State: "active",
			Rationale: "Select the exact audited ticket.", SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selectionMember, err := tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: fixture.revision.ID, ApprovalRowID: approval.ID,
		})
		if err != nil {
			return err
		}
		fixture.executionPackage, err = tx.CreateExecutionPackage(ctx, workflowstore.CreateExecutionPackageParams{
			PackageID: "package-remediation", SelectionRowID: selection.ID, WorkspaceRowID: fixture.workspace.ID, RepoTarget: "project", Branch: "main",
			BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID, AuthorityRevisionRowID: fixture.authority.ID,
			PackageSha256: packageSHA, AuthoritySha256: strings.Repeat("3", 64), SourceSha256: strings.Repeat("4", 64), DesignBriefSha256: strings.Repeat("5", 64),
			DeterministicOperationsSha256: sql.NullString{String: strings.Repeat("6", 64), Valid: true}, DeterministicOperationsCoverage: sql.NullString{String: "complete", Valid: true},
		})
		if err != nil {
			return err
		}
		member, err := tx.CreateExecutionPackageMember(ctx, workflowstore.CreateExecutionPackageMemberParams{
			PackageRowID: fixture.executionPackage.ID, SelectionMemberRowID: selectionMember.ID, Sequence: 1, RevisionRowID: fixture.revision.ID, MemberSha256: strings.Repeat("7", 64),
		})
		if err != nil {
			return err
		}
		if _, err = tx.CreateExecutionPackageApprovalBinding(ctx, workflowstore.CreateExecutionPackageApprovalBindingParams{
			PackageRowID: fixture.executionPackage.ID, PackageMemberRowID: member.ID, ApprovalRowID: approval.ID, AuthorityRevisionRowID: fixture.authority.ID,
			SourceClosureRowID: fixture.closure.ID, ApprovalBasisSha256: strings.Repeat("8", 64),
		}); err != nil {
			return err
		}
		if _, err = tx.ConsumeDeliveryTicketSelection(ctx, selection.SelectionID); err != nil {
			return err
		}
		packageApproval, err := tx.CreateExecutionPackageApproval(ctx, workflowstore.CreateExecutionPackageApprovalParams{
			ApprovalID: "pkg-approval-remediation", PackageRowID: fixture.executionPackage.ID, PackageSha256: packageSHA,
			OperatorConfirmationEvidence: "Approved exact package basis.",
		})
		if err != nil {
			return err
		}
		if _, err = tx.LinkRunToExecutionPackage(ctx, fixture.run.RunID, fixture.executionPackage.ID); err != nil {
			return err
		}
		if _, err = tx.LinkRunToExecutionPackageApproval(ctx, workflowstore.LinkRunToExecutionPackageApprovalParams{PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, RunID: fixture.run.RunID}); err != nil {
			return err
		}
		for _, transition := range [][2]string{{workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady}, {workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting}, {workflowstore.RunStatusExecuting, workflowstore.RunStatusValidating}, {workflowstore.RunStatusValidating, workflowstore.RunStatusAuditReady}} {
			if fixture.run, err = tx.TransitionRun(ctx, fixture.run.RunID, transition[0], transition[1]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	auditBytes := []byte(`{"audit":"exact package audit"}`)
	auditArtifact := createRemediationArtifact(t, fixture.store, fixture.ctx, fixture.run.ID, "audit-packet", "audit_packet", auditBytes)
	fixture.auditPacket, fixture.decision, fixture.seed, fixture.findings = createRemediationAuditHistory(t, fixture, auditArtifact, packageSHA)
	return fixture
}

func TestResolveCurrentWorkspaceAndDeliveryTicketReferences(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	project, err := fixture.store.GetProjectByProjectID(fixture.ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(fixture.ctx, fixture.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var route workflowstore.FeatureWorkspaceRouteState
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		routes, err := tx.ListFeatureWorkspaceRouteStates(fixture.ctx, workspace.ID)
		if err != nil {
			return err
		}
		route, err = tx.CreateFeatureWorkspaceRouteState(fixture.ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: "route-remediation-1", WorkspaceRowID: workspace.ID, Sequence: int64(len(routes) + 1), WorkspaceVersion: workspace.Version + 1, State: "ready"})
		if err != nil {
			return err
		}
		workspace, err = tx.AdvanceFeatureWorkspaceRouteState(fixture.ctx, route.ID, "open", workspace.WorkspaceID, workspace.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	workspaceReference, err := fixture.service.resolveWorkflowReference(fixture.ctx, project, semanticidentity.WorkflowReferenceRequest{Kind: "feature_workspace", WorkspaceID: workspace.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if workspaceReference.WorkspaceVersion != workspace.Version || workspaceReference.RouteStateID != route.RouteStateID || workspaceReference.RouteWorkspaceVersion != route.WorkspaceVersion {
		t.Fatalf("workspace reference = %#v", workspaceReference)
	}
	ticketReference, err := fixture.service.resolveWorkflowReference(fixture.ctx, project, semanticidentity.WorkflowReferenceRequest{Kind: "delivery_ticket", WorkspaceID: workspace.WorkspaceID, TicketID: fixture.ticket.TicketID})
	if err != nil {
		t.Fatal(err)
	}
	if ticketReference.RevisionID != fixture.revision.ID || ticketReference.RevisionNumber != fixture.revision.RevisionNumber || ticketReference.SourceClosureID != fixture.closure.ClosureID {
		t.Fatalf("ticket reference = %#v", ticketReference)
	}
	workflowArtifact := fixture.authorityLayers[0].artifact
	workflowRecord, workflowBytes, err := fixture.service.materializeWorkflowRecord(fixture.ctx, semanticidentity.WorkflowRecordInputReference{Kind: "run_execution_spec", RunID: fixture.run.RunID, ArtifactID: workflowArtifact.ArtifactID, ExpectedSHA256: workflowArtifact.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if workflowRecord.Kind != "run_execution_spec" || workflowRecord.RunID != fixture.run.RunID || workflowRecord.ArtifactID != workflowArtifact.ArtifactID || workflowRecord.ArtifactSHA256 != workflowArtifact.SHA256 || !bytes.Equal(workflowBytes, fixture.authorityLayers[0].bytes) {
		t.Fatalf("workflow record = %#v, bytes = %q", workflowRecord, workflowBytes)
	}

	if _, err := fixture.service.prepareWorkflowReferences(fixture.ctx, project, []semanticidentity.WorkflowReferenceRequest{{Kind: "feature_workspace", WorkspaceID: workspace.WorkspaceID}, {Kind: "feature_workspace", WorkspaceID: workspace.WorkspaceID}}); err == nil {
		t.Fatal("duplicate workspace references were accepted")
	}

	var foreignWorkspace, noRouteWorkspace workflowstore.FeatureWorkspace
	var wrongWorkspaceTicket, noRevisionTicket workflowstore.DeliveryTicket
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		foreignProject, createErr := tx.CreateProject(fixture.ctx, workflowstore.CreateProjectParams{ProjectID: "project-foreign-reference", Name: "Foreign", Description: "foreign reference"})
		if createErr != nil {
			return createErr
		}
		foreignWorkspace, createErr = tx.CreateFeatureWorkspace(fixture.ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: "workspace-foreign-reference", ProjectRowID: foreignProject.ID, FeatureSlug: "foreign-reference"})
		if createErr != nil {
			return createErr
		}
		noRouteWorkspace, createErr = tx.CreateFeatureWorkspace(fixture.ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: "workspace-no-route-reference", ProjectRowID: project.ID, FeatureSlug: "no-route-reference"})
		if createErr != nil {
			return createErr
		}
		wrongWorkspaceTicket, createErr = tx.CreateDeliveryTicket(fixture.ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "TICKET-WRONG-WORKSPACE-REFERENCE", WorkspaceRowID: noRouteWorkspace.ID, ExternalPriority: 1})
		if createErr != nil {
			return createErr
		}
		noRevisionTicket, createErr = tx.CreateDeliveryTicket(fixture.ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "TICKET-NO-REVISION-REFERENCE", WorkspaceRowID: workspace.ID, ExternalPriority: 2})
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	invalidRequests := []semanticidentity.WorkflowReferenceRequest{
		{Kind: "feature_workspace", WorkspaceID: foreignWorkspace.WorkspaceID},
		{Kind: "feature_workspace", WorkspaceID: noRouteWorkspace.WorkspaceID},
		{Kind: "delivery_ticket", WorkspaceID: workspace.WorkspaceID, TicketID: wrongWorkspaceTicket.TicketID},
		{Kind: "delivery_ticket", WorkspaceID: workspace.WorkspaceID, TicketID: noRevisionTicket.TicketID},
	}
	for _, request := range invalidRequests {
		if _, err := fixture.service.resolveWorkflowReference(fixture.ctx, project, request); err == nil {
			t.Fatalf("invalid reference accepted: %#v", request)
		}
	}

	var cancelledTicket workflowstore.DeliveryTicket
	var cancelled workflowstore.DeliveryTicketRevision
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		cancelledTicket, err = tx.CreateDeliveryTicket(fixture.ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "TICKET-CANCELLED-REFERENCE", WorkspaceRowID: workspace.ID, ExternalPriority: 3})
		if err != nil {
			return err
		}
		cancelled, err = tx.CreateDeliveryTicketRevision(fixture.ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: cancelledTicket.ID, RevisionNumber: 1, CancellationReason: sql.NullString{String: "cancelled", Valid: true}, RepoTarget: fixture.revision.RepoTarget, Branch: fixture.revision.Branch, BaseCommit: fixture.revision.BaseCommit, SourceClosureRowID: fixture.closure.ID, SourcePath: fixture.revision.SourcePath, Goal: fixture.revision.Goal, Context: fixture.revision.Context, TransitionApplicability: fixture.revision.TransitionApplicability})
		if err != nil {
			return err
		}
		_, err = tx.SetDeliveryTicketCurrentRevision(fixture.ctx, cancelledTicket.TicketID, cancelled.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cancelledRequest := semanticidentity.WorkflowReferenceRequest{Kind: "delivery_ticket", WorkspaceID: workspace.WorkspaceID, TicketID: cancelledTicket.TicketID}
	if _, err := fixture.service.resolveWorkflowReference(fixture.ctx, project, cancelledRequest); err == nil {
		t.Fatal("cancelled current revision was accepted")
	}
	request := semanticidentity.WorkflowReferenceRequest{Kind: "delivery_ticket", WorkspaceID: workspace.WorkspaceID, TicketID: fixture.ticket.TicketID}
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		_, err = tx.TransitionSourceVaultClosure(fixture.ctx, workflowstore.TransitionSourceVaultClosureParams{ClosureID: fixture.closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateReady, NextState: workflowstore.SourceVaultClosureStateUnavailable, FailureReason: sql.NullString{String: "operation_cancelled", Valid: true}, TransitionAt: "2026-08-03T00:00:00.000000000Z"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.resolveWorkflowReference(fixture.ctx, project, request); err == nil {
		t.Fatal("non-ready source closure was accepted")
	}
}

func createRemediationArtifact(t *testing.T, store *workflowstore.Store, ctx context.Context, runRowID int64, name, kind string, data []byte) workflowstore.Artifact {
	t.Helper()
	batch, err := store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("runs", fmt.Sprintf("run-remediation-%s", name))))
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage(kind, name+".json", "application/json", data)
	if err != nil {
		t.Fatal(err)
	}
	var artifact workflowstore.Artifact
	if err := store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		var txErr error
		artifact, txErr = tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID: "artifact-" + name, OwnerType: "run", RunRowID: sql.NullInt64{Int64: runRowID, Valid: true}, Kind: kind,
			RelativePath: file.RelativePath, MediaType: file.MediaType, SHA256: file.SHA256, SizeBytes: file.SizeBytes,
		})
		return txErr
	}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func createRemediationAuditHistory(t *testing.T, fixture remediationLifecycleFixture, artifact workflowstore.Artifact, packageSHA string) (workflowstore.AuditPacket, workflowstore.AuditDecision, workflowstore.AuditRemediationSeed, []workflowstore.AuditRemediationSeedFinding) {
	t.Helper()
	ctx := fixture.ctx
	auditedCommit := fixture.closure.CommitOID
	var packet workflowstore.AuditPacket
	var decision workflowstore.AuditDecision
	var seed workflowstore.AuditRemediationSeed
	var findings []workflowstore.AuditRemediationSeedFinding
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		packet, err = tx.CreateAuditPacket(ctx, workflowstore.CreateAuditPacketParams{AuditPacketID: "packet-remediation", RunRowID: fixture.run.ID, ImplementationActorKind: "applier", ArtifactRowID: artifact.ID, BaseCommit: fixture.closure.CommitOID, AuditedCommit: auditedCommit, PacketSHA256: artifact.SHA256})
		if err != nil {
			return err
		}
		members, err := tx.ListExecutionPackageMembers(ctx, fixture.executionPackage.ID)
		if err != nil || len(members) != 1 {
			return errors.New("remediation package member is unavailable")
		}
		packageApproval, err := tx.GetExecutionPackageApprovalByPackageRowID(ctx, fixture.executionPackage.ID)
		if err != nil {
			return err
		}
		obligation, err := tx.CreateAuditPacketTicketObligation(ctx, workflowstore.CreateAuditPacketTicketObligationParams{
			AuditPacketRowID: packet.ID, ExecutionPackageRowID: fixture.executionPackage.ID, ExecutionPackageMemberRowID: members[0].ID, DeliveryTicketRowID: fixture.ticket.ID,
			DeliveryTicketRevisionRowID: fixture.revision.ID, AuthorityRevisionRowID: fixture.authority.ID, SourceClosureRowID: fixture.closure.ID,
			PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: packageSHA, Valid: true},
		})
		if err != nil {
			return err
		}
		decision, err = tx.CreateAuditDecision(ctx, workflowstore.CreateAuditDecisionParams{AuditDecisionID: "audit-remediation", RunRowID: fixture.run.ID, AuditPacketArtifactRowID: artifact.ID, AuditedCommit: auditedCommit, PacketSHA256: packet.PacketSHA256, Decision: workflowstore.AuditDecisionNeedsRevision, Rationale: "The exact package requires remediation."})
		if err != nil {
			return err
		}
		revisionDecision, err := tx.CreateAuditTicketRevisionDecision(ctx, workflowstore.CreateAuditTicketRevisionDecisionParams{AuditDecisionRowID: decision.ID, AuditPacketTicketObligationRowID: obligation.ID, PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: packageSHA, Valid: true}})
		if err != nil {
			return err
		}
		seed, err = tx.CreateAuditRemediationSeed(ctx, workflowstore.CreateAuditRemediationSeedParams{RemediationSeedID: "remediation-seed", AuditTicketRevisionDecisionRowID: revisionDecision.ID, AuditPacketRowID: packet.ID, ExecutionPackageRowID: fixture.executionPackage.ID, AuditedCommit: auditedCommit, DecisionRationale: decision.Rationale})
		if err != nil {
			return err
		}
		for index, value := range []struct{ class, summary, evidence, remediation string }{
			{"implementation", "implementation finding", "implementation evidence", "implementation remediation"},
			{"governing_package", "package finding", "package evidence", "package remediation"},
			{"both", "combined finding", "combined evidence", "combined remediation"},
		} {
			finding, findingErr := tx.CreateAuditRemediationSeedFinding(ctx, workflowstore.CreateAuditRemediationSeedFindingParams{RemediationSeedRowID: seed.ID, Sequence: int64(index + 1), UpstreamClassification: value.class, Summary: value.summary, Evidence: value.evidence, RequiredRemediation: value.remediation})
			if findingErr != nil {
				return findingErr
			}
			findings = append(findings, finding)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return packet, decision, seed, findings
}

func remediationIdentity(fixture remediationLifecycleFixture) semanticidentity.CreateOperationPacket {
	return semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1", OperationID: "planner.delivery_ticket_remediation", ProjectID: fixture.projectID,
		WorkflowReferences: []semanticidentity.WorkflowReferenceRequest{{Kind: "audit_decision", RunID: fixture.run.RunID, AuditDecisionID: fixture.decision.AuditDecisionID}},
	}
}

func remediationRefreshIdentity(fixture remediationLifecycleFixture, packetID string) semanticidentity.RefreshOperationPacket {
	return semanticidentity.RefreshOperationPacket{
		SurfaceContract: "planner-authoring.v1", ExpectedPacketID: packetID,
		WorkflowReferences: []semanticidentity.WorkflowReferenceRequest{{Kind: "audit_decision", RunID: fixture.run.RunID, AuditDecisionID: fixture.decision.AuditDecisionID}},
	}
}

func TestLifecycleRemediationCreatesCanonicalPacketAndRefreshesCurrentAuthority(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	identity := remediationIdentity(fixture)
	if _, err := semanticidentity.BuildFingerprint(identity); err != nil {
		t.Fatalf("identity is invalid: %v", err)
	}
	beforeCreate := remediationStateSnapshot(t, fixture)
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-remediation", Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if created.Packet.Summary.OperationID != "planner.delivery_ticket_remediation" || created.Packet.Summary.SurfaceContract != "planner-authoring.v1" {
		t.Fatalf("packet route = %#v", created.Packet.Summary)
	}
	seedBytes, authorityBytes := readRemediationInputs(t, fixture, created.Packet)
	assertRemediationSeed(t, fixture, seedBytes)
	assertCurrentAuthority(t, fixture, authorityBytes, fixture.authority, fixture.authorityLayers)
	assertRemediationRetention(t, fixture, created.Packet, seedBytes, authorityBytes)
	assertRemediationBoundaryStable(t, beforeCreate, remediationStateSnapshot(t, fixture))

	newLayerBytes := []byte("new exact requirements bytes\n")
	newLayerArtifact := createRemediationArtifact(t, fixture.store, fixture.ctx, fixture.run.ID, "authority-layer-new", "authority_layer", newLayerBytes)
	var newAuthority workflowstore.FeatureWorkspaceAuthorityRevision
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		var err error
		newAuthority, err = tx.CreateFeatureWorkspaceAuthorityRevision(fixture.ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: "authority-remediation-2", WorkspaceRowID: fixture.workspace.ID, RevisionNumber: 2, SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true}})
		if err != nil {
			return err
		}
		if _, err = tx.CreateFeatureWorkspaceAuthorityLayer(fixture.ctx, workflowstore.CreateFeatureWorkspaceAuthorityLayerParams{AuthorityRevisionRowID: newAuthority.ID, LayerKind: "requirements", Sequence: 1, ArtifactRowID: sql.NullInt64{Int64: newLayerArtifact.ID, Valid: true}, ArtifactSha256: newLayerArtifact.SHA256, SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true}}); err != nil {
			return err
		}
		_, err = tx.SetFeatureWorkspaceAuthorityRevision(fixture.ctx, newAuthority.ID, fixture.workspace.WorkspaceID, fixture.workspace.Version+1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	beforeRefresh := remediationStateSnapshot(t, fixture)
	refreshed, err := fixture.service.Refresh(fixture.ctx, RefreshLifecycleInput{MutationID: "refresh-remediation", PriorPacketID: created.Packet.Summary.PacketID, Identity: remediationRefreshIdentity(fixture, created.Packet.Summary.PacketID)})
	if err != nil {
		t.Fatal(err)
	}
	refreshedSeed, refreshedAuthority := readRemediationInputs(t, fixture, refreshed.Packet)
	if !bytesEqual(refreshedSeed, seedBytes) || bytes.Equal(refreshedAuthority, authorityBytes) {
		t.Fatal("refresh did not preserve the seed and reconstruct the changed authority")
	}
	assertCurrentAuthority(t, fixture, refreshedAuthority, newAuthority, []remediationAuthorityLayer{{kind: "requirements", bytes: newLayerBytes, artifact: newLayerArtifact}})
	if refreshed.Prior.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded || refreshed.Prior.ReplacementPacket == nil || refreshed.Prior.ReplacementPacket.PacketID != refreshed.Packet.Summary.PacketID {
		t.Fatalf("refresh lifecycle = %#v", refreshed)
	}
	if len(refreshed.Packet.DocumentBytes) == 0 || bytes.Equal(refreshed.Packet.DocumentBytes, created.Packet.DocumentBytes) {
		t.Fatal("refresh copied the prior packet document")
	}
	assertRemediationBoundaryStable(t, beforeRefresh, remediationStateSnapshot(t, fixture))
	assertNoRemediationConversationData(t, seedBytes, authorityBytes)
}

func readRemediationInputs(t *testing.T, fixture remediationLifecycleFixture, view PacketView) ([]byte, []byte) {
	t.Helper()
	var document struct {
		Inputs []struct {
			InputName string `json:"input_name"`
			SHA256    string `json:"sha256"`
			SizeBytes int64  `json:"size_bytes"`
			Source    struct {
				ArtifactID string `json:"artifact_id"`
			} `json:"source"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(view.DocumentBytes, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Inputs) != 2 {
		t.Fatalf("derived inputs = %#v", document.Inputs)
	}
	values := make(map[string][]byte, 2)
	integrity, err := fixture.store.GetOperationPacketPublicationIntegrity(fixture.ctx, viewPublicationID(t, fixture, view))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range document.Inputs {
		if input.InputName != "remediation_seed" && input.InputName != "current_approved_authority" || input.Source.ArtifactID == "" {
			t.Fatalf("invalid derived input = %#v", input)
		}
		var retained workflowstore.OperationPacketRetainedArtifact
		for _, candidate := range integrity.RetainedArtifacts {
			if candidate.ArtifactID == input.Source.ArtifactID {
				retained = candidate
				break
			}
		}
		if retained.ID == 0 {
			t.Fatalf("retained artifact %q not found", input.Source.ArtifactID)
		}
		path := filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(retained.RelativePath))
		data, err := os.ReadFile(path)
		if err != nil || int64(len(data)) != retained.SizeBytes || lifecycleSHA(data) != retained.SHA256 || input.SHA256 != retained.SHA256 || input.SizeBytes != retained.SizeBytes {
			t.Fatalf("retained input %q integrity failed: %v", input.InputName, err)
		}
		values[input.InputName] = data
	}
	return values["remediation_seed"], values["current_approved_authority"]
}

func viewPublicationID(t *testing.T, fixture remediationLifecycleFixture, view PacketView) string {
	t.Helper()
	publication, err := fixture.store.GetOperationPacketPublicationByPacketID(fixture.ctx, view.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	return publication.PublicationID
}

func assertRemediationSeed(t *testing.T, fixture remediationLifecycleFixture, data []byte) {
	t.Helper()
	var document remediationSeedInput
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.RemediationSeedID != fixture.seed.RemediationSeedID || document.AuditDecisionID != fixture.decision.AuditDecisionID || document.AuditPacketID != fixture.auditPacket.AuditPacketID || document.ApprovedExecutionPackage.PackageID != fixture.executionPackage.PackageID || document.ApprovedExecutionPackage.PackageSHA256 != fixture.executionPackage.PackageSha256 || document.AuditedDeliveryTicket.TicketID != fixture.ticket.TicketID || document.AuditedDeliveryTicketRevision.RevisionID != fixture.revision.ID || document.AuditedDeliveryTicketRevision.RevisionNumber != fixture.revision.RevisionNumber || document.AuditedCommit != fixture.seed.AuditedCommit || document.DecisionRationale != fixture.seed.DecisionRationale {
		t.Fatalf("seed identity = %#v", document)
	}
	if len(document.MaterialFindings) != len(fixture.findings) {
		t.Fatalf("seed findings = %#v", document.MaterialFindings)
	}
	for index, finding := range fixture.findings {
		got := document.MaterialFindings[index]
		if got.Sequence != int64(index+1) || finding.Sequence != int64(index+1) || got.Sequence != finding.Sequence || got.UpstreamClassification != finding.UpstreamClassification || got.Summary != finding.Summary || got.Evidence != finding.Evidence || got.RequiredRemediation != finding.RequiredRemediation {
			t.Fatalf("finding %d = %#v, want %#v", index, got, finding)
		}
	}
	canonical, err := canonicalJSON(document)
	if err != nil || !bytesEqual(canonical, data) {
		t.Fatalf("seed is not canonical")
	}
}

func assertCurrentAuthority(t *testing.T, fixture remediationLifecycleFixture, data []byte, authority workflowstore.FeatureWorkspaceAuthorityRevision, layers []remediationAuthorityLayer) {
	t.Helper()
	if !authority.SourceClosureRowID.Valid {
		t.Fatalf("authority source closure row ID is invalid: %#v", authority)
	}
	expectedClosure, err := fixture.store.GetSourceVaultClosureByRowID(fixture.ctx, authority.SourceClosureRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	var input currentApprovedAuthorityInput
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	if input.FeatureWorkspaceID != fixture.workspace.WorkspaceID || input.CurrentAuthorityRevisionID != authority.AuthorityRevisionID || input.SourceClosureID != expectedClosure.ClosureID || input.SourceClosureCommit != expectedClosure.CommitOID || input.AuthorityByteDigest != lifecycleSHA(mustAuthorityDocument(t, authority.AuthorityRevisionID, layers)) {
		t.Fatalf("authority identity = %#v", input)
	}
	authorityBytes, err := base64.StdEncoding.Strict().DecodeString(input.AuthorityBytes)
	if err != nil || lifecycleSHA(authorityBytes) != input.AuthorityByteDigest {
		t.Fatal("authority byte digest does not cover the retained document")
	}
	var document retainedAuthorityDocument
	if err := json.Unmarshal(authorityBytes, &document); err != nil || document.AuthorityRevisionID != authority.AuthorityRevisionID || len(document.Layers) != len(layers) {
		t.Fatalf("authority document = %#v err=%v", document, err)
	}
	for index, layer := range layers {
		expectedKind := layer.kind
		if expectedKind == "design" {
			expectedKind = "shared_design"
		}
		bytesValue, err := base64.StdEncoding.Strict().DecodeString(document.Layers[index].BytesBase64)
		if err != nil || !bytesEqual(bytesValue, layer.bytes) || document.Layers[index].Sequence != int64(index+1) || document.Layers[index].LayerKind != expectedKind || document.Layers[index].ArtifactSHA256 != layer.artifact.SHA256 || lifecycleSHA(layer.bytes) != layer.artifact.SHA256 {
			t.Fatalf("authority layer %d = %#v", index, document.Layers[index])
		}
	}
	canonical, err := canonicalJSON(input)
	if err != nil || !bytesEqual(canonical, data) {
		t.Fatal("authority input is not canonical")
	}
}

func mustAuthorityDocument(t *testing.T, authorityRevisionID string, layers []remediationAuthorityLayer) []byte {
	t.Helper()
	document := retainedAuthorityDocument{AuthorityRevisionID: authorityRevisionID, Layers: make([]retainedAuthorityLayer, len(layers))}
	for index, layer := range layers {
		expectedKind := layer.kind
		if expectedKind == "design" {
			expectedKind = "shared_design"
		}
		document.Layers[index] = retainedAuthorityLayer{Sequence: int64(index + 1), LayerKind: expectedKind, ArtifactSHA256: layer.artifact.SHA256, BytesBase64: base64.StdEncoding.EncodeToString(layer.bytes)}
	}
	data, err := canonicalJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertRemediationRetention(t *testing.T, fixture remediationLifecycleFixture, view PacketView, seedBytes, authorityBytes []byte) {
	t.Helper()
	integrity, err := fixture.store.GetOperationPacketPublicationIntegrity(fixture.ctx, viewPublicationID(t, fixture, view))
	if err != nil || len(integrity.RetainedArtifacts) != 2 || len(integrity.Bindings) < 3 {
		t.Fatalf("publication integrity = %#v err=%v", integrity, err)
	}
	seen := map[string]workflowstore.OperationPacketRetainedArtifact{}
	for _, artifact := range integrity.RetainedArtifacts {
		seen[artifact.ArtifactID] = artifact
	}
	if len(seen) != 2 {
		t.Fatal("derived artifacts are not distinct")
	}
	for name, expected := range map[string][]byte{"remediation_seed": seedBytes, "current_approved_authority": authorityBytes} {
		var found bool
		for _, binding := range integrity.Bindings {
			if binding.DependencyKey == name && binding.RetainedArtifactRowID.Valid {
				artifact, err := fixture.store.GetOperationPacketRetainedArtifactByRowID(fixture.ctx, binding.RetainedArtifactRowID.Int64)
				if err != nil || artifact.SHA256 != lifecycleSHA(expected) || artifact.SizeBytes != int64(len(expected)) || artifact.Kind != workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot || !strings.Contains(artifact.RelativePath, "operation-packet-publications") {
					t.Fatalf("binding %q = %#v err=%v", name, artifact, err)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("missing retained binding %q", name)
		}
	}
}

func assertNoRemediationConversationData(t *testing.T, values ...[]byte) {
	t.Helper()
	for _, data := range values {
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"executor_transcript", "attempt_messages", "execution_evidence", "validation_stdout", "validation_stderr", "effective_executor_brief", "deterministic_application_trace", "planner_conversation", "auditor_conversation"} {
			if _, ok := decoded[forbidden]; ok {
				t.Fatalf("derived document contains forbidden field %q", forbidden)
			}
		}
	}
}

func bytesEqual(left, right []byte) bool { return reflect.DeepEqual(left, right) }

func createStandaloneRemediationDecision(t *testing.T, fixture remediationLifecycleFixture, runID, decisionID, packetID, artifactName, decisionKind string) workflowstore.AuditDecision {
	t.Helper()
	var run workflowstore.Run
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		var err error
		run, err = tx.CreateRun(fixture.ctx, workflowstore.CreateRunParams{
			RunID: runID, FeatureSlug: "remediation-" + strings.TrimPrefix(runID, "run-remediation-"), RepoTarget: "project",
			Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: fixture.closure.CommitOID, CanonicalSHA256: strings.Repeat("9", 64),
		})
		if err != nil {
			return err
		}
		for _, transition := range [][2]string{{workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady}, {workflowstore.RunStatusSetupReady, workflowstore.RunStatusExecuting}, {workflowstore.RunStatusExecuting, workflowstore.RunStatusValidating}, {workflowstore.RunStatusValidating, workflowstore.RunStatusAuditReady}} {
			if run, err = tx.TransitionRun(fixture.ctx, run.RunID, transition[0], transition[1]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	artifact := createRemediationArtifact(t, fixture.store, fixture.ctx, run.ID, artifactName, "audit_packet", []byte(`{"audit":"standalone remediation decision"}`))
	var result workflowstore.AuditDecision
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		packet, err := tx.CreateAuditPacket(fixture.ctx, workflowstore.CreateAuditPacketParams{
			AuditPacketID: packetID, RunRowID: run.ID, ImplementationActorKind: "applier", ArtifactRowID: artifact.ID,
			BaseCommit: fixture.closure.CommitOID, AuditedCommit: fixture.closure.CommitOID, PacketSHA256: artifact.SHA256,
		})
		if err != nil {
			return err
		}
		result, err = tx.CreateAuditDecision(fixture.ctx, workflowstore.CreateAuditDecisionParams{
			AuditDecisionID: decisionID, RunRowID: run.ID, AuditPacketArtifactRowID: artifact.ID,
			AuditedCommit: fixture.closure.CommitOID, PacketSHA256: packet.PacketSHA256, Decision: decisionKind,
			Rationale: "Standalone decision for lifecycle rejection coverage.",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func setUnavailableCurrentAuthority(t *testing.T, fixture remediationLifecycleFixture) {
	t.Helper()
	ctx := fixture.ctx
	vault, err := fixture.store.GetSourceVaultByRepositoryTarget(ctx, "project")
	if err != nil {
		t.Fatal(err)
	}
	var closure workflowstore.SourceVaultClosure
	var authority workflowstore.FeatureWorkspaceAuthorityRevision
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		acquired, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: "closure-authority-unavailable", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40),
			RefName: "refs/relay/closures/closure-authority-unavailable", StartedAt: "2026-07-29T00:00:00.000000000Z",
		})
		if err != nil {
			return err
		}
		closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: acquired.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting, NextState: workflowstore.SourceVaultClosureStateReady,
			TransitionAt: "2026-07-29T00:00:01.000000000Z",
		})
		if err != nil {
			return err
		}
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, fixture.workspace.WorkspaceID)
		if err != nil {
			return err
		}
		authority, err = tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: "authority-remediation-unavailable", WorkspaceRowID: workspace.ID, RevisionNumber: 2,
			SourceClosureRowID: sql.NullInt64{Int64: closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		for index, layer := range fixture.authorityLayers {
			if _, err := tx.CreateFeatureWorkspaceAuthorityLayer(ctx, workflowstore.CreateFeatureWorkspaceAuthorityLayerParams{
				AuthorityRevisionRowID: authority.ID, LayerKind: layer.kind, Sequence: int64(index + 1), ArtifactRowID: sql.NullInt64{Int64: layer.artifact.ID, Valid: true},
				ArtifactSha256: layer.artifact.SHA256, SourceClosureRowID: sql.NullInt64{Int64: closure.ID, Valid: true},
			}); err != nil {
				return err
			}
		}
		if _, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, authority.ID, workspace.WorkspaceID, workspace.Version); err != nil {
			return err
		}
		_, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateReady, NextState: workflowstore.SourceVaultClosureStateUnavailable,
			FailureReason: sql.NullString{String: workflowstore.SourceVaultFailureSourceCommitMissing, Valid: true}, TransitionAt: "2026-07-29T00:00:02.000000000Z",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleRemediationRejectsInvalidInputsAtomically(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*testing.T, remediationLifecycleFixture)
		mutate  func(*testing.T, remediationLifecycleFixture, semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket
	}{
		{name: "unknown audit decision", mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			identity.WorkflowReferences[0].AuditDecisionID = "missing"
			return identity
		}},
		{name: "accepted audit decision", mutate: func(t *testing.T, f remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			decision := createStandaloneRemediationDecision(t, f, "run-remediation-accepted", "audit-accepted", "packet-accepted", "audit-accepted", workflowstore.AuditDecisionAccepted)
			identity.WorkflowReferences[0].RunID = "run-remediation-accepted"
			identity.WorkflowReferences[0].AuditDecisionID = decision.AuditDecisionID
			return identity
		}},
		{name: "needs revision without a seed", mutate: func(t *testing.T, f remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			decision := createStandaloneRemediationDecision(t, f, "run-remediation-no-seed", "audit-no-seed", "packet-no-seed", "audit-no-seed", workflowstore.AuditDecisionNeedsRevision)
			identity.WorkflowReferences[0].RunID = "run-remediation-no-seed"
			identity.WorkflowReferences[0].AuditDecisionID = decision.AuditDecisionID
			return identity
		}},
		{name: "already-consumed seed", prepare: func(t *testing.T, f remediationLifecycleFixture) {
			if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
				ticket, err := tx.CreateDeliveryTicket(f.ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "TICKET-REMEDIATION-REOPEN", WorkspaceRowID: f.workspace.ID, ExternalPriority: 20})
				if err != nil {
					return err
				}
				revision, err := tx.CreateDeliveryTicketRevision(f.ctx, workflowstore.CreateDeliveryTicketRevisionParams{
					DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "project", Branch: "main", BaseCommit: f.closure.CommitOID,
					SourceClosureRowID: f.closure.ID, SourcePath: "tickets/remediation-reopen.delivery-ticket.json", Goal: "Reopen the remediation ticket.",
					Context: "A valid remediation ticket revision consumes the seed.", TransitionApplicability: "not_required",
				})
				if err != nil {
					return err
				}
				if _, err = tx.SetDeliveryTicketCurrentRevision(f.ctx, ticket.TicketID, revision.ID); err != nil {
					return err
				}
				_, err = tx.CreateAuditRemediationSeedReopening(f.ctx, workflowstore.CreateAuditRemediationSeedReopeningParams{RemediationSeedRowID: f.seed.ID, ReopeningRevisionRowID: revision.ID, ReopeningKind: "remediation_ticket"})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}, mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			return identity
		}},
		{name: "workspace without current authority", prepare: func(t *testing.T, f remediationLifecycleFixture) {
			if _, err := f.store.DB().Exec(`UPDATE feature_workspaces SET current_authority_revision_row_id = NULL, version = version + 1 WHERE id = ?`, f.workspace.ID); err != nil {
				t.Fatal(err)
			}
		}, mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			return identity
		}},
		{name: "current authority without ready source closure", prepare: func(t *testing.T, f remediationLifecycleFixture) {
			setUnavailableCurrentAuthority(t, f)
		}, mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			return identity
		}},
		{name: "caller supplied seed", mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			identity.Inputs = []semanticidentity.InputBinding{{InputName: "remediation_seed", SourceKind: "inline_text", DisplayName: "seed.json", MediaType: "application/json", ExpectedSHA256: strings.Repeat("a", 64), Source: semanticidentity.InputBindingSource{Text: "{}"}}}
			return identity
		}},
		{name: "caller supplied authority", mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			identity.Inputs = []semanticidentity.InputBinding{{InputName: "current_approved_authority", SourceKind: "inline_text", DisplayName: "authority.json", MediaType: "application/json", ExpectedSHA256: strings.Repeat("a", 64), Source: semanticidentity.InputBindingSource{Text: "{}"}}}
			return identity
		}},
		{name: "duplicate audit reference", mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			identity.WorkflowReferences = append(identity.WorkflowReferences, identity.WorkflowReferences[0])
			return identity
		}},
		{name: "missing audit reference", mutate: func(_ *testing.T, _ remediationLifecycleFixture, identity semanticidentity.CreateOperationPacket) semanticidentity.CreateOperationPacket {
			identity.WorkflowReferences = nil
			return identity
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRemediationLifecycleFixture(t)
			if test.prepare != nil {
				test.prepare(t, fixture)
			}
			identity := test.mutate(t, fixture, remediationIdentity(fixture))
			before := remediationStateSnapshot(t, fixture)
			_, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "reject-" + strings.ReplaceAll(test.name, " ", "-"), Identity: identity})
			if err == nil {
				t.Fatal("invalid remediation request was accepted")
			}
			if after := remediationStateSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed create changed state: before=%#v after=%#v", before, after)
			}
		})
	}
}

type remediationState struct {
	tables map[string]string
	tree   map[string]string
}

func remediationStateSnapshot(t *testing.T, fixture remediationLifecycleFixture) remediationState {
	t.Helper()
	state := remediationState{tables: map[string]string{}, tree: map[string]string{}}
	for _, table := range []string{"operation_packets", "operation_packet_publications", "operation_packet_artifacts", "operation_packet_retained_artifacts", "operation_packet_artifact_bindings", "operation_packet_retention_dependencies", "operation_packet_vault_relationships", "source_vault_retentions", "artifacts", "delivery_tickets", "delivery_ticket_revisions", "delivery_ticket_revision_members", "delivery_ticket_revision_dependencies", "delivery_ticket_revision_approvals", "delivery_ticket_selections", "delivery_ticket_selection_members", "delivery_ticket_revision_satisfactions", "execution_packages", "execution_package_members", "execution_package_approval_bindings", "execution_package_approvals", "runs", "plans", "plan_passes", "execution_attempts", "audit_packets", "audit_decisions", "audit_packet_ticket_obligations", "audit_ticket_revision_decisions", "audit_remediation_seeds", "audit_remediation_seed_findings", "audit_remediation_seed_reopenings", "feature_workspace_completion_reopenings"} {
		rows, err := fixture.store.DB().Query(`SELECT * FROM ` + table + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var values []string
		for rows.Next() {
			cells := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range cells {
				pointers[index] = &cells[index]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			values = append(values, fmt.Sprint(cells...))
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		state.tables[table] = strings.Join(values, "\x00")
	}
	for _, root := range []string{"operation-packet-publications", "delivery-tickets", "packages", "runs", "audit-packets", "audit-decisions", ".staging"} {
		base := filepath.Join(fixture.store.ArtifactStore().Root(), root)
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				state.tree[filepath.Join(root, strings.TrimPrefix(path, base))] = lifecycleSHA(data)
			}
			return nil
		})
	}
	return state
}

func assertRemediationBoundaryStable(t *testing.T, before, after remediationState) {
	t.Helper()
	for _, table := range []string{"delivery_tickets", "delivery_ticket_revisions", "delivery_ticket_revision_members", "delivery_ticket_revision_dependencies", "delivery_ticket_revision_approvals", "delivery_ticket_selections", "delivery_ticket_selection_members", "delivery_ticket_revision_satisfactions", "execution_packages", "execution_package_members", "execution_package_approval_bindings", "execution_package_approvals", "runs", "plans", "plan_passes", "execution_attempts", "audit_packets", "audit_decisions", "audit_packet_ticket_obligations", "audit_ticket_revision_decisions", "audit_remediation_seeds", "audit_remediation_seed_findings", "audit_remediation_seed_reopenings", "feature_workspace_completion_reopenings"} {
		if before.tables[table] != after.tables[table] {
			t.Fatalf("successful remediation changed %s", table)
		}
	}
}

func TestLifecycleRemediationRefreshRejectsUnavailableAuthorityAtomically(t *testing.T) {
	fixture := newRemediationLifecycleFixture(t)
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-unavailable-authority", Identity: remediationIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	setUnavailableCurrentAuthority(t, fixture)
	before := remediationStateSnapshot(t, fixture)
	if _, err := fixture.service.Refresh(fixture.ctx, RefreshLifecycleInput{MutationID: "refresh-unavailable-authority", PriorPacketID: created.Packet.Summary.PacketID, Identity: remediationRefreshIdentity(fixture, created.Packet.Summary.PacketID)}); err == nil {
		t.Fatal("refresh accepted unavailable authority")
	}
	if after := remediationStateSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatalf("failed refresh changed state")
	}
}
