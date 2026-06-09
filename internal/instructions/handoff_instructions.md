# Relay Implementation Handoff Instructions

## Purpose

This document defines the structure and rules for surgical implementation handoffs in Relay.

Handoffs are the orchestration/source artifact. Relay parses them into structured intake data, extracts validation commands, and generates a transformed Agent Prompt for the running repo agent.

## Required handoff .txt structure

Every Relay handoff must follow this structure in a context-named `.txt` file:

```text
# <Title>

## Execution model

Use: <Model Name>
Reason: <Brief justification for model choice>

## Goal

<One-paragraph description of what this handoff should accomplish>

## Scope

Exact areas affected:

- <file path or subdirectory>
- ...

## Do not change

- <list of things that must not be modified>
- ...

## Task checklist

- [ ] <actionable item>
- [ ] ...

## Direct files likely changed

- <file paths>

## Direct context files

- <file paths that provide context but should not be edited>

## Current implementation facts to preserve

- <existing behavior that must be preserved>
- ...

## Tests / validation

<Validation commands that Relay will extract and run. Use canonical raw commands.>

```bash
go fmt ./...
templ generate
npm run build
go test ./...
go vet ./...
```

RTK preference:

```text
If RTK is available in the environment, Relay or the user may prefer rtk.exe first, then rtk, then the raw command.
```

Do not list RTK-wrapped commands as separate validation commands.

## Agent final output requirement

Return only:

- DONE or BLOCKED
- build status
- test status
- count of LOC changed
- blocker/error only if BLOCKED
```

## Surgical implementation details

<Detailed implementation instructions, code snippets, and architectural guidance>
```

## Suggested commit message

After the `.txt` file, provide a suggested conventional commit message:

```text
Suggested commit message: type(scope): brief description
```

## Relay Agent Prompt transformation rules

Relay generates a transformed Agent Prompt from the original handoff:

1. Preserve implementation instructions (`## Goal`, `## Scope`, `## Do not change`, `## Task checklist`, `## Direct files likely changed`, `## Direct context files`, `## Current implementation facts to preserve`, `## Surgical implementation details`)
2. Remove or rewrite `## Execution model` section
3. Remove or rewrite `## Tests / validation` section — validation commands are part of the original handoff for Relay extraction only
4. Remove original `## Agent final output requirement` and append a clean final output contract
5. Add `## Validation responsibility` section telling the agent Relay will run validation
6. Add `## Relay validation plan` with extracted commands (not for agent execution)

## Validation commands are part of the original handoff

Validation commands remain in the original handoff so Relay can extract and run them locally after agent result. The running agent is told not to run validation commands by default.

Generated Agent Prompts tell the running agent not to run validation by default and not to paste validation logs.

Validation commands should be canonical raw commands (e.g., `go test ./...`, not `rtk.exe go test ./...`).

## RTK preference

Relay recommends RTK for noisy shell commands when available. Prefer `rtk.exe` first, then `rtk`, then the raw command.

RTK preference is described separately in `AGENTS.md` and `.clinerules` templates. Do not list RTK-wrapped variants as additional validation commands.

## Agent final output requirement

When the task is complete, the running agent must reply with only:

```text
DONE or BLOCKED
Build status
Test status
Count of LOC changed
Blocker/error only if BLOCKED
```

Keep output minimal. Do not include changed-file lists, implementation summaries, validation logs, or explanations unless BLOCKED.
