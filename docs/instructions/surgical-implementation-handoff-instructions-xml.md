# Surgical Implementation Handoff Instructions — Hybrid Protocol Edition

Use these instructions whenever I ask for implementation instructions, a surgical implementation handoff, repo-agent handoff, Cline/Codex/OpenCode/SWE prompt for code changes, or a Relay/local-agent execution handoff.

These instructions define two supported handoff modes:

1. Human repo-agent mode
   - Default mode.
   - Best for Cline, Codex, OpenCode, SWE agents, or direct paste into an LLM coding agent.
   - Uses a precise Markdown-style `.txt` handoff optimized for execution quality and readability.

2. Relay/parser mode
   - Use when I explicitly ask for a Relay handoff, local parser handoff, machine-readable handoff, schema-validated handoff, XML/JSON handoff, executable handoff payload, or anything intended for automated extraction.
   - Uses an XML-like envelope with a JSON payload.
   - The JSON payload is the only executable contract.
   - The notes block is for human review only.

Do not force Relay/parser mode for normal implementation prompts unless I ask for it or the request clearly targets an automated local pipeline.

---

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

The recommended model and suggested commit message belong in chat after the file link, not inside the `.txt` handoff, unless I explicitly ask otherwise.

---

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

---

## File naming

Use a descriptive, lowercase, hyphenated filename.

Use a timestamped filename when replacing a prior handoff, correcting a handoff, creating multiple handoffs in one conversation, or updating reusable instruction sets.

Examples:

```text
async-validation-progress-ui-surgical-implementation.txt
settings-toggle-blocked-state-fix-surgical-implementation.txt
status-workspace-density-affordances-surgical-implementation-20260614-0319.txt
surgical-handoff-instructions-hybrid-protocol-20260614.txt
````

Do not reuse an old filename for a corrected handoff.

---

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

A good handoff gives the repo agent pre-reasoned implementation instructions so the agent can focus on execution: editing named code, applying specified structure, preserving listed invariants, and running validation commands.

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

---

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

---

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

---

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

---

## Human repo-agent mode

Use this mode by default unless I explicitly ask for Relay/parser mode or a machine-readable handoff.

Human repo-agent mode produces a `.txt` handoff using the Markdown structure below.

### Human repo-agent file structure

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

---

## Relay/parser mode

Use this mode when I explicitly ask for any of the following:

- Relay handoff
- local parser handoff
- machine-readable handoff
- schema-validated handoff
- XML/JSON handoff
- executable handoff payload
- handoff payload for local execution
- parser-safe version
- structured handoff contract

Relay/parser mode is for a pipeline that extracts a JSON payload and validates it before handing it to a local execution agent.

### Relay/parser output shape

The `.txt` file must contain exactly one XML-like envelope with two top-level blocks:

```xml
<handoff_notes>
Human-review notes only.

Keep this concise. Include assumptions, repo-state facts, risks, and any code-shape guidance that is useful for a human reviewer.

This block is not executable. The local parser must not execute from this block.
</handoff_notes>

<handoff_payload format="json" schema="relay.surgical_handoff.v1">
{
  "schema_id": "relay.surgical_handoff.v1",
  "handoff_mode": "relay_parser",
  "project_name": "...",
  "context_name": "...",
  "goal": "...",
  "current_completed_state": "...",
  "why_this_pass_exists": "...",
  "scope": {
    "likely_changed_files": [],
    "generated_files_policy": "Generated files may change only through generation commands."
  },
  "do_not_change": [],
  "task_checklist": [],
  "direct_files_likely_changed": [],
  "direct_context_files": [],
  "current_implementation_facts_to_preserve": [],
  "behavior_changes": [],
  "surgical_implementation_details": [],
  "edge_cases_and_regressions_to_guard_against": [],
  "tests_to_add_or_update": [],
  "validation_commands": [],
  "agent_final_output_requirement": {
    "allowed_statuses": ["DONE", "BLOCKED"],
    "required_fields": ["build status", "test status"],
    "blocker_rule": "Include blocker/error only if BLOCKED."
  },
  "expected_result": "...",
  "final_self_check": []
}
</handoff_payload>
````

### Relay/parser strict rules

The `<handoff_payload>` content must be a single valid JSON object.

Do not wrap the JSON in Markdown fences.

Do not include JSON comments.

Do not include trailing commas.

Do not include extra prose inside `<handoff_payload>`.

Do not use `<thinking_trace>`.

Do not expose private chain-of-thought.

Use `<handoff_notes>` only for concise, reviewable rationale and assumptions.

Use descriptive XML-like tags only as section boundaries. Do not rely on a strict XML parser unless the payload has been XML-escaped. The intended extraction method is section-boundary extraction followed by JSON parsing and schema validation.

The local executor should:

1. Extract only the content inside `<handoff_payload>`.
2. Strip surrounding whitespace.
3. Parse as JSON.
4. Validate against the expected schema.
5. Reject or repair/retry on invalid JSON.
6. Execute only validated fields.
7. Ignore `<handoff_notes>` for execution.

### Relay/parser payload requirements

The JSON payload must carry the complete executable implementation contract. Do not put required execution details only in `<handoff_notes>`.

Every known concrete implementation decision must appear in the JSON payload, including:

- exact files
- exact functions/components/selectors/state fields
- exact behavior rules
- code-shape replacement guidance
- task order
- validation commands
- tests and assertion intent
- non-goals
- final output requirement

For long code-shape replacements, prefer structured objects instead of huge escaped prose strings:

```json
{
  "step_id": 3,
  "title": "Guard duplicate polling runs",
  "target_files": ["src/runtime/polling.ts"],
  "instructions": [
    "Add field `pollInFlight: boolean` to `RuntimeState`.",
    "Initialize it to `false` in `createRuntimeState(...)`.",
    "Set it to `true` before `readFeedSnapshot(...)`.",
    "Reset it in `finally`."
  ],
  "code_shape_guidance": [
    {
      "kind": "state_field",
      "current_shape": "RuntimeState currently owns timers.pollTimer, runtimeContext.nextCycleAt, and runtimeContext.activeIntervalMs.",
      "required_shape": "RuntimeState also owns pollInFlight: boolean."
    }
  ],
  "tests": [
    "Assert overlapping poll calls do not execute `readFeedSnapshot(...)` concurrently."
  ]
}
```

Use arrays of short strings when that keeps JSON valid and easier to repair.

### Relay/parser schema guidance

Use this schema shape unless I provide a stricter project-specific schema:

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "RelaySurgicalHandoffV1",
  "type": "object",
  "additionalProperties": false,
  "required": [
    "schema_id",
    "handoff_mode",
    "project_name",
    "context_name",
    "goal",
    "current_completed_state",
    "why_this_pass_exists",
    "scope",
    "do_not_change",
    "task_checklist",
    "direct_files_likely_changed",
    "current_implementation_facts_to_preserve",
    "behavior_changes",
    "surgical_implementation_details",
    "edge_cases_and_regressions_to_guard_against",
    "tests_to_add_or_update",
    "validation_commands",
    "agent_final_output_requirement",
    "expected_result",
    "final_self_check"
  ],
  "properties": {
    "schema_id": { "const": "relay.surgical_handoff.v1" },
    "handoff_mode": { "const": "relay_parser" },
    "project_name": { "type": "string" },
    "context_name": { "type": "string" },
    "goal": { "type": "string" },
    "current_completed_state": { "type": "string" },
    "why_this_pass_exists": { "type": "string" },
    "scope": {
      "type": "object",
      "additionalProperties": false,
      "required": ["likely_changed_files", "generated_files_policy"],
      "properties": {
        "likely_changed_files": {
          "type": "array",
          "items": { "type": "string" }
        },
        "generated_files_policy": { "type": "string" }
      }
    },
    "do_not_change": {
      "type": "array",
      "items": { "type": "string" }
    },
    "task_checklist": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "task"],
        "properties": {
          "id": { "type": "string" },
          "task": { "type": "string" }
        }
      }
    },
    "direct_files_likely_changed": {
      "type": "array",
      "items": { "type": "string" }
    },
    "direct_context_files": {
      "type": "array",
      "items": { "type": "string" }
    },
    "current_implementation_facts_to_preserve": {
      "type": "array",
      "items": { "type": "string" }
    },
    "behavior_changes": {
      "type": "array",
      "items": { "type": "string" }
    },
    "surgical_implementation_details": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "step_id",
          "title",
          "target_files",
          "instructions",
          "code_shape_guidance"
        ],
        "properties": {
          "step_id": { "type": "integer" },
          "title": { "type": "string" },
          "target_files": {
            "type": "array",
            "items": { "type": "string" }
          },
          "instructions": {
            "type": "array",
            "items": { "type": "string" }
          },
          "code_shape_guidance": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["kind", "current_shape", "required_shape"],
              "properties": {
                "kind": { "type": "string" },
                "current_shape": { "type": "string" },
                "required_shape": { "type": "string" }
              }
            }
          }
        }
      }
    },
    "edge_cases_and_regressions_to_guard_against": {
      "type": "array",
      "items": { "type": "string" }
    },
    "tests_to_add_or_update": {
      "type": "array",
      "items": { "type": "string" }
    },
    "validation_commands": {
      "type": "array",
      "items": { "type": "string" }
    },
    "agent_final_output_requirement": {
      "type": "object",
      "additionalProperties": false,
      "required": ["allowed_statuses", "required_fields", "blocker_rule"],
      "properties": {
        "allowed_statuses": {
          "type": "array",
          "items": { "type": "string" }
        },
        "required_fields": {
          "type": "array",
          "items": { "type": "string" }
        },
        "blocker_rule": { "type": "string" }
      }
    },
    "expected_result": { "type": "string" },
    "final_self_check": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}
```

---

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

---

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
```

Prefer DeepSeek V4 Flash for normal surgical handoffs unless the pass is broad, safety-sensitive, ambiguous, or already failed once.

Prefer DeepSeek V4 Pro when a bad partial implementation would create meaningful cleanup debt.

Prefer MiniMax M3 or Qwen3.7 Max only when the task needs recovery, broader autonomy, or a different failure profile.

For Relay/parser mode specifically:

```text
Mapped implementation with complete payload:
DeepSeek V4 Flash

Parser/schema-sensitive implementation or broad local pipeline changes:
DeepSeek V4 Pro

Handoff schema design, audit, or critique:
Kimi K2.6

Recovery from malformed payloads or failed architecture:
MiniMax M3 or Qwen3.7 Max
```

If no model list is available and model choice matters, ask for the available model list before creating the handoff.

---

## Correction behavior

If I challenge the quality, completeness, model choice, or reasoning of a handoff:

1. Do not re-link the same file.
2. Do not immediately generate a replacement artifact unless I ask.
3. Diagnose what failed: structure, depth, repo-state awareness, decision removal, code-replacement specificity, test specificity, stale assumptions, model choice, token bloat, artifact verification, or parser-safety.
4. Regenerate only after the disconnect is identified or I explicitly ask.

For parser/schema corrections, also check:

- Did `<handoff_payload>` contain only one valid JSON object?
- Did the JSON include required fields?
- Did required execution detail appear only in `<handoff_notes>` instead of the payload?
- Did the payload contain invalid comments, trailing commas, Markdown fences, or unescaped newlines?
- Did the handoff confuse human notes with executable contract?

---

## Standing style rules

- Keep handoffs direct, ordered, and implementation-focused.
- Use human repo-agent mode by default.
- Use Relay/parser mode only when requested or clearly required by the execution pipeline.
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
- Do not call human-review notes “thinking,” “chain of thought,” or “thinking trace.”
- Do not claim XML is safe by itself; parser safety comes from extracting the payload, parsing JSON, validating the schema, and rejecting invalid output.
- Do not rely on natural-language handoff text for local execution when a machine-readable payload was requested.
