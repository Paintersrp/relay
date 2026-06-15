# Planner Handoff: Fix Overflow Stale UI

## Artifact Metadata

```yaml
handoff_id: planner-handoff-2026-06-15-fix-overflow-stale-ui
schema_version: 1.0.0
created_at: 2026-06-15T09:00:00-04:00
planner_agent: Planner
intended_handoff_path: handoffs/planner/2026-06-15_fix-overflow-stale-ui.planner-handoff.md
target_packet_path: handoffs/packets/2026-06-15_fix-overflow-stale-ui.canonical-packet.json
canonical_packet_schema_path: handoffs/schema/canonical_packet.schema.json
target_executor: deepseek-v4-flash
repo_target: Paintersrp/auto-seso
branch_context: current working branch
```

<context_snapshot>
The user wants a surgical fix for an overflow page where active and free counts appear stale after the normal one-minute refresh. The current pass should address the stale count rendering path only. The implementation should not redesign the page, change endpoint contracts, introduce a second polling loop, or add unrelated display sections.
</context_snapshot>

<decision_log>

- D1: Limit this pass to stale active/free count refresh behavior.
  - Rationale: The reported issue concerns visible counts not updating after refresh, not overall overflow workflow design.
- D2: Reuse the existing refresh/render path.
  - Rationale: Adding another scheduler risks duplicate fetches and inconsistent UI state.
    </decision_log>

<constraints>
- C1: Use existing state and render helpers where available.
  - Applies to: packet_maker, renderer, executor, auditor
- C2: Do not change backend endpoint contracts.
  - Applies to: packet_maker, executor, auditor
- C3: Do not redesign the overflow UI in this pass.
  - Applies to: packet_maker, executor, auditor
</constraints>

<assumptions>
- AS1: The overflow page already has a current-status fetch path and visible count rendering path.
  - If false: block
- AS2: The project has an npm build command.
  - If false: continue_with_note
</assumptions>

<known_repo_facts>

- F1: The relevant UI behavior is likely under a source UI module rather than generated bundle output.
  - Source: user_provided
- F2: The displayed counts should update after each successful current-status refresh.
  - Source: user_provided
    </known_repo_facts>

<pass_boundary>

```yaml
current_pass: 1
total_planned_passes: 1
this_pass_scope: Fix stale active/free count rendering after successful refresh.
out_of_scope_for_this_pass:
  - Redesigning the overflow page
  - Adding new KPI sections
  - Changing endpoint contracts
  - Editing generated userscript bundle output
depends_on_packet_id:
next_pass_hint:
```

</pass_boundary>

<packet_maker_brief>
Goal:
Fix stale active/free count rendering after successful overflow current-status refresh.

Scope:
Patch the existing refresh/render path so visible active and free counts reflect the latest successful fetch.

Non-goals:

- Do not redesign the UI.
- Do not add new polling loops.
- Do not change endpoint contracts.
- Do not edit generated bundle output.

Likely file targets:

- `src/ui/overflowPage.ts`
  - expected action: must_edit
  - reason: likely owns overflow page rendering and refresh behavior

Required implementation steps:

1. Locate the existing current-status refresh success path.
2. Ensure the active/free count state is updated from the latest successful response.
3. Ensure the existing render/update helper for the count section runs after state update.
4. Preserve fetch failure behavior so prior visible data is not cleared.

Expected behavior:

- Active/free counts update after each successful refresh.
- No duplicate polling interval is introduced.
- Fetch failures preserve prior visible data and use existing error handling.

Completion requirements:

- DONE when the stale count display updates from latest successful refresh and validation passes or is reported according to the command failure policy.
- BLOCKED when the relevant refresh/render path cannot be located or required validation fails and cannot be fixed within scope.
  </packet_maker_brief>

<validation_expectations>

- V1:
  - command: `npm run build`
  - required: true
  - purpose: Verify the UI code still builds.
  - success signal: Command exits 0.
  - failure handling: attempt_fix_once_then_block
    </validation_expectations>

<audit_priorities>

- Confirm changed files are limited to the stale refresh path.
- Confirm no new polling loop or scheduler is introduced.
- Confirm generated bundle output was not edited.
- Confirm visible counts update after successful refresh.
  </audit_priorities>

<unresolved_questions>
None.
</unresolved_questions>

<packet_maker_directives>
The Packet Maker must:

- output a complete canonical packet JSON file
- save it to `handoffs/packets/2026-06-15_fix-overflow-stale-ui.canonical-packet.json`
- validate against `handoffs/schema/canonical_packet.schema.json`
- preserve the pass boundary
- not invent product behavior
- not add optional enhancements
- put executable requirements in `execution_payload`
- put audit checks in `audit_seed`
- preserve planning context in `planner_context`
  </packet_maker_directives>
