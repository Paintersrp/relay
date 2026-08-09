package tickets

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appoperations "relay/internal/app/operations"
	apptickets "relay/internal/app/tickets"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWorkflow struct {
	frontier                   apptickets.Frontier
	err                        error
	publishInput               apptickets.PublishInput
	publishReference           *appoperations.RemediationAuthoringReference
	replacementInput           apptickets.PublishInput
	approvalInput              apptickets.ApproveInput
	approvalSourceClosureRowID int64
	priorityTicketID           string
	priorityExternalPriority   int64
	frontierWorkspaceID        string
	selectionInput             apptickets.SelectInput
	briefAdmissionInput        apptickets.TicketDesignBriefAdmissionInput
	reviewCompletionInput      apptickets.CompleteBriefReviewInput
	publishCalled              bool
	replacementCalled          bool
	approvalCalled             bool
	priorityCalled             bool
	frontierCalled             bool
	selectionCalled            bool
	briefAdmissionCalled       bool
	reviewCompletionCalled     bool
}

func (f *fakeWorkflow) Publish(_ context.Context, input apptickets.PublishInput, reference *appoperations.RemediationAuthoringReference) (apptickets.PublishedRevision, error) {
	f.publishCalled = true
	f.publishInput = input
	if reference != nil {
		copy := *reference
		f.publishReference = &copy
	} else {
		f.publishReference = nil
	}
	return publishedRevision(input), f.err
}

func (f *fakeWorkflow) ReplaceDependencies(_ context.Context, input apptickets.PublishInput) (apptickets.PublishedRevision, error) {
	f.replacementCalled = true
	f.replacementInput = input
	return publishedRevision(input), f.err
}

func (f *fakeWorkflow) Approve(_ context.Context, input apptickets.ApproveInput, sourceClosureRowID int64) (RevisionApproval, error) {
	f.approvalCalled = true
	f.approvalInput = input
	f.approvalSourceClosureRowID = sourceClosureRowID
	return RevisionApproval{RevisionRowID: input.RevisionRowID, SourceClosureRowID: sourceClosureRowID}, f.err
}

func (f *fakeWorkflow) UpdatePriority(_ context.Context, ticketID string, externalPriority int64) (DeliveryTicket, error) {
	f.priorityCalled = true
	f.priorityTicketID = ticketID
	f.priorityExternalPriority = externalPriority
	return DeliveryTicket{TicketID: ticketID, ExternalPriority: externalPriority}, f.err
}

func (f *fakeWorkflow) ListFrontier(_ context.Context, workspaceID string) (apptickets.Frontier, error) {
	f.frontierCalled = true
	f.frontierWorkspaceID = workspaceID
	return f.frontier, f.err
}

func (f *fakeWorkflow) Select(_ context.Context, input apptickets.SelectInput) (apptickets.SelectionResult, error) {
	f.selectionCalled = true
	f.selectionInput = input
	return apptickets.SelectionResult{}, f.err
}

func (f *fakeWorkflow) AdmitTicketDesignBrief(_ context.Context, input apptickets.TicketDesignBriefAdmissionInput) (apptickets.TicketDesignBriefAdmissionResult, error) {
	f.briefAdmissionCalled = true
	f.briefAdmissionInput = input
	return apptickets.TicketDesignBriefAdmissionResult{
		Brief:    workflowstore.TicketDesignBrief{BriefID: "brief-api-1", ArtifactSha256: strings.Repeat("a", 64), ArtifactSizeBytes: int64(len(input.Bytes))},
		Filename: "checkout.ticket-P1-T1.r1.design-brief.md",
	}, f.err
}

func (f *fakeWorkflow) CompleteTicketDesignBriefReview(_ context.Context, input apptickets.CompleteBriefReviewInput) (apptickets.TicketDesignBriefReviewResult, error) {
	f.reviewCompletionCalled = true
	f.reviewCompletionInput = input
	return apptickets.TicketDesignBriefReviewResult{
		Brief:  workflowstore.TicketDesignBrief{BriefID: "brief-api-1"},
		Review: workflowstore.TicketDesignBriefReview{ReviewID: "brief-review-api-1", ReviewerIdentity: input.ReviewerIdentity, Disposition: string(input.Disposition), CompletedAt: "2026-08-08T00:00:00.000000000Z"},
	}, f.err
}

func (f *fakeWorkflow) called() bool {
	return f.publishCalled || f.replacementCalled || f.approvalCalled || f.priorityCalled || f.frontierCalled || f.selectionCalled || f.briefAdmissionCalled || f.reviewCompletionCalled
}

func publishedRevision(input apptickets.PublishInput) apptickets.PublishedRevision {
	return apptickets.PublishedRevision{
		Ticket:    workflowstore.DeliveryTicket{TicketID: input.TicketID, ExternalPriority: input.ExternalPriority},
		Revision:  workflowstore.DeliveryTicketRevision{ID: 4, RevisionNumber: input.ExpectedRevisionNumber + 1, SourceClosureRowID: input.Revision.SourceClosureRowID},
		Canonical: apptickets.StoredArtifact{RelativePath: "delivery-tickets/t/revisions/1/delivery-ticket.json"},
		Rendered:  apptickets.StoredArtifact{RelativePath: "delivery-tickets/t/revisions/1/delivery-ticket.md"},
	}
}

type fakeRead struct {
	detail  apptickets.TicketDetail
	history []RevisionHistory
	err     error
}

func (f *fakeRead) Read(context.Context, string) (apptickets.TicketDetail, error) {
	return f.detail, f.err
}
func (f *fakeRead) ListHistory(context.Context, string) ([]RevisionHistory, error) {
	return f.history, f.err
}

func ticketRouter(workflow WorkflowService, read ReadService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(workflow, read))
	return router
}

func TestFrontierRouteProjectsOwnerTieReasons(t *testing.T) {
	service := &fakeWorkflow{frontier: apptickets.Frontier{WorkspaceID: "workspace-api", Entries: []apptickets.FrontierEntry{{TicketID: "ticket-2", RevisionRowID: 9, RevisionNumber: 2, ExternalPriority: 66, CreatedAt: "2026-01-01T00:00:00Z", RepoTarget: "relay", Branch: "main", SourceClosureRowID: 12, TieWithPrevious: &apptickets.AdjacentTieReason{PreviousTicketID: "ticket-1", Rule: apptickets.FrontierTieRuleStableTicketID}}}}}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api/tickets/frontier", nil))
	if response.Code != http.StatusOK || service.frontierWorkspaceID != "workspace-api" || !strings.Contains(response.Body.String(), `"previousTicketId":"ticket-1"`) || !strings.Contains(response.Body.String(), `"rule":"stable_ticket_id"`) {
		t.Fatalf("response = %d %s workspace = %q", response.Code, response.Body.String(), service.frontierWorkspaceID)
	}
}

func TestFrontierRouteRejectsQueryInput(t *testing.T) {
	for _, query := range []string{"packetId=packet-1", "operationId=planner.ticket_frontier", "unexpected=value"} {
		t.Run(query, func(t *testing.T) {
			service := &fakeWorkflow{}
			response := httptest.NewRecorder()
			ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api/tickets/frontier?"+query, nil))
			if response.Code != http.StatusBadRequest || service.frontierCalled {
				t.Fatalf("response = %d %s frontier called = %t", response.Code, response.Body.String(), service.frontierCalled)
			}
		})
	}
}

func TestPublishRouteBuildsDirectOwnerInput(t *testing.T) {
	service := &fakeWorkflow{}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/ticket-1/revisions", strings.NewReader(ticketMutationJSON("", "0"))))
	if response.Code != http.StatusCreated || !service.publishCalled || service.publishReference != nil || service.publishInput.TicketID != "ticket-1" || service.publishInput.WorkspaceID != "workspace-api" || service.publishInput.Revision.SourceClosureRowID != 12 || len(service.publishInput.Revision.CanonicalJSON) == 0 {
		t.Fatalf("response = %d %s input = %#v reference = %#v", response.Code, response.Body.String(), service.publishInput, service.publishReference)
	}
}

func TestPublishRouteBindsRemediationFieldsToOwnerInput(t *testing.T) {
	service := &fakeWorkflow{}
	seedID := "remediation-seed-1"
	packetID := "planner-packet-1"
	packetSHA := strings.Repeat("a", 64)
	body := ticketMutationJSON(`"remediationSeedId":"`+seedID+`","authoringPacketId":"`+packetID+`","expectedAuthoringPacketSha256":"`+packetSHA+`"`, "0")
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/ticket-1/revisions", strings.NewReader(body)))
	if response.Code != http.StatusCreated || !service.publishCalled || service.publishInput.RemediationSeedID != seedID || service.publishReference == nil || service.publishReference.PacketID != packetID || service.publishReference.ExpectedPacketSHA256 != packetSHA {
		t.Fatalf("response = %d %s input = %#v reference = %#v", response.Code, response.Body.String(), service.publishInput, service.publishReference)
	}
}

func TestPublishRouteRejectsPartialRemediationFields(t *testing.T) {
	for _, extra := range []string{
		`"remediationSeedId":"seed-1"`,
		`"authoringPacketId":"packet-1"`,
		`"expectedAuthoringPacketSha256":"` + strings.Repeat("a", 64) + `"`,
		`"remediationSeedId":"seed-1","authoringPacketId":"packet-1"`,
		`"remediationSeedId":"seed-1","expectedAuthoringPacketSha256":"` + strings.Repeat("a", 64) + `"`,
		`"authoringPacketId":"packet-1","expectedAuthoringPacketSha256":"` + strings.Repeat("a", 64) + `"`,
	} {
		service := &fakeWorkflow{}
		response := httptest.NewRecorder()
		ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/ticket-1/revisions", strings.NewReader(ticketMutationJSON(extra, "0"))))
		if response.Code != http.StatusBadRequest || service.publishCalled {
			t.Fatalf("extra = %s response = %d %s publish called = %t", extra, response.Code, response.Body.String(), service.publishCalled)
		}
	}
}

func TestWorkflowMutationRoutesRejectLegacyPacketFields(t *testing.T) {
	const legacyFields = `"packetId":"operator-packet","operationId":"local_operator.ticket_workflow","requiredDependencies":[]`
	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "publish", method: http.MethodPost, path: "/feature-workspaces/workspace-api/tickets/ticket-1/revisions", body: ticketMutationJSON(legacyFields, "0")},
		{name: "replace dependencies", method: http.MethodPost, path: "/feature-workspaces/workspace-api/tickets/ticket-1/dependencies", body: ticketMutationJSON(legacyFields, "1")},
		{name: "approve", method: http.MethodPost, path: "/delivery-tickets/ticket-1/approvals", body: approvalRequestJSON(legacyFields)},
		{name: "priority", method: http.MethodPatch, path: "/delivery-tickets/ticket-1/priority", body: priorityRequestJSON(legacyFields)},
		{name: "select", method: http.MethodPost, path: "/feature-workspaces/workspace-api/tickets/selection", body: selectionRequestJSON(legacyFields)},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeWorkflow{}
			response := httptest.NewRecorder()
			ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			if response.Code != http.StatusBadRequest || service.called() {
				t.Fatalf("response = %d %s workflow called = %t", response.Code, response.Body.String(), service.called())
			}
		})
	}
}

func TestPublishRouteMapsRemediationSeedConflict(t *testing.T) {
	service := &fakeWorkflow{err: apptickets.ErrRemediationSeed}
	body := ticketMutationJSON(`"remediationSeedId":"seed-1","authoringPacketId":"packet-1","expectedAuthoringPacketSha256":"`+strings.Repeat("a", 64)+`"`, "0")
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/ticket-1/revisions", strings.NewReader(body)))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"CONFLICT"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestDependencyReplacementRoutePassesDirectDomainInput(t *testing.T) {
	service := &fakeWorkflow{}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/ticket-1/dependencies", strings.NewReader(ticketMutationJSON("", "1"))))
	if response.Code != http.StatusCreated || !service.replacementCalled || service.replacementInput.TicketID != "ticket-1" || service.replacementInput.ExpectedRevisionNumber != 1 || service.replacementInput.Revision.SourceClosureRowID != 12 {
		t.Fatalf("response = %d %s input = %#v", response.Code, response.Body.String(), service.replacementInput)
	}
}

func TestDependencyReplacementRouteRejectsRemediationFields(t *testing.T) {
	service := &fakeWorkflow{}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/ticket-1/dependencies", strings.NewReader(ticketMutationJSON(`"remediationSeedId":"seed-1"`, "1"))))
	if response.Code != http.StatusBadRequest || service.replacementCalled {
		t.Fatalf("response = %d %s replacement called = %t", response.Code, response.Body.String(), service.replacementCalled)
	}
}

func TestApproveAndPriorityRoutesPassDirectDomainArguments(t *testing.T) {
	service := &fakeWorkflow{}
	router := ticketRouter(service, &fakeRead{})
	approval := httptest.NewRecorder()
	router.ServeHTTP(approval, httptest.NewRequest(http.MethodPost, "/delivery-tickets/ticket-1/approvals", strings.NewReader(approvalRequestJSON(""))))
	if approval.Code != http.StatusCreated || !service.approvalCalled || service.approvalInput.TicketID != "ticket-1" || service.approvalInput.RevisionRowID != 9 || service.approvalInput.AuthorityRevisionID != "authority-1" || service.approvalSourceClosureRowID != 12 {
		t.Fatalf("approval = %d %s input = %#v source closure = %d", approval.Code, approval.Body.String(), service.approvalInput, service.approvalSourceClosureRowID)
	}
	priority := httptest.NewRecorder()
	router.ServeHTTP(priority, httptest.NewRequest(http.MethodPatch, "/delivery-tickets/ticket-1/priority", strings.NewReader(priorityRequestJSON(""))))
	if priority.Code != http.StatusOK || !service.priorityCalled || service.priorityTicketID != "ticket-1" || service.priorityExternalPriority != 66 {
		t.Fatalf("priority = %d %s ticket = %q external priority = %d", priority.Code, priority.Body.String(), service.priorityTicketID, service.priorityExternalPriority)
	}
}

func TestSelectionRouteMapsAtomicConflict(t *testing.T) {
	service := &fakeWorkflow{err: apptickets.ErrSelectionConflict}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/selection", strings.NewReader(selectionRequestJSON(""))))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"CONFLICT"`) || service.selectionInput.TicketID != "ticket-1" || service.selectionInput.RevisionRowID != 9 {
		t.Fatalf("response = %d %s selection = %#v", response.Code, response.Body.String(), service.selectionInput)
	}
}

func TestTicketDesignBriefAdmissionRouteDelegatesServerResolvedBasis(t *testing.T) {
	service := &fakeWorkflow{}
	response := httptest.NewRecorder()
	body := `{"bytesBase64":"` + base64.StdEncoding.EncodeToString([]byte("# Ticket Design Brief\n")) + `","createdIdentity":"planner"}`
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/ticket-design-briefs", strings.NewReader(body)))
	if response.Code != http.StatusCreated || !service.briefAdmissionCalled || service.briefAdmissionInput.WorkspaceID != "workspace-api" || service.briefAdmissionInput.CreatedIdentity != "planner" || string(service.briefAdmissionInput.Bytes) != "# Ticket Design Brief\n" {
		t.Fatalf("response = %d %s admission = %#v", response.Code, response.Body.String(), service.briefAdmissionInput)
	}
	if !strings.Contains(response.Body.String(), `"briefId":"brief-api-1"`) || !strings.Contains(response.Body.String(), `"filename":"checkout.ticket-P1-T1.r1.design-brief.md"`) {
		t.Fatalf("admission response body = %s", response.Body.String())
	}
}

func TestTicketDesignBriefAdmissionRouteLeavesReplacementAdmissionToOwner(t *testing.T) {
	service := &fakeWorkflow{}
	router := ticketRouter(service, &fakeRead{})
	body := `{"bytesBase64":"IyBUaWNrZXQgRGVzaWduIEJyaWVmCg==","createdIdentity":"planner"}`
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/ticket-design-briefs", strings.NewReader(body)))
		if response.Code != http.StatusCreated || !service.briefAdmissionCalled || service.briefAdmissionInput.WorkspaceID != "workspace-api" {
			t.Fatalf("attempt %d response=%d %s input=%#v", attempt, response.Code, response.Body.String(), service.briefAdmissionInput)
		}
	}
}

func TestTicketDesignBriefAdmissionRouteRejectsInvalidBytes(t *testing.T) {
	service := &fakeWorkflow{}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/ticket-design-briefs", strings.NewReader(`{"bytesBase64":"not-base64","createdIdentity":"planner"}`)))
	if response.Code != http.StatusBadRequest || service.briefAdmissionCalled {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestReviewCompletionRouteRecordsBoundedDispositionOnServerResolvedBrief(t *testing.T) {
	service := &fakeWorkflow{}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/ticket-design-briefs/review-completions", strings.NewReader(`{"reviewerIdentity":"auditor","disposition":"ready_for_approval"}`)))
	if response.Code != http.StatusCreated || !service.reviewCompletionCalled || service.reviewCompletionInput.WorkspaceID != "workspace-api" || service.reviewCompletionInput.ReviewerIdentity != "auditor" || service.reviewCompletionInput.Disposition != apptickets.TicketDesignBriefReviewReadyForApproval {
		t.Fatalf("response = %d %s completion = %#v", response.Code, response.Body.String(), service.reviewCompletionInput)
	}
	if !strings.Contains(response.Body.String(), `"reviewId":"brief-review-api-1"`) || !strings.Contains(response.Body.String(), `"reviewerIdentity":"auditor"`) || !strings.Contains(response.Body.String(), `"disposition":"ready_for_approval"`) {
		t.Fatalf("completion response body = %s", response.Body.String())
	}
}

func TestReviewCompletionRouteRejectsMissingIdentity(t *testing.T) {
	service := &fakeWorkflow{err: apptickets.ErrTicketDesignBriefReview}
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/ticket-design-briefs/review-completions", strings.NewReader(`{"disposition":"ready_for_approval"}`)))
	if response.Code != http.StatusBadRequest || !service.reviewCompletionCalled || strings.TrimSpace(service.reviewCompletionInput.ReviewerIdentity) != "" {
		t.Fatalf("response = %d %s completion = %#v", response.Code, response.Body.String(), service.reviewCompletionInput)
	}
}

func TestReviewCompletionRouteRejectsClientBriefIdentityAndInvalidDisposition(t *testing.T) {
	for _, body := range []string{
		`{"reviewerIdentity":"auditor","disposition":"ready_for_approval","briefId":"other-brief"}`,
		`{"reviewerIdentity":"auditor","disposition":"findings_attached"}`,
		`{"reviewerIdentity":"auditor"}`,
	} {
		service := &fakeWorkflow{err: apptickets.ErrTicketDesignBriefReview}
		response := httptest.NewRecorder()
		ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/ticket-design-briefs/review-completions", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s response=%d %s", body, response.Code, response.Body.String())
		}
		if service.reviewCompletionCalled {
			t.Fatalf("invalid review request reached owner: %#v", service.reviewCompletionInput)
		}
	}
}

func ticketRevisionJSON() string {
	return `{"repoTarget":"relay","branch":"main","baseCommit":"abc","sourceClosureRowId":12,"sourcePath":"tickets/ticket-1.json","goal":"Ship ticket","context":"Exact context","transitionApplicability":"not_required","canonicalJson":{"ticket":"ticket-1"},"renderedMarkdown":"# Ticket\\n","members":[{"kind":"implementation_obligation","path":"internal/app/tickets","text":"Derive readiness."}],"dependencies":[]}`
}

func ticketMutationJSON(extra, expectedRevisionNumber string) string {
	if extra != "" {
		extra += ","
	}
	return `{` + extra + `"externalPriority":66,"expectedRevisionNumber":` + expectedRevisionNumber + `,"revision":` + ticketRevisionJSON() + `}`
}

func approvalRequestJSON(extra string) string {
	if extra != "" {
		extra += ","
	}
	return `{` + extra + `"revisionRowId":9,"authorityRevisionId":"authority-1","sourceClosureRowId":12,"rationale":"approve"}`
}

func priorityRequestJSON(extra string) string {
	if extra != "" {
		extra += ","
	}
	return `{` + extra + `"externalPriority":66}`
}

func selectionRequestJSON(extra string) string {
	if extra != "" {
		extra += ","
	}
	return `{` + extra + `"ticketId":"ticket-1","revisionRowId":9,"rationale":"reserve"}`
}
