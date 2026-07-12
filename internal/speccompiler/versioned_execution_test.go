package speccompiler

import (
	"bytes"
	"reflect"
	"testing"
)

func TestExecutionDependencyDiagnostics(t *testing.T) {
	cases := []struct {
		name          string
		firstDepends  string
		secondDepends string
		code          string
	}{
		{name: "malformed", secondDepends: `["01.1"]`, code: "invalid_substep_reference"},
		{name: "wrong type", secondDepends: `[1]`, code: "invalid_substep_reference"},
		{name: "duplicate", secondDepends: `["1.1", "1.1"]`, code: "duplicate_dependency"},
		{name: "self", secondDepends: `["1.2"]`, code: "self_dependency"},
		{name: "forward", firstDepends: `["1.2"]`, code: "forward_dependency"},
		{name: "unknown", secondDepends: `["9.9"]`, code: "unknown_dependency"},
		{name: "circular", firstDepends: `["1.2"]`, secondDepends: `["1.1"]`, code: "circular_dependency"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := Compile("dependency-fixture.execution-spec.json", executionV2DependencyDocument(test.firstDepends, test.secondDepends))
			assertFailureCode(t, result, test.code)
		})
	}
}

func TestExecutionCompileDocumentAndProjectionAreDeterministic(t *testing.T) {
	raw := readFixture(t, "valid-v2.execution-spec.json")
	firstResult, firstDocument := CompileExecutionSpec("compiler-v2-fixture.execution-spec.json", raw)
	secondResult, secondDocument := CompileExecutionSpec("compiler-v2-fixture.execution-spec.json", append([]byte(nil), raw...))
	if !reflect.DeepEqual(firstResult, secondResult) || !reflect.DeepEqual(firstDocument, secondDocument) {
		t.Fatalf("identical bytes produced different compiler values")
	}
	firstProjection, firstDiagnostics := ProjectExecutionSpec(firstDocument)
	secondProjection, secondDiagnostics := ProjectExecutionSpec(secondDocument)
	if !reflect.DeepEqual(firstProjection, secondProjection) || !reflect.DeepEqual(firstDiagnostics, secondDiagnostics) {
		t.Fatalf("identical documents produced different projections")
	}

	invalid := bytes.Replace(raw, []byte("  \"steps\": ["), []byte("  \"execution_payload\": {},\n  \"steps\": ["), 1)
	result, document := CompileExecutionSpec("compiler-v2-fixture.execution-spec.json", invalid)
	assertFailureCode(t, result, "unknown_property")
	if document != nil {
		t.Fatalf("invalid input returned document: %+v", document)
	}
}

func executionV2DependencyDocument(firstDepends, secondDepends string) []byte {
	dependencyField := func(value string) string {
		if value == "" {
			return ""
		}
		return "          \"depends_on\": " + value + ",\n"
	}
	return []byte(`{
  "schema_version": "2.0",
  "feature_slug": "dependency-fixture",
  "repo_target": "relay",
  "branch": "main",
  "base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "goal": "Exercise dependency validation.",
  "context": "Dependency fixture.",
  "scope": {
    "in_scope": ["Exercise dependency diagnostics."],
    "out_of_scope": ["Do not mutate repositories."]
  },
  "steps": [
    {
      "number": 1,
      "goal": "Validate dependencies.",
      "substeps": [
        {
          "number": 1,
          "instruction": "Create the first file.",
` + dependencyField(firstDepends) + `          "files": [
            {
              "path": "internal/example/first.go",
              "operation": "create",
              "purpose": "Provide first work.",
              "implementation": {"content": "package example\n"}
            }
          ],
          "completion_criteria": ["The first substep is declared."]
        },
        {
          "number": 2,
          "instruction": "Create the second file.",
` + dependencyField(secondDepends) + `          "files": [
            {
              "path": "internal/example/second.go",
              "operation": "create",
              "purpose": "Provide second work.",
              "implementation": {"content": "package example\n"}
            }
          ],
          "completion_criteria": ["The second substep is declared."]
        }
      ],
      "completion_criteria": ["Dependencies are validated."]
    }
  ],
  "validation": {
    "commands": [
      {"command": "go test ./internal/speccompiler", "expected": "The focused compiler tests pass."}
    ]
  },
  "completion_criteria": ["The dependency fixture receives the expected diagnostic."]
}
`)
}
