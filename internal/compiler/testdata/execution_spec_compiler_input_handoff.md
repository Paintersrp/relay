---
title: Runtime compatibility path fixture
repo_target: Paintersrp/relay
branch_context: main
target_executor: opencode
created_at: 2026-07-01T12:00:00Z
---

# Planner Handoff

<handoff_meta>
handoff_id: planner-handoff-2026-07-01-runtime-compatibility-path
repo_target: Paintersrp/relay
branch_context: main
target_executor: opencode
content_profile: implementation_ready
created_at: 2026-07-01T12:00:00Z
</handoff_meta>

<context_snapshot>
Reviewed handoff contains a selected-pass Execution Spec and intentionally omits structured compiler input.
</context_snapshot>

<decision_log>
- D1: Use the Execution Spec compatibility path.
  Rationale: Selected-pass executable content is already normalized in the embedded artifact.
</decision_log>

<constraints>
- C1: Do not change canonical packet schema.
  Applies to: packet_compiler
</constraints>

<assumptions>
- AS1: The embedded Execution Spec is selected-pass scoped.
  If false: block
</assumptions>

<known_repo_facts>
- F1: Compiler and renderer runtime code consume canonical packet execution payload.
  Source: repo_inspection
</known_repo_facts>

<pass_boundary>
current_pass: 6
total_planned_passes: 6
this_pass_scope: Project selected-pass Execution Spec executable payload into canonical execution payload.
out_of_scope_for_this_pass:
  - Do not change intake API payloads.
</pass_boundary>

<audit_priorities>
- A1: Confirm upstream artifact fields do not render wholesale.
  severity_if_failed: error
</audit_priorities>

<execution_spec>
```json
{
  "execution_spec_id": "EXECSPEC-2026-07-01-runtime-compatibility-path",
  "schema_version": "1.0.0",
  "project_id": "relay",
  "source_authority": {
    "repo_target": "Paintersrp/relay",
    "branch_context": "main",
    "source_paths": [
      "handoffs/requirements/2026-07-01_runtime-compatibility-path.requirements-record.json",
      "handoffs/design/2026-07-01_runtime-compatibility-path.design-record.json"
    ]
  },
  "source_requirements": {
    "requirements_record_id": "REQREC-2026-07-01-runtime-compatibility-path",
    "requirements_record_path": "handoffs/requirements/2026-07-01_runtime-compatibility-path.requirements-record.json",
    "linked_requirement_ids": ["REQ-001"],
    "linked_acceptance_criterion_ids": ["AC-001"]
  },
  "source_design": {
    "design_record_id": "DESREC-2026-07-01-runtime-compatibility-path",
    "design_record_path": "handoffs/design/2026-07-01_runtime-compatibility-path.design-record.json",
    "linked_design_item_ids": ["DEC-001"]
  },
  "selected_pass": {
    "pass_id": "PASS-006",
    "pass_name": "Runtime compatibility path",
    "pass_scope": "Compile selected-pass Execution Spec content through canonical packet execution boundaries.",
    "pass_non_goals": ["Do not add new run creation fields."]
  },
  "execution_payload": {
    "goal": "Implement the runtime compatibility projection for selected-pass Execution Spec input.",
    "scope": "Compiler projection and renderer coverage for selected executable payload content.",
    "non_goals": ["Do not render upstream planning artifacts wholesale."],
    "file_targets": [
      {
        "path": "internal/compiler/compiler.go",
        "role": "primary",
        "action": "must_edit",
        "reason": "Owns compatibility projection."
      }
    ],
    "target_symbols": [
      {
        "path": "internal/compiler/compiler.go",
        "symbol": "projectExecutionSpecCompatibility",
        "symbol_type": "function",
        "action": "must_edit",
        "reason": "Migration-only compatibility projection."
      }
    ],
    "implementation_steps": [
      {
        "id": "EXEC-001",
        "title": "Parse embedded Execution Spec",
        "action": "modify",
        "target_paths": ["internal/compiler/compiler.go"],
        "instructions": "Parse the embedded selected-pass Execution Spec when structured compiler input is absent.",
        "acceptance_criteria": ["EXEC-001 compiles only selected execution payload fields."]
      },
      {
        "id": "EXEC-002",
        "title": "Preserve projection boundary",
        "action": "modify",
        "target_paths": ["internal/compiler/compiler.go"],
        "instructions": "Keep source requirements, source design, traceability, lint, and open questions outside canonical execution payload.",
        "acceptance_criteria": ["EXEC-002 does not leak upstream artifact objects."]
      }
    ],
    "code_requirements": [
      {
        "id": "CR-001",
        "requirement": "Projection must preserve executable code requirements in canonical execution payload.",
        "applies_to": ["internal/compiler/compiler.go"]
      },
      {
        "id": "CR-002",
        "requirement": "Projection must block when Execution Spec open questions are blocking.",
        "applies_to": ["internal/compiler/compiler.go"]
      }
    ],
    "expected_behavior": [
      "Selected-pass Execution Spec input compiles to schema-valid canonical packet execution payload."
    ],
    "validation_contract": {
      "mode": "commands",
      "failure_policy": "block",
      "commands": [
        {
          "id": "V-001",
          "command_or_check": "go test ./internal/compiler ./internal/renderer",
          "required": true,
          "purpose": "Verify compatibility projection and renderer regressions.",
          "success_signal": "Command exits 0.",
          "failure_handling": "attempt_fix_once_then_block"
        }
      ]
    },
    "completion_contract": {
      "done_when": ["Compatibility projection tests pass."],
      "blocked_when": ["Blocking Execution Spec open questions are present."],
      "allowed_discretion": ["Helper placement inside compiler.go."],
      "forbidden_discretion": ["Changing canonical packet schema."]
    },
    "executor_final_response_format": "DONE_or_BLOCKED_strict_text"
  },
  "traceability": {
    "traceability_boundary": "Pass-local trace slice only; schema validation does not perform cross-reference existence checks.",
    "links": [
      {
        "id": "TRACE-001",
        "requirement_ids": ["REQ-001"],
        "acceptance_criterion_ids": ["AC-001"],
        "design_record_id": "DESREC-2026-07-01-runtime-compatibility-path",
        "design_item_ids": ["DEC-001"],
        "pass_id": "PASS-006",
        "execution_step_ids": ["EXEC-001", "EXEC-002"],
        "code_requirement_ids": ["CR-001", "CR-002"],
        "validation_ids": ["V-001"],
        "future_finding_ids": ["FIND-001"],
        "notes": "Compact trace labels may appear in executable text."
      }
    ]
  },
  "vague_instruction_lint": {
    "status": "pass",
    "linted_sections": ["selected_pass", "execution_payload", "traceability", "open_questions"],
    "checks": [
      {
        "id": "LINT-001",
        "check_type": "mechanical_under_specification",
        "status": "pass",
        "findings": ["No blocking ambiguity detected."]
      }
    ],
    "semantic_boundary": "Mechanical under-specification and delegation-risk check only; does not decide semantic correctness."
  },
  "open_questions": [],
  "created_at": "2026-07-01T12:00:00Z"
}
```
</execution_spec>
