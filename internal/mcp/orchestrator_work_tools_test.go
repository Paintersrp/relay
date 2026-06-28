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
	"relay/internal/app/projects"
	"relay/internal/sources"
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

	payload := decodeNextPassSummary(t, result)
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

func decodeNextPassSummary(t *testing.T, result ToolCallResult) appplans.NextPassWorkMCPSummary {
	t.Helper()
	if result.StructuredContent == nil {
		t.Fatal("expected structuredContent")
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var summary appplans.NextPassWorkMCPSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	return summary
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
	outputSchema := schemaMap(t, ToolGetNextPassWork.OutputSchema)
	outputRequired := requiredSet(t, outputSchema)
	for _, field := range []string{"ok", "tool", "context_ready", "blockers", "local_preview_hint"} {
		if !outputRequired[field] {
			t.Errorf("get_next_pass_work outputSchema must require %q", field)
		}
	}
	outputProps, _ := outputSchema["properties"].(map[string]any)
	for _, field := range []string{"handoff_work", "handoff_authoring_packet"} {
		if _, ok := outputProps[field]; !ok {
			t.Errorf("get_next_pass_work outputSchema must define %q", field)
		}
	}
	if ToolGetNextPassWork.Annotations["readOnlyHint"] != false {
		t.Error("get_next_pass_work annotations must include readOnlyHint=false")
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

	resp := decodeNextPassSummary(t, result)
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.Tool != appplans.NextPassWorkTool {
		t.Errorf("tool = %q, want %q", resp.Tool, appplans.NextPassWorkTool)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 selected, got %+v", resp.SelectedPass)
	}
	if resp.ReadinessState != "ready_for_handoff_authoring" {
		t.Errorf("expected readiness_state=ready_for_handoff_authoring, got %q", resp.ReadinessState)
	}
	if len(resp.NextActions) == 0 || resp.NextActions[0].Tool != "draft_planner_handoff" {
		t.Fatalf("expected draft_planner_handoff next action, got %+v", resp.NextActions)
	}
	if resp.HandoffWork == nil || resp.HandoffPacket == nil {
		t.Fatalf("expected handoff_work and handoff_authoring_packet in structuredContent: %+v", resp)
	}
	if resp.HandoffWork.PlanID != "plan-mcp-passwork" || resp.HandoffWork.PassID != "PASS-001" {
		t.Fatalf("unexpected handoff_work IDs: %+v", resp.HandoffWork)
	}
	if resp.HandoffWork.SuggestedAuthoringAction != "draft_planner_handoff" {
		t.Fatalf("unexpected authoring action: %+v", resp.HandoffWork)
	}
	for _, action := range resp.NextActions {
		if action.Tool == "create_run_from_planner_handoff" {
			t.Fatalf("ready planned pass must not suggest run submission without reviewed handoff: %+v", resp.NextActions)
		}
	}
	if !strings.Contains(result.Content[0].Text, "Use the Relay pass-detail preview") {
		t.Fatalf("expected local preview hint in text, got %q", result.Content[0].Text)
	}
	if strings.Contains(result.Content[0].Text, "create_run_from_planner_handoff") {
		t.Fatalf("text must describe handoff authoring, not run submission: %q", result.Content[0].Text)
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
	payload := decodeNextPassSummary(t, toolResult)
	if payload.ReadinessState != "needs_context_packet" {
		t.Fatalf("expected readiness_state=needs_context_packet, got %q", payload.ReadinessState)
	}
	if len(payload.NextActions) != 2 {
		t.Fatalf("expected two concise next actions, got %d: %#v", len(payload.NextActions), payload.NextActions)
	}
	if payload.NextActions[0].Tool != "create_source_snapshot" || payload.NextActions[1].Tool != "create_context_packet" {
		t.Fatalf("unexpected actions: %#v", payload.NextActions)
	}
	args := payload.NextActions[1].Arguments
	for _, key := range []string{"project_id", "plan_id", "pass_id", "task_slug", "seed_files", "seed_searches", "include_inventory", "max_sources", "max_total_bytes"} {
		if _, ok := args[key]; !ok {
			t.Fatalf("expected create_context_packet arguments to include %q: %#v", key, args)
		}
	}
	if payload.NextActions[1].DependsOn != "create_source_snapshot" {
		t.Fatalf("expected create_context_packet depends_on create_source_snapshot, got %#v", payload.NextActions[1])
	}
	if payload.NextActions[1].ArgumentBindings["source_snapshot_id"] != "$.result.source_snapshot_id" {
		t.Fatalf("expected source_snapshot_id binding, got %#v", payload.NextActions[1].ArgumentBindings)
	}
}

func TestOrchestratorWorkTools_GetNextPassWorkActionFeedsCreateContextPacket(t *testing.T) {
	t.Parallel()

	deps := setupTestDeps(t)
	deps.ToolProfile = ToolProfileLocalOperator
	deps.ContextBrokerEnabled = true
	st := deps.Store
	srv := NewServer(nil, deps)

	projectService := projects.NewService(st)
	sourceService := sources.NewService(st)
	repoRoot := brokerSetupGitRepo(t)
	brokerMkdirAll(t, filepath.Join(repoRoot, "src"))
	brokerWriteFile(t, filepath.Join(repoRoot, "src", "app.txt"), "alpha\nneedle\n")
	brokerRunGit(t, repoRoot, "add", ".")
	brokerRunGit(t, repoRoot, "commit", "-m", "next pass alias fixture")

	project, err := projectService.GetProjectByProjectID(context.Background(), "relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	_, issues, err := projectService.UpsertProjectRepository(context.Background(), project.ProjectID, projects.ProjectRepositoryInput{
		RepoID:           "Paintersrp/relay",
		Role:             projects.RepositoryRolePrimary,
		LocalPath:        repoRoot,
		DefaultBranch:    "main",
		AllowedRoots:     []string{"src"},
		MaxFileSizeBytes: projects.MinMaxFileSizeBytes,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("UpsertProjectRepository: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected repository issues: %+v", issues)
	}
	_, err = sourceService.CreateSourceSnapshot(context.Background(), sources.SourceSnapshotInput{
		ProjectID:           project.ProjectID,
		RepoIDs:             []string{"relay"},
		IncludeFileMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot: %v", err)
	}

	planSvc := appplans.NewService(st)
	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        "plan-mcp-action-chain",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Action chain",
			Goal:          "Verify action chain.",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "test-profile",
				Mode:                 "submission_only",
				ContextBrokerEnabled: mcpBoolPtr(true),
			},
		},
		SourceIntent: appplans.SourceIntent{Summary: "Action chain test."},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []appplans.PlanPassInput{{
			PassID: "PASS-002", Sequence: 2, Name: "Context pass", Goal: "Collect context.",
			IntendedExecutionScope: []string{"src/app.txt"},
			NonGoals:               []string{"No run creation."},
			Dependencies:           []string{},
			Status:                 appplans.StatusPassPlanned,
			PassType:               "backend_vertical_slice",
			ContextPlan: appplans.ContextPlan{
				RequiredRepositories: []string{"relay"},
				SeedSearchTerms: []appplans.ContextSearchTerm{
					{RepoID: "relay", Query: "needle", Purpose: "Find fixture marker.", Required: mcpBoolPtr(true)},
				},
				SeedFilesToRead: []appplans.ContextFileRead{
					{RepoID: "relay", Path: "src/app.txt", Purpose: "Read fixture source.", Required: mcpBoolPtr(true)},
				},
				ContextCoverageExpectations: []string{"Fixture source is covered."},
				BlockedIfMissing:            []string{"Fixture source cannot be located."},
			},
			SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
				RequireGitStatus:   mcpBoolPtr(false),
				RequireCommitSHA:   mcpBoolPtr(false),
				AllowDirtyWorktree: mcpBoolPtr(true),
			},
			HandoffReadinessCriteria: []string{"Context packet exists."},
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

	toolResult := srv.HandleGetNextPassWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-action-chain"}`))
	if toolResult.IsError {
		t.Fatalf("get_next_pass_work failed: %+v", toolResult.Content)
	}
	payload := decodeNextPassSummary(t, toolResult)
	// With one-call acquisition, the work service creates the context packet internally
	// and returns ready_for_handoff_authoring with handoff_work.
	if !payload.OK || payload.ReadinessState != "ready_for_handoff_authoring" {
		t.Fatalf("expected ready_for_handoff_authoring, got ok=%v readiness=%q: %+v", payload.OK, payload.ReadinessState, payload)
	}
	if payload.HandoffWork == nil {
		t.Fatal("expected handoff_work in structuredContent")
	}
	var foundDraftAction bool
	for _, act := range payload.NextActions {
		if act.Tool == "draft_planner_handoff" {
			foundDraftAction = true
		}
	}
	if !foundDraftAction {
		t.Fatalf("expected draft_planner_handoff in next_actions, got %+v", payload.NextActions)
	}
	// Verify the handoff packet has proper IDs
	if payload.HandoffWork.ProjectID != "relay" || payload.HandoffWork.PlanID != "plan-mcp-action-chain" || payload.HandoffWork.PassID != "PASS-002" {
		t.Fatalf("unexpected handoff_work IDs: %+v", payload.HandoffWork)
	}
	if payload.AcquisitionSummary == nil || !payload.AcquisitionSummary.ContextPacketCreated {
		t.Fatalf("expected acquisition_summary with context_packet_created=true: %+v", payload.AcquisitionSummary)
	}
}

func TestOrchestratorWorkTools_GetNextPassWorkTextOmitsVerboseHookProse(t *testing.T) {
	t.Parallel()

	st := setupOrchestratorTestStore(t)
	if _, err := st.CreateProject("relay", "Relay", "Orchestrator MCP test project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	verbose := "pre-commit, pre-push, and ordinary commit/push flow details stay in local preview only."
	planSvc := appplans.NewService(st)
	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        "plan-mcp-compact-text",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Compact text test plan",
			Goal:          "Exercise compact MCP output.",
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
		SourceIntent:       appplans.SourceIntent{Summary: "MCP compact text test plan."},
		GlobalContextRules: &appplans.GlobalContextRules{DefaultSourceOfTruth: "Relay managed plan.", PlannerContextBoundary: "Test only.", ForbiddenContextDomains: []string{"GitHub issues"}},
		Passes: []appplans.PlanPassInput{{
			PassID: "PASS-002", Sequence: 2, Name: "Context packet pass", Goal: verbose,
			IntendedExecutionScope: []string{"Inspect compact output."},
			NonGoals:               []string{"No run creation."},
			Dependencies:           []string{},
			Status:                 appplans.StatusPassPlanned,
			PassType:               "backend_vertical_slice",
			ContextPlan: appplans.ContextPlan{
				RequiredRepositories: []string{"relay"},
				SeedSearchTerms: []appplans.ContextSearchTerm{
					{RepoID: "relay", Query: "pre-commit", Purpose: verbose, Required: mcpBoolPtr(true)},
				},
				SeedFilesToRead: []appplans.ContextFileRead{
					{RepoID: "relay", Path: "internal/app/plans/work_packets.go", Purpose: verbose, Required: mcpBoolPtr(true)},
				},
				ContextCoverageExpectations: []string{verbose},
				BlockedIfMissing:            []string{verbose},
			},
			SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
				RequireGitStatus:   mcpBoolPtr(false),
				RequireCommitSHA:   mcpBoolPtr(false),
				AllowDirtyWorktree: mcpBoolPtr(true),
			},
			HandoffReadinessCriteria: []string{"Context packet exists."},
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

	fullResp, err := appplans.NewOrchestratorWorkService(st).GetNextPassWork(context.Background(), appplans.NextPassWorkRequest{
		ProjectID: "relay",
		PlanID:    "plan-mcp-compact-text",
	})
	if err != nil {
		t.Fatalf("GetNextPassWork: %v", err)
	}
	fullJSON, err := json.Marshal(fullResp)
	if err != nil {
		t.Fatalf("marshal full response: %v", err)
	}
	if !strings.Contains(string(fullJSON), "pre-commit") {
		t.Fatal("expected full service response to preserve verbose hook text")
	}

	srv := NewServer(nil, &MCPDeps{Store: st, ToolProfile: ToolProfileLocalOperator})
	result := srv.HandleGetNextPassWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-compact-text"}`))
	if result.IsError {
		t.Fatalf("expected IsError=false, got error result: %+v", result.Content)
	}
	for _, banned := range []string{"pre-commit", "pre-push", "ordinary commit/push flow"} {
		if strings.Contains(result.Content[0].Text, banned) {
			t.Fatalf("MCP text leaked verbose text %q: %s", banned, result.Content[0].Text)
		}
	}
	summary := decodeNextPassSummary(t, result)
	if summary.SelectedPass == nil || summary.SelectedPass.PassID != "PASS-002" {
		t.Fatalf("expected PASS-002 selected, got %+v", summary.SelectedPass)
	}
	if summary.ReadinessState != "needs_context_packet" {
		t.Fatalf("expected needs_context_packet, got %q", summary.ReadinessState)
	}
	if len(summary.Blockers) == 0 || (summary.Blockers[0].Code != appplans.BlockerRequiredContextPacketMissing && summary.Blockers[0].Code != appplans.BlockerContextPacketAcquisitionFailed) {
		t.Fatalf("expected context-packet blocker, got %+v", summary.Blockers)
	}
	if !strings.Contains(summary.LocalPreviewHint, "pass-detail preview") {
		t.Fatalf("expected local preview hint, got %q", summary.LocalPreviewHint)
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
	resp := decodeNextPassSummary(t, result)
	if !resp.OK {
		t.Fatalf("expected ok=true for requested PASS-001, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedPass == nil || resp.SelectedPass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001 selected, got %+v", resp.SelectedPass)
	}
	if resp.ReadinessState == "" {
		t.Fatal("expected readiness_state for pass_id request")
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

func TestOrchestratorWorkTools_GetNextPassWork_ContextPacketUsability(t *testing.T) {
	t.Parallel()

	st := setupOrchestratorTestStore(t)
	if _, err := st.CreateProject("relay", "Relay", "Orchestrator MCP test project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	project, err := st.GetProjectByProjectID("relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}

	// Seed source snapshot
	if _, err := st.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: "snap-mcp-1",
		ProjectRowID:     project.ID,
		ProjectID:        project.ProjectID,
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-28T00:00:00Z",
		SummaryJSON:      "{}",
	}); err != nil {
		t.Fatalf("CreateSourceSnapshot: %v", err)
	}

	planSvc := appplans.NewService(st)
	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        "plan-mcp-usability",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Orchestrator MCP usability test plan",
			Goal:          "Exercise context packet usability.",
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
		SourceIntent: appplans.SourceIntent{Summary: "MCP context packet usability test plan."},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []appplans.PlanPassInput{{
			PassID: "PASS-001", Sequence: 1, Name: "Context pass", Goal: "Collect context.",
			IntendedExecutionScope: []string{"Inspect usability."},
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
				ContextCoverageExpectations: []string{"Usability contract is covered."},
				BlockedIfMissing:            []string{"Action payload cannot be checked."},
			},
			SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
				RequireGitStatus:   mcpBoolPtr(false),
				RequireCommitSHA:   mcpBoolPtr(false),
				AllowDirtyWorktree: mcpBoolPtr(true),
			},
			HandoffReadinessCriteria: []string{"Usability packet reviewed."},
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

	// Seed unusable context packet (status blocked)
	_, err = st.CreateContextPacket(store.CreateContextPacketParams{
		ContextPacketID:     "packet-mcp-unusable",
		ProjectRowID:        project.ID,
		ProjectID:           project.ProjectID,
		PlanID:              "plan-mcp-usability",
		PassID:              "PASS-001",
		TaskSlug:            "slug",
		SourceSnapshotRowID: 1, // dummy
		SourceSnapshotID:    "snap-mcp-1",
		Status:              "blocked", // blocked makes it unusable
		BlockedSeedCount:    0,
		MissingSeedCount:    0,
		CompletedAt:         "2026-06-28T12:00:00Z",
		PacketJSONPath:      "/artifacts/ctxpkt/packet-mcp-unusable.json",
		CoverageReportPath:  "/artifacts/ctxpkt/packet-mcp-unusable-coverage.json",
	})
	if err != nil {
		t.Fatalf("CreateContextPacket: %v", err)
	}

	srv := NewServer(nil, &MCPDeps{Store: st, ToolProfile: ToolProfileLocalOperator})

	// 1. Unusable packet check
	toolResult := srv.HandleGetNextPassWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-usability"}`))
	if toolResult.IsError {
		t.Fatalf("expected IsError=false, got: %+v", toolResult.Content)
	}
	payload := decodeNextPassSummary(t, toolResult)
	if payload.OK {
		t.Fatal("expected ok=false for unusable context packet")
	}
	if payload.HandoffWork != nil || payload.HandoffPacket != nil {
		t.Fatal("expected handoff_work and handoff_authoring_packet to be nil in structured content when unusable")
	}
	if payload.ReadinessState != "needs_context_packet" {
		t.Errorf("expected readiness_state=needs_context_packet, got %q", payload.ReadinessState)
	}
	var foundCreateAction bool
	for _, act := range payload.NextActions {
		if act.Tool == "create_context_packet" {
			foundCreateAction = true
			if act.Arguments["source_snapshot_id"] != "snap-mcp-1" {
				t.Errorf("expected suggested action source_snapshot_id=\"snap-mcp-1\", got %q", act.Arguments["source_snapshot_id"])
			}
		}
		if act.Tool == "draft_planner_handoff" {
			t.Fatal("did not expect draft_planner_handoff next action when context is unusable")
		}
	}
	if !foundCreateAction {
		t.Fatal("expected create_context_packet action")
	}

	// 2. Usable packet check
	_, err = st.CreateContextPacket(store.CreateContextPacketParams{
		ContextPacketID:     "packet-mcp-usable",
		ProjectRowID:        project.ID,
		ProjectID:           project.ProjectID,
		PlanID:              "plan-mcp-usability",
		PassID:              "PASS-001",
		TaskSlug:            "slug",
		SourceSnapshotRowID: 1, // dummy
		SourceSnapshotID:    "snap-mcp-1",
		Status:              "created", // created is usable
		BlockedSeedCount:    0,
		MissingSeedCount:    0,
		CompletedAt:         "2026-06-28T13:00:00Z", // later completed_at makes it latest
		PacketJSONPath:      "/artifacts/ctxpkt/packet-mcp-usable.json",
		CoverageReportPath:  "/artifacts/ctxpkt/packet-mcp-usable-coverage.json",
	})
	if err != nil {
		t.Fatalf("CreateContextPacket: %v", err)
	}

	toolResult = srv.HandleGetNextPassWork(json.RawMessage(`{"project_id":"relay","plan_id":"plan-mcp-usability"}`))
	if toolResult.IsError {
		t.Fatalf("expected IsError=false, got: %+v", toolResult.Content)
	}
	payload = decodeNextPassSummary(t, toolResult)
	if !payload.OK {
		t.Fatalf("expected ok=true for usable context packet, got blockers: %+v", payload.Blockers)
	}
	if payload.HandoffWork == nil || payload.HandoffPacket == nil {
		t.Fatal("expected handoff_work and handoff_authoring_packet to be non-nil when usable")
	}
	if payload.ReadinessState != "ready_for_handoff_authoring" {
		t.Errorf("expected readiness_state=ready_for_handoff_authoring, got %q", payload.ReadinessState)
	}
	var foundDraftAction bool
	for _, act := range payload.NextActions {
		if act.Tool == "draft_planner_handoff" {
			foundDraftAction = true
		}
	}
	if !foundDraftAction {
		t.Fatal("expected draft_planner_handoff action when usable")
	}
}
