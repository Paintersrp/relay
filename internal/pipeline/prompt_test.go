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
}
