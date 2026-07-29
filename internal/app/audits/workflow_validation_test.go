package audits

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeWorkflowExecutionEvidenceRejectsAmbiguousValidation(t *testing.T) {
	secret := "audit-validation-secret"
	t.Setenv("OPENAI_API_KEY", secret)
	base := map[string]any{
		"effective_brief_artifact_id": "artifact-brief",
		"effective_brief_sha256":      strings.Repeat("a", 64),
		"effective_brief_mode":        "full",
	}
	valid := func(status string) map[string]any {
		return map[string]any{"command": "go test ./...", "expected": "passes", "status": status, "concise_result": "result"}
	}
	tests := []struct {
		name    string
		results any
	}{
		{name: "null", results: nil},
		{name: "empty", results: []any{}},
		{name: "duplicate", results: []any{valid("passed"), valid("failed")}},
		{name: "unsupported status", results: []any{valid("success")}},
		{name: "invented exit code", results: []any{map[string]any{"command": "go test ./...", "expected": "passes", "status": "passed", "concise_result": "result", "exit_code": 0}}},
		{name: "oversized concise result", results: []any{map[string]any{"command": "go test ./...", "expected": "passes", "status": "failed", "concise_result": strings.Repeat("x", maxWorkflowAuditValidationConciseRunes+1)}}},
		{name: "unredacted concise result", results: []any{map[string]any{"command": "go test ./...", "expected": "passes", "status": "failed", "concise_result": secret}}},
		{name: "noncanonical command identity", results: []any{map[string]any{"command": " go test ./...", "expected": "passes", "status": "failed", "concise_result": "result"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{}
			for key, value := range base {
				payload[key] = value
			}
			payload["validation_results"] = tt.results
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeWorkflowExecutionEvidence(data); err == nil {
				t.Fatal("expected invalid structured evidence to be rejected")
			}
		})
	}
}
