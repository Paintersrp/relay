package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"relay/internal/mcp/routecontracts"
)

func newMCPRouteTestHandler(t *testing.T) http.Handler {
	surfaces, err := routecontracts.BuildMCPAppSurfaceManifests()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	for _, surface := range surfaces.Surfaces {
		mux.HandleFunc(surface.PublicPath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}
	return mux
}

func TestMCPRoutesPublishOnlyRoleApps(t *testing.T) {
	handler := newMCPRouteTestHandler(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/mcp", nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s /mcp status=%d, want 404", method, response.Code)
		}
	}
	for _, path := range []string{"/mcp/wayfinder", "/mcp/planner", "/mcp/auditor"} {
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
