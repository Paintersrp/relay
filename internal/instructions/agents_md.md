# AGENTS.md

## Repo workflow

This repo is worked through surgical implementation handoffs.

Follow the handoff exactly. Do not perform unrelated cleanup, broad refactors, or formatting churn unless the handoff explicitly asks for it.

## Source boundaries

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

## Validation responsibility

Relay may extract validation commands from the original handoff and run validation separately after implementation.

Do not run validation commands unless explicitly instructed by the handoff or user.

Do not paste full validation logs.

If you run checks yourself, summarize only:

- command run
- pass/fail
- blocker if failed

Relay owns the final validation result when validation is run by Relay.

## RTK / noisy shell output

This repo is commonly worked from Windows Git Bash / MINGW64.

Prefer RTK-wrapped commands for noisy inspection, search, diff, build, generation, migration, and test output when RTK is available.

Prefer `rtk.exe` over `rtk`.

Check RTK availability with:

```bash
rtk.exe --version
```

not:

```bash
rtk -v
```

Examples:

```bash
rtk.exe git status
rtk.exe git diff
rtk.exe grep "<pattern>" .
rtk.exe find "*.go" .
rtk.exe test "npm run build"
rtk.exe test "go test ./..."
```

Preserve full error detail when a build, typecheck, generation, migration, or test command fails.

If RTK output is too compact to diagnose a failure, inspect the relevant source file or rerun the narrow failing command without RTK only for that failing case.

## Change style

- Make focused, surgical changes.
- Preserve existing behavior unless the task explicitly asks to change it.
- Prefer simple, explicit control flow over clever abstractions.
- Prefer small local helpers over broad shared refactors unless refactoring is requested.
- Avoid import cycles.
- Avoid request-safety regressions when touching network behavior.
- Avoid local-preview/test isolation regressions when touching production endpoints.
- Avoid storage-shape changes unless explicitly requested.

## Completion response

When the task is complete, reply with:

```text
DONE or BLOCKED
Build status
Test status
Count of LOC changed
Blocker/error only if BLOCKED
```

Keep output minimal.
