# Project-Scoped Orchestrator Workflow

This document is the operator and auditor guide for the chat-mediated,
project-scoped orchestrator workflow. It describes how the loop runs end to end,
which human gates are mandatory, what blocks advancement, and how to verify the
behavior manually and with existing validation commands.

> [!IMPORTANT]
> **Human gates are always required.** Relay owns project/plan/pass/run state and
> surfaces retrieval-only work packets. It does **not** generate Planner handoffs,
> does **not** generate audit judgments, and does **not** apply audit decisions on
> its own. A human must review and approve at every gate described below.

## Purpose

Relay is the durable system of record for `project -> plan -> pass -> run` state.
Chat agents (Planner and Auditor) read retrieval-only work packets and produce
reviewed artifacts (a Planner handoff or an audit decision) that a human approves
before Relay records the next state transition. This separation keeps all
judgment with humans and chat agents while Relay enforces sequencing and
fail-closed advancement.

## Roles

- **Operator / User** — Opens projects/plans in the Relay web UI, clicks
  Continue Plan and Audit Ready, reviews generated artifacts, and approves each
  human gate.
- **Planner chat agent** — Retrieves `get_next_pass_work`, drafts a Planner
  handoff for the selected pass, and presents it in chat for review. It does not
  submit runs autonomously.
- **Relay** — Persists state, selects the next eligible pass/audit work,
  associates runs created from reviewed handoffs, synchronizes pass status from
  run status deterministically, and applies the human-approved audit decision.
- **Executor** — Runs the pass-associated run through prepare/execute/validate
  under existing run-stage gates. Out of scope for this workflow except as a
  consumer of approved run submissions.
- **Auditor chat agent** — Retrieves `get_next_audit_work`, reviews the audit
  evidence, and proposes a decision for human approval. It does not finalize
  decisions autonomously.

## Required Human Gates

1. **Planner handoff approval** — A reviewed Planner handoff must be approved
   before `create_run_from_planner_handoff(plan_id, pass_id, ...)` creates a run.
2. **Packet / brief / executor approval** — Each configured run-stage gate
   (intake, packet/brief, executor) requires explicit human approval.
3. **Audit decision approval** — An audit decision must be reviewed and approved
   in the run audit workbench before a pass can complete, require revision, or be
   blocked.
4. **Manual Continue Plan** — After a gate resolves, the operator must click
   Continue Plan again. Relay never auto-advances to the next pass.

## Happy Path

1. Open the project.
2. Open the active plan.
3. Click **Continue Plan**.
4. Use the selected pass work packet to request a Planner handoff in chat. The
   `get_next_pass_work` response includes metadata-only `required_context_bundle`,
   `handoff_work`, and `handoff_authoring_packet`.
5. Optionally call `prepare_handoff_context` with `project_id`, `plan_id`, and
   `pass_id` to prepare metadata-only source and context evidence readiness before
   authoring. The response includes `source_snapshot_id`, `context_packet_id`,
   `repo_heads`, `freshness_report`, `required_coverage`, `optional_coverage`,
   `required_context_bundle`, typed `blockers`, `recommended_next_action`, and
   `lower_level_recovery_actions`.
6. Author the reviewed Planner handoff from prepared context.
7. Call `validate_planner_handoff_for_compile` for deterministic compile-aware
   preflight without creating a run. Blocking preflight failures return the shared
   blocker envelope with bounded `metadata.preflight` details.
8. Perform human review and explicit confirmation of the validated handoff.
9. Submit the reviewed handoff with
   `create_run_from_planner_handoff_file(planner_handoff_file, expected_sha256, ...)`
   when a reviewed file exists, preserving exact file-byte provenance. Inline
   submission via `create_run_from_planner_handoff` remains the fallback for
   chat-only drafts.
10. Progress the run through the configured gates (intake -> prepare -> execute ->
    validate).
11. During or after execution, use `get_run_artifact` for bounded inspection of
    registered run artifacts (validation reports, compiler outputs, executor
    diagnostics) through safe view modes (`metadata_only`, `bounded_excerpt`,
    `summary`, `errors`). Artifact readback is bounded, redacted, and path-safe;
    it never provides generic file access or unbounded content dumps.
12. When the run is audit-ready, click **Audit Ready** or call
    `get_next_audit_work`.
13. The Auditor reviews the evidence and proposes a decision.
14. The user submits/approves the decision from the run audit workbench.
15. **Continue Plan** returns the next pass only after the prior pass is
    `completed` or `skipped`.

### Recovery Path

When `prepare_handoff_context` returns blocked or warns of optional-context limitations:

1. Use `resolve_project_repository` to verify registered repository identities
   and accepted aliases. Unknown or ambiguous cases return structured blockers with
   safe evidence and next actions.
2. Use `create_source_snapshot` to acquire a fresh bounded source snapshot.
   Assert the structured `freshness_report` for freshness, `reusable_for_handoff`,
   and bounded repository identity. Stale, dirty, or drifted snapshots return typed
   source blockers with recovery guidance.
3. Use `create_context_packet` or `get_context_packet` to establish or retrieve
   required context packet evidence.
4. Retry `prepare_handoff_context` with the corrected source and context evidence.

No recovery step bypasses human review, explicit confirmation, or the
preflight/exact-file-submission gates. All responses remain bounded and
path-safe.

## Blocking Behavior

The following states stop advancement. Continue Plan and Audit Ready surface the
blocker `code` and `message` rather than selecting a later pass:

- `dependencies_incomplete` — a declared dependency pass is not `completed`/`skipped`.
- `active_run_exists` — the candidate pass has a non-terminal associated run.
- `prior_pass_awaits_audit` — an earlier pass is `audit_ready` and must be audited first.
- `audit_evidence_missing` — the audit-ready run lacks the required `audit_packet`
  and `audit_evidence_manifest_json` evidence.
- `audit_already_finalized` — the run/pass audit has already been decided.
- `revision_required_same_pass` — the pass needs repair/follow-up on the same
  pass/run; advancement is blocked and no later pass is selected.
- `blocked` — a blocked pass prevents continuation (surfaced as a fail-closed
  blocker; not treated as dependency-satisfied).
- `unsafe_request` — `project_id`/`plan_id` are missing or not safe identifiers.

Only `completed` and `skipped` are dependency-satisfying terminal pass states.
`revision_required` and `blocked` never advance the plan automatically.

## Manual UI Checklists

These checks are concrete enough for an operator or auditor to run against a
local Relay web UI. Each lists the route, the action, and the expected visible
result. They verify visible behavior, not just type compilation.

### `/projects`

- **Action:** Open the projects list.
- **Expected:** Projects render with their identifiers. Selecting a project
  navigates to `/projects/{projectId}`.

### `/projects/{projectId}`

- **Action:** Open a project detail.
- **Expected:** The project's plans are listed and link to their plan detail
  routes. The project identifier matches the one used by work-packet retrieval.

### `/plans`

- **Action:** Open the plans list.
- **Expected:** Plans render with `plan_id` and status. Active, project-scoped
  plans are the ones eligible for Continue Plan.

### `/plans/{planId}` (Plan Detail — Project Workflow panel)

- **Project scope strip:** Shows Project (linking to `/projects/{projectId}`),
  Plan, Status, and Passes count.
- **Continue Plan visibility:** The Continue Plan button is enabled only when the
  plan has project scope (`projectId` present) and status `active`. With no
  project scope, a "Project unavailable" notice appears and both buttons are
  disabled.
- **Continue Plan — blocked:** When blocked, a "Continue Plan Blocked" card
  displays the blocker `code` (monospace) and `message`, plus a "Recoverable"
  hint when applicable. No pass is selected.
- **Continue Plan — success:** The "Next Pass Work" card displays the selected
  pass (Pass ID, Sequence, Name, Status, Goal), a Context Summary with a
  Context Ready badge and any source snapshot / context packet IDs, handoff
  readiness criteria, a suggested run submission JSON block (with a Copy JSON
  button), a "Copy Planner handoff prompt" button, associated runs when present,
  and dependency status badges (Satisfied / Not Satisfied) for each dependency.
- **Audit Ready — blocked:** When blocked, an "Audit Ready Blocked" card displays
  the blocker `code` and `message`.
- **Audit Ready — success:** The "Next Audit Work" card displays the selected
  pass, the selected run (Run ID linking to the audit workbench), allowed
  decisions as badges, artifact reference sections (executor results, validation
  reports, audit packets, diff evidence), and an "Open run audit workbench" link.
  The panel explicitly states that decisions are applied from the run audit
  workbench, not from this project workflow panel.

### `/plans/{planId}/passes/{passId}`

- **Action:** Open a pass detail.
- **Expected:** The pass status reflects the deterministic synchronization from
  its associated run (for example `run_created` -> `in_progress` -> `audit_ready`).
  `revision_required` and `blocked` are shown as non-advancing states.

### `/runs/{runId}/audit`

- **Action:** Open the run audit workbench from the Audit Ready card link.
- **Expected:** The audit evidence is reviewable and the human-gated decision
  controls (approve / request revision) are present here. This is the only place
  an audit decision is applied; the project workflow panel never finalizes a
  decision.

## MCP / CLI Validation

Run these existing commands to validate the workflow surfaces. No new commands
are introduced by this pass.

```bash
make mcp-test                          # MCP package tests (profile gating, schemas, responses)
make mcp-smoke                         # builds the MCP binary and runs the deterministic streamlined workflow smoke harness
npm run smoke                          # integrated Go/MCP/server/local-scripts/web smoke command
npm run release:smoke                  # canonical local release gate including MCP smoke and all retained checks
make validate                          # repository validation report (go fmt/test, web typecheck/build)
```

Backend and route coverage can be run directly:

```bash
go test ./internal/plans/...           # orchestrator service/lifecycle end-to-end tests
go test ./internal/api/...             # next-pass / next-audit route tests
go test ./internal/mcp/...             # MCP orchestrator work-tool tests
npm --prefix apps/web run typecheck    # web type correctness
npm --prefix apps/web test             # web Vitest suite
```

> [!NOTE]
> UI verification for Continue Plan and Audit Ready relies on the documented
> manual checks above rather than browser end-to-end automation, because the web
> package uses Vitest without a browser E2E framework installed.

## Safety Boundaries

The work-packet retrieval tools (`get_next_pass_work`, `get_next_audit_work`) are
strictly retrieval-only. They do **not**:

- create runs,
- submit plans,
- generate Planner handoffs,
- generate audit judgments,
- apply audit decisions,
- dispatch executors,
- run shell commands,
- mutate git, or
- expose arbitrary filesystem access.

They return bounded work-packet JSON (`ok: true` with a selected pass/run, or
`ok: false` with structured `blockers`). No tool output includes secrets, tokens,
auth headers, private keys, or signed URLs.
