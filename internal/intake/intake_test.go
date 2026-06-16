package intake

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	t.Run("Standard Frontmatter", func(t *testing.T) {
		text := `---
title: "My Test Run"
repo: "my-org/my-repo"
branch: main
---
# Title
Content here.`
		metadata, rawFm, content, warnings := ParseFrontmatter(text)

		if len(warnings) > 0 {
			t.Errorf("expected 0 warnings, got %d: %v", len(warnings), warnings)
		}
		if metadata["title"] != "My Test Run" {
			t.Errorf("expected title 'My Test Run', got %q", metadata["title"])
		}
		if metadata["repo"] != "my-org/my-repo" {
			t.Errorf("expected repo 'my-org/my-repo', got %q", metadata["repo"])
		}
		if metadata["branch"] != "main" {
			t.Errorf("expected branch 'main', got %q", metadata["branch"])
		}
		if !testingContains(content, "# Title") {
			t.Errorf("expected content to contain '# Title', got %q", content)
		}
		if !testingContains(rawFm, "title:") {
			t.Errorf("expected raw frontmatter to contain 'title:', got %q", rawFm)
		}
	})

	t.Run("No Frontmatter", func(t *testing.T) {
		text := `# No Frontmatter
Content directly.`
		metadata, rawFm, content, warnings := ParseFrontmatter(text)

		if len(warnings) != 1 || warnings[0] != "No frontmatter block found" {
			t.Errorf("expected 'No frontmatter block found' warning, got %v", warnings)
		}
		if len(metadata) != 0 {
			t.Errorf("expected empty metadata, got %v", metadata)
		}
		if rawFm != "" {
			t.Errorf("expected empty raw frontmatter, got %q", rawFm)
		}
		if content != text {
			t.Errorf("expected content to match original text, got %q", content)
		}
	})

	t.Run("Malformed Frontmatter", func(t *testing.T) {
		text := `---
title: Unclosed
# Content`
		_, _, _, warnings := ParseFrontmatter(text)

		if len(warnings) != 1 || warnings[0] != "Frontmatter block is missing a closing '---' delimiter" {
			t.Errorf("expected missing delimiter warning, got %v", warnings)
		}
	})
}

func TestValidateHandoffText(t *testing.T) {
	t.Run("Empty Markdown", func(t *testing.T) {
		_, blockers := ValidateHandoffText("   \n   ")
		if len(blockers) != 1 || blockers[0] != "Handoff markdown is empty" {
			t.Errorf("expected 'Handoff markdown is empty' blocker, got blockers: %v", blockers)
		}
	})

	t.Run("Missing Metadata Warnings", func(t *testing.T) {
		text := `# Header Only`
		warnings, blockers := ValidateHandoffText(text)

		if len(blockers) > 0 {
			t.Errorf("expected 0 blockers, got %v", blockers)
		}
		// Expect warnings about: no frontmatter block, no repo, no branch
		hasNoFm := false
		hasNoRepo := false
		hasNoBranch := false
		for _, w := range warnings {
			if w == "No frontmatter block found" {
				hasNoFm = true
			}
			if w == "No repository specified in frontmatter" {
				hasNoRepo = true
			}
			if w == "No branch specified in frontmatter" {
				hasNoBranch = true
			}
		}
		if !hasNoFm || !hasNoRepo || !hasNoBranch {
			t.Errorf("missing expected warnings. Got: %v", warnings)
		}
	})

	t.Run("Valid with metadata", func(t *testing.T) {
		text := `---
title: Valid Title
repo: Paintersrp/relay
branch: main
---
# Valid Title
Goal: test`
		warnings, blockers := ValidateHandoffText(text)

		if len(blockers) > 0 {
			t.Errorf("expected 0 blockers, got %v", blockers)
		}
		if len(warnings) > 0 {
			t.Errorf("expected 0 warnings, got %v", warnings)
		}
	})
}

func testingContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || stringsContainsHelper(s, substr))
}

func stringsContainsHelper(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
