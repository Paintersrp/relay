package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apicanonical "relay/internal/api/canonical"
	workflowsubmissions "relay/internal/app/submissions"

	"github.com/go-chi/chi/v5"
)

type transportFixture struct {
	harness *canonicalTestHarness
	router  http.Handler
}

func newTransportFixture(t *testing.T) *transportFixture {
	t.Helper()
	harness := newCanonicalTestHarness(t, ToolProfilePlanner)
	harness.registerRepo(t, "relay")
	canonicalService, err := workflowsubmissions.NewService(harness.store)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	apicanonical.MountWorkflowRoutes(router, apicanonical.NewWorkflowHandler(canonicalService))
	return &transportFixture{harness: harness, router: router}
}

func (f *transportFixture) requestJSON(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func TestCanonicalMCPAndHTTPValidationParity(t *testing.T) {
	mcpFixture := newTransportFixture(t)
	httpFixture := newTransportFixture(t)
	data := canonicalPlanBytes("relay")

	ref := mcpFixture.harness.put("parity-validate", "canonical-test.plan.json", data)
	mcpResult := mcpFixture.harness.server.HandleValidateArtifact(canonicalArgs(t, artifactArgs{ArtifactFile: ref}))
	if mcpResult.IsError {
		t.Fatalf("MCP validation failed: %s", canonicalToolText(t, mcpResult))
	}
	var mcpValidation artifactValidationOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, mcpResult)), &mcpValidation); err != nil {
		t.Fatal(err)
	}
	httpValidation := httpFixture.requestJSON(t, http.MethodPost, "/canonical-artifacts/validate", map[string]any{
		"fileName":         "canonical-test.plan.json",
		"canonicalContent": string(data),
	})
	if httpValidation.Code != http.StatusOK {
		t.Fatalf("HTTP validation failed: %d %s", httpValidation.Code, httpValidation.Body.String())
	}
	var httpValidationOutput struct {
		OK          bool             `json:"ok"`
		Status      string           `json:"status"`
		Kind        string           `json:"kind"`
		SHA256      string           `json:"sha256"`
		Diagnostics []map[string]any `json:"diagnostics"`
		Notices     []map[string]any `json:"notices"`
	}
	if err := json.Unmarshal(httpValidation.Body.Bytes(), &httpValidationOutput); err != nil {
		t.Fatal(err)
	}
	if mcpValidation.OK != httpValidationOutput.OK ||
		mcpValidation.Status != httpValidationOutput.Status ||
		mcpValidation.Kind != httpValidationOutput.Kind ||
		mcpValidation.SHA256 != httpValidationOutput.SHA256 ||
		len(mcpValidation.Diagnostics) != len(httpValidationOutput.Diagnostics) ||
		len(mcpValidation.Notices) != len(httpValidationOutput.Notices) {
		t.Fatalf("validation parity mismatch: MCP=%+v HTTP=%+v", mcpValidation, httpValidationOutput)
	}
}

// Legacy Plan write admission is retired on both transports: neither the HTTP
// canonical router nor the aggregate MCP surface exposes a Plan-creating
// operation, and validation leaves no durable Plan state.
func TestCanonicalTransportsExposeNoPlanSubmissionParity(t *testing.T) {
	fixture := newTransportFixture(t)
	data := canonicalPlanBytes("relay")

	response := fixture.requestJSON(t, http.MethodPost, "/plans", map[string]any{
		"projectId":        "project-canonical-tests",
		"fileName":         "canonical-test.plan.json",
		"canonicalContent": string(data),
		"expectedSha256":   canonicalTestSHA(data),
	})
	if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HTTP Plan submission is still mounted: %d %s", response.Code, response.Body.String())
	}

	ref := fixture.harness.put("parity-retired", "canonical-test.plan.json", data)
	validated := fixture.harness.server.HandleValidateArtifact(canonicalArgs(t, artifactArgs{ArtifactFile: ref}))
	if validated.IsError {
		t.Fatalf("MCP validation failed: %s", canonicalToolText(t, validated))
	}
	for _, table := range []string{"plans", "plan_passes", "runs", "artifacts"} {
		if got := workflowRowCount(t, fixture.harness.store, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
}
