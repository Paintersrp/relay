package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHTTPHandlerAuth(t *testing.T) {
	srv := NewServer(discardLogger())

	t.Run("missing token rejected", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "super-secret-token")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "")
		h := NewHTTPHandler(srv, discardLogger())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{}`))))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "super-secret-token")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "")
		h := NewHTTPHandler(srv, discardLogger())
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Authorization", "Bearer wrong-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})

	t.Run("valid token accepted", func(t *testing.T) {
		t.Setenv("RELAY_MCP_AUTH_TOKEN", "super-secret-token")
		t.Setenv("RELAY_MCP_DISABLE_AUTH", "")
		h := NewHTTPHandler(srv, discardLogger())
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(mustMarshal(t, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})))
		req.Header.Set("Authorization", "Bearer super-secret-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHTTPHandlerCanonicalProtocol(t *testing.T) {
	harness := newCanonicalTestHarness(t, ToolProfilePlanner)
	handler := &HTTPHandler{server: harness.server, log: discardLogger(), disableAuth: true}

	t.Run("method not allowed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d", rec.Code)
		}
	})

	t.Run("initialize", func(t *testing.T) {
		resp := postMCPRequest(t, handler, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
			Params:  mustMarshal(t, InitializeParams{ProtocolVersion: MCPProtocolVersion}),
		})
		if resp.Error != nil {
			t.Fatalf("initialize error: %+v", resp.Error)
		}
	})

	t.Run("exact Planner tools", func(t *testing.T) {
		resp := postMCPRequest(t, handler, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`2`),
			Method:  "tools/list",
		})
		if resp.Error != nil {
			t.Fatalf("tools/list error: %+v", resp.Error)
		}
		var list ToolsListResult
		data, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(data, &list); err != nil {
			t.Fatal(err)
		}
		want := []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run"}
		if got := toolNames(list.Tools); !reflect.DeepEqual(got, want) {
			t.Fatalf("tools = %v, want %v", got, want)
		}
	})

	t.Run("structured file parameter reaches canonical handler", func(t *testing.T) {
		data := canonicalPlanBytes("relay")
		ref := harness.put("http-validate", "canonical-test.plan.json", data)
		args := canonicalArgs(t, artifactArgs{ArtifactFile: ref})
		resp := postMCPRequest(t, handler, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`3`),
			Method:  "tools/call",
			Params:  mustMarshal(t, ToolCallParams{Name: "validate_artifact", Arguments: args}),
		})
		if resp.Error != nil {
			t.Fatalf("tools/call error: %+v", resp.Error)
		}
		var result ToolCallResult
		body, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("validate_artifact failed: %s", canonicalToolText(t, result))
		}
		var out artifactValidationOutput
		if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &out); err != nil {
			t.Fatal(err)
		}
		if !out.OK || out.SHA256 != canonicalTestSHA(data) {
			t.Fatalf("unexpected validation output: %+v", out)
		}
	})

	t.Run("legacy tool is unknown", func(t *testing.T) {
		resp := postMCPRequest(t, handler, Request{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`4`),
			Method:  "tools/call",
			Params: mustMarshal(t, ToolCallParams{
				Name:      "create_run_from_planner_handoff",
				Arguments: json.RawMessage(`{}`),
			}),
		})
		if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
			t.Fatalf("legacy tool response = %+v", resp.Error)
		}
	})
}

func TestHTTPHandlerToolsListUsesCanonicalProfiles(t *testing.T) {
	t.Setenv("RELAY_MCP_DISABLE_AUTH", "true")
	tests := []struct {
		profile ToolProfile
		want    []string
	}{
		{profile: ToolProfilePlanner, want: []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run"}},
		{profile: ToolProfileAuditor, want: []string{"validate_artifact", "create_run", "get_audit_packet", "get_run_artifact", "record_audit_decision"}},
		{profile: ToolProfileLocalOperator, want: []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run", "get_audit_packet", "get_run_artifact", "record_audit_decision"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			handler := NewHTTPHandler(NewServer(discardLogger(), &MCPDeps{ToolProfile: tt.profile}), discardLogger())
			resp := postMCPRequest(t, handler, Request{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`1`),
				Method:  "tools/list",
			})
			if resp.Error != nil {
				t.Fatal(resp.Error)
			}
			var list ToolsListResult
			data, _ := json.Marshal(resp.Result)
			if err := json.Unmarshal(data, &list); err != nil {
				t.Fatal(err)
			}
			if got := toolNames(list.Tools); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tools = %v, want %v", got, tt.want)
			}
		})
	}
}

func postMCPRequest(t *testing.T, handler http.Handler, request Request) Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(mustMarshal(t, request)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var response Response
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}
