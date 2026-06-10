package pipeline

import (
	"fmt"
	"strings"
)

type IntakeRemediationInput struct {
	RunID       int64
	RepoName    string
	RepoPath    string
	BranchName  string
	RunStatus   string
	Warnings    []string
	Blockers    []string
	ScopedFiles []string
}

func BuildIntakeRemediationHandoff(input IntakeRemediationInput) string {
	var b strings.Builder

	b.WriteString("# Relay Intake Review Remediation Surgical Implementation\n\n")

	b.WriteString("## Goal\n\n")
	b.WriteString("Fix the original handoff so Relay Intake Review warnings/blockers are resolved without changing the intended implementation scope.\n\n")

	b.WriteString("## Context\n\n")
	b.WriteString(fmt.Sprintf("Run ID: %d\n", input.RunID))
	b.WriteString(fmt.Sprintf("Repo: %s\n", input.RepoName))
	b.WriteString(fmt.Sprintf("Repo path: %s\n", input.RepoPath))
	b.WriteString(fmt.Sprintf("Branch/worktree: %s\n", input.BranchName))
	b.WriteString(fmt.Sprintf("Run status: %s\n\n", input.RunStatus))

	b.WriteString("## Intake Review findings\n\n")

	b.WriteString("### Blockers\n\n")
	if len(input.Blockers) == 0 {
		b.WriteString("- None\n\n")
	} else {
		for _, bl := range input.Blockers {
			b.WriteString(fmt.Sprintf("- %s\n", bl))
		}
		b.WriteString("\n")
	}

	b.WriteString("### Warnings\n\n")
	if len(input.Warnings) == 0 {
		b.WriteString("- None\n\n")
	} else {
		for _, w := range input.Warnings {
			b.WriteString(fmt.Sprintf("- %s\n", w))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Scoped files detected\n\n")
	if len(input.ScopedFiles) == 0 {
		b.WriteString("- None detected\n\n")
	} else {
		for _, f := range input.ScopedFiles {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
		b.WriteString("\n")

	}

	b.WriteString("## Required fix\n\n")
	b.WriteString("Update the original handoff text so it satisfies Relay's intake requirements.\n\n")

	hasMissingValidationWarning := false
	for _, w := range input.Warnings {
		if strings.Contains(strings.ToLower(w), "no validation commands") {
			hasMissingValidationWarning = true
			break
		}
	}
	if hasMissingValidationWarning {
		b.WriteString("If the only warning is missing validation commands, add a `## Relay validation commands` section with appropriate commands for the target repo.\n\n")
		b.WriteString("Use this canonical format:\n\n")
		b.WriteString("## Relay validation commands\n\n")
		b.WriteString("```bash\n")
		b.WriteString("go fmt ./...\n")
		b.WriteString("templ generate\n")
		b.WriteString("npm run build\n")
		b.WriteString("go test ./...\n")
		b.WriteString("go vet ./...\n")
		b.WriteString("```\n\n")
		b.WriteString("Only include `npm run build` if the repo can run it in the target shell.\n\n")
	}

	b.WriteString("## Do not change\n\n")
	b.WriteString("- Do not change the intended implementation scope.\n")
	b.WriteString("- Do not add unrelated files.\n")
	b.WriteString("- Do not turn warnings into unrelated feature work.\n")
	b.WriteString("- Do not remove the original task goal.\n\n")

	b.WriteString("## Expected result\n\n")
	b.WriteString("The revised handoff can be pasted back into Relay and Step 1 Intake Review no longer reports the listed warnings/blockers.\n\n")

	b.WriteString("## Agent final output requirement\n\n")
	b.WriteString("Return only:\n\n")
	b.WriteString("- DONE or BLOCKED\n")
	b.WriteString("- build status\n")
	b.WriteString("- test status\n")
	b.WriteString("- count of LOC changed\n")
	b.WriteString("- blocker/error only if BLOCKED\n")

	return b.String()
}
