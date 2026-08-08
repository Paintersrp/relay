package registry

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestRegistryMatchesPublicContractAndPinnedIdentity(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	if len(RawRegistryDocument()) != OperationRegistryBytes {
		t.Fatalf("registry byte length = %d, want %d", len(RawRegistryDocument()), OperationRegistryBytes)
	}
	operations, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 17 {
		t.Fatalf("operation count = %d, want 17", len(operations))
	}
	if operations[0].OperationID != "wayfinder.workspace" {
		t.Fatalf("first operation = %q", operations[0].OperationID)
	}
	if operations[len(operations)-1].OperationID != "auditor.audit" {
		t.Fatalf("last operation = %q", operations[len(operations)-1].OperationID)
	}

	for index := range operations {
		if operations[index].OperationID == "planner.requirements" {
			operations[index].RequiredInputs[0].InputName = "mutated"
		}
	}
	reloaded, ok := Lookup("planner.requirements")
	if !ok {
		t.Fatal("planner.requirements missing")
	}
	if reloaded.RequiredInputs[0].InputName != "confirmed_intent" {
		t.Fatalf("registry was mutated: %+v", reloaded.RequiredInputs)
	}

	audit, ok := Lookup("auditor.audit")
	if !ok {
		t.Fatal("auditor.audit missing")
	}
	if len(audit.RequiredInputs) != 0 || len(audit.DerivedInputs) != 6 {
		t.Fatalf("auditor.audit inputs = %d caller, %d derived", len(audit.RequiredInputs), len(audit.DerivedInputs))
	}
}

func TestOperationRegistryIdentityRejectsEveryPolicyClass(t *testing.T) {
	original := RawRegistryDocument()
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{"semantic_projection", `"create_operation_packet":"relay.semantic.create-operation-packet.v1"`, `"create_operation_packet":"relay.semantic.create-operation-packet.v2"`},
		{"transport_exclusion", `"download_url"`, `"download_url_changed"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !bytes.Contains(original, []byte(test.old)) {
				t.Fatalf("fixture does not contain %s", test.old)
			}
			mutated := bytes.Replace(original, []byte(test.old), []byte(test.new), 1)
			if _, err := validateRegistryBytes(mutated); err == nil || !strings.Contains(err.Error(), "operation registry") {
				t.Fatalf("mutation was accepted: %v", err)
			}
		})
	}
}

func TestOperationRegistryStrictDecodeRejectsUnknownProperties(t *testing.T) {
	var document map[string]any
	if err := json.Unmarshal(RawRegistryDocument(), &document); err != nil {
		t.Fatal(err)
	}
	document["unknown_policy"] = true
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var decoded registryDocument
	if err := decodeStrict(raw, &decoded); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown property error = %v", err)
	}
}

func TestSemanticRequestBasisCanonicalizesOperationCollections(t *testing.T) {
	first := plannerAuthoringRequest(false)
	second := plannerAuthoringRequest(true)

	firstBasis, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", first)
	if err != nil {
		t.Fatal(err)
	}
	secondBasis, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBasis, secondBasis) {
		t.Fatalf("equivalent requests differ:\n%s\n%s", firstBasis, secondBasis)
	}
	if !bytes.Contains(firstBasis, []byte(`"semantic_projection":"relay.semantic.create-operation-packet.v1"`)) {
		t.Fatalf("tool projection is absent: %s", firstBasis)
	}
	if !bytes.Contains(firstBasis, []byte(`"operation_projection":"relay.semantic.operation-packet-request.v1"`)) {
		t.Fatalf("operation projection is absent: %s", firstBasis)
	}
	if bytes.Contains(firstBasis, []byte(`"input_files"`)) {
		t.Fatalf("transport input_files entered semantic basis: %s", firstBasis)
	}
}

func TestSemanticRequestBasisExcludesArtifactTransportIdentity(t *testing.T) {
	first := packetRequestWithTransport("https://files.example/one", "file-one")
	second := packetRequestWithTransport("https://files.example/two", "file-two")

	firstBasis, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", first)
	if err != nil {
		t.Fatal(err)
	}
	secondBasis, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBasis, secondBasis) {
		t.Fatalf("transport-only artifact fields changed basis:\n%s\n%s", firstBasis, secondBasis)
	}
	if bytes.Contains(firstBasis, []byte("files.example")) || bytes.Contains(firstBasis, []byte("file-one")) {
		t.Fatalf("transport identity leaked into basis: %s", firstBasis)
	}
}

func TestValidateRequestRejectsExactSchemaViolations(t *testing.T) {
	validPacket := plannerAuthoringRequest(false)
	tests := []struct {
		name    string
		surface SurfaceContractID
		tool    string
		raw     []byte
		code    string
	}{
		{"missing_required", "planner-authoring.v1", "create_operation_packet", mutateObject(t, validPacket, func(value map[string]any) { delete(value, "project_id") }), "request_required_missing"},
		{"surface_const", "planner-authoring.v1", "create_operation_packet", mutateObject(t, validPacket, func(value map[string]any) { value["surface_contract"] = "auditor-review.v1" }), "request_const_invalid"},
		{"explicit_null", "planner-authoring.v1", "list_projects", []byte(`{"surface_contract":"planner-authoring.v1","limit":null}`), "request_type_invalid"},
		{"numeric_minimum", "planner-authoring.v1", "list_projects", []byte(`{"surface_contract":"planner-authoring.v1","limit":0}`), "request_number_too_small"},
		{"numeric_maximum", "planner-authoring.v1", "list_projects", []byte(`{"surface_contract":"planner-authoring.v1","limit":101}`), "request_number_too_large"},
		{"operation_enum", "planner-authoring.v1", "create_operation_packet", mutateObject(t, validPacket, func(value map[string]any) { value["operation_id"] = "auditor.audit" }), "request_enum_invalid"},
		{"array_maximum", "planner-authoring.v1", "create_operation_packet", mutateObject(t, validPacket, func(value map[string]any) {
			files := make([]any, 65)
			for index := range files {
				files[index] = map[string]any{"download_url": "https://files.example/item", "file_id": "file", "mime_type": "application/json", "file_name": "item.json"}
			}
			value["input_files"] = files
		}), "request_array_too_long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ValidateRequest(test.surface, test.tool, test.raw); err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("validation error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestSemanticRequestBasisRejectsDuplicateAndOperationDisallowedValues(t *testing.T) {
	var request map[string]any
	if err := json.Unmarshal(plannerAuthoringRequest(false), &request); err != nil {
		t.Fatal(err)
	}
	inputs := request["inputs"].([]any)
	request["inputs"] = append(inputs, inputs[0])
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", raw); err == nil || (err.Error() != "request_semantic_invalid:$" && err.Error() != "request_union_invalid:$") {
		t.Fatalf("duplicate input error = %v", err)
	}

	disallowed := mutateObject(t, plannerAuthoringRequest(false), func(value map[string]any) {
		value["workflow_references"] = []any{map[string]any{"kind": "run", "run_id": "run-1"}}
	})
	if _, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", disallowed); err == nil || (err.Error() != "request_semantic_invalid:$" && err.Error() != "request_union_invalid:$") {
		t.Fatalf("operation-disallowed reference error = %v", err)
	}
}

func TestRequestErrorsDoNotEchoUnboundedCallerValues(t *testing.T) {
	marker := strings.Repeat("caller-secret-marker", 200)
	raw := mutateObject(t, plannerAuthoringRequest(false), func(value map[string]any) {
		value[marker] = marker
	})
	_, err := ValidateRequest("planner-authoring.v1", "create_operation_packet", raw)
	if err == nil {
		t.Fatal("unknown property was accepted")
	}
	if strings.Contains(err.Error(), marker) || len(err.Error()) > 128 {
		t.Fatalf("error is not bounded: %q", err.Error())
	}
}

func TestConcurrentRegistrySchemaAndSemanticReadsAreDeterministic(t *testing.T) {
	expectedBasis, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", plannerAuthoringRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 32
	const iterations = 20
	errorsOut := make(chan error, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				operations, err := All()
				if err != nil {
					errorsOut <- err
					return
				}
				for index := range operations {
					if operations[index].OperationID == "planner.requirements" {
						operations[index].RequiredInputs[0].InputName = "caller-mutation"
					}
				}
				operation, ok := Lookup("planner.requirements")
				if !ok || operation.RequiredInputs[0].InputName != "confirmed_intent" {
					errorsOut <- &RequestError{Code: "registry_copy_mutated"}
					return
				}
				basis, err := SemanticRequestBasis("planner-authoring.v1", "create_operation_packet", plannerAuthoringRequest(iteration%2 == 1))
				if err != nil {
					errorsOut <- err
					return
				}
				if !bytes.Equal(basis, expectedBasis) {
					errorsOut <- &RequestError{Code: "semantic_basis_nondeterministic"}
					return
				}
			}
		}()
	}
	group.Wait()
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
}

func TestValidateOperationRequestRejectsOperationSemanticViolations(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{
			name: "operation_disallowed_reference",
			raw: mutateObject(t, plannerAuthoringRequest(false), func(value map[string]any) {
				value["workflow_references"] = []any{map[string]any{"kind": "run", "run_id": "run-1"}}
			}),
		},
		{
			name: "source_kind_branch_mismatch",
			raw: mutateObject(t, plannerAuthoringRequest(false), func(value map[string]any) {
				value["inputs"].([]any)[1].(map[string]any)["source"] = map[string]any{"text": "requirements"}
			}),
		},
		{
			name: "workflow_record_digest_mismatch",
			raw: mutateObject(t, plannerAuthoringRequest(false), func(value map[string]any) {
				input := value["inputs"].([]any)[0].(map[string]any)
				input["source_kind"] = "workflow_record"
				input["source"] = map[string]any{
					"workflow_record": map[string]any{
						"kind":            "plan_artifact",
						"plan_id":         "plan-1",
						"artifact_id":     "artifact-1",
						"expected_sha256": strings.Repeat("f", 64),
					},
				}
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateOperationRequest("planner-authoring.v1", "create_operation_packet", test.raw)
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			allowedSchemaRejection := test.name == "operation_disallowed_reference" || test.name == "source_kind_branch_mismatch" || test.name == "workflow_record_digest_mismatch"
			if err == nil || (errText != "request_semantic_invalid:$" && !(allowedSchemaRejection && errText == "request_union_invalid:$")) {
				t.Fatalf("operation validation error = %v", err)
			}
		})
	}
}

func mutateObject(t *testing.T, raw []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	mutate(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func plannerAuthoringRequest(reverse bool) []byte {
	shaA := strings.Repeat("a", 64)
	shaB := strings.Repeat("b", 64)
	inputs := []any{
		input("confirmed_intent", "inline_text", "intent.txt", "text/plain", shaA, map[string]any{"text": "implement current behavior"}),
		input("wayfinder_transfer", "uploaded_file", "handoff.md", "text/markdown", shaB, map[string]any{"file_index": 0}),
	}
	attestations := []any{
		map[string]any{"kind": "confirmed_intent", "input_name": "confirmed_intent", "subject_sha256": shaA},
		clearance("confirmed_intent", shaA),
		map[string]any{"kind": "exact_evidence", "input_name": "wayfinder_transfer", "subject_sha256": shaB},
		clearance("wayfinder_transfer", shaB),
	}
	if reverse {
		reverseAny(inputs)
		reverseAny(attestations)
	}
	request := map[string]any{
		"surface_contract":    "planner-authoring.v1",
		"mutation_id":         "mutation-1",
		"operation_id":        "planner.requirements",
		"project_id":          "project-1",
		"input_files":         []any{map[string]any{"download_url": "https://files.example/one", "file_id": "file-one", "mime_type": "text/markdown", "file_name": "handoff.md"}},
		"inputs":              inputs,
		"workflow_references": []any{},
		"attestations":        attestations,
		"primary_revisions":   []any{},
		"comparison_anchors":  []any{},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return raw
}

func packetRequestWithTransport(downloadURL, fileID string) []byte {
	return mutateObjectForRequest(plannerAuthoringRequest(false), func(value map[string]any) {
		file := value["input_files"].([]any)[0].(map[string]any)
		file["download_url"] = downloadURL
		file["file_id"] = fileID
		file["file_name"] = "temporary-handoff-name.md"
	})
}

func mutateObjectForRequest(raw []byte, mutate func(map[string]any)) []byte {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		panic(err)
	}
	mutate(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func input(name, sourceKind, displayName, mediaType, sha string, source map[string]any) map[string]any {
	return map[string]any{
		"input_name":      name,
		"source_kind":     sourceKind,
		"display_name":    displayName,
		"media_type":      mediaType,
		"expected_sha256": sha,
		"source":          source,
	}
}

func clearance(name, sha string) map[string]any {
	return map[string]any{
		"kind":       "sensitive_data_clearance",
		"input_name": name,
		"clearance": map[string]any{
			"policy_version": "relay.canonical-artifact-sensitive-data.v1",
			"subject_sha256": sha,
			"declaration":    clearanceDeclaration(),
			"confirmed":      true,
		},
	}
}

func clearanceDeclaration() map[string]any {
	return map[string]any{
		"password":                                 false,
		"api_key_or_access_token":                  false,
		"refresh_token_or_session_material":        false,
		"cookie_or_authorization_header":           false,
		"private_or_ssh_key":                       false,
		"credential":                               false,
		"complete_secret_bearing_environment_file": false,
		"avoidable_signed_secret_bearing_url":      false,
	}
}

func reverseAny(values []any) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
