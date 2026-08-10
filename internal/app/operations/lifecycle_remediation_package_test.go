package operations

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appackages "relay/internal/app/packages"
	"relay/internal/executor"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type remediationPackageSourceReader struct {
	path  string
	bytes []byte
}

func (r remediationPackageSourceReader) ReadPath(_ context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	if request.Path != r.path {
		return sourcevault.ReadPathResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
	}
	return sourcevault.ReadPathResult{ObjectOID: strings.Repeat("e", 40), Bytes: append([]byte(nil), r.bytes...)}, nil
}

func TestLifecycleRemediationPackageWorkflowTicketOnlyAndDeterministicOperations(t *testing.T) {
	ticketOnly := runRemediationPackageWorkflow(t, false)
	withOperations := runRemediationPackageWorkflow(t, true)
	if ticketOnly.packageRow.DeterministicOperationsSha256.Valid || ticketOnly.packageRow.DeterministicOperationsCoverage.Valid {
		t.Fatal("ticket-only package retained deterministic operations identity")
	}
	if withOperations.packageRow.DeterministicOperationsSha256.String == "" || withOperations.packageRow.DeterministicOperationsCoverage.String != "complete" {
		t.Fatalf("operations package identity = %#v", withOperations.packageRow)
	}
	if ticketOnly.packageRow.PackageSha256 == withOperations.packageRow.PackageSha256 {
		t.Fatal("ticket-only and operations packages reused the same digest")
	}
}

type remediationPackageWorkflowResult struct {
	packageRow workflowstore.ExecutionPackage
}

func runRemediationPackageWorkflow(t *testing.T, withOperations bool) remediationPackageWorkflowResult {
	t.Helper()
	fixture := newRemediationLifecycleFixture(t)
	publication := publishRemediationTicket(t, fixture, false)
	prepare := appackages.PrepareInput{SelectionID: publication.selection.Selection.SelectionID}
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
	if prepared.PackageID == "" || len(prepared.Members) != 1 || prepared.Members[0].RevisionRowID != publication.result.Revision.ID || prepared.TicketDocument.SHA256 != lifecycleSHA(publication.canonical) || prepared.DeterministicOperations != nil != withOperations {
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
	if approvedAuthority.Run.ID != run.ID || approvedAuthority.Package.ID != packageRow.ID || approvedAuthority.PackageApproval.ApprovalID != approved.PackageApprovalID || approvedAuthority.Ticket.TicketID != publication.result.Ticket.TicketID || approvedAuthority.TicketRevision.ID != publication.result.Revision.ID || approvedAuthority.DeterministicOperations != nil != withOperations {
		t.Fatalf("approved-authority readback = %#v", approvedAuthority)
	}
	if string(approvedAuthority.DeliveryTicket.Bytes) != string(publication.canonical) || approvedAuthority.DeliveryTicket.SHA256 != lifecycleSHA(publication.canonical) || len(approvedAuthority.TicketProjection.ValidationCommands) != 1 || strings.TrimSpace(approvedAuthority.TicketProjection.ValidationCommands[0].Command) == "" {
		t.Fatalf("approved authority retained bytes/projection = %#v", approvedAuthority)
	}
	if withOperations && string(approvedAuthority.DeterministicOperations.Bytes) != string(prepare.DeterministicOperations.Bytes) {
		t.Fatal("approved authority changed deterministic operations bytes")
	}

	// Continue through the existing assignment, deterministic-outcome,
	// execution-mode, and adaptive-attempt owners. External model execution is
	// not called.
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
	adaptive, err := executor.NewAdaptiveExecutionAttemptService(fixture.store, sourceReader)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := adaptive.Prepare(fixture.ctx, executor.AdaptiveExecutionAttemptInput{RunID: approved.Run.RunID, Adapter: "codex", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Mode != executor.ExecutionModeAbsent || !attempt.AdaptiveDispatchRequired || attempt.Attempt == nil || attempt.Attempt.AttemptNumber != 1 || attempt.InputArtifact == nil || len(attempt.InputBytes) == 0 {
		t.Fatalf("adaptive preparation = %#v", attempt)
	}
	if countRows(t, fixture.store, "plans") != 0 || countRows(t, fixture.store, "plan_passes") != 0 {
		t.Fatal("remediation package created Plan or Pass linkage")
	}
	return remediationPackageWorkflowResult{packageRow: packageRow}
}

func countRows(t *testing.T, store *workflowstore.Store, table string) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
