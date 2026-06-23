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

func TestRenderMarkdownIncludesSummaryAndCoverage(t *testing.T) {
	packet := ContextPacket{
		ContextPacketID:  "ctxpkt-2026-06-22-abcd",
		ProjectID:        "relay",
		TaskSlug:         "test-task",
		SourceSnapshotID: "srcsnap_test",
		Status:           ContextPacketStatusCreated,
		GeneratedAt:      "2026-06-22 00:00:00",
		Summary: ContextPacketSummary{
			SourceCount:       1,
			CoveredSeedCount:  1,
			MaxSources:        50,
			MaxTotalBytes:     1024,
			TotalSourceBytes:  128,
			InventoryIncluded: false,
		},
		Coverage: []ContextCoverageEntry{{
			SeedID:    "file:1",
			SeedType:  "file",
			Required:  true,
			Status:    CoverageStatusCovered,
			SourceIDs: []string{"src_test"},
		}},
		Sources: []ContextSource{{
			SourceID:        "src_test",
			SourceType:      SourceTypeFileRead,
			RepoID:          "relay",
			Path:            "src/app.txt",
			Content:         "hello world\n",
			ContentHash:     "hash",
			SnippetHash:     "snippet",
			RedactionStatus: "not_needed",
		}},
	}

	md := renderMarkdown(packet)
	if !strings.Contains(md, "## Summary") || !strings.Contains(md, "Covered Seed Count") {
		t.Fatalf("expected markdown to contain Summary table, got:\n%s", md)
	}
	if !strings.Contains(md, "## Coverage") || !strings.Contains(md, "file:1") {
		t.Fatalf("expected markdown to contain Coverage table, got:\n%s", md)
	}
}
