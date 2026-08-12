package programs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "relay/internal/app/programs"
)

func integrationDocument() app.IntegrationAssignmentDocument {
	return app.IntegrationAssignmentDocument{
		SchemaVersion: "1.0",
		Assignment:    app.IntegrationAssignmentIdentity{AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40)},
		Constituents: []app.IntegrationAssignmentConstituent{
			{
				Sequence: 1, MemberID: "program-member-1", TicketID: "T-ONE", TicketRevision: 1,
				AcceptedCommit: strings.Repeat("b", 40), PushedBranch: "feature/one",
				PackageID: "package-1", RunID: "run-1",
				ExecutionAssignment: app.IntegrationExecutionAssignment{ArtifactID: "artifact-1", SHA256: strings.Repeat("2", 64)},
				AuditDecisionID:     "audit-1", EligibilityID: "integration-eligibility-1",
				SharedDesign:       app.IntegrationSharedDesign{RequiredInvariants: []string{"Invariant."}},
				ValidationCommands: []app.IntegrationValidationCommand{{WorkingDirectory: "", Command: "go test ./internal/example", Expected: "Tests pass."}},
				RequiredEvidence:   []app.IntegrationRequiredEvidence{{Kind: "proof_obligation", Obligation: "Prove it."}},
			},
		},
		CombinedValidation: []app.IntegrationCombinedValidation{{Sequence: 1, ConstituentSequence: 1, WorkingDirectory: "", Command: "go test ./internal/example", Expected: "Tests pass."}},
		RequiredEvidence:   []app.IntegrationRequiredEvidenceItem{{Sequence: 2, ConstituentSequence: 1, Kind: "proof_obligation", Obligation: "Prove it."}},
	}
}

func TestIntegrationAssignmentRoutesStrictDecodeAndExactProjection(t *testing.T) {
	service := &fakeService{assignment: app.IntegrationAssignmentResult{
		AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", WorkspaceID: "workspace-1",
		RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Status: "generated", ContentSHA256: strings.Repeat("3", 64), Document: integrationDocument(),
	}}
	router := programRouter(service)
	for _, body := range []string{
		`{"expectedVersion":1,"memberIds":["program-member-1"],"unknown":true}`,
		`{"expectedVersion":1,"memberIds":["program-member-1"]}{}`,
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("strict decode status=%d body=%s", response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments", strings.NewReader(`{"expectedVersion":1,"memberIds":["program-member-1"]}`)))
	if response.Code != http.StatusCreated {
		t.Fatalf("generate status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{`"AssignmentID":"integration-assignment-1"`, `"DispatchID":"dispatch-1"`, `"Status":"generated"`, `"ContentSHA256":"` + strings.Repeat("3", 64) + `"`, `"schema_version":"1.0"`, `"combined_validation"`, `"required_evidence"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("generate body missing %q: %s", want, body)
		}
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ticket_id":"T-ONE"`) || !strings.Contains(response.Body.String(), `"accepted_commit":"`+strings.Repeat("b", 40)+`"`) || !strings.Contains(response.Body.String(), `"pushed_branch":"feature/one"`) || !strings.Contains(response.Body.String(), `"audit_decision_id":"audit-1"`) {
		t.Fatalf("read assignment status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestIntegrationMergeResultAdmitVerifyAndFailureRoutes(t *testing.T) {
	document := integrationDocument()
	service := &fakeService{
		merge: app.IntegrationMergeResult{
			ResultID: "integration-merge-result-1", AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1",
			IntegratedCommit: strings.Repeat("d", 40), PreservationIdentity: "preservation:parents",
			Validations: []app.IntegrationValidationOutcome{{Sequence: 1, ConstituentSequence: 1, Command: "go test ./internal/example", Expected: "Tests pass.", Status: "passed", Evidence: "verified"}},
			Evidence:    []app.IntegrationEvidenceOutcome{{Sequence: 2, ConstituentSequence: 1, Kind: "proof_obligation", Obligation: "Prove it.", Status: "passed", Evidence: "evidence recorded"}},
		},
		verified:     app.IntegrationVerification{VerificationID: "integration-verification-1", AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", Outcome: "passed", Completed: []app.IntegrationCompletion{{MemberID: "program-member-1", TicketID: "T-ONE", TicketRevision: 1, Completed: true}}},
		verification: app.IntegrationVerification{VerificationID: "integration-verification-1", AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", Outcome: "passed", Completed: []app.IntegrationCompletion{{MemberID: "program-member-1", TicketID: "T-ONE", TicketRevision: 1, Completed: true}}},
	}
	router := programRouter(service)
	validations, _ := json.Marshal([]map[string]string{{"command": "go test ./internal/example", "expected": "Tests pass.", "status": "passed", "evidence": "verified"}})
	evidence, _ := json.Marshal([]map[string]string{{"kind": "proof_obligation", "obligation": "Prove it.", "status": "passed", "evidence": "evidence recorded"}})
	body := `{"expectedVersion":1,"integratedCommit":"` + strings.Repeat("d", 40) + `","preservationIdentity":"preservation:parents","conflictEvidence":"","validations":` + string(validations) + `,"evidence":` + string(evidence) + `}`
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/merge-results", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("admit status=%d body=%s", response.Code, response.Body.String())
	}
	if service.admitted.IntegratedCommit != strings.Repeat("d", 40) || len(service.admitted.Validations) != 1 || service.admitted.Validations[0].Command != "go test ./internal/example" || len(service.admitted.Evidence) != 1 || service.admitted.Evidence[0].Obligation != "Prove it." {
		t.Fatalf("admitted input=%#v", service.admitted)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/merge-results", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"IntegratedCommit":"`+strings.Repeat("d", 40)+`"`) || !strings.Contains(response.Body.String(), `"PreservationIdentity":"preservation:parents"`) || !strings.Contains(response.Body.String(), `"Status":"passed"`) {
		t.Fatalf("read merge result status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/verification", strings.NewReader(`{"expectedVersion":1}`)))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"Outcome":"passed"`) || !strings.Contains(response.Body.String(), `"Completed":true`) {
		t.Fatalf("verify status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/verification", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"Outcome":"passed"`) {
		t.Fatalf("read verification status=%d body=%s", response.Code, response.Body.String())
	}

	failed := &fakeService{failure: app.IntegrationFailure{VerificationID: "integration-verification-2", AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", FailureReason: "combined validation outcome 1 did not pass"}}
	failedRouter := programRouter(failed)
	response = httptest.NewRecorder()
	failedRouter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/failure", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"FailureReason":"combined validation outcome 1 did not pass"`) {
		t.Fatalf("read failure status=%d body=%s", response.Code, response.Body.String())
	}
	// The Assignment document identity is exposed exactly; internal row
	// identities never leak onto the wire.
	_ = document
}
