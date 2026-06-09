package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHandoffMetadataTitle(t *testing.T) {
	text := "# My Handoff Title\n\nSome text\n"
	meta := ParseHandoffMetadata(text, "[]")
	if meta.Title != "My Handoff Title" {
		t.Fatalf("expected 'My Handoff Title', got %q", meta.Title)
	}
}

func TestParseHandoffMetadataRecommendedModel(t *testing.T) {
	text := "## Execution model\n\nUse: DeepSeek V4 Flash\n"
	meta := ParseHandoffMetadata(text, "[]")
	if meta.RecommendedModel != "DeepSeek V4 Flash" {
		t.Fatalf("expected 'DeepSeek V4 Flash', got %q", meta.RecommendedModel)
	}
}

func TestParseHandoffMetadataSuggestedCommit(t *testing.T) {
	text := "# Test\n\nSuggested commit message: fix the thing\n"
	meta := ParseHandoffMetadata(text, "[]")
	if meta.SuggestedCommit != "fix the thing" {
		t.Fatalf("expected 'fix the thing', got %q", meta.SuggestedCommit)
	}
}

func TestParseHandoffMetadataSuggestedCommitSection(t *testing.T) {
	text := "# Test\n\n## Suggested commit message\n\n`fix: resolve the bug`\n"
	meta := ParseHandoffMetadata(text, "[]")
	if meta.SuggestedCommit != "fix: resolve the bug" {
		t.Fatalf("expected 'fix: resolve the bug', got %q", meta.SuggestedCommit)
	}
}

func TestParseHandoffMetadataFinalOutputContract(t *testing.T) {
	text := "# Test\n\n## Agent final output requirement\n\nReturn only:\n- DONE or BLOCKED\n"
	meta := ParseHandoffMetadata(text, "[]")
	if meta.FinalOutputContract == "" {
		t.Fatal("expected non-empty final output contract")
	}
}

func TestParseHandoffMetadataScopedFiles(t *testing.T) {
	text := "# Test\n\n## Scope\n\n- `internal/foo.go`\n- internal/bar.go\n- `cmd/main.go`\n"
	meta := ParseHandoffMetadata(text, "[]")
	if len(meta.ScopedFiles) != 3 {
		t.Fatalf("expected 3 scoped files, got %d: %#v", len(meta.ScopedFiles), meta.ScopedFiles)
	}
}

func TestParseHandoffMetadataScopedFilesDirectFiles(t *testing.T) {
	text := "# Test\n\n## Direct files likely changed\n\n- src/foo.ts\n- src/bar.ts\n"
	meta := ParseHandoffMetadata(text, "[]")
	if len(meta.ScopedFiles) != 2 {
		t.Fatalf("expected 2 scoped files, got %d", len(meta.ScopedFiles))
	}
}

func TestParseHandoffMetadataScopedFilesDeduplicates(t *testing.T) {
	text := "# Test\n\n## Scope\n\n- `internal/foo.go`\n- internal/foo.go\n"
	meta := ParseHandoffMetadata(text, "[]")
	if len(meta.ScopedFiles) != 1 {
		t.Fatalf("expected 1 unique file, got %d", len(meta.ScopedFiles))
	}
}

func TestParseHandoffMetadataValidationCommands(t *testing.T) {
	text := "# Test\n\n## Tests / validation\n\n" + "```bash\ngo test ./...\n" + "```\n"
	meta := ParseHandoffMetadata(text, "[]")
	if len(meta.ValidationCommands) != 1 {
		t.Fatalf("expected 1 validation command, got %d", len(meta.ValidationCommands))
	}
}

func TestBuildIntakeReviewAllFilesExist(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "foo.go"), []byte{}, 0644)
	os.WriteFile(filepath.Join(dir, "bar.go"), []byte{}, 0644)

	meta := HandoffMetadata{
		ScopedFiles: []ScopedFile{
			{Path: "foo.go", Source: "handoff"},
			{Path: "bar.go", Source: "handoff"},
		},
	}
	review := BuildIntakeReview(meta, dir)
	if len(review.Blockers) > 0 {
		t.Fatalf("expected no blockers, got %q", review.Blockers)
	}
	if len(review.Warnings) > 0 {
		t.Fatalf("expected no warnings, got %q", review.Warnings)
	}
	for _, fc := range review.ScopedFileChecks {
		if !fc.Exists {
			t.Fatalf("expected %s to exist", fc.Path)
		}
	}
}

func TestBuildIntakeReviewNoFilesExist(t *testing.T) {
	dir := t.TempDir()
	meta := HandoffMetadata{
		ScopedFiles: []ScopedFile{
			{Path: "nonexistent.go", Source: "handoff"},
		},
	}
	review := BuildIntakeReview(meta, dir)
	hasBlocker := false
	for _, b := range review.Blockers {
		if b == "Selected repo does not appear to match handoff scope." {
			hasBlocker = true
			break
		}
	}
	if !hasBlocker {
		t.Fatalf("expected scope mismatch blocker, got blockers: %q", review.Blockers)
	}
}

func TestBuildIntakeReviewSomeFilesExist(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "foo.go"), []byte{}, 0644)

	meta := HandoffMetadata{
		ScopedFiles: []ScopedFile{
			{Path: "foo.go", Source: "handoff"},
			{Path: "bar.go", Source: "handoff"},
		},
	}
	review := BuildIntakeReview(meta, dir)
	hasWarning := false
	for _, w := range review.Warnings {
		if w == "Some scoped files were not found in the selected repo." {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Fatalf("expected missing files warning, got warnings: %q", review.Warnings)
	}
}

func TestBuildIntakeReviewPathTraversal(t *testing.T) {
	dir := t.TempDir()
	meta := HandoffMetadata{
		ScopedFiles: []ScopedFile{
			{Path: "../outside.go", Source: "handoff"},
		},
	}
	review := BuildIntakeReview(meta, dir)
	for _, fc := range review.ScopedFileChecks {
		if fc.Exists {
			t.Fatal("path traversal file should not be found")
		}
	}
}

func TestBuildIntakeReviewEmptyRepoPath(t *testing.T) {
	meta := HandoffMetadata{}
	review := BuildIntakeReview(meta, "")
	hasBlocker := false
	for _, b := range review.Blockers {
		if b == "Selected repo path is missing." {
			hasBlocker = true
			break
		}
	}
	if !hasBlocker {
		t.Fatalf("expected missing repo path blocker")
	}
}

func TestBuildIntakeReviewNoScopedFilesNoBlocker(t *testing.T) {
	dir := t.TempDir()
	meta := HandoffMetadata{}
	review := BuildIntakeReview(meta, dir)
	if len(review.Blockers) > 0 {
		t.Fatalf("expected no blockers when no scoped files, got %q", review.Blockers)
	}
}
