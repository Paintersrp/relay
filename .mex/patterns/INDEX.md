---
name: pattern-index
description: Lookup table for Relay implementation patterns.
triggers:
  - pattern index
  - task pattern
  - workflow
edges:
  - target: patterns/add-plan-api-read-field.md
    condition: when adding or changing managed plan API read fields
  - target: patterns/add-sqlc-query.md
    condition: when adding or changing SQL/sqlc behavior
  - target: patterns/update-plan-pass-ui.md
    condition: when changing managed plan UI surfaces
  - target: patterns/create-planner-pass-handoff.md
    condition: when producing a selected pass handoff
  - target: patterns/audit-managed-pass.md
    condition: when auditing completed managed pass work
last_updated: 2026-06-23
---

# Pattern Index

Lookup table for all pattern files in this directory. Check here before starting any task; if a pattern exists, follow it.

| Pattern | Use when |
|---------|----------|
| [add-plan-api-read-field.md](add-plan-api-read-field.md) | Adding or changing read-side managed plan/pass/run fields across API, store, and frontend. |
| [add-sqlc-query.md](add-sqlc-query.md) | Adding or changing SQLite/sqlc queries, migrations, or generated store methods. |
| [audit-managed-pass.md](audit-managed-pass.md) | Auditing a completed managed pass against its selected Planner handoff. |
| [create-planner-pass-handoff.md](create-planner-pass-handoff.md) | Creating a surgical implementation handoff for one selected Planner pass. |
| [update-plan-pass-ui.md](update-plan-pass-ui.md) | Updating plan registry, plan detail, pass timeline, or pass detail UI. |
