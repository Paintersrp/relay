package speccompiler

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestExecutionVersionSelectionAndTypedDocument(t *testing.T) {
	v1Raw := readFixture(t, "valid.execution-spec.json")
	v1Result, v1Document := CompileExecutionSpec("compiler-fixture.execution-spec.json", v1Raw)
	assertSuccess(t, v1Result)
	if v1Document == nil || v1Document.SchemaVersion != "1.0" {
		t.Fatalf("v1 document = %+v", v1Document)
	}

	v2Raw := readFixture(t, "valid-v2.execution-spec.json")
	v2Result, v2Document := CompileExecutionSpec("compiler-v2-fixture.execution-spec.json", v2Raw)
	assertSuccess(t, v2Result)
	if v2Document == nil || v2Document.SchemaVersion != "2.0" {
		t.Fatalf("v2 document = %+v", v2Document)
	}

	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{name: "absent", raw: bytes.Replace(v2Raw, []byte("  \"schema_version\": \"2.0\",\n"), nil, 1)},
		{name: "null", raw: bytes.Replace(v2Raw, []byte(`"schema_version": "2.0"`), []byte(`"schema_version": null`), 1)},
		{name: "wrong type", raw: bytes.Replace(v2Raw, []byte(`"schema_version": "2.0"`), []byte(`"schema_version": 2`), 1)},
		{name: "malformed string", raw: bytes.Replace(v2Raw, []byte(`"schema_version": "2.0"`), []byte(`"schema_version": "current"`), 1)},
	} {
		t.Run("fallback "+test.name, func(t *testing.T) {
			result, document := CompileExecutionSpec("compiler-v2-fixture.execution-spec.json", test.raw)
			assertSuccess(t, result)
			if document == nil || document.SchemaVersion != "2.0" {
				t.Fatalf("fallback document = %+v", document)
			}
			if len(result.Notices) != 1 || result.Notices[0].Code != "schema_version_fallback" {
				t.Fatalf("fallback notices = %+v", result.Notices)
			}
		})
	}

	for _, version := range []string{"1.1", "3.0"} {
		t.Run("unsupported "+version, func(t *testing.T) {
			raw := bytes.Replace(v2Raw, []byte(`"schema_version": "2.0"`), []byte(`"schema_version": "`+version+`"`), 1)
			result, document := CompileExecutionSpec("compiler-v2-fixture.execution-spec.json", raw)
			assertFailureCode(t, result, "unsupported_schema_version")
			if document != nil || len(result.Notices) != 0 {
				t.Fatalf("unsupported version returned document or notice: document=%+v notices=%+v", document, result.Notices)
			}
		})
	}
}

func TestExecutionVersionSpecificSchemaAndRendering(t *testing.T) {
	v1Raw := readFixture(t, "valid.execution-spec.json")
	v1WithMetadata := bytes.Replace(v1Raw, []byte(`          "instruction": "Render the declared file operations in source order.",`), []byte("          \"instruction\": \"Render the declared file operations in source order.\",\n          \"atomic\": true,"), 1)
	assertFailureCode(t, Compile("compiler-fixture.execution-spec.json", v1WithMetadata), "unknown_property")

	for _, fixture := range []struct {
		name     string
		filename string
		raw      []byte
	}{
		{name: "v1", filename: "compiler-fixture.execution-spec.json", raw: v1Raw},
		{name: "v2", filename: "compiler-v2-fixture.execution-spec.json", raw: readFixture(t, "valid-v2.execution-spec.json")},
	} {
		t.Run(fixture.name+" rejects execution_payload", func(t *testing.T) {
			raw := bytes.Replace(fixture.raw, []byte("  \"steps\": ["), []byte("  \"execution_payload\": {},\n  \"steps\": ["), 1)
			assertFailureCode(t, Compile(fixture.filename, raw), "unknown_property")
		})
	}

	v2Raw := readFixture(t, "valid-v2.execution-spec.json")
	noncanonical := bytes.Replace(v2Raw, []byte("          \"depends_on\": [\n            \"1.3\"\n          ],\n          \"atomic\": true,\n"), []byte("          \"atomic\": true,\n          \"depends_on\": [\n            \"1.3\"\n          ],\n"), 1)
	assertFailureCode(t, Compile("compiler-v2-fixture.execution-spec.json", noncanonical), "noncanonical_property_order")

	v2Result := Compile("compiler-v2-fixture.execution-spec.json", v2Raw)
	assertSuccess(t, v2Result)
	golden := string(readFixture(t, "compiler-v2-fixture.executor-brief.md"))
	if dereference(v2Result.Markdown) != golden {
		t.Fatalf("v2 rendered brief does not match golden\n--- got ---\n%s\n--- want ---\n%s", dereference(v2Result.Markdown), golden)
	}
	sections := map[string]string{
		"metadata-free":   substepMarkdown(t, golden, "#### Substep 1.1", "#### Substep 1.2"),
		"dependency-only": substepMarkdown(t, golden, "#### Substep 1.2", "#### Substep 1.3"),
		"atomic-only":     substepMarkdown(t, golden, "#### Substep 1.3", "#### Substep 1.4"),
		"combined":        substepMarkdown(t, golden, "#### Substep 1.4", "#### Step Completion Criteria"),
	}
	if strings.Contains(sections["metadata-free"], "##### Execution Constraints") {
		t.Fatal("metadata-free v2 rendered an empty constraints section")
	}
	if !strings.Contains(sections["dependency-only"], "- Depends on: `1.1`") || strings.Contains(sections["dependency-only"], "Atomic deterministic preflight") {
		t.Fatalf("dependency-only section = %s", sections["dependency-only"])
	}
	if !strings.Contains(sections["atomic-only"], "- Atomic deterministic preflight: not required") || strings.Contains(sections["atomic-only"], "- Depends on:") {
		t.Fatalf("atomic-only section = %s", sections["atomic-only"])
	}
	if !strings.Contains(sections["combined"], "- Depends on: `1.3`\n- Atomic deterministic preflight: required") {
		t.Fatalf("combined section = %s", sections["combined"])
	}

	v1Result := Compile("compiler-fixture.execution-spec.json", v1Raw)
	assertSuccess(t, v1Result)
	if strings.Contains(dereference(v1Result.Markdown), "##### Execution Constraints") {
		t.Fatal("v1 rendered v2 constraints")
	}
}

func substepMarkdown(t *testing.T, markdown, start, end string) string {
	t.Helper()
	startIndex := strings.Index(markdown, start)
	if startIndex < 0 {
		t.Fatalf("missing start heading %q", start)
	}
	endIndex := strings.Index(markdown[startIndex+len(start):], end)
	if endIndex < 0 {
		t.Fatalf("missing end heading %q", end)
	}
	return markdown[startIndex : startIndex+len(start)+endIndex]
}

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
