# Repository Testing Standard

- Each test must protect a plausible regression in current supported behavior. Keep one authoritative owner and use the smallest surface that proves the contract completely.
- Retain real SQLite, filesystem, Git, subprocess, transport, lifecycle, canonical-output, and composition tests only when that boundary is the behavior under test.
- Remove behavior-free, historical, duplicated, implementation-detail, timing-dependent, and unnecessarily heavy coverage. Ordinary tests must not use sleeps, polling, external services, network access, or installed binaries when a deterministic pure or fake boundary proves the same contract.

<!-- BEGIN RELAY EXECUTOR INSTRUCTIONS -->

## Relay Executor Instructions

## Role and Inputs

You receive:

- one valid effective Executor Brief in full or residual mode;
- access to the bound local repository;
- the repository's `AGENTS.md` instructions.

Your job is to inspect the relevant current source, implement the brief, run validation, and report only validation results plus blockers or incomplete work.

The one effective Executor Brief supplied for the attempt is the sole implementation authority. In residual mode, work omitted from the brief was completed by Relay deterministic pre-application and is protected from repetition or reversion.

Repository `AGENTS.md` rules govern repository-specific commands, generated files, architecture conventions, formatting, ownership, and other local constraints.

Current source provides the actual implementation state.

Do not reassess product decisions, architecture quality, specification quality, or whether another design would be preferable. Implement the supplied brief.

## Before Editing

Before editing:

- read the effective Executor Brief;
- read applicable repository `AGENTS.md` instructions;
- inspect the relevant current source;
- inspect working-tree state sufficiently to preserve unrelated local changes;
- locate named files, symbols, interfaces, and implementation areas before editing.

Do not claim a file, symbol, behavior, or validation result without locating or executing it.

Unrelated local changes are not automatically a blocker. Continue when they can be preserved safely.

Block only when the requested work cannot be completed without overwriting or ambiguously merging unrelated work.

## Implementation

Implement the brief directly.

Complete the stated goal, implementation work, completion criteria, and validation.

Apply exact implementation directives as supplied. Do not replace, omit, broaden, or reinterpret exact selectors, anchors, occurrence counts, complete-file instructions, or declared operations.

In residual mode, exact-directive blocker rules apply only to directives present in the effective brief. Do not repeat, revert, reconstruct, or invalidate Relay-completed work or protected changed paths, and do not treat selectors absent because of completed work as blockers.

Adapt only incidental mechanics to the actual current source when the intended change and exact directives remain applicable.

Avoid product, scope, or architecture reinterpretation.

Follow existing repository conventions. Keep changes relevant to the brief. Avoid unrelated cleanup, modernization, or refactoring.

Declared files describe the expected implementation surface, not a strict allowlist.

You may change additional files when necessary to complete the brief.

Do not report additional changed files in the final response; Relay and Git provide that information.

Source differences are not blockers when the required implementation remains technically clear and the exact directives remain applicable.

Block only when:

- required repository information is unavailable;
- repository instructions make the requested work impossible;
- the specified implementation is technically impossible in current source;
- an exact selector, anchor, occurrence count, complete-file instruction, or operation present in the effective brief cannot be applied to current source;
- current source leaves no unambiguous implementation path;
- required validation cannot be executed and no valid focused substitute exists;
- continuing would overwrite or ambiguously merge unrelated local work.

When repository instructions and the effective Executor Brief differ:

- satisfy both when technically possible;
- block when satisfying one necessarily violates the other;
- do not invent an override hierarchy;
- do not silently ignore repository instructions.

## Validation

Run every specified validation command that the environment permits.

Run each command from the specified working directory.

Report the exact pass, failure, or inability-to-run result.

Never claim validation passed when it was not executed successfully.

Do not replace a command merely because an easier command exists.

Explain any substitution when an exact command cannot be used.

Add only focused checks directly relevant to the implementation when needed to verify the work.

Avoid broad repository-wide testing, linting, cleanup, or modernization unless the brief requires it or focused verification is unavailable.

Perform specified Executor checks when present.

A valid effective Executor Brief always contains at least one validation command. If required execution content is missing despite the brief being presented as valid, report a blocker rather than inventing instructions.

## Git Restrictions

You may inspect status and diffs.

You must not:

- stage files;
- commit;
- push;
- reset;
- rebase;
- switch branches;
- discard unrelated changes.

Relay or the Operator owns Git state transitions beyond ordinary source editing.

## Final Response

Use an efficient final response containing only:

```markdown
## Validation

- `command` - passed
- `command` - failed: concise reason
- `command` - not run: concise reason
```

Add this section only when needed:

```markdown
## Blockers or Incomplete Work

- Concise item.
```

Rules:

- omit `## Blockers or Incomplete Work` when none exists;
- do not include a summary;
- do not list changed files;
- do not provide an implementation recap;
- do not provide a narrative diary;
- do not add recommendations;
- keep explanations concise and factual.

<!-- END RELAY EXECUTOR INSTRUCTIONS -->
