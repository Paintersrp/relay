---
name: create-planner-pass-handoff
description: Produce one selected pass handoff under Relay contract rules without broadening scope.
triggers:
  - planner handoff
  - selected pass
  - surgical implementation
  - pass handoff
edges:
  - target: context/pipeline-contracts.md
    condition: to locate authoritative contracts and templates
  - target: context/decisions.md
    condition: when scope or authority is disputed
last_updated: 2026-06-23
---

# Create Planner Pass Handoff

## Context

Use `relay-contracts/` contracts, schemas, policies, templates, and planner instructions as the authority. If a legacy instruction path is missing, record the drift and follow checked-out contract files.

## Steps

1. Identify the selected plan/pass and load only the required source artifacts.
2. State the pass goal, scope, non-goals, dependencies, file targets, validation contract, and completion/blocker conditions.
3. Remove implementation decisions where required behavior is already known.
4. Do not include future-pass implementation work unless the selected pass explicitly requires it.
5. Reference source-controlled contract paths instead of copying full contracts.

## Gotchas

- Chat memory is not a source of truth for handoff structure.
- A pass handoff should be implementation-ready but narrow; avoid architecture cleanup or adjacent features.
- Do not include secrets, tokens, private URLs, cookies, or unrelated personal data.

## Verify

- Check the handoff against the relevant relay-contracts planner handoff/pass plan contract.
- Confirm every target path is known or clearly marked proposed.
- Confirm non-goals exclude future passes.

## Update Scaffold

- [ ] Update `context/pipeline-contracts.md` only if contract path implications changed.
