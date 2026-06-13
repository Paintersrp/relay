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
	if !strings.Contains(prompt, "Run relevant tests/checks during implementation when practical.") {
		t.Error("prompt should encourage running relevant tests/checks during implementation")
	}
	if !strings.Contains(prompt, "Relay validation is the authoritative final gate") {
		t.Error("prompt should say Relay validation is the authoritative final gate")
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
	if !strings.Contains(prompt, "Run relevant tests/checks during implementation when practical.") {
		t.Error("prompt should encourage running relevant tests/checks during implementation")
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

func TestBuildAgentPromptPreservesFencedMarkdownHandoffExample(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Goal

Fix setup.

## Surgical implementation details

### Task 5 — Add repo/scope mismatch auto-setup test

Example handoff:

` + "```md" + `
# Scope Mismatch Handoff

## Goal

Do a thing.

## Scope

- src/definitely-missing.ts

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
` + "```" + `

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return only DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	// The fenced markdown example should be preserved
	if !strings.Contains(prompt, "# Scope Mismatch Handoff") {
		t.Error("prompt should preserve # Scope Mismatch Handoff inside fenced markdown example")
	}
	if !strings.Contains(prompt, "src/definitely-missing.ts") {
		t.Error("prompt should preserve scoped file path inside fenced markdown example")
	}
	if !strings.Contains(prompt, "## Tests / validation") {
		t.Error("prompt should preserve ## Tests / validation heading (including inside fenced example)")
	}

	// The fenced markdown example's inner content should be preserved
	// Including the go test command (it's part of the example, not real validation)
	if !strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should preserve go test ./... inside the fenced markdown example")
	}

	// The fenced markdown example's headings should be preserved inside the example
	if !strings.Contains(prompt, "## Agent final output requirement") {
		t.Error("prompt should contain ## Agent final output requirement (from appended contract or example)")
	}

	// The prompt has one appended contract heading + one preserved inside the md example
	// (the real top-level one was stripped by stripSections)
	count := strings.Count(prompt, "## Agent final output requirement")
	if count < 2 {
		t.Errorf("expected at least 2 '## Agent final output requirement' (1 from md example + 1 appended), got %d", count)
	}
}

func TestBuildAgentPromptDoesNotStripHeadingsInsideCodeFence(t *testing.T) {
	handoff := `## Surgical implementation details

` + "```go" + `
// This string is part of a test fixture:
const text = "` + "`" + `
## Execution model
Use: DeepSeek V4 Flash

## Agent final output requirement
Return DONE.
` + "`" + `
` + "```" + `

## Agent final output requirement

Return only DONE or BLOCKED.
`
	prompt := BuildAgentPrompt(handoff)

	// Headings inside Go code fence should be preserved
	if !strings.Contains(prompt, "## Execution model") {
		t.Error("prompt should preserve ## Execution model inside Go code fence")
	}
	if !strings.Contains(prompt, "## Agent final output requirement") {
		t.Error("prompt should preserve ## Agent final output requirement inside Go code fence")
	}

	// There are 2 occurrences: 1 preserved inside the Go code fence + 1 appended contract.
	// The original top-level heading after the Go fence was stripped by stripSections.
	count := strings.Count(prompt, "## Agent final output requirement")
	if count != 2 {
		t.Errorf("expected exactly 2 '## Agent final output requirement' (1 inside Go fence + 1 appended), got %d", count)
	}
}

func TestBuildCompactAgentPromptPreservesFencedMarkdownExampleWithNestedBash(t *testing.T) {
	handoff := `# Example

## Surgical implementation details

Keep this fixture intact:

` + "```md" + `
# Nested Example

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE.
` + "```" + `

## Relay validation commands

` + "```bash" + `
go test ./...
` + "```" + `
`
	prompt := BuildCompactAgentPrompt(handoff)

	// The nested example heading inside the fenced markdown block must be preserved
	if !strings.Contains(prompt, "# Nested Example") {
		t.Error("prompt should preserve # Nested Example inside fenced markdown example")
	}

	// The Tests / validation heading inside the fenced markdown example must be preserved
	if !strings.Contains(prompt, "## Tests / validation") {
		t.Error("prompt should preserve ## Tests / validation heading inside fenced markdown example")
	}

	// The nested bash fence opener inside the md block must be preserved
	if !strings.Contains(prompt, "```bash") {
		t.Error("prompt should preserve nested bash fence opener inside fenced markdown example")
	}

	// The go test command inside the fenced markdown example must still be present
	if !strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should preserve go test ./... inside the fenced markdown example")
	}

	// The real top-level Relay validation commands heading must be removed
	if strings.Contains(prompt, "## Relay validation commands") {
		t.Error("prompt should NOT contain real ## Relay validation commands heading")
	}

	// The go test command should appear exactly once (inside the fenced md example)
	count := strings.Count(prompt, "go test ./...")
	if count != 1 {
		t.Errorf("expected 'go test ./...' to appear exactly 1 time, got %d", count)
	}
}

func TestBuildCompactAgentPromptIncludesValidationGuidance(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.
`
	prompt := BuildCompactAgentPrompt(handoff)

	if !strings.Contains(prompt, "Run relevant tests/checks during implementation when practical.") {
		t.Fatal("expected compact prompt to encourage running relevant tests/checks during implementation")
	}
	if !strings.Contains(prompt, "Relay validation is the authoritative final gate") {
		t.Fatal("expected compact prompt to state Relay validation is the authoritative final gate")
	}
}

func TestBuildAgentPromptStillRemovesRealValidationCommands(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.

## Tests / validation

` + "```bash" + `
go test ./...
npm run build
` + "```" + `

If RTK is available in the environment, Relay or the user may prefer ` + "`rtk.exe`" + ` first, then ` + "`rtk`" + `, then the raw command.

Do not list RTK-wrapped commands as separate validation commands.

## Expected result

- Tests pass.
`
	prompt := BuildAgentPrompt(handoff)

	// Real shell command block should be removed
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should not contain go test command")
	}
	if strings.Contains(prompt, "npm run build") {
		t.Error("prompt should not contain npm run build command")
	}

	// RTK wrapper text should be removed
	if strings.Contains(prompt, "If RTK is available") {
		t.Error("prompt should not contain RTK wrapper text")
	}
	if strings.Contains(prompt, "Do not list RTK-wrapped") {
		t.Error("prompt should not contain 'Do not list RTK-wrapped'")
	}

	// Relay validation removed note should be present
	if !strings.Contains(prompt, "Relay validation commands were extracted") {
		t.Error("prompt should contain Relay validation removed note")
	}
	if !strings.Contains(prompt, "Run relevant tests/checks during implementation when practical.") {
		t.Error("prompt should encourage running relevant tests/checks during implementation")
	}

	// Expected result should be preserved
	if !strings.Contains(prompt, "- Tests pass.") {
		t.Error("prompt should preserve expected result")
	}
}
