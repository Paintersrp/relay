package registry

import (
	"encoding/json"
	"testing"
)

func TestWayfinderPacketContractSupportsColdStart(t *testing.T) {
	for _, operation := range []OperationID{"wayfinder.workspace", "wayfinder.discovery", "wayfinder.investigation"} {
		t.Run(string(operation), func(t *testing.T) {
			definition, ok := LookupPublishedOperation(operation)
			if !ok {
				t.Fatal("published Wayfinder operation is missing")
			}
			raw, err := json.Marshal(map[string]any{
				"surface_contract":    definition.SurfaceContract,
				"mutation_id":         "cold-start-1",
				"operation_id":        operation,
				"project_id":          "project-1",
				"inputs":              []any{},
				"workflow_references": []any{},
				"attestations":        []any{},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateOperationRequest(definition.SurfaceContract, "create_operation_packet", raw); err != nil {
				t.Fatalf("minimal packet request rejected: %v", err)
			}
			if _, err := SemanticRequestSHA256(definition.SurfaceContract, "create_operation_packet", raw); err != nil {
				t.Fatalf("minimal packet semantic identity rejected: %v", err)
			}
		})
	}
}

func TestSharedPacketReadContractsUseCoherentIdentities(t *testing.T) {
	active := []byte(`{"surface_contract":"wayfinder-discovery.v1","project_id":"project-1","operation_id":"wayfinder.discovery"}`)
	if _, err := ValidateRequest("wayfinder-discovery.v1", "get_active_operation_packet", active); err != nil {
		t.Fatalf("active lookup request rejected: %v", err)
	}
	if _, err := ValidateRequest("wayfinder-discovery.v1", "get_active_operation_packet", []byte(`{"surface_contract":"wayfinder-discovery.v1","expected_packet_id":"packet-1"}`)); err == nil {
		t.Fatal("active lookup accepted the stale expected_packet_id-only contract")
	}

	list := []byte(`{"surface_contract":"wayfinder-discovery.v1","packet_id":"packet-1"}`)
	if _, err := ValidateRequest("wayfinder-discovery.v1", "list_operation_repositories", list); err != nil {
		t.Fatalf("repository listing request rejected: %v", err)
	}
	if _, err := ValidateRequest("wayfinder-discovery.v1", "list_operation_repositories", []byte(`{"surface_contract":"wayfinder-discovery.v1","expected_packet_id":"packet-1"}`)); err == nil {
		t.Fatal("repository listing accepted expected_packet_id")
	}
	if _, err := ValidateRequest("wayfinder-discovery.v1", "list_operation_repositories", []byte(`{"surface_contract":"wayfinder-discovery.v1","packet_id":"packet-1","unknown":true}`)); err == nil {
		t.Fatal("repository listing accepted an unknown field")
	}
}
