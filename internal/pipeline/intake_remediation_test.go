package pipeline

import (
	"strings"
	"testing"
)

func TestBuildIntakeRemediationHandoffIncludesRunID(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      42,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{"No validation commands found."},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "Run ID: 42") {
		t.Fatal("expected output to contain Run ID: 42")
	}
}

func TestBuildIntakeRemediationHandoffIncludesWarnings(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{"No validation commands found.", "Some scoped files were not found in the selected repo."},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "No validation commands found.") {
		t.Fatal("expected output to contain first warning")
	}
	if !strings.Contains(output, "Some scoped files were not found in the selected repo.") {
		t.Fatal("expected output to contain second warning")
	}
}

func TestBuildIntakeRemediationHandoffIncludesBlockers(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{},
		Blockers:   []string{"Selected repo path is missing.", "Selected repo does not appear to match handoff scope."},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "Selected repo path is missing.") {
		t.Fatal("expected output to contain first blocker")
	}
	if !strings.Contains(output, "Selected repo does not appear to match handoff scope.") {
		t.Fatal("expected output to contain second blocker")
	}
}

func TestBuildIntakeRemediationHandoffIncludesScopedFiles(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:       1,
		RepoName:    "test-repo",
		RepoPath:    "/tmp/test-repo",
		BranchName:  "main",
		RunStatus:   "needs_review",
		Warnings:    []string{},
		Blockers:    []string{},
		ScopedFiles: []string{"README.md", "internal/foo.go", "go.mod"},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "README.md") {
		t.Fatal("expected output to contain README.md scoped file")
	}
	if !strings.Contains(output, "internal/foo.go") {
		t.Fatal("expected output to contain internal/foo.go scoped file")
	}
	if !strings.Contains(output, "go.mod") {
		t.Fatal("expected output to contain go.mod scoped file")
	}
}

func TestBuildIntakeRemediationHandoffNoScopedFiles(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "None detected") {
		t.Fatal("expected 'None detected' when no scoped files")
	}
}

func TestBuildIntakeRemediationHandoffMissingValidationWarningAddsCommandsSection(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{"No validation commands found. Agent execution can continue, but Relay Validation will be unavailable until validation commands are added to the handoff or repo defaults."},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "## Relay validation commands") {
		t.Fatal("expected ## Relay validation commands section when missing validation commands warning present")
	}
	if !strings.Contains(output, "go fmt ./...") {
		t.Fatal("expected go fmt ./... in remediation handoff for missing validation commands")
	}
	if !strings.Contains(output, "templ generate") {
		t.Fatal("expected templ generate in remediation handoff for missing validation commands")
	}
	if !strings.Contains(output, "go test ./...") {
		t.Fatal("expected go test ./... in remediation handoff")
	}
	if !strings.Contains(output, "go vet ./...") {
		t.Fatal("expected go vet ./... in remediation handoff")
	}
}

func TestBuildIntakeRemediationHandoffDoesNotOverwriteOriginal(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{"No validation commands found."},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if strings.Contains(output, "# Relay Intake Review Remediation Surgical Implementation") {
		// Good - this is the remediation handoff title, not the original handoff
	} else {
		t.Fatal("expected remediation handoff title")
	}
}

func TestBuildIntakeRemediationHandoffHasDoNotChangeSection(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{"No validation commands found."},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "## Do not change") {
		t.Fatal("expected ## Do not change section in remediation handoff")
	}
}

func TestBuildIntakeRemediationHandoffHasExpectedResult(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{"No validation commands found."},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "## Expected result") {
		t.Fatal("expected ## Expected result section")
	}
}

func TestBuildIntakeRemediationHandoffHasFinalOutputRequirement(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
		Warnings:   []string{},
		Blockers:   []string{},
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "## Agent final output requirement") {
		t.Fatal("expected ## Agent final output requirement section")
	}
}

func TestBuildIntakeRemediationHandoffHasContextSection(t *testing.T) {
	input := IntakeRemediationInput{
		RunID:      1,
		RepoName:   "test-repo",
		RepoPath:   "/tmp/test-repo",
		BranchName: "main",
		RunStatus:  "needs_review",
	}

	output := BuildIntakeRemediationHandoff(input)
	if !strings.Contains(output, "## Context") {
		t.Fatal("expected ## Context section")
	}
	if !strings.Contains(output, "Repo: test-repo") {
		t.Fatal("expected Repo in context section")
	}
	if !strings.Contains(output, "Branch/worktree: main") {
		t.Fatal("expected Branch/worktree in context section")
	}
}
