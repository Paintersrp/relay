package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apicanonical "relay/internal/api/canonical"
	workflowcanonical "relay/internal/app/canonical"
	workflowplans "relay/internal/app/plans/workflow"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type transportFixture struct {
	harness *canonicalTestHarness
	project workflowstore.Project
	router  http.Handler
}

func newTransportFixture(t *testing.T) *transportFixture {
	t.Helper()
	harness := newCanonicalTestHarness(t, ToolProfilePlanner)
	harness.registerRepo(t, "relay")
	projects, err := workflowprojects.NewService(harness.store)
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.CreateProject(context.Background(), workflowprojects.CreateProjectInput{Name: "Relay"})
	if err != nil {
		t.Fatal(err)
	}
	canonicalService, err := workflowcanonical.NewService(harness.store)
	if err != nil {
		t.Fatal(err)
	}
	planService, err := workflowplans.NewService(harness.store)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	apicanonical.MountWorkflowRoutes(router, apicanonical.NewWorkflowHandler(canonicalService, planService))
	return &transportFixture{harness: harness, project: project, router: router}
}

func (f *transportFixture) submitPlanThroughMCP(t *testing.T, fileID string) canonicalPlanOutput {
	t.Helper()
	data := canonicalPlanBytes("relay")
	ref := f.harness.put(fileID, "canonical-test.plan.json", data)
	result := f.harness.server.HandleSubmitPlan(canonicalArgs(t, canonicalSubmissionArgs{
		ProjectID:      f.project.ProjectID,
		ArtifactFile:   ref,
		ExpectedSHA256: canonicalTestSHA(data),
	}))
	if result.IsError {
		t.Fatalf("MCP submit Plan failed: %s", canonicalToolText(t, result))
	}
	var output canonicalPlanOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func (f *transportFixture) submitPlanThroughHTTP(t *testing.T) map[string]any {
	t.Helper()
	data := canonicalPlanBytes("relay")
	response := f.requestJSON(t, http.MethodPost, "/plans", map[string]any{
		"projectId":        f.project.ProjectID,
		"fileName":         "canonical-test.plan.json",
		"canonicalContent": string(data),
		"expectedSha256":   canonicalTestSHA(data),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("HTTP submit Plan failed: %d %s", response.Code, response.Body.String())
	}
	var output map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	return output
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

func TestCanonicalMCPAndHTTPValidationAndPlanSubmissionParity(t *testing.T) {
	mcpFixture := newTransportFixture(t)
	httpFixture := newTransportFixture(t)
	data := canonicalPlanBytes("relay")

	ref := mcpFixture.harness.put("parity-validate", "canonical-test.plan.json", data)
	mcpResult := mcpFixture.harness.server.HandleValidateArtifact(canonicalArgs(t, canonicalArtifactArgs{ArtifactFile: ref}))
	if mcpResult.IsError {
		t.Fatalf("MCP validation failed: %s", canonicalToolText(t, mcpResult))
	}
	var mcpValidation canonicalValidationOutput
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

	mcpPlan := mcpFixture.submitPlanThroughMCP(t, "parity-plan")
	httpPlan := httpFixture.submitPlanThroughHTTP(t)
	httpPlanValue, ok := httpPlan["plan"].(map[string]any)
	if !ok {
		t.Fatalf("HTTP Plan response missing plan: %+v", httpPlan)
	}
	httpPasses, _ := httpPlan["passes"].([]any)
	httpArtifacts, _ := httpPlan["artifacts"].([]any)
	httpProject, _ := httpPlanValue["project"].(map[string]any)
	if mcpPlan.Project.ProjectID != mcpFixture.project.ProjectID ||
		httpProject["projectId"] != httpFixture.project.ProjectID ||
		mcpPlan.Plan.FeatureSlug != httpPlanValue["featureSlug"] ||
		mcpPlan.Plan.Status != httpPlanValue["status"] ||
		len(mcpPlan.Passes) != len(httpPasses) ||
		len(mcpPlan.Artifacts) != len(httpArtifacts) {
		t.Fatalf("submission parity mismatch: MCP=%+v HTTP=%+v", mcpPlan, httpPlan)
	}
}

func TestCanonicalMCPAndHTTPAssociationFailureParityAndRollback(t *testing.T) {
	mcpFixture := newTransportFixture(t)
	httpFixture := newTransportFixture(t)
	mcpPlan := mcpFixture.submitPlanThroughMCP(t, "parity-managed-mcp")
	httpPlan := httpFixture.submitPlanThroughHTTP(t)
	httpPlanValue := httpPlan["plan"].(map[string]any)
	httpPlanID := httpPlanValue["planId"].(string)

	mcpBeforeArtifacts := workflowRowCount(t, mcpFixture.harness.store, "artifacts")
	mcpBeforeFiles := artifactFileCount(t, mcpFixture.harness.artifactRoot)
	data := canonicalExecutionSpecBytes("relay")
	ref := mcpFixture.harness.put("parity-run-mcp", "canonical-test.execution-spec.json", data)
	mcpFailure := mcpFixture.harness.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
		ArtifactFile:   ref,
		ExpectedSHA256: canonicalTestSHA(data),
		PlanID:         mcpPlan.Plan.PlanID,
		PassNumber:     1,
	}))
	if code := canonicalBlockerCode(t, mcpFailure); code != canonicalBlockerAssociationInvalid {
		t.Fatalf("MCP blocker code = %q", code)
	}
	if workflowRowCount(t, mcpFixture.harness.store, "runs") != 0 ||
		workflowRowCount(t, mcpFixture.harness.store, "artifacts") != mcpBeforeArtifacts ||
		artifactFileCount(t, mcpFixture.harness.artifactRoot) != mcpBeforeFiles {
		t.Fatal("MCP association failure wrote durable state")
	}

	httpBeforeArtifacts := workflowRowCount(t, httpFixture.harness.store, "artifacts")
	httpBeforeFiles := artifactFileCount(t, httpFixture.harness.artifactRoot)
	httpFailure := httpFixture.requestJSON(t, http.MethodPost, "/runs", map[string]any{
		"fileName":         "canonical-test.execution-spec.json",
		"canonicalContent": string(data),
		"expectedSha256":   canonicalTestSHA(data),
		"planId":           httpPlanID,
		"passNumber":       1,
	})
	if httpFailure.Code != http.StatusBadRequest {
		t.Fatalf("HTTP status = %d, body = %s", httpFailure.Code, httpFailure.Body.String())
	}
	var errorBody struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(httpFailure.Body.Bytes(), &errorBody); err != nil {
		t.Fatal(err)
	}
	if errorBody.Error != "ASSOCIATION_INVALID" {
		t.Fatalf("HTTP error = %q, body = %s", errorBody.Error, httpFailure.Body.String())
	}
	if workflowRowCount(t, httpFixture.harness.store, "runs") != 0 ||
		workflowRowCount(t, httpFixture.harness.store, "artifacts") != httpBeforeArtifacts ||
		artifactFileCount(t, httpFixture.harness.artifactRoot) != httpBeforeFiles {
		t.Fatal("HTTP association failure wrote durable state")
	}
}
