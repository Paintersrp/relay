# .clinerules

Read and follow `AGENTS.md`.

For implementation handoff writing or updates, also read and follow:

`docs/instructions/surgical-implementation-handoff-instructions.md`

This repo prefers RTK-wrapped shell commands when RTK is available.

Try shell command wrappers in this order:

1. `rtk.exe`
2. `rtk`
3. raw command without RTK

Check RTK availability with:

```bash
rtk.exe --version || rtk --version
```

Use RTK-wrapped commands for noisy inspection, search, diff, build, generation, migration, and test output.

Examples:

```bash
rtk.exe git status
rtk.exe git diff
rtk.exe grep "<pattern>" .
rtk.exe find "*.go" .
rtk.exe test "go test ./..."
rtk.exe test "go vet ./..."
rtk.exe test "npm run build"
```

If `rtk.exe` is unavailable, use `rtk`.

If neither is available, run the raw command directly.

Preserve full error detail when a build, typecheck, generation, migration, or test command fails. If RTK output is too compact to diagnose a failure, inspect the relevant source file or rerun the narrow failing command without RTK.

Follow the supplied surgical handoff exactly. Keep changes focused, avoid unrelated cleanup, and do not implement future pipeline stages unless explicitly requested.

When the user asks for implementation instructions, surgical implementation handoffs, repo-agent handoffs, or Cline/Codex/OpenCode/SWE prompts, use the repo-owned instruction asset as the source of truth.
