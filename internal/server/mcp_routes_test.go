package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"relay/internal/mcp/routecontracts"
)

func newMCPRouteTestHandler(t *testing.T) http.Handler {
	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, manifest := range set.Manifests {
		mux.HandleFunc(manifest.RoutePath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}
	return mux
}

func newMCPRouteFailureTestHandler(t *testing.T) (handler http.Handler) {
	_ = t
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
}

func TestMCPRoutesPublishCompleteSetBesideAggregate(t *testing.T) {
	handler := newMCPRouteTestHandler(t)
	for _, path := range []string{"/mcp", "/mcp/v1/wayfinder/workspace", "/mcp/v1/wayfinder/discovery", "/mcp/v1/wayfinder/investigation", "/mcp/v1/planner/authoring", "/mcp/v1/planner/frontier", "/mcp/v1/auditor/review", "/mcp/v1/auditor/audit"} {
		request := httptest.NewRequest("POST", path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code == 404 {
			t.Fatalf("%s not mounted", path)
		}
	}
}
func TestMCPPrebuildFailurePublishesNoRoute(t *testing.T) {
	handler := newMCPRouteFailureTestHandler(t)
	for _, path := range []string{"/mcp/v1/wayfinder/workspace", "/mcp/v1/planner/authoring", "/mcp/v1/auditor/audit"} {
		request := httptest.NewRequest("POST", path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 404 {
			t.Fatalf("%s published: %d", path, response.Code)
		}
	}
}
