package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"relay/internal/mcp"
	"relay/internal/mcp/routecontracts"
)

var ingressListenerEnvironment = []string{
	"RELAY_MCP_INGRESS_WAYFINDER_WORKSPACE_ADDR",
	"RELAY_MCP_INGRESS_WAYFINDER_DISCOVERY_ADDR",
	"RELAY_MCP_INGRESS_WAYFINDER_INVESTIGATION_ADDR",
	"RELAY_MCP_INGRESS_PLANNER_AUTHORING_ADDR",
	"RELAY_MCP_INGRESS_PLANNER_FRONTIER_ADDR",
	"RELAY_MCP_INGRESS_AUDITOR_REVIEW_ADDR",
	"RELAY_MCP_INGRESS_AUDITOR_AUDIT_ADDR",
}

func TestPrivateMCPIngressRoutesTraceAndFailureIsolation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RELAY_WORKFLOW_DB_PATH", filepath.Join(root, "workflow.sqlite"))
	t.Setenv("RELAY_WORKFLOW_ARTIFACTS_DIR", filepath.Join(root, "artifacts"))
	t.Setenv("RELAY_SOURCE_VAULT_DIR", filepath.Join(root, "source-vaults"))
	traceRoot := filepath.Join(root, "traces")
	t.Setenv("RELAY_MCP_TRACE_DIR", traceRoot)
	t.Setenv("RELAY_MCP_TRACE_MAX_AGE", "1h")
	t.Setenv("RELAY_MCP_TRACE_MAX_BYTES", "1048576")
	t.Setenv("RELAY_SOURCE_CURSOR_HMAC_KEY", strings.Repeat("k", 32))
	t.Setenv("RELAY_MCP_AUTH_TOKEN", "upstream-secret")
	t.Setenv("RELAY_MCP_DISABLE_AUTH", "")
	t.Setenv("RELAY_MCP_INGRESS_UPSTREAM_BEARER_TOKEN", "upstream-secret")
	t.Setenv("PORT", "0")

	addresses, blocker := reserveIngressAddresses(t)
	defer func() {
		if blocker != nil {
			_ = blocker.Close()
		}
	}()
	for index, name := range ingressListenerEnvironment {
		t.Setenv(name, addresses[index])
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan runtimeReady, 1)
	runResult := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { runResult <- run(ctx, log, ready) }()

	var runtime runtimeReady
	select {
	case runtime = <-ready:
	case err := <-runResult:
		t.Fatalf("Relay stopped before readiness: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("Relay did not become ready")
	}
	if len(runtime.MCPIngress.Mappings) != 7 {
		t.Fatalf("mappings=%d", len(runtime.MCPIngress.Mappings))
	}

	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	mainResponse := postRPC(t, client, runtime.MainURL+"/mcp", "upstream-secret", mcp.Request{JSONRPC: mcp.JSONRPCVersion, ID: json.RawMessage(`1`), Method: "ping"})
	if mainResponse.Error != nil {
		t.Fatalf("aggregate ping=%#v", mainResponse.Error)
	}

	set, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	manifestByPath := map[string]routecontracts.RouteManifest{}
	for _, manifest := range set.Manifests {
		manifestByPath[manifest.RoutePath] = manifest
	}

	for index := 1; index < len(runtime.MCPIngress.Mappings); index++ {
		assertPrivateRoute(t, client, runtime.MCPIngress.Mappings[index].RoutePath, addresses[index], manifestByPath)
	}

	_ = blocker.Close()
	blocker = nil
	waitForEndpoint(t, client, "http://"+addresses[0]+"/healthz")
	assertPrivateRoute(t, client, runtime.MCPIngress.Mappings[0].RoutePath, addresses[0], manifestByPath)
	protectedRequest := mcp.Request{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      json.RawMessage(`9`),
		Method:  "tools/call",
		Params: mustJSON(t, map[string]any{
			"name":      "list_projects",
			"arguments": map[string]any{"status": "active", "limit": 10, "protected_body": "BODY_SENTINEL"},
		}),
	}
	_ = postRPC(t, client, "http://"+addresses[0]+runtime.MCPIngress.Mappings[0].RoutePath, "client-credential-must-be-stripped", protectedRequest)

	waitForTraceMappings(t, traceRoot, 7)
	inspectTraceFiles(t, traceRoot, []string{"upstream-secret", "client-credential-must-be-stripped", "BODY_SENTINEL"})

	cancel()
	select {
	case err := <-runResult:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Relay shutdown: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Relay did not stop")
	}
}

func reserveIngressAddresses(t *testing.T) ([]string, net.Listener) {
	t.Helper()
	listeners := make([]net.Listener, len(ingressListenerEnvironment))
	addresses := make([]string, len(listeners))
	for index := range listeners {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners[index] = listener
		addresses[index] = listener.Addr().String()
	}
	for index := 1; index < len(listeners); index++ {
		_ = listeners[index].Close()
	}
	return addresses, listeners[0]
}

func assertPrivateRoute(t *testing.T, client *http.Client, routePath, address string, manifests map[string]routecontracts.RouteManifest) {
	t.Helper()
	target := "http://" + address + routePath
	ping := postRPC(t, client, target, "client-credential-must-be-stripped", mcp.Request{JSONRPC: mcp.JSONRPCVersion, ID: json.RawMessage(`1`), Method: "ping"})
	if ping.Error != nil {
		t.Fatalf("%s ping=%#v", routePath, ping.Error)
	}
	listed := postRPC(t, client, target, "client-credential-must-be-stripped", mcp.Request{JSONRPC: mcp.JSONRPCVersion, ID: json.RawMessage(`2`), Method: "tools/list"})
	if listed.Error != nil {
		t.Fatalf("%s tools/list=%#v", routePath, listed.Error)
	}
	var tools mcp.ToolsListResult
	data, _ := json.Marshal(listed.Result)
	if err := json.Unmarshal(data, &tools); err != nil {
		t.Fatal(err)
	}
	manifest, ok := manifests[routePath]
	if !ok {
		t.Fatalf("manifest missing for %s", routePath)
	}
	want := make([]string, len(manifest.Tools))
	for index, tool := range manifest.Tools {
		want[index] = tool.Name
	}
	got := make([]string, len(tools.Tools))
	for index, tool := range tools.Tools {
		got[index] = tool.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s tools=%v want=%v", routePath, got, want)
	}
}

func postRPC(t *testing.T, client *http.Client, target, authorization string, request mcp.Request) mcp.Response {
	t.Helper()
	body := mustJSON(t, request)
	httpRequest, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+authorization)
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer response.Body.Close()
	var result mcp.Response
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode %s status=%d: %v", target, response.StatusCode, err)
	}
	return result
}

func waitForEndpoint(t *testing.T, client *http.Client, target string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(target)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("endpoint did not become available: %s", target)
}

func waitForTraceMappings(t *testing.T, root string, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(root)
		if err == nil && len(entries) == count {
			ready := true
			for _, entry := range entries {
				files, _ := filepath.Glob(filepath.Join(root, entry.Name(), "*.jsonl"))
				if len(files) == 0 {
					ready = false
					break
				}
			}
			if ready {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("trace mappings did not become ready under %s", root)
}

func inspectTraceFiles(t *testing.T, root string, prohibited []string) {
	t.Helper()
	seenMappings := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, value := range prohibited {
			if strings.Contains(text, value) {
				return fmt.Errorf("trace %s contains protected value %q", path, value)
			}
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			var record struct {
				SchemaVersion   string `json:"schema_version"`
				MappingID       string `json:"mapping_id"`
				RoutePath       string `json:"route_path"`
				ResponseSHA256  string `json:"response_sha256"`
				DownstreamWrite struct {
					Complete bool `json:"complete"`
				} `json:"downstream_write"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				return err
			}
			if record.SchemaVersion != "relay.transport.mcp-trace.v1" || record.MappingID == "" || record.RoutePath == "" || len(record.ResponseSHA256) != 64 {
				return fmt.Errorf("invalid trace record in %s: %#v", path, record)
			}
			seenMappings[record.MappingID] = true
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seenMappings) != 7 {
		t.Fatalf("traced mappings=%v", seenMappings)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
