package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appplans "relay/internal/app/plans"
	"relay/internal/store"
)

func setupOrchestratorTestStore(t *testing.T) *store.Store {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	testStore, err := store.Open(dbPath, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })
	return testStore
}

func TestOrchestratorWorkToolsListing(t *testing.T) {
	testStore := setupOrchestratorTestStore(t)

	tests := []struct {
		name             string
		profile          ToolProfile
		wantNextPassWork bool
		wantNextAudit    bool
	}{
		{
			name:             "local-operator profile includes orchestrator work tools",
			profile:          ToolProfileLocalOperator,
			wantNextPassWork: true,
			wantNextAudit:    true,
		},
		{
			name:             "restricted profile excludes orchestrator work tools",
			profile:          ToolProfileRestricted,
			wantNextPassWork: false,
			wantNextAudit:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := &MCPDeps{
				Store:       testStore,
				ToolProfile: tt.profile,
			}
			srv := NewServer(nil, deps)

			req := Request{
				ID:     json.RawMessage(`1`),
				Method: "tools/list",
				Params: json.RawMessage(`{}`),
			}

			resp := srv.handleLine(mustMarshalJSON(t, req))

			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}

			resultBytes, err := json.Marshal(resp.Result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}

			var result ToolsListResult
			if err := json.Unmarshal(resultBytes, &result); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}

			hasNextPassWork := false
			hasNextAudit := false
			for _, tool := range result.Tools {
				if tool.Name == appplans.NextPassWorkTool {
					hasNextPassWork = true
				}
				if tool.Name == appplans.NextAuditWorkTool {
					hasNextAudit = true
				}
			}

			if hasNextPassWork != tt.wantNextPassWork {
				t.Errorf("get_next_pass_work presence = %v, want %v", hasNextPassWork, tt.wantNextPassWork)
			}
			if hasNextAudit != tt.wantNextAudit {
				t.Errorf("get_next_audit_work presence = %v, want %v", hasNextAudit, tt.wantNextAudit)
			}
		})
	}
}

func TestOrchestratorWorkToolsRestrictedProfileReturnsMethodNotFound(t *testing.T) {
	testStore := setupOrchestratorTestStore(t)

	deps := &MCPDeps{
		Store:       testStore,
		ToolProfile: ToolProfileRestricted,
	}
	srv := NewServer(nil, deps)

	tests := []struct {
		name     string
		toolName string
	}{
		{
			name:     "get_next_pass_work returns method not found under restricted profile",
			toolName: appplans.NextPassWorkTool,
		},
		{
			name:     "get_next_audit_work returns method not found under restricted profile",
			toolName: appplans.NextAuditWorkTool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{
				ID:     json.RawMessage(`1`),
				Method: "tools/call",
				Params: mustMarshalJSON(t, ToolCallParams{
					Name:      tt.toolName,
					Arguments: json.RawMessage(`{"project_id":"test","plan_id":"test"}`),
				}),
			}

			resp := srv.handleLine(mustMarshalJSON(t, req))

			if resp.Error == nil {
				t.Fatal("expected error, got nil")
			}

			if resp.Error.Code != CodeMethodNotFound {
				t.Errorf("error code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
			}
		})
	}
}

func TestOrchestratorWorkToolsStrictArgumentDecoding(t *testing.T) {
	testStore := setupOrchestratorTestStore(t)

	deps := &MCPDeps{
		Store:       testStore,
		ToolProfile: ToolProfileLocalOperator,
	}
	srv := NewServer(nil, deps)

	tests := []struct {
		name     string
		toolName string
		args     string
	}{
		{
			name:     "get_next_pass_work rejects unknown fields",
			toolName: appplans.NextPassWorkTool,
			args:     `{"project_id":"test","plan_id":"test","unknown_field":"value"}`,
		},
		{
			name:     "get_next_audit_work rejects unknown fields",
			toolName: appplans.NextAuditWorkTool,
			args:     `{"project_id":"test","plan_id":"test","unknown_field":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{
				ID:     json.RawMessage(`1`),
				Method: "tools/call",
				Params: mustMarshalJSON(t, ToolCallParams{
					Name:      tt.toolName,
					Arguments: json.RawMessage(tt.args),
				}),
			}

			resp := srv.handleLine(mustMarshalJSON(t, req))

			if resp.Error != nil {
				t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
			}

			resultBytes, err := json.Marshal(resp.Result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}

			var result ToolCallResult
			if err := json.Unmarshal(resultBytes, &result); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}

			if !result.IsError {
				t.Error("expected IsError=true for unknown field, got false")
			}

			if len(result.Content) == 0 {
				t.Fatal("expected content block")
			}

			var payload map[string]interface{}
			if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
				t.Fatalf("unmarshal content: %v", err)
			}

			if ok, _ := payload["ok"].(bool); ok {
				t.Error("expected ok=false for unknown field, got true")
			}
		})
	}
}

func TestGetNextPassWorkUnknownProject(t *testing.T) {
	testStore := setupOrchestratorTestStore(t)

	deps := &MCPDeps{
		Store:       testStore,
		ToolProfile: ToolProfileLocalOperator,
	}
	srv := NewServer(nil, deps)

	req := Request{
		ID:     json.RawMessage(`1`),
		Method: "tools/call",
		Params: mustMarshalJSON(t, ToolCallParams{
			Name:      appplans.NextPassWorkTool,
			Arguments: json.RawMessage(`{"project_id":"missing","plan_id":"missing"}`),
		}),
	}

	resp := srv.handleLine(mustMarshalJSON(t, req))

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Error("expected IsError=false for business blocker, got true")
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content block")
	}

	var payload appplans.NextPassWorkResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.OK {
		t.Error("expected ok=false for unknown project, got true")
	}

	if payload.Tool != appplans.NextPassWorkTool {
		t.Errorf("tool = %q, want %q", payload.Tool, appplans.NextPassWorkTool)
	}

	if len(payload.Blockers) == 0 {
		t.Fatal("expected at least one blocker")
	}

	if payload.Blockers[0].Code != string(appplans.BlockerUnknownProject) {
		t.Errorf("blocker code = %q, want %q", payload.Blockers[0].Code, appplans.BlockerUnknownProject)
	}
}

func TestGetNextAuditWorkUnknownProject(t *testing.T) {
	testStore := setupOrchestratorTestStore(t)

	deps := &MCPDeps{
		Store:       testStore,
		ToolProfile: ToolProfileLocalOperator,
	}
	srv := NewServer(nil, deps)

	req := Request{
		ID:     json.RawMessage(`1`),
		Method: "tools/call",
		Params: mustMarshalJSON(t, ToolCallParams{
			Name:      appplans.NextAuditWorkTool,
			Arguments: json.RawMessage(`{"project_id":"missing","plan_id":"missing"}`),
		}),
	}

	resp := srv.handleLine(mustMarshalJSON(t, req))

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Error("expected IsError=false for business blocker, got true")
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content block")
	}

	var payload appplans.NextAuditWorkResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.OK {
		t.Error("expected ok=false for unknown project, got true")
	}

	if payload.Tool != appplans.NextAuditWorkTool {
		t.Errorf("tool = %q, want %q", payload.Tool, appplans.NextAuditWorkTool)
	}

	if len(payload.Blockers) == 0 {
		t.Fatal("expected at least one blocker")
	}

	if payload.Blockers[0].Code != string(appplans.BlockerUnknownProject) {
		t.Errorf("blocker code = %q, want %q", payload.Blockers[0].Code, appplans.BlockerUnknownProject)
	}
}

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}

// ----------------------------------------------------------------------------
// PASS-008 additions: schema strictness, store-less blockers, and
// success-through-tool coverage for the orchestrator work tools.
// ----------------------------------------------------------------------------

// schemaMap decodes a raw JSON schema into a generic map for assertions.
func schemaMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return m
}

// decodeToolJSON parses the first text content block of a tool result as JSON.
func decodeToolJSON(t *testing.T, result ToolCallResult) map[string]any {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal tool JSON: %v", err)
	}
	return payload
}

// seedMCPOrchestratorPlan submits a valid two-pass plan for project "relay"
// (which it also creates) using the real plans service, with no required
// context inputs so PASS-001 is immediately selectable.
func seedMCPOrchestratorPlan(t *testing.T, st *store.Store, planID string) *store.Plan {
	t.Helper()

	if _, err := st.CreateProject("relay", "Relay", "Orchestrator MCP test project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	noReqContext := func() appplans.ContextPlan {
		return appplans.ContextPlan{
			RequiredRepositories: []string{"relay"},
			SeedSearchTerms: []appplans.ContextSearchTerm{
				{RepoID: "relay", Query: "orchestrator work", Purpose: "Optional context.", Required: mcpBoolPtr(false)},
			},
			SeedFilesToRead: []appplans.ContextFileRead{
				{RepoID: "relay", Path: "internal/plans/work_packets.go", Purpose: "Optional file.", Required: mcpBoolPtr(false)},
			},
			ContextCoverageExpectations: []string{"Coverage is best-effort."},
			BlockedIfMissing:            []string{"Not blocked if missing."},
		}
	}
	noReqSnapshot := appplans.SourceSnapshotRequirements{
		RequireGitStatus:   mcpBoolPtr(false),
		RequireCommitSHA:   mcpBoolPtr(false),
		AllowDirtyWorktree: mcpBoolPtr(true),
	}

	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Orchestrator MCP test plan",
			Goal:          "Exercise orchestrator work tools through MCP.",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "test-profile",
				Mode:                 "submission_only",
				ContextBrokerEnabled: mcpBoolPtr(false),
			},
		},
		SourceIntent: appplans.SourceIntent{Summary: "MCP orchestrator work tool test plan."},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []appplans.PlanPassInput{
			{
				PassID:                     "PASS-001",
				Sequence:                   1,
				Name:                       "First pass",
				Goal:                       "First pass goal",
				IntendedExecutionScope:     []string{"internal/plans"},
				NonGoals:                   []string{"No UI"},
				Dependencies:               []string{},
				Status:                     "planned",
				PassType:                   "backend_vertical_slice",
				ContextPlan:                noReqContext(),
				SourceSnapshotRequirements: noReqSnapshot,
				HandoffReadinessCriteria:   []string{"Pass 1 complete"},
			},
			{
				PassID:                     "PASS-002",
				Sequence:                   2,
				Name:                       "Second pass",
				Goal:                       "Second pass goal",
				IntendedExecutionScope:     []string{"internal/plans"},
				NonGoals:                   []string{"No UI"},
				Dependencies:               []string{"PASS-001"},
				Status:                     "planned",
				PassType:                   "backend_vertical_slice",
				ContextPlan:                noReqContext(),
				SourceSnapshotRequirements: noReqSnapshot,
				HandoffReadinessCriteria:   []string{"Pass 2 complete"},
			},
		},
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	result, err := appplans.NewService(st).SubmitPlan(context.Background(), appplans.SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw, ProjectID: "relay"})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("SubmitPlan invalid: %+v", result.Report.Issues)
	}

	created, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	return created
}

func TestOrchestratorWorkTools_SchemasAreStrictAndScoped(t *testing.T) {
	t.Parallel()

	// get_next_pass_work schema.
	passSchema := schemaMap(t, ToolGetNextPassWork.InputSchema)
	if additional, _ := passSchema["additionalProperties"].(bool); additional {
		t.Error("get_next_pass_work schema must set additionalProperties:false")
	}
	passRequired := requiredSet(t, passSchema)
	for _, field := range []string{"project_id", "plan_id"} {
		if !passRequired[field] {
			t.Errorf("get_next_pass_work schema must require %q", field)
		}
	}
	passProps, _ := passSchema["properties"].(map[string]any)
	if _, ok := passProps["pass_id"]; !ok {
		t.Error("get_next_pass_work schema must define optional pass_id")
	}
	if passRequired["pass_id"] {
		t.Error("get_next_pass_work schema must not require pass_id")
	}

	// get_next_audit_work schema.
	auditSchema := schemaMap(t, ToolGetNextAuditWork.InputSchema)
	if additional, _ := auditSchema["additionalProperties"].(bool); additional {
		t.Error("get_next_audit_work schema must set additionalProperties:false")
	}
	auditRequired := requiredSet(t, auditSchema)
	for _, field := range []string{"project_id", "plan_id"} {
		if !auditRequired[field] {
			t.Errorf("get_next_audit_work schema must require %q", field)
		}
	}
	auditProps, _ := auditSchema["properties"].(map[string]any)
	for _, optional := range []string{"pass_id", "run_id"} {
		if _, ok := auditProps[optional]; !ok {
			t.Errorf("get_next_audit_work schema must define optional %q", optional)
		}
		if auditRequired[optional] {
			t.Errorf("get_next_audit_work schema must not require %q", optional)
		}
	}

	// Neither schema may expose mutation-oriented properties.
	mutationProps := []string{"planner_handoff_markdown", "audit_packet_markdown", "decision", "command", "path", "repo_path"}
	for _, schema := range []map[string]any{passSchema, auditSchema} {
		props, _ := schema["properties"].(map[string]any)
		for _, banned := range mutationProps {
			if _, ok := props[banned]; ok {
				t.Errorf("schema must not expose mutation-oriented property %q", banned)
			}
		}
	}
}

// requiredSet extracts the "required" array of a schema map as a set.
func requiredSet(t *testing.T, schema map[string]any) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	raw, ok := schema["required"].([]any)
	if !ok {
		return set
	}
	for _, item := range raw {
		if s, ok := item.(string); ok {
			set[s] = true
		}
	}
	return set
}

func TestOrchestratorWorkTools_HandlersReturnStructuredBlockersWithoutStore(t *testing.T) {
	t.Parallel()

	srv := NewServer(nil, &MCPDeps{Store: nil, ToolProfile: ToolProfileLocalOperator})

	cases := []struct {
		name string
		tool string
		call func(json.RawMessage) ToolCallResult
		args string
	}{
		{
			name: "get_next_pass_work",
			tool: appplans.NextPassWorkTool,
			call: srv.HandleGetNextPassWork,
			args: `{"project_id":"relay","plan_id":"plan-x"}`,
		},
		{
			name: "get_next_audit_work",
			tool: appplans.NextAuditWorkTool,
			call: srv.HandleGetNextAuditWork,
			args: `{"project_id":"relay","plan_id":"plan-x"}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := tc.call(json.RawMessage(tc.args))
			if !result.IsError {
				t.Fatal("expected IsError=true when store is unavailable")
			}
			payload := decodeToolJSON(t, result)
			if ok, _ := payload["ok"].(bool); ok {
				t.Error("expected ok=false")
			}
			if tool, _ := payload["tool"].(string); tool != tc.tool {
				t.Errorf("tool = %q, want %q", tool, tc.tool)
			}
			blockers, _ := payload["blockers"].([]any)
			if len(blockers) == 0 {
				t.Fatal("expected at least one blocker")
			}
			first, _ := blockers[0].(map[string]any)
			if code, _ := first["code"].(string); code != appplans.BlockerUnsafeRequest {
				t.Errorf("blocker code = %q, want %q", code, appplans.BlockerUnsafeRequest)
			}
		})
	}
}

func TestOrchestratorWorkTools_GetNextPassWorkSuccessThroughTool(t *testing.T) {
	t.Parallel()

	st := setupOrchestratorTestStore(t)
	plan := seedMCPOrchestratorPlan(t, st, "plan-mcp-passwork")
	_ = plan

	srv := NewServer(nil, &MCPDeps{Store: st, ToolProfile: ToolProfileLocalOperator})

	result := srv.HandleGetNextPassWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-passwork"}`))
	if result.IsError {
		t.Fatalf("expected IsError=false, got error result: %+v", result.Content)
	}

	var resp appplans.NextPassWorkResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.Tool != appplans.NextPassWorkTool {
		t.Errorf("tool = %q, want %q", resp.Tool, appplans.NextPassWorkTool)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 selected, got %+v", resp.SelectedPass)
	}
	if resp.SuggestedRunSubmission == nil {
		t.Fatal("expected suggested_run_submission")
	}

	// The suggested run submission must include only plan_id and pass_id.
	payload := decodeToolJSON(t, result)
	suggested, _ := payload["suggested_run_submission"].(map[string]any)
	args, _ := suggested["arguments"].(map[string]any)
	if len(args) != 2 {
		t.Fatalf("expected exactly 2 suggested arguments (plan_id, pass_id), got %d: %v", len(args), args)
	}
	if _, ok := args["plan_id"]; !ok {
		t.Error("suggested arguments missing plan_id")
	}
	if _, ok := args["pass_id"]; !ok {
		t.Error("suggested arguments missing pass_id")
	}

	// Verify jumpstart field is present in the JSON payload.
	js, _ := payload["planner_jumpstart"].(map[string]any)
	if js == nil {
		t.Fatal("expected planner_jumpstart in response JSON")
	}
	if state, _ := js["readiness_state"].(string); state != "ready" {
		t.Errorf("expected readiness_state=ready, got %q", state)
	}
	if summary, _ := js["selected_pass_summary"].(map[string]any); summary == nil {
		t.Error("expected selected_pass_summary in planner_jumpstart")
	}
	checklist, _ := js["handoff_preflight_checklist"].([]any)
	if len(checklist) == 0 {
		t.Error("expected non-empty handoff_preflight_checklist")
	}
}

func TestOrchestratorWorkTools_GetNextPassWorkPlannerJumpstartActions(t *testing.T) {
	t.Parallel()

	st := setupOrchestratorTestStore(t)
	if _, err := st.CreateProject("relay", "Relay", "Orchestrator MCP test project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planSvc := appplans.NewService(st)
	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        "plan-mcp-jumpstart-actions",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Orchestrator MCP jumpstart test plan",
			Goal:          "Exercise jumpstart action payloads.",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "test-profile",
				Mode:                 "submission_only",
				ContextBrokerEnabled: mcpBoolPtr(false),
			},
		},
		SourceIntent: appplans.SourceIntent{Summary: "MCP jumpstart action test plan."},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []appplans.PlanPassInput{{
			PassID: "PASS-001", Sequence: 1, Name: "Context pass", Goal: "Collect context.",
			IntendedExecutionScope: []string{"Inspect jumpstart actions."},
			NonGoals:               []string{"No run creation."},
			Dependencies:           []string{},
			Status:                 appplans.StatusPassPlanned,
			PassType:               "backend_vertical_slice",
			ContextPlan: appplans.ContextPlan{
				RequiredRepositories: []string{"relay"},
				SeedSearchTerms: []appplans.ContextSearchTerm{
					{RepoID: "relay", Query: "planner_jumpstart", Purpose: "Find jumpstart code.", Required: mcpBoolPtr(true)},
				},
				SeedFilesToRead: []appplans.ContextFileRead{
					{RepoID: "relay", Path: "internal/app/plans/work_packets.go", Purpose: "Review work packet logic.", Required: mcpBoolPtr(true)},
				},
				ContextCoverageExpectations: []string{"Jumpstart action contract is covered."},
				BlockedIfMissing:            []string{"Action payload cannot be checked."},
			},
			SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
				RequireGitStatus:   mcpBoolPtr(false),
				RequireCommitSHA:   mcpBoolPtr(false),
				AllowDirtyWorktree: mcpBoolPtr(true),
			},
			HandoffReadinessCriteria: []string{"Jumpstart payload reviewed."},
		}},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	submitResult, err := planSvc.SubmitPlan(context.Background(), appplans.SubmitPlanRequest{
		RawJSON:               raw,
		UnmanagedAcknowledged: true,
	})
	if err != nil || !submitResult.Report.Valid {
		t.Fatalf("SubmitPlan failed: err=%v issues=%+v", err, submitResult.Report.Issues)
	}

	srv := NewServer(nil, &MCPDeps{Store: st, ToolProfile: ToolProfileLocalOperator})
	toolResult := srv.HandleGetNextPassWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-jumpstart-actions"}`))
	if toolResult.IsError {
		t.Fatalf("expected IsError=false, got error result: %+v", toolResult.Content)
	}
	payload := decodeToolJSON(t, toolResult)
	js, _ := payload["planner_jumpstart"].(map[string]any)
	if js == nil {
		t.Fatal("expected planner_jumpstart in response JSON")
	}
	actions, _ := js["suggested_context_acquisition_actions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("expected two suggested actions, got %d: %#v", len(actions), actions)
	}
	first, _ := actions[0].(map[string]any)
	second, _ := actions[1].(map[string]any)
	if first["tool"] != "create_source_snapshot" || second["tool"] != "create_context_packet" {
		t.Fatalf("unexpected actions: %#v", actions)
	}
	if second["depends_on"] != "create_source_snapshot" {
		t.Fatalf("expected depends_on create_source_snapshot, got %#v", second["depends_on"])
	}
	bindings, _ := second["argument_bindings"].(map[string]any)
	if bindings["source_snapshot_id"] != "$.result.source_snapshot_id" {
		t.Fatalf("expected source_snapshot_id binding, got %#v", bindings)
	}
	args, _ := second["arguments"].(map[string]any)
	for _, key := range []string{"project_id", "plan_id", "pass_id", "task_slug", "seed_files", "seed_searches", "include_inventory", "max_sources", "max_total_bytes"} {
		if _, ok := args[key]; !ok {
			t.Fatalf("expected create_context_packet arguments to include %q: %#v", key, args)
		}
	}
	if _, ok := args["source_snapshot_id"]; ok {
		t.Fatalf("did not expect static source_snapshot_id without a snapshot: %#v", args)
	}
}

func TestOrchestratorWorkTools_GetNextPassWorkWithPassID(t *testing.T) {
	t.Parallel()

	st := setupOrchestratorTestStore(t)
	plan := seedMCPOrchestratorPlan(t, st, "plan-mcp-passid")
	_ = plan

	srv := NewServer(nil, &MCPDeps{Store: st, ToolProfile: ToolProfileLocalOperator})

	// Request PASS-001 explicitly (the earliest eligible pass).
	result := srv.HandleGetNextPassWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-passid","pass_id":"PASS-001"}`))
	if result.IsError {
		t.Fatalf("expected IsError=false, got error result: %+v", result.Content)
	}
	var resp appplans.NextPassWorkResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true for requested PASS-001, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 selected, got %+v", resp.SelectedPass)
	}
	if resp.PlannerJumpstart == nil {
		t.Fatal("expected planner_jumpstart for pass_id request")
	}
}

func TestOrchestratorWorkTools_GetNextAuditWorkSuccessThroughTool(t *testing.T) {
	t.Parallel()

	st := setupOrchestratorTestStore(t)
	plan := seedMCPOrchestratorPlan(t, st, "plan-mcp-auditwork")

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	pass1, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}
	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"audit-ready run",
		"audit_ready",
		"", "", "opencode_go", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass1.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRunWithAssociation: %v", err)
	}
	if _, err := st.CreateArtifact(run.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact audit_packet: %v", err)
	}
	if _, err := st.CreateArtifact(run.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact audit_evidence_manifest_json: %v", err)
	}
	if _, err := st.UpdatePlanPassStatus(pass1.ID, appplans.StatusPassAuditReady); err != nil {
		t.Fatalf("UpdatePlanPassStatus: %v", err)
	}

	srv := NewServer(nil, &MCPDeps{Store: st, ToolProfile: ToolProfileLocalOperator})

	result := srv.HandleGetNextAuditWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-auditwork"}`))
	if result.IsError {
		t.Fatalf("expected IsError=false, got error result: %+v", result.Content)
	}

	var resp appplans.NextAuditWorkResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.Tool != appplans.NextAuditWorkTool {
		t.Errorf("tool = %q, want %q", resp.Tool, appplans.NextAuditWorkTool)
	}
	if resp.SelectedRun == nil || resp.SelectedRun.RunID == "" {
		t.Fatalf("expected a selected run, got %+v", resp.SelectedRun)
	}
	if len(resp.AllowedDecisions) == 0 {
		t.Error("expected allowed_decisions in response")
	}

	// The response must not dump full artifact content; only references.
	if strings.Contains(result.Content[0].Text, "\"content\":") {
		t.Error("audit work response must not embed full artifact content")
	}
}
