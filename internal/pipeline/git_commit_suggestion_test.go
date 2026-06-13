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
	if suggestion.Selected == "" {
		t.Fatal("expected non-empty commit message")
	}
	if !strings.HasPrefix(suggestion.Selected, "feat:") {
		t.Errorf("expected feat: prefix, got %s", suggestion.Selected)
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
	if suggestion.Selected == "" {
		t.Fatal("expected non-empty commit message")
	}
	if !strings.HasPrefix(suggestion.Selected, "chore:") {
		t.Errorf("expected chore: prefix for fallback, got %s", suggestion.Selected)
	}
}

func TestBuildCommitSuggestionPreferExplicitSuggestions(t *testing.T) {
	input := CommitSuggestionInput{
		OriginalHandoff: "# Test\n\nSuggested commit message: fix: resolve race condition\n",
		RepoPath:        "/tmp/test",
		DiffInspected:   true,
	}
	suggestion := BuildCommitSuggestion(input)
	if !strings.Contains(suggestion.Selected, "fix:") {
		t.Errorf("expected fix: from explicit suggestion, got %s", suggestion.Selected)
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
	if len(suggestion.Selected) > 72 {
		t.Errorf("commit message longer than 72 chars: %d: %s", len(suggestion.Selected), suggestion.Selected)
	}
}

func TestBuildCommitSuggestionExistingCommitWins(t *testing.T) {
	input := CommitSuggestionInput{
		OriginalHandoff: "# My Feature\n\nSuggested commit message: feat: something else\n",
		RepoPath:        "/tmp/test",
		DiffInspected:   true,
		EvidenceMode:    "committed_range",
		EvidenceCommits: []string{"feat: add the actual feature"},
	}
	suggestion := BuildCommitSuggestion(input)
	if suggestion.Selected != "feat: add the actual feature" {
		t.Errorf("expected existing commit subject to win, got %q", suggestion.Selected)
	}
	if suggestion.Source != "existing_commit" {
		t.Errorf("expected source existing_commit, got %s", suggestion.Source)
	}
}

func TestBuildCommitSuggestionRejectsBadMessages(t *testing.T) {
	input := CommitSuggestionInput{
		OriginalHandoff: "# Surgical Implementation\n\nSuggested commit message: Surgical Implementation of Step 8\n",
		RepoPath:        "/tmp/test",
		DiffInspected:   true,
	}
	suggestion := BuildCommitSuggestion(input)
	if strings.Contains(suggestion.Selected, "Surgical Implementation") {
		t.Errorf("selected message should not contain Surgical Implementation, got %q", suggestion.Selected)
	}
	if suggestion.Selected == "" {
		t.Fatal("expected non-empty fallback message")
	}
}

func TestParseCodeBlockSuggestions(t *testing.T) {
	text := "Some text\n\nSuggested commit message:\n\n```\nstyle: tighten workbench border radius\n```\n\nMore text."
	messages := parseCodeBlockSuggestions(text)
	if len(messages) == 0 {
		t.Fatal("expected at least one parsed message")
	}
	if messages[0] != "style: tighten workbench border radius" {
		t.Errorf("expected parsed message, got %q", messages[0])
	}
}

func TestNormalizeMessage(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"fix: resolve issue"`, "fix: resolve issue"},
		{"`style: adjust padding`", "style: adjust padding"},
		{"```\nfeat: add feature\n```", "feat: add feature"},
		{"fix: first line\n\nsecond line", "fix: first line"},
		{"  chore: trimmed  ", "chore: trimmed"},
	}
	for _, tt := range tests {
		result := normalizeMessage(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeMessage(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsBadMessage(t *testing.T) {
	bad := []string{
		"# H1 heading",
		"Surgical Implementation of Step 8",
		"/path/to/file.go",
		"para1\n\npara2",
		strings.Repeat("x", 121),
	}
	for _, msg := range bad {
		if !isBadMessage(msg) {
			t.Errorf("expected %q to be rejected", msg)
		}
	}
	good := []string{
		"fix: resolve race condition",
		"feat: add dashboard panel",
		"style: tighten border radius",
	}
	for _, msg := range good {
		if isBadMessage(msg) {
			t.Errorf("expected %q to be accepted", msg)
		}
	}
}
