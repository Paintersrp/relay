package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPHandler_Auth(t *testing.T) {
	// Set up dependencies.
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	t.Run("MissingTokenReject", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "super-secret-token")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "")

		h := NewHTTPHandler(srv, discardLogger())
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("WrongTokenReject", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "super-secret-token")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "")

		h := NewHTTPHandler(srv, discardLogger())
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Authorization", "Bearer wrong-token")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("ValidTokenAccept", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "super-secret-token")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "")

		h := NewHTTPHandler(srv, discardLogger())
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer super-secret-token")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AuthDisabledAccept", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "true")

		h := NewHTTPHandler(srv, discardLogger())
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AuthMissingTokenUnconfiguredAccept", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "false")

		h := NewHTTPHandler(srv, discardLogger())
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHTTPHandler_Protocol(t *testing.T) {
	deps := setupTestDeps(t)
	deps.ToolProfile = ToolProfileRestricted
	srv := NewServer(discardLogger(), deps)
	h := &HTTPHandler{
		server:      srv,
		log:         discardLogger(),
		disableAuth: true,
	}

	t.Run("MethodNotAllowed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/mcp", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})

	t.Run("Initialize", func(t *testing.T) {
		params, _ := json.Marshal(InitializeParams{ProtocolVersion: MCPProtocolVersion})
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
			Params:  params,
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Error != nil {
			t.Fatalf("unexpected RPC error: %s", resp.Error.Message)
		}

		var initResult InitializeResult
		b, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(b, &initResult); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if initResult.ProtocolVersion != MCPProtocolVersion {
			t.Errorf("expected protocol version %q, got %q", MCPProtocolVersion, initResult.ProtocolVersion)
		}
	})

	t.Run("ToolsList", func(t *testing.T) {
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`2`),
			Method:  "tools/list",
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Error != nil {
			t.Fatalf("unexpected RPC error: %s", resp.Error.Message)
		}

		var list ToolsListResult
		b, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(b, &list); err != nil {
			t.Fatalf("unmarshal tools: %v", err)
		}

		approvedTools := map[string]bool{}
		for _, name := range baseToolNamesForTest() {
			approvedTools[name] = true
		}
		unsafeKeywords := []string{"exec", "shell", "read_file", "write_file", "git_commit", "git_push", "checkout", "reset", "branch"}

		if len(list.Tools) != len(baseToolNamesForTest()) {
			t.Errorf("expected exactly %d tools, got %d", len(baseToolNamesForTest()), len(list.Tools))
		}

		for _, tool := range list.Tools {
			if !approvedTools[tool.Name] {
				t.Errorf("unapproved tool registered: %q", tool.Name)
			}
			for _, unsafe := range unsafeKeywords {
				if contains(strings.ToLower(tool.Name), unsafe) {
					t.Errorf("unsafe tool name registered: %q contains unsafe keyword %q", tool.Name, unsafe)
				}
			}
		}
	})

	t.Run("CreateRunAndStatusFlow", func(t *testing.T) {
		// 1. Create run.
		markdown := "---\ntitle: HTTP Test Run\nrepo_target: http-repo\n---\n\n# HTTP Test Run\n\nContent for HTTP test."
		args, _ := json.Marshal(map[string]string{
			"planner_handoff_markdown": markdown,
			"repo_target":              "http-repo",
			"source":                   "http_test",
		})
		params, _ := json.Marshal(ToolCallParams{
			Name:      "create_run_from_planner_handoff",
			Arguments: args,
		})
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`3`),
			Method:  "tools/call",
			Params:  params,
		})

		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error != nil {
			t.Fatalf("unexpected RPC error creating run: %s", resp.Error.Message)
		}

		var createRes ToolCallResult
		b, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(b, &createRes)
		if createRes.IsError {
			t.Fatalf("tool error: %s", createRes.Content[0].Text)
		}

		var createOut createRunOutput
		_ = json.Unmarshal([]byte(createRes.Content[0].Text), &createOut)
		if !createOut.OK || createOut.RunID <= 0 {
			t.Fatalf("invalid create output: %+v", createOut)
		}

		runIDStr := fmt.Sprintf("%d", createOut.RunID)

		// 2. Get status.
		statusArgs, _ := json.Marshal(map[string]string{"run_id": runIDStr})
		statusParams, _ := json.Marshal(ToolCallParams{
			Name:      "get_run_status",
			Arguments: statusArgs,
		})
		statusBody := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`4`),
			Method:  "tools/call",
			Params:  statusParams,
		})

		req = httptest.NewRequest("POST", "/mcp", bytes.NewReader(statusBody))
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		var statusRes ToolCallResult
		b, _ = json.Marshal(resp.Result)
		_ = json.Unmarshal(b, &statusRes)
		if statusRes.IsError {
			t.Fatalf("tool error on get status: %s", statusRes.Content[0].Text)
		}

		var statusOut map[string]interface{}
		_ = json.Unmarshal([]byte(statusRes.Content[0].Text), &statusOut)
		if statusOut["run_id"] != runIDStr {
			t.Errorf("expected run_id %q, got %v", runIDStr, statusOut["run_id"])
		}

		// 3. List open runs.
		listParams, _ := json.Marshal(ToolCallParams{
			Name:      "list_open_runs",
			Arguments: json.RawMessage(`{"limit":5}`),
		})
		listBody := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`5`),
			Method:  "tools/call",
			Params:  listParams,
		})

		req = httptest.NewRequest("POST", "/mcp", bytes.NewReader(listBody))
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		var listRes ToolCallResult
		b, _ = json.Marshal(resp.Result)
		_ = json.Unmarshal(b, &listRes)
		var listOut listRunsOutput
		_ = json.Unmarshal([]byte(listRes.Content[0].Text), &listOut)
		found := false
		for _, r := range listOut.Runs {
			if r.RunID == runIDStr {
				found = true
			}
		}
		if !found {
			t.Errorf("created run %q not found in list_open_runs", runIDStr)
		}

		runIDInt, err := strconv.ParseInt(runIDStr, 10, 64)
		if err != nil {
			t.Fatalf("parse created run id: %v", err)
		}
		if _, err := deps.Store.UpdateRunStatus(runIDInt, "audit_ready"); err != nil {
			t.Fatalf("set audit_ready: %v", err)
		}

		// 4. Submit audit packet.
		auditArgs, _ := json.Marshal(map[string]string{
			"run_id":                runIDStr,
			"audit_packet_markdown": "# Audit success",
			"decision":              "accepted",
		})
		auditParams, _ := json.Marshal(ToolCallParams{
			Name:      "submit_audit_packet",
			Arguments: auditArgs,
		})
		auditBody := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`6`),
			Method:  "tools/call",
			Params:  auditParams,
		})

		req = httptest.NewRequest("POST", "/mcp", bytes.NewReader(auditBody))
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		var auditRes ToolCallResult
		b, _ = json.Marshal(resp.Result)
		_ = json.Unmarshal(b, &auditRes)
		if auditRes.IsError {
			t.Fatalf("audit tool error: %s", auditRes.Content[0].Text)
		}
	})

	t.Run("UnknownTool", func(t *testing.T) {
		params, _ := json.Marshal(ToolCallParams{Name: "nonexistent_tool"})
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`7`),
			Method:  "tools/call",
			Params:  params,
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
			t.Errorf("expected MethodNotFound error, got %+v", resp.Error)
		}
	})

	t.Run("InvalidArguments", func(t *testing.T) {
		params, _ := json.Marshal(ToolCallParams{
			Name:      "create_run_from_planner_handoff",
			Arguments: json.RawMessage(`{}`), // missing required markdown
		})
		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`8`),
			Method:  "tools/call",
			Params:  params,
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		var result ToolCallResult
		b, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(b, &result)
		if !result.IsError {
			t.Error("expected tool-level error for missing required arguments")
		}
		if len(result.Content) > 0 && !strings.Contains(result.Content[0].Text, "VALIDATION_ERROR") {
			t.Errorf("expected VALIDATION_ERROR, got %q", result.Content[0].Text)
		}
	})
}

func TestHTTPHandlerToolsListUsesServerToolSurface(t *testing.T) {
	t.Setenv("RELAY_MCP_DISABLE_AUTH", "true")

	t.Run("LocalOperatorProfileIncludesBrokerTools", func(t *testing.T) {
		srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileLocalOperator})
		h := NewHTTPHandler(srv, discardLogger())

		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  "tools/list",
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		var toolsResult ToolsListResult
		b, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(b, &toolsResult); err != nil {
			t.Fatalf("unmarshal tools: %v", err)
		}

		gotNames := toolNamesFromList(toolsResult.Tools)
		expected := append(baseToolNamesForTest(),
			"get_project",
			"get_plan",
			"get_pass",
			"get_pass_context",
			"get_next_pass_work",
			"get_next_audit_work",
			"create_source_snapshot",
			"list_project_files",
			"search_project_files",
			"read_project_file",
			"get_repository_git_status",
			"get_repository_recent_commit",
			"list_repository_changed_files",
			"get_repository_diff",
			"create_context_packet",
			"get_context_packet",
			"create_local_audit",
			"get_local_audit",
			"list_project_local_audits",
			"search_project_context_memory",
			"list_project_context_records",
			"get_project_context_record",
			"create_project_context_record",
			"supersede_project_context_record",
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
		)
		if !reflect.DeepEqual(gotNames, expected) {
			t.Errorf("expected tools:\n%v\ngot:\n%v", expected, gotNames)
		}
	})

	t.Run("RestrictedProfileExcludesBrokerTools", func(t *testing.T) {
		srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileRestricted})
		h := NewHTTPHandler(srv, discardLogger())

		body := mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`2`),
			Method:  "tools/list",
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		var toolsResult ToolsListResult
		b, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(b, &toolsResult); err != nil {
			t.Fatalf("unmarshal tools: %v", err)
		}

		gotNames := toolNamesFromList(toolsResult.Tools)
		expected := baseToolNamesForTest()
		if !reflect.DeepEqual(gotNames, expected) {
			t.Errorf("expected tools:\n%v\ngot:\n%v", expected, gotNames)
		}
	})
}

func toolNamesFromList(tools []ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
