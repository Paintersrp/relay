package pipeline

import (
	"strings"
	"testing"
)

func TestBuildAgentPromptRemovesExecutionModel(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Execution model

Use: DeepSeek V4 Flash
Reason: It is fast.

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	if strings.Contains(prompt, "Use: DeepSeek") {
		t.Error("prompt should not contain execution model details")
	}
	if !strings.Contains(prompt, "Validation responsibility") {
		t.Error("prompt should contain Validation responsibility")
	}
	if !strings.Contains(prompt, "Relay will run validation") {
		t.Error("prompt should state Relay will run validation")
	}
	// Section heading preserved, commands removed
	if !strings.Contains(prompt, "## Tests / validation") {
		t.Error("prompt should preserve Tests / validation heading")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should not contain raw go test command")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
}

func TestBuildAgentPromptRemovesRawValidationCommands(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Execution model

Use: DeepSeek V4 Flash

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
npm run build
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	if strings.Contains(prompt, "```bash") {
		t.Error("prompt should not contain fenced validation command blocks")
	}
	if !strings.Contains(prompt, "Relay will run validation") {
		t.Error("prompt should state Relay will run validation")
	}
	if !strings.Contains(prompt, "Do not run validation commands") {
		t.Error("prompt should tell agent not to run validation")
	}
	// Section heading preserved
	if !strings.Contains(prompt, "## Tests / validation") {
		t.Error("prompt should preserve Tests / validation heading")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should not contain go test command")
	}
	if strings.Contains(prompt, "npm run build") {
		t.Error("prompt should not contain npm run build command")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
}

func TestBuildAgentPromptPreservesImplementationDetails(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Execution model

Use: DeepSeek V4 Flash

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Update code in specific way

## Direct files likely changed

- internal/handlers/foo.go

## Surgical implementation details

Update the handler to return a 200 status.

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	if !strings.Contains(prompt, "Do a thing") {
		t.Error("prompt should preserve Goal")
	}
	if !strings.Contains(prompt, "internal/handlers/foo.go") {
		t.Error("prompt should preserve file paths")
	}
	if !strings.Contains(prompt, "Surgical implementation details") {
		t.Error("prompt should preserve implementation details section")
	}
}

func TestBuildAgentPromptFinalOutputContract(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Execution model

Use: DeepSeek V4 Flash

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	count := strings.Count(prompt, "## Agent final output requirement")
	if count != 1 {
		t.Errorf("expected exactly 1 '## Agent final output requirement', got %d", count)
	}
	if !strings.Contains(prompt, "DONE or BLOCKED") {
		t.Error("prompt should contain DONE or BLOCKED")
	}
	if !strings.Contains(prompt, "count of LOC changed") {
		t.Error("prompt should contain LOC changed requirement")
	}
}

func TestBuildAgentPromptContainsTitle(t *testing.T) {
	handoff := `# My Awesome Handoff

## Execution model

Use: DeepSeek V4 Flash

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Output

Return DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	if !strings.Contains(prompt, "My Awesome Handoff Agent Execution Prompt") {
		t.Error("prompt title should include original handoff title")
	}
	if !strings.Contains(prompt, "## Tests / validation") {
		t.Error("prompt should preserve Tests / validation heading")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should not contain go test command")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
}

func TestBuildAgentPromptPreservesTestImplementationInstructions(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Goal

Fix parser.

## Tests / validation

Add or update focused tests:

- Test parser keeps README.md
- Test parser ignores rtk.exe outside scope sections

` + "```bash" + `
go test ./...
npm run build
` + "```" + `

## Agent final output requirement

Return only DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	if !strings.Contains(prompt, "Test parser keeps README.md") {
		t.Error("prompt should preserve test instruction: Test parser keeps README.md")
	}
	if !strings.Contains(prompt, "Test parser ignores rtk.exe outside scope sections") {
		t.Error("prompt should preserve test instruction: Test parser ignores rtk.exe outside scope sections")
	}
	if !strings.Contains(prompt, "Add or update focused tests:") {
		t.Error("prompt should preserve prose in test section")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should not contain go test command")
	}
	if strings.Contains(prompt, "npm run build") {
		t.Error("prompt should not contain npm run build command")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
	if !strings.Contains(prompt, "## Agent final output requirement") {
		t.Error("prompt should contain final output contract")
	}
}

func TestBuildAgentPromptCleansRelayValidationCommandsSection(t *testing.T) {
	handoff := `## Relay validation commands

` + "```bash" + `
go fmt ./...
go test ./...
` + "```" + `
`
	prompt := BuildAgentPrompt(handoff)

	if strings.Contains(prompt, "go fmt ./...") {
		t.Error("prompt should not contain go fmt command")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should not contain go test command")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
	// Should not leave an empty orphaned shell fence
	if strings.Contains(prompt, "```") {
		t.Error("prompt should not contain orphaned shell fence markers")
	}
}

func TestBuildAgentPromptPreservesTestsToAddOrUpdateSection(t *testing.T) {
	handoff := `## Tests to add or update

- Add parser test
- Add UI test

` + "```bash" + `
go test ./...
` + "```" + `
`
	prompt := BuildAgentPrompt(handoff)

	if !strings.Contains(prompt, "Add parser test") {
		t.Error("prompt should preserve 'Add parser test'")
	}
	if !strings.Contains(prompt, "Add UI test") {
		t.Error("prompt should preserve 'Add UI test'")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should remove go test command")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
}

func TestBuildAgentPromptDoesNotStripTestsSection(t *testing.T) {
	handoff := `## Tests

- Add test A
`
	prompt := BuildAgentPrompt(handoff)

	if !strings.Contains(prompt, "## Tests") {
		t.Error("prompt should preserve ## Tests heading")
	}
	if !strings.Contains(prompt, "Add test A") {
		t.Error("prompt should preserve test instruction 'Add test A'")
	}
}

func TestBuildAgentPromptDoesNotDuplicateFinalOutputContract(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	count := strings.Count(prompt, "## Agent final output requirement")
	if count != 1 {
		t.Errorf("expected exactly 1 '## Agent final output requirement', got %d", count)
	}
}

func TestBuildAgentPromptPreservesNonCommandTestProseInValidationSection(t *testing.T) {
	handoff := `## Validation

Ensure false positives remain excluded.
Mock repo path with TempDir.
Expected: report status is ready.

` + "```bash" + `
go test ./...
` + "```" + `
`
	prompt := BuildAgentPrompt(handoff)

	if !strings.Contains(prompt, "Ensure false positives remain excluded") {
		t.Error("prompt should preserve test prose")
	}
	if !strings.Contains(prompt, "Mock repo path with TempDir") {
		t.Error("prompt should preserve test prose")
	}
	if !strings.Contains(prompt, "Expected: report status is ready") {
		t.Error("prompt should preserve test prose")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should remove go test command")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
}

func TestBuildAgentPromptRemovesValidationWrapperText(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.

## Tests / validation

Validation commands:

` + "```bash" + `
npm run typecheck
npm test
` + "```" + `

If RTK is available in the environment, Relay or the user may prefer ` + "`rtk.exe`" + ` first, then ` + "`rtk`" + `, then the raw command.

Do not list RTK-wrapped commands as separate validation commands.

## Expected result

- Typecheck and tests pass.
`
	prompt := BuildAgentPrompt(handoff)

	validationSection := "## Tests / validation"
	extracted := prompt[strings.Index(prompt, validationSection):]
	if strings.Index(extracted, "\n## ") > 0 {
		extracted = extracted[:strings.Index(extracted, "\n## ")]
	}

	if strings.Contains(prompt, "Validation commands:") {
		t.Error("prompt should not contain 'Validation commands:' label")
	}
	if strings.Contains(prompt, "If RTK is available") {
		t.Error("prompt should not contain 'If RTK is available' wrapper text")
	}
	if strings.Contains(prompt, "Do not list RTK-wrapped") {
		t.Error("prompt should not contain 'Do not list RTK-wrapped' wrapper text")
	}
	if strings.Contains(prompt, "npm run typecheck") {
		t.Error("prompt should not contain raw command 'npm run typecheck'")
	}
	if strings.Contains(prompt, "npm test") {
		t.Error("prompt should not contain raw command 'npm test'")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
	if !strings.Contains(prompt, "- Typecheck and tests pass") {
		t.Error("prompt should preserve expected result line: '- Typecheck and tests pass'")
	}
}

func TestBuildAgentPromptKeepsExpectedResultValidationOutcome(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Expected result

Typecheck, tests, production build, and local build pass.
`
	prompt := BuildAgentPrompt(handoff)
	if !strings.Contains(prompt, "Typecheck, tests, production build, and local build pass.") {
		t.Error("prompt should preserve expected result validation outcome")
	}
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
}

func TestBuildAgentPromptNoRelayNoteWhenNoCommandsRemoved(t *testing.T) {
	handoff := `## Tests

- Add test A
- Add test B
`
	prompt := BuildAgentPrompt(handoff)

	if strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should NOT contain Relay validation removed note when no commands were removed")
	}
	if !strings.Contains(prompt, "Add test A") {
		t.Error("prompt should preserve test instruction")
	}
}
