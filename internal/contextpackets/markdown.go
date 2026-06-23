package contextpackets

import (
	"fmt"
	"strings"
)

func renderMarkdown(packet ContextPacket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Context Packet: %s\n\n", packet.TaskSlug)
	fmt.Fprintf(&b, "- Context packet ID: `%s`\n", packet.ContextPacketID)
	fmt.Fprintf(&b, "- Project: `%s`\n", packet.ProjectID)
	if packet.PlanID != "" {
		fmt.Fprintf(&b, "- Plan: `%s`\n", packet.PlanID)
	}
	if packet.PassID != "" {
		fmt.Fprintf(&b, "- Pass: `%s`\n", packet.PassID)
	}
	fmt.Fprintf(&b, "- Source snapshot: `%s`\n", packet.SourceSnapshotID)
	fmt.Fprintf(&b, "- Status: `%s`\n", packet.Status)
	fmt.Fprintf(&b, "- Generated at: `%s`\n\n", packet.GeneratedAt)

	b.WriteString("## Summary\n\n")
	b.WriteString("| Field | Value |\n")
	b.WriteString("|---|---|\n")
	fmt.Fprintf(&b, "| Source Count | %d |\n", packet.Summary.SourceCount)
	fmt.Fprintf(&b, "| Covered Seed Count | %d |\n", packet.Summary.CoveredSeedCount)
	fmt.Fprintf(&b, "| Blocked Seed Count | %d |\n", packet.Summary.BlockedSeedCount)
	fmt.Fprintf(&b, "| Missing Seed Count | %d |\n", packet.Summary.MissingSeedCount)
	fmt.Fprintf(&b, "| Truncated | %t |\n", packet.Summary.Truncated)
	fmt.Fprintf(&b, "| Max Sources | %d |\n", packet.Summary.MaxSources)
	fmt.Fprintf(&b, "| Max Total Bytes | %d |\n", packet.Summary.MaxTotalBytes)
	fmt.Fprintf(&b, "| Total Source Bytes | %d |\n", packet.Summary.TotalSourceBytes)
	fmt.Fprintf(&b, "| Inventory Included | %t |\n\n", packet.Summary.InventoryIncluded)

	b.WriteString("## Coverage\n\n")
	b.WriteString("| Seed | Type | Required | Status | Sources | Blockers |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, entry := range packet.Coverage {
		var blockerMsgs []string
		for _, blk := range entry.Blockers {
			blockerMsgs = append(blockerMsgs, fmt.Sprintf("%s: %s", blk.Code, blk.Message))
		}
		blockersStr := strings.Join(blockerMsgs, "; ")
		if entry.MissingCause != "" {
			if blockersStr != "" {
				blockersStr += "; "
			}
			blockersStr += "missing cause: " + entry.MissingCause
		}
		sourcesStr := strings.Join(entry.SourceIDs, ", ")
		fmt.Fprintf(&b, "| %s | %s | %t | %s | %s | %s |\n",
			entry.SeedID, entry.SeedType, entry.Required, entry.Status, sourcesStr, blockersStr)
	}
	b.WriteString("\n")

	b.WriteString("## Sources\n\n")
	for _, source := range packet.Sources {
		fmt.Fprintf(&b, "### %s\n\n", source.SourceID)
		fmt.Fprintf(&b, "- Type: `%s`\n", source.SourceType)
		fmt.Fprintf(&b, "- Repo: `%s`\n", source.RepoID)
		fmt.Fprintf(&b, "- Path: `%s`\n", source.Path)
		if source.LineStart > 0 {
			fmt.Fprintf(&b, "- Lines: `%d-%d`\n", source.LineStart, source.LineEnd)
		}
		fmt.Fprintf(&b, "- Content hash: `%s`\n", source.ContentHash)
		if source.SnippetHash != "" {
			fmt.Fprintf(&b, "- Snippet hash: `%s`\n", source.SnippetHash)
		}
		fmt.Fprintf(&b, "- Redaction: `%s`\n", source.RedactionStatus)
		fmt.Fprintf(&b, "- Truncated: `%t`\n\n", source.Truncated)
		body := source.Content
		if body == "" {
			body = source.Snippet
		}
		if body != "" {
			b.WriteString("```text\n")
			b.WriteString(body)
			if !strings.HasSuffix(body, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n")
		}
	}
	if len(packet.Blockers) > 0 {
		b.WriteString("## Blockers\n\n")
		for _, blocker := range packet.Blockers {
			fmt.Fprintf(&b, "- `%s` `%s`: %s\n", blocker.RepoID, blocker.Code, blocker.Message)
		}
	}
	return b.String()
}
