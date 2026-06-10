package pipeline

import (
	"strings"
	"testing"
)

func TestBuildAuditHandoff_AllFields(t *testing.T) {
	input := AuditHandoffInput{
		RunID:      42,
		Title:      "Test handoff",
		RepoName:   "relay",
		BranchName: "feature/test",
		Status:     "validation_passed",

		OriginalHandoff:   "# Test handoff\n\nDo some work.",
		AgentResultStatus: "DONE",
		BuildStatus:       "PASS",
		TestStatus:        "PASS",
		LOCChanged:        "42",
		ResultRaw:         "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 42",

		ValidationStatus:   "pass",
		ValidationRepoPath: "D:/Code/relay",
		ValidationCommands: []CommandRunResult{
			{Label: "go fmt", Command: "go fmt ./...", Source: "handoff", ExitCode: 0, Stdout: "", DurationMS: 1500},
			{Label: "go test", Command: "go test ./...", Source: "handoff", ExitCode: 0, Stdout: "ok", Stderr: "", DurationMS: 5000},
			{Label: "go vet", Command: "go vet ./...", Source: "handoff", ExitCode: 1, Stdout: "", Stderr: "warning", DurationMS: 2000},
		},
	}

	content := BuildAuditHandoff(input)

	if !strings.Contains(content, "Run ID: 42") {
		t.Error("expected Run ID in output")
	}
	if !strings.Contains(content, "Title: Test handoff") {
		t.Error("expected Title in output")
	}
	if !strings.Contains(content, "Repo: relay") {
		t.Error("expected Repo in output")
	}
	if !strings.Contains(content, "Branch: feature/test") {
		t.Error("expected Branch in output")
	}
	if !strings.Contains(content, "Status: validation_passed") {
		t.Error("expected Status in output")
	}
	if !strings.Contains(content, "# Test handoff") {
		t.Error("expected original handoff in output")
	}
	if !strings.Contains(content, "DONE") {
		t.Error("expected agent result status in output")
	}
	if !strings.Contains(content, "PASS") {
		t.Error("expected build/test status in output")
	}
	if !strings.Contains(content, "pass") {
		t.Error("expected validation status in output")
	}
	if !strings.Contains(content, "D:/Code/relay") {
		t.Error("expected validation repo path in output")
	}
	if !strings.Contains(content, "go fmt ./...") {
		t.Error("expected validation commands in output")
	}
	if !strings.Contains(content, "go test ./...") {
		t.Error("expected go test command in output")
	}
	if !strings.Contains(content, "go vet ./...") {
		t.Error("expected go vet command in output")
	}
	if !strings.Contains(content, "agent_prompt") {
		t.Error("expected agent_prompt in artifact list")
	}
	if !strings.Contains(content, "validation_run_json") {
		t.Error("expected validation_run_json in artifact list")
	}
	if !strings.Contains(content, "Review request") {
		t.Error("expected review request section")
	}
}

func TestBuildAuditHandoff_MinimalFields(t *testing.T) {
	input := AuditHandoffInput{
		RunID:            1,
		Title:            "Minimal run",
		ValidationStatus: "pass",
	}

	content := BuildAuditHandoff(input)

	if !strings.Contains(content, "Run ID: 1") {
		t.Error("expected Run ID in output")
	}
	if !strings.Contains(content, "Minimal run") {
		t.Error("expected Title in output")
	}
	if !strings.Contains(content, "unknown") {
		t.Error("expected fallback for missing agent result status")
	}
	if !strings.Contains(content, "No commands executed") {
		t.Error("expected fallback for empty validation commands")
	}
}

func TestBuildAuditHandoff_TruncatesLargeContent(t *testing.T) {
	longHandoff := strings.Repeat("line\n", 5000)
	input := AuditHandoffInput{
		RunID:            2,
		Title:            "Large handoff",
		OriginalHandoff:  longHandoff,
		ValidationStatus: "pass",
		ValidationCommands: []CommandRunResult{
			{Command: "go test", ExitCode: 0, DurationMS: 100},
		},
	}

	content := BuildAuditHandoff(input)

	if !strings.Contains(content, "[truncated") {
		t.Error("expected truncation marker for large handoff")
	}
}

func TestBuildAuditHandoff_CommandStatuses(t *testing.T) {
	input := AuditHandoffInput{
		RunID:            3,
		Title:            "Command statuses",
		ValidationStatus: "fail",
		ValidationCommands: []CommandRunResult{
			{Command: "passing", ExitCode: 0, DurationMS: 100},
			{Command: "failing", ExitCode: 1, DurationMS: 200, Stderr: "error"},
			{Command: "timedout", ExitCode: -2, TimedOut: true, DurationMS: 300},
		},
	}

	content := BuildAuditHandoff(input)

	if !strings.Contains(content, "passing") {
		t.Error("expected passing command")
	}
	if !strings.Contains(content, "pass") {
		t.Error("expected pass status for exit 0")
	}
	if !strings.Contains(content, "failing") {
		t.Error("expected failing command")
	}
	if !strings.Contains(content, "fail") {
		t.Error("expected fail status for exit 1")
	}
	if !strings.Contains(content, "timedout") {
		t.Error("expected timed out command")
	}
	if !strings.Contains(content, "timed out") {
		t.Error("expected timed out status")
	}
}

func TestBuildAuditHandoffIncludesGitDiffEvidence(t *testing.T) {
	input := AuditHandoffInput{
		RunID:              10,
		Title:              "Diff evidence test",
		ValidationStatus:   "pass",
		ValidationRepoPath: "D:/Code/test",
		GitStatusText:      " M README.md\n?? untracked.txt\n",
		GitDiffStat:        " 1 file changed, 2 insertions(+)",
		GitDiffNameStatus:  "M\tREADME.md",
		GitDiffPatch:       "diff --git a/README.md b/README.md\nindex abc..def 100644\n--- a/README.md\n+++ b/README.md\n@@ -1 +1,3 @@\n-# Test\n+# Modified\n+\n+New line\n",
	}

	content := BuildAuditHandoff(input)

	if !strings.Contains(content, "Git Diff Evidence") {
		t.Error("expected Git Diff Evidence section")
	}
	if !strings.Contains(content, "README.md") {
		t.Error("expected changed file reference in output")
	}
	if !strings.Contains(content, "git_diff_patch") {
		t.Error("expected patch artifact reference")
	}
	if !strings.Contains(content, "git_status_text") {
		t.Error("expected git_status_text in artifact list")
	}
	if !strings.Contains(content, "git_diff_stat") {
		t.Error("expected git_diff_stat in artifact list")
	}
	if !strings.Contains(content, "git_diff_patch") {
		t.Error("expected git_diff_patch in artifact list")
	}
}

func TestBuildAuditHandoffNoGitDiffEvidence(t *testing.T) {
	input := AuditHandoffInput{
		RunID:            11,
		Title:            "No diff",
		ValidationStatus: "pass",
	}

	content := BuildAuditHandoff(input)

	if !strings.Contains(content, "No git diff evidence artifact was available") {
		t.Error("expected fallback message when no git diff evidence")
	}
}
