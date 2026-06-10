package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func noValidationSectionHandoff() string {
	return `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md
- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Agent final output requirement

Return DONE or BLOCKED.
`
}

func withValidationSectionHandoff() string {
	return `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md
- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
}

func setupRepoWithFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo"), 0644)
	return dir
}

func TestBuildIntakeReviewMissingValidationSectionIsWarningOnly(t *testing.T) {
	repoDir := setupRepoWithFiles(t)
	metadata := ParseHandoffMetadata(noValidationSectionHandoff(), "")
	if len(metadata.ValidationCommands) != 0 {
		t.Fatalf("expected no validation commands, got %d", len(metadata.ValidationCommands))
	}

	review := BuildIntakeReview(metadata, repoDir)

	if len(review.Blockers) > 0 {
		t.Fatalf("expected no blockers for missing validation section, got %v", review.Blockers)
	}

	foundWarning := false
	for _, w := range review.Warnings {
		if contains(w, "validation") || contains(w, "validation commands") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("expected a warning about missing validation commands, got warnings: %v", review.Warnings)
	}
}

func TestBuildIntakeReviewMissingValidationCommandsDoesNotBlockPrompt(t *testing.T) {
	repoDir := setupRepoWithFiles(t)
	metadata := ParseHandoffMetadata(noValidationSectionHandoff(), "")
	if len(metadata.ValidationCommands) != 0 {
		t.Fatalf("expected no validation commands, got %d", len(metadata.ValidationCommands))
	}

	review := BuildIntakeReview(metadata, repoDir)

	if len(review.Blockers) > 0 {
		t.Fatalf("expected no blockers for missing validation commands, got %v", review.Blockers)
	}
}

func TestBuildIntakeReviewWithValidationCommandsNoWarning(t *testing.T) {
	repoDir := setupRepoWithFiles(t)
	metadata := ParseHandoffMetadata(withValidationSectionHandoff(), "")
	if len(metadata.ValidationCommands) == 0 {
		t.Fatalf("expected validation commands, got none")
	}

	review := BuildIntakeReview(metadata, repoDir)

	for _, w := range review.Warnings {
		if contains(w, "validation") || contains(w, "validation commands") {
			t.Fatalf("unexpected validation warning when commands exist: %s", w)
		}
	}
}

func TestBuildIntakeReviewMissingRepoStillBlocks(t *testing.T) {
	metadata := ParseHandoffMetadata(noValidationSectionHandoff(), "")

	review := BuildIntakeReview(metadata, "")

	found := false
	for _, b := range review.Blockers {
		if contains(b, "repo path") || contains(b, "Repo path") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected blocker for missing repo path, got blockers: %v", review.Blockers)
	}
}

func TestBuildIntakeReviewMissingOutputContractStillShows(t *testing.T) {
	handoff := `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md

## Do not change

- Nothing.

## Task checklist

- [ ] Do it
`
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)

	metadata := ParseHandoffMetadata(handoff, "")
	review := BuildIntakeReview(metadata, repoDir)

	if len(review.Blockers) > 0 {
		t.Fatalf("expected no blockers for missing output contract, got %v", review.Blockers)
	}

	if review.Metadata.FinalOutputContract != "" {
		t.Fatal("expected empty output contract")
	}
}

func TestBuildIntakeReviewWithRepoDefaultsNoWarning(t *testing.T) {
	repoDir := setupRepoWithFiles(t)
	repoDefaults := `["go test ./..."]`
	metadata := ParseHandoffMetadata(noValidationSectionHandoff(), repoDefaults)
	if len(metadata.ValidationCommands) == 0 {
		t.Fatal("expected validation commands from repo defaults")
	}

	review := BuildIntakeReview(metadata, repoDir)

	for _, w := range review.Warnings {
		if contains(w, "validation") || contains(w, "validation commands") {
			t.Fatalf("unexpected validation warning when repo defaults exist: %s", w)
		}
	}
}

func TestPreparePromptAllowedWithValidationWarnings(t *testing.T) {
	handoffText := noValidationSectionHandoff()
	prompt := PreparePrompt(handoffText)
	if prompt == "" {
		t.Fatal("expected non-empty prompt even with missing validation section")
	}
	if !contains(prompt, "Test Handoff") {
		t.Fatal("expected prompt to contain handoff title")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
