# Relay Frontend Pivot

- Existing Go backend remains the orchestration daemon.
- Existing templ/htmx UI remains during transition.
- `apps/web` is the new TanStack Start frontend prototype.
- The frontend is manually scaffolded because external scaffold commands were blocked by network/npm 403.
- shadcn-style UI components are used selectively.
- New UI model:
  Step 1 — Intake / Configure
  Step 2 — Compile / Render
  Step 3 — Execute
  Step 4 — Audit / Close
- This pass uses mock data only.
- Future passes will add frontend API contract, Go JSON endpoints, real intake, compile/render wiring, executor wiring, audit/closeout, and MCP handback.
