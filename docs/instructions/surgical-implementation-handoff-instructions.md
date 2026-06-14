# Surgical Implementation Handoff Instructions

Use these instructions whenever I ask for implementation instructions, a surgical implementation handoff, repo-agent handoff, or Cline/Codex/OpenCode/SWE prompt for code changes.

## Required behavior

When I ask for implementation instructions, do not only explain the plan in chat.

You must:

1. Create a descriptive, lowercase, hyphenated `.txt` file containing the full handoff.
2. Link the `.txt` file in the chat response.
3. Keep the chat response short.
4. Include the recommended execution model at the bottom of the chat response.
5. Include one suggested conventional commit message at the bottom of the chat response.
6. Do not put the full handoff only in chat unless I explicitly ask for inline-only output.

The handoff file must be written for a repo agent working directly in the target codebase.

## Chat output format

After creating the file, respond only with:

````text
Created the surgical implementation handoff:

[<filename>.txt](sandbox:/mnt/data/<filename>.txt)

Recommended model:

<model name>

Reason:

<brief reason>

Suggested commit message:

```text
<conventional commit message>
````

````

Do not summarize the handoff in chat unless I explicitly ask.

The recommended model and suggested commit message belong in chat after the file link, not inside the `.txt` handoff, unless I explicitly ask otherwise.

## File naming

Use a descriptive, lowercase, hyphenated filename.

Use a timestamped filename when replacing a prior handoff or creating multiple handoffs in one conversation.

Examples:

```text
async-validation-progress-ui-surgical-implementation.txt
settings-toggle-blocked-state-fix-surgical-implementation.txt
status-workspace-density-affordances-surgical-implementation-20260614-0319.txt
````

Do not reuse an old filename for a corrected handoff.

## Core handoff standard

The handoff must be comprehensive, surgical, and execution-focused.

The purpose is to remove implementation decisions before the repo agent starts coding.

The handoff is unacceptable if it is only:

- a summary
- a vague task description
- a formatted outline
- a high-level plan
- a list of desired outcomes without implementation shape
- a generic “inspect and update as needed” prompt

A good handoff should give the repo agent pre-reasoned implementation instructions so the agent can focus on execution: editing the named code, applying the specified structure, preserving the listed invariants, and running the validation commands.

## Code-replacement specificity requirement

When current codebase structure is known, include concrete code-shape guidance.

Prefer:

```text
Find this current shape:
...

Replace it with:
...
```

or:

```text
In `renderModalHeaderControls(...)`, change the Local Demo visibility condition from:
...

to:
...
```

or:

```text
Add field `pollInFlight: boolean` to `RuntimeState`.
Initialize it to `false` in `createRuntimeState(...)`.
Set it to `true` before `readFeedSnapshot(...)`.
Reset it in `finally`.
```

The handoff should specify exact items whenever known:

- files
- functions
- components
- imports
- state fields
- type names
- selectors
- IDs/classes
- storage keys
- route names
- command names
- event names
- status names
- call order
- test names
- assertion intent

Do not leave the agent to choose implementation strategy when the strategy can be specified.

Bad:

```text
Update the header controls so Local Demo appears correctly.
```

Good:

```text
In `src/ui/createApp.ts`, update `renderModalHeaderControls(...)` so Local Demo summary rendering is gated by local/demo snapshot state, not by `localActions.length`.

Render the Local Demo summary whenever local/demo snapshot state exists.

Render the Controls toggle only when the existing toggle callback exists.

Do not render a dead Controls button when no toggle callback exists.
```

Bad:

```text
Make tabs calmer.
```

Good:

```text
In `src/ui/styles/modal.ts`, update `.auto-seso-modal-tab` so inactive tabs use a transparent or near-transparent background and a quieter border.

Move active emphasis to `.auto-seso-modal-tab.is-active::after` as a bottom underline.

Keep `:focus-visible` behavior.
```

Bad:

```text
Update tests.
```

Good:

```text
Update `test/createApp.test.ts`:
- assert Local Demo summary renders when local/demo snapshot state exists
- assert the Controls toggle only renders when the toggle callback exists
- assert `aria-label="Close Auto-SESO"` still calls `closeModal`
```

## Token-control / anti-bloat requirement

The handoff should be precise, not padded.

Do not inflate the handoff with generic explanations, product background, motivational text, repeated warnings, or restated goals.

Avoid:

- repeated “why this matters” prose
- repeated non-goals in multiple sections
- generic repo-reading instructions
- assistant-facing reminders
- context-size guidance unless requested
- long narrative history when current state can be stated directly
- repeated acceptance criteria that duplicate implementation steps
- vague style adjectives without selectors or behavior
- redundant file lists repeated in multiple places

Prefer compact but complete instructions:

```text
Current problem:
Local Demo controls are missing in local/demo screenshots because header rendering is gated too narrowly.

Required change:
Render the Local Demo summary from local/demo snapshot state. Render the controls toggle only when the existing toggle callback exists.
```

Do not remove important implementation detail just to shorten the file. The goal is fewer wasted tokens, not less precision.

## Repo-state requirement

When current repo state, prior pass results, audit findings, file paths, functions, selectors, tests, or behavior contracts are known, use that information.

Do not replace known facts with generic boilerplate.

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

If repo facts are unknown, say so clearly. Do not invent exact paths, selectors, or line numbers. Use known directories or proposed paths only when clearly labeled.

## Handoff quality checklist

Before linking the file, verify that it includes, when known:

- Current repo/pass state.
- Why this pass exists.
- Concrete user-visible behavior change.
- Exact files likely affected.
- Exact functions, components, selectors, state fields, routes, storage keys, commands, and tests likely involved.
- Specific implementation steps in execution order.
- Code-shape replacement guidance where current code structure is known.
- Behavior boundaries and non-goals.
- Safety/correctness invariants.
- Edge cases and likely regressions.
- Tests to add or update with expected assertions.
- Validation commands.
- Agent final output requirement.
- Expected result.

If known details are missing, expand the file before linking it.

## Precision target

Target 5/5 precision.

5/5 means the repo agent should not need to decide:

- UX flow
- enablement/disablement rules
- state transitions
- persistence behavior
- routing/navigation behavior
- naming
- implementation approach
- validation behavior
- success/failure behavior
- what tests prove the pass worked

When multiple valid approaches exist, choose the preferred approach.

If a decision truly must be left to the implementer, label it:

```text
Implementation choice required:
...
```

Minimize these.

## Handoff file structure

Use this structure for implementation handoffs. For audit-only requests, use the user’s requested audit format instead of forcing this implementation structure.

````markdown
# <Context Name> Surgical Implementation

## Goal

Concrete final behavior.

## Current completed state

Current repo/pass state and behavior this pass starts from.

Keep this short and implementation-relevant.

## Why this pass exists

The problem this pass solves and why it is the next correct pass.

Keep this short.

## Scope

Files likely affected, if known.

Generated files may change only through generation commands.

## Do not change

Explicit non-goals and invariants.

Do not repeat the same constraint in later sections unless needed for local clarity.

## Task checklist

- [ ] Concrete implementation task
- [ ] Concrete implementation task
- [ ] Add/update tests
- [ ] Run validation

## Direct files likely changed

List expected edit targets, if known.

Do not list proposed new files as if they already exist.

## Direct context files

List supporting files to inspect, if useful and known.

Omit this section if not useful.

## Current implementation facts to preserve

Exact selectors, functions, state fields, stage names, routes, tests, guardrails, or behavior that must remain unchanged.

## Behavior changes

Exact expected behavior, including success paths, failure paths, edge cases, visibility rules, enablement rules, state transitions, persistence, routing, and responsive behavior when relevant.

## Surgical implementation details

Specific ordered implementation instructions.

This should be the highest-value section.

Use exact names when known.

Include code-shape replacement guidance when current code structure is known.

Prefer:

```text
Replace X with Y.
Call A before B.
Persist field Z.
Render section Q only when condition R is true.
Disable button S while condition T is true.
```
````

Avoid:

```text
Improve X.
Handle initialization.
Save progress.
Clean up styling.
```

For UI work, specify section hierarchy, button behavior, disabled states, helper text, visibility rules, clickable/passive affordances, hover/focus behavior, and responsive behavior when relevant.

For state work, specify field names, initial values, update triggers, reset conditions, persistence rules, and migration/backward compatibility when relevant.

For CSS work, specify selectors, scope boundaries, theme tokens/classes, breakpoints, and selectors that must not be reintroduced.

## Edge cases and regressions to guard against

List likely failure modes and stale behavior risks.

Keep this concise and specific.

## Tests to add/update

List exact tests and expected assertions when known.

Do not write only “update tests.”

## Validation commands

Raw commands only.

Use known project commands. Do not invent validation commands.

## Agent final output requirement

Return only:

- DONE or BLOCKED
- build status
- test status
- blocker/error only if BLOCKED

## Expected result

Final observable/testable outcome.

## Final self-check

Before linking the file, verify:

- It is not a thin summary.
- It reflects latest known repo/pass state.
- It removes known decisions instead of delegating them.
- It includes code-shape replacement guidance where current code structure is known.
- It avoids stale filenames, selectors, pass names, or assumptions.
- It avoids unnecessary explanation and repeated prose.
- Tests prove behavior, not just code existence.

````

## Model recommendation rule

Choose only from the available model list unless I explicitly provide a different model.

Available model lanes:

- DeepSeek V4 Flash
- DeepSeek V4 Pro
- MiniMax M3
- MiniMax M2.7
- Qwen3.6 Plus
- Qwen3.7 Plus
- Qwen3.7 Max
- Kimi K2.6
- Kimi K2.7 Code
- MiMo-V2.5
- MiMo-V2.5-Pro
- GLM 5
- GLM 5.1

General routing:

```text
Normal mapped surgical implementation:
DeepSeek V4 Flash

Safety-sensitive mapped implementation:
DeepSeek V4 Pro

Broad scaffold or multi-system implementation:
DeepSeek V4 Pro

Broad autonomous repair or second serious implementation attempt:
MiniMax M3

Escalation when DeepSeek Pro or MiniMax M3 misses architecture:
Qwen3.7 Max

Code-heavy alternate failure profile:
Kimi K2.7 Code

Planning, critique, audit, or handoff review:
Kimi K2.6

Experimental high reasoning/coding alternate:
GLM 5.1 or MiMo-V2.5-Pro

Medium fallback or polish:
Qwen3.6 Plus or Qwen3.7 Plus
````

Prefer DeepSeek V4 Flash for normal surgical handoffs unless the pass is broad, safety-sensitive, ambiguous, or already failed once.

Prefer DeepSeek V4 Pro when a bad partial implementation would create meaningful cleanup debt.

Prefer MiniMax M3 or Qwen3.7 Max only when the task needs recovery, broader autonomy, or a different failure profile.

If no model list is available and model choice matters, ask for the available model list before creating the handoff.

## Correction behavior

If I challenge the quality, completeness, model choice, or reasoning of a handoff:

1. Do not re-link the same file.
2. Do not immediately generate a replacement artifact unless I ask.
3. Diagnose what failed: structure, depth, repo-state awareness, decision removal, code-replacement specificity, test specificity, stale assumptions, model choice, token bloat, or artifact verification.
4. Regenerate only after the disconnect is identified or I explicitly ask.

## Standing style rules

- Keep handoffs direct, ordered, and implementation-focused.
- Do not include generic “inspect the repo first” boilerplate when known facts can be stated.
- Do not include context-size guidance unless requested.
- Do not include assistant-facing reminders.
- Do not over-explain product background.
- Do not repeat the same non-goal across multiple sections.
- Do not list generated files as manual edit targets.
- Do not list new files as if they already exist.
- Put the task checklist near the top.
- Prefer exact implementation instructions over descriptive outcomes.
- Prefer pre-reasoned code-shape replacements over abstract guidance.
- Prefer one cohesive pass over many tiny passes when the work is related.
- Do not pad the file with filler.
