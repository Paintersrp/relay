# Relay .mex Scaffold

`.mex` is committed repo-local implementation memory for Relay agents.

It helps agents navigate Relay source, follow repeatable implementation patterns, and detect stale repo-memory claims.

It does not override:

- checked-out source code
- `Paintersrp/relay-contracts`
- Planner handoffs
- canonical packets
- Relay DB state
- run artifacts
- audit evidence

Commit context, router, agent-anchor, and pattern files. Do not commit mex CLI source, caches, reports, logs, screenshots, package source, generated output, or generic tool templates.

Use `npx mex-agent check --json` to inspect scaffold drift when mex is available.
