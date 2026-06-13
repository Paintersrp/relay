package pipeline

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseUnifiedDiffPatchCapturesFileMetadata(t *testing.T) {
	patch := `diff --git a/old.txt b/old.txt
deleted file mode 100644
index 1111111..0000000
--- a/old.txt
+++ /dev/null
@@ -1 +0,0 @@
-old line
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..2222222
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+alpha
+beta
`

	files := ParseUnifiedDiffPatch(patch)
	if len(files) != 2 {
		t.Fatalf("expected 2 parsed files, got %d", len(files))
	}

	first := files[0]
	if first.Path != "old.txt" {
		t.Fatalf("expected first file path old.txt, got %s", first.Path)
	}
	if first.ChangeType != "deleted" {
		t.Fatalf("expected deleted change type, got %s", first.ChangeType)
	}
	if !first.Deleted {
		t.Fatal("expected deleted file flag on first file")
	}
	if len(first.Hunks) != 1 {
		t.Fatalf("expected 1 hunk for first file, got %d", len(first.Hunks))
	}
	added, deleted, context := countAuditPatchLineKinds(first)
	if added != 0 || deleted != 1 || context != 0 {
		t.Fatalf("unexpected line counts for first file: added=%d deleted=%d context=%d", added, deleted, context)
	}

	second := files[1]
	if second.Path != "new.txt" {
		t.Fatalf("expected second file path new.txt, got %s", second.Path)
	}
	if second.ChangeType != "added" {
		t.Fatalf("expected added change type, got %s", second.ChangeType)
	}
	if !second.Created {
		t.Fatal("expected created file flag on second file")
	}
	if len(second.Hunks) != 1 {
		t.Fatalf("expected 1 hunk for second file, got %d", len(second.Hunks))
	}
	added, deleted, context = countAuditPatchLineKinds(second)
	if added != 2 || deleted != 0 || context != 0 {
		t.Fatalf("unexpected line counts for second file: added=%d deleted=%d context=%d", added, deleted, context)
	}
}

func TestParseUnifiedDiffPatchCapturesBinaryIndicators(t *testing.T) {
	patch := `diff --git a/img.png b/img.png
index abcdef0..1234567 100644
Binary files a/img.png and b/img.png differ
`

	files := ParseUnifiedDiffPatch(patch)
	if len(files) != 1 {
		t.Fatalf("expected 1 parsed file, got %d", len(files))
	}
	file := files[0]
	if !file.Binary {
		t.Fatal("expected binary indicator on parsed file")
	}
	if file.ChangeType != "binary" {
		t.Fatalf("expected binary change type, got %s", file.ChangeType)
	}
}

func TestBuildAuditHandoff_AllFields(t *testing.T) {
	input := AuditHandoffInput{
		RunID:            42,
		Title:            "Test handoff",
		RepoName:         "relay",
		BranchName:       "feature/test",
		Status:           "validation_passed",
		SelectedModel:    "DeepSeek V4 Pro",
		RecommendedModel: "DeepSeek V4 Flash",

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

		GitStatusText:     " M README.md\n?? untracked.txt\n",
		GitDiffStat:       " README.md | 2 ++\n 1 file changed, 2 insertions(+)",
		GitDiffNumstat:    "2\t0\tREADME.md",
		GitDiffNameStatus: "M\tREADME.md",
		GitDiffPatch: `diff --git a/README.md b/README.md
index abc..def 100644
--- a/README.md
+++ b/README.md
@@ -1 +1,3 @@
-# Test
+# Modified
+
+New line
diff --git a/new-file.txt b/new-file.txt
new file mode 100644
--- /dev/null
+++ b/new-file.txt
@@ -0,0 +1 @@
+hello
`,
	}

	content := BuildAuditHandoff(input)

	checks := []string{
		"Run ID: 42",
		"Title: Test handoff",
		"Repo: relay",
		"Branch: feature/test",
		"Status: validation_passed",
		"Selected model: DeepSeek V4 Pro",
		"Recommended model: DeepSeek V4 Flash",
		"Status: DONE",
		"Build status: PASS",
		"Test status: PASS",
		"LOC changed: 42",
		"D:/Code/relay",
		"go fmt ./...",
		"go test ./...",
		"go vet ./...",
		"Exit code: 1",
		"Duration: 2000ms",
		"Stdout present: true",
		"Stderr present: true",
		"Failure excerpt",
		" M README.md",
		"?? untracked.txt",
		"git status --short",
		"git diff --name-status",
		"git diff --stat",
		"git diff --numstat",
		"Full Patch For Review",
		"```diff",
		"README.md",
		"new-file.txt",
		"Per-file Review Notes",
		"Change type: modified",
		"Change type: added",
		"Patch included inline above: yes",
		"Audit Request",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Fatalf("expected audit handoff to contain %q", check)
		}
	}
	if strings.Contains(content, "TRUNCATED: The full git diff patch exceeded the inline audit budget.") {
		t.Fatal("did not expect global patch truncation for small diff")
	}
}

func TestBuildAuditHandoff_TruncatesLargePatch(t *testing.T) {
	var patch strings.Builder
	patch.WriteString("diff --git a/large.txt b/large.txt\n")
	patch.WriteString("new file mode 100644\n")
	patch.WriteString("--- /dev/null\n")
	patch.WriteString("+++ b/large.txt\n")
	patch.WriteString("@@ -0,0 +1,5000 @@\n")
	for i := 0; i < 6000; i++ {
		patch.WriteString(fmt.Sprintf("+line %04d %s\n", i, strings.Repeat("x", 20)))
	}

	input := AuditHandoffInput{
		RunID:            7,
		Title:            "Large patch",
		RepoName:         "relay",
		BranchName:       "main",
		Status:           "validation_passed",
		SelectedModel:    "DeepSeek V4 Pro",
		RecommendedModel: "DeepSeek V4 Flash",
		ValidationStatus: "pass",
		GitDiffPatch:     patch.String(),
	}

	content := BuildAuditHandoff(input)

	checks := []string{
		"TRUNCATED: The full git diff patch exceeded the inline audit budget.",
		"TRUNCATED: This file's patch exceeded the audit handoff inline budget.",
		"Patch included inline above: truncated",
		"large.txt",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Fatalf("expected truncated audit handoff to contain %q", check)
		}
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
	if !strings.Contains(content, "No git diff patch artifact was available") {
		t.Error("expected fallback when git diff patch is missing")
	}
}

func TestBuildAuditHandoff_TruncatesLargeOriginalHandoff(t *testing.T) {
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

	if !strings.Contains(content, "[truncated; full artifact available in Relay]") {
		t.Error("expected truncation marker for large handoff")
	}
}
