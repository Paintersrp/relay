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
