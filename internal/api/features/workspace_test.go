package features

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	featureapp "relay/internal/app/features"
	appoperations "relay/internal/app/operations"
	wayfinder "relay/internal/app/wayfinder"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWayfinder struct {
	detail wayfinder.WorkspaceDetail
	err    error
}

func (f *fakeWayfinder) CreateWorkspace(context.Context, wayfinder.CreateWorkspaceInput) (Workspace, error) {
	return Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) ReadWorkspace(context.Context, string) (wayfinder.WorkspaceDetail, error) {
	return f.detail, f.err
}
func (f *fakeWayfinder) AdmitInput(context.Context, wayfinder.AdmitInputInput) (AdmittedInput, Workspace, error) {
	return AdmittedInput{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) AddDestination(context.Context, wayfinder.AddDestinationInput) (Destination, Workspace, error) {
	return Destination{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) CreateDiscoveryTicket(context.Context, wayfinder.CreateDiscoveryTicketInput) (DiscoveryTicket, Workspace, error) {
	return DiscoveryTicket{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) ResolveDiscoveryTicket(context.Context, wayfinder.ResolveDiscoveryTicketInput) (Resolution, DiscoveryTicket, Workspace, error) {
	return Resolution{}, DiscoveryTicket{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) RouteWorkspace(context.Context, wayfinder.RouteWorkspaceInput) (RouteState, Workspace, error) {
	return RouteState{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}

type fakeAuthority struct {
	revisions []featureapp.AuthorityRevisionDetail
	err       error
}

func (f *fakeAuthority) ReadAuthority(context.Context, string) ([]featureapp.AuthorityRevisionDetail, error) {
	return f.revisions, f.err
}
func (f *fakeAuthority) PublishAuthority(context.Context, featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, Workspace, error) {
	return featureapp.AuthorityRevisionDetail{}, Workspace{}, f.err
}
func (f *fakeAuthority) RecordAuthorityApproval(context.Context, featureapp.RecordAuthorityApprovalInput) (featureapp.RecordAuthorityApprovalResult, error) {
	return featureapp.RecordAuthorityApprovalResult{}, f.err
}

type fakeCompletion struct {
	status appoperations.FeatureCompletionStatus
	result appoperations.FeatureCompletionResult
	err    error
	input  appoperations.FeatureCompletionOperationInput
}

func (f *fakeCompletion) Evaluate(context.Context, string) (appoperations.FeatureCompletionStatus, error) {
	return f.status, f.err
}
func (f *fakeCompletion) Complete(_ context.Context, input appoperations.FeatureCompletionOperationInput) (appoperations.FeatureCompletionResult, error) {
	f.input = input
	return f.result, f.err
}

func workspaceRouter(wayfinderService WayfinderService, authorityService AuthorityService, completionService CompletionService) http.Handler {
	router := chi.NewRouter()
	MountWorkspaceRoutes(router, NewWorkspaceHandler(wayfinderService, authorityService, completionService))
	return router
}

func TestWorkspaceDetailRouteReturnsResumableProjectionWithoutVaultPaths(t *testing.T) {
	service := &fakeWayfinder{detail: wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-api", FeatureSlug: "payments", State: "open", Version: 3}, Investigations: []workflowstore.FeatureWorkspaceInvestigation{{InvestigationID: "investigation-api"}}}}
	response := httptest.NewRecorder()
	workspaceRouter(service, &fakeAuthority{}, &fakeCompletion{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"workspaceId":"workspace-api"`) || !strings.Contains(response.Body.String(), `"status":"not_recorded"`) || strings.Contains(strings.ToLower(response.Body.String()), "vault") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceMutationReturnsTypedVersionConflict(t *testing.T) {
	service := &fakeWayfinder{detail: wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-api", Version: 2}}, err: wayfinder.ErrVersionConflict}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/routes", strings.NewReader(`{"expectedVersion":1,"sequence":1,"state":"ready"}`))
	workspaceRouter(service, &fakeAuthority{}, &fakeCompletion{}).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"VERSION_CONFLICT"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceRoutesDoNotExposeDeliveryTicketOrPackageSurfaces(t *testing.T) {
	router := workspaceRouter(&fakeWayfinder{err: errors.New("unexpected")}, &fakeAuthority{}, &fakeCompletion{})
	for _, path := range []string{"/feature-workspaces/workspace-api/delivery-tickets", "/feature-workspaces/workspace-api/packages"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
}

func TestWorkspaceCompletionShowsBlockersAndForwardsExplicitAdmission(t *testing.T) {
	completion := &fakeCompletion{status: appoperations.FeatureCompletionStatus{
		Workspace:       appoperations.FeatureCompletionWorkspace{WorkspaceID: "workspace-api", FeatureSlug: "payments", State: "open", Version: 3},
		Gates:           []appoperations.FeatureCompletionGate{{Name: "authority", Ready: true}, {Name: "audit", Ready: false}},
		CurrentDecision: &appoperations.FeatureCompletionDecision{CompletionDecisionID: "current-completion-api", AuthorityRevisionRowID: 2, SourceClosureRowID: 3, Decision: "completed"},
	}, result: appoperations.FeatureCompletionResult{
		Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: "workspace-api", FeatureSlug: "payments", State: "open", Version: 4},
		Decision:  appoperations.FeatureCompletionDecision{CompletionDecisionID: "completion-api", AuthorityRevisionRowID: 3, SourceClosureRowID: 4, Decision: "completed"},
	}}
	router := workspaceRouter(&fakeWayfinder{}, &fakeAuthority{}, completion)
	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api/completion", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"name":"audit","ready":false`) || !strings.Contains(status.Body.String(), `"completionDecisionId":"current-completion-api"`) {
		t.Fatalf("status = %d %s", status.Code, status.Body.String())
	}
	body := `{"packetId":"packet-api","operationId":"local_operator.ticket_workflow","requiredDependencies":[{"class":"feature_workspace_completion","key":"workspace:workspace-api:version:3"}],"expectedVersion":3,"operatorConfirmed":true}`
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/completion", strings.NewReader(body)))
	if response.Code != http.StatusCreated || completion.input.Admission.PacketID != "packet-api" || !completion.input.Complete.OperatorConfirmed || !strings.Contains(response.Body.String(), `"completionDecisionId":"completion-api"`) {
		t.Fatalf("response = %d input = %#v body = %s", response.Code, completion.input, response.Body.String())
	}
}
