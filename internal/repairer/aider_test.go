package repairer

import (
	"testing"

	"relay/internal/validation"
)

func TestCheckEligibility_eligible(t *testing.T) {
	tests := []struct {
		name   string
		report *validation.ValidationReport
	}{
		{
			name: "structural only",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: true,
				Errors: []validation.ValidationError{
					{Type: "structural", Message: "invalid JSON", RepairEligible: true},
				},
			},
		},
		{
			name: "schema only",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: true,
				Errors: []validation.ValidationError{
					{Type: "schema", Message: "missing required field", RepairEligible: true},
				},
			},
		},
		{
			name: "structural and schema",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: true,
				Errors: []validation.ValidationError{
					{Type: "structural", Message: "syntax error", RepairEligible: true},
					{Type: "schema", Message: "invalid type", RepairEligible: true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := CheckEligibility(tt.report)
			if !ok {
				t.Errorf("expected eligible, got ineligible: %s", reason)
			}
		})
	}
}

func TestCheckEligibility_ineligible(t *testing.T) {
	tests := []struct {
		name   string
		report *validation.ValidationReport
	}{
		{
			name: "security error",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "security", Message: "secret detected", RepairEligible: false},
				},
			},
		},
		{
			name: "path error",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "path", Message: "unsafe path", RepairEligible: false},
				},
			},
		},
		{
			name: "input error",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "input", Message: "missing field", RepairEligible: false},
				},
			},
		},
		{
			name: "mixed eligible and ineligible",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "structural", Message: "syntax", RepairEligible: true},
					{Type: "security", Message: "secret", RepairEligible: false},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, _ := CheckEligibility(tt.report)
			if ok {
				t.Errorf("expected ineligible for %q, got eligible", tt.name)
			}
		})
	}
}

func TestCheckEligibility_edgeCases(t *testing.T) {
	t.Run("nil report", func(t *testing.T) {
		ok, reason := CheckEligibility(nil)
		if ok {
			t.Error("expected ineligible for nil report")
		}
		if reason == "" {
			t.Error("expected non-empty reason for nil report")
		}
	})

	t.Run("empty errors", func(t *testing.T) {
		report := &validation.ValidationReport{
			Valid:          true,
			RepairEligible: true,
			Errors:         []validation.ValidationError{},
		}
		ok, reason := CheckEligibility(report)
		if ok {
			t.Error("expected ineligible for empty errors")
		}
		if reason == "" {
			t.Error("expected non-empty reason for empty errors")
		}
	})
}

func TestBuildRepairPrompt(t *testing.T) {
	packetJSON := []byte(`{"packet_meta":{"packet_id":"test-1"},"execution_payload":{"goal":"test"}}`)
	report := &validation.ValidationReport{
		Errors: []validation.ValidationError{
			{Type: "structural", Message: "invalid JSON syntax"},
		},
	}

	prompt := BuildRepairPrompt(packetJSON, report)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !contains(prompt, "invalid JSON syntax") {
		t.Error("expected prompt to contain error message")
	}
	if !contains(prompt, string(packetJSON)) {
		t.Error("expected prompt to contain packet JSON")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsString(s, substr)
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
