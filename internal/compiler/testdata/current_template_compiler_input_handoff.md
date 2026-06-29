# Planner Handoff: Current Template Compiler Input

## Artifact Metadata

```yaml
handoff_id: planner-handoff-2026-06-27-current-template-compiler-input
schema_version: 1.0.0
created_at: 2026-06-27T00:00:00Z
target_executor: deepseek-v4-flash
repo_target: Paintersrp/relay
branch_context: main
content_profile: remediation
```

<context_snapshot>
Relay must compile the source-controlled planner handoff template without duplicate prose aliases.
</context_snapshot>

<decision_log>
- D1: Parse the structured compiler input YAML.
  Rationale: It is the current handoff contract.
</decision_log>

<constraints>
- C1: Keep canonical packet validation strict.
  Applies to: packet_compiler
</constraints>

<pass_boundary>
```yaml
current_pass: 1
total_planned_passes: 1
this_pass_scope: Parser alignment only.
out_of_scope_for_this_pass:
  - UI changes
depends_on_packet_id: ""
```
</pass_boundary>

<compiler_input>
```yaml
compiler_input:
  goal: "Compile structured compiler input YAML into a canonical packet."
  scope: "Update parser behavior and regression coverage only."
  non_goals:
    - "Do not change the canonical packet schema."
  file_targets:
    - path: "internal/compiler/compiler.go"
      role: "primary"
      action: "must_edit"
      reason: "Owns compiler input parsing."
      grounding: "Template-only note that must not appear in canonical output."
  implementation_steps:
    - id: "STEP-001"
      action: "modify"
      target_paths:
        - "internal/compiler/compiler.go"
      instructions: |
        Parse the fenced YAML block rooted at compiler_input before legacy fallback.
        Preserve existing heading-style parsing for older handoffs.
      acceptance_criteria:
        - "Structured YAML fields populate execution_payload."
  code_requirements:
    - id: "REQ-001"
      applies_to:
        - "internal/compiler/compiler.go"
      requirement: |
        Structured compiler input YAML must populate file targets, implementation steps, and code requirements.
  validation_contract:
    mode: "commands"
    failure_policy: "block"
    commands:
      - id: "CHECK-1"
        command: "go test ./internal/compiler"
        required: true
        purpose: "Verify compiler parser regression coverage."
        success_signal: "Command exits 0."
        failure_handling: "block_if_fails"
      - id: "CHECK-2"
        command: "make validate"
        required: false
        purpose: "Advisory final/full validation compatibility evidence."
        success_signal: "Command exits 0 and writes full validation evidence."
        failure_handling: "report_if_fails"
  completion_contract:
    done_when:
      - "The YAML-only fixture compiles successfully."
    blocked_when:
      - "The compiler cannot parse structured YAML."
```
</compiler_input>

<audit_priorities>
- A1: Confirm the generated packet is schema-valid.
</audit_priorities>
