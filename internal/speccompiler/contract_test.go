package speccompiler

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLexicalDiagnosticsPreserveDuplicateBeforeMalformedJSON(t *testing.T) {
	result := Compile("contract-fixture.execution-spec.json", []byte(`{"feature_slug":"contract-fixture","feature_slug":"contract-fixture",`))
	if result.OutputFilename != nil || result.Markdown != nil {
		t.Fatalf("lexical failure returned partial output: %+v", result)
	}
	got := diagnosticKeys(result.Errors)
	want := []string{
		"|invalid_json",
		"/feature_slug|duplicate_object_key",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected combined lexical diagnostics\ngot:  %v\nwant: %v", got, want)
	}
}

func TestSchemaVersionFallbackForms(t *testing.T) {
	base := readFixture(t, "valid.plan.json")
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "missing", raw: replaceOnce(t, base, []byte("  \"schema_version\": \"1.0\",\n"), nil)},
		{name: "null", raw: replaceOnce(t, base, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": null`))},
		{name: "number", raw: replaceOnce(t, base, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": 1`))},
		{name: "boolean", raw: replaceOnce(t, base, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": true`))},
		{name: "array", raw: replaceOnce(t, base, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": []`))},
		{name: "object", raw: replaceOnce(t, base, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": {}`))},
		{name: "older unsupported", raw: replaceOnce(t, base, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": "0.9"`))},
		{name: "newer unsupported", raw: replaceOnce(t, base, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": "2.0"`))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Compile("compiler-plan-fixture.plan.json", tc.raw)
			assertSuccess(t, result)
			if len(result.Notices) != 1 || result.Notices[0].Code != "schema_version_fallback" || result.Notices[0].Path != "/schema_version" {
				t.Fatalf("expected one schema fallback notice, got %+v", result.Notices)
			}
		})
	}
}

func TestLexicalRejections(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "comment", raw: "{\n  // comment\n  \"feature_slug\": \"contract-fixture\"\n}"},
		{name: "trailing comma", raw: `{"feature_slug":"contract-fixture",}`},
		{name: "multiple values", raw: `{} {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Compile("contract-fixture.plan.json", []byte(tc.raw))
			assertFailureCode(t, result, "invalid_json")
		})
	}
}

func TestCanonicalOrderAcrossObjectShapes(t *testing.T) {
	execution := readFixture(t, "valid.execution-spec.json")
	plan := readFixture(t, "valid.plan.json")
	cases := []struct {
		name     string
		filename string
		raw      []byte
	}{
		{
			name:     "execution root",
			filename: "compiler-fixture.execution-spec.json",
			raw: replaceOnce(t, execution,
				[]byte("  \"goal\": \"Compile a representative Execution Spec fixture.\",\n  \"context\": \"Representative context with `inline code`.\\n\\n```go\\npackage example\\n```\",\n"),
				[]byte("  \"context\": \"Representative context with `inline code`.\\n\\n```go\\npackage example\\n```\",\n  \"goal\": \"Compile a representative Execution Spec fixture.\",\n")),
		},
		{
			name:     "scope",
			filename: "compiler-fixture.execution-spec.json",
			raw:      swapFirst(t, execution, []byte(`"in_scope"`), []byte(`"out_of_scope"`)),
		},
		{
			name:     "step",
			filename: "compiler-fixture.execution-spec.json",
			raw: replaceOnce(t, execution,
				[]byte("      \"number\": 1,\n      \"goal\": \"Render exact implementation directives.\",\n"),
				[]byte("      \"goal\": \"Render exact implementation directives.\",\n      \"number\": 1,\n")),
		},
		{
			name:     "substep",
			filename: "compiler-fixture.execution-spec.json",
			raw: replaceOnce(t, execution,
				[]byte("          \"number\": 1,\n          \"instruction\": \"Render the declared file operations in source order.\",\n"),
				[]byte("          \"instruction\": \"Render the declared file operations in source order.\",\n          \"number\": 1,\n")),
		},
		{
			name:     "normal file",
			filename: "compiler-fixture.execution-spec.json",
			raw: replaceOnce(t, execution,
				[]byte("              \"path\": \"internal/example/config.go\",\n              \"operation\": \"modify\",\n"),
				[]byte("              \"operation\": \"modify\",\n              \"path\": \"internal/example/config.go\",\n")),
		},
		{
			name:     "rename file",
			filename: "compiler-fixture.execution-spec.json",
			raw: replaceOnce(t, execution,
				[]byte("              \"path\": \"internal/example/name.go\",\n              \"destination_path\": \"internal/example/new_name.go\",\n"),
				[]byte("              \"destination_path\": \"internal/example/new_name.go\",\n              \"path\": \"internal/example/name.go\",\n")),
		},
		{
			name:     "modify directive",
			filename: "compiler-fixture.execution-spec.json",
			raw: replaceOnce(t, execution,
				[]byte("                    \"kind\": \"replace\",\n                    \"old_text\": \"const enabled = false\\n\",\n"),
				[]byte("                    \"old_text\": \"const enabled = false\\n\",\n                    \"kind\": \"replace\",\n")),
		},
		{
			name:     "validation",
			filename: "compiler-fixture.execution-spec.json",
			raw:      swapFirst(t, execution, []byte(`"commands"`), []byte(`"executor_checks"`)),
		},
		{
			name:     "validation command",
			filename: "compiler-fixture.execution-spec.json",
			raw: replaceOnce(t, execution,
				[]byte("        \"command\": \"go test ./internal/speccompiler\",\n        \"expected\": \"The focused compiler tests pass.\"\n"),
				[]byte("        \"expected\": \"The focused compiler tests pass.\",\n        \"command\": \"go test ./internal/speccompiler\"\n")),
		},
		{
			name:     "plan root",
			filename: "compiler-plan-fixture.plan.json",
			raw: replaceOnce(t, plan,
				[]byte("  \"goal\": \"Render a representative Plan fixture.\",\n  \"context\": \"The Plan fixture covers repository targets, dependencies, and pass sections.\",\n"),
				[]byte("  \"context\": \"The Plan fixture covers repository targets, dependencies, and pass sections.\",\n  \"goal\": \"Render a representative Plan fixture.\",\n")),
		},
		{
			name:     "repository target",
			filename: "compiler-plan-fixture.plan.json",
			raw: replaceOnce(t, plan,
				[]byte("      \"repo_target\": \"relay\",\n      \"branch\": \"feat/simplification\",\n"),
				[]byte("      \"branch\": \"feat/simplification\",\n      \"repo_target\": \"relay\",\n")),
		},
		{
			name:     "pass",
			filename: "compiler-plan-fixture.plan.json",
			raw: replaceOnce(t, plan,
				[]byte("      \"number\": 1,\n      \"name\": \"Foundation\",\n"),
				[]byte("      \"name\": \"Foundation\",\n      \"number\": 1,\n")),
		},
		{
			name:     "source target",
			filename: "compiler-plan-fixture.plan.json",
			raw: replaceOnce(t, plan,
				[]byte("          \"path\": \"internal/speccompiler\",\n          \"purpose\": \"Compiler package.\"\n"),
				[]byte("          \"purpose\": \"Compiler package.\",\n          \"path\": \"internal/speccompiler\"\n")),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Compile(tc.filename, tc.raw)
			assertFailureCode(t, result, "noncanonical_property_order")
		})
	}
}

func TestExecutionStructuralDiagnostics(t *testing.T) {
	validModify := `{
                "path": "internal/example/a.go",
                "operation": "modify",
                "purpose": "Modify a file.",
                "implementation": {
                  "changes": [
                    {
                      "kind": "replace",
                      "old_text": "old\n",
                      "new_text": "new\n",
                      "expected_occurrences": 1
                    }
                  ]
                }
              }`
	validCreate := `{
                "path": "internal/example/b.go",
                "operation": "create",
                "purpose": "Create a file.",
                "implementation": {
                  "content": "package example\n"
                }
              }`
	validRename := `{
                "path": "internal/example/old.go",
                "destination_path": "internal/example/new.go",
                "operation": "rename",
                "purpose": "Rename a file.",
                "implementation": {
                  "preserve_content": true
                }
              }`
	cases := []struct {
		name  string
		files string
		code  string
	}{
		{name: "missing implementation", files: strings.Replace(validModify, `"implementation"`, `"missing_implementation"`, 1), code: "missing_file_implementation"},
		{name: "invalid operation", files: strings.Replace(validModify, `"modify"`, `"copy"`, 1), code: "invalid_file_operation"},
		{name: "incompatible create implementation", files: strings.Replace(validCreate, `"implementation": {
                  "content": "package example\n"
                }`, `"implementation": "wrong"`, 1), code: "operation_incompatible_file_implementation"},
		{name: "empty modify changes", files: strings.Replace(validModify, `"changes": [
                    {
                      "kind": "replace",
                      "old_text": "old\n",
                      "new_text": "new\n",
                      "expected_occurrences": 1
                    }
                  ]`, `"changes": []`, 1), code: "empty_modify_changes"},
		{name: "invalid modify directive", files: strings.Replace(validModify, `"kind": "replace"`, `"kind": "merge"`, 1), code: "invalid_modify_directive"},
		{name: "invalid expected occurrences", files: strings.Replace(validModify, `"expected_occurrences": 1`, `"expected_occurrences": 0`, 1), code: "invalid_expected_occurrences"},
		{name: "missing rename destination", files: strings.Replace(validRename, `                "destination_path": "internal/example/new.go",
`, "", 1), code: "missing_rename_destination"},
		{name: "unexpected rename destination", files: strings.Replace(validModify, `                "operation": "modify",
`, `                "destination_path": "internal/example/new.go",
                "operation": "modify",
`, 1), code: "unexpected_rename_destination"},
		{name: "conflicting operation", files: validModify + ",\n" + strings.Replace(validCreate, `internal/example/b.go`, `internal/example/a.go`, 1), code: "conflicting_file_operation"},
		{name: "conflicting rename destination", files: validRename + ",\n" + strings.Replace(validRename, `internal/example/new.go`, `internal/example/other.go`, 1), code: "conflicting_rename_destination"},
		{name: "rename with both modes", files: strings.Replace(validRename, `"preserve_content": true`, `"preserve_content": true,
                  "content": "package example\n"`, 1), code: "invalid_rename_implementation"},
		{name: "rename with neither mode", files: strings.Replace(validRename, `"preserve_content": true`, `"other": true`, 1), code: "invalid_rename_implementation"},
		{name: "placeholder target", files: strings.Replace(validCreate, `package example\n`, `TODO`, 1), code: "placeholder_implementation_content"},
		{name: "template marker", files: strings.Replace(validCreate, `package example\n`, "package {"+"{name}"+"}\\n", 1), code: "unresolved_template_marker"},
		{name: "unsafe path", files: strings.Replace(validModify, `internal/example/a.go`, `../a.go`, 1), code: "unsafe_repository_path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Compile("contract-fixture.execution-spec.json", executionDocument(tc.files, 1, 1, true))
			assertFailureCode(t, result, tc.code)
		})
	}

	sequentialCases := []struct {
		name          string
		stepNumber    int
		substepNumber int
		commands      bool
		code          string
	}{
		{name: "nonsequential step", stepNumber: 2, substepNumber: 1, commands: true, code: "nonsequential_step_number"},
		{name: "nonsequential substep", stepNumber: 1, substepNumber: 2, commands: true, code: "nonsequential_substep_number"},
		{name: "missing validation command", stepNumber: 1, substepNumber: 1, commands: false, code: "missing_validation_command"},
		{name: "missing file declaration", stepNumber: 1, substepNumber: 1, commands: true, code: "missing_file_declaration"},
	}
	for _, tc := range sequentialCases {
		t.Run(tc.name, func(t *testing.T) {
			files := validModify
			if tc.code == "missing_file_declaration" {
				files = ""
			}
			result := Compile("contract-fixture.execution-spec.json", executionDocument(files, tc.stepNumber, tc.substepNumber, tc.commands))
			assertFailureCode(t, result, tc.code)
		})
	}
}

func TestRepositoryPathSafetyMatrix(t *testing.T) {
	cases := []struct {
		name         string
		path         string
		expectedCode string
	}{
		{name: "reject empty", path: "", expectedCode: "empty_required_value"},
		{name: "reject absolute", path: "/absolute.go", expectedCode: "unsafe_repository_path"},
		{name: "reject UNC or double slash", path: "//server/share.go", expectedCode: "unsafe_repository_path"},
		{name: "reject Windows drive prefix", path: "C:/source/file.go", expectedCode: "unsafe_repository_path"},
		{name: "reject backslash", path: "dir\\file.go", expectedCode: "unsafe_repository_path"},
		{name: "reject empty segment", path: "dir//file.go", expectedCode: "unsafe_repository_path"},
		{name: "reject root current segment", path: ".", expectedCode: "unsafe_repository_path"},
		{name: "reject root parent segment", path: "..", expectedCode: "unsafe_repository_path"},
		{name: "reject terminal current segment", path: "dir/.", expectedCode: "unsafe_repository_path"},
		{name: "reject terminal parent segment", path: "dir/..", expectedCode: "unsafe_repository_path"},
		{name: "reject internal current segment", path: "dir/./file.go", expectedCode: "unsafe_repository_path"},
		{name: "reject internal parent segment", path: "dir/../file.go", expectedCode: "unsafe_repository_path"},
		{name: "reject leading whitespace", path: " leading.go", expectedCode: "unsafe_repository_path"},
		{name: "reject trailing whitespace", path: "trailing.go ", expectedCode: "unsafe_repository_path"},
		{name: "reject control character", path: "dir/\u0001file.go", expectedCode: "unsafe_repository_path"},
		{name: "accept compiler source", path: "internal/speccompiler/compiler.go"},
		{name: "accept route parameter", path: "apps/web/src/routes/run.$runID.tsx"},
		{name: "accept generated reference", path: "docs/generated/agent-references/index.json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pathJSON, err := json.Marshal(tc.path)
			if err != nil {
				t.Fatalf("marshal path %q: %v", tc.path, err)
			}
			file := fmt.Sprintf(`{
                "path": %s,
                "operation": "create",
                "purpose": "Exercise repository path validation.",
                "implementation": {
                  "content": "package example\n"
                }
              }`, pathJSON)
			result := Compile("contract-fixture.execution-spec.json", executionDocument(file, 1, 1, true))
			if tc.expectedCode == "" {
				assertSuccess(t, result)
				return
			}
			assertFailureCode(t, result, tc.expectedCode)
		})
	}
}

func TestPlanStructuralDiagnostics(t *testing.T) {
	base := readFixture(t, "valid.plan.json")
	cases := []struct {
		name string
		raw  []byte
		code string
	}{
		{
			name: "duplicate repository target",
			raw: replaceOnce(t, base,
				[]byte("    }\n  ],\n  \"passes\":"),
				[]byte("    },\n    {\n      \"repo_target\": \"relay\",\n      \"branch\": \"feat/simplification\",\n      \"planning_base_commit\": \"e9e1759821de943643f6ea7f6ae0ceb7db9db951\"\n    }\n  ],\n  \"passes\":")),
			code: "duplicate_repository_target",
		},
		{name: "unknown repository target", raw: replaceOnce(t, base, []byte(`"repo_target": "relay",
      "goal": "Create the foundation."`), []byte(`"repo_target": "missing",
      "goal": "Create the foundation."`)), code: "unknown_repository_target"},
		{name: "nonsequential pass", raw: replaceOnce(t, base, []byte(`"number": 1,
      "name": "Foundation"`), []byte(`"number": 3,
      "name": "Foundation"`)), code: "nonsequential_pass_number"},
		{name: "duplicate dependency", raw: replaceOnce(t, base, []byte(`"depends_on": [
        1
      ]`), []byte(`"depends_on": [
        1,
        1
      ]`)), code: "duplicate_dependency"},
		{name: "self dependency", raw: replaceOnce(t, base, []byte(`"depends_on": []`), []byte(`"depends_on": [
        1
      ]`)), code: "self_dependency"},
		{name: "forward dependency", raw: replaceOnce(t, base, []byte(`"depends_on": []`), []byte(`"depends_on": [
        2
      ]`)), code: "forward_dependency"},
		{name: "unknown dependency", raw: replaceOnce(t, base, []byte(`"depends_on": []`), []byte(`"depends_on": [
        3
      ]`)), code: "unknown_dependency"},
		{name: "circular dependency", raw: replaceOnce(t, base, []byte(`"depends_on": []`), []byte(`"depends_on": [
        2
      ]`)), code: "circular_dependency"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Compile("compiler-plan-fixture.plan.json", tc.raw)
			assertFailureCode(t, result, tc.code)
		})
	}
}

func TestAdditionalRendererBranches(t *testing.T) {
	files := `{
                "path": "internal/example/config.go",
                "operation": "modify",
                "purpose": "Render remaining modify directives.",
                "implementation": {
                  "changes": [
                    {
                      "kind": "insert_before",
                      "anchor": "const enabled = true\n",
                      "content": "const prefix = true\n",
                      "expected_occurrences": 1
                    },
                    {
                      "kind": "replace_file",
                      "content": "package example\n"
                    }
                  ]
                }
              },
              {
                "path": "internal/example/old.go",
                "destination_path": "internal/example/new.go",
                "operation": "rename",
                "purpose": "Render rename replacement content.",
                "implementation": {
                  "content": "package example\n"
                }
              }`
	result := Compile("contract-fixture.execution-spec.json", executionDocument(files, 1, 1, true))
	assertSuccess(t, result)
	for _, expected := range []string{
		"- insert_before, expected occurrences: 1\n",
		"- replace_file\n",
		"###### `rename` `internal/example/old.go` -> `internal/example/new.go`\n",
		"Content:\n\n```text\npackage example\n```\n",
	} {
		if !strings.Contains(*result.Markdown, expected) {
			t.Fatalf("rendered Markdown missing %q\n%s", expected, *result.Markdown)
		}
	}
	assertOneFinalNewline(t, *result.Markdown)
}

func TestEmbeddedSchemasMatchPinnedRelaySpecsBlobs(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: "schemas/plan.schema.json", want: "2a2fb55b39d6be8d79ab1de124c017d85ea1d872"},
		{path: "schemas/execution-spec.schema.json", want: "af6a5f0d8f546b5434dfe104c7b7f4159ff40cbe"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := schemaFS.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read embedded schema: %v", err)
			}
			if got := gitBlobSHA(raw); got != tc.want {
				t.Fatalf("embedded schema blob mismatch: got %s want %s", got, tc.want)
			}
		})
	}
}

func executionDocument(files string, stepNumber, substepNumber int, includeCommands bool) []byte {
	commands := "[]"
	if includeCommands {
		commands = `[
      {
        "command": "go test ./internal/speccompiler",
        "expected": "The focused compiler tests pass."
      }
    ]`
	}
	return []byte(fmt.Sprintf(`{
  "schema_version": "1.0",
  "feature_slug": "contract-fixture",
  "repo_target": "relay",
  "branch": "feat/simplification",
  "base_commit": "52b814a76d1776503d335e84fb85ba6ceeab4cf4",
  "goal": "Exercise one compiler contract rule.",
  "context": "Contract fixture.",
  "scope": {
    "in_scope": [
      "Exercise the selected rule."
    ],
    "out_of_scope": [
      "Do not mutate repositories."
    ]
  },
  "steps": [
    {
      "number": %d,
      "goal": "Validate the selected rule.",
      "substeps": [
        {
          "number": %d,
          "instruction": "Compile the contract fixture.",
          "files": [
%s
          ],
          "completion_criteria": [
            "The selected rule is covered."
          ]
        }
      ],
      "completion_criteria": [
        "The step is complete."
      ]
    }
  ],
  "validation": {
    "commands": %s
  },
  "completion_criteria": [
    "The document compiles or fails with the expected diagnostic."
  ]
}
`, stepNumber, substepNumber, files, commands))
}

func replaceOnce(t *testing.T, raw, old, replacement []byte) []byte {
	t.Helper()
	if bytes.Count(raw, old) != 1 {
		t.Fatalf("expected exactly one replacement target %q, found %d", old, bytes.Count(raw, old))
	}
	return bytes.Replace(raw, old, replacement, 1)
}

func swapFirst(t *testing.T, raw, first, second []byte) []byte {
	t.Helper()
	marker := []byte(`"__contract_swap_marker__"`)
	if bytes.Contains(raw, marker) {
		t.Fatal("swap marker unexpectedly present")
	}
	result := replaceOnce(t, raw, first, marker)
	result = replaceOnce(t, result, second, first)
	return replaceOnce(t, result, marker, second)
}

func diagnosticKeys(diagnostics []Diagnostic) []string {
	keys := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		keys[i] = diagnostic.Path + "|" + diagnostic.Code
	}
	return keys
}

func gitBlobSHA(raw []byte) string {
	hash := sha1.New()
	_, _ = fmt.Fprintf(hash, "blob %d%c", len(raw), byte(0))
	_, _ = hash.Write(raw)
	return hex.EncodeToString(hash.Sum(nil))
}
