package instructions

import (
	"os"
	"strings"
	"testing"
)

func TestHandoffInstructionsNonEmpty(t *testing.T) {
	if HandoffInstructions == "" {
		t.Fatal("HandoffInstructions is empty")
	}
}

func TestHandoffInstructionsContainsRequiredSections(t *testing.T) {
	checks := []string{
		"## Required handoff .txt structure",
		"## Relay Agent Prompt transformation rules",
		"## Test instructions are preserved in Agent Prompts",
	}
	for _, s := range checks {
		if !strings.Contains(HandoffInstructions, s) {
			t.Errorf("HandoffInstructions missing: %q", s)
		}
	}
}

func TestAgentsMDNonEmpty(t *testing.T) {
	if AgentsMD == "" {
		t.Fatal("AgentsMD is empty")
	}
}

func TestAgentsMDContainsRequiredSections(t *testing.T) {
	checks := []string{
		"Stack",
		"RTK shell command rule",
		"Completion response format",
	}
	for _, s := range checks {
		if !strings.Contains(AgentsMD, s) {
			t.Errorf("AgentsMD missing: %q", s)
		}
	}
}

func TestClineRulesNonEmpty(t *testing.T) {
	if ClineRules == "" {
		t.Fatal("ClineRules is empty")
	}
}

func TestClineRulesContainsRequiredSections(t *testing.T) {
	checks := []string{
		"Read and follow `AGENTS.md`",
		"Follow the supplied surgical handoff exactly",
	}
	for _, s := range checks {
		if !strings.Contains(ClineRules, s) {
			t.Errorf("ClineRules missing: %q", s)
		}
	}
}

// Test that root AGENTS.md matches the canonical assets version.
func TestRootAGENTSMDMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile("../../AGENTS.md")
	if err != nil {
		t.Fatalf("read root AGENTS.md: %v", err)
	}
	rootContent := strings.TrimSpace(string(canonical))
	assetContent := strings.TrimSpace(AssetsAGENTSMD)
	if rootContent != assetContent {
		t.Error("root AGENTS.md does not match canonical assets/AGENTS.md")
	}
}

// Test that root .clinerules matches the canonical assets version.
func TestRootClineRulesMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile("../../.clinerules")
	if err != nil {
		t.Fatalf("read root .clinerules: %v", err)
	}
	rootContent := strings.TrimSpace(string(canonical))
	assetContent := strings.TrimSpace(AssetsClineRules)
	if rootContent != assetContent {
		t.Error("root .clinerules does not match canonical assets/.clinerules")
	}
}

func TestAssetRegistryContainsAllAssets(t *testing.T) {
	assets := Registry()
	if len(assets) != 3 {
		t.Errorf("expected 3 assets, got %d", len(assets))
	}

	found := map[string]bool{}
	for _, a := range assets {
		found[a.Key] = true
		switch a.Key {
		case "surgical-chat-instructions":
			if a.Filename != "surgical-chat-instructions.txt" {
				t.Errorf("expected filename surgical-chat-instructions.txt, got %s", a.Filename)
			}
		case "agents-md":
			if a.Filename != "AGENTS.md" {
				t.Errorf("expected filename AGENTS.md, got %s", a.Filename)
			}
		case "clinerules":
			if a.Filename != ".clinerules" {
				t.Errorf("expected filename .clinerules, got %s", a.Filename)
			}
		default:
			t.Errorf("unexpected asset key: %s", a.Key)
		}
	}

	if !found["surgical-chat-instructions"] {
		t.Error("missing surgical-chat-instructions asset")
	}
	if !found["agents-md"] {
		t.Error("missing agents-md asset")
	}
	if !found["clinerules"] {
		t.Error("missing clinerules asset")
	}
}

func TestAssetDownloadHeaders(t *testing.T) {
	contentType, disposition := AssetDownloadHeaders("AGENTS.md")
	if contentType != "text/markdown; charset=utf-8" {
		t.Errorf("expected markdown content type, got %s", contentType)
	}
	if disposition != `attachment; filename="AGENTS.md"` {
		t.Errorf("unexpected disposition: %s", disposition)
	}

	contentType, disposition = AssetDownloadHeaders(".clinerules")
	if contentType != "text/plain; charset=utf-8" {
		t.Errorf("expected plain content type, got %s", contentType)
	}
}
