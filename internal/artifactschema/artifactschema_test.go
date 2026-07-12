package artifactschema

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDefinitionsMatchAcceptedAuthorityBytes(t *testing.T) {
	if AuthorityRepository != "Paintersrp/relay-specs" || AuthorityCommit != "7bd061c3ad989260345da5c5b2f42b3833561242" {
		t.Fatalf("authority = %s@%s", AuthorityRepository, AuthorityCommit)
	}
	want := []struct {
		kind    Kind
		version string
		path    string
		sha256  string
	}{
		{KindPlan, "1.0", "schemas/plan.schema.json", "03a75ab1352d27193ec27b5aec9f449e65daf69de66d6897ab74672bdc705cf8"},
		{KindExecutionSpec, "2.0", "schemas/execution-spec.schema.json", "92a1e8f1c2b9cc7bd4382f69f3d8bf8668c1ab72f2985c10fd746ba23a3df4d7"},
		{KindAuditPacket, "2.0", "schemas/audit-packet.schema.json", "91aaed33acca520d0ad2f511a472be7296993b37cb1dcd0b1976025b648850cf"},
	}
	got := Definitions()
	if len(got) != len(want) {
		t.Fatalf("definitions = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index].Kind != want[index].kind ||
			got[index].ProducerVersion != want[index].version ||
			got[index].AuthorityPath != want[index].path ||
			got[index].SHA256 != want[index].sha256 {
			t.Fatalf("definition %d = %+v, want %+v", index, got[index], want[index])
		}
		embedded, err := schemaFS.ReadFile("schemas/" + schemaFilename(want[index].kind))
		if err != nil {
			t.Fatal(err)
		}
		embedded = bytes.ReplaceAll(embedded, []byte("\r\n"), []byte("\n"))
		if !bytes.Equal(got[index].Bytes, embedded) {
			t.Fatalf("%s bytes differ from the accepted embedded schema", want[index].kind)
		}
		if _, err := prepareSchema(got[index]); err != nil {
			t.Fatalf("prepare %s schema: %v", want[index].kind, err)
		}
	}

	first, _ := Current(KindPlan)
	original := append([]byte(nil), first.Bytes...)
	first.Bytes[0] ^= 0xff
	second, _ := Current(KindPlan)
	if !bytes.Equal(second.Bytes, original) {
		t.Fatal("Current exposed mutable package-owned schema bytes")
	}
	if _, ok := Current(Kind("unknown")); ok {
		t.Fatal("unknown kind resolved")
	}
}

func TestCurrentSchemasTreatEveryVersionVariantAsMetadata(t *testing.T) {
	tests := []struct {
		name     string
		kind     Kind
		document map[string]any
		current  any
	}{
		{"plan", KindPlan, validPlanDocument(), "1.0"},
		{"execution_spec", KindExecutionSpec, validExecutionDocument(), "2.0"},
	}
	variants := []struct {
		name   string
		value  any
		absent bool
	}{
		{"current", nil, false},
		{"absent", nil, true},
		{"null", nil, false},
		{"boolean", true, false},
		{"number", 7, false},
		{"object", map[string]any{"version": "2.0"}, false},
		{"array", []any{"2.0"}, false},
		{"malformed_string", "not-a-version", false},
		{"stale", "0.1", false},
		{"unsupported", "3.7", false},
		{"future", "999.0", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, variant := range variants {
				t.Run(variant.name, func(t *testing.T) {
					clone := cloneDocument(t, test.document)
					if variant.absent {
						delete(clone, "schema_version")
					} else if variant.name == "current" {
						clone["schema_version"] = test.current
					} else {
						clone["schema_version"] = variant.value
					}
					raw, err := json.Marshal(clone)
					if err != nil {
						t.Fatal(err)
					}
					valid, err := Validate(test.kind, raw)
					if err != nil || !valid {
						t.Fatalf("valid=%v err=%v raw=%s", valid, err, raw)
					}
				})
			}
		})
	}
}

func TestCurrentSchemasPreserveLexicalPatternEnforcement(t *testing.T) {
	tests := []struct {
		name     string
		kind     Kind
		document map[string]any
		mutate   func(map[string]any)
	}{
		{
			name: "plan_feature_slug",
			kind: KindPlan, document: validPlanDocument(),
			mutate: func(document map[string]any) { document["feature_slug"] = "Invalid Slug" },
		},
		{
			name: "plan_branch_outer_whitespace",
			kind: KindPlan, document: validPlanDocument(),
			mutate: func(document map[string]any) {
				document["repo_targets"].([]any)[0].(map[string]any)["branch"] = "\u00a0main"
			},
		},
		{
			name: "plan_unsafe_source_path",
			kind: KindPlan, document: validPlanDocument(),
			mutate: func(document map[string]any) {
				document["passes"].([]any)[0].(map[string]any)["source_targets"].([]any)[0].(map[string]any)["path"] = "../unsafe.go"
			},
		},
		{
			name: "execution_short_commit",
			kind: KindExecutionSpec, document: validExecutionDocument(),
			mutate: func(document map[string]any) { document["base_commit"] = "short" },
		},
		{
			name: "execution_blank_prose",
			kind: KindExecutionSpec, document: validExecutionDocument(),
			mutate: func(document map[string]any) { document["goal"] = "\u00a0" },
		},
		{
			name: "execution_unsafe_file_path",
			kind: KindExecutionSpec, document: validExecutionDocument(),
			mutate: func(document map[string]any) {
				document["steps"].([]any)[0].(map[string]any)["substeps"].([]any)[0].(map[string]any)["files"].([]any)[0].(map[string]any)["path"] = "internal/../unsafe.go"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := cloneDocument(t, test.document)
			test.mutate(document)
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			valid, err := Validate(test.kind, raw)
			if err != nil {
				t.Fatal(err)
			}
			if valid {
				t.Fatalf("lexically invalid document passed current %s schema: %s", test.kind, raw)
			}
		})
	}
}

func TestUnknownAuthoritativePatternFailsClosed(t *testing.T) {
	_, err := portablePatternConstraints(`^(?=unregistered)`)
	if err == nil || !strings.Contains(err.Error(), "unsupported authoritative schema pattern") {
		t.Fatalf("error = %v", err)
	}
}

func validPlanDocument() map[string]any {
	return map[string]any{
		"schema_version": "1.0",
		"feature_slug":   "schema-test",
		"goal":           "Test the current Plan schema.",
		"context":        "Schema validation test.",
		"scope": map[string]any{
			"in_scope": []any{"Validate current content."}, "out_of_scope": []any{"No mutation."},
		},
		"repo_targets": []any{map[string]any{
			"repo_target": "relay", "branch": "main", "planning_base_commit": strings.Repeat("a", 40),
		}},
		"passes": []any{map[string]any{
			"number": 1, "name": "Validate", "repo_target": "relay", "goal": "Validate.",
			"context": "Validate current content.",
			"scope": map[string]any{
				"in_scope": []any{"Validate."}, "out_of_scope": []any{"No mutation."},
			},
			"depends_on": []any{}, "outcomes": []any{"Validated."},
			"source_targets":    []any{map[string]any{"path": "internal/example.go", "purpose": "Validate."}},
			"validation_intent": []any{"Prove validity."}, "completion_criteria": []any{"Valid."},
		}},
		"completion_criteria": []any{"Complete."},
	}
}

func validExecutionDocument() map[string]any {
	return map[string]any{
		"schema_version": "2.0", "feature_slug": "schema-test", "repo_target": "relay",
		"branch": "main", "base_commit": strings.Repeat("a", 40),
		"goal": "Test the current Execution Spec schema.", "context": "Schema validation test.",
		"scope": map[string]any{
			"in_scope": []any{"Validate."}, "out_of_scope": []any{"No mutation."},
		},
		"steps": []any{map[string]any{
			"number": 1, "goal": "Validate.",
			"substeps": []any{map[string]any{
				"number": 1, "instruction": "Create the fixture file.",
				"files": []any{map[string]any{
					"path": "internal/example.go", "operation": "create", "purpose": "Validate schema handling.",
					"implementation": map[string]any{"content": "package example\n"},
				}},
				"completion_criteria": []any{"Created."},
			}},
			"completion_criteria": []any{"Complete."},
		}},
		"validation": map[string]any{"commands": []any{map[string]any{
			"command": "go test ./internal/example", "expected": "Tests pass.",
		}}},
		"completion_criteria": []any{"Complete."},
	}
}

func cloneDocument(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
