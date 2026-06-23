---
name: decisions
description: Durable project decisions and stale-memory notes.
triggers:
  - decision
  - authority
  - scope
  - stale
  - why
edges:
  - target: context/pipeline-contracts.md
    condition: when a decision touches Planner or pipeline contracts
  - target: context/managed-plans.md
    condition: when a decision touches managed plans or run association
  - target: context/stack.md
    condition: when a decision touches current stack shape
last_updated: 2026-06-23
---

# Decisions

## Decision Log

### relay-contracts is authoritative for Planner contracts
| Field | Value |
|---|---|
| Date | 2026-06-23 |
| Status | Active |
| Decision | Planner handoffs, pass plans, canonical packets, policies, templates, schemas, and audit packet contracts are governed by `relay-contracts/`, not `.mex`. |
| Reasoning | `.mex` is compact implementation memory; duplicating contracts risks drift. |
| Alternatives considered | Copying full contracts into `.mex` was rejected because it would create a stale second source of truth. |
| Consequences | `.mex` points to contract paths and records implementation-facing implications only. |

### .mex is implementation memory only
| Field | Value |
|---|---|
| Date | 2026-06-23 |
| Status | Active |
| Decision | `.mex` guides future repo agents but is not pipeline law, DB state, run evidence, or audit evidence. |
| Reasoning | Local memory should help navigation without overriding checked-out source or contract repos. |
| Alternatives considered | Treating `.mex` as normative was rejected because it could conflict with source-controlled contracts and runtime artifacts. |
| Consequences | On conflict, use checked-out source and relay-contracts, then update `.mex` to remove drift. |

### Managed plans are optional
| Field | Value |
|---|---|
| Date | 2026-06-23 |
| Status | Active |
| Decision | Runs may be standalone or associated to a plan/pass through nullable DB fields. |
| Reasoning | Relay supports direct surgical handoff runs as well as managed multi-pass plan workflows. |
| Alternatives considered | Requiring every run to belong to a plan was rejected because existing intake/run flows must remain valid. |
| Consequences | API, store, and UI work must preserve omitted plan/pass behavior. |

### Plan/pass association is read/UI context, not mandatory lifecycle state
| Field | Value |
|---|---|
| Date | 2026-06-23 |
| Status | Active |
| Decision | Plan/pass/run association supports traceability and UI context; it does not force all lifecycle state into Alpine or the frontend. |
| Reasoning | The backend store remains authoritative for run lifecycle and audit evidence. |
| Alternatives considered | Client-only lifecycle state was rejected because it would make local UI state authoritative. |
| Consequences | Surface associated runs in read APIs while keeping run state transitions server-backed. |

### Preserve root templ/htmx stack until explicit decommission
| Field | Value |
|---|---|
| Date | 2026-06-23 |
| Status | Active |
| Decision | The root Go/templ/htmx/Tailwind assets remain part of the repo while `apps/web` is the primary React workbench. |
| Reasoning | Current files and scripts support both surfaces. |
| Alternatives considered | Removing the old surface during unrelated work was rejected as out of scope. |
| Consequences | Do not casually delete `web/`, templ views, root npm scripts, or generated templ output. |

## Stale / Conflicting Brief Notes

- The installer brief may reference paths or project structure that are stale for the current checkout. Use checked-out Relay source and the canonical relay-contracts source as authoritative.
- The broader project instruction says to use Go/templ/htmx, while current checked-out files also contain a primary `apps/web` React/TanStack Start workbench. For current UI work, use the checked-out `apps/web` structure.
