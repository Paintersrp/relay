package pipeline

import "testing"

func TestValidateHandoffDoesNotTreatNonShellFenceAsValidationCommands(t *testing.T) {
	text := `# Example Surgical Implementation

## Goal

Do a thing.

## Scope

- ` + "`internal/foo.go`" + `

## Do not change

- Unrelated behavior.

## Task checklist

- [ ] Update code

## Validation

` + "```json" + `
{
  "not": "a command"
}
` + "```" + `

## Output

Return DONE or BLOCKED.
`

	report := ValidateHandoff(text, "DeepSeek V4 Flash")

	if len(report.Detected.ValidationCommands) != 0 {
		t.Fatalf("expected no validation commands, got %#v", report.Detected.ValidationCommands)
	}

	var found bool
	for _, check := range report.Checks {
		if check.Kind == "validation_commands" {
			found = true
			if check.Status != "warn" {
				t.Fatalf("expected validation_commands warn, got %q", check.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected validation_commands check")
	}
}

func TestValidateHandoffDetectsShellValidationCommands(t *testing.T) {
	text := `# Example Surgical Implementation

## Goal

Do a thing.

## Scope

- ` + "`internal/foo.go`" + `

## Do not change

- Unrelated behavior.

## Task checklist

- [ ] Update code

## Validation

` + "```bash" + `
go test ./...
npm run build
` + "```" + `

## Output

Return DONE or BLOCKED.
`

	report := ValidateHandoff(text, "DeepSeek V4 Flash")

	if report.Status != "ready" {
		t.Fatalf("expected ready, got %q with checks %#v", report.Status, report.Checks)
	}

	if len(report.Detected.ValidationCommands) != 2 {
		t.Fatalf("expected 2 validation commands, got %#v", report.Detected.ValidationCommands)
	}
}
