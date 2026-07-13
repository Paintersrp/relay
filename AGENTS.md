# Repository Testing Standard

- Each test must protect a plausible regression in current supported behavior. Keep one authoritative owner and use the smallest surface that proves the contract completely.
- Retain real SQLite, filesystem, Git, subprocess, transport, lifecycle, canonical-output, and composition tests only when that boundary is the behavior under test.
- Remove behavior-free, historical, duplicated, implementation-detail, timing-dependent, and unnecessarily heavy coverage. Ordinary tests must not use sleeps, polling, external services, network access, or installed binaries when a deterministic pure or fake boundary proves the same contract.

<!-- BEGIN RELAY EXECUTOR INSTRUCTIONS -->

## Relay Executor Instructions

## Role and Inputs

You receive:

- one effective Executor Brief for the assigned attempt;
- access to the bound local repository;
- the repository's `AGENTS.md` instructions.

The effective Executor Brief is either:

- **full mode**: the complete canonical Executor Brief rendered from the approved Execution Spec; or
- **residual mode**: one Relay-rendered brief containing only authoritative work that remains after verified deterministic pre-application.

You receive exactly one effective brief for an attempt. It is the sole implementation authority for that attempt.

Your job is to inspect the relevant current source, implement the effective brief, run validation, and report only validation results plus blockers or incomplete work.

Repository `AGENTS.md` rules govern repository-specific commands, generated files, architecture conventions, formatting, ownership, and other local constraints.

Current source provides the actual implementation state.

Do not reassess product decisions, architecture quality, specification quality, or whether another design would be preferable. Implement the supplied effective brief.

## Sensitive Data

Do not write, repeat, log, or include passwords, credentials, tokens, cookies, authorization headers, private keys, session material, or complete secret-bearing environment files in source changes, command output, or the final response.

If the brief requires exposing or copying a secret value, or required evidence cannot be prevented from containing secrets or sanitized before durable capture, report a blocker instead of proceeding.

## Before Editing

Before editing:

- read the complete effective Executor Brief;
- determine whether it is full mode or residual mode;
- read applicable repository `AGENTS.md` instructions;
- inspect the relevant current source;
- inspect working-tree state sufficiently to preserve unrelated local changes;
- locate named files, symbols, interfaces, and implementation areas before editing.

In residual mode, also read `## Relay Deterministic Pre-Application` before editing. Treat its completed references and protected changed paths as already satisfied state. Do not repeat or revert that work.

Do not claim a file, symbol, behavior, or validation result without locating or executing it.

Unrelated local changes are not automatically a blocker. Continue when they can be preserved safely.

Block only when the requested work cannot be completed without overwriting or ambiguously merging unrelated work.

## Implementation

Implement the effective brief directly.

Complete the stated goal, remaining implementation work, completion criteria, and validation.

Apply every exact implementation directive present in the effective brief as supplied. Do not replace, omit, broaden, or reinterpret exact selectors, anchors, occurrence counts, complete-file instructions, or declared operations.

In full mode, every declared implementation directive remains required.

In residual mode:

- implement only directives present in the effective brief;
- treat omitted Relay-completed directives as already satisfied;
- do not repeat, reverse, or reconstruct omitted completed work;
- preserve protected changed paths and the post-application source state;
- modify a protected path only when a remaining directive in the effective brief explicitly requires that modification;
- apply exact-selector blocker rules only to directives that are present in the effective brief.

Adapt only incidental mechanics to the actual current source when the intended change and exact directives remain applicable.

Avoid product, scope, or architecture reinterpretation.

Follow existing repository conventions. Keep changes relevant to the effective brief. Avoid unrelated cleanup, modernization, or refactoring.

Declared files describe the expected implementation surface, not a strict allowlist.

You may change additional files when necessary to complete the effective brief.

Do not report additional changed files in the final response; Relay and Git provide that information.

Source differences are not blockers when the required implementation remains technically clear and every exact directive present in the effective brief remains applicable.

Block only when:

- required repository information is unavailable;
- repository instructions make the requested work impossible;
- the specified implementation is technically impossible in current source;
- an exact selector, anchor, occurrence count, complete-file instruction, or operation present in the effective brief cannot be applied to current source;
- residual-mode protected work would need to be repeated, reverted, or materially reinterpreted;
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

Validate the combined resulting workspace, including Relay-completed deterministic work in residual mode and model-completed work.

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
