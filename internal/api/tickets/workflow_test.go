package tickets

import (
	"context"
	"errors"
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
	frontier       apptickets.Frontier
	err            error
	publishInput   appoperations.TicketPublishOperationInput
	selectionInput appoperations.TicketSelectionOperationInput
}

func (f *fakeWorkflow) Publish(_ context.Context, input appoperations.TicketPublishOperationInput) (apptickets.PublishedRevision, error) {
	f.publishInput = input
	return apptickets.PublishedRevision{Ticket: workflowstore.DeliveryTicket{TicketID: input.Publish.TicketID, ExternalPriority: input.Publish.ExternalPriority}, Revision: workflowstore.DeliveryTicketRevision{ID: 4, RevisionNumber: 1, SourceClosureRowID: input.Publish.Revision.SourceClosureRowID}, Canonical: apptickets.StoredArtifact{RelativePath: "delivery-tickets/t/revisions/1/delivery-ticket.json"}, Rendered: apptickets.StoredArtifact{RelativePath: "delivery-tickets/t/revisions/1/delivery-ticket.md"}}, f.err
}
func (f *fakeWorkflow) ReplaceDependencies(context.Context, appoperations.TicketPublishOperationInput) (apptickets.PublishedRevision, error) {
	return apptickets.PublishedRevision{}, f.err
}
func (f *fakeWorkflow) Approve(context.Context, appoperations.TicketApprovalOperationInput) (RevisionApproval, error) {
	return RevisionApproval{}, f.err
}
func (f *fakeWorkflow) UpdatePriority(context.Context, appoperations.TicketOperationRequest) (DeliveryTicket, error) {
	return DeliveryTicket{}, f.err
}
func (f *fakeWorkflow) ListFrontier(_ context.Context, request appoperations.TicketOperationRequest) (apptickets.Frontier, error) {
	if request.Action != "read_ticket_frontier" {
		return apptickets.Frontier{}, errors.New("wrong action")
	}
	return f.frontier, f.err
}
func (f *fakeWorkflow) Select(_ context.Context, input appoperations.TicketSelectionOperationInput) (apptickets.SelectionResult, error) {
	f.selectionInput = input
	return apptickets.SelectionResult{}, f.err
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
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api/tickets/frontier?packetId=packet-1&operationId=planner.ticket_frontier", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"previousTicketId":"ticket-1"`) || !strings.Contains(response.Body.String(), `"rule":"stable_ticket_id"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestPublishRouteBuildsExactPacketBoundOwnerInput(t *testing.T) {
	service := &fakeWorkflow{}
	body := `{"packetId":"operator-packet","operationId":"local_operator.ticket_workflow","externalPriority":66,"expectedRevisionNumber":0,"revision":{"repoTarget":"relay","branch":"main","baseCommit":"abc","sourceClosureRowId":12,"sourcePath":"tickets/ticket-1.json","goal":"Ship ticket","context":"Exact context","transitionApplicability":"not_required","canonicalJson":{"ticket":"ticket-1"},"renderedMarkdown":"# Ticket\\n","members":[{"kind":"implementation_obligation","path":"internal/app/tickets","text":"Derive readiness."}],"dependencies":[]}}`
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/ticket-1/revisions", strings.NewReader(body)))
	if response.Code != http.StatusCreated || service.publishInput.Admission.PacketID != "operator-packet" || service.publishInput.Publish.Revision.SourceClosureRowID != 12 || len(service.publishInput.Publish.Revision.CanonicalJSON) == 0 {
		t.Fatalf("response = %d %s input = %#v", response.Code, response.Body.String(), service.publishInput)
	}
}

func TestSelectionRouteMapsAtomicConflict(t *testing.T) {
	service := &fakeWorkflow{err: apptickets.ErrSelectionConflict}
	body := `{"packetId":"operator-packet","operationId":"local_operator.ticket_workflow","rationale":"reserve","members":[{"ticketId":"ticket-1","revisionRowId":9}]}`
	response := httptest.NewRecorder()
	ticketRouter(service, &fakeRead{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/tickets/selection", strings.NewReader(body)))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"CONFLICT"`) || len(service.selectionInput.Admission.SelectionMembers) != 1 {
		t.Fatalf("response = %d %s selection = %#v", response.Code, response.Body.String(), service.selectionInput)
	}
}
