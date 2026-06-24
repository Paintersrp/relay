package mcp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// refactorBacklogToolNames is the exact PASS-005 tool surface.
func refactorBacklogToolNames() []string {
	return []string{
		"list_refactor_discovery_tasks",
		"get_refactor_discovery_task",
		"create_refactor_discovery_task",
		"update_refactor_discovery_task",
		"complete_refactor_discovery_task",
		"close_refactor_discovery_task",
		"supersede_refactor_discovery_task",
		"list_refactor_candidates",
		"get_refactor_candidate",
		"search_refactor_candidates",
		"create_refactor_candidate",
		"update_refactor_candidate",
		"defer_refactor_candidate",
		"reject_refactor_candidate",
		"supersede_refactor_candidate",
		"suggest_refactor_candidate_placement",
		"promote_refactor_candidate_to_plan",
		"generate_refactor_only_plan",
	}
}

// setupRefactorServer builds a store-backed MCP server (project "relay" seeded)
// under the local-operator profile with artifacts pointed at a temp dir.
func setupRefactorServer(t *testing.T) *Server {
	t.Helper()
	setupTestArtifactDir(t)
	st := setupTestStore(t)
	deps := &MCPDeps{Store: st, Log: discardLogger(), ToolProfile: ToolProfileLocalOperator}
	return NewServer(discardLogger(), deps)
}

// validCandidateArgsJSON mirrors the pass-ready candidate fields used by the
// internal/refactors tests.
func validCandidateArgsJSON(candidateID string) json.RawMessage {
	payload := map[string]interface{}{
		"project_id":            "relay",
		"candidate_id":          candidateID,
		"title":                 candidateID,
		"problem_summary":       "Duplicate parsing branch causes drift.",
		"desired_behavior":      "Single parsing path shared across callers.",
		"rationale":             "Reduces maintenance burden.",
		"proposed_pass_name":    "Consolidate parsing",
		"proposed_pass_goal":    "Remove the duplicate parsing branch.",
		"proposed_pass_scope":   []string{"Replace duplicate parsing branch in internal/foo/bar.go"},
		"non_goals":             []string{"Do not change public API behavior"},
		"target_files":          []string{"internal/foo/bar.go"},
		"validation_commands":   []string{"go test ./internal/foo/..."},
		"audit_focus":           []string{"Verify behavior remains unchanged"},
		"risk_level":            "medium",
		"confirmed_user_intent": true,
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestRefactorBacklogToolsListedUnderLocalOperator(t *testing.T) {
	srv := setupRefactorServer(t)
	list := listTools(t, srv)

	for _, name := range refactorBacklogToolNames() {
		count := 0
		for _, tool := range list.Tools {
			if tool.Name == name {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected refactor tool %q exactly once, got %d", name, count)
		}
	}
}

func TestRefactorBacklogToolsHiddenUnderRestricted(t *testing.T) {
	st := setupTestStore(t)
	deps := &MCPDeps{Store: st, Log: discardLogger(), ToolProfile: ToolProfileRestricted}
	srv := NewServer(discardLogger(), deps)
	list := listTools(t, srv)

	for _, name := range refactorBacklogToolNames() {
		if hasTool(list, name) {
			t.Errorf("refactor tool %q must be hidden under restricted profile", name)
		}
	}
}

func TestRefactorBacklogSchemasAreStrictAndProjectScoped(t *testing.T) {
	for _, tool := range refactorBacklogToolDefinitions() {
		var schema map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("unmarshal schema for %s: %v", tool.Name, err)
		}
		if schema["additionalProperties"] != false {
			t.Errorf("expected additionalProperties=false for %s", tool.Name)
		}
		required, ok := schema["required"].([]interface{})
		if !ok {
			t.Fatalf("expected required array for %s", tool.Name)
		}
		hasProjectID := false
		for _, r := range required {
			if r == "project_id" {
				hasProjectID = true
			}
		}
		if !hasProjectID {
			t.Errorf("expected project_id required for %s", tool.Name)
		}
	}
}

func TestRefactorBacklogToolCallHiddenUnderRestricted(t *testing.T) {
	st := setupTestStore(t)
	deps := &MCPDeps{Store: st, Log: discardLogger(), ToolProfile: ToolProfileRestricted}
	srv := NewServer(discardLogger(), deps)

	for _, name := range refactorBacklogToolNames() {
		params, _ := json.Marshal(ToolCallParams{Name: name, Arguments: json.RawMessage(`{"project_id":"relay"}`)})
		req := Request{JSONRPC: JSONRPCVersion, ID: json.RawMessage(`1`), Method: "tools/call", Params: params}
		resp := srv.handleLine(mustMarshal(t, req))
		if resp.Error == nil {
			t.Errorf("expected method-not-found calling %q under restricted, got success", name)
			continue
		}
		if resp.Error.Code != CodeMethodNotFound {
			t.Errorf("expected CodeMethodNotFound for %q, got %d", name, resp.Error.Code)
		}
		if !strings.Contains(resp.Error.Message, "unknown tool") {
			t.Errorf("expected 'unknown tool' message for %q, got %q", name, resp.Error.Message)
		}
	}
}

func TestRefactorBacklogToolsRequireProjectID(t *testing.T) {
	srv := setupRefactorServer(t)
	for _, name := range refactorBacklogToolNames() {
		result := callTool(t, srv, name, json.RawMessage(`{}`))
		if !result.IsError {
			t.Errorf("expected validation error for %q with empty args", name)
			continue
		}
		errEnvelope := decodeBrokerError(t, result)
		if errEnvelope.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("expected VALIDATION_ERROR for %q, got %q", name, errEnvelope.Error.Code)
		}
	}
}

func TestRefactorMutationRequiresConfirmation(t *testing.T) {
	srv := setupRefactorServer(t)

	// create_refactor_candidate with confirmed_user_intent omitted must block
	// before any service write.
	args := json.RawMessage(`{
		"project_id":"relay",
		"candidate_id":"rc-x",
		"title":"x",
		"problem_summary":"x",
		"desired_behavior":"x",
		"rationale":"x",
		"proposed_pass_name":"x",
		"proposed_pass_goal":"x",
		"proposed_pass_scope":["x"],
		"non_goals":["x"],
		"target_files":["internal/foo/bar.go"],
		"validation_commands":["go test ./..."],
		"audit_focus":["x"],
		"risk_level":"low",
		"confirmed_user_intent":false
	}`)
	result := callTool(t, srv, "create_refactor_candidate", args)
	if !result.IsError {
		t.Fatal("expected CONFIRMATION_REQUIRED for create without confirmation")
	}
	if code := decodeBrokerError(t, result).Error.Code; code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("expected CONFIRMATION_REQUIRED, got %q", code)
	}

	// Prove no write occurred: the candidate must not exist.
	listResult := callTool(t, srv, "list_refactor_candidates", json.RawMessage(`{"project_id":"relay"}`))
	success := decodeBrokerSuccess(t, listResult)
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("expected no candidates created, got count=%d", payload.Count)
	}
}

func TestRefactorPromotionRequiresConfirmationString(t *testing.T) {
	srv := setupRefactorServer(t)

	// confirmed_user_intent true but confirmation string missing.
	missing := callTool(t, srv, "promote_refactor_candidate_to_plan", json.RawMessage(`{
		"project_id":"relay","candidate_id":"rc-a","plan_id":"plan-1","confirmed_user_intent":true
	}`))
	if !missing.IsError {
		t.Fatal("expected confirmation block for promotion without confirmation string")
	}
	if code := decodeBrokerError(t, missing).Error.Code; code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("expected CONFIRMATION_REQUIRED, got %q", code)
	}

	// confirmed_user_intent true but confirmation string mismatched.
	mismatch := callTool(t, srv, "promote_refactor_candidate_to_plan", json.RawMessage(`{
		"project_id":"relay","candidate_id":"rc-a","plan_id":"plan-1","confirmed_user_intent":true,"confirmation":"wrong"
	}`))
	if !mismatch.IsError {
		t.Fatal("expected confirmation block for promotion with wrong confirmation string")
	}
	if code := decodeBrokerError(t, mismatch).Error.Code; code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("expected CONFIRMATION_REQUIRED, got %q", code)
	}
}

func TestRefactorGeneratePlanReturnsBoundedArtifactMetadata(t *testing.T) {
	srv := setupRefactorServer(t)

	created := callTool(t, srv, "create_refactor_candidate", validCandidateArgsJSON("rc-a"))
	decodeBrokerSuccess(t, created)

	genArgs := json.RawMessage(`{
		"project_id":"relay",
		"candidate_ids":["rc-a"],
		"confirmed_user_intent":true,
		"confirmation":"generate_reviewable_refactor_only_plan"
	}`)
	result := callTool(t, srv, "generate_refactor_only_plan", genArgs)
	success := decodeBrokerSuccess(t, result)

	raw := result.Content[0].Text
	for _, banned := range []string{`"raw_plan_json"`, `"plan_json"`, `"markdown":`, `"raw_pass_json"`} {
		if strings.Contains(raw, banned) {
			t.Errorf("generated plan response must not include %s; got:\n%s", banned, raw)
		}
	}

	var payload struct {
		PlanID               string   `json:"plan_id"`
		CandidateIDs         []string `json:"candidate_ids"`
		JSONArtifactPath     string   `json:"json_artifact_path"`
		MarkdownArtifactPath string   `json:"markdown_artifact_path"`
		SubmissionPolicy     string   `json:"submission_policy"`
	}
	if err := json.Unmarshal(success.Result, &payload); err != nil {
		t.Fatalf("unmarshal generate result: %v", err)
	}
	if payload.PlanID == "" {
		t.Error("expected generated plan_id")
	}
	if payload.JSONArtifactPath == "" || payload.MarkdownArtifactPath == "" {
		t.Error("expected artifact paths in generate result")
	}
	if payload.SubmissionPolicy != "review_required_no_auto_submit" {
		t.Errorf("expected review_required_no_auto_submit submission policy, got %q", payload.SubmissionPolicy)
	}

	// Candidate must remain ready (generation does not mutate candidate status).
	getResult := callTool(t, srv, "get_refactor_candidate", json.RawMessage(`{"project_id":"relay","candidate_id":"rc-a"}`))
	getSuccess := decodeBrokerSuccess(t, getResult)
	var getPayload struct {
		Candidate struct {
			Status string `json:"status"`
		} `json:"candidate"`
	}
	if err := json.Unmarshal(getSuccess.Result, &getPayload); err != nil {
		t.Fatalf("unmarshal candidate: %v", err)
	}
	if getPayload.Candidate.Status != "ready" {
		t.Errorf("expected candidate to remain ready after generation, got %q", getPayload.Candidate.Status)
	}
}

func TestRefactorBacklogToolsFileHasNoForbiddenCalls(t *testing.T) {
	// Source-scan guard: the MCP wrapper must not call plan submission, run
	// creation, executor dispatch, audit submission, shell, or git mutation.
	data, err := os.ReadFile("refactor_backlog_tools.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	src := string(data)
	for _, forbidden := range []string{
		"HandleSubmitPlannerPassPlan",
		"HandleCreateRunFromPlannerHandoff",
		"SubmitPlan",
		"CreateRun",
		"ExecuteRun",
		"SubmitAuditPacket",
		"exec.Command",
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("refactor_backlog_tools.go must not reference %q", forbidden)
		}
	}
}
