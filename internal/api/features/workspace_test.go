package features

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	featureapp "relay/internal/app/features"
	wayfinder "relay/internal/app/wayfinder"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWayfinder struct {
	detail wayfinder.WorkspaceDetail
	err    error
}

func (f *fakeWayfinder) CreateWorkspace(context.Context, wayfinder.CreateWorkspaceInput) (workflowstore.FeatureWorkspace, error) {
	return f.detail.Workspace, f.err
}
func (f *fakeWayfinder) ReadWorkspace(context.Context, string) (wayfinder.WorkspaceDetail, error) {
	return f.detail, f.err
}
func (f *fakeWayfinder) AdmitInput(context.Context, wayfinder.AdmitInputInput) (workflowstore.FeatureWorkspaceAdmittedInput, workflowstore.FeatureWorkspace, error) {
	return workflowstore.FeatureWorkspaceAdmittedInput{}, f.detail.Workspace, f.err
}
func (f *fakeWayfinder) AddDestination(context.Context, wayfinder.AddDestinationInput) (workflowstore.FeatureWorkspaceDestination, workflowstore.FeatureWorkspace, error) {
	return workflowstore.FeatureWorkspaceDestination{}, f.detail.Workspace, f.err
}
func (f *fakeWayfinder) CreateDiscoveryTicket(context.Context, wayfinder.CreateDiscoveryTicketInput) (workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.FeatureWorkspace, error) {
	return workflowstore.FeatureWorkspaceDiscoveryTicket{}, f.detail.Workspace, f.err
}
func (f *fakeWayfinder) ResolveDiscoveryTicket(context.Context, wayfinder.ResolveDiscoveryTicketInput) (workflowstore.FeatureWorkspaceTicketResolution, workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.FeatureWorkspace, error) {
	return workflowstore.FeatureWorkspaceTicketResolution{}, workflowstore.FeatureWorkspaceDiscoveryTicket{}, f.detail.Workspace, f.err
}
func (f *fakeWayfinder) RouteWorkspace(context.Context, wayfinder.RouteWorkspaceInput) (workflowstore.FeatureWorkspaceRouteState, workflowstore.FeatureWorkspace, error) {
	return workflowstore.FeatureWorkspaceRouteState{}, f.detail.Workspace, f.err
}

type fakeAuthority struct {
	revisions []featureapp.AuthorityRevisionDetail
	err       error
}

func (f *fakeAuthority) ReadAuthority(context.Context, string) ([]featureapp.AuthorityRevisionDetail, error) {
	return f.revisions, f.err
}
func (f *fakeAuthority) PublishAuthority(context.Context, featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, workflowstore.FeatureWorkspace, error) {
	return featureapp.AuthorityRevisionDetail{}, workflowstore.FeatureWorkspace{}, f.err
}

func workspaceRouter(wayfinderService WayfinderService, authorityService AuthorityService) http.Handler {
	router := chi.NewRouter()
	MountWorkspaceRoutes(router, NewWorkspaceHandler(wayfinderService, authorityService))
	return router
}

func TestWorkspaceDetailRouteReturnsResumableProjectionWithoutVaultPaths(t *testing.T) {
	service := &fakeWayfinder{detail: wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-api", FeatureSlug: "payments", State: "open", Version: 3}, Investigations: []workflowstore.FeatureWorkspaceInvestigation{{InvestigationID: "investigation-api"}}}}
	response := httptest.NewRecorder()
	workspaceRouter(service, &fakeAuthority{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"workspaceId":"workspace-api"`) || !strings.Contains(response.Body.String(), `"status":"not_recorded"`) || strings.Contains(strings.ToLower(response.Body.String()), "vault") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceMutationReturnsTypedVersionConflict(t *testing.T) {
	service := &fakeWayfinder{detail: wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-api", Version: 2}}, err: wayfinder.ErrVersionConflict}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/routes", strings.NewReader(`{"expectedVersion":1,"sequence":1,"state":"ready"}`))
	workspaceRouter(service, &fakeAuthority{}).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"VERSION_CONFLICT"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceRoutesDoNotExposeDeliveryTicketOrPackageSurfaces(t *testing.T) {
	router := workspaceRouter(&fakeWayfinder{err: errors.New("unexpected")}, &fakeAuthority{})
	for _, path := range []string{"/feature-workspaces/workspace-api/delivery-tickets", "/feature-workspaces/workspace-api/packages"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
}
