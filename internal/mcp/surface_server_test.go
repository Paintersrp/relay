package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"relay/internal/mcp/surfacecontracts"
	"relay/internal/operations/registry"
)

func TestNewServerForSurfaceRequiresExactHandlerAgreement(t *testing.T) {
	manifest, ok := surfacecontracts.Get("planner-authoring.v1")
	if !ok {
		t.Fatal("planner-authoring.v1 missing")
	}
	handlers := handlersForManifest(manifest)

	server, err := NewServerForSurface(nil, &MCPDeps{}, "planner-authoring.v1", handlers)
	if err != nil {
		t.Fatal(err)
	}
	if len(server.tools) != len(manifest.Tools) {
		t.Fatalf("tool count = %d, want %d", len(server.tools), len(manifest.Tools))
	}
	for index, definition := range server.tools {
		if definition.Name != manifest.Tools[index].Name {
			t.Fatalf("tool %d = %q, want %q", index, definition.Name, manifest.Tools[index].Name)
		}
		if definition.Title != manifest.Tools[index].Title {
			t.Fatalf("tool %s title = %q, want %q", definition.Name, definition.Title, manifest.Tools[index].Title)
		}
		if definition.Meta["relay/surfaceManifestSHA256"] != manifest.ManifestSHA256 {
			t.Fatalf("tool %s manifest digest = %v", definition.Name, definition.Meta["relay/surfaceManifestSHA256"])
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		encodedText := string(encoded)
		assertOrderedFragments(t, encodedText, []string{
			`"name"`, `"title"`, `"description"`, `"inputSchema"`, `"outputSchema"`, `"annotations"`, `"_meta"`,
		})
		if !strings.Contains(encodedText, `"annotations":{"readOnlyHint":`) ||
			!strings.Contains(encodedText, `,"destructiveHint":`) ||
			!strings.Contains(encodedText, `,"idempotentHint":`) ||
			!strings.Contains(encodedText, `,"openWorldHint":`) {
			t.Fatalf("annotation order is not canonical: %s", encodedText)
		}
		assertOrderedFragments(t, encodedText[strings.Index(encodedText, `"_meta":`):], []string{
			`"openai/widgetAccessible"`, `"openai/toolInvocation/invoking"`, `"openai/toolInvocation/invoked"`,
			`"relay/surfaceContract"`, `"relay/surfaceManifestSHA256"`, `"relay/semanticToolID"`,
		})
	}

	params, err := json.Marshal(ToolCallParams{
		Name:      manifest.Tools[0].Name,
		Arguments: json.RawMessage(`{"surface_contract":"planner-authoring.v1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := server.handleToolsCall(Request{ID: json.RawMessage(`1`), Params: params})
	if response.Error != nil {
		t.Fatalf("surface dispatch failed: %+v", response.Error)
	}
}

func TestNewServerForSurfaceRejectsMissingHandler(t *testing.T) {
	manifest, ok := surfacecontracts.Get("planner-authoring.v1")
	if !ok {
		t.Fatal("planner-authoring.v1 missing")
	}
	handlers := handlersForManifest(manifest)
	_, err := NewServerForSurface(nil, &MCPDeps{}, manifest.SurfaceContract, handlers[:len(handlers)-1])
	if err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("missing handler error = %v", err)
	}
}

func TestNewServerForSurfaceRejectsReorderedHandler(t *testing.T) {
	manifest, ok := surfacecontracts.Get("planner-authoring.v1")
	if !ok {
		t.Fatal("planner-authoring.v1 missing")
	}
	handlers := handlersForManifest(manifest)
	handlers[0], handlers[1] = handlers[1], handlers[0]
	_, err := NewServerForSurface(nil, &MCPDeps{}, manifest.SurfaceContract, handlers)
	if err == nil || !strings.Contains(err.Error(), "handler 0") {
		t.Fatalf("reordered handler error = %v", err)
	}
}

func TestNewServerForSurfaceRejectsMutabilityMismatch(t *testing.T) {
	manifest, ok := surfacecontracts.Get("planner-authoring.v1")
	if !ok {
		t.Fatal("planner-authoring.v1 missing")
	}
	handlers := handlersForManifest(manifest)
	handlers[0].ReadOnly = !handlers[0].ReadOnly
	_, err := NewServerForSurface(nil, &MCPDeps{}, manifest.SurfaceContract, handlers)
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("mutability error = %v", err)
	}
}

func TestCompiledSurfaceCatalogDoesNotRequireFutureHandlers(t *testing.T) {
	if err := ValidateCompiledSurfaceCatalog(); err != nil {
		t.Fatal(err)
	}
}

func assertOrderedFragments(t *testing.T, value string, fragments []string) {
	t.Helper()
	previous := -1
	for _, fragment := range fragments {
		index := strings.Index(value, fragment)
		if index < 0 {
			t.Fatalf("fragment %s is missing from %s", fragment, value)
		}
		if index <= previous {
			t.Fatalf("fragment %s is out of order in %s", fragment, value)
		}
		previous = index
	}
}

func handlersForManifest(manifest surfacecontracts.SurfaceManifest) []SurfaceToolHandler {
	handlers := make([]SurfaceToolHandler, len(manifest.Tools))
	for index, tool := range manifest.Tools {
		name := tool.Name
		handlers[index] = SurfaceToolHandler{
			Name:     name,
			ReadOnly: tool.Annotations.ReadOnlyHint,
			Handle: func(json.RawMessage) ToolCallResult {
				return toolOK(name)
			},
		}
	}
	return handlers
}

func TestAllRegisteredSurfacesAssembleWithSyntheticHandlers(t *testing.T) {
	surfaces, err := surfacecontracts.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range surfaces {
		t.Run(string(manifest.SurfaceContract), func(t *testing.T) {
			server, err := NewServerForSurface(nil, &MCPDeps{}, registry.SurfaceContractID(manifest.SurfaceContract), handlersForManifest(manifest))
			if err != nil {
				t.Fatal(err)
			}
			if len(server.tools) != len(manifest.Tools) {
				t.Fatalf("tool count = %d, want %d", len(server.tools), len(manifest.Tools))
			}
		})
	}
}

func TestToolDefinitionMarshalPreservesAggregateOmissions(t *testing.T) {
	definition := ToolDefinition{
		Name:        "aggregate_tool",
		Description: "aggregate",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Annotations: map[string]any{"readOnlyHint": true},
		Meta:        map[string]any{"relay/example": "value"},
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"title"`) {
		t.Fatalf("empty aggregate title was emitted: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"annotations"`) || !strings.Contains(string(encoded), `"_meta"`) {
		t.Fatalf("aggregate maps were omitted: %s", encoded)
	}
}

func TestSurfaceDescriptorEmitsFileParamsInCanonicalMetadataOrder(t *testing.T) {
	manifest, ok := surfacecontracts.Get("planner-authoring.v1")
	if !ok {
		t.Fatal("planner-authoring.v1 missing")
	}
	var packetTool surfacecontracts.ToolContract
	for _, tool := range manifest.Tools {
		if tool.Name == "create_operation_packet" {
			packetTool = tool
			break
		}
	}
	if packetTool.Name == "" {
		t.Fatal("create_operation_packet missing")
	}
	encoded, err := json.Marshal(toolDefinitionFromContract(packetTool))
	if err != nil {
		t.Fatal(err)
	}
	metaIndex := strings.Index(string(encoded), `"_meta":`)
	if metaIndex < 0 {
		t.Fatalf("_meta missing: %s", encoded)
	}
	assertOrderedFragments(t, string(encoded)[metaIndex:], []string{
		`"openai/widgetAccessible"`,
		`"openai/toolInvocation/invoking"`,
		`"openai/toolInvocation/invoked"`,
		`"openai/fileParams"`,
		`"relay/surfaceContract"`,
		`"relay/surfaceManifestSHA256"`,
		`"relay/semanticToolID"`,
	})
}

func TestSurfaceDispatcherValidatesExactSchemaBeforeHandler(t *testing.T) {
	manifest, ok := surfacecontracts.Get("planner-plan.v1")
	if !ok {
		t.Fatal("planner-plan.v1 missing")
	}
	var called atomic.Int32
	handlers := handlersForManifest(manifest)
	for index := range handlers {
		if handlers[index].Name == "validate_artifact" {
			handlers[index].Handle = func(json.RawMessage) ToolCallResult {
				called.Add(1)
				return toolOK("unexpected")
			}
		}
	}
	server, err := NewServerForSurface(nil, &MCPDeps{}, manifest.SurfaceContract, handlers)
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{"surface_contract":"planner-plan.v1","expected_packet_id":"packet-1","artifact_name":"plan.json","media_type":"application/json","expected_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sensitive_data_clearance":{"policy_version":"relay.canonical-artifact-sensitive-data.v1","subject_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","declaration":{"password":false,"api_key_or_access_token":false,"refresh_token_or_session_material":false,"cookie_or_authorization_header":false,"private_or_ssh_key":false,"credential":false,"complete_secret_bearing_environment_file":false,"avoidable_signed_secret_bearing_url":false},"confirmed":true}}`)
	params, err := json.Marshal(ToolCallParams{Name: "validate_artifact", Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	response := server.handleToolsCall(Request{ID: json.RawMessage(`1`), Params: params})
	if response.Error == nil || response.Error.Code != CodeInvalidParams || !strings.Contains(response.Error.Message, "request_required_missing") {
		t.Fatalf("schema validation response = %+v", response)
	}
	if called.Load() != 0 {
		t.Fatalf("handler was invoked %d times", called.Load())
	}
}

func TestConcurrentDescriptorConversionAndSurfaceAssemblyAreDeterministic(t *testing.T) {
	manifest, ok := surfacecontracts.Get("planner-plan.v1")
	if !ok {
		t.Fatal("planner-plan.v1 missing")
	}
	expected := make([][]byte, len(manifest.Tools))
	for index, tool := range manifest.Tools {
		encoded, err := json.Marshal(toolDefinitionFromContract(tool))
		if err != nil {
			t.Fatal(err)
		}
		expected[index] = encoded
	}
	const workers = 24
	const iterations = 20
	errorsOut := make(chan string, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				fresh, ok := surfacecontracts.Get("planner-plan.v1")
				if !ok {
					errorsOut <- "manifest missing"
					return
				}
				for index, tool := range fresh.Tools {
					definition := toolDefinitionFromContract(tool)
					encoded, err := json.Marshal(definition)
					if err != nil {
						errorsOut <- err.Error()
						return
					}
					if !bytes.Equal(encoded, expected[index]) {
						errorsOut <- "descriptor is nondeterministic"
						return
					}
					definition.InputSchema[0] ^= 0xff
					definition.Annotations["readOnlyHint"] = !tool.Annotations.ReadOnlyHint
					definition.Meta["relay/surfaceContract"] = "caller-mutation"
					freshDefinition := toolDefinitionFromContract(tool)
					freshEncoded, err := json.Marshal(freshDefinition)
					if err != nil || !bytes.Equal(freshEncoded, expected[index]) {
						errorsOut <- "descriptor defensive copy isolation failed"
						return
					}
				}
				server, err := NewServerForSurface(nil, &MCPDeps{}, manifest.SurfaceContract, handlersForManifest(fresh))
				if err != nil || len(server.tools) != len(manifest.Tools) {
					errorsOut <- "surface assembly is nondeterministic"
					return
				}
			}
		}()
	}
	group.Wait()
	close(errorsOut)
	for message := range errorsOut {
		t.Fatal(message)
	}
}
