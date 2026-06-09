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

func TestValidateHandoffAgentFinalOutputRequirement(t *testing.T) {
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
` + "```" + `

## Agent final output requirement

Return only:

- DONE or BLOCKED
- build status
- test status
- count of LOC changed
`

	report := ValidateHandoff(text, "DeepSeek V4 Flash")

	var outputSectionFound bool
	for _, c := range report.Checks {
		if c.Kind == "output_section" {
			outputSectionFound = true
			if c.Status == "fail" {
				t.Fatalf("output section check should not fail for ## Agent final output requirement, got %q", c.Status)
			}
		}
	}
	if !outputSectionFound {
		t.Fatal("expected output_section check")
	}
}

func TestValidateHandoffAcceptsTestsValidationSection(t *testing.T) {
	text := `# Example Surgical Implementation

## Goal

Do a thing.

## Scope

- ` + "`internal/foo.go`" + `

## Do not change

- Unrelated behavior.

## Task checklist

- [ ] Update code

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Output

Return DONE or BLOCKED.
`

	report := ValidateHandoff(text, "DeepSeek V4 Flash")

	var validationSectionFail bool
	var validationCommandsPass bool
	for _, c := range report.Checks {
		if c.Kind == "validation_section" && c.Status == "fail" {
			validationSectionFail = true
		}
		if c.Kind == "validation_commands" && c.Status == "pass" {
			validationCommandsPass = true
		}
	}
	if validationSectionFail {
		t.Fatal("validation_section check should not fail for ## Tests / validation")
	}
	if !validationCommandsPass {
		t.Fatal("expected validation_commands to pass")
	}
	if len(report.Detected.ValidationCommands) != 1 {
		t.Fatalf("expected 1 validation command, got %d: %#v", len(report.Detected.ValidationCommands), report.Detected.ValidationCommands)
	}
	if report.Detected.ValidationCommands[0] != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", report.Detected.ValidationCommands[0])
	}
}

func TestValidateHandoffDoesNotDetectValidationProseAsCommand(t *testing.T) {
	text := `# Example Surgical Implementation

## Goal

Do a thing.

## Scope

- ` + "`internal/foo.go`" + `

## Do not change

- Unrelated behavior.

## Task checklist

- [ ] Update code

## Tests / validation

` + "```bash" + `
npm run build
` + "```" + `

If one command fails, fix it unless unrelated.

## Output

Return DONE or BLOCKED.
`

	report := ValidateHandoff(text, "DeepSeek V4 Flash")

	if len(report.Detected.ValidationCommands) != 1 {
		t.Fatalf("expected 1 validation command, got %d: %#v", len(report.Detected.ValidationCommands), report.Detected.ValidationCommands)
	}
	if report.Detected.ValidationCommands[0] != "npm run build" {
		t.Errorf("expected 'npm run build', got %q", report.Detected.ValidationCommands[0])
	}
	for _, cmd := range report.Detected.ValidationCommands {
		if len(cmd) >= 3 && cmd[:3] == "If " {
			t.Fatalf("prose line detected as command: %q", cmd)
		}
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
