# Planner Handoff: Fix Overflow Stale UI

## Artifact Metadata

```yaml
handoff_id: planner-handoff-2026-06-15-fix-overflow-stale-ui
schema_version: 1.0.0
created_at: 2026-06-15T09:00:00Z
planner_agent: Planner
intended_handoff_path: handoffs/planner/2026-06-15_fix-overflow-stale-ui.planner-handoff.md
target_packet_path: handoffs/packets/2026-06-15_fix-overflow-stale-ui.canonical-packet.json
canonical_packet_schema_path: handoffs/schema/canonical_packet.schema.json
target_executor: deepseek-v4-flash
repo_target: Paintersrp/auto-seso
branch_context: current working branch
task_slug: fix-overflow-stale-ui
content_profile: implementation_ready
```

<context_snapshot>
The user wants a surgical fix for an overflow page. This counts render path must update.
The implementation should not redesign the page.
</context_snapshot>

<decision_log>
- D1: Limit this pass to stale active/free count refresh behavior.
  - Rationale: The reported issue concerns visible counts not updating.
- D2: Reuse the existing refresh/render path.
  - Rationale: Adding another scheduler risks duplicate fetches.
</decision_log>

<constraints>
- C1: Use existing state and render helpers where available.
  - Applies to: packet_maker, renderer, executor, auditor
- C2: Do not change backend endpoint contracts.
  - Applies to: packet_maker, executor, auditor
</constraints>

<assumptions>
- AS1: The overflow page already has a current-status fetch path.
  - If false: block
- AS2: The project has an npm build command.
  - If false: continue_with_note
</assumptions>

<known_repo_facts>
- F1: The relevant UI behavior is likely under a source UI module.
  - Source: user_provided
- F2: The displayed counts should update.
  - Source: user_provided
</known_repo_facts>

<rejected_alternatives>
- RA1: Polling every 5 seconds.
  - Reason rejected: Too much traffic.
</rejected_alternatives>

<risk_register>
- R1: UI flickering during refresh.
  - Severity: low
  - Description: UI elements might flicker when counts refresh.
  - Mitigation: Cache count elements and perform DOM operations offscreen.
</risk_register>

<unresolved_questions>
- Q1: Whether we need backend logging.
  - Blocking: false
</unresolved_questions>

<pass_boundary>
```yaml
current_pass: 1
total_planned_passes: 1
this_pass_scope: Fix stale active/free count rendering after successful refresh.
out_of_scope_for_this_pass:
  - Redesigning the overflow page
depends_on_packet_id: ""
next_pass_hint: ""
```
</pass_boundary>

<packet_maker_brief>
Goal:
Fix stale active/free count rendering.

Scope:
Patch the existing refresh/render path.

Non-goals:
- Do not redesign the UI.

Likely file targets:
- `src/ui/overflowPage.ts`
  - role: primary
  - action: must_edit
  - reason: owns overflow page rendering

Required implementation steps:
1. Locate the existing current-status refresh success path.
   - action: modify
   - target_paths:
     - src/ui/overflowPage.ts
   - instructions: Add update state helper.
   - acceptance_criteria:
     - Active count updates.

Expected behavior:
- Active/free counts update after each successful refresh.

Completion requirements:
- DONE when the stale count display updates.
- BLOCKED when the refresh path cannot be located.
- Allowed discretion:
  - Tweak CSS styling.
- Forbidden discretion:
  - Redesigning page layout.
</packet_maker_brief>

<code_requirements>
- CR1: Preserve all existing code structure and comments.
  - Applies to:
    - src/ui/overflowPage.ts
</code_requirements>

<validation_expectations>
- V1:
  - command: `npm run build`
  - required: true
  - purpose: Verify the UI code still builds.
  - success signal: Command exits 0.
  - failure handling: attempt_fix_once_then_block
</validation_expectations>

<audit_priorities>
- A1: Confirm changed files are limited to the stale refresh path.
  - severity_if_failed: blocker
- A2: Confirm no new polling loop or scheduler is introduced.
  - severity_if_failed: error
</audit_priorities>
