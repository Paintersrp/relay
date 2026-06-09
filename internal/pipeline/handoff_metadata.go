package pipeline

import (
	"path/filepath"
	"regexp"
	"strings"
)

var allowedRootScopedFiles = map[string]bool{
	"README.md":     true,
	"AGENTS.md":     true,
	".clinerules":   true,
	"Makefile":      true,
	"go.mod":        true,
	"go.sum":        true,
	"package.json":  true,
	"tsconfig.json": true,
	"sqlc.yaml":     true,
	"sqlc.yml":      true,
}

type HandoffMetadata struct {
	Title               string              `json:"title"`
	RecommendedModel    string              `json:"recommended_model"`
	SuggestedCommit     string              `json:"suggested_commit"`
	FinalOutputContract string              `json:"final_output_contract"`
	ScopedFiles         []ScopedFile        `json:"scoped_files"`
	ValidationCommands  []ValidationCommand `json:"validation_commands"`
}

type ScopedFile struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

var outputContractHeadings = []string{
	"## Agent final output requirement",
	"## Agent final output",
	"## Agent final response",
	"## Final output",
	"## Output",
}

var commitLabelRe = regexp.MustCompile(`(?i)^(?:\*\*)?(?:Suggested commit message|Suggested commit|Commit message)(?::\*\*|:\*|:|)\s*(.+)$`)

func isOutputContractHeading(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, h := range outputContractHeadings {
		if strings.EqualFold(trimmed, h) {
			return true
		}
	}
	return false
}

var sourceFileRe = regexp.MustCompile(`[a-zA-Z0-9_/\\.*-]+\.[a-zA-Z]+`)
var scopedFileRe = regexp.MustCompile(`^[a-zA-Z0-9_./\\-]+\.[a-zA-Z0-9]+$`)

func looksLikeSourceFile(s string) bool {
	return sourceFileRe.MatchString(s) && (strings.Contains(s, "/") || strings.Contains(s, "\\"))
}

func looksLikeScopedFile(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	if strings.Contains(s, "/") || strings.Contains(s, "\\") {
		return scopedFileRe.MatchString(s)
	}

	if allowedRootScopedFiles[s] {
		return true
	}

	ext := strings.ToLower(filepath.Ext(s))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".templ", ".md", ".json", ".yaml", ".yml", ".sql":
		return true
	default:
		return false
	}
}

func cleanPathToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "-*+ ")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`")
	s = strings.TrimSpace(s)
	// Strip checkbox prefix like "- [ ] " or "- [x] "
	if idx := strings.Index(s, "] "); idx > 0 && idx < 5 {
		s = strings.TrimSpace(s[idx+2:])
	}
	return s
}

var scopeSectionRe = regexp.MustCompile(`(?i)^##\s+(Scope|Direct files likely changed|Direct files needed for context|Direct context files)\s*$`)

func ParseHandoffMetadata(handoffText string, repoDefaultCommandsJSON string) HandoffMetadata {
	lines := strings.Split(handoffText, "\n")
	meta := HandoffMetadata{}

	// title
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			meta.Title = strings.TrimPrefix(line, "# ")
			break
		}
	}

	// recommended model
	meta.RecommendedModel, _ = ParseRecommendedModel(handoffText)

	// suggested commit
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := commitLabelRe.FindStringSubmatch(trimmed); len(m) > 1 {
			meta.SuggestedCommit = strings.TrimSpace(m[1])
			break
		}
	}
	if meta.SuggestedCommit == "" {
		inCommitSection := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.EqualFold(trimmed, "## Suggested commit message") || strings.EqualFold(trimmed, "## Suggested commit") {
				inCommitSection = true
				continue
			}
			if inCommitSection {
				if strings.HasPrefix(trimmed, "## ") {
					break
				}
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					meta.SuggestedCommit = strings.Trim(trimmed, "`")
					break
				}
			}
		}
	}

	// final output contract
	var b strings.Builder
	inOutputSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inOutputSection && isOutputContractHeading(trimmed) {
			inOutputSection = true
			continue
		}
		if inOutputSection {
			if strings.HasPrefix(trimmed, "## ") {
				break
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(line)
		}
	}
	meta.FinalOutputContract = strings.TrimSpace(b.String())

	// scoped files
	seen := map[string]bool{}
	inScopeSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			inScopeSection = scopeSectionRe.MatchString(trimmed)
			continue
		}

		if !inScopeSection {
			continue
		}

		cleaned := cleanPathToken(trimmed)
		if cleaned == "" || seen[cleaned] {
			continue
		}
		if looksLikeScopedFile(cleaned) {
			seen[cleaned] = true
			meta.ScopedFiles = append(meta.ScopedFiles, ScopedFile{
				Path:   cleaned,
				Source: "handoff",
			})
		}
	}

	// validation commands
	meta.ValidationCommands = ExtractValidationCommands(handoffText, repoDefaultCommandsJSON)

	return meta
}

func ExtractScopedFilePaths(text string) []string {
	meta := ParseHandoffMetadata(text, "")
	paths := make([]string, 0, len(meta.ScopedFiles))
	for _, f := range meta.ScopedFiles {
		paths = append(paths, f.Path)
	}
	return paths
}

func IsSourceFilePath(s string) bool {
	ext := filepath.Ext(s)
	if ext == "" {
		return false
	}
	return strings.Contains(s, "/") || strings.Contains(s, "\\")
}
