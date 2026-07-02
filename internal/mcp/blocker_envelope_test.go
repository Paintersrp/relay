package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBlockerEnvelopeBoundsAndSanitizes(t *testing.T) {
	evidence := []MCPBlockerEvidence{
		{Kind: "path", Ref: `C:\Users\trist\secret.txt`, Detail: "drop absolute path"},
		{Kind: "path", Ref: "docs/mcp.md", Detail: strings.Repeat("a", 500)},
	}
	for i := 0; i < 12; i++ {
		evidence = append(evidence, MCPBlockerEvidence{Kind: "tool", Ref: "tool_ref"})
	}
	blocker := newMCPBlocker(MCPBlockerBlockedPath, "bad\x00message", true, evidence, []string{
		"first action",
		"second action",
		"third action",
		"fourth action",
		"fifth action",
		"sixth action",
		"seventh action",
		"eighth action",
		"ninth action",
	})
	if len(blocker.Evidence) != maxBlockerEvidence {
		t.Fatalf("expected bounded evidence, got %d", len(blocker.Evidence))
	}
	if blocker.Evidence[0].Ref == `C:\Users\trist\secret.txt` {
		t.Fatal("absolute path evidence was not removed")
	}
	if len(blocker.NextActions) != maxBlockerActions {
		t.Fatalf("expected bounded next_actions, got %d", len(blocker.NextActions))
	}
	if strings.Contains(blocker.Message, "\x00") {
		t.Fatal("control character was not stripped")
	}
}

func TestToolBlockedResultStructuredContent(t *testing.T) {
	result := toolBlockedResult("example_tool", []MCPBlocker{
		newMCPBlocker(MCPBlockerSchemaMismatch, "bad input", false, []MCPBlockerEvidence{{Kind: "schema", Ref: "example_tool"}}, []string{"Retry with valid input."}),
	}, map[string]any{"request_id": "req-1"})
	if !result.IsError {
		t.Fatal("expected blocked tool result to set IsError")
	}
	var structured MCPBlockedResponse
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	if err := json.Unmarshal(data, &structured); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if structured.OK || structured.Status != "blocked" || structured.Tool != "example_tool" {
		t.Fatalf("unexpected blocked response: %+v", structured)
	}
	if len(structured.Blockers) != 1 || structured.Blockers[0].Code != MCPBlockerSchemaMismatch {
		t.Fatalf("unexpected blockers: %+v", structured.Blockers)
	}
}
