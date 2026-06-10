package pipeline

import (
	"fmt"
	"strings"
)

type AuditHandoffInput struct {
	RunID      int64
	Title      string
	RepoName   string
	BranchName string
	Status     string

	OriginalHandoff   string
	AgentResultStatus string
	BuildStatus       string
	TestStatus        string
	LOCChanged        string
	ResultRaw         string

	ValidationStatus   string
	ValidationRepoPath string
	ValidationCommands []CommandRunResult
}

func BuildAuditHandoff(input AuditHandoffInput) string {
	var b strings.Builder

	b.WriteString("# Relay Run Audit Handoff\n\n")

	b.WriteString("## Run\n\n")
	b.WriteString(fmt.Sprintf("- Run ID: %d\n", input.RunID))
	b.WriteString(fmt.Sprintf("- Title: %s\n", input.Title))
	b.WriteString(fmt.Sprintf("- Repo: %s\n", input.RepoName))
	b.WriteString(fmt.Sprintf("- Branch: %s\n", input.BranchName))
	b.WriteString(fmt.Sprintf("- Status: %s\n", input.Status))

	b.WriteString("\n## Original Handoff\n\n")
	if input.OriginalHandoff != "" {
		preview := input.OriginalHandoff
		if len(preview) > 2000 {
			preview = preview[:2000] + "\n\n[truncated; full artifact available in Relay]"
		}
		b.WriteString("```\n")
		b.WriteString(preview)
		b.WriteString("\n```\n")
	} else {
		b.WriteString("Not available.\n")
	}

	b.WriteString("\n## Agent Result\n\n")
	b.WriteString(fmt.Sprintf("- Status: %s\n", orEmpty(input.AgentResultStatus, "unknown")))
	b.WriteString(fmt.Sprintf("- Build status: %s\n", orEmpty(input.BuildStatus, "N/A")))
	b.WriteString(fmt.Sprintf("- Test status: %s\n", orEmpty(input.TestStatus, "N/A")))
	b.WriteString(fmt.Sprintf("- LOC changed: %s\n", orEmpty(input.LOCChanged, "N/A")))
	if input.ResultRaw != "" {
		raw := input.ResultRaw
		if len(raw) > 500 {
			raw = raw[:500] + "...\n[truncated]"
		}
		b.WriteString(fmt.Sprintf("\nRaw result excerpt:\n```\n%s\n```\n", raw))
	}

	b.WriteString("\n## Relay Validation\n\n")
	b.WriteString(fmt.Sprintf("- Status: %s\n", input.ValidationStatus))
	b.WriteString(fmt.Sprintf("- Repo path: %s\n", input.ValidationRepoPath))
	if len(input.ValidationCommands) > 0 {
		b.WriteString("- Commands:\n")
		for _, cmd := range input.ValidationCommands {
			status := "pass"
			if cmd.ExitCode != 0 || cmd.TimedOut {
				status = "fail"
				if cmd.TimedOut {
					status = "timed out"
				}
			}
			hasStdout := ""
			if cmd.Stdout != "" {
				hasStdout = " stdout"
			}
			hasStderr := ""
			if cmd.Stderr != "" {
				hasStderr = " stderr"
			}
			b.WriteString(fmt.Sprintf("  - `%s` %s exit %d %dms%s%s\n",
				cmd.Command, status, cmd.ExitCode, cmd.DurationMS, hasStdout, hasStderr))
		}
	} else {
		b.WriteString("  No commands executed.\n")
	}

	b.WriteString("\n## Artifacts\n\n")
	b.WriteString("- agent_prompt\n")
	b.WriteString("- opencode_handoff_packet\n")
	b.WriteString("- agent_result_raw\n")
	b.WriteString("- validation_run_json\n")
	b.WriteString("- validation_stdout\n")
	b.WriteString("- validation_stderr\n")
	if input.AgentResultStatus != "" {
		b.WriteString("- opencode_stdout\n")
		b.WriteString("- opencode_stderr\n")
		b.WriteString("- opencode_combined_log\n")
	}

	b.WriteString("\n## Review request\n\n")
	b.WriteString("Please audit whether the implementation appears complete and whether the validation evidence supports accepting the run.\n")

	return b.String()
}

func orEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
