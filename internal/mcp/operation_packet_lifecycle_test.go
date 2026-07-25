package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"relay/internal/app/idempotency"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
)

func TestLifecycleMutationOmitsStoredResultIdentity(t *testing.T) {
	stored := idempotency.StoredResult{ResultKind: semanticidentity.ResultKindCreateOperationPacket, ResultIdentityJSON: []byte(`{"packet":{"summary":{"packet_id":"opkt"}}}`), ResultSHA256: "sha", CommittedAt: "2026-07-18T00:00:00.000000000Z"}
	mutation := lifecycleMutation(stored, true)
	if !mutation.Replay || mutation.ResultKind != stored.ResultKind || mutation.ResultSHA256 != stored.ResultSHA256 || mutation.CommittedAt != stored.CommittedAt {
		t.Fatalf("mutation = %#v", mutation)
	}
	encoded, err := json.Marshal(mutation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "result_identity_json") {
		t.Fatalf("mutation contains stored result identity: %s", encoded)
	}
}

func TestPacketLifecycleOutputSchemasMatchRuntimeResults(t *testing.T) {
	samples := map[string]any{
		"create_operation_packet":  CreateOperationPacketResult{},
		"refresh_operation_packet": RefreshOperationPacketResult{},
		"close_operation_packet":   CloseOperationPacketResult{},
	}
	for toolName, sample := range samples {
		contract, ok := registry.LookupPublishedToolContract(toolName)
		if !ok {
			t.Fatalf("published contract %q is missing", toolName)
		}
		var schema struct {
			OneOf []struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"oneOf"`
		}
		if err := json.Unmarshal(contract.OutputSchema, &schema); err != nil {
			t.Fatalf("decode %s output schema: %v", toolName, err)
		}
		if len(schema.OneOf) != 1 {
			t.Fatalf("%s output schema has %d variants, want one success variant", toolName, len(schema.OneOf))
		}
		var runtime map[string]json.RawMessage
		encoded, err := json.Marshal(sample)
		if err != nil {
			t.Fatalf("encode %s runtime result: %v", toolName, err)
		}
		if err := json.Unmarshal(encoded, &runtime); err != nil {
			t.Fatalf("decode %s runtime result: %v", toolName, err)
		}
		if len(runtime) != len(schema.OneOf[0].Properties) {
			t.Fatalf("%s runtime keys %v do not match schema keys %v", toolName, mapKeys(runtime), mapKeys(schema.OneOf[0].Properties))
		}
		for key := range runtime {
			if _, ok := schema.OneOf[0].Properties[key]; !ok {
				t.Fatalf("%s runtime key %q is absent from output schema", toolName, key)
			}
		}
	}
}

func mapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestLifecycleHandlerRequiresService(t *testing.T) {
	if _, err := NewOperationPacketLifecycleHandler(nil); err == nil {
		t.Fatal("expected nil service rejection")
	}
}
