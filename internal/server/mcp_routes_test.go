package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"relay/internal/mcp/routecontracts"
)

type aggregateStateStub struct {
	closed bool
	err    error
}

func (stub aggregateStateStub) IsLegacyAdmissionClosed(context.Context) (bool, error) {
	return stub.closed, stub.err
}

func newMCPRouteTestHandler(t *testing.T) http.Handler {
	surfaces, err := routecontracts.BuildMCPAppSurfaceManifests()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	aggregatePath := "/" + "mcp"
	mux.Handle(aggregatePath, newCutoverAggregateHandler(aggregateStateStub{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	for _, surface := range surfaces.Surfaces {
		mux.HandleFunc(surface.PublicPath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}
	return mux
}

func TestMCPRoutesPublishOnlyRoleAppsBesideAggregateBeforeActivation(t *testing.T) {
	handler := newMCPRouteTestHandler(t)
	for _, path := range []string{"/mcp", "/mcp/wayfinder", "/mcp/planner", "/mcp/auditor"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code == http.StatusNotFound {
			t.Fatalf("%s not mounted", path)
		}
	}
	for _, path := range []string{"/mcp/v1/wayfinder/workspace", "/mcp/v1/wayfinder/discovery", "/mcp/v1/wayfinder/investigation", "/mcp/v1/planner/authoring", "/mcp/v1/planner/frontier", "/mcp/v1/auditor/review", "/mcp/v1/auditor/audit"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s status=%d, want 404", path, response.Code)
		}
	}
}

func TestAggregateClosesWithoutAffectingVersionedRoutes(t *testing.T) {
	aggregate := newCutoverAggregateHandler(aggregateStateStub{closed: true}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("closed aggregate request dispatched")
	}))
	response := httptest.NewRecorder()
	aggregate.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("closed aggregate status = %d", response.Code)
	}
}

func TestAggregateFailsClosedWhenStateUnavailable(t *testing.T) {
	aggregate := newCutoverAggregateHandler(aggregateStateStub{err: errors.New("unavailable")}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("unavailable state dispatched")
	}))
	response := httptest.NewRecorder()
	aggregate.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable aggregate status = %d", response.Code)
	}
}
