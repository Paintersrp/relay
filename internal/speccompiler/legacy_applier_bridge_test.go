package speccompiler

import (
	"bytes"
	"reflect"
	"testing"
)

func TestLegacyApplierBridgeDerivesV1OperationsFromCanonicalWork(t *testing.T) {
	projection, diagnostics := ProjectExecutionPayload(readFixture(t, "valid.execution-spec.json"))
	if len(diagnostics) != 0 {
		t.Fatalf("bridge diagnostics = %+v", diagnostics)
	}
	if len(projection.OperationGroups) != 0 || len(projection.DeterministicOperations) != 6 {
		t.Fatalf("legacy projection = %+v", projection)
	}
	wantIDs := []string{
		"1.1.file.1.change.1",
		"1.1.file.1.change.2",
		"1.1.file.1.change.3",
		"1.1.file.2",
		"1.1.file.3",
		"1.1.file.4",
	}
	for index, want := range wantIDs {
		if projection.DeterministicOperations[index].ID != want {
			t.Fatalf("operation %d id = %q want %q", index, projection.DeterministicOperations[index].ID, want)
		}
	}
	if !reflect.DeepEqual(projection.DeterministicOperations[1].DependsOn, []string{"1.1.file.1.change.1"}) ||
		!reflect.DeepEqual(projection.DeterministicOperations[2].DependsOn, []string{"1.1.file.1.change.2"}) {
		t.Fatalf("file-local dependencies = %+v", projection.DeterministicOperations[:3])
	}
	for _, operation := range projection.DeterministicOperations {
		if operation.Group != "" || operation.OnFailure != "" || len(operation.Guards) != 0 {
			t.Fatalf("bridge synthesized payload-only policy: %+v", operation)
		}
	}
}

func TestLegacyApplierBridgeRoutesV2ToFullBrief(t *testing.T) {
	projection, diagnostics := ProjectExecutionPayload(readFixture(t, "valid-v2.execution-spec.json"))
	if len(diagnostics) != 0 || len(projection.DeterministicOperations) != 0 || len(projection.OperationGroups) != 0 {
		t.Fatalf("v2 bridge result = projection=%+v diagnostics=%+v", projection, diagnostics)
	}
}

func TestLegacyApplierBridgeRejectsAuthoredPayloadAndInvalidInput(t *testing.T) {
	v1Raw := readFixture(t, "valid.execution-spec.json")
	withPayload := bytes.Replace(v1Raw, []byte("  \"steps\": ["), []byte("  \"execution_payload\": {},\n  \"steps\": ["), 1)
	projection, diagnostics := ProjectExecutionPayload(withPayload)
	if len(projection.DeterministicOperations) != 0 || !hasDiagnosticCode(diagnostics, "unknown_property") {
		t.Fatalf("authored payload bridge result = projection=%+v diagnostics=%+v", projection, diagnostics)
	}

	projection, diagnostics = ProjectExecutionPayload([]byte(`{"feature_slug":`))
	if len(projection.DeterministicOperations) != 0 || !hasDiagnosticCode(diagnostics, "invalid_json") {
		t.Fatalf("invalid bridge result = projection=%+v diagnostics=%+v", projection, diagnostics)
	}
}

func hasDiagnosticCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestLegacyApplierBridgeMapsImplicitSubstepDependencies(t *testing.T) {
	projection, diagnostics := ProjectExecutionPayload(executionV1BridgeDependencyDocument())
	if len(diagnostics) != 0 {
		t.Fatalf("bridge diagnostics = %+v", diagnostics)
	}
	if len(projection.DeterministicOperations) != 2 {
		t.Fatalf("bridge operations = %+v", projection.DeterministicOperations)
	}
	first, second := projection.DeterministicOperations[0], projection.DeterministicOperations[1]
	if first.ID != "1.1.file.1.change.1" || second.ID != "1.2.file.1.change.1" || !reflect.DeepEqual(second.DependsOn, []string{first.ID}) {
		t.Fatalf("bridge dependency mapping = first=%+v second=%+v", first, second)
	}
}

func executionV1BridgeDependencyDocument() []byte {
	return []byte(`{
  "schema_version": "1.0",
  "feature_slug": "bridge-dependency",
  "repo_target": "relay",
  "branch": "main",
  "base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "goal": "Exercise legacy bridge dependency mapping.",
  "context": "Bridge dependency fixture.",
  "scope": {
    "in_scope": ["Exercise repeated-path implicit dependencies."],
    "out_of_scope": ["Do not mutate repositories."]
  },
  "steps": [
    {
      "number": 1,
      "goal": "Project two ordered substeps.",
      "substeps": [
        {
          "number": 1,
          "instruction": "Apply the first exact directive.",
          "files": [
            {
              "path": "internal/example/config.go",
              "operation": "modify",
              "purpose": "Provide the dependency source operation.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace",
                    "old_text": "const enabled = false\n",
                    "new_text": "const enabled = true\n",
                    "expected_occurrences": 1
                  }
                ]
              }
            }
          ],
          "completion_criteria": ["The first operation is projected."]
        },
        {
          "number": 2,
          "instruction": "Apply the later exact directive.",
          "files": [
            {
              "path": "internal/example/config.go",
              "operation": "modify",
              "purpose": "Provide the dependent operation.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace",
                    "old_text": "const mode = \"old\"\n",
                    "new_text": "const mode = \"new\"\n",
                    "expected_occurrences": 1
                  }
                ]
              }
            }
          ],
          "completion_criteria": ["The later operation depends on the earlier substep."]
        }
      ],
      "completion_criteria": ["Bridge dependencies are deterministic."]
    }
  ],
  "validation": {
    "commands": [
      {"command": "go test ./internal/speccompiler", "expected": "The focused compiler tests pass."}
    ]
  },
  "completion_criteria": ["The bridge preserves canonical operation order and dependency identity."]
}
`)
}
