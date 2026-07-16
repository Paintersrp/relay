package mcp

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"relay/internal/mcp/surfacecontracts"
)

func TestCorrectedRefreshDescriptorsRejectOversizedFilesBeforeHandler(t *testing.T) {
	surfaces, err := surfacecontracts.All()
	if err != nil {
		t.Fatal(err)
	}

	for _, manifest := range surfaces {
		t.Run(string(manifest.SurfaceContract), func(t *testing.T) {
			var refresh surfacecontracts.ToolContract
			for _, tool := range manifest.Tools {
				if tool.Name == "refresh_operation_packet" {
					refresh = tool
					break
				}
			}
			if refresh.Name == "" {
				t.Fatal("refresh_operation_packet is missing")
			}

			definition := toolDefinitionFromContract(refresh)
			encodedDefinition, err := json.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			descriptor := string(encodedDefinition)
			if !strings.Contains(descriptor, `"input_files":{"type":"array","minItems":0,"maxItems":64,"items":{"$ref":"#/$defs/OpenAIFileParameter"}}`) {
				t.Fatalf("refresh descriptor lacks corrected input_files schema: %s", descriptor)
			}
			if !strings.Contains(descriptor, `"openai/fileParams":["input_files"]`) {
				t.Fatalf("refresh descriptor lacks input_files metadata: %s", descriptor)
			}
			if definition.Meta["relay/surfaceManifestSHA256"] != manifest.ManifestSHA256 {
				t.Fatalf("descriptor manifest sha256 = %v, want %s", definition.Meta["relay/surfaceManifestSHA256"], manifest.ManifestSHA256)
			}

			handlers := handlersForManifest(manifest)
			var called atomic.Int32
			for index := range handlers {
				if handlers[index].Name == "refresh_operation_packet" {
					handlers[index].Handle = func(json.RawMessage) ToolCallResult {
						called.Add(1)
						return toolOK("refresh_operation_packet")
					}
				}
			}
			server, err := NewServerForSurface(nil, &MCPDeps{}, manifest.SurfaceContract, handlers)
			if err != nil {
				t.Fatal(err)
			}

			files := make([]any, 65)
			for index := range files {
				files[index] = map[string]any{
					"download_url": "https://files.example/item",
					"file_id":      "file",
				}
			}
			arguments, err := json.Marshal(map[string]any{
				"surface_contract":    string(manifest.SurfaceContract),
				"mutation_id":         "mutation-1",
				"expected_packet_id":  "packet-1",
				"input_files":         files,
				"inputs":              []any{},
				"workflow_references": []any{},
				"attestations":        []any{},
			})
			if err != nil {
				t.Fatal(err)
			}
			params, err := json.Marshal(ToolCallParams{
				Name:      "refresh_operation_packet",
				Arguments: arguments,
			})
			if err != nil {
				t.Fatal(err)
			}
			response := server.handleToolsCall(Request{ID: json.RawMessage(`1`), Params: params})
			if response.Error == nil || response.Error.Code != CodeInvalidParams || !strings.Contains(response.Error.Message, "request_array_too_long") {
				t.Fatalf("oversized refresh schema response = %+v", response)
			}
			if called.Load() != 0 {
				t.Fatalf("refresh handler was invoked %d times", called.Load())
			}
		})
	}
}
