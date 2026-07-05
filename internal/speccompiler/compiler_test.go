package speccompiler

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompileExecutionSpecMatchesGolden(t *testing.T) {
	raw := readFixture(t, "valid.execution-spec.json")
	golden := string(readFixture(t, "compiler-fixture.executor-brief.md"))
	result := Compile("compiler-fixture.execution-spec.json", raw)
	assertSuccess(t, result)
	if result.OutputFilename == nil || *result.OutputFilename != "compiler-fixture.executor-brief.md" {
		t.Fatalf("unexpected output filename: %#v", result.OutputFilename)
	}
	if result.Markdown == nil || *result.Markdown != golden {
		t.Fatalf("rendered brief does not match golden\n--- got ---\n%s\n--- want ---\n%s", dereference(result.Markdown), golden)
	}
	assertOneFinalNewline(t, *result.Markdown)
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

func TestSchemaVersionFallbackIsNonblocking(t *testing.T) {
	result := Compile("fallback-plan.plan.json", readFixture(t, "fallback.plan.json"))
	assertSuccess(t, result)
	if len(result.Notices) != 1 || result.Notices[0].Code != "schema_version_fallback" {
		t.Fatalf("expected one fallback notice, got %+v", result.Notices)
	}
	if strings.Contains(dereference(result.Markdown), "schema_version_fallback") {
		t.Fatalf("fallback notice leaked into rendered Markdown")
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

func TestFilenameMustBeBasename(t *testing.T) {
	result := Compile("dir/compiler-fixture.plan.json", readFixture(t, "valid.plan.json"))
	assertFailureCode(t, result, "invalid_filename_basename")
}

func TestSourceProvenanceIsPinned(t *testing.T) {
	provenance := SourceProvenance()
	if provenance.Repository != "Paintersrp/relay-specs" || provenance.Commit != "1d6afbea47a246b3b473176c760aed53457774d6" {
		t.Fatalf("unexpected provenance: %+v", provenance)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
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
