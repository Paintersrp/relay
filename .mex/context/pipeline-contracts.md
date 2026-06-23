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

## Important Paths

- `relay-contracts/agents/instructions/planner_agent_instructions.md`
- `relay-contracts/agents/knowledge/planner_github_knowledge_manifest.json`
- `relay-contracts/contracts/planner_handoff_markdown_contract.md`
- `relay-contracts/contracts/planner_pass_plan_contract.md`
- `relay-contracts/contracts/planner_to_compiler_contract.md`
- `relay-contracts/contracts/canonical_packet_contract.md`
- `relay-contracts/contracts/executor_brief_contract.md`
- `relay-contracts/contracts/executor_result_contract.md`
- `relay-contracts/contracts/audit_packet_contract.md`
- `relay-contracts/contracts/pipeline_artifact_model.md`
- `relay-contracts/schema/planner_pass_plan.schema.json`
- `relay-contracts/schema/canonical_packet.schema.json`
- `relay-contracts/schema/executor_result.schema.json`
- `relay-contracts/schema/audit_packet.schema.json`
- `relay-contracts/policies/*`
- `relay-contracts/templates/*`

## Implementation Implications

- Planner handoffs must stay scoped to the selected pass and must not implement future-pass work.
- Plan/pass JSON validation should use contract schemas and preserve contract field names.
- Canonical packets, executor briefs/results, validation reports, repair prompts, and audit packets should follow relay-contracts templates and schemas.
- Large artifact contents remain on disk; Relay stores artifact metadata and paths in SQLite.

## Drift Guard

If a brief, chat instruction, or `.mex` note conflicts with relay-contracts or checked-out source, follow relay-contracts/source and update `.mex/context/decisions.md` with the stale claim.
