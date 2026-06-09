# .clinerules

# RTK - Rust Token Killer

Use RTK to reduce noisy shell-command output.

## Required RTK behavior

Always prefer `rtk.exe` for shell commands unless the command is interactive or RTK output is too compact to diagnose a narrow failure.

This repo is commonly worked from Windows Git Bash / MINGW64.

Prefer:

```bash
rtk.exe --version
```

Do not use:

```bash
rtk -v
```

## RTK command examples

Use RTK-wrapped commands for noisy inspection, search, diff, build, generation, migration, and test output.

Examples:

```bash
rtk.exe git status
rtk.exe git diff
rtk.exe grep "<pattern>" .
rtk.exe find "*.go" .
rtk.exe test "npm run build"
rtk.exe test "go test ./..."
rtk.exe test "go vet ./..."
rtk.exe test "templ generate"
```

If `rtk.exe` is unavailable, try `rtk`.

If RTK is unavailable, fall back to the raw command.

Preserve full error detail when a build, typecheck, generation, migration, or test command fails.

If RTK output is too compact to diagnose a failure, inspect the relevant source file or rerun the specific failing command without RTK for the narrow failing case only.

# Repo hygiene rules

## Scope

Work from source files, not generated output.

Do not manually edit:

- `dist/`
- `coverage/`
- `node_modules/`
- bundled userscript output
- generated templ output
- generated sqlc output
- other generated artifacts

Generated files may only change by running the appropriate generator command.

## Change style

- Make focused, surgical changes.
- Preserve existing behavior unless the task explicitly asks to change it.
- Avoid unrelated formatting churn.
- Avoid broad refactors unless explicitly requested.
- Prefer simple, local changes.

# Validation behavior

Relay may run validation separately after implementation.

Do not run validation commands unless the handoff or user explicitly instructs you to run them.

Do not paste full validation logs.

If you run checks yourself, summarize only:

- command run
- pass/fail
- blocker if failed

# Completion response format

When the task is complete, reply with:

```text
DONE or BLOCKED
Build status
Test status
Count of LOC changed
Blocker/error only if BLOCKED
```

Keep output minimal.

Do not include changed-file lists, implementation summaries, validation logs, or explanations unless BLOCKED.
