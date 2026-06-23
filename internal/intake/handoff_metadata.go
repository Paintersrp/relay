package intake

import "strings"

// ExtractHandoffMetadata returns a bounded metadata map derived from supported
// planner handoff structures. It merges simple top-of-file frontmatter with the
// Artifact Metadata YAML code block used by the current handoff contract.
func ExtractHandoffMetadata(text string) map[string]string {
	metadata, _, _, _ := ParseFrontmatter(text)
	if metadata == nil {
		metadata = map[string]string{}
	}

	artifactMetadata := extractArtifactMetadataBlock(text)
	for key, value := range artifactMetadata {
		if strings.TrimSpace(metadata[key]) == "" {
			metadata[key] = value
		}
	}

	return metadata
}

func extractArtifactMetadataBlock(text string) map[string]string {
	lines := strings.Split(text, "\n")
	metadata := map[string]string{}

	inArtifactSection := false
	inYAMLBlock := false
	var blockLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inArtifactSection {
			if strings.EqualFold(trimmed, "## Artifact Metadata") {
				inArtifactSection = true
			}
			continue
		}

		if strings.HasPrefix(trimmed, "## ") && !strings.EqualFold(trimmed, "## Artifact Metadata") {
			break
		}

		if !inYAMLBlock {
			if trimmed == "```yaml" {
				inYAMLBlock = true
			}
			continue
		}

		if trimmed == "```" {
			break
		}

		blockLines = append(blockLines, line)
	}

	for _, line := range blockLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.Index(trimmed, ":")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			if len(value) >= 2 {
				value = value[1 : len(value)-1]
			}
		}
		metadata[key] = value
	}

	return metadata
}
