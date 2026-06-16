package intake

import (
	"strings"
)

// ParseFrontmatter parses leading frontmatter delimited by ---.
// It returns a map of keys to values, the raw frontmatter block (if any), the remaining markdown content, and any warnings.
func ParseFrontmatter(text string) (map[string]string, string, string, []string) {
	var warnings []string
	metadata := make(map[string]string)
	lines := strings.Split(text, "\n")

	if len(lines) == 0 {
		return metadata, "", "", []string{"Empty markdown"}
	}

	firstLine := strings.TrimSpace(lines[0])
	if firstLine != "---" {
		// No frontmatter
		warnings = append(warnings, "No frontmatter block found")
		return metadata, "", text, warnings
	}

	var frontmatterLines []string
	var contentLines []string
	inFrontmatter := true
	foundClosing := false

	for i := 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
				foundClosing = true
				continue
			}
			frontmatterLines = append(frontmatterLines, line)
		} else {
			contentLines = append(contentLines, line)
		}
	}

	if !foundClosing {
		warnings = append(warnings, "Frontmatter block is missing a closing '---' delimiter")
		// Treat the whole text as content
		return metadata, "", text, warnings
	}

	frontmatterText := strings.Join(frontmatterLines, "\n")
	content := strings.Join(contentLines, "\n")

	// Parse simple key: value lines
	for _, line := range frontmatterLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.Index(trimmed, ":")
		if idx == -1 {
			// Malformed line in frontmatter
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])

		// Strip quotes if present
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			if len(val) >= 2 {
				val = val[1 : len(val)-1]
			}
		}
		metadata[key] = val
	}

	return metadata, frontmatterText, content, warnings
}

// ValidateHandoffText validates the handoff markdown, returning warnings and blocking errors.
func ValidateHandoffText(text string) (warnings []string, blockers []string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		blockers = append(blockers, "Handoff markdown is empty")
		return warnings, blockers
	}

	metadata, _, _, fmWarnings := ParseFrontmatter(text)
	warnings = append(warnings, fmWarnings...)

	if len(metadata) == 0 && len(fmWarnings) == 0 {
		warnings = append(warnings, "Frontmatter block is empty")
	}

	// Check for title in metadata or markdown header
	title := metadata["title"]
	if title == "" {
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# "))
				break
			}
		}
		if title == "" {
			warnings = append(warnings, "No title found in metadata or markdown header")
		}
	}

	// Check for repo/branch
	repo := metadata["repo"]
	if repo == "" {
		repo = metadata["repo_target"]
	}
	if repo == "" {
		warnings = append(warnings, "No repository specified in frontmatter")
	}

	branch := metadata["branch"]
	if branch == "" {
		branch = metadata["branch_context"]
	}
	if branch == "" {
		warnings = append(warnings, "No branch specified in frontmatter")
	}

	return warnings, blockers
}
