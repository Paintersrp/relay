package speccompiler

import (
	"strings"
	"testing"
)

const validDeterministicOperations = `{"schema_version":"1.0","feature_slug":"checkout","repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","coverage":"partial","operations":[{"path":"internal/old.go","operation":"modify","implementation":{"changes":[{"kind":"replace","old_text":"old","new_text":"new","expected_occurrences":1}]}},{"path":"internal/new.go","operation":"create","implementation":{"content":"package new\n"}},{"path":"internal/delete.go","operation":"delete","implementation":{"expected_content":"package delete\n"}},{"path":"internal/source.go","destination_path":"internal/target.go","operation":"rename","implementation":{"expected_content":"package source\n","preserve_content":true}}]}`

func TestDeterministicOperationsFilenameAndCompilation(t *testing.T) {
	result, document := CompileDeterministicOperations("checkout.ticket-P2-T2.r3.deterministic-operations.json", []byte(validDeterministicOperations))
	if len(result.Errors) != 0 || document == nil {
		t.Fatalf("errors=%v document=%+v", result.Errors, document)
	}
	if result.Markdown != nil || result.OutputFilename != nil {
		t.Fatalf("deterministic operations unexpectedly rendered: %+v", result)
	}
	if document.FeatureSlug != "checkout" || document.Coverage != "partial" || len(document.Operations) != 4 {
		t.Fatalf("document=%+v", document)
	}
	filename, diagnostics := ParseFilename("checkout.ticket-P2-T2.r3.deterministic-operations.json")
	if len(diagnostics) != 0 || filename.Kind != ArtifactDeterministicOperations || filename.TicketID != "P2-T2" || filename.Revision != 3 {
		t.Fatalf("filename=%+v diagnostics=%v", filename, diagnostics)
	}
}

func TestDeterministicOperationsRejectsRetiredExecutionSpecAndStructuralDefects(t *testing.T) {
	if _, diagnostics := ParseFilename("checkout.execution-spec.json"); len(diagnostics) == 0 {
		t.Fatal("retired Execution Spec filename was recognized")
	}
	tests := []struct {
		name string
		raw  string
		code string
	}{
		{"duplicate key", strings.Replace(validDeterministicOperations, "\"coverage\":\"partial\"", "\"coverage\":\"partial\",\"coverage\":\"complete\"", 1), "duplicate_object_key"},
		{"unsafe path", strings.Replace(validDeterministicOperations, "\"path\":\"internal/old.go\"", "\"path\":\"../old.go\"", 1), "unsafe_repository_path"},
		{"invalid coverage", strings.Replace(validDeterministicOperations, "\"coverage\":\"partial\"", "\"coverage\":\"full\"", 1), "invalid_coverage"},
		{"noncanonical top-level order", `{"feature_slug":"checkout","schema_version":"1.0","repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","coverage":"partial","operations":[]}`, "noncanonical_property_order"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, _ := CompileDeterministicOperations("checkout.ticket-P2-T2.r3.deterministic-operations.json", []byte(test.raw))
			for _, diagnostic := range result.Errors {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("errors=%v; missing %s", result.Errors, test.code)
		})
	}
}
