---
name: audit-managed-pass
description: Audit completed managed-pass work against its selected Planner handoff and evidence.
triggers:
  - audit pass
  - managed pass audit
  - audit packet
  - closeout
edges:
  - target: context/pipeline-contracts.md
    condition: for audit packet and evidence contract pointers
  - target: context/managed-plans.md
    condition: for pass/run association and completion readiness
last_updated: 2026-06-23
---

# Audit Managed Pass

## Context

Audit the selected pass against its handoff goal, scope, non-goals, acceptance criteria, validation contract, and produced artifacts. Relay DB state and filesystem artifacts are evidence; `.mex` is not evidence.

## Steps

1. Load the selected pass handoff and relevant relay-contracts audit packet contract/template.
2. Inspect actual changed files, generated artifacts, validation reports, and run outputs.
3. Confirm implementation stayed inside the selected pass scope.
4. Compare validation commands requested by the handoff with commands actually run.
5. Record DONE/BLOCKED status and evidence gaps without inventing missing evidence.

## Gotchas

- Do not mark future-pass non-goals as required failures unless the selected pass required them.
- Do not accept a UI-only signal as lifecycle truth when backend state/artifacts disagree.
- Missing validation output is an evidence gap, not something to infer.

## Verify

- Contract-required audit fields are present.
- Every acceptance criterion is backed by source, command output, or artifact evidence.
- Any blocker/error is concrete and tied to the selected pass.

## Update Scaffold

- [ ] Update this pattern if a repeated audit evidence gap appears.
