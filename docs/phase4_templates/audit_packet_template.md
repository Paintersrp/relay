# Audit Packet: {{packet_meta.task_slug}}

Schema companion:

```text
handoffs/schema/audit_packet.schema.json
```

Contract:

```text
handoffs/contracts/audit_packet_contract.md
```

## Artifact Metadata

- Packet ID: `{{packet_meta.packet_id}}`
- Canonical packet: `{{packet_meta.artifact_paths.canonical_packet}}`
- Executor brief: `{{packet_meta.artifact_paths.executor_brief}}`
- Executor result: `{{packet_meta.artifact_paths.executor_result}}`
- Validation report: `{{packet_meta.artifact_paths.validation_report}}`
- Audit packet: `{{packet_meta.artifact_paths.audit_packet}}`
- Target executor: `{{packet_meta.target_executor}}`
- Repo target: `{{packet_meta.repo_target}}`
- Branch/context: `{{packet_meta.branch_context}}`
- Generated at: `{{generated_at}}`

## Audit Status

Audit status: `{{audit_status}}`

Allowed values:

```text
pass
pass_with_warnings
needs_revision
blocked
```

## Original Goal

{{execution_payload.goal}}

## Scope

{{execution_payload.scope}}

## Non-goals

{{#execution_payload.non_goals}}

- {{.}}
  {{/execution_payload.non_goals}}

## Expected Behavior

{{#execution_payload.expected_behavior}}

- {{.}}
  {{/execution_payload.expected_behavior}}

## Executor Result Summary

```text
STATUS: {{executor_result.status}}
BUILD: {{executor_result.build}}
TESTS: {{executor_result.tests}}
```

## Executor Validation Evidence

{{#executor_result.validation}}

- `{{name}}`: `{{status}}` — {{evidence}}
  {{/executor_result.validation}}

## Changed Files

{{#executor_result.changed_files}}

- `{{.}}`
  {{/executor_result.changed_files}}

If no files changed:

```text
none
```

## Scope Review

### In-scope findings

{{#scope_review.in_scope_findings}}

- {{.}}
  {{/scope_review.in_scope_findings}}

### Potential scope drift

{{#scope_review.scope_drift_findings}}

- Severity: `{{severity}}`
  - Finding: {{finding}}
  - Evidence: {{evidence}}
  - Recommended disposition: {{recommended_disposition}}
    {{/scope_review.scope_drift_findings}}

If no scope drift is found:

```text
No scope drift detected from available evidence.
```

## Non-goal Review

{{#non_goal_review}}

- Non-goal: {{non_goal}}
  - Result: `{{result}}`
  - Evidence: {{evidence}}
    {{/non_goal_review}}

Allowed result values:

```text
respected
possibly_violated
violated
unknown
```

## File-Scope Review

{{#file_scope_review}}

- File: `{{path}}`
  - Listed in packet: `{{listed_in_packet}}`
  - Packet action: `{{packet_action}}`
  - Executor changed: `{{executor_changed}}`
  - Review result: `{{review_result}}`
  - Evidence: {{evidence}}
    {{/file_scope_review}}

Allowed review result values:

```text
allowed
allowed_but_review
unexpected
forbidden
unknown
```

## Acceptance Criteria Review

{{#acceptance_criteria_review}}

- Step: `{{step_id}}`
  - Criterion: {{criterion}}
  - Result: `{{result}}`
  - Evidence: {{evidence}}
    {{/acceptance_criteria_review}}

Allowed result values:

```text
met
not_met
unknown
not_applicable
```

## Validation Review

{{#validation_review}}

- Command/check: `{{command_or_check}}`
  - Required: `{{required}}`
  - Result: `{{result}}`
  - Evidence: {{evidence}}
  - Disposition: `{{disposition}}`
    {{/validation_review}}

## Risk Review

{{#risk_review}}

- Risk: `{{risk_id}}`
  - Original severity: `{{severity}}`
  - Mitigation expected: {{mitigation}}
  - Observed result: {{observed_result}}
  - Residual concern: {{residual_concern}}
    {{/risk_review}}

## Diff Review

### Diff summary

{{diff_summary}}

### Suspicious or review-worthy diff areas

{{#diff_review_items}}

- File: `{{path}}`
  - Concern: {{concern}}
  - Evidence: {{evidence}}
  - Severity: `{{severity}}`
    {{/diff_review_items}}

If no suspicious diff areas are found:

```text
No suspicious diff areas detected from available evidence.
```

## Manual Review Checklist

{{#audit_seed.manual_review_checklist}}

- [ ] {{.}}
      {{/audit_seed.manual_review_checklist}}

## Auditor Decision

Decision: `{{auditor_decision}}`

Allowed values:

```text
accept
accept_with_followup
request_revision_packet
reject
blocked
```

## Revision Guidance

Only include when decision is not `accept`.

{{revision_guidance}}

Rules:

- Do not turn revision guidance into direct execution instructions.
- If implementation changes are needed, recommend a new Planner handoff / canonical packet.
- Keep guidance grounded in packet scope, validation evidence, and diff evidence.

## Source Artifacts Used

{{#source_artifacts}}

- `{{path}}` — {{description}}
  {{/source_artifacts}}

## Audit Notes

{{audit_notes}}
