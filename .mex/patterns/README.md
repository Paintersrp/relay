---
name: patterns-readme
description: How Relay-specific implementation patterns are used and maintained.
triggers:
  - pattern
  - scaffold growth
  - mex
edges:
  - target: patterns/INDEX.md
    condition: when looking for a matching task pattern
  - target: context/conventions.md
    condition: when deciding whether a convention or pattern should hold guidance
last_updated: 2026-06-23
---

# Patterns

Pattern files are task-specific implementation memory for Relay. They should describe repeatable repo workflows, cross-layer gotchas, and verification steps that future agents are likely to need.

## Use

1. Check `INDEX.md` before starting implementation work.
2. Load the matching pattern and every context file named in its edges.
3. Follow the pattern unless current source or `relay-contracts/` proves it stale.
4. If a pattern is stale, update the pattern after completing the task.

## Add or Update

Create or update a pattern when a task has a repeatable Relay-specific workflow, a non-obvious integration boundary, or a verification sequence that prevents likely regressions.

Keep patterns concise. Prefer links to context files and source-controlled contracts over copying large source or contract text.
