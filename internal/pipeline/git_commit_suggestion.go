package pipeline

import (
	"fmt"
	"strings"
	"time"
)

type CommitSuggestion struct {
	Status                   string   `json:"status"`
	Message                  string   `json:"message"`
	RepoPath                 string   `json:"repo_path"`
	ValidationStatus         string   `json:"validation_status"`
	ValidationFailedAccepted bool     `json:"validation_failed_accepted"`
	DiffInspected            bool     `json:"diff_inspected"`
	AuditHandoffPresent      bool     `json:"audit_handoff_present"`
	ChangedFileCount         int64    `json:"changed_file_count"`
	SourceArtifacts          []string `json:"source_artifacts"`
	GeneratedAt              string   `json:"generated_at"`
}

func BuildCommitSuggestion(input CommitSuggestionInput) CommitSuggestion {
	msg := inferCommitMessage(input)
	now := time.Now().Format(time.RFC3339)
	sources := buildSourceList(input)

	return CommitSuggestion{
		Status:                   "ready",
		Message:                  msg,
		RepoPath:                 input.RepoPath,
		ValidationStatus:         input.ValidationStatus,
		ValidationFailedAccepted: input.ValidationFailedAccepted,
		DiffInspected:            input.DiffInspected,
		AuditHandoffPresent:      input.AuditHandoffPresent,
		ChangedFileCount:         input.ChangedFileCount,
		SourceArtifacts:          sources,
		GeneratedAt:              now,
	}
}

type CommitSuggestionInput struct {
	OriginalHandoff          string
	AuditHandoff             string
	GitDiffStat              string
	GitDiffNameStatus        string
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
}

func inferCommitMessage(input CommitSuggestionInput) string {
	handoffText := input.OriginalHandoff

	// Check for explicit suggested commit message in the handoff
	lines := strings.Split(handoffText, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "suggested commit message:") {
			msg := strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
			if msg != "" {
				return msg
			}
		}
		if strings.HasPrefix(lower, "suggested commit:") {
			msg := strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
			if msg != "" {
				return msg
			}
		}
	}

	// Infer type from handoff title and content
	title := extractTitle(handoffText)
	commitType := inferType(handoffText, title)

	// Build subject
	subject := title
	if subject == "" {
		subject = "update relay workflow"
	}

	// Keep lowercase after type
	if len(subject) > 0 {
		subject = strings.ToLower(subject[:1]) + subject[1:]
	}

	// Truncate to 72 chars
	fullMsg := fmt.Sprintf("%s: %s", commitType, subject)
	if len(fullMsg) > 72 {
		fullMsg = fullMsg[:69] + "..."
	}

	return fullMsg
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

	// Check for fix-indicating keywords
	fixKeywords := []string{"bug", "blocker", "failed", "stale", "prevent", "harden", "correct", "fix", "error", "issue"}
	for _, kw := range fixKeywords {
		if strings.Contains(lowerTitle, kw) || strings.Contains(lowerHandoff, " "+kw+" ") {
			return "fix"
		}
	}

	// Check for feature-indicating keywords
	featKeywords := []string{"add", "implement", "new step", "ui addition", "workflow addition", "new feature", "feature"}
	for _, kw := range featKeywords {
		if strings.Contains(lowerTitle, kw) || strings.Contains(lowerHandoff, " "+kw+" ") {
			return "feat"
		}
	}

	// Check for test-only
	if strings.Contains(lowerTitle, "test") || strings.Contains(lowerHandoff, "test on") || strings.Contains(lowerHandoff, "## tests to add or update") {
		if len(lowerHandoff) < 500 || strings.Count(lowerHandoff, "##") < 3 {
			return "test"
		}
	}

	// Check for docs-only
	if strings.Contains(lowerTitle, "doc") || strings.Contains(lowerTitle, "readme") || strings.Contains(lowerTitle, "instruction") {
		return "docs"
	}

	// Check for refactor
	refactorKeywords := []string{"refactor", "restructure", "rename", "move", "cleanup", "simplify"}
	for _, kw := range refactorKeywords {
		if strings.Contains(lowerTitle, kw) || strings.Contains(lowerTitle, " "+kw+" ") {
			return "refactor"
		}
	}

	return "chore"
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
