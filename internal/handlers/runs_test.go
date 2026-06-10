package handlers

import (
	"testing"

	"relay/internal/pipeline"
	"relay/internal/store"
)

func TestDefaultActiveRunStep_StartsAtIntakeAfterAutoSetup(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
	}
	checks := []store.Check{
		{Kind: "validation", Status: "pass"},
	}

	got := defaultActiveRunStep(artifacts, checks)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeForFreshRun(t *testing.T) {
	got := defaultActiveRunStep(nil, nil)
	if got != "intake" {
		t.Fatalf("expected intake for fresh run, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeForArtifactsOnly(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
	}
	got := defaultActiveRunStep(artifacts, nil)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeAfterAgentResult(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
		{Kind: "agent_result_raw"},
	}

	got := defaultActiveRunStep(artifacts, nil)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeAfterValidationRun(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
		{Kind: "validation_run_json"},
	}

	got := defaultActiveRunStep(artifacts, nil)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeAfterValidationRunCheck(t *testing.T) {
	checks := []store.Check{
		{Kind: "validation_run", Status: "pass"},
	}

	got := defaultActiveRunStep(nil, checks)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestNormalizeRunStepAcceptsKnownSteps(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"intake", "intake"},
		{"prompt", "prompt"},
		{"packet", "packet"},
		{"handoff", "handoff"},
		{"result", "result"},
		{"validation", "validation"},
		{"audit", "audit"},
	}
	for _, tt := range tests {
		got := normalizeRunStep(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeRunStep(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeRunStepRejectsInvalidStep(t *testing.T) {
	tests := []string{
		"",
		"nonsense",
		"invalid",
		"step",
		"  ",
		"intake ",
	}
	for _, input := range tests {
		got := normalizeRunStep(input)
		if got != "intake" {
			t.Errorf("normalizeRunStep(%q) = %q, want %q", input, got, "intake")
		}
	}
}

func TestNormalizeRunStepHandoffIsRealStep(t *testing.T) {
	got := normalizeRunStep("handoff")
	if got != "handoff" {
		t.Fatalf("normalizeRunStep(%q) = %q, want %q", "handoff", got, "handoff")
	}
}

func TestHasArtifactKind_ReturnsTrueWhenFound(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "agent_prompt"},
	}
	if !hasArtifactKind(artifacts, "agent_prompt") {
		t.Error("expected true for existing artifact kind")
	}
}

func TestHasArtifactKind_ReturnsFalseWhenNotFound(t *testing.T) {
	if hasArtifactKind(nil, "agent_prompt") {
		t.Error("expected false for nil slice")
	}
}

func TestHasCheckKind_ReturnsTrueWhenFound(t *testing.T) {
	checks := []store.Check{
		{Kind: "validation_run", Status: "pass"},
	}
	if !hasCheckKind(checks, "validation_run") {
		t.Error("expected true for existing check kind")
	}
}

func TestHasCheckKind_ReturnsFalseWhenNotFound(t *testing.T) {
	if hasCheckKind(nil, "validation_run") {
		t.Error("expected false for nil slice")
	}
}

func TestHasValidationCommandsForPreviewFromHandoff(t *testing.T) {
	// Verify that an unwrapped command in a bash fence under "## Tests / validation" is detected
	handoff := "# Test\n\n## Tests / validation\n\n" + "```bash\n" + "go test ./...\n" + "```\n"
	if !hasValidationCommandsForPreview(handoff, "") {
		t.Fatal("expected validation commands from handoff")
	}
}

func TestHasValidationCommandsForPreviewFromRepoDefaults(t *testing.T) {
	// When handoff has no commands, repo defaults should be used
	if !hasValidationCommandsForPreview("# Test", "[\"go test ./...\"]") {
		t.Fatal("expected validation commands from repo defaults")
	}
}

func TestHasValidationCommandsForPreviewMissing(t *testing.T) {
	if hasValidationCommandsForPreview("# Test", "") {
		t.Fatal("expected no validation commands")
	}
}

func TestHasValidationCommandsForPreviewFallsBackToRepoDefaults(t *testing.T) {
	// Full integration-style test that handoff metadata parsing falls back to defaults
	handoff := "# Test\n\nNo validation section here.\n"
	repoDefaults := "[\"npm run build\"]"
	commands := pipeline.ExtractValidationCommands(handoff, repoDefaults)
	if len(commands) != 1 {
		t.Fatalf("expected 1 command from repo defaults, got %d", len(commands))
	}
	if commands[0].Source != "repo_default" {
		t.Fatalf("expected source 'repo_default', got %q", commands[0].Source)
	}
}
