package auditor

import (
	"fmt"
	"strings"
	"time"
)

// GenerateInputSummary produces an audit_input_summary.md from the collected evidence.
// This is a human-readable summary artifact; the full audit_packet.md is the authoritative document.
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
	if ev.Packet.Goal != "" {
		b.WriteString("\n**Goal:**\n\n")
		b.WriteString(ev.Packet.Goal)
		b.WriteString("\n")
	} else {
		b.WriteString("\n⚠ Goal not available from canonical_packet.json\n")
	}
	if ev.Packet.Scope != "" {
		b.WriteString("\n**Scope:**\n\n")
		b.WriteString(ev.Packet.Scope)
		b.WriteString("\n")
	} else {
		b.WriteString("\n⚠ Scope not available from canonical_packet.json\n")
	}
	if ev.Packet.NonGoals != "" {
		b.WriteString("\n**Non-goals:**\n\n")
		for _, l := range strings.Split(strings.TrimSpace(ev.Packet.NonGoals), "\n") {
			l = strings.TrimSpace(l)
			if l != "" {
				if !strings.HasPrefix(l, "-") {
					b.WriteString(fmt.Sprintf("- %s\n", l))
				} else {
					b.WriteString(l + "\n")
				}
			}
		}
	}
	if len(ev.Packet.MissingFields) > 0 {
		b.WriteString(fmt.Sprintf("\n⚠ Missing packet fields: %s\n", strings.Join(ev.Packet.MissingFields, ", ")))
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
		if ev.ExecutorResult.RawArtifactPath != "" {
			b.WriteString(fmt.Sprintf("Full artifact: `%s`\n\n", ev.ExecutorResult.RawArtifactPath))
		}
		b.WriteString(fmt.Sprintf("Content preview (%d bytes):\n```\n%s\n```\n", len(ev.ExecutorResult.Content), ev.ExecutorResult.Content))
	} else {
		b.WriteString("⚠ **Not available** — executor_result artifact not found\n")
		b.WriteString("Audit consequence: executor status cannot be confirmed.\n")
	}
	b.WriteString("\n")

	b.WriteString("## Validation Evidence\n\n")
	if len(ev.ValidationResults) == 0 {
		b.WriteString("⚠ **Not available** — no validation output artifacts found\n")
		b.WriteString("Audit consequence: validation cannot be confirmed.\n")
	} else {
		for _, vr := range ev.ValidationResults {
			b.WriteString(fmt.Sprintf("**%s:** `%s`\n", vr.ID, vr.Command))
			b.WriteString(fmt.Sprintf("- Status: `%s`\n", string(vr.Status)))
			b.WriteString(fmt.Sprintf("- Exit: %s\n", vr.ExitResult))
			b.WriteString(fmt.Sprintf("- Evidence: %s\n", vr.EvidenceSummary))
			if vr.RawArtifactPath != "" {
				b.WriteString(fmt.Sprintf("- Raw artifact: `%s`\n", vr.RawArtifactPath))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	if ev.AcceptanceEvidence.Present {
		b.WriteString("## Validation Failure Acceptance\n\n")
		b.WriteString(fmt.Sprintf("Evidence source: `%s`\n\n", ev.AcceptanceEvidence.RawArtifactPath))
		b.WriteString("```json\n")
		b.WriteString(ev.AcceptanceEvidence.Content)
		b.WriteString("\n```\n\n")
	}

	b.WriteString("## Changed Files\n\n")
	if ev.ChangedFiles.Present {
		b.WriteString(fmt.Sprintf("Evidence source kind: `%s`\n", ev.ChangedFiles.SourceKind))
		b.WriteString(fmt.Sprintf("Full artifact: `%s`\n\n", ev.ChangedFiles.RawArtifactPath))
		b.WriteString("```\n")
		for _, f := range ev.ChangedFiles.Files {
			b.WriteString(fmt.Sprintf("%s\t%s\n", f.Status, f.Path))
		}
		b.WriteString("```\n")
	} else {
		b.WriteString("⚠ **Not available** — changed files artifact not found\n")
		b.WriteString("Audit consequence: file scope cannot be confirmed.\n")
	}
	b.WriteString("\n")

	b.WriteString("## Diff Evidence\n\n")
	if ev.GitDiff.Present {
		b.WriteString(fmt.Sprintf("Full artifact: `%s`\n\n", ev.GitDiff.RawArtifactPath))
		b.WriteString(fmt.Sprintf("Preview (%d bytes):\n```diff\n%s\n```\n", len(ev.GitDiff.Preview), ev.GitDiff.Preview))
	} else {
		b.WriteString("⚠ **Not available** — git_diff.patch artifact not found\n")
		b.WriteString("Audit consequence: diff review requires external data.\n")
	}
	b.WriteString("\n")

	if len(ev.Warnings) > 0 {
		b.WriteString("## Missing Evidence / Warnings\n\n")
		for _, w := range ev.Warnings {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", string(w.Severity), w.Message))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Auditor Review Focus\n\n")
	b.WriteString("- Verify the executor result matches the handoff goal.\n")
	b.WriteString("- Confirm all changed files are within scope.\n")
	b.WriteString("- Review diff for quality and correctness.\n")
	if len(ev.Warnings) > 0 {
		b.WriteString("- Review missing evidence warnings; some evidence may need manual collection.\n")
	}
	if len(ev.Packet.AuditChecklist) > 0 {
		b.WriteString(fmt.Sprintf("- Evaluate %d checklist items in the audit_packet.md.\n", len(ev.Packet.AuditChecklist)))
	}
	b.WriteString("\n---\n")

	return b.String()
}
