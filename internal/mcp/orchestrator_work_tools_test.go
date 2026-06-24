package mcp

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"relay/internal/plans"
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
				if tool.Name == plans.NextPassWorkTool {
					hasNextPassWork = true
				}
				if tool.Name == plans.NextAuditWorkTool {
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
			toolName: plans.NextPassWorkTool,
		},
		{
			name:     "get_next_audit_work returns method not found under restricted profile",
			toolName: plans.NextAuditWorkTool,
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
			toolName: plans.NextPassWorkTool,
			args:     `{"project_id":"test","plan_id":"test","unknown_field":"value"}`,
		},
		{
			name:     "get_next_audit_work rejects unknown fields",
			toolName: plans.NextAuditWorkTool,
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
			Name:      plans.NextPassWorkTool,
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

	var payload plans.NextPassWorkResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.OK {
		t.Error("expected ok=false for unknown project, got true")
	}

	if payload.Tool != plans.NextPassWorkTool {
		t.Errorf("tool = %q, want %q", payload.Tool, plans.NextPassWorkTool)
	}

	if len(payload.Blockers) == 0 {
		t.Fatal("expected at least one blocker")
	}

	if payload.Blockers[0].Code != plans.BlockerUnknownProject {
		t.Errorf("blocker code = %q, want %q", payload.Blockers[0].Code, plans.BlockerUnknownProject)
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
			Name:      plans.NextAuditWorkTool,
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

	var payload plans.NextAuditWorkResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.OK {
		t.Error("expected ok=false for unknown project, got true")
	}

	if payload.Tool != plans.NextAuditWorkTool {
		t.Errorf("tool = %q, want %q", payload.Tool, plans.NextAuditWorkTool)
	}

	if len(payload.Blockers) == 0 {
		t.Fatal("expected at least one blocker")
	}

	if payload.Blockers[0].Code != plans.BlockerUnknownProject {
		t.Errorf("blocker code = %q, want %q", payload.Blockers[0].Code, plans.BlockerUnknownProject)
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
