package pipeline

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type CommitSuggestionCandidate struct {
	Message    string `json:"message"`
	Source     string `json:"source"`
	Confidence string `json:"confidence"`
}

type CommitSuggestion struct {
	Selected                 string                      `json:"selected"`
	Source                   string                      `json:"source"`
	Confidence               string                      `json:"confidence"`
	Candidates               []CommitSuggestionCandidate `json:"candidates"`
	Warnings                 []string                    `json:"warnings"`
	Status                   string                      `json:"status"`
	RepoPath                 string                      `json:"repo_path"`
	ValidationStatus         string                      `json:"validation_status"`
	ValidationFailedAccepted bool                        `json:"validation_failed_accepted"`
	DiffInspected            bool                        `json:"diff_inspected"`
	AuditHandoffPresent      bool                        `json:"audit_handoff_present"`
	ChangedFileCount         int64                       `json:"changed_file_count"`
	SourceArtifacts          []string                    `json:"source_artifacts"`
	GeneratedAt              string                      `json:"generated_at"`
	EvidenceMode             string                      `json:"evidence_mode,omitempty"`
}

type CommitSuggestionInput struct {
	OriginalHandoff          string
	AuditHandoff             string
	GitDiffStat              string
	GitDiffNameStatus        string
	GitChangeEvidenceJSON    string
	AgentResultStatus        string
	AgentBuildStatus         string
	AgentTestStatus          string
	AgentLOCChanged          string
	RepoPath                 string
	ValidationStatus         string
	ValidationFailedAccepted bool
	DiffInspected            bool
	AuditHandoffPresent      bool
	ChangedFileCount         int64
	EvidenceMode             string
	EvidenceCommits          []string
}

func BuildCommitSuggestion(input CommitSuggestionInput) CommitSuggestion {
	now := time.Now().Format(time.RFC3339)
	sources := buildSourceList(input)
	candidates := buildCandidates(input)
	selected, source, confidence, warnings := selectBestCandidate(candidates, input)

	return CommitSuggestion{
		Status:                   "ready",
		Selected:                 selected,
		Source:                   source,
		Confidence:               confidence,
		Candidates:               candidates,
		Warnings:                 warnings,
		RepoPath:                 input.RepoPath,
		ValidationStatus:         input.ValidationStatus,
		ValidationFailedAccepted: input.ValidationFailedAccepted,
		DiffInspected:            input.DiffInspected,
		AuditHandoffPresent:      input.AuditHandoffPresent,
		ChangedFileCount:         input.ChangedFileCount,
		SourceArtifacts:          sources,
		GeneratedAt:              now,
		EvidenceMode:             input.EvidenceMode,
	}
}

func buildCandidates(input CommitSuggestionInput) []CommitSuggestionCandidate {
	var candidates []CommitSuggestionCandidate
	seen := map[string]bool{}

	addUnique := func(msg, source, confidence string) {
		normalized := normalizeMessage(msg)
		if normalized == "" || seen[normalized] {
			return
		}
		seen[normalized] = true
		candidates = append(candidates, CommitSuggestionCandidate{
			Message:    normalized,
			Source:     source,
			Confidence: confidence,
		})
	}

	// 1. Existing commit subject (committed_range with single commit)
	if input.EvidenceMode == "committed_range" && len(input.EvidenceCommits) == 1 {
		addUnique(input.EvidenceCommits[0], "existing_commit", "high")
	}

	// 2. Explicit suggested commit message from handoff text
	for _, text := range []string{input.OriginalHandoff, input.AuditHandoff} {
		if text == "" {
			continue
		}
		msg := parseExplicitSuggestion(text)
		if msg != "" {
			addUnique(msg, "handoff_suggestion", "high")
		}
	}

	// 3. Parse code-block suggested commit messages
	for _, text := range []string{input.OriginalHandoff, input.AuditHandoff} {
		if text == "" {
			continue
		}
		messages := parseCodeBlockSuggestions(text)
		for _, m := range messages {
			addUnique(m, "code_block_suggestion", "medium")
		}
	}

	// 4. Infer from diff stat and run title
	title := extractTitle(input.OriginalHandoff)
	if title != "" {
		commitType := inferType(input.OriginalHandoff, title)
		if isCSSOnly(input.GitDiffNameStatus) {
			commitType = "style"
		}
		subject := strings.ToLower(title[:1]) + title[1:]
		msg := fmt.Sprintf("%s: %s", commitType, subject)
		if len(msg) > 72 {
			msg = msg[:69] + "..."
		}
		addUnique(msg, "diff_summary", "medium")
	}

	// 5. Last resort: fallback
	addUnique("chore: update relay workflow", "fallback", "low")

	return candidates
}

func selectBestCandidate(candidates []CommitSuggestionCandidate, input CommitSuggestionInput) (string, string, string, []string) {
	var warnings []string
	if len(candidates) == 0 {
		return "", "", "none", warnings
	}

	// Score: high > medium > low
	type scored struct {
		candidate CommitSuggestionCandidate
		score     int
	}

	var scoredCandidates []scored
	for _, c := range candidates {
		score := 0
		switch c.Confidence {
		case "high":
			score = 3
		case "medium":
			score = 2
		case "low":
			score = 1
		}

		// Existing commit wins over everything
		if c.Source == "existing_commit" {
			score = 10
		}

		// Reject bad messages
		if isBadMessage(c.Message) {
			warnings = append(warnings, fmt.Sprintf("Rejected candidate from %s: contains disallowed content", c.Source))
			continue
		}

		scoredCandidates = append(scoredCandidates, scored{candidate: c, score: score})
	}

	if len(scoredCandidates) == 0 {
		return "", "", "none", warnings
	}

	// Pick highest score, then first occurrence
	best := scoredCandidates[0]
	for _, sc := range scoredCandidates[1:] {
		if sc.score > best.score || (sc.score == best.score && sc.candidate.Source == "existing_commit") {
			best = sc
		}
	}

	return best.candidate.Message, best.candidate.Source, best.candidate.Confidence, warnings
}

// parseExplicitSuggestion finds "Suggested commit message:" or "Suggested commit:" prefixed lines.
func parseExplicitSuggestion(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		var msg string
		if strings.HasPrefix(lower, "suggested commit message:") {
			msg = strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
		} else if strings.HasPrefix(lower, "suggested commit:") {
			msg = strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
		}
		if msg != "" {
			msg = normalizeMessage(msg)
			if msg != "" {
				return msg
			}
		}
	}
	return ""
}

// parseCodeBlockSuggestions finds suggested commit messages inside short code blocks
// that immediately follow a "Suggested commit" label.
func parseCodeBlockSuggestions(text string) []string {
	var messages []string
	lines := strings.Split(text, "\n")

	inBlock := false
	afterLabel := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		if strings.HasPrefix(lower, "suggested commit message:") || strings.HasPrefix(lower, "suggested commit:") ||
			strings.HasPrefix(lower, "suggested commit message") || strings.HasPrefix(lower, "suggested commit") {
			// Check same line for inline code block
			if idx := strings.Index(trimmed, "`"); idx >= 0 {
				codeContent := extractBacktickContent(trimmed[idx:])
				if codeContent != "" {
					msg := normalizeMessage(codeContent)
					if msg != "" {
						messages = append(messages, msg)
					}
				}
			}
			afterLabel = true
			continue
		}

		if afterLabel && strings.HasPrefix(trimmed, "```") {
			inBlock = true
			afterLabel = false
			continue
		}

		if inBlock {
			if strings.HasPrefix(trimmed, "```") {
				inBlock = false
				continue
			}
			if trimmed != "" {
				msg := normalizeMessage(trimmed)
				if msg != "" {
					messages = append(messages, msg)
				}
			}
			// Only take first line after the fence
			if len(messages) > 0 {
				inBlock = false
			}
		}

		// Reset after label if next line is not a code block
		if afterLabel && trimmed != "" && !strings.HasPrefix(trimmed, "```") {
			afterLabel = false
			// Check if this line itself is the message
			if !strings.HasPrefix(lower, "suggested") && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "-") {
				msg := normalizeMessage(trimmed)
				if msg != "" {
					messages = append(messages, msg)
				}
			}
		}

		_ = i
	}

	return messages
}

var backtickRe = regexp.MustCompile("`([^`]+)`")

func extractBacktickContent(s string) string {
	matches := backtickRe.FindStringSubmatch(s)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func normalizeMessage(msg string) string {
	// Trim quotes, backticks, whitespace
	msg = strings.Trim(msg, "\"'` \t\r\n")

	// Remove code fence markers
	msg = strings.TrimPrefix(msg, "```")
	msg = strings.TrimSuffix(msg, "```")
	msg = strings.TrimSpace(msg)

	// Only first non-empty line
	lines := strings.Split(msg, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			msg = l
			break
		}
	}

	// Remove trailing punctuation that is not part of conventional commit
	msg = strings.TrimRight(msg, ". ")
	if msg == "" {
		return ""
	}

	return msg
}

func isBadMessage(msg string) bool {
	lower := strings.ToLower(msg)

	// Reject handoff headings
	if strings.HasPrefix(lower, "# ") || strings.HasPrefix(lower, "surgical implementation") {
		return true
	}

	// Reject messages with "surgical implementation"
	if strings.Contains(lower, "surgical implementation") {
		return true
	}

	// Reject file-path-only messages
	if strings.Contains(msg, "/") && !strings.Contains(msg, " ") {
		return true
	}

	// Reject multi-paragraph summaries (contains blank line in middle)
	if strings.Contains(msg, "\n\n") {
		return true
	}

	// Reject excessively long messages
	if len(msg) > 120 {
		return true
	}

	return false
}

func isCSSOnly(nameStatus string) bool {
	if nameStatus == "" {
		return false
	}
	lines := strings.Split(nameStatus, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		path := parts[len(parts)-1]
		if !strings.HasSuffix(strings.ToLower(path), ".css") {
			return false
		}
	}
	return len(lines) > 0
}

func buildSourceList(input CommitSuggestionInput) []string {
	var sources []string
	if input.GitDiffStat != "" {
		sources = append(sources, "git_diff_stat")
	}
	if input.GitDiffNameStatus != "" {
		sources = append(sources, "git_diff_name_status")
	}
	if input.AuditHandoff != "" {
		sources = append(sources, "audit_handoff")
	}
	if len(sources) == 0 {
		sources = append(sources, "original_handoff")
	}
	return sources
}

func extractTitle(handoff string) string {
	lines := strings.Split(handoff, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			title := strings.TrimPrefix(trimmed, "# ")
			title = strings.TrimSpace(title)
			if title != "" {
				return title
			}
		}
	}
	return ""
}

func inferType(handoff string, title string) string {
	lowerTitle := strings.ToLower(title)
	lowerHandoff := strings.ToLower(handoff)

	fixKeywords := []string{"bug", "blocker", "failed", "stale", "prevent", "harden", "correct", "fix", "error", "issue"}
	for _, kw := range fixKeywords {
		if strings.Contains(lowerTitle, kw) || strings.Contains(lowerHandoff, " "+kw+" ") {
			return "fix"
		}
	}

	featKeywords := []string{"add", "implement", "new step", "ui addition", "workflow addition", "new feature", "feature"}
	for _, kw := range featKeywords {
		if strings.Contains(lowerTitle, kw) || strings.Contains(lowerHandoff, " "+kw+" ") {
			return "feat"
		}
	}

	if strings.Contains(lowerTitle, "test") || strings.Contains(lowerHandoff, "test on") || strings.Contains(lowerHandoff, "## tests to add or update") {
		if len(lowerHandoff) < 500 || strings.Count(lowerHandoff, "##") < 3 {
			return "test"
		}
	}

	if strings.Contains(lowerTitle, "doc") || strings.Contains(lowerTitle, "readme") || strings.Contains(lowerTitle, "instruction") {
		return "docs"
	}

	refactorKeywords := []string{"refactor", "restructure", "rename", "move", "cleanup", "simplify"}
	for _, kw := range refactorKeywords {
		if strings.Contains(lowerTitle, kw) || strings.Contains(lowerTitle, " "+kw+" ") {
			return "refactor"
		}
	}

	return "chore"
}
