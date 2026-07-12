package speccompiler

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompileExecutionSpecFilenameVariantsMatchOneGolden(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	golden := string(readFixture(t, "compiler-fixture.executor-brief.md"))

	unqualified := Compile("compiler-fixture.execution-spec.json", raw)
	assertSuccess(t, unqualified)
	if unqualified.OutputFilename == nil || *unqualified.OutputFilename != "compiler-fixture.executor-brief.md" {
		t.Fatalf("unexpected unqualified output filename: %#v", unqualified.OutputFilename)
	}
	if unqualified.Markdown == nil || *unqualified.Markdown != golden {
		t.Fatalf("rendered brief does not match golden\n--- got ---\n%s\n--- want ---\n%s", dereference(unqualified.Markdown), golden)
	}
	assertOneFinalNewline(t, *unqualified.Markdown)

	qualified := Compile("compiler-fixture.pass-12.execution-spec.json", raw)
	assertSuccess(t, qualified)
	if qualified.OutputFilename == nil || *qualified.OutputFilename != "compiler-fixture.pass-12.executor-brief.md" {
		t.Fatalf("unexpected qualified output filename: %#v", qualified.OutputFilename)
	}
	if qualified.Markdown == nil || *qualified.Markdown != *unqualified.Markdown {
		t.Fatalf("pass qualification changed rendered brief content")
	}
}

func TestCompilePlanMatchesGolden(t *testing.T) {
	raw := readFixture(t, "valid.plan.json")
	golden := string(readFixture(t, "compiler-plan-fixture.plan.md"))
	result := Compile("compiler-plan-fixture.plan.json", raw)
	assertSuccess(t, result)
	if result.OutputFilename == nil || *result.OutputFilename != "compiler-plan-fixture.plan.md" {
		t.Fatalf("unexpected output filename: %#v", result.OutputFilename)
	}
	if result.Markdown == nil || *result.Markdown != golden {
		t.Fatalf("rendered plan does not match golden")
	}
	assertOneFinalNewline(t, *result.Markdown)
}

func TestCompileIsDeterministic(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	first := Compile("compiler-fixture.execution-spec.json", raw)
	second := Compile("compiler-fixture.execution-spec.json", append([]byte(nil), raw...))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("identical input produced different results\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestDuplicateKeysStopLaterValidation(t *testing.T) {
	result := Compile("duplicate-key.execution-spec.json", readFixture(t, "duplicate.execution-spec.json"))
	assertFailureCode(t, result, "duplicate_object_key")
	if len(result.Errors) != 1 {
		t.Fatalf("duplicate fixture should stop later validation, got %+v", result.Errors)
	}
}

func TestNoncanonicalOrderBlocksRendering(t *testing.T) {
	result := Compile("noncanonical-order.execution-spec.json", readFixture(t, "noncanonical.execution-spec.json"))
	assertFailureCode(t, result, "noncanonical_property_order")
}

func TestSchemaVersionAnomalyIsNonblockingAndOutputNeutral(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		fixture      string
		currentToken []byte
		metadataLine []byte
		slugToken    []byte
		project      bool
	}{
		{
			name:         "plan",
			filename:     "compiler-plan-fixture.plan.json",
			fixture:      "valid.plan.json",
			currentToken: []byte(`"1.0"`),
			metadataLine: []byte("  \"schema_version\": \"1.0\",\n"),
			slugToken:    []byte(`"feature_slug": "compiler-plan-fixture"`),
		},
		{
			name:         "execution_spec",
			filename:     "compiler-fixture.execution-spec.json",
			fixture:      "valid.execution-spec.json",
			currentToken: []byte(`"2.0"`),
			metadataLine: []byte("  \"schema_version\": \"2.0\",\n"),
			slugToken:    []byte(`"feature_slug": "compiler-fixture"`),
			project:      true,
		},
	}
	variants := []struct {
		name        string
		replacement []byte
		absent      bool
	}{
		{name: "absent", absent: true},
		{name: "null", replacement: []byte(`null`)},
		{name: "boolean", replacement: []byte(`true`)},
		{name: "number", replacement: []byte(`7`)},
		{name: "object", replacement: []byte(`{"version":"2.0"}`)},
		{name: "array", replacement: []byte(`["2.0"]`)},
		{name: "malformed_string", replacement: []byte(`"not-a-version"`)},
		{name: "stale", replacement: []byte(`"0.1"`)},
		{name: "unsupported", replacement: []byte(`"3.7"`)},
		{name: "future", replacement: []byte(`"999.0"`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := readFixture(t, test.fixture)
			baseline := Compile(test.filename, current)
			assertSuccess(t, baseline)
			if len(baseline.Notices) != 0 {
				t.Fatalf("current metadata notices = %+v", baseline.Notices)
			}

			var baselineProjection ExecutionProjection
			if test.project {
				compiled, document := CompileExecutionSpec(test.filename, current)
				assertSuccess(t, compiled)
				if document == nil {
					t.Fatal("current Execution Spec omitted its document")
				}
				var diagnostics []Diagnostic
				baselineProjection, diagnostics = ProjectExecutionSpec(document)
				if len(diagnostics) != 0 {
					t.Fatalf("current projection diagnostics = %+v", diagnostics)
				}
			}

			for _, variant := range variants {
				t.Run(variant.name, func(t *testing.T) {
					raw := append([]byte(nil), current...)
					if variant.absent {
						raw = bytes.Replace(raw, test.metadataLine, nil, 1)
					} else {
						raw = bytes.Replace(raw, test.currentToken, variant.replacement, 1)
					}
					result := Compile(test.filename, raw)
					assertSuccess(t, result)
					if len(result.Notices) != 1 || result.Notices[0].Code != "schema_version_anomaly" {
						t.Fatalf("notices = %+v", result.Notices)
					}
					if dereference(result.Markdown) != dereference(baseline.Markdown) {
						t.Fatal("schema_version metadata changed rendered output")
					}

					if test.project {
						compiled, document := CompileExecutionSpec(test.filename, raw)
						assertSuccess(t, compiled)
						if document == nil {
							t.Fatal("metadata variant omitted its Execution Spec document")
						}
						projection, diagnostics := ProjectExecutionSpec(document)
						if len(diagnostics) != 0 {
							t.Fatalf("projection diagnostics = %+v", diagnostics)
						}
						if !reflect.DeepEqual(projection, baselineProjection) {
							t.Fatalf("schema_version metadata changed projection\ngot:  %+v\nwant: %+v", projection, baselineProjection)
						}
					}
				})
			}

			t.Run("anomaly_does_not_mask_current_content_error", func(t *testing.T) {
				raw := bytes.Replace(current, test.currentToken, []byte(`null`), 1)
				raw = bytes.Replace(raw, test.slugToken, []byte(`"feature_slug": "Invalid Slug"`), 1)
				result := Compile(test.filename, raw)
				if len(result.Notices) != 1 || result.Notices[0].Code != "schema_version_anomaly" {
					t.Fatalf("notices = %+v", result.Notices)
				}
				if len(result.Errors) == 0 || result.Markdown != nil || result.OutputFilename != nil {
					t.Fatalf("metadata anomaly masked current-content failure: %+v", result)
				}
			})
		})
	}
}

func TestPlaceholderTargetContentBlocksRendering(t *testing.T) {
	result := Compile("placeholder-content.execution-spec.json", readFixture(t, "placeholder.execution-spec.json"))
	assertFailureCode(t, result, "placeholder_implementation_content")
}

func TestMalformedJSONReturnsNoPartialMarkdown(t *testing.T) {
	result := Compile("broken.plan.json", []byte(`{"feature_slug":"broken",}`))
	assertFailureCode(t, result, "invalid_json")
}

func TestInvalidUTF8ReturnsNoPartialMarkdown(t *testing.T) {
	result := Compile("broken.plan.json", []byte{0xff, 0xfe})
	assertFailureCode(t, result, "invalid_utf8")
}

func TestUnsupportedFilenameStopsBeforeParsing(t *testing.T) {
	result := Compile("compiler-fixture.json", []byte(`not json`))
	assertFailureCode(t, result, "unsupported_artifact_filename")
	if len(result.Errors) != 1 {
		t.Fatalf("expected only filename diagnostic, got %+v", result.Errors)
	}
}

func TestInvalidExecutionPassQualifiersBlockRendering(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	for _, filename := range []string{
		"compiler-fixture.pass-.execution-spec.json",
		"compiler-fixture.pass-0.execution-spec.json",
		"compiler-fixture.pass-01.execution-spec.json",
		"compiler-fixture.pass-x.execution-spec.json",
		"compiler-fixture.pass-1-extra.execution-spec.json",
		"compiler-fixture.pass-1.pass-2.execution-spec.json",
	} {
		t.Run(filename, func(t *testing.T) {
			result := Compile(filename, raw)
			assertFailureCode(t, result, "invalid_pass_qualifier")
		})
	}
}

func TestFilenameMustBeBasename(t *testing.T) {
	result := Compile("dir/compiler-fixture.plan.json", readFixture(t, "valid.plan.json"))
	assertFailureCode(t, result, "invalid_filename_basename")
}

func TestSourceProvenanceIsPinned(t *testing.T) {
	want := Provenance{
		Repository:       "Paintersrp/relay-specs",
		Commit:           "7bd061c3ad989260345da5c5b2f42b3833561242",
		CompilerContract: "contracts/compiler.md",
		Schemas: []SchemaProvenance{
			{ArtifactKind: ArtifactPlan, Version: "1.0", Path: "schemas/plan.schema.json"},
			{ArtifactKind: ArtifactExecutionSpec, Version: "2.0", Path: "schemas/execution-spec.schema.json"},
		},
	}
	if got := SourceProvenance(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected provenance: got=%+v want=%+v", got, want)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
}

func assertSuccess(t *testing.T, result Result) {
	t.Helper()
	if len(result.Errors) != 0 {
		t.Fatalf("expected success, got errors: %+v", result.Errors)
	}
	if result.OutputFilename == nil || result.Markdown == nil {
		t.Fatalf("success omitted output: %+v", result)
	}
}

func assertFailureCode(t *testing.T, result Result, code string) {
	t.Helper()
	if result.OutputFilename != nil || result.Markdown != nil {
		t.Fatalf("failure returned partial output: %+v", result)
	}
	for _, diagnostic := range result.Errors {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("missing diagnostic code %q in %+v", code, result.Errors)
}

func assertOneFinalNewline(t *testing.T, value string) {
	t.Helper()
	if !strings.HasSuffix(value, "\n") || strings.HasSuffix(value, "\n\n") {
		t.Fatalf("output must end with exactly one newline: %q", value[len(value)-min(len(value), 8):])
	}
	if bytes.Contains([]byte(value), []byte("\r")) {
		t.Fatalf("output contains non-LF line endings")
	}
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestSchemaAndCrossFieldDiagnosticsAreAggregatedDeterministically(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	raw = bytes.Replace(raw, []byte(`"base_commit": "e9e1759821de943643f6ea7f6ae0ceb7db9db951"`), []byte(`"base_commit": "short"`), 1)
	raw = bytes.Replace(raw, []byte(`"path": "internal/example/config.go"`), []byte(`"path": "../unsafe.go"`), 1)
	raw = bytes.Replace(raw, []byte(`"expected_occurrences": 1`), []byte(`"expected_occurrences": 0`), 1)
	result := Compile("compiler-fixture.execution-spec.json", raw)
	got := diagnosticKeys(result.Errors)
	want := []string{
		"/base_commit|invalid_commit_sha",
		"/steps/0/substeps/0/files/0/implementation/changes/0/expected_occurrences|invalid_expected_occurrences",
		"/steps/0/substeps/0/files/0/path|unsafe_repository_path",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected diagnostic order\ngot:  %v\nwant: %v", got, want)
	}
}

func TestPlanCrossFieldValidation(t *testing.T) {
	raw := readFixture(t, "valid.plan.json")
	raw = bytes.Replace(raw, []byte("\"repo_target\": \"relay\",\n      \"goal\": \"Integrate the foundation.\""), []byte("\"repo_target\": \"missing\",\n      \"goal\": \"Integrate the foundation.\""), 1)
	raw = bytes.Replace(raw, []byte("\"depends_on\": [\n        1\n      ]"), []byte("\"depends_on\": [\n        2,\n        3\n      ]"), 1)
	result := Compile("compiler-plan-fixture.plan.json", raw)
	for _, code := range []string{"unknown_repository_target", "self_dependency", "unknown_dependency"} {
		assertFailureCode(t, result, code)
	}
}

func TestUnresolvedTemplateMarkerBlocksRendering(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	raw = bytes.Replace(raw, []byte("const enabled = true\\n"), []byte("const enabled = {"+"{value}"+"}\\n"), 1)
	result := Compile("compiler-fixture.execution-spec.json", raw)
	assertFailureCode(t, result, "unresolved_template_marker")
}

func TestMissingValidationCommandBlocksRendering(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	raw = bytes.Replace(raw, []byte("\"commands\": [\n      {\n        \"command\": \"go test ./internal/speccompiler\",\n        \"expected\": \"The focused compiler tests pass.\"\n      }\n    ]"), []byte("\"commands\": []"), 1)
	result := Compile("compiler-fixture.execution-spec.json", raw)
	assertFailureCode(t, result, "missing_validation_command")
}

func TestDynamicFenceLength(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	raw = bytes.Replace(raw, []byte("const mode = `strict`\\n"), []byte("const mode = ```strict```\\n"), 1)
	result := Compile("compiler-fixture.execution-spec.json", raw)
	assertSuccess(t, result)
	if !strings.Contains(*result.Markdown, "````text") {
		t.Fatalf("expected four-backtick fence for content containing a three-backtick run")
	}
}
