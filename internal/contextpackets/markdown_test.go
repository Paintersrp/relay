package contextpackets

import (
	"strings"
	"testing"
)

func TestRenderMarkdownIncludesRedactedSnippet(t *testing.T) {
	packet := ContextPacket{
		ContextPacketID:  "ctxpkt_test",
		ProjectID:        "relay",
		TaskSlug:         "packet",
		SourceSnapshotID: "srcsnap_test",
		Status:           ContextPacketStatusCreated,
		GeneratedAt:      "2026-06-22 00:00:00",
		Sources: []ContextSource{{
			SourceID:        "src_test",
			SourceType:      SourceTypeFileRead,
			RepoID:          "relay",
			Path:            "src/app.txt",
			Content:         "token: [REDACTED_TOKEN]\n",
			ContentHash:     "hash",
			SnippetHash:     "snippet",
			RedactionStatus: "redacted",
		}},
	}

	md := renderMarkdown(packet)
	if !strings.Contains(md, "[REDACTED_TOKEN]") || strings.Contains(md, "super-secret-token") {
		t.Fatalf("unexpected markdown: %s", md)
	}
	if !strings.Contains(md, "```text") {
		t.Fatalf("expected fenced source content, got %s", md)
	}
}
