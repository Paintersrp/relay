package pipeline

import (
	"strings"
	"testing"
)

func TestBuildCommitSuggestionConventionalMessage(t *testing.T) {
	input := CommitSuggestionInput{
		OriginalHandoff:     "# Add git commit workflow step\n\n## Goal\nAdd a new workflow step.\n",
		AuditHandoff:        "## Audit\nValidation passed.\n",
		GitDiffStat:         "1 file changed",
		RepoPath:            "/tmp/test",
		ValidationStatus:    "pass",
		ChangedFileCount:    1,
		DiffInspected:       true,
		AuditHandoffPresent: true,
	}
	suggestion := BuildCommitSuggestion(input)
	if suggestion.Message == "" {
		t.Fatal("expected non-empty commit message")
	}
	if !strings.HasPrefix(suggestion.Message, "feat:") {
		t.Errorf("expected feat: prefix, got %s", suggestion.Message)
	}
	if suggestion.Status != "ready" {
		t.Errorf("expected status ready, got %s", suggestion.Status)
	}
	if suggestion.RepoPath != "/tmp/test" {
		t.Errorf("expected repo path /tmp/test, got %s", suggestion.RepoPath)
	}
	if suggestion.ValidationStatus != "pass" {
		t.Errorf("expected validation pass, got %s", suggestion.ValidationStatus)
	}
	if !suggestion.DiffInspected {
		t.Error("expected diff inspected")
	}
	if !suggestion.AuditHandoffPresent {
		t.Error("expected audit handoff present")
	}
	if suggestion.ChangedFileCount != 1 {
		t.Errorf("expected 1 changed file, got %d", suggestion.ChangedFileCount)
	}
	if suggestion.GeneratedAt == "" {
		t.Error("expected generated_at timestamp")
	}
}

func TestBuildCommitSuggestionFallsBackToChore(t *testing.T) {
	input := CommitSuggestionInput{
		OriginalHandoff: "# Misc internal update\n",
		RepoPath:        "/tmp/test",
		DiffInspected:   true,
	}
	suggestion := BuildCommitSuggestion(input)
	if suggestion.Message == "" {
		t.Fatal("expected non-empty commit message")
	}
	if !strings.HasPrefix(suggestion.Message, "chore:") {
		t.Errorf("expected chore: prefix for fallback, got %s", suggestion.Message)
	}
}

func TestBuildCommitSuggestionPreferExplicitSuggestions(t *testing.T) {
	input := CommitSuggestionInput{
		OriginalHandoff: "# Test\n\nSuggested commit message: fix: resolve race condition\n",
		RepoPath:        "/tmp/test",
		DiffInspected:   true,
	}
	suggestion := BuildCommitSuggestion(input)
	if !strings.Contains(suggestion.Message, "fix:") {
		t.Errorf("expected fix: from explicit suggestion, got %s", suggestion.Message)
	}
}

func TestBuildCommitSuggestionWithAuditAndDiffSourceList(t *testing.T) {
	input := CommitSuggestionInput{
		OriginalHandoff:     "# Test\n",
		AuditHandoff:        "audit content",
		GitDiffStat:         "1 file changed",
		GitDiffNameStatus:   "M file.go",
		RepoPath:            "/tmp/test",
		DiffInspected:       true,
		AuditHandoffPresent: true,
	}
	suggestion := BuildCommitSuggestion(input)
	foundStat := false
	foundNameStatus := false
	foundAudit := false
	for _, s := range suggestion.SourceArtifacts {
		switch s {
		case "git_diff_stat":
			foundStat = true
		case "git_diff_name_status":
			foundNameStatus = true
		case "audit_handoff":
			foundAudit = true
		}
	}
	if !foundStat {
		t.Error("expected git_diff_stat in source artifacts")
	}
	if !foundNameStatus {
		t.Error("expected git_diff_name_status in source artifacts")
	}
	if !foundAudit {
		t.Error("expected audit_handoff in source artifacts")
	}
}

func TestBuildCommitSuggestionSubjectUnder72Chars(t *testing.T) {
	longTitle := "# " + strings.Repeat("a", 100) + "\n\nLong handoff text here."
	input := CommitSuggestionInput{
		OriginalHandoff: longTitle,
		RepoPath:        "/tmp/test",
		DiffInspected:   true,
	}
	suggestion := BuildCommitSuggestion(input)
	if len(suggestion.Message) > 72 {
		t.Errorf("commit message longer than 72 chars: %d: %s", len(suggestion.Message), suggestion.Message)
	}
}
