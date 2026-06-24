# Relay Refactor Backlog (v1)

This document describes the Relay refactor backlog workflow as shipped in v1. It
is an operator-facing concept and workflow reference. The authoritative behavioral
contract lives in `Paintersrp/relay-contracts` at
`contracts/refactor_backlog_contract.md`; this guide describes how the
already-implemented Relay surfaces (backend service, MCP local-operator tools,
and the React workbench) expose that contract.

The refactor backlog lets an operator capture refactor ideas, develop them into
pass-ready work, and feed them into the normal managed plan/pass/run/audit
lifecycle. It adds no autonomous behavior: every transition that creates or
advances real work is an explicit, human-confirmed action.

---

## Scope and Safety Boundaries

The refactor backlog in v1 is deliberately narrow:

- **No sidecars.** There is no sidecar refactor execution path. A refactor is
  either captured as backlog state or scheduled as a normal managed pass. There
  is no hidden, parallel, or out-of-band refactor runner.
- **No autonomous repository analysis.** Relay does not scan, crawl, or analyze a
  repository on its own to invent refactor work. Discovery tasks are human-authored
  analysis prompts; candidates are human-authored pass-ready proposals.
- **No automatic plan submission.** Generating a refactor-only plan produces a
  reviewable artifact only. It never calls `submit_planner_pass_plan`.
- **No automatic run creation.** Promotion and generation never call
  `create_run_from_planner_handoff` or otherwise create or dispatch a run.
- **No hidden execution, no git mutation, no shell, no model calls.** The backlog
  surfaces share the same safety boundaries as the rest of Relay.
- **No expanded source access or MCP mutation scope.** The MCP refactor tools are
  project-scoped, schema-strict, and confirmation-gated.

---

## Discovery Tasks vs. Refactor Candidates

The backlog has two distinct record types. Keeping them distinct is important:
a **discovery task** is a question to investigate; a **refactor candidate** is an
answer that is ready to become a pass.

### Discovery task

A discovery task is a project-scoped analysis prompt — a reminder or assignment to
investigate a potential refactor. It captures intent and scope, not an executable
plan. It has:

- a `title` and an `analysis_prompt`,
- a structured `target_scope` (`{kind, values}` where `kind` is one of
  `repository`, `subsystem`, `directory`, `file_set`, `plan`, `pass`),
- optional `priority`, `tags`, and `metadata`.

Discovery tasks never create candidates, plans, passes, runs, or audits on their
own. Investigating a discovery task may lead a human to author one or more
candidates that reference it, but that link is purely informational.

### Refactor candidate

A refactor candidate is a **pass-ready** proposal: enough detail to become a
managed `pass_type: "refactor"` pass. A candidate is only accepted as `ready` when
it satisfies the pass-ready requirements below.

---

## Pass-Ready Candidate Requirements

A candidate must provide all of the following before it is persisted as `ready`
(missing or blank fields are rejected with structured validation issues):

- `title`
- `problem_summary`
- `desired_behavior`
- `rationale`
- `proposed_pass_name`
- `proposed_pass_goal`
- `proposed_pass_scope` (at least one non-empty entry)
- `non_goals` (at least one non-empty entry)
- `target_files` (at least one non-empty entry)
- `validation_commands` (at least one non-empty entry)
- `audit_focus` (at least one non-empty entry)
- `risk_level` — one of `low`, `medium`, `high`

Additional rules:

- `current_behavior`, `constraints`, `dependency_notes`, and `metadata` are
  optional.
- Candidate dependencies and source discovery-task references must resolve within
  the **same project**; cross-project references and self-references are rejected.
- Obvious secret-like values (private keys, bearer tokens, common key prefixes)
  are rejected before persistence.

---

## Candidate Lifecycle and Status

| Status | Meaning | Allowed actions | Next states |
|---|---|---|---|
| `ready` | Pass-ready, unscheduled | edit, defer, reject, supersede, suggest placement, promote, include in a generated refactor-only plan | `scheduled`, `deferred`, `rejected`, `superseded` |
| `scheduled` | Slotted into a managed pass | none from the backlog; completion is audit-derived from the scheduled pass | `scheduled_revision_required`, `completed`, `completed_with_warnings`, `deferred` |
| `scheduled_revision_required` | Scheduled pass needs repair | repair/follow-up through the normal managed pass/run lifecycle | `scheduled`, `completed`, `completed_with_warnings` |
| `completed` | Scheduled refactor pass audit accepted | none (terminal) | terminal |
| `completed_with_warnings` | Scheduled refactor pass audit accepted with warnings | none (terminal) | terminal |
| `deferred` | Human-deferred or pass skipped | edit | `ready`-style recovery only where the service allows it |
| `rejected` | Human-rejected | none (terminal) | terminal |
| `superseded` | Replaced by another candidate | none (terminal) | terminal |

Discovery tasks have their own smaller lifecycle: `open` (editable; may be
completed, closed, or superseded), `completed` (may be superseded), and the
terminal `closed`/`superseded` states.

---

## Promotion Into an Existing Managed Plan

Promotion slots one `ready` candidate into an existing, project-owned, **active**
managed plan as a normal pass.

- The new pass is a normal managed pass with `pass_type: "refactor"` and
  `refactor_candidate` metadata (`source: refactor_backlog_candidate`,
  `scheduling_mode: existing_plan_bonus_pass`). It is sequenced like any other
  pass; refactor metadata does not change normal pass ordering.
- Placement is either explicit (`after_pass_id`) or a deterministic advisory
  suggestion (`exact_file_overlap` > `same_directory` > `same_subsystem` >
  appended-with-no-suggestion).
- On success the candidate moves to `scheduled` and an active schedule reference
  is created that links the candidate to the plan/pass. No `run_id` is set, and no
  run is created.
- Promotion is blocked for non-`ready` candidates, candidates that already have an
  active schedule reference, unsatisfied dependencies, plans that are not active or
  belong to another project, and candidates with no concrete repo-relative file
  targets to seed pass context.

Promotion never submits the plan, creates a run, or dispatches an executor.

---

## Generated Refactor-Only Plan Workflow

Generation takes one or more `ready` candidates and produces a reviewable
refactor-only Plan of Passes artifact set.

- The result includes a generated plan ID, the selected candidate IDs, a JSON
  artifact path, a Markdown artifact path, any warnings, and
  `submission_policy: review_required_no_auto_submit`.
- **Generated refactor-only plans are reviewable artifacts only.** Generation does
  not submit the plan, create candidate schedule references, change candidate
  status (selected candidates remain `ready`), or create any run.
- To act on a generated plan, a human must separately review it and then submit it
  through the normal, user-confirmed `submit_planner_pass_plan` action.
  `create_run_from_planner_handoff` remains a separate, user-confirmed action.
- Through MCP, generation returns artifact metadata only — never the full raw plan
  JSON or Markdown body.

---

## Scheduled Refactor Passes and Audit-Derived Completion

A scheduled refactor candidate rides the normal managed pass/run/audit lifecycle.
There is no separate refactor audit.

- A scheduled refactor pass is a normal managed pass with `pass_type: "refactor"`
  and `refactor_candidate` metadata. It is selected, executed, and audited exactly
  like any other managed pass.
- Candidate completion is **derived from the scheduled managed pass audit
  outcome**, applied at the audit/lifecycle boundary:

  | Scheduled pass audit decision | Candidate result |
  |---|---|
  | `accepted` | `completed` |
  | `accepted_with_warnings` | `completed_with_warnings` |
  | `revision_required` | `scheduled_revision_required` (same pass/run stays selected) |
  | `skipped` | `deferred` |
  | `blocked` / `manual_review_required` / `rejected` | no candidate change; an explicit human decision is required |

- `revision_required` blocks advancement: the orchestrator keeps the same pass and
  run selected for repair or follow-up and does not advance to a later pass.
- A **stale, missing, mismatched, or malformed schedule reference fails closed**:
  it blocks promotion and the audit-to-candidate mapping rather than silently
  completing, rescheduling, or rejecting a candidate. Neither the managed pass
  status nor the candidate status is partially mutated on such a failure.
- Terminal candidates are never downgraded, and re-applying the same accepted
  outcome is idempotent (no duplicate status events).

---

## MCP Safety Boundaries and Profiles

The refactor backlog is exposed through MCP only under the local-operator profile.

- The refactor backlog tools register **only** when
  `RELAY_MCP_PROFILE=local-operator`. Under `RELAY_MCP_PROFILE=restricted` they are
  hidden, and calling them returns an unknown-tool error.
- Every tool is project-scoped and requires `project_id`. Schemas are strict
  (`additionalProperties: false`); callers must not infer `project_id` from repo,
  branch, chat, or working directory.
- Retrieval tools are bounded and require no confirmation. Backlog mutation tools
  require `confirmed_user_intent: true`. `promote_refactor_candidate_to_plan`
  requires the exact confirmation string `promote_refactor_candidate_to_plan`, and
  `generate_refactor_only_plan` requires the exact confirmation string
  `generate_reviewable_refactor_only_plan`.
- The MCP layer is a thin wrapper over the `internal/refactors` service. It adds no
  shell execution, no arbitrary filesystem access, no git mutation, no model calls,
  no automatic plan submission, and no run creation.

See [`docs/mcp.md`](mcp.md) for the full MCP tool inventory and
[`docs/operator-guide.md`](operator-guide.md) for the operator workflow and the
manual QA checklist.

---

## Release Validation

The refactor backlog hardening is covered by the deterministic, local-only release
smoke suite. Run it through the root npm script:

```bash
npm run release:smoke
```

This wraps `scripts/release-smoke.sh`, which runs the Go test suite (including
`internal/refactors`, `internal/plans`, and `internal/mcp`), the local script
tests, the `apps/web` typecheck/test/build, the root `smoke` suite, and
`make validate`. It performs no network calls, git mutation, executor dispatch, or
automatic submission.
