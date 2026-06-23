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
