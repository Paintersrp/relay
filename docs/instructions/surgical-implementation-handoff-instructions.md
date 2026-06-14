# Surgical Implementation Handoff Instructions (5/5 Precision Edition)

Use these instructions whenever I ask for implementation instructions, a surgical implementation handoff, a repo-agent handoff, or a Cline/Codex/OpenCode/SWE prompt for code changes.

## Required output behavior

When I ask for implementation instructions, do not only describe the idea in chat.

You must:

1. Create a context-named `.txt` file containing the full surgical implementation handoff.
2. Link the `.txt` file in the response.
3. Suggest one conventional commit message in a copyable code block.
4. Keep the chat response short.

The `.txt` file should be written for a repo agent working directly in the target codebase.

The handoff must be precise enough that the agent can execute without making product, UX, architecture, state-management, naming, or workflow decisions that could have been specified by the handoff author.

Do not put the full handoff only in the chat message unless I explicitly ask for inline-only output.

## Artifact acceptance gate

A handoff is not acceptable just because it has the correct headings.

A handoff is unacceptable if it is a thin summary, a vague task description, or a formatted outline that still leaves the agent to infer known implementation details.

Before linking any handoff file, verify that the file includes:

- Current repo/pass state.
- Why this pass exists now.
- The intended user-visible behavior change.
- Exact files likely affected, when known.
- Exact functions, components, selectors, state fields, routes, storage keys, commands, or tests likely involved, when known.
- Specific implementation steps in execution order.
- Behavior boundaries and non-goals.
- Safety/correctness invariants that must not regress.
- Existing guardrails that must remain passing.
- Edge cases and likely regressions.
- Tests to add or update.
- Validation commands.
- Agent final output requirement.
- Expected result.

If any of those are missing, do not link the artifact. Expand it first.

The handoff should be comprehensive enough for a weaker/flash model to complete without guessing.

## Decision removal requirement

The purpose of the handoff is to remove implementation decisions whenever the information is known.

When implementation details are known:

- Specify exact functions to modify.
- Specify exact state fields to add/change.
- Specify exact UI sections/components to create/remove.
- Specify exact control IDs/classes when relevant.
- Specify exact enable/disable conditions.
- Specify exact validation behavior.
- Specify exact success/failure behavior.
- Specify exact persistence behavior.
- Specify exact routing/navigation behavior.
- Specify exact command execution behavior.
- Specify exact dependency wiring.
- Specify exact test assertions when known.
- Specify exact selectors and responsive behavior for UI/CSS work.

Do not describe desired outcomes when implementation steps can be specified.

Bad:

```text
Improve the workflow.
```

Good:

```text
Disable `#tm-generate-report` while `loadedRows.length === 0`.
Enable after successful dwell load.
Show helper text "Load dwell data before generating a report."
```

Bad:

```text
Make validation asynchronous.
```

Good:

```text
Replace `runValidation()` with `startValidationWorker()`.
Persist `validation_progress_json`.
Redirect immediately after worker launch.
```

## Precision scoring target

Every handoff should target 5/5 precision.

5/5 means:

- An agent should not need to invent behavior.
- An agent should not need to decide UX flow.
- An agent should not need to decide enablement rules.
- An agent should not need to decide persistence behavior.
- An agent should not need to decide state transitions.
- An agent should not need to decide which implementation approach is preferred.
- An agent should not need to infer the current repo/pass state when that state is already known.
- An agent should not need to decide what tests would prove the pass worked when test intent can be specified.

If multiple valid implementation approaches exist, specify the preferred approach.

If a decision truly must be made by the implementer, explicitly label it:

```text
Implementation choice required:
...
```

and minimize the number of such decisions.

## Repo-state requirement

When current repo state, prior pass results, audit results, file paths, functions, selectors, tests, or behavior contracts are known, the handoff must use that information.

Do not give generic repo-inspection boilerplate in place of known facts.

Bad:

```text
Inspect the status tab and update it as needed.
```

Good:

```text
Update `src/ui/tabs/status/statusComponents.ts`.
In `renderSelectedStageWorkspace(...)`, keep the existing workspace header and `.tm-status-stage-workspace-grid`.
Only refine row rendering inside:
- `renderBreakdownRow(...)`
- `renderInsightCard(...)`
- `renderActionCard(...)`
```

If repo facts are unknown, say so clearly and use known directories or proposed paths rather than inventing exact paths.

## File naming

Use a descriptive, lowercase, hyphenated filename.

Use a unique timestamped filename when multiple handoffs are created in the same conversation or when replacing a prior handoff.

Examples:

- `async-validation-progress-ui-surgical-implementation.txt`
- `settings-toggle-blocked-state-fix-surgical-implementation.txt`
- `audit-diff-evidence-regeneration-surgical-implementation.txt`
- `opencode-run-monitor-stale-state-fix-surgical-implementation.txt`
- `status-workspace-density-affordances-comprehensive-surgical-implementation-20260614-0319.txt`

Do not use vague names.

Do not reuse an old filename for a corrected handoff.

## Chat response format

After creating the file, respond with only this structure:

````text
Created the surgical implementation handoff:

[<filename>.txt](sandbox:/mnt/data/<filename>.txt)

Suggested commit message:

```text
<conventional commit message>
````

````

Do not summarize the handoff in chat unless explicitly requested.

The suggested commit message belongs in the chat response after the file link, not inside the `.txt` handoff, unless I explicitly ask to include it in the handoff file.

## Handoff file structure

Use this structure for the `.txt` file.

```markdown
# <Context Name> Surgical Implementation

## Execution model

Use: <Model Name>

Reason: <One-sentence reason this model is appropriate for this task.>

## Goal

Concrete user-visible behavior change.

Describe the final behavior, not the intent.

## Current completed state

Summarize the current repo/pass state this implementation starts from.

Include relevant completed passes, audit outcomes, and current behavior that this pass must preserve.

## Why this pass exists

Explain the problem this pass solves and why it is the next correct pass.

Do not over-explain the product background. Focus on implementation relevance.

## Scope

Existing files likely affected:

- exact file paths, if known

New files may be added for helpers/tests if needed.

Generated files may change only through generation commands.

Do not invent exact file paths when they are not known. Use known directories or clearly mark the path as proposed.

## Do not change

Explicit constraints and non-goals.

Include behavior, architecture, safety gates, generated files, public APIs, runtime state, naming, or UI flows that must remain unchanged.

## Task checklist

Use checkbox items.

The checklist should map directly to implementation work.

Put this near the top so the agent can quickly build its task list.

## Direct files likely changed

List expected edit targets, if known.

Do not list new files as if they already exist.

## Direct context files

List supporting context files when useful and known.

Omit this section or write `None known` when there are no known context files.

## Current implementation facts to preserve

List behavior that must remain unchanged.

Use exact current selectors, functions, stage names, state fields, tests, or guardrails when known.

## Behavior changes

Describe exact expected behavior.

Include:

- enablement rules
- disablement rules
- state transitions
- validation rules
- success paths
- failure paths
- edge cases
- routing/navigation behavior
- responsive behavior when relevant

## Surgical implementation details

This is the most important section and should be the largest practical section.

Requirements:

- Use exact function names when known.
- Use exact component names when known.
- Use exact state names when known.
- Use exact artifact names when known.
- Use exact route names when known.
- Use exact selectors/IDs/classes when known.
- Use exact storage keys when known.
- Use exact test names or assertion patterns when known.

Whenever possible specify:

```text
Replace X with Y.
````

instead of:

```text
Improve X.
```

Whenever possible specify:

```text
Call A before B.
```

instead of:

```text
Handle initialization.
```

Whenever possible specify:

```text
Persist field Z.
```

instead of:

```text
Save progress.
```

### UI requirements

For UI work:

- Specify exact section hierarchy.
- Specify exact button behavior.
- Specify exact disabled states.
- Specify exact helper text.
- Specify exact visibility rules.
- Specify exact responsive behavior if relevant.
- Specify clickable versus passive affordances.
- Specify hover/focus behavior when relevant.

Avoid:

```text
Make the page cleaner.
```

Prefer:

```text
Replace the single-row command bar with:
- Sources card
- Data Load card
- Report card
- Filters card

Generate Report remains disabled until `loadedRows.length > 0`.
```

### State requirements

For state changes, specify:

- state field names
- initialization values
- update triggers
- reset conditions
- persistence rules
- migration/backward-compatibility behavior when relevant

### CSS requirements

For style work, specify:

- exact selectors
- exact scope boundaries
- exact theme tokens
- exact classes
- responsive breakpoints
- selectors that must not be reintroduced

Do not write:

```text
Improve styling.
```

Write:

```text
Scope all modal controls beneath `.tm-root`.
Remove unscoped `button`, `input`, and `table` selectors.
```

## Edge cases and regressions to guard against

List likely failure modes.

Include stale state, stale selectors, old labels, disabled interactions, responsive breakage, runtime behavior regressions, and test regressions when relevant.

## Tests to add/update

List exact tests.

Use test names whenever possible.

Specify expected assertions.

Do not write only:

```text
Update tests.
```

Write:

```text
Update `test/statusTab.test.ts`:
- assert `.tm-stage-action-card` is the only workspace row type with hover styling
- assert `.tm-stage-insight-card` has no hover selector
- assert disabled action cards do not set `data-target-tab`
```

## Validation commands

Raw commands only.

Use the project's known validation commands when available.

Do not invent validation commands.

## Agent final output requirement

Return only:

- DONE or BLOCKED
- build status
- test status
- count of LOC changed
- blocker/error only if BLOCKED

## Expected result

Summarize the final observable outcome.

The result should be testable and user-visible.

## Final self-check before handing off

Before considering this handoff complete, verify:

- The file is not a thin summary.
- The implementation details are specific enough for a weaker/flash model.
- The handoff reflects the latest known repo/pass state.
- The handoff does not rely on the agent guessing product, UX, architecture, state, naming, or workflow decisions.
- The handoff does not include stale pass names, stale filenames, stale selectors, or stale assumptions.
- The tests prove the intended behavior, not just that code exists.

```

## Correction behavior

If I challenge the quality, completeness, or reasoning of a generated handoff:

- Stop generating replacement artifacts immediately.
- Do not re-link the same file.
- Do not claim memory was updated unless it was actually updated in a user-visible memory system.
- Diagnose which instruction failed.
- Explain whether the issue was structure, depth, repo-state awareness, decision removal, test specificity, or artifact verification.
- Only regenerate the file after the disconnect is identified or after I explicitly ask for regeneration.

## Standing style rules

- Keep the handoff direct, ordered, and implementation-focused.
- Do not include generic "inspect the repo first" boilerplate.
- Do not include context-size guidance unless requested.
- Do not include assistant-facing reminders.
- Do not over-explain the product background.
- Do not list new files as if they already exist.
- Do not list generated files as manual edit targets.
- Put the task checklist near the top.
- Prefer one cohesive pass over many tiny passes when the work is related.
- Prefer exact implementation instructions over descriptive outcomes.
- Prefer explicit decision removal over open-ended agent discretion.
- Do not pad the file with filler just to make it longer.

## Commit message guidance

Suggest one conventional commit message after the file link.

Examples:

- `feat: add validation progress UI`
- `fix: prevent stale running state`
- `refactor: split action runner lifecycle`
- `test: cover async worker finalization`
- `docs: update validation workflow`
```
