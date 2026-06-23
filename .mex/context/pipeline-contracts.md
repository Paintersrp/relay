---
name: pipeline-contracts
description: Source-of-truth pointers and implementation implications for Relay planner/pipeline contracts.
triggers:
  - relay-contracts
  - planner handoff
  - canonical packet
  - audit packet
  - contract
edges:
  - target: context/architecture.md
    condition: when placing contracts in the Relay run workflow
  - target: context/decisions.md
    condition: when authority or stale memory is disputed
  - target: patterns/create-planner-pass-handoff.md
    condition: when producing a selected pass handoff
  - target: patterns/audit-managed-pass.md
    condition: when auditing completed pass work
last_updated: 2026-06-23
---

# Pipeline Contracts

## Source-of-Truth Rule

`relay-contracts/` is authoritative for Planner/pipeline behavior. `.mex` summarizes implementation-facing implications and points to source files; it must not duplicate whole contract documents.

## Important Source-Controlled Contract Paths

Canonical contract repository: `Paintersrp/relay-contracts`.

Fetch these from that repository when producing Planner handoffs, auditing contract behavior, or discussing current Planner/pipeline rules:

- `agents/knowledge/planner_github_knowledge_manifest.json`
- `agents/instructions/planner_agent_instructions.md`
- `contracts/planner_to_compiler_contract.md`
- `contracts/planner_mcp_run_submission_contract.md`
- `contracts/planner_mcp_plan_submission_contract.md`
- `contracts/planner_pass_plan_contract.md`
- `contracts/pipeline_artifact_model.md`
- `templates/planner_handoff_template.md`
- `schema/planner_handoff_manifest.schema.json`
- `schema/canonical_packet.schema.json`
- `schema/planner_pass_plan.schema.json`
- `policies/artifact_naming_policy.md`
- `policies/pipeline_lifecycle_policy.md`
- `policies/security_redaction_policy.md`
- `policies/human_approval_gate_policy.md`

Do not assume these files exist inside `Paintersrp/relay` unless they are intentionally vendored or mounted.

## Implementation Implications

- Planner handoffs must stay scoped to the selected pass and must not implement future-pass work.
- Plan/pass JSON validation should use contract schemas and preserve contract field names.
- Canonical packets, executor briefs/results, validation reports, repair prompts, and audit packets should follow relay-contracts templates and schemas.
- Large artifact contents remain on disk; Relay stores artifact metadata and paths in SQLite.

## Drift Guard

If a brief, chat instruction, or `.mex` note conflicts with relay-contracts or checked-out source, follow relay-contracts/source and update `.mex/context/decisions.md` with the stale claim.
