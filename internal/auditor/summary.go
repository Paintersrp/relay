package auditor

import (
	"fmt"
	"strings"
	"time"
)

func GenerateInputSummary(ev *Evidence) string {
	var b strings.Builder

	b.WriteString("# Audit Input Summary\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	b.WriteString("## Run Summary\n\n")
	b.WriteString(fmt.Sprintf("- Run ID: %d\n", ev.RunID))
	b.WriteString(fmt.Sprintf("- Title: %s\n", ev.RunTitle))
	b.WriteString(fmt.Sprintf("- Status: %s\n", ev.RunStatus))
	b.WriteString("\n")

	b.WriteString("## Packet Scope\n\n")
	b.WriteString(fmt.Sprintf("- Packet ID: %s\n", ev.Packet.PacketID))
	if ev.Packet.AuditSeed != "" {
		b.WriteString("- Goal / Scope excerpt:\n")
		b.WriteString("```\n")
		b.WriteString(ev.Packet.AuditSeed)
		b.WriteString("\n```\n")
	}
	b.WriteString("\n")

	b.WriteString("## Executor Result Summary\n\n")
	if ev.ExecutorResult.Present {
		b.WriteString("Evidence source: `executor_result.txt`\n\n")
		if ev.ExecutorResult.Summary != "" {
			b.WriteString("```\n")
			b.WriteString(ev.ExecutorResult.Summary)
			b.WriteString("\n```\n\n")
		}
		b.WriteString(fmt.Sprintf("Content preview (%d bytes):\n```\n%s\n```\n", len(ev.ExecutorResult.Content), ev.ExecutorResult.Content))
	} else {
		b.WriteString("_Not available_\n")
	}
	b.WriteString("\n")

	b.WriteString("## Validation Evidence\n\n")
	if ev.ValidationOutput.Present {
		b.WriteString(fmt.Sprintf("Evidence source: `%s`\n\n", ev.ValidationOutput.Summary))
		b.WriteString(fmt.Sprintf("Content preview (%d bytes):\n```\n%s\n```\n", len(ev.ValidationOutput.Content), ev.ValidationOutput.Content))
	} else {
		b.WriteString("_Not available_\n")
	}
	b.WriteString("\n")

	b.WriteString("## Changed Files\n\n")
	if ev.ChangedFiles.Present {
		b.WriteString("Evidence sources: `git_diff_name_status`, `git_status_text`, `git_diff_stat`\n\n")
		b.WriteString("```\n")
		b.WriteString(ev.ChangedFiles.Preview)
		b.WriteString("\n```\n")
	} else {
		b.WriteString("_Not available_\n")
	}
	b.WriteString("\n")

	b.WriteString("## Diff Evidence\n\n")
	if ev.GitDiff.Present {
		b.WriteString("Evidence source: `git_diff.patch`\n\n")
		b.WriteString(fmt.Sprintf("Preview (%d bytes):\n```diff\n%s\n```\n", len(ev.GitDiff.Preview), ev.GitDiff.Preview))
	} else {
		b.WriteString("_Not available_\n")
	}
	b.WriteString("\n")

	if len(ev.Warnings) > 0 {
		b.WriteString("## Missing Evidence / Warnings\n\n")
		for _, w := range ev.Warnings {
			b.WriteString(fmt.Sprintf("- ⚠ %s\n", w))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Suggested Auditor Review Focus\n\n")
	b.WriteString("- Verify the executor result matches the handoff goal.\n")
	b.WriteString("- Confirm all changed files are within scope.\n")
	b.WriteString("- Review diff for quality and correctness.\n")
	if len(ev.Warnings) > 0 {
		b.WriteString("- Review missing evidence warnings; some evidence may need manual collection.\n")
	}
	b.WriteString("\n---\n")

	return b.String()
}
