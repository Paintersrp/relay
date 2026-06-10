package pipeline

import (
	"strings"
	"testing"
)

func TestExtractFencedBashCommandsUnderValidationHeading(t *testing.T) {
	handoff := `# Test

## Goal

Do something.

## Scope

- foo.go

## Do not change

Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
go vet ./...
` + "```" + `

## Output

DONE or BLOCKED
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", cmds[0].Command)
	}
	if cmds[1].Command != "go vet ./..." {
		t.Errorf("expected 'go vet ./...', got %q", cmds[1].Command)
	}
	if cmds[0].Source != "handoff" {
		t.Errorf("expected source handoff, got %q", cmds[0].Source)
	}
}

func TestExtractsCommandLinesUnderValidationHeading(t *testing.T) {
	handoff := `# Test

## Tests / validation

Run:

go test ./...
npm run build
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", cmds[0].Command)
	}
	if cmds[1].Command != "npm run build" {
		t.Errorf("expected 'npm run build', got %q", cmds[1].Command)
	}
}

func TestIgnoresCommandsOutsideValidationSection(t *testing.T) {
	handoff := `## Goal

Run go test ./... eventually.
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(cmds))
	}
}

func TestFallsBackToRepoDefaultStringJSON(t *testing.T) {
	handoff := "no commands"
	repoDefaults := `["go test ./...", "go vet ./..."]`
	cmds := ExtractValidationCommands(handoff, repoDefaults)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Source != "repo_default" {
		t.Errorf("expected source repo_default, got %q", cmds[0].Source)
	}
	if cmds[1].Command != "go vet ./..." {
		t.Errorf("expected 'go vet ./...', got %q", cmds[1].Command)
	}
}

func TestFallsBackToRepoDefaultObjectJSON(t *testing.T) {
	handoff := "no commands"
	repoDefaults := `[{"label":"Tests","command":"go test ./..."}]`
	cmds := ExtractValidationCommands(handoff, repoDefaults)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Label != "Tests" {
		t.Errorf("expected label 'Tests', got %q", cmds[0].Label)
	}
	if cmds[0].Command != "go test ./..." {
		t.Errorf("expected command 'go test ./...', got %q", cmds[0].Command)
	}
}

func TestRejectsDestructiveChainedAgentCommands(t *testing.T) {
	handoff := `## Tests / validation

rm -rf data
go test ./... && go vet ./...
opencode run something
go test ./...
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", cmds[0].Command)
	}
}

func TestHandoffWinsOverRepoDefaults(t *testing.T) {
	handoff := `## Tests / validation

templ generate
`
	repoDefaults := `["go test ./..."]`
	cmds := ExtractValidationCommands(handoff, repoDefaults)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Command != "templ generate" {
		t.Errorf("expected 'templ generate', got %q", cmds[0].Command)
	}
	if cmds[0].Source != "handoff" {
		t.Errorf("expected source handoff, got %q", cmds[0].Source)
	}
}

func TestEmptyHandoffAndEmptyDefaults(t *testing.T) {
	cmds := ExtractValidationCommands("", "")
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(cmds))
	}
}

func TestExtractsCommandsFromValidationSectionOnly(t *testing.T) {
	handoff := `## Goal

go build ./...

## Tests / validation

go test ./...
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", cmds[0].Command)
	}
}

func TestDoesNotExtractValidationProseAfterFence(t *testing.T) {
	handoff := `# Test

## Tests / validation

` + "```bash" + `
npm run typecheck
npm test
npm run build
` + "```" + `

If one command fails, fix it unless the failure is clearly unrelated and pre-existing.

If blocked, report the exact command and exact error.
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d: %#v", len(cmds), cmds)
	}
	for _, cmd := range cmds {
		if strings.HasPrefix(cmd.Command, "If ") {
			t.Fatalf("prose line extracted as command: %q", cmd.Command)
		}
	}
}

func TestExtractsKnownBareCommandPrefixesOnly(t *testing.T) {
	handoff := `## Tests / validation

Run:
npm run build
If one command fails, fix it.
go test ./...
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "npm run build" {
		t.Errorf("expected 'npm run build', got %q", cmds[0].Command)
	}
	if cmds[1].Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", cmds[1].Command)
	}
}

func TestIgnoresNonShellFenceInValidationSection(t *testing.T) {
	handoff := `## Tests / validation

` + "```json" + `
{"not": "a command"}
` + "```" + `
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(cmds))
	}
}

func TestExtractValidationCommandsRelayValidationCommandsSection(t *testing.T) {
	handoff := `# Relay Guided Workbench Smoke Test Surgical Implementation

## Execution model

Use: DeepSeek V4 Flash

## Goal

Run a smoke test.

## Scope

- README.md

## Do not change

- Do not change unrelated files.

## Task checklist

- [ ] Inspect README.md
- [ ] Make the tiny smoke change if missing

## Relay validation commands

` + "```bash" + `
go fmt ./...
templ generate
npm run build
go test ./...
go vet ./...
` + "```" + `

## Agent final output requirement

Return only:

- DONE or BLOCKED
- Build status: <status>
- Test status: <status>
- Count of LOC changed: <count>
`
	cmds := ExtractValidationCommands(handoff, "[]")
	expected := []string{
		"go fmt ./...",
		"templ generate",
		"npm run build",
		"go test ./...",
		"go vet ./...",
	}
	if len(cmds) != len(expected) {
		t.Fatalf("expected %d commands, got %d: %#v", len(expected), len(cmds), cmds)
	}
	for i, cmd := range cmds {
		if cmd.Command != expected[i] {
			t.Errorf("command %d: expected %q, got %q", i, expected[i], cmd.Command)
		}
		if cmd.Source != "handoff" {
			t.Errorf("command %d: expected source handoff, got %q", i, cmd.Source)
		}
	}
}

func TestExtractValidationCommandsValidationCommandsSection(t *testing.T) {
	handoff := `# Test

## Goal

Do something.

## Validation commands

` + "```bash" + `
go test ./...
go vet ./...
` + "```" + `

## Agent final output

DONE or BLOCKED
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", cmds[0].Command)
	}
	if cmds[1].Command != "go vet ./..." {
		t.Errorf("expected 'go vet ./...', got %q", cmds[1].Command)
	}
}

func TestExtractValidationCommandsH3RelayValidationCommands(t *testing.T) {
	handoff := `# Test

### Relay validation commands

` + "```bash" + `
go fmt ./...
` + "```" + `

### Some other subsection

This should not be extracted.
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "go fmt ./..." {
		t.Errorf("expected 'go fmt ./...', got %q", cmds[0].Command)
	}
}

func TestFinalOutputBulletsNotTreatedAsValidationCommands(t *testing.T) {
	handoff := `## Relay validation commands

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return only:

- DONE or BLOCKED
- Build status: <status>
- Test status: <status>
- Count of LOC changed: <count>
`
	cmds := ExtractValidationCommands(handoff, "[]")
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command from relay validation section, got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", cmds[0].Command)
	}
}

func TestExtractValidationCommandsNormalizesRTKDuplicates(t *testing.T) {
	handoff := `## Tests / validation

` + "```bash" + `
go fmt ./...
npm run build
go test ./...
` + "```" + `

If RTK is available, prefer:

` + "```bash" + `
rtk.exe go fmt ./...
rtk.exe test "npm run build"
rtk.exe go test ./...
` + "```" + `
`
	cmds := ExtractValidationCommands(handoff, "[]")

	expected := []string{
		"go fmt ./...",
		"npm run build",
		"go test ./...",
	}

	if len(cmds) != len(expected) {
		t.Fatalf("expected %d canonical commands, got %d: %#v", len(expected), len(cmds), cmds)
	}

	for i, cmd := range cmds {
		if cmd.Command != expected[i] {
			t.Errorf("command %d: expected %q, got %q", i, expected[i], cmd.Command)
		}
	}

	for _, cmd := range cmds {
		if strings.HasPrefix(cmd.Command, "rtk") {
			t.Errorf("RTK-wrapped command should not appear after normalization: %q", cmd.Command)
		}
	}
}
