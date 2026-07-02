package intake

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func validHandoffMarkdown(title, repo, branch string) string {
	return fmt.Sprintf(`---
title: %s
repo: %s
branch: %s
recommended_model: deepseek-v4-flash
---

<decision_log>
- D1: Use deterministic preflight for compile-readiness checks.
</decision_log>

<constraints>
- C1: Do not create runs from preflight tool.
</constraints>

<compiler_input>
`+"```"+`yaml
compiler_input:
  goal: "Verify preflight works correctly."
  scope: "Purely deterministic checks only."
  file_targets:
    - path: "internal/intake/preflight.go"
      role: primary
      action: must_edit
      reason: "Core preflight implementation."
  implementation_steps:
    - id: S1
      title: "Add preflight tests"
      action: modify
      target_paths:
        - "internal/intake/preflight_test.go"
      instructions: "Write comprehensive tests for all failure cases."
      acceptance_criteria:
        - "Tests pass."
  code_requirements:
    - id: CR1
      requirement: "Preflight must be reusable by both standalone MCP validation and run submission."
      applies_to:
        - "internal/intake/preflight.go"
  validation_contract:
    mode: commands
    failure_policy: block
    commands:
      - command: "go test ./internal/intake -run Preflight -count=1"
        required: true
  completion_contract:
    done_when:
      - "All tests pass."
    blocked_when:
      - "Tests fail."
`+"```"+`
</compiler_input>
`, title, repo, branch)
}

func handoffWithoutSection(title, repo, branch, missingSection string) string {
	base := validHandoffMarkdown(title, repo, branch)
	var result strings.Builder
	lines := strings.Split(base, "\n")
	skipUntilNextTag := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "<"+missingSection+">") {
			skipUntilNextTag = true
			continue
		}
		if skipUntilNextTag {
			if strings.Contains(trimmed, "</"+missingSection+">") {
				skipUntilNextTag = false
			}
			continue
		}
		result.WriteString(line)
		result.WriteString("\n")
	}
	return result.String()
}

func handoffWithInvalidYAML(title, repo, branch string) string {
	base := validHandoffMarkdown(title, repo, branch)
	return strings.Replace(base,
		"compiler_input:\n  goal:",
		"compiler_input:\n  goal: [invalid { yaml\n  scope:",
		1)
}

func handoffMissingFrontmatter() string {
	return `# No Frontmatter

<decision_log>
- D1: This handoff has no frontmatter.
</decision_log>

<constraints>
- C1: Must fail preflight.
</constraints>

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test missing frontmatter.\"\n  scope: \"Deterministic check.\"\n  file_targets:\n    - path: \"test.go\"\n  implementation_steps:\n    - id: S1\n      title: \"Step\"\n  code_requirements:\n    - id: CR1\n      requirement: \"Must pass preflight.\"\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - \"Done.\"\n```" + `
</compiler_input>`
}

func handoffMissingRepo() string {
	return `---
title: No Repo
branch: main
---

<decision_log>
- D1: This has no repo.
</decision_log>

<constraints>
- C1: Must fail preflight.
</constraints>

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test missing repo.\"\n  scope: \"Deterministic check.\"\n  file_targets:\n    - path: \"test.go\"\n  implementation_steps:\n    - id: S1\n      title: \"Step\"\n  code_requirements:\n    - id: CR1\n      requirement: \"Must pass preflight.\"\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - \"Done.\"\n```" + `
</compiler_input>`
}

func TestValidatePlannerHandoffForCompile_ValidPasses(t *testing.T) {
	markdown := validHandoffMarkdown("Valid Handoff", "test-org/test-repo", "main")
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true for valid handoff, got OK=false with issues: %v", result.Issues)
	}
	if result.Status != "passed" {
		t.Fatalf("expected status=passed, got %q", result.Status)
	}
	if !result.IsCompileReady {
		t.Fatal("expected is_compile_ready=true")
	}
	if result.SubmittedHandoffSHA256 == "" {
		t.Fatal("expected non-empty submitted_handoff_sha256")
	}
	if result.ByteCount == 0 {
		t.Fatal("expected non-zero byte_count")
	}
	for _, issue := range result.Issues {
		if issue.BlocksSubmission {
			t.Errorf("unexpected blocking issue: %s: %s", issue.Code, issue.Message)
		}
	}
}

func TestValidatePlannerHandoffForCompile_EmptyMarkdown(t *testing.T) {
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for empty markdown")
	}
	if result.Status != "blocked" {
		t.Fatalf("expected status=blocked, got %q", result.Status)
	}
	assertIssueExists(t, result.Issues, "handoff_empty")
}

func TestValidatePlannerHandoffForCompile_MissingFrontmatter(t *testing.T) {
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: handoffMissingFrontmatter(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for missing frontmatter")
	}
	assertIssueExists(t, result.Issues, "frontmatter_missing")
}

func TestValidatePlannerHandoffForCompile_MissingRepo(t *testing.T) {
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: handoffMissingRepo(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for missing repo")
	}
	assertIssueExists(t, result.Issues, "repository_target_missing")
}

func TestValidatePlannerHandoffForCompile_MissingDecisionLog(t *testing.T) {
	markdown := handoffWithoutSection("No Decision Log", "test-org/repo", "main", "decision_log")
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("missing decision_log should not block submission, got blocked with issues: %v", result.Issues)
	}
	assertIssueExists(t, result.Issues, "semantic_section_missing")
}

func TestValidatePlannerHandoffForCompile_MissingConstraints(t *testing.T) {
	markdown := handoffWithoutSection("No Constraints", "test-org/repo", "main", "constraints")
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("missing constraints should not block submission, got blocked with issues: %v", result.Issues)
	}
	assertIssueExists(t, result.Issues, "semantic_section_missing")
}

func TestValidatePlannerHandoffForCompile_MissingCompilerInput(t *testing.T) {
	markdown := `---
title: No Compiler Input
repo: test-org/repo
branch: main
---

<decision_log>
- D1: Test decision.
</decision_log>

<constraints>
- C1: Test constraint.
</constraints>
`
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for missing compiler_input")
	}
	assertIssueExists(t, result.Issues, "compiler_input_missing")
}

func TestValidatePlannerHandoffForCompile_InvalidYAML(t *testing.T) {
	markdown := handoffWithInvalidYAML("Invalid YAML", "test-org/repo", "main")
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for invalid compiler_input YAML")
	}
	assertIssueExists(t, result.Issues, "compiler_input_yaml_invalid")
}

func TestValidatePlannerHandoffForCompile_EmptyFileTargets(t *testing.T) {
	markdown := `---
title: No File Targets
repo: test-org/repo
branch: main
---

<decision_log>
- D1: Test decision.
</decision_log>

<constraints>
- C1: Test constraint.
</constraints>

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test\"\n  scope: \"Test\"\n  file_targets:\n  implementation_steps:\n    - id: S1\n      title: \"Step\"\n  code_requirements:\n    - id: CR1\n      requirement: \"Requirement\"\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - \"Done\"\n```" + `
</compiler_input>`
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for empty file_targets")
	}
	assertIssueExists(t, result.Issues, "compiler_input_list_empty")
}

func TestValidatePlannerHandoffForCompile_EmptyImplementationSteps(t *testing.T) {
	markdown := `---
title: Empty Implementation Steps
repo: test-org/repo
branch: main
---

<decision_log>
- D1: Test decision.
</decision_log>

<constraints>
- C1: Test constraint.
</constraints>

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test\"\n  scope: \"Test\"\n  file_targets:\n    - path: \"test.go\"\n  implementation_steps:\n  code_requirements:\n    - id: CR1\n      requirement: \"Requirement\"\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - \"Done\"\n```" + `
</compiler_input>`
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for empty implementation_steps")
	}
	assertIssueExists(t, result.Issues, "compiler_input_list_empty")
}

func TestValidatePlannerHandoffForCompile_MissingValidationContract(t *testing.T) {
	markdown := `---
title: Missing Validation Contract
repo: test-org/repo
branch: main
---

<decision_log>
- D1: Test decision.
</decision_log>

<constraints>
- C1: Test constraint.
</constraints>

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test\"\n  scope: \"Test\"\n  file_targets:\n    - path: \"test.go\"\n  implementation_steps:\n    - id: S1\n      title: \"Step\"\n  code_requirements:\n    - id: CR1\n      requirement: \"Requirement\"\n  validation_contract:\n  completion_contract:\n    done_when:\n      - \"Done\"\n```" + `
</compiler_input>`
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for missing validation_contract")
	}
	assertIssueExists(t, result.Issues, "compiler_input_required_field_missing")
}

func TestValidatePlannerHandoffForCompile_MissingCompletionContract(t *testing.T) {
	markdown := `---
title: Missing Completion Contract
repo: test-org/repo
branch: main
---

<decision_log>
- D1: Test decision.
</decision_log>

<constraints>
- C1: Test constraint.
</constraints>

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test\"\n  scope: \"Test\"\n  file_targets:\n    - path: \"test.go\"\n  implementation_steps:\n    - id: S1\n      title: \"Step\"\n  code_requirements:\n    - id: CR1\n      requirement: \"Requirement\"\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n```" + `
</compiler_input>`
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for missing completion_contract")
	}
	assertIssueExists(t, result.Issues, "compiler_input_required_field_missing")
}

func TestValidatePlannerHandoffForCompile_PlanPassMismatch(t *testing.T) {
	markdown := `---
title: Mismatch Handoff
repo: test-org/repo
branch: main
managed_plan_pass: PASS-001
---

<decision_log>
- D1: Test decision.
</decision_log>

<constraints>
- C1: Test constraint.
</constraints>

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test\"\n  scope: \"Test\"\n  file_targets:\n    - path: \"test.go\"\n  implementation_steps:\n    - id: S1\n      title: \"Step\"\n  code_requirements:\n    - id: CR1\n      requirement: \"Requirement\"\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - \"Done\"\n```" + `
</compiler_input>`
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
		PlanID:   "test-plan",
		PassID:   "PASS-002",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected OK=false for managed_plan_pass mismatch")
	}
	assertIssueExists(t, result.Issues, "managed_pass_mismatch")
}

func TestValidatePlannerHandoffForCompile_StructFields(t *testing.T) {
	markdown := validHandoffMarkdown("Struct Test", "test-org/repo", "main")
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown:         markdown,
		SourceMode:       "file_parameter",
		PlanID:           "test-plan",
		PassID:           "PASS-001",
		ContextPacketID:  "ctx-123",
		SourceSnapshotID: "snap-456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SourceMode != "file_parameter" {
		t.Errorf("expected SourceMode file_parameter, got %q", result.SourceMode)
	}
	if result.PlanID != "test-plan" {
		t.Errorf("expected PlanID test-plan, got %q", result.PlanID)
	}
	if result.PassID != "PASS-001" {
		t.Errorf("expected PassID PASS-001, got %q", result.PassID)
	}
	if result.ContextPacketID != "ctx-123" {
		t.Errorf("expected ContextPacketID ctx-123, got %q", result.ContextPacketID)
	}
	if result.SourceSnapshotID != "snap-456" {
		t.Errorf("expected SourceSnapshotID snap-456, got %q", result.SourceSnapshotID)
	}
	if result.GeneratedAt == "" {
		t.Error("expected non-empty generated_at")
	}
	if result.ByteCount == 0 {
		t.Error("expected non-zero byte_count")
	}
}

func TestValidatePlannerHandoffForCompile_RejectsFullPayload(t *testing.T) {
	markdown := validHandoffMarkdown("Payload Rejection", "test-org/repo", "main")
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	resultJSON := string(b)
	if strings.Contains(resultJSON, strings.TrimSpace(markdown)) {
		t.Error("result JSON must not contain the full handoff markdown body")
	}
}

func TestValidatePlannerHandoffForCompile_IssueCodes(t *testing.T) {
	markdown := `---
title: Issue Code Test
repo: test-org/repo
branch: main
---

<compiler_input>
` + "```yaml\ncompiler_input:\n  goal: \"Test\"\n  scope: \"Test\"\n  file_targets:\n  implementation_steps:\n  code_requirements:\n  validation_contract:\n  completion_contract:\n```" + `
</compiler_input>`
	result, err := ValidatePlannerHandoffForCompile(HandoffPreflightInput{
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, issue := range result.Issues {
		if issue.Code == "" {
			t.Errorf("issue %d has empty code", i)
		}
		if issue.Severity == "" {
			t.Errorf("issue %d has empty severity", i)
		}
		if issue.Message == "" {
			t.Errorf("issue %d has empty message", i)
		}
		if issue.RepairGuidance == "" {
			t.Errorf("issue %d has empty repair_guidance", i)
		}
		if issue.BlocksSubmission && issue.Severity != SeverityError {
			t.Errorf("issue %d blocks_submission but severity is %q instead of error", i, issue.Severity)
		}
	}
}

func assertIssueExists(t *testing.T, issues []HandoffPreflightIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Errorf("expected issue with code %q, but it was not found. Issues: %v", code, issues)
}
