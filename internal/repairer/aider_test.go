package repairer

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"relay/internal/store"
	"relay/internal/validation"
)

type FakeAdapter struct {
	Calls  int
	Stdout string
	Stderr string
	Err    error
}

func (f *FakeAdapter) Repair(ctx context.Context, prompt string) (string, string, error) {
	f.Calls++
	return f.Stdout, f.Stderr, f.Err
}

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
					{Type: "structural", Code: validation.CodeJSONSyntax, Message: "invalid JSON", RepairEligible: true},
				},
			},
		},
		{
			name: "schema only",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: true,
				Errors: []validation.ValidationError{
					{Type: "schema", Code: validation.CodeMissingRequiredField, Message: "missing required field", RepairEligible: true},
				},
			},
		},
		{
			name: "structural and schema",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: true,
				Errors: []validation.ValidationError{
					{Type: "structural", Code: validation.CodeJSONSyntax, Message: "syntax error", RepairEligible: true},
					{Type: "schema", Code: validation.CodeInvalidType, Message: "invalid type", RepairEligible: true},
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
					{Type: "security", Code: "CANONICAL_PACKET_SECURITY", Message: "secret detected", RepairEligible: false},
				},
			},
		},
		{
			name: "path error",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "path", Code: "CANONICAL_PACKET_UNSAFE_PATH", Message: "unsafe path", RepairEligible: false},
				},
			},
		},
		{
			name: "input error",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "input", Code: "CANONICAL_PACKET_MISSING_PAYLOAD", Message: "missing field", RepairEligible: false},
				},
			},
		},
		{
			name: "mixed eligible and ineligible",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "structural", Code: validation.CodeJSONSyntax, Message: "syntax", RepairEligible: true},
					{Type: "security", Code: "CANONICAL_PACKET_SECURITY", Message: "secret", RepairEligible: false},
				},
			},
		},
		{
			name: "file target mismatch",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: false,
				Errors: []validation.ValidationError{
					{Type: "input", Code: validation.CodeFileTargetMismatch, Message: "mismatch", RepairEligible: false},
				},
			},
		},
		{
			name: "missing code",
			report: &validation.ValidationReport{
				Valid:          false,
				RepairEligible: true,
				Errors: []validation.ValidationError{
					{Type: "structural", Message: "syntax", RepairEligible: true},
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
	packetJSON := []byte(`{"packet_meta":{"packet_id":"test-1"},"execution_payload":{"goal":"test","token":"Bearer 12345"}}`)
	report := &validation.ValidationReport{
		Errors: []validation.ValidationError{
			{Type: "structural", Code: validation.CodeJSONSyntax, Message: "invalid JSON syntax"},
		},
	}

	prompt := BuildRepairPrompt(packetJSON, report)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !contains(prompt, "invalid JSON syntax") {
		t.Error("expected prompt to contain error message")
	}
	if contains(prompt, "Bearer 12345") {
		t.Error("expected prompt to be redacted")
	}
	if !contains(prompt, "[REDACTED]") {
		t.Error("expected prompt to contain [REDACTED]")
	}
}

func TestService_RepairValidation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	repo, _ := s.CreateRepo("test-repo", dir)
	run, _ := s.CreateRun(repo.ID, "test-run", "packet_validation_failed", "gpt-4o", "gpt-4o", "main")

	t.Run("eligible one-call behavior, valid output", func(t *testing.T) {
		validBytes, err := os.ReadFile("../../relay-contracts/examples/canonical_packet.valid.example.json")
		if err != nil {
			validBytes, err = os.ReadFile("../../../relay-contracts/examples/canonical_packet.valid.example.json")
			if err != nil {
				t.Fatalf("failed to read valid packet for test: %v", err)
			}
		}

		fakeAdapter := &FakeAdapter{
			Stdout: string(validBytes),
		}
		svc := NewServiceWithAdapter(s, fakeAdapter)

		report := &validation.ValidationReport{
			Valid:          false,
			RepairEligible: true,
			Errors: []validation.ValidationError{
				{Type: "structural", Code: validation.CodeJSONSyntax, Message: "syntax", RepairEligible: true},
			},
		}

		packetJSON := []byte(`{"packet_meta": {}}`)
		res := svc.RepairValidation(run.ID, packetJSON, report)

		if !res.Eligible {
			t.Errorf("expected eligible")
		}
		if !res.RepairAttempted {
			t.Errorf("expected repair attempted")
		}
		if res.BlockedReason != "" {
			t.Errorf("expected no blocked reason")
		}
		if res.Success != true {
			t.Errorf("expected success, got error: %s", res.Error)
		}
		if fakeAdapter.Calls != 1 {
			t.Errorf("expected 1 call, got %d", fakeAdapter.Calls)
		}
		if len(res.RepairArtifacts) == 0 {
			t.Errorf("expected repair artifacts, got none")
		}
		if res.RepairArtifacts[ArtifactKindRepairPrompt] == "" {
			t.Errorf("expected repair prompt artifact path")
		}
		
		runStatus, _ := s.GetRun(run.ID)
		if runStatus.Status != "repair_validated" {
			t.Errorf("expected repair_validated, got %s", runStatus.Status)
		}
	})
    
	t.Run("ineligible zero-call behavior", func(t *testing.T) {
		fakeAdapter := &FakeAdapter{}
		svc := NewServiceWithAdapter(s, fakeAdapter)

		report := &validation.ValidationReport{
			Valid:          false,
			RepairEligible: false,
			Errors: []validation.ValidationError{
				{Type: "security", Code: "CANONICAL_PACKET_SECURITY", Message: "secret", RepairEligible: false},
			},
		}

		res := svc.RepairValidation(run.ID, []byte("{}"), report)

		if res.Eligible {
			t.Errorf("expected ineligible")
		}
		if fakeAdapter.Calls != 0 {
			t.Errorf("expected 0 calls")
		}
	})

	t.Run("missing command blocked/no-attempt behavior", func(t *testing.T) {
		svc := NewService(s) // Default adapter with no ENV set

		report := &validation.ValidationReport{
			Valid:          false,
			RepairEligible: true,
			Errors: []validation.ValidationError{
				{Type: "structural", Code: validation.CodeJSONSyntax, Message: "syntax", RepairEligible: true},
			},
		}

		res := svc.RepairValidation(run.ID, []byte("{}"), report)

		if !res.Eligible {
			t.Errorf("expected eligible")
		}
		if res.RepairAttempted {
			t.Errorf("expected no attempt")
		}
		if res.BlockedReason == "" {
			t.Errorf("expected blocked reason")
		}
	})

	t.Run("invalid output status preservation", func(t *testing.T) {
		fakeAdapter := &FakeAdapter{
			Stdout: `{"invalid": true}`,
		}
		svc := NewServiceWithAdapter(s, fakeAdapter)

		report := &validation.ValidationReport{
			Valid:          false,
			RepairEligible: true,
			Errors: []validation.ValidationError{
				{Type: "structural", Code: validation.CodeJSONSyntax, Message: "syntax", RepairEligible: true},
			},
		}

		// Reset run status before test
		_, _ = s.UpdateRunStatus(run.ID, "packet_validation_failed")

		res := svc.RepairValidation(run.ID, []byte("{}"), report)

		if res.Success {
			t.Errorf("expected failure")
		}
		if res.ReValidationValid == nil || *res.ReValidationValid != false {
			t.Errorf("expected revalidation invalid")
		}

		runStatus, _ := s.GetRun(run.ID)
		if runStatus.Status == "repair_validated" {
			t.Errorf("expected status to not advance")
		}
	})
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
