# Generated Agent References

This directory contains project-level generated agent references for the relay repository.

## Model

- **JSON files are the source of truth.** Markdown files are generated companions rendered from the same in-memory document model.
- Generated references use repo-relative paths only (no absolute paths, no `..` traversal).
- Generated references must not include wall-clock timestamps.

## Commands

```bash
# Regenerate foundation/index JSON and Markdown
make agentrefs-generate

# Check that committed outputs are up to date
make agentrefs-check
```

Or use npm wrappers:

```bash
npm run agentrefs:generate
npm run agentrefs:check
```

## Fact Labels

| Label | Meaning |
| --- | --- |
| `proven` | Verified by source evidence or test output |
| `derived` | Inferred from source structure or convention |
| `convention` | Established by project convention, not source-enforced |
| `unresolved` | Question open; no conclusion yet |
| `conflict` | Competing signals or contradictory evidence |

## Scope

PASS-001 creates only foundation/index output for this directory. Later selected passes add domain-specific generated references (backend packages, MCP actions, HTTP routes, storage, frontend contracts, etc.).

## Authority

These generated project references are not task/run-specific context packets or Planner handoffs. They supplement — but do not override — checked-out source code, selected Planner handoffs or canonical packets, Relay DB state, run artifacts, audit evidence, and canonical `Paintersrp/relay-contracts` files.
