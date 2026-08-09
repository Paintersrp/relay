package operations

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appackages "relay/internal/app/packages"
	apptickets "relay/internal/app/tickets"
	"relay/internal/executor"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
)

type remediationPackageSourceReader struct {
	path  string
	bytes []byte
}

func (r remediationPackageSourceReader) ReadPath(_ context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	if request.Path != r.path {
		return sourcevault.ReadPathResult{}, fmt.Errorf("unexpected source path %q", request.Path)
	}
	return sourcevault.ReadPathResult{ObjectOID: strings.Repeat("d", 40), Bytes: append([]byte(nil), r.bytes...)}, nil
}

func TestLifecycleRemediationPackageWorkflowBriefOnlyAndDeterministicOperations(t *testing.T) {
	briefOnly := runRemediationPackageWorkflow(t, false)
	withOperations := runRemediationPackageWorkflow(t, true)
	if briefOnly.packageRow.DeterministicOperationsSha256.Valid || briefOnly.packageRow.DeterministicOperationsCoverage.Valid {
		t.Fatal("brief-only package retained deterministic operations identity")
	}
	if withOperations.packageRow.DeterministicOperationsSha256.String == "" || withOperations.packageRow.DeterministicOperationsCoverage.String != "complete" {
		t.Fatalf("operations package identity = %#v", withOperations.packageRow)
	}
	if briefOnly.packageRow.PackageSha256 == withOperations.packageRow.PackageSha256 {
		t.Fatal("brief-only and operations packages reused the same digest")
	}
}

type remediationPackageWorkflowResult struct {
	packageRow workflowstore.ExecutionPackage
}

func runRemediationPackageWorkflow(t *testing.T, withOperations bool) remediationPackageWorkflowResult {
	t.Helper()
	fixture := newRemediationLifecycleFixture(t)
	publication := publishRemediationBriefTicket(t, fixture, false)
	approveRemediationCurrentBrief(t, fixture)
	brief := []byte(testfixtures.TicketDesignBrief)
	briefName := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", fixture.workspace.FeatureSlug, publication.result.Ticket.TicketID, publication.result.Revision.RevisionNumber)
	prepare := appackages.PrepareInput{SelectionID: publication.selection.Selection.SelectionID, TicketDesignBrief: appackages.ArtifactInput{DisplayName: briefName, ExpectedSHA256: lifecycleSHA(brief), Bytes: brief}}
	if withOperations {
		operations := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"%s","repo_target":"project","branch":"main","base_commit":"%s","coverage":"complete","operations":[{"path":"internal/example.go","operation":"create","implementation":{"content":"package example\n"}}]}`, fixture.workspace.FeatureSlug, fixture.closure.CommitOID))
		name := fmt.Sprintf("%s.ticket-%s.r%d.deterministic-operations.json", fixture.workspace.FeatureSlug, publication.result.Ticket.TicketID, publication.result.Revision.RevisionNumber)
		prepare.DeterministicOperations = &appackages.ArtifactInput{DisplayName: name, ExpectedSHA256: lifecycleSHA(operations), Bytes: operations}
	}
	owner, err := appackages.NewServiceWithSourceVaults(fixture.store, remediationPackageSourceReader{path: publication.result.Revision.SourcePath, bytes: publication.canonical})
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := NewPackageWorkflowService(owner, fakeMutationLeaseReconciler{}, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := workflow.Prepare(fixture.ctx, prepare)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PackageID == "" || len(prepared.Members) != 1 || prepared.Members[0].RevisionRowID != publication.result.Revision.ID || prepared.TicketDesignBrief.DisplayName != briefName || prepared.TicketDesignBrief.SHA256 != lifecycleSHA(brief) || prepared.DeterministicOperations != nil != withOperations {
		t.Fatalf("prepared remediation package = %#v", prepared)
	}
	if withOperations && prepared.DeterministicOperations.SHA256 != prepare.DeterministicOperations.ExpectedSHA256 {
		t.Fatalf("prepared operations artifact = %#v", prepared.DeterministicOperations)
	}
	packageRow, err := fixture.store.GetExecutionPackageByPackageID(fixture.ctx, prepared.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if packageRow.SelectionRowID != publication.selection.Selection.ID || packageRow.WorkspaceRowID != fixture.workspace.ID || packageRow.AuthorityRevisionRowID != fixture.authority.ID || packageRow.SourceClosureRowID != fixture.closure.ID {
		t.Fatalf("package basis = %#v", packageRow)
	}
	if packageRow.DeterministicOperationsSha256.Valid != withOperations {
		t.Fatalf("package operations presence = %#v", packageRow)
	}
	approved, err := workflow.Approve(fixture.ctx, appackages.ApproveInput{PackageID: packageRow.PackageID, ExpectedPackageSha256: packageRow.PackageSha256, OperatorConfirmationEvidence: "Approve the exact remediation package."})
	if err != nil {
		t.Fatal(err)
	}
	run, err := fixture.store.GetRunByRunID(fixture.ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Run.RunID == "" || approved.Run.Status != workflowstore.RunStatusSetupReady || run.PlanRowID.Valid || run.PlanPassRowID.Valid || approved.PackageApprovalID == "" {
		t.Fatalf("approved remediation package = %#v", approved)
	}
	selection, err := fixture.store.GetDeliveryTicketSelectionByRowID(fixture.ctx, publication.selection.Selection.ID)
	if err != nil || selection.State != "consumed" {
		t.Fatalf("selection after package approval = %#v err=%v", selection, err)
	}
	if countRows(t, fixture.store, "execution_package_approvals") < 2 || countRows(t, fixture.store, "execution_package_approval_bindings") < 2 || countRows(t, fixture.store, "plans") != 0 || countRows(t, fixture.store, "plan_passes") != 0 {
		t.Fatal("package approval created unexpected legacy execution linkage")
	}
	approvedAuthority, err := owner.LoadApprovedAuthorityForRun(fixture.ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if approvedAuthority.Run.ID != run.ID || approvedAuthority.Package.ID != packageRow.ID || approvedAuthority.PackageApproval.ApprovalID != approved.PackageApprovalID || approvedAuthority.Ticket.TicketID != publication.result.Ticket.TicketID || approvedAuthority.TicketRevision.ID != publication.result.Revision.ID || approvedAuthority.TicketDesignBrief.DisplayName != briefName || string(approvedAuthority.TicketDesignBrief.Bytes) != string(brief) || approvedAuthority.DeterministicOperations != nil != withOperations {
		t.Fatalf("approved-authority readback = %#v", approvedAuthority)
	}
	if string(approvedAuthority.DeliveryTicket.Bytes) != string(publication.canonical) || approvedAuthority.DeliveryTicket.SHA256 != lifecycleSHA(publication.canonical) || len(approvedAuthority.BriefProjection.ValidationCommands) != 1 || strings.TrimSpace(approvedAuthority.BriefProjection.ValidationCommands[0].Command) == "" {
		t.Fatalf("approved authority retained bytes/projection = %#v", approvedAuthority)
	}
	if withOperations && string(approvedAuthority.DeterministicOperations.Bytes) != string(prepare.DeterministicOperations.Bytes) {
		t.Fatal("approved authority changed deterministic operations bytes")
	}

	// Continue through the existing assignment, deterministic-outcome, effective
	// brief, and adaptive-attempt owners. External model execution is not called.
	sourceReader := remediationPackageSourceReader{path: publication.result.Revision.SourcePath, bytes: publication.canonical}
	assignments, err := executor.NewExecutionAssignmentService(fixture.store, sourceReader)
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := assignments.PrepareExecutionAssignment(fixture.ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if assignment.Assignment.Run.RunID != approved.Run.RunID || assignment.Assignment.Package.PackageID != packageRow.PackageID || assignment.Assignment.Ticket.RevisionRowID != publication.result.Revision.ID {
		t.Fatalf("execution assignment = %#v", assignment.Assignment)
	}
	if withOperations {
		deterministic, err := executor.NewPackageDeterministicExecutionService(fixture.store, sourceReader)
		if err != nil {
			t.Fatal(err)
		}
		result, err := deterministic.Execute(fixture.ctx, approved.Run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		attempts, err := fixture.store.ListExecutionAttemptsByRun(fixture.ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome.Outcome.Outcome.Status != "applied" || result.Outcome.Outcome.Outcome.Coverage != "complete" || len(attempts) != 0 {
			t.Fatalf("deterministic remediation preparation = %#v attempts=%#v", result, attempts)
		}
		if countRows(t, fixture.store, "plans") != 0 || countRows(t, fixture.store, "plan_passes") != 0 {
			t.Fatal("deterministic remediation package created Plan or Pass linkage")
		}
		return remediationPackageWorkflowResult{packageRow: packageRow}
	}
	outcomes, err := executor.NewDeterministicOutcomeService(fixture.store, sourceReader)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := outcomes.Persist(fixture.ctx, executor.DeterministicOutcomeInput{RunID: approved.Run.RunID, Preflight: executor.DeterministicPreflightResult{Status: executor.DeterministicPreflightNotPresent}})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Outcome.Outcome.Status != string(executor.DeterministicPreflightNotPresent) {
		t.Fatalf("deterministic outcome = %#v", outcome.Outcome)
	}
	briefService, err := executor.NewEffectiveExecutorBriefService(fixture.store, sourceReader)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := briefService.Prepare(fixture.ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Mode != executor.EffectiveExecutorBriefAdaptiveNoOperations || !effective.AdaptiveDispatchRequired || effective.Artifact == nil || len(effective.Bytes) == 0 {
		t.Fatalf("effective Executor Brief = %#v", effective)
	}
	adaptive, err := executor.NewAdaptiveExecutionAttemptService(fixture.store, sourceReader)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := adaptive.Prepare(fixture.ctx, executor.AdaptiveExecutionAttemptInput{RunID: approved.Run.RunID, Adapter: "codex", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Mode != executor.EffectiveExecutorBriefAdaptiveNoOperations || !attempt.AdaptiveDispatchRequired || attempt.Attempt == nil || attempt.Attempt.AttemptNumber != 1 || attempt.InputArtifact == nil || len(attempt.InputBytes) == 0 {
		t.Fatalf("adaptive preparation = %#v", attempt)
	}
	if countRows(t, fixture.store, "plans") != 0 || countRows(t, fixture.store, "plan_passes") != 0 {
		t.Fatal("remediation package created Plan or Pass linkage")
	}
	return remediationPackageWorkflowResult{packageRow: packageRow}
}

func consumeRemediationBriefSelection(t *testing.T, fixture remediationLifecycleFixture, publication remediationBriefPublication) {
	t.Helper()
	approveRemediationCurrentBrief(t, fixture)
	owner, err := appackages.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	brief := []byte(testfixtures.TicketDesignBrief)
	name := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", fixture.workspace.FeatureSlug, publication.result.Ticket.TicketID, publication.result.Revision.RevisionNumber)
	prepared, err := owner.Prepare(fixture.ctx, appackages.PrepareInput{SelectionID: publication.selection.Selection.SelectionID, TicketDesignBrief: appackages.ArtifactInput{DisplayName: name, ExpectedSHA256: lifecycleSHA(brief), Bytes: brief}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Approve(fixture.ctx, appackages.ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "Consume the exact prior remediation selection."}); err != nil {
		t.Fatal(err)
	}
}

func approveRemediationCurrentBrief(t *testing.T, fixture remediationLifecycleFixture) {
	t.Helper()
	owner, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.AdmitTicketDesignBrief(fixture.ctx, apptickets.TicketDesignBriefAdmissionInput{WorkspaceID: fixture.workspace.WorkspaceID, Bytes: []byte(testfixtures.TicketDesignBrief), CreatedIdentity: "planner"}); err != nil {
		t.Fatal(err)
	}
	review, err := owner.CompleteTicketDesignBriefReview(fixture.ctx, apptickets.CompleteBriefReviewInput{WorkspaceID: fixture.workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: apptickets.TicketDesignBriefReviewReadyForApproval, ReviewedBytes: []byte(testfixtures.TicketDesignBrief)})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(fixture.ctx, fixture.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ApproveReviewedTicketDesignBrief(fixture.ctx, review, apptickets.TicketDesignBriefApprovalInput{WorkspaceID: fixture.workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "Approve the exact remediation Brief.", CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, store *workflowstore.Store, table string) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
