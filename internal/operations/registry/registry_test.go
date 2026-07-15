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
	if len(operations) != 15 {
		t.Fatalf("operation count = %d, want 15", len(operations))
	}
	if operations[0].OperationID != "planner.requirements" {
		t.Fatalf("first operation = %q", operations[0].OperationID)
	}
	if operations[len(operations)-1].OperationID != "auditor.remediation_execution_spec_review" {
		t.Fatalf("last operation = %q", operations[len(operations)-1].OperationID)
	}

	operations[0].RequiredInputs[0].InputName = "mutated"
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
		{"manifest_domain", `"manifest_domain":"requirements"`, `"manifest_domain":"design"`},
		{"output_kind", `"output_kind":"requirements_markdown"`, `"output_kind":"design_markdown"`},
		{"output_persistence", `"output_persistence":"chat_unrecorded"`, `"output_persistence":"relay_artifact"`},
		{"required_input", `"input_name":"confirmed_intent"`, `"input_name":"approved_design"`},
		{"conditional_input", `"input_name":"reviewed_candidate"`, `"input_name":"approved_design"`},
		{"derived_input", `"input_name":"current_audit_packet"`, `"input_name":"approved_plan"`},
		{"input_role", `"input_role":"authority"`, `"input_role":"candidate"`},
		{"attestation_kind", `"attestation_kind":"confirmed_intent"`, `"attestation_kind":"approved_artifact"`},
		{"source_kind", `"allowed_source_kinds":["inline_text"]`, `"allowed_source_kinds":["uploaded_file"]`},
		{"workflow_record_policy", `"workflow_record_policy":"none"`, `"workflow_record_policy":"artifact"`},
		{"workflow_reference", `"workflow_reference_kinds":["plan","pass"]`, `"workflow_reference_kinds":["plan"]`},
		{"comparison_anchor", `"comparison_anchor_purposes":["operator_supplied_comparison","reviewed_source_basis"]`, `"comparison_anchor_purposes":["operator_supplied_comparison"]`},
		{"source_policy", `"source_policy":"current_clean_project_optional_source"`, `"source_policy":"current_project_source_required"`},
		{"historical_authority", `"historical_authority":"none"`, `"historical_authority":"packet_bound"`},
		{"allowed_action", `"allowed_non_source_actions":["validate_artifact","submit_plan"]`, `"allowed_non_source_actions":["validate_artifact"]`},
		{"packet_projection", `"packet_semantic_projection":"relay.semantic.operation-packet-request.v1"`, `"packet_semantic_projection":"relay.semantic.operation-packet-request.v2"`},
		{"tool_projection", `"create_operation_packet":"relay.semantic.create-operation-packet.v1"`, `"create_operation_packet":"relay.semantic.create-operation-packet.v2"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !bytes.Contains(original, []byte(test.old)) {
				t.Fatalf("fixture does not contain %s", test.old)
			}
			mutated := bytes.Replace(original, []byte(test.old), []byte(test.new), 1)
			if _, err := validateRegistryBytes(RawPublicContract(), mutated); err == nil || !strings.Contains(err.Error(), "operation registry") {
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
	first := plannerPlanRequest(false)
	second := plannerPlanRequest(true)

	firstBasis, err := SemanticRequestBasis("planner-plan.v1", "create_operation_packet", first)
	if err != nil {
		t.Fatal(err)
	}
	secondBasis, err := SemanticRequestBasis("planner-plan.v1", "create_operation_packet", second)
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
	first := artifactRequest("https://files.example/one", "file-one")
	second := artifactRequest("https://files.example/two", "file-two")

	firstBasis, err := SemanticRequestBasis("planner-plan.v1", "validate_artifact", first)
	if err != nil {
		t.Fatal(err)
	}
	secondBasis, err := SemanticRequestBasis("planner-plan.v1", "validate_artifact", second)
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
	validArtifact := artifactRequest("https://files.example/one", "file-one")
	validPacket := plannerPlanRequest(false)
	tests := []struct {
		name    string
		surface SurfaceContractID
		tool    string
		raw     []byte
		code    string
	}{
		{"missing_required", "planner-plan.v1", "validate_artifact", mutateObject(t, validArtifact, func(value map[string]any) { delete(value, "artifact_file") }), "request_required_missing"},
		{"surface_const", "planner-plan.v1", "validate_artifact", mutateObject(t, validArtifact, func(value map[string]any) { value["surface_contract"] = "planner-authoring.v1" }), "request_const_invalid"},
		{"explicit_null", "planner-plan.v1", "validate_artifact", mutateObject(t, validArtifact, func(value map[string]any) { value["artifact_name"] = nil }), "request_type_invalid"},
		{"opaque_id_min_length", "planner-plan.v1", "validate_artifact", mutateObject(t, validArtifact, func(value map[string]any) { value["expected_packet_id"] = "" }), "request_string_too_short"},
		{"artifact_name_pattern", "planner-plan.v1", "validate_artifact", mutateObject(t, validArtifact, func(value map[string]any) { value["artifact_name"] = "bad/name" }), "request_pattern_invalid"},
		{"media_type_pattern", "planner-plan.v1", "validate_artifact", mutateObject(t, validArtifact, func(value map[string]any) { value["media_type"] = "not-a-media-type" }), "request_pattern_invalid"},
		{"numeric_minimum", "planner-authoring.v1", "list_projects", []byte(`{"surface_contract":"planner-authoring.v1","limit":0}`), "request_number_too_small"},
		{"numeric_maximum", "planner-authoring.v1", "list_projects", []byte(`{"surface_contract":"planner-authoring.v1","limit":101}`), "request_number_too_large"},
		{"operation_enum", "planner-plan.v1", "create_operation_packet", mutateObject(t, validPacket, func(value map[string]any) { value["operation_id"] = "auditor.audit" }), "request_enum_invalid"},
		{"malformed_union", "planner-plan.v1", "create_operation_packet", mutateObject(t, validPacket, func(value map[string]any) { value["inputs"].([]any)[0].(map[string]any)["source"] = map[string]any{} }), "request_union_invalid"},
		{"array_maximum", "planner-plan.v1", "create_operation_packet", mutateObject(t, validPacket, func(value map[string]any) {
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
	if err := json.Unmarshal(plannerPlanRequest(false), &request); err != nil {
		t.Fatal(err)
	}
	inputs := request["inputs"].([]any)
	request["inputs"] = append(inputs, inputs[0])
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SemanticRequestBasis("planner-plan.v1", "create_operation_packet", raw); err == nil || err.Error() != "request_semantic_invalid:$" {
		t.Fatalf("duplicate input error = %v", err)
	}

	disallowed := mutateObject(t, plannerPlanRequest(false), func(value map[string]any) {
		value["workflow_references"] = []any{map[string]any{"kind": "run", "run_id": "run-1"}}
	})
	if _, err := SemanticRequestBasis("planner-plan.v1", "create_operation_packet", disallowed); err == nil || err.Error() != "request_semantic_invalid:$" {
		t.Fatalf("operation-disallowed reference error = %v", err)
	}
}

func TestRequestErrorsDoNotEchoUnboundedCallerValues(t *testing.T) {
	marker := strings.Repeat("caller-secret-marker", 200)
	raw := mutateObject(t, artifactRequest("https://files.example/one", "file-one"), func(value map[string]any) {
		value[marker] = marker
	})
	_, err := ValidateRequest("planner-plan.v1", "validate_artifact", raw)
	if err == nil {
		t.Fatal("unknown property was accepted")
	}
	if strings.Contains(err.Error(), marker) || len(err.Error()) > 128 {
		t.Fatalf("error is not bounded: %q", err.Error())
	}
}

func TestConcurrentRegistrySchemaAndSemanticReadsAreDeterministic(t *testing.T) {
	expectedBasis, err := SemanticRequestBasis("planner-plan.v1", "create_operation_packet", plannerPlanRequest(false))
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
				operations[0].RequiredInputs[0].InputName = "caller-mutation"
				operation, ok := Lookup("planner.requirements")
				if !ok || operation.RequiredInputs[0].InputName != "confirmed_intent" {
					errorsOut <- &RequestError{Code: "registry_copy_mutated"}
					return
				}
				basis, err := SemanticRequestBasis("planner-plan.v1", "create_operation_packet", plannerPlanRequest(iteration%2 == 1))
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
			raw: mutateObject(t, plannerPlanRequest(false), func(value map[string]any) {
				value["workflow_references"] = []any{map[string]any{"kind": "run", "run_id": "run-1"}}
			}),
		},
		{
			name: "source_kind_branch_mismatch",
			raw: mutateObject(t, plannerPlanRequest(false), func(value map[string]any) {
				value["inputs"].([]any)[0].(map[string]any)["source"] = map[string]any{"text": "requirements"}
			}),
		},
		{
			name: "workflow_record_digest_mismatch",
			raw: mutateObject(t, plannerPlanRequest(false), func(value map[string]any) {
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
			if err := ValidateOperationRequest("planner-plan.v1", "create_operation_packet", test.raw); err == nil || err.Error() != "request_semantic_invalid:$" {
				t.Fatalf("operation validation error = %v", err)
			}
		})
	}
	if err := validateSurfaceAction("planner-authoring.v1", "validate_artifact"); err == nil {
		t.Fatal("planner-authoring accepted validate_artifact")
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

func plannerPlanRequest(reverse bool) []byte {
	shaA := strings.Repeat("a", 64)
	shaB := strings.Repeat("b", 64)
	shaC := strings.Repeat("c", 64)
	inputs := []any{
		input("approved_requirements", "relay_artifact", "requirements.md", "text/markdown", shaA, map[string]any{"artifact_id": "artifact-requirements"}),
		input("approved_design", "relay_artifact", "design.md", "text/markdown", shaB, map[string]any{"artifact_id": "artifact-design"}),
		input("plan_mode_selection", "inline_text", "mode.txt", "text/plain", shaC, map[string]any{"text": "plan"}),
	}
	attestations := []any{
		map[string]any{"kind": "approved_artifact", "input_name": "approved_requirements", "subject_sha256": shaA, "approved": true},
		clearance("approved_requirements", shaA),
		map[string]any{"kind": "approved_artifact", "input_name": "approved_design", "subject_sha256": shaB, "approved": true},
		clearance("approved_design", shaB),
		map[string]any{"kind": "execution_mode_selection", "input_name": "plan_mode_selection", "selected_mode": "plan"},
		clearance("plan_mode_selection", shaC),
	}
	revisions := []any{
		map[string]any{"repository_key": "zeta", "commit_oid": strings.Repeat("d", 40)},
		map[string]any{"repository_key": "alpha", "commit_oid": strings.Repeat("e", 40)},
	}
	anchors := []any{
		map[string]any{"repository_key": "zeta", "anchor_name": "later", "purpose": "reviewed_source_basis", "commit_oid": strings.Repeat("f", 40), "expected_tree_oid": strings.Repeat("1", 40)},
		map[string]any{"repository_key": "alpha", "anchor_name": "base", "purpose": "operator_supplied_comparison", "commit_oid": strings.Repeat("2", 40), "expected_tree_oid": strings.Repeat("3", 40)},
	}
	if reverse {
		reverseAny(inputs)
		reverseAny(attestations)
		reverseAny(revisions)
		reverseAny(anchors)
	}
	request := map[string]any{
		"surface_contract":    "planner-plan.v1",
		"mutation_id":         "mutation-1",
		"operation_id":        "planner.plan",
		"project_id":          "project-1",
		"inputs":              inputs,
		"workflow_references": []any{},
		"attestations":        attestations,
		"primary_revisions":   revisions,
		"comparison_anchors":  anchors,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return raw
}

func artifactRequest(downloadURL, fileID string) []byte {
	sha := strings.Repeat("a", 64)
	request := map[string]any{
		"surface_contract":   "planner-plan.v1",
		"expected_packet_id": "packet-1",
		"artifact_file": map[string]any{
			"download_url": downloadURL,
			"file_id":      fileID,
			"mime_type":    "application/json",
			"file_name":    "plan.json",
		},
		"artifact_name":   "plan.json",
		"media_type":      "application/json",
		"expected_sha256": sha,
		"sensitive_data_clearance": map[string]any{
			"policy_version": "relay.canonical-artifact-sensitive-data.v1",
			"subject_sha256": sha,
			"declaration":    clearanceDeclaration(),
			"confirmed":      true,
		},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return raw
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
