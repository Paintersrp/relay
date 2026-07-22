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
	"RELAY_MCP_INGRESS_WAYFINDER_ADDR",
	"RELAY_MCP_INGRESS_PLANNER_ADDR",
	"RELAY_MCP_INGRESS_AUDITOR_ADDR",
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
	if len(runtime.MCPIngress.Mappings) != 3 {
		t.Fatalf("mappings=%d", len(runtime.MCPIngress.Mappings))
	}

	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	mainResponse := postRPC(t, client, runtime.MainURL+"/mcp", "upstream-secret", mcp.Request{JSONRPC: mcp.JSONRPCVersion, ID: json.RawMessage(`1`), Method: "ping"})
	if mainResponse.Error != nil {
		t.Fatalf("aggregate ping=%#v", mainResponse.Error)
	}

	surfaces, err := routecontracts.BuildMCPAppSurfaceManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaceByPath := make(map[string]routecontracts.AppSurfaceManifest, len(surfaces.Surfaces))
	for _, surface := range surfaces.Surfaces {
		surfaceByPath[surface.PublicPath] = surface
	}

	for index := 1; index < len(runtime.MCPIngress.Mappings); index++ {
		assertPrivateRoute(t, client, runtime.MCPIngress.Mappings[index].RoutePath, addresses[index], surfaceByPath)
	}

	_ = blocker.Close()
	blocker = nil
	waitForEndpoint(t, client, "http://"+addresses[0]+"/healthz")
	assertPrivateRoute(t, client, runtime.MCPIngress.Mappings[0].RoutePath, addresses[0], surfaceByPath)
	wayfinder, ok := surfaceByPath[runtime.MCPIngress.Mappings[0].RoutePath]
	if !ok || len(wayfinder.Tools) == 0 {
		t.Fatalf("wayfinder app surface missing for %s", runtime.MCPIngress.Mappings[0].RoutePath)
	}
	protectedRequest := mcp.Request{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      json.RawMessage(`9`),
		Method:  "tools/call",
		Params: mustJSON(t, map[string]any{
			"name":      wayfinder.Tools[0].AdvertisedName,
			"arguments": map[string]any{"status": "active", "limit": 10, "protected_body": "BODY_SENTINEL"},
		}),
	}
	_ = postRPC(t, client, "http://"+addresses[0]+runtime.MCPIngress.Mappings[0].RoutePath, "client-credential-must-be-stripped", protectedRequest)

	waitForTraceMappings(t, traceRoot, 3)
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

func assertPrivateRoute(t *testing.T, client *http.Client, routePath, address string, surfaces map[string]routecontracts.AppSurfaceManifest) {
	t.Helper()
	target := "http://" + address + routePath
	ping := postRPC(t, client, target, "client-credential-must-be-stripped", mcp.Request{JSONRPC: mcp.JSONRPCVersion, ID: json.RawMessage(`1`), Method: "ping"})
	if ping.Error != nil {
		t.Fatalf("%s ping=%#v", routePath, ping.Error)
	}
	surface, ok := surfaces[routePath]
	if !ok {
		t.Fatalf("app surface missing for %s", routePath)
	}
	got := make([]string, 0, len(surface.Tools))
	cursor := ""
	for {
		var params json.RawMessage
		if cursor != "" {
			params = mustJSON(t, map[string]string{"cursor": cursor})
		}
		listed := postRPC(t, client, target, "client-credential-must-be-stripped", mcp.Request{JSONRPC: mcp.JSONRPCVersion, ID: json.RawMessage(`2`), Method: "tools/list", Params: params})
		if listed.Error != nil {
			t.Fatalf("%s tools/list=%#v", routePath, listed.Error)
		}
		var tools mcp.ToolsListResult
		data, _ := json.Marshal(listed.Result)
		if err := json.Unmarshal(data, &tools); err != nil {
			t.Fatal(err)
		}
		for _, tool := range tools.Tools {
			got = append(got, tool.Name)
		}
		if tools.NextCursor == "" {
			break
		}
		cursor = tools.NextCursor
	}
	want := make([]string, len(surface.Tools))
	for index, tool := range surface.Tools {
		want[index] = tool.AdvertisedName
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
	if len(seenMappings) != len(ingressListenerEnvironment) {
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
