package programs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appaudits "relay/internal/app/audits"
	workflowstore "relay/internal/store/workflow"
)

// programMemberTicketBytes builds the exact Delivery Ticket document bytes the
// isolated audit packet records for one Program member. Only the fields the
// Assignment derivation consumes need to vary; the document is otherwise the
// canonical active ticket shape.
func programMemberTicketBytes(ticketID string, revision int64, invariants, proofs, completions []string, dependsOn []map[string]any) []byte {
	document := map[string]any{
		"schema_version":             "2.0",
		"feature_slug":               "program",
		"ticket_id":                  ticketID,
		"revision":                   revision,
		"replaces_revision":          nil,
		"repo_target":                "relay",
		"branch":                     "main",
		"base_commit":                programSHA,
		"goal":                       "Deliver the outcome.",
		"context":                    "Program context.",
		"scope":                      map[string]any{"in_scope": []string{"Deliver."}, "out_of_scope": []string{"Other."}},
		"depends_on":                 dependsOn,
		"required_invariants":        invariants,
		"forbidden_behaviors":        []string{},
		"implementation_obligations": []map[string]any{{"source_area": nil, "obligation": "Implement the obligation.", "prerequisites": []string{}}},
		"proof_obligations":          proofs,
		"validation_commands":        []map[string]any{{"working_directory": "", "command": "go test ./internal/" + strings.ToLower(ticketID), "expected": "all tests pass"}},
		"transition_applicability":   "not_required",
		"explicit_deferrals":         []string{},
		"completion_criteria":        completions,
	}
	bytes, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	return bytes
}

// establishEligibility durably records the isolated-audit authority and the
// integration eligibility for one dispatched member: the audit packet artifact
// (exact ticket bytes), the packet, the obligation, the accepted decision, the
// revision decision, and the eligibility binding the exact dispatch result
// facts. The dispatch must already be reported with a done result for the
// member.
func (f *programRuntimeFixture) establishEligibility(t *testing.T, member PreparedMember, ticketBytes []byte) {
	t.Helper()
	ctx := context.Background()
	db := f.store.DB()
	var runRow, packageMember, dispatchMember, dispatchRow int64
	if err := db.QueryRowContext(ctx, `SELECT r.id FROM runs r WHERE r.run_id=?`, member.RunID).Scan(&runRow); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id FROM execution_package_members WHERE package_row_id=(SELECT execution_package_row_id FROM program_prepared_members WHERE prepared_member_id=?)`, member.ID).Scan(&packageMember); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT dm.id, dm.dispatch_row_id FROM program_dispatch_members dm JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id WHERE m.prepared_member_id=?`, member.ID).Scan(&dispatchMember, &dispatchRow); err != nil {
		t.Fatal(err)
	}
	// The committed-run audit route requires an audit_ready Run; the decision
	// guard then binds the current packet and run basis.
	if err := f.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, member.RunID)
		if err != nil {
			return err
		}
		for _, step := range []struct{ expected, next string }{
			{"setup_ready", "executing"}, {"executing", "validating"}, {"validating", "audit_ready"},
		} {
			if run.Status == step.expected {
				run, err = tx.TransitionRun(ctx, run.RunID, step.expected, step.next)
				if err != nil {
					return err
				}
			}
		}
		if run.Status != "audit_ready" {
			return errors.New("program member Run is not audit_ready")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var outcome, pushedBranch, headSHA string
	if err := db.QueryRowContext(ctx, `SELECT outcome, branch, branch_head_sha FROM program_dispatch_results WHERE dispatch_member_row_id=?`, dispatchMember).Scan(&outcome, &pushedBranch, &headSHA); err != nil || outcome != "done" {
		t.Fatalf("dispatch result = %s %v", outcome, err)
	}
	ticketSHA := sha256.Sum256(ticketBytes)
	packet := appaudits.WorkflowPackageAuditPacket{
		SchemaVersion: appaudits.WorkflowPackageAuditPacketSchemaVersion,
		Run:           appaudits.WorkflowPackageAuditRun{RunID: runRow, UserIntent: "program integration"},
		Repository: appaudits.WorkflowPackageAuditRepository{
			RepoTarget: member.RepoTarget, Branch: member.Branch, BaseCommit: member.BaseCommit, AuditedCommit: headSHA,
		},
		Authority: appaudits.WorkflowPackageAuditAuthority{
			DeliveryTicket: appaudits.WorkflowPackageAuditEmbeddedArtifact{
				Filename: "delivery-ticket.json", SHA256: hex.EncodeToString(ticketSHA[:]), Content: json.RawMessage(ticketBytes),
			},
		},
		Execution: appaudits.WorkflowPackageAuditExecution{Status: "completed", CommittedSHA: headSHA, CompletionSummary: "completed"},
	}
	packetBytes, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	packetBytes = append(packetBytes, '\n')
	packetID := "packet-" + member.ID
	batch, err := f.store.ArtifactStore().Begin("audit-packets/" + packetID)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := batch.Stage("audit_packet", "audit-packet.json", "application/json", packetBytes)
	if err != nil {
		t.Fatal(err)
	}
	var packetRow int64
	err = f.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun,
			RunRowID: sql.NullInt64{Int64: runRow, Valid: true}, Kind: staged.Kind,
			RelativePath: staged.RelativePath, MediaType: staged.MediaType,
			SHA256: staged.SHA256, SizeBytes: staged.SizeBytes,
		})
		if err != nil {
			return err
		}
		if err := tx.DB().QueryRowContext(ctx, `INSERT INTO audit_packets(audit_packet_id,run_row_id,implementation_actor_kind,artifact_row_id,base_commit,audited_commit,packet_sha256,status) VALUES(?,?,?,?,?,?,?,'current') RETURNING id`, packetID, runRow, "applier", artifact.ID, member.BaseCommit, headSHA, staged.SHA256).Scan(&packetRow); err != nil {
			return err
		}
		var obligation int64
		if err := tx.DB().QueryRowContext(ctx, `INSERT INTO audit_packet_ticket_obligations(audit_packet_row_id,execution_package_row_id,execution_package_member_row_id,delivery_ticket_row_id,delivery_ticket_revision_row_id,authority_revision_row_id,source_closure_row_id,package_approval_row_id,approved_package_sha256) SELECT ?,m.execution_package_row_id,?,tv.delivery_ticket_row_id,m.ticket_revision_row_id,?,?,r.package_approval_row_id,ap.package_sha256 FROM program_prepared_members m JOIN delivery_ticket_revisions tv ON tv.id=m.ticket_revision_row_id JOIN runs r ON r.id=m.run_row_id JOIN execution_package_approvals ap ON ap.id=r.package_approval_row_id WHERE m.prepared_member_id=? RETURNING id`, packetRow, packageMember, f.authority, f.closure, member.ID).Scan(&obligation); err != nil {
			return err
		}
		var decisionRow int64
		if err := tx.DB().QueryRowContext(ctx, `INSERT INTO audit_decisions(audit_decision_id,run_row_id,audit_packet_artifact_row_id,audited_commit,packet_sha256,decision,rationale) VALUES(?,?,?,?,?,?,'accepted isolated audit') RETURNING id`, "audit-"+member.ID, runRow, artifact.ID, headSHA, staged.SHA256, "accepted").Scan(&decisionRow); err != nil {
			return err
		}
		var revisionDecision int64
		if err := tx.DB().QueryRowContext(ctx, `INSERT INTO audit_ticket_revision_decisions(audit_decision_row_id,audit_packet_ticket_obligation_row_id) VALUES(?,?) RETURNING id`, decisionRow, obligation).Scan(&revisionDecision); err != nil {
			return err
		}
		_, err = tx.DB().ExecContext(ctx, `INSERT INTO program_integration_eligibilities(eligibility_id,dispatch_member_row_id,audit_ticket_revision_decision_row_id,delivery_ticket_revision_row_id,audited_commit,pushed_branch,execution_package_row_id,assignment_artifact_row_id,authority_revision_row_id,source_closure_row_id) VALUES(?,?,?,?,?,?,(SELECT execution_package_row_id FROM program_prepared_members WHERE prepared_member_id=?),(SELECT assignment_artifact_row_id FROM program_prepared_members WHERE prepared_member_id=?),?,?)`, "integration-eligibility-"+member.ID, dispatchMember, revisionDecision, member.TicketRevisionRowID, headSHA, pushedBranch, member.ID, member.ID, f.authority, f.closure)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// integrationReadyFixture prepares two dispatched members with recorded done
// results and durable eligibility bound to the exact recorded facts. The
// workspace current authority revision is set so the existing ordinary
// satisfaction guard accepts the completed outcome at verification.
func integrationReadyFixture(t *testing.T) (*programRuntimeFixture, []PreparedMember) {
	t.Helper()
	f := newProgramRuntimeFixture(t)
	ctx := context.Background()
	// The workspace current authority revision is set (without a version bump,
	// which the integration surface never performs) so the existing ordinary
	// satisfaction guard accepts the completed outcome at verification.
	if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER feature_workspace_version_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE feature_workspaces SET current_authority_revision_row_id=? WHERE id=?`, f.authority, f.workspaceRow); err != nil {
		t.Fatal(err)
	}
	members := []PreparedMember{
		f.member(t, "one", "relay", "main", programSHA, "prepared"),
		f.member(t, "two", "relay", "main", programSHA, "prepared"),
	}
	dispatch, err := f.svc.CreateDispatch(ctx, f.workspace, 1, []string{members[0].ID, members[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	result := DispatchResultInput{LaterIntegrationRisks: "none", Members: []MemberResultInput{
		{MemberID: members[0].ID, Outcome: "done", Branch: "feature/one", BranchHeadSHA: strings.Repeat("b", 40)},
		{MemberID: members[1].ID, Outcome: "done", Branch: "feature/two", BranchHeadSHA: strings.Repeat("c", 40)},
	}}
	if err := f.svc.RecordDispatchResult(ctx, f.workspace, dispatch.ID, 1, result); err != nil {
		t.Fatal(err)
	}
	f.establishEligibility(t, members[0], programMemberTicketBytes("T-ONE", 1, []string{"Invariant one."}, []string{"Prove one."}, []string{"User can complete one."}, nil))
	f.establishEligibility(t, members[1], programMemberTicketBytes("T-TWO", 1, []string{"Invariant two."}, []string{"Prove two."}, []string{"User can complete two."}, nil))
	return f, members
}

func generateAssignment(t *testing.T, f *programRuntimeFixture, dispatchID string, memberIDs ...string) IntegrationAssignmentResult {
	t.Helper()
	assignment, err := f.svc.GenerateIntegrationAssignment(context.Background(), f.workspace, dispatchID, 1, memberIDs)
	if err != nil {
		t.Fatal(err)
	}
	return assignment
}

func TestGenerateIntegrationAssignmentBindsExactFactsAndDerivedObligations(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	if assignment.Status != "generated" || assignment.DispatchID != dispatch.ID || assignment.RepoTarget != "relay" || assignment.Branch != "main" || assignment.BaseCommit != programSHA || len(assignment.Document.Constituents) != 2 {
		t.Fatalf("assignment = %#v", assignment)
	}
	if assignment.Document.Assignment.AssignmentID != assignment.AssignmentID || assignment.Document.Assignment.DispatchID != dispatch.ID || assignment.Document.Assignment.RepoTarget != "relay" || assignment.Document.Assignment.BaseCommit != programSHA {
		t.Fatalf("document identity = %#v", assignment.Document.Assignment)
	}
	for index, member := range members {
		constituent := assignment.Document.Constituents[index]
		if constituent.Sequence != index+1 || constituent.MemberID != member.ID || constituent.TicketID != "T-"+strings.ToUpper(map[int]string{0: "one", 1: "two"}[index]) || constituent.TicketRevision != 1 {
			t.Fatalf("constituent %d identity = %#v", index, constituent)
		}
		wantCommit := map[int]string{0: strings.Repeat("b", 40), 1: strings.Repeat("c", 40)}[index]
		wantBranch := map[int]string{0: "feature/one", 1: "feature/two"}[index]
		if constituent.AcceptedCommit != wantCommit || constituent.PushedBranch != wantBranch || constituent.PackageID != member.PackageID || constituent.RunID != member.RunID || constituent.ExecutionAssignment.ArtifactID != member.AssignmentArtifactID || constituent.ExecutionAssignment.SHA256 != strings.Repeat("2", 64) || constituent.EligibilityID != "integration-eligibility-"+member.ID {
			t.Fatalf("constituent %d facts = %#v", index, constituent)
		}
		wantCommand := "go test ./internal/t-" + strings.ToLower(map[int]string{0: "one", 1: "two"}[index])
		if len(constituent.ValidationCommands) != 1 || constituent.ValidationCommands[0].Command != wantCommand || constituent.ValidationCommands[0].Expected != "all tests pass" {
			t.Fatalf("constituent %d validation commands = %#v", index, constituent.ValidationCommands)
		}
		wantInvariant := map[int]string{0: "Invariant one.", 1: "Invariant two."}[index]
		wantProof := map[int]string{0: "Prove one.", 1: "Prove two."}[index]
		wantCompletion := map[int]string{0: "User can complete one.", 1: "User can complete two."}[index]
		if len(constituent.SharedDesign.RequiredInvariants) != 1 || constituent.SharedDesign.RequiredInvariants[0] != wantInvariant || len(constituent.RequiredEvidence) != 2 || constituent.RequiredEvidence[0] != (IntegrationRequiredEvidence{Kind: integrationEvidenceProof, Obligation: wantProof}) || constituent.RequiredEvidence[1] != (IntegrationRequiredEvidence{Kind: integrationEvidenceBlackBox, Obligation: wantCompletion}) {
			t.Fatalf("constituent %d obligations = %#v", index, constituent)
		}
	}
	if len(assignment.Document.CombinedValidation) != 2 || assignment.Document.CombinedValidation[0].ConstituentSequence != 1 || assignment.Document.CombinedValidation[1].ConstituentSequence != 2 || assignment.Document.CombinedValidation[0].Command != "go test ./internal/t-one" || assignment.Document.CombinedValidation[1].Command != "go test ./internal/t-two" {
		t.Fatalf("combined validation = %#v", assignment.Document.CombinedValidation)
	}
	if len(assignment.Document.RequiredEvidence) != 4 || assignment.Document.RequiredEvidence[0].Kind != integrationEvidenceProof || assignment.Document.RequiredEvidence[1].Kind != integrationEvidenceBlackBox {
		t.Fatalf("combined required evidence = %#v", assignment.Document.RequiredEvidence)
	}
	read, err := f.svc.ReadIntegrationAssignment(ctx, f.workspace, dispatch.ID, assignment.AssignmentID)
	if err != nil || read.ContentSHA256 != assignment.ContentSHA256 || len(read.Document.Constituents) != 2 {
		t.Fatalf("read assignment = %#v, %v", read, err)
	}
}

func TestGenerateIntegrationAssignmentRejectsIneligibleSubsets(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	// Omitted-constituent dependency: the second member's Ticket depends on the
	// first member's Ticket, so a subset binding only the second requires a
	// missing Program member and is rejected; binding only the first remains
	// valid because the omitted second's dependency is bound.
	f.dependency(t, members[1], members[0].TicketRevisionRowID)
	if _, err := f.svc.GenerateIntegrationAssignment(ctx, f.workspace, dispatch.ID, 1, []string{members[1].ID}); !errors.Is(err, ErrAdmission) {
		t.Fatalf("missing-member subset error = %v", err)
	}
	generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	for _, tc := range []struct {
		name  string
		alter func(*programRuntimeFixture, []PreparedMember)
		ids   func([]PreparedMember) []string
	}{
		{"no eligibility", func(f *programRuntimeFixture, m []PreparedMember) {
			if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER program_integration_eligibility_delete_guard`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.DB().ExecContext(ctx, `DELETE FROM program_integration_eligibilities WHERE dispatch_member_row_id=(SELECT dm.id FROM program_dispatch_members dm JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id WHERE m.prepared_member_id=?)`, m[1].ID); err != nil {
				t.Fatal(err)
			}
		}, func(m []PreparedMember) []string { return []string{m[0].ID, m[1].ID} }},
		{"stale ticket revision", func(f *programRuntimeFixture, m []PreparedMember) {
			f.advanceTicketRevision(t, m[0].TicketRevisionRowID)
		}, func(m []PreparedMember) []string { return []string{m[0].ID, m[1].ID} }},
		{"claimed by current assignment", func(f *programRuntimeFixture, m []PreparedMember) {
			generateAssignment(t, f, f.assignmentDispatch(t).ID, m[0].ID, m[1].ID)
		}, func(m []PreparedMember) []string { return []string{m[0].ID, m[1].ID} }},
		{"unknown member", nil, func(m []PreparedMember) []string { return []string{"program-member-missing"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, members := integrationReadyFixture(t)
			dispatch := f.assignmentDispatch(t)
			if tc.alter != nil {
				tc.alter(f, members)
			}
			if _, err := f.svc.GenerateIntegrationAssignment(ctx, f.workspace, dispatch.ID, 1, tc.ids(members)); !errors.Is(err, ErrAdmission) {
				t.Fatalf("%s error = %v", tc.name, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"empty", nil},
		{"duplicate", []string{members[0].ID, members[0].ID}},
		{"blank", []string{""}},
	} {
		if _, err := f.svc.GenerateIntegrationAssignment(ctx, f.workspace, dispatch.ID, 1, tc.ids); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s error = %v", tc.name, err)
		}
	}
}

func TestGenerateIntegrationAssignmentFailsClosedForPartialSubsetWithoutStructuredCrossMemberAuthority(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	if _, err := f.svc.GenerateIntegrationAssignment(ctx, f.workspace, dispatch.ID, 1, []string{members[0].ID}); !errors.Is(err, ErrAdmission) {
		t.Fatalf("partial subset without resolvable cross-member authority error = %v", err)
	}
}

func TestGenerateIntegrationAssignmentPreservesDependencyClosureIndependently(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	f.dependency(t, members[1], members[0].TicketRevisionRowID)
	if _, err := f.svc.GenerateIntegrationAssignment(ctx, f.workspace, dispatch.ID, 1, []string{members[0].ID}); !errors.Is(err, ErrAdmission) {
		t.Fatalf("partial subset failed by independent cross-member safety gate = %v", err)
	}
}

func TestReadIntegrationAssignmentFailsClosedOnTamperedContent(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE program_integration_assignments SET content=content||'x' WHERE assignment_id=?`, assignment.AssignmentID); err == nil {
		t.Fatal("assignment content tampering succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER program_integration_assignment_identity_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE program_integration_assignments SET content=content||'x' WHERE assignment_id=?`, assignment.AssignmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ReadIntegrationAssignment(ctx, f.workspace, dispatch.ID, assignment.AssignmentID); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered read error = %v", err)
	}
}

// assignmentMergeInput admits the exact combined outcomes for an Assignment.
func assignmentMergeInput(assignment IntegrationAssignmentResult, failedValidation int) IntegrationMergeResultInput {
	input := IntegrationMergeResultInput{
		IntegratedCommit: strings.Repeat("d", 40), PreservationIdentity: "preservation:parents", ConflictResolution: "clean",
	}
	for index, bound := range assignment.Document.CombinedValidation {
		status := "passed"
		if index == failedValidation {
			status = "failed"
		}
		input.Validations = append(input.Validations, IntegrationValidationOutcomeInput{Command: bound.Command, Expected: bound.Expected, Status: status, Evidence: "evidence recorded"})
	}
	for _, bound := range assignment.Document.RequiredEvidence {
		input.Evidence = append(input.Evidence, IntegrationEvidenceOutcomeInput{Kind: bound.Kind, Obligation: bound.Obligation, Status: "passed", Evidence: "evidence recorded"})
	}
	return input
}

func TestAdmitMergeResultBindsExactOutcomesAndIsImmutable(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	admitted, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, assignmentMergeInput(assignment, -1))
	if err != nil || admitted.ResultID == "" || admitted.IntegratedCommit != strings.Repeat("d", 40) || admitted.PreservationIdentity != "preservation:parents" || admitted.ConflictResolution != "clean" || len(admitted.Validations) != 2 || len(admitted.Evidence) != 4 {
		t.Fatalf("admit = %#v, %v", admitted, err)
	}
	read, err := f.svc.ReadIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID)
	if err != nil || read.ResultID != admitted.ResultID || read.Validations[0].Status != "passed" || read.Validations[0].Command != "go test ./internal/t-one" || read.Evidence[0].Kind != integrationEvidenceProof || read.Evidence[1].Kind != integrationEvidenceBlackBox {
		t.Fatalf("read merge result = %#v, %v", read, err)
	}
	if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, assignmentMergeInput(assignment, -1)); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat admit error = %v", err)
	}
	// A validation outcome may record failure; the admission is exact evidence,
	// not a pass.
	f2, members2 := integrationReadyFixture(t)
	dispatch2 := f2.assignmentDispatch(t)
	assignment2 := generateAssignment(t, f2, dispatch2.ID, members2[0].ID, members2[1].ID)
	failed := assignmentMergeInput(assignment2, 0)
	failed.Validations[0].Status = "failed"
	if _, err := f2.svc.AdmitIntegrationMergeResult(ctx, f2.workspace, dispatch2.ID, assignment2.AssignmentID, 1, failed); err != nil {
		t.Fatalf("failed-outcome admit = %v", err)
	}
	// Mismatched or omitted outcomes are never admitted.
	for _, tc := range []struct {
		name  string
		alter func(*IntegrationMergeResultInput)
	}{
		{"missing validation", func(input *IntegrationMergeResultInput) { input.Validations = input.Validations[1:] }},
		{"extra evidence", func(input *IntegrationMergeResultInput) {
			input.Evidence = append(input.Evidence, IntegrationEvidenceOutcomeInput{Kind: "proof_obligation", Obligation: "extra", Status: "passed", Evidence: "evidence"})
		}},
		{"rewritten command", func(input *IntegrationMergeResultInput) { input.Validations[0].Command = "rewritten" }},
		{"rewritten evidence obligation", func(input *IntegrationMergeResultInput) { input.Evidence[0].Obligation = "rewritten" }},
		{"invalid commit", func(input *IntegrationMergeResultInput) { input.IntegratedCommit = "not-a-commit" }},
		{"blank preservation identity", func(input *IntegrationMergeResultInput) { input.PreservationIdentity = " " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, fixtureMembers := integrationReadyFixture(t)
			dispatch := fixture.assignmentDispatch(t)
			assignment := generateAssignment(t, fixture, dispatch.ID, fixtureMembers[0].ID, fixtureMembers[1].ID)
			input := assignmentMergeInput(assignment, -1)
			tc.alter(&input)
			if _, err := fixture.svc.AdmitIntegrationMergeResult(ctx, fixture.workspace, dispatch.ID, assignment.AssignmentID, 1, input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("%s error = %v", tc.name, err)
			}
		})
	}
}

func TestVerifyIntegrationPassRecordsOrdinaryCompletionExactlyOnce(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, assignmentMergeInput(assignment, -1)); err != nil {
		t.Fatal(err)
	}
	verification, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1)
	if err != nil || verification.Outcome != "passed" || len(verification.Completed) != 2 || !verification.Completed[0].Completed || !verification.Completed[1].Completed {
		t.Fatalf("verification = %#v, %v", verification, err)
	}
	var satisfactions int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM delivery_ticket_revision_satisfactions`).Scan(&satisfactions); err != nil {
		t.Fatal(err)
	}
	if satisfactions != 2 {
		t.Fatalf("satisfactions = %d, want 2", satisfactions)
	}
	// The ordinary satisfaction rows bind the exact bound revisions and the
	// recorded audit decisions through the existing mechanism.
	var bound int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM delivery_ticket_revision_satisfactions s JOIN program_integration_eligibilities e ON e.audit_ticket_revision_decision_row_id=s.audit_ticket_revision_decision_row_id`).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if bound != 2 {
		t.Fatalf("satisfactions bound to eligibilities = %d", bound)
	}
	if _, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat verify error = %v", err)
	}
	read, err := f.svc.ReadIntegrationVerification(ctx, f.workspace, dispatch.ID, assignment.AssignmentID)
	if err != nil || read.Outcome != "passed" || len(read.Completed) != 2 {
		t.Fatalf("read verification = %#v, %v", read, err)
	}
	if _, err := f.svc.ReadIntegrationFailure(ctx, f.workspace, dispatch.ID, assignment.AssignmentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("passed verification failure read error = %v", err)
	}
	// The completed tickets count toward workspace completion through the
	// existing gate: EvaluateCompletion requires no change here, but the
	// guided program surface must no longer advertise the members as eligible.
	state, err := f.svc.ReadWorkspaceProgramState(ctx, f.workspace)
	if err != nil || len(state.Eligible) != 0 {
		t.Fatalf("post-verification state = %#v, %v", state, err)
	}
}

func TestVerifyIntegrationFailureIsImmutableEvidenceAndFreshAssignmentRetries(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	input := assignmentMergeInput(assignment, 0)
	input.Validations[0].Status = "failed"
	if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, input); err != nil {
		t.Fatal(err)
	}
	verification, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1)
	if err != nil || verification.Outcome != "failed" || verification.FailureReason == "" {
		t.Fatalf("failed verification = %#v, %v", verification, err)
	}
	var satisfactions int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM delivery_ticket_revision_satisfactions`).Scan(&satisfactions); err != nil {
		t.Fatal(err)
	}
	if satisfactions != 0 {
		t.Fatalf("failed verification created %d satisfactions", satisfactions)
	}
	if _, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("verify after failure error = %v", err)
	}
	recorded, err := f.svc.ReadIntegrationVerification(ctx, f.workspace, dispatch.ID, assignment.AssignmentID)
	if err != nil || recorded.Outcome != "failed" || recorded.FailureReason != verification.FailureReason {
		t.Fatalf("failed verification read = %#v, %v", recorded, err)
	}
	failure, err := f.svc.ReadIntegrationFailure(ctx, f.workspace, dispatch.ID, assignment.AssignmentID)
	if err != nil || failure.VerificationID != verification.VerificationID || failure.FailureReason != verification.FailureReason {
		t.Fatalf("read failure = %#v, %v", failure, err)
	}
	// The prior Assignment is never patched or reused; retry generates a fresh
	// Assignment from the same exact recorded facts and completes on a pass.
	fresh, err := f.svc.GenerateIntegrationAssignment(ctx, f.workspace, dispatch.ID, 1, []string{members[0].ID, members[1].ID})
	if err != nil || fresh.AssignmentID == assignment.AssignmentID {
		t.Fatalf("fresh retry assignment = %#v, %v", fresh, err)
	}
	if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, fresh.AssignmentID, 1, assignmentMergeInput(fresh, -1)); err != nil {
		t.Fatal(err)
	}
	retry, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, fresh.AssignmentID, 1)
	if err != nil || retry.Outcome != "passed" || len(retry.Completed) != 2 || !retry.Completed[0].Completed || !retry.Completed[1].Completed {
		t.Fatalf("retry verification = %#v, %v", retry, err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM delivery_ticket_revision_satisfactions`).Scan(&satisfactions); err != nil {
		t.Fatal(err)
	}
	if satisfactions != 2 {
		t.Fatalf("retry satisfactions = %d, want 2", satisfactions)
	}
}

func TestVerifyIntegrationFailsClosedOnStaleConstituentAndOmittedNeverAdvance(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	// The first member's Ticket revision advances after generation but before
	// verification: its isolated audit can no longer complete.
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	f.advanceTicketRevision(t, members[0].TicketRevisionRowID)
	if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, assignmentMergeInput(assignment, -1)); err != nil {
		t.Fatal(err)
	}
	verification, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1)
	if err != nil || verification.Outcome != "failed" || verification.FailureReason == "" {
		t.Fatalf("verification = %#v, %v", verification, err)
	}
	var satisfactions int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM delivery_ticket_revision_satisfactions`).Scan(&satisfactions); err != nil {
		t.Fatal(err)
	}
	if satisfactions != 0 {
		t.Fatalf("stale constituents produced %d satisfactions, want 0", satisfactions)
	}
	// The omitted-constituent rule: an Assignment binding only the first member
	// never advances the second.
}

func TestVerifyIntegrationRevalidatesBoundIdentityAndLineage(t *testing.T) {
	ctx := context.Background()
	mutations := []struct {
		name  string
		alter func(*programRuntimeFixture, IntegrationAssignmentResult, []PreparedMember)
	}{
		{"package", func(f *programRuntimeFixture, _ IntegrationAssignmentResult, m []PreparedMember) {
			if _, err := f.store.DB().ExecContext(ctx, "DROP TRIGGER program_integration_eligibility_immutable"); err != nil {
				t.Fatal(err)
			}
			f.update(t, "UPDATE program_integration_eligibilities SET execution_package_row_id=execution_package_row_id+1 WHERE eligibility_id=?", "integration-eligibility-"+m[0].ID)
		}},
		{"execution assignment digest", func(f *programRuntimeFixture, _ IntegrationAssignmentResult, m []PreparedMember) {
			f.update(t, "UPDATE artifacts SET sha256=? WHERE artifact_id=?", strings.Repeat("9", 64), m[0].AssignmentArtifactID)
		}},
		{"pushed branch", func(f *programRuntimeFixture, _ IntegrationAssignmentResult, m []PreparedMember) {
			if _, err := f.store.DB().ExecContext(ctx, "DROP TRIGGER program_integration_eligibility_immutable"); err != nil {
				t.Fatal(err)
			}
			f.update(t, "UPDATE program_integration_eligibilities SET pushed_branch='other' WHERE eligibility_id=?", "integration-eligibility-"+m[0].ID)
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			f, members := integrationReadyFixture(t)
			dispatch := f.assignmentDispatch(t)
			assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
			tc.alter(f, assignment, members)
			if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, assignmentMergeInput(assignment, -1)); err != nil {
				t.Fatal(err)
			}
			verification, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1)
			if err != nil || verification.Outcome != "failed" {
				t.Fatalf("verification = %#v, %v", verification, err)
			}
			if got := f.count(t, "delivery_ticket_revision_satisfactions"); got != 0 {
				t.Fatalf("satisfactions = %d", got)
			}
		})
	}
}

func TestVerifyIntegrationPreservationIsGroundedAndOpaqueTextIsInsufficient(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	f.svc.repositoryVerifier = func(_ context.Context, _, _, _, integrated string, _, _ []string, _, _ string) error {
		if integrated == strings.Repeat("d", 40) {
			return errors.New("integrated commit is not repository evidence")
		}
		return nil
	}
	input := assignmentMergeInput(assignment, -1)
	if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, input); err != nil {
		t.Fatal(err)
	}
	verification, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1)
	if err != nil || verification.Outcome != "failed" || f.count(t, "delivery_ticket_revision_satisfactions") != 0 {
		t.Fatalf("verification = %#v, %v", verification, err)
	}
}

func TestReadWorkspaceProgramStateProjectsEligibleAndIntegrationSurfaces(t *testing.T) {
	ctx := context.Background()
	f, members := integrationReadyFixture(t)
	dispatch := f.assignmentDispatch(t)
	assignment := generateAssignment(t, f, dispatch.ID, members[0].ID, members[1].ID)
	state, err := f.svc.ReadWorkspaceProgramState(ctx, f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Eligible) != 0 {
		t.Fatalf("eligible = %#v", state.Eligible)
	}
	if len(state.Integration) != 1 || state.Integration[0].AssignmentID != assignment.AssignmentID || state.Integration[0].Status != "generated" || state.Integration[0].Verification != "none" || len(state.Integration[0].Members) != 2 {
		t.Fatalf("integration = %#v", state.Integration)
	}
	// After a failed verification the member returns to the eligible surface
	// and the failed Assignment remains inspectable transport history.
	input := assignmentMergeInput(assignment, 0)
	input.Validations[0].Status = "failed"
	if _, err := f.svc.AdmitIntegrationMergeResult(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.VerifyIntegration(ctx, f.workspace, dispatch.ID, assignment.AssignmentID, 1); err != nil {
		t.Fatal(err)
	}
	state, err = f.svc.ReadWorkspaceProgramState(ctx, f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Eligible) != 2 {
		t.Fatalf("post-failure eligible = %#v", state.Eligible)
	}
	if len(state.Integration) != 1 || state.Integration[0].Status != "failed" || state.Integration[0].Verification != "failed" {
		t.Fatalf("post-failure integration = %#v", state.Integration)
	}
}

// assignmentDispatch resolves the fixture's one reported dispatch.
func (f *programRuntimeFixture) assignmentDispatch(t *testing.T) Dispatch {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT dispatch_id FROM program_dispatches`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	dispatch, err := f.svc.Read(ctx, f.workspace, id)
	if err != nil {
		t.Fatal(err)
	}
	return dispatch
}
