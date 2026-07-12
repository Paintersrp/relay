package executor

import (
	"strings"
	"testing"

	"relay/internal/speccompiler"
)

func TestParseWorkflowValidationReportUsesFinalSectionAndCanonicalOrder(t *testing.T) {
	commands := []speccompiler.ProjectedValidationCommand{
		{Command: "go test ./internal/executor", Expected: "Executor tests pass."},
		{Command: "go test ./internal/app/audits", WorkingDirectory: "tools", Expected: "Audit tests pass."},
		{Command: "go run ./cmd/mcp-smoke", SuccessSignal: "Smoke passes."},
	}
	raw := strings.Join([]string{
		"## Validation",
		"- `go test ./internal/executor` - failed: earlier section is ignored",
		"## Notes",
		"Narrative text.",
		"## Validation",
		"- `go run ./cmd/mcp-smoke` - not run: environment unavailable",
		"- `focused check` - passed",
		"- `go test ./internal/app/audits` - failed: assertion mismatch",
		"- `go test ./internal/executor` - passed",
		"## Blockers or Incomplete Work",
		"- `go test ./internal/executor` - failed: outside section",
	}, "\n")

	result := parseWorkflowValidationReport(raw, commands)
	if len(result.Results) != 3 {
		t.Fatalf("results = %+v, diagnostics = %+v", result.Results, result.Diagnostics)
	}
	for index, command := range commands {
		if result.Results[index].Command != command.Command {
			t.Fatalf("result order = %+v", result.Results)
		}
	}
	if result.Results[0].Status != workflowValidationPassed || result.Results[0].ConciseResult != "Validation command passed." {
		t.Fatalf("passed result = %+v", result.Results[0])
	}
	if result.Results[1].Status != workflowValidationFailed || result.Results[1].WorkingDirectory != "tools" || result.Results[1].Expected != "Audit tests pass." {
		t.Fatalf("failed result = %+v", result.Results[1])
	}
	if result.Results[2].Status != workflowValidationNotRun || result.Results[2].Expected != "Smoke passes." {
		t.Fatalf("not-run result = %+v", result.Results[2])
	}
}

func TestParseWorkflowValidationReportRejectsMalformedDuplicateAndAlteredCommands(t *testing.T) {
	commands := []speccompiler.ProjectedValidationCommand{
		{Command: "go test ./internal/executor", Expected: "Executor tests pass."},
		{Command: "go test ./internal/app/audits", Expected: "Audit tests pass."},
		{Command: "go run ./cmd/mcp-smoke", Expected: "Smoke passes."},
	}
	raw := strings.Join([]string{
		"## Validation",
		"- `go test ./internal/executor` - passed",
		"- `go test ./internal/executor` - failed: contradictory duplicate",
		"- `go test ./internal/app/audits` - failed:",
		"- `go run  ./cmd/mcp-smoke` - passed",
	}, "\n")

	result := parseWorkflowValidationReport(raw, commands)
	if len(result.Results) != 0 {
		t.Fatalf("untrustworthy results were retained: %+v", result.Results)
	}
	if len(result.Diagnostics) < 3 {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestParseWorkflowValidationReportMissingSectionAndMissingCommandRemainUnavailable(t *testing.T) {
	commands := []speccompiler.ProjectedValidationCommand{
		{Command: "go test ./internal/executor", Expected: "Executor tests pass."},
		{Command: "go test ./internal/app/audits", Expected: "Audit tests pass."},
	}
	missingSection := parseWorkflowValidationReport("STATUS: DONE\n", commands)
	if len(missingSection.Results) != 0 || len(missingSection.Diagnostics) == 0 {
		t.Fatalf("missing section result = %+v", missingSection)
	}
	partial := parseWorkflowValidationReport("## Validation\n- `go test ./internal/executor` - passed\n", commands)
	if len(partial.Results) != 1 || partial.Results[0].Command != commands[0].Command {
		t.Fatalf("partial result = %+v", partial)
	}
}

func TestParseWorkflowValidationReportRedactsAndBoundsReasons(t *testing.T) {
	secret := "validation-report-secret"
	t.Setenv("OPENAI_API_KEY", secret)
	command := speccompiler.ProjectedValidationCommand{Command: "go test ./internal/executor", Expected: "Executor tests pass."}
	reason := secret + " " + strings.Repeat("x", maxWorkflowValidationConciseRunes+50)
	result := parseWorkflowValidationReport("## Validation\n- `go test ./internal/executor` - failed: "+reason+"\n", []speccompiler.ProjectedValidationCommand{command})
	if len(result.Results) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(result.Results[0].ConciseResult, "secret") || len([]rune(result.Results[0].ConciseResult)) > maxWorkflowValidationConciseRunes {
		t.Fatalf("concise result was not redacted and bounded: %q", result.Results[0].ConciseResult)
	}
}
