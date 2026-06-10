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

	GitStatusText     string
	GitDiffStat       string
	GitDiffNumstat    string
	GitDiffNameStatus string
	GitDiffPatch      string
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
	if input.GitStatusText != "" {
		b.WriteString("- git_status_text\n")
	}
	if input.GitDiffStat != "" {
		b.WriteString("- git_diff_stat\n")
	}
	if input.GitDiffNumstat != "" {
		b.WriteString("- git_diff_numstat\n")
	}
	if input.GitDiffNameStatus != "" {
		b.WriteString("- git_diff_name_status\n")
	}
	if input.GitDiffPatch != "" {
		b.WriteString("- git_diff_patch\n")
	}

	b.WriteString("\n## Git Diff Evidence\n\n")
	if input.GitStatusText != "" || input.GitDiffStat != "" || input.GitDiffPatch != "" {
		if input.GitStatusText != "" {
			b.WriteString("### Git status\n\n```text\n")
			b.WriteString(input.GitStatusText)
			b.WriteString("\n```\n\n")
		}
		if input.GitDiffStat != "" {
			b.WriteString("### Diff stat\n\n```text\n")
			b.WriteString(input.GitDiffStat)
			b.WriteString("\n```\n\n")
		}
		if input.GitDiffNameStatus != "" {
			b.WriteString("### Changed files\n\n```text\n")
			b.WriteString(input.GitDiffNameStatus)
			b.WriteString("\n```\n\n")
		}
		if input.GitDiffPatch != "" {
			b.WriteString("### Patch\n\n")
			b.WriteString("Patch artifact: git_diff_patch\n\n")
			excerpt := input.GitDiffPatch
			if len(excerpt) > 4000 {
				excerpt = excerpt[:4000] + "\n[truncated; full patch available in Relay artifacts]"
			}
			b.WriteString("Small excerpt:\n```diff\n")
			b.WriteString(excerpt)
			b.WriteString("\n```\n")
		}
	} else {
		b.WriteString("No git diff evidence artifact was available. Run Inspect Git Diff in Step 7 before audit for stronger evidence.\n")
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
