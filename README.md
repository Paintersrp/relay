# Relay

Local-first handoff orchestration web app.

Relay accepts surgical implementation handoffs, stores run metadata and artifacts, validates handoff structure, generates transformed Agent Prompts, and provides a run workbench for inspection.

## Intended Relay workflow

Relay's intended workflow is:

1. Parse the original handoff.
2. Build Intake Review.
3. Detect model, branch, repo, scoped files, validation commands, final output contract, and suggested commit.
4. Warn or block when the selected repo does not match the handoff scope.
5. Generate a transformed Agent Prompt for the running repo agent.
   - Preserves test implementation instructions.
   - Removes validation command execution material.
   - Tells the agent Relay will run validation separately.
6. Store original handoff and transformed Agent Prompt separately.
7. Store manual agent result intake.
8. Run validation commands locally after agent result.
9. Store validation stdout/stderr/json artifacts.
10. Inspect git diff for local changes.
11. Generate audit handoff for GPT review (includes validation evidence and git diff evidence).
12. Prepare git commit message suggestion based on handoff, audit, and diff evidence.
13. Review and manually run `git commit` in the repo (Relay does not commit on your behalf).

Key design points:

- Original handoff contains validation commands for Relay extraction.
- Agent Prompt preserves test implementation instructions in validation sections.
- Agent Prompt tells agent not to run validation by default.
- Test/validation section headings are preserved; only command fences and command lines are removed.
- Validation runner is local/user-triggered.
- `AGENTS.md` and `.clinerules` source templates live under `internal/instructions`.

## Not implemented yet

The current local-first flow does not yet implement:

- automatic repo-agent execution
- automatic branch/worktree creation
- automatic validation failure repair
- automatic diff-based audit / AI audit automation

## Stack

- Go `net/http` + `chi` router
- `templ` for server-rendered views
- SQLite via `database/sql`
- `sqlc` for typed queries
- `goose` for migrations
- `htmx` + Alpine for browser interactions
- Tailwind CSS v4
- TypeScript bundled with esbuild

## Prerequisites

- Go 1.25+
- Node.js 18+ with npm
- `sqlc` CLI
- `goose` CLI
- `templ` CLI
- `air` CLI for live-reload development

Install CLI tools:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/air-verse/air@latest
```

## Setup

```bash
# Install frontend dependencies
npm install

# Build frontend assets (CSS + JS)
npm run build

# Generate sqlc typed queries
sqlc generate

# Generate templ views
templ generate

# Run database migrations
goose -dir internal/db/migrations sqlite3 data/relay.sqlite up

# Build the server
go build -o bin/relay.exe ./cmd/relay

# Run the server (port 8080 by default)
go run ./cmd/relay
```

Or use the Makefile:

```bash
make install    # npm install
make assets     # build CSS + JS
make sqlc       # generate sqlc
make templ      # generate templ
make db-migrate # run migrations
make dev        # build assets + run server
make build      # full build
make test       # run tests
```

## React Workbench Frontend (apps/web)

`apps/web` is an additive TanStack Start React frontend for the Relay run workbench. The existing
Go backend (`cmd/relay`, port 8080) and server-rendered templ/htmx UI (`web/`) remain unchanged.

```bash
# Start the React workbench (separate terminal, port 3000):
cd apps/web
cp .env.example .env        # sets VITE_RELAY_API_BASE_URL=http://localhost:8080
npm install
npm run dev
# → http://localhost:3000
```

Pass 1 uses mock data only. Real API wiring begins in Pass 3.
See `docs/frontend-pivot.md` for the full additive pivot documentation.

## Database

Relay uses a local SQLite database at `data/relay.sqlite` by default.

Relay applies bundled SQLite migrations on startup automatically. The `goose` CLI
command remains useful for manual repair/debugging, but normal local startup does
not require running it separately.

If Relay reports a stale database schema error at startup, the embedded
auto-migration may have failed. Run:

```bash
goose -dir internal/db/migrations sqlite3 data/relay.sqlite up
```

## Intake Remediation Handoff

When Intake Review reports warnings or blockers, Step 1 can generate an
`intake_remediation_handoff` artifact. This artifact is a copyable repair prompt
for revising the original handoff and resolving intake issues.

To generate:

1. Complete Intake Review on Step 1.
2. Click **Generate Fix Handoff** when warnings or blockers are present.
3. View, download, or copy the generated remediation handoff.
4. Use the remediation handoff as a repair prompt for the original handoff.

Missing validation commands produce a remediation handoff that includes an actual
`## Relay validation commands` section with canonical command fences.

## Replace Original Handoff

If Intake Review finds issues, the original handoff can be replaced on the existing run from Step 1.
Replacing the handoff clears generated prompt/packet artifacts so they can be regenerated from the
corrected source.

To replace:

1. Open **Replace original handoff** in Step 1 Intake Review.
2. Paste the corrected handoff text into the textarea.
3. Click **Replace Handoff and Re-run Intake Review**.

The action writes the new text to the `original_handoff` artifact, clears stale downstream artifacts
(`agent_prompt`, `opencode_handoff_packet`, etc.) and stale checks, then immediately re-runs Intake
Review against the corrected handoff.

## Relay validation commands

Commands Relay should extract and run locally after agent result:

```bash
go fmt ./...
templ generate
npm run build
go test ./...
go vet ./...
```

If RTK is available, Relay or the user may prefer `rtk.exe` first, then `rtk`, then the raw command.

## Audit Handoff

When Relay Validation passes or after git diff evidence is collected, Step 7 provides actions to **Inspect Git Diff**, **Generate Audit Handoff**, and then proceed to **Step 8: Git Commit**.

**Git diff inspection** (`inspect-diff`) runs `git status --short`, `git diff --stat`, `git diff --numstat`, `git diff --name-status`, and `git diff --no-ext-diff --patch` in the selected repo path. All output is stored as run artifacts (`git_status_text`, `git_diff_stat`, `git_diff_numstat`, `git_diff_name_status`, `git_diff_patch`). The inspection does not modify the worktree.

**Audit handoff** generates a compact markdown artifact (`audit_handoff.md`) containing:

- Run metadata (ID, title, repo, branch, status)
- Original handoff preview (truncated if large)
- Agent result status, build/test results, LOC changed
- Validation results with per-command status, exit code, and duration
- **Git diff evidence** (status, diff stat, changed files, patch excerpt) when available
- Artifact manifest
- Review request for GPT

The audit handoff is intended to be copied into GPT for audit/review. Full AI audit/review is performed by pasting the audit handoff into GPT.

To generate:

1. Complete Step 6 Relay Validation successfully.
2. Optionally run **Inspect Git Diff** in Step 7 first for stronger evidence.
3. Click **Generate Audit Handoff** in Step 7 or from the Next Action card.
4. View, download, or copy the handoff from Step 7.
5. Paste the handoff into GPT for review.

After collecting git diff evidence, proceed to Step 8 to prepare a commit suggestion, then manually commit the implementation, re-inspect the diff if needed, and regenerate the audit handoff to include the latest evidence.

The audit handoff is always available for view/download after generation, even after page reload. Regenerating the audit handoff replaces the previous version so the latest handoff always reflects the most recent evidence.

## Git Diff Evidence

Step 7 can inspect the local git worktree at the selected repo path. When **Inspect Git Diff** is triggered:

- `git status --short` is saved as `git_status_text`
- `git diff --stat` is saved as `git_diff_stat`
- `git diff --numstat` is saved as `git_diff_numstat`
- `git diff --name-status` is saved as `git_diff_name_status`
- `git diff --no-ext-diff --patch` is saved as `git_diff_patch`

Diff evidence is displayed inline in Step 7 and included in the audit handoff when available. The patch is truncated in the handoff to avoid excessive size. Full patches remain accessible through artifact view/download links.

## Routes

| Method | Path                                   | Description                |
| ------ | -------------------------------------- | -------------------------- |
| GET    | `/`                                    | Dashboard with recent runs |
| GET    | `/handoffs/new`                        | New handoff form           |
| POST   | `/handoffs`                            | Create handoff run         |
| GET    | `/runs/{id}`                           | Run detail workbench       |
| POST   | `/runs/{id}/actions`                   | Execute run action         |
| GET    | `/runs/{id}/agent-run-monitor`         | Agent run monitor partial  |
| GET    | `/runs/{id}/artifacts/{kind}`          | View artifact              |
| GET    | `/runs/{id}/artifacts/{kind}/download` | Download artifact          |
| GET    | `/instructions`                        | Instruction assets list    |
| GET    | `/instructions/{kind}`                 | View instruction asset     |
| GET    | `/instructions/{kind}/download`        | Download instruction asset |
| GET    | `/settings/repos`                      | Repository settings        |
| POST   | `/settings/repos/roots`                | Add scan root              |
| POST   | `/settings/repos/scan`                 | Scan repos now             |

## Run Actions

| Action                                | Status                               |
| ------------------------------------- | ------------------------------------ |
| `validate-handoff`                    | Implemented                          |
| `prepare-prompt`                      | Implemented (generates Agent Prompt) |
| `mark-accepted`                       | Implemented                          |
| `mark-needs-cleanup`                  | Implemented                          |
| `generate-opencode-packet`            | Implemented                          |
| `start-opencode-go`                   | Implemented                          |
| `dry-run-opencode-go`                 | Implemented                          |
| `check-opencode-cli`                  | Implemented                          |
| `submit-agent-result`                 | Implemented                          |
| `generate-intake-remediation-handoff` | Implemented                          |
| `replace-original-handoff`            | Implemented                          |
| `run-agent`                           | Future                               |
| `run-validation`                      | Implemented                          |
| `inspect-diff`                        | Implemented                          |
| `generate-audit-handoff`              | Implemented                          |
| `prepare-git-commit`                  | Implemented                          |
| `generate-audit-packet`               | Future                               |

## Development live reload

Relay supports a local dev live-reload workflow for server-rendered development.

Install frontend dependencies and required CLIs, then run:

```bash
make dev
```

This runs Tailwind watch, esbuild watch, templ watch, and an Air-managed Go server with `RELAY_DEV_RELOAD=1`.

If running directly on Windows PowerShell:

```powershell
$env:RELAY_DEV_RELOAD="1"
npm run dev
```

The browser reloads when built frontend assets change or when the Go server restarts.

## Intake review

Relay's first useful step is the intake review.

The intake review parses the pasted handoff and shows:

- detected model
- selected model
- selected repo
- branch/worktree
- scoped files
- scoped file existence checks
- validation commands
- final output contract
- suggested commit message
- blockers and warnings

Relay should warn before validation when the selected repo does not appear to match the handoff scope.

## Model selection

When creating a handoff, Relay parses a recommended model from the pasted handoff text automatically. It supports:

- `## Execution model` / `Use:` section
- Labels like `Recommended Model:`, `Model:`, `Use model:`, `Suggested model:`

If no model is found and no override is selected, Relay defaults the selected model to DeepSeek V4 Flash.

Model selection is automatic by default. The model override control is optional and should only be used when intentionally overriding the handoff's execution model. The New Handoff form provides a collapsible model override with a dropdown and a Custom option for provider-specific model IDs.

## Agent Prompt

The Agent Prompt is a compact execution prompt for the running repo agent. Relay parses the original handoff, removes orchestration-only metadata (Execution model, RTK preference, Relay validation commands), strips validation command execution material (shell fences and command lines), and appends validation responsibility and final output contract sections.

Key behaviors:

- Relay stores the verbose original handoff as a source/orchestration artifact.
- The Agent Prompt (`agent_prompt`) is compact for repo-agent execution.
- Relay validation commands stay out of the Agent Prompt.
- Agent Prompt preserves test implementation instructions (prose, bullets, checklists) under `## Tests / validation`, `## Tests`, `## Validation`, and `## Tests to add or update` sections.
- Relay removes only command execution material (shell fenced blocks, bare command lines) from test/validation sections.
- Relay runs validation separately after agent result — the agent is told not to run validation commands by default.
- Validation commands remain in the original handoff for Relay extraction only.

The original handoff and compact Agent Prompt are stored separately.

Run detail shows an inline Original Handoff → Agent Prompt hunk diff after the Agent Prompt is generated, while keeping View/Download links for full artifacts.

## Run workbench workflow

After handoff creation, Relay automatically runs Intake Review and, when there are no blockers, prepares the Agent Prompt and Agent Packet. The run workbench is then used for review: Intake Review → Agent Prompt → Agent Packet → OpenCode Go Handoff → Agent Run → Relay Validation.

Run detail now surfaces a top-level Next Action card. The eight-step navigation remains available for review/debugging, but the primary workflow is guided by the recommended next action.

After each action completes, Relay redirects to the step where the next decision is most useful:

- **Intake Review** (ready) &rarr; Step 2 Agent Prompt
- **Intake Review** (blockers/warnings) &rarr; stays on Step 1
- **Agent Prompt** generation &rarr; stays on Step 2 for review
- **Agent Packet** generation &rarr; Step 4 OpenCode Handoff
- **OpenCode start** &rarr; Step 5 Agent Run Monitor
- **Agent result** &rarr; Step 6 Relay Validation
- **Validation run** &rarr; stays on Step 6
- **Validation pass + audit handoff** &rarr; Step 7 Diff/Audit
- **Commit suggestion prepared** &rarr; Step 8 Git Commit

The Next Action card updates naturally after each redirect because the server owns state.

Guided workbench smoke test: the next-action flow can launch, monitor, and validate a small OpenCode run.

The workbench is responsive for desktop and phone-sized devices. Long commands, JSON, and logs are contained inside scrollable panels so the page itself should not horizontally overflow.

Run detail defaults to Step 1 Intake Review. The user intentionally reviews each step and navigates forward using the step navigation links. Explicit `?step=` query parameter navigation is also supported.

Run detail is organized as a guided step-by-step workbench with a single active review step. Step navigation is server-rendered using query parameters (`?step=intake`, `?step=prompt`, `?step=handoff`, etc.). Each step panel includes a status chip, purpose description, and actionable controls based on current run state:

- **Step 1 — Intake Review**: Default active step. Shows validation checks, warnings, blockers, and the Intake Review panel. The "Run Intake Review" button changes to "Re-run Intake Review" after first run. Original handoff is available collapsed. After successful review with no blockers, redirects to Step 2.
- **Step 2 — Agent Prompt**: Shows the Original → Agent Prompt hunk diff inline with View/Download links. Buttons toggle between "Generate Agent Prompt" and "Regenerate Agent Prompt". Stays on this step after generation so the user can review.
- **Step 3 — Agent Packet**: Shows packet preview, View/Download links. Buttons toggle between "Generate Agent Packet" and "Regenerate Agent Packet". After generation, redirects to Step 4.
- **Step 4 — OpenCode Go Handoff**: Shows preflight readiness, OpenCode adapter configuration (binary, model, agent, working directory, command preview), and an explicit "Start OpenCode Go" button. Adapter readiness/blockers are visible at the top of the adapter section. If an execution exists, shows a notice linking to Step 5. Step 4 is handoff/preflight only; execution results appear in Step 5.
- **Step 5 — Agent Run Monitor**: Shows the running or completed OpenCode execution. Displays command context, terminal-style output transcript, artifact links (stdout, stderr, combined log), and parsed final result (DONE/BLOCKED). Auto-refreshes via HTMX polling while running. When DONE/BLOCKED is parsed, a validation CTA appears. Manual result intake fallback is available and shown more prominently when no result was auto-parsed.
- **Step 6 — Relay Validation**: Runs Relay-extracted validation commands locally after agent result. Validation starts asynchronously — clicking **Run Validation Commands** returns immediately and Step 6 auto-refreshes via HTMX polling every 2 seconds while validation runs. Shows real-time progress (current command, completed commands with inline results, elapsed time). Requires an agent result before the validation button is enabled. After completion, shows command-level results with status chips, exit codes, duration, and stdout/stderr indicators for each command. When validation passes, an audit handoff section appears. Stays on this step after run so the user can inspect pass/fail and output links.
- **Step 7 — Diff/Audit**: Inspect the local git diff/status from the selected repo path and generate the audit handoff. Step 7 provides an "Inspect Git Diff" action that collects git status, diff stat, and patch artifacts. Diff evidence is displayed inline preview and included in the audit handoff. The audit handoff is a compact markdown document intended to be copied into GPT for review. Full AI audit/review is performed by pasting the audit handoff into GPT. After audit, proceed to Step 8 for commit preparation.
- **Step 8 — Git Commit**: Prepare a suggested conventional commit message based on the handoff, audit handoff, and git diff evidence. Relay displays the suggested message and a copyable `git add -A && git commit -m "..."` command. Relay does not stage or commit on your behalf. The commit message is deterministic — no external API calls. Commit suggestion artifacts (`commit_message_text`, `commit_suggestion_json`) are created as run artifacts.

Clarifications:

- The top-level Next Action card is the primary guide. The stepper remains for manual review/debugging.
- After actions complete, Relay redirects to the step where the next decision is most useful.
- Relay does not execute OpenCode automatically. Execution only starts when the user explicitly clicks "Start OpenCode Go".
- Step 4 shows the OpenCode adapter configuration (binary, args, model, agent, working directory, command preview) in a details panel.
- If the adapter is blocked (e.g., missing model mapping), an error message is shown with the specific env var to set.
- If preflight checks are blocked, the Start button remains disabled.
- If an OpenCode execution exists, Step 4 shows a notice linking to Step 5 for monitoring or review.
- Starting OpenCode Go creates an execution record and returns immediately. The browser redirects to Step 5 Agent Run Monitor.
- Step 5 shows running status with auto-refresh via HTMX polling every 2 seconds while the execution is active.
- Relay captures stdout, stderr, and a combined log as run artifacts after execution.
- Step 5 displays a terminal-style output transcript parsed from OpenCode JSONL events.
- Relay extracts assistant text from JSONL stdout events and parses DONE/BLOCKED final output automatically.
- Relay does not persist UNKNOWN results automatically from JSON noise.
- When DONE/BLOCKED is parsed, Step 5 shows a prominent validation CTA.
- When no result is auto-parsed, Step 5 shows a prominent manual fallback.
- Relay Validation remains user-triggered after agent result. Step 5 does not auto-navigate to validation.
- Manual agent result intake remains available as a fallback in Step 5.
- Manual action buttons remain available as retry/regenerate controls for each step.
- Artifact previews (Original Handoff, Validation Report, Agent Prompt) are available in a collapsed `<details>` element at the bottom of the run detail page, not expanded by default.

### First-run checklist

1. Install OpenCode.
2. Connect OpenCode Go in the TUI:
   ```text
   opencode
   /connect
   /models
   ```
3. Confirm CLI models:
   ```bash
   opencode models
   ```
4. Fill `.env.local` (copy `.env.example` to `.env.local`).
5. Restart Relay.
6. Open Step 4 (OpenCode Go Handoff) for a run.
7. Click **Check OpenCode CLI** to verify binary and model availability.
8. Click **Dry Run / Preview Command** to confirm the full invocation.
9. Confirm preview includes `--model opencode-go/deepseek-v4-flash --thinking max`.
10. Click **Start OpenCode Go**.
11. Review Step 5 Agent Run Monitor for status, transcript, and parsed result.
12. When DONE/BLOCKED appears, proceed to Step 6 Relay Validation.

### Troubleshooting

- **Binary missing**: Set `RELAY_OPENCODE_BIN` or ensure `opencode` is on PATH. Run `opencode --version` to verify.
- **Auth missing/expired**: Run `opencode`, then `/connect`, then `opencode models` in the OpenCode TUI.
- **Model mapping missing**: If using a friendly model label, set `RELAY_OPENCODE_MODEL_<SLUG>` in `.env.local`. For DeepSeek V4 Flash: `RELAY_OPENCODE_MODEL_DEEPSEEK_V4_FLASH=opencode-go/deepseek-v4-flash`.
- **Model unavailable**: Run `opencode models` and confirm the resolved model ID appears in the list.
- **Windows Git Bash TUI issue**: PowerShell is safer for `opencode`. If using Git Bash, the TUI may not render correctly.
- **Shell/path/working-directory issue**: Check the "Resolved OpenCode command" panel in Step 4 for the exact working directory and binary.
- **`opencode run` returns non-zero**: Review the stderr and combined log artifacts linked after failure. The failure hint in the Step 4 UI provides actionable guidance.

## OpenCode adapter

Relay has a built-in OpenCode adapter that invokes `opencode run` with explicit arguments rather than relying on a generic shell command template.

Relay uses `opencode run` in non-interactive mode with `--format json`. The compact Agent Prompt is piped into stdin. The adapter parses JSONL text events from stdout to extract the final assistant text (DONE/BLOCKED).

Relay always invokes OpenCode with max thinking (`--thinking max`). This is intentional so Relay handoffs use OpenCode's highest reasoning setting for implementation work. Thinking level is not configurable in this release.

### Local setup with `.env.local`

Relay can load `.env` and `.env.local` from the working directory at startup.

Copy `.env.example` to `.env.local`:

```bash
cp .env.example .env.local
```

For Windows PowerShell:

```powershell
Copy-Item .env.example .env.local
```

OpenCode auth is owned by OpenCode, not Relay. Connect OpenCode Go once through the OpenCode TUI:

```text
opencode
/connect
/models
```

After auth, confirm the CLI model list:

```bash
opencode models
```

For DeepSeek V4 Flash on OpenCode Go, `.env.local` should include:

```env
RELAY_OPENCODE_MODEL_DEEPSEEK_V4_FLASH=opencode-go/deepseek-v4-flash
```

Restart Relay after editing `.env.local`.

### Configuration

| Env variable             | Default    | Description                               |
| ------------------------ | ---------- | ----------------------------------------- |
| `RELAY_OPENCODE_BIN`     | `opencode` | Path or name of the OpenCode binary       |
| `RELAY_OPENCODE_AGENT`   | `build`    | Agent to use (`build`, `architect`, etc.) |
| `RELAY_OPENCODE_VARIANT` | (none)     | Optional variant (e.g. `high`)            |

### Model resolution

Relay supports two model-selection paths:

1. **Direct OpenCode model ID** — if the selected model already contains `/`, Relay passes it through directly. Example:

   ```text
   opencode-go/deepseek-v4-flash
   ```

   No `RELAY_OPENCODE_MODEL_*` mapping is required.

2. **Friendly Relay model label** — if the selected model is a human label like `DeepSeek V4 Flash`, Relay converts it to a slug and looks for:

   ```text
   RELAY_OPENCODE_MODEL_DEEPSEEK_V4_FLASH
   ```

   This keeps Relay from guessing provider/model IDs incorrectly.

Do not invent exact provider/model IDs. Mappings must be configured explicitly.

Current tested OpenCode Go mapping:

```env
RELAY_OPENCODE_MODEL_DEEPSEEK_V4_FLASH=opencode-go/deepseek-v4-flash
```

You can confirm installed/available models with:

```bash
opencode models
```

### CLI Check

The **Check OpenCode CLI** action records an `opencode_cli_check_json` artifact and shows the latest result inline in Step 4, including binary, resolved model, model availability, and exit codes.

### Dry Run / Preview

Step 4 provides a **Dry Run / Preview Command** button that builds the same OpenCode invocation that Start will use, but does not execute it. The preview is saved as an `opencode_dry_run_json` artifact for review.

Dry Run never calls the command runner.

### Start behavior

- Execution is manual only. Relay never starts OpenCode automatically.
- Clicking Start OpenCode Go returns immediately (303 redirect to Step 5). The command runs in a background goroutine.
- Step 5 Agent Run Monitor shows running/completed status with auto-refresh via HTMX polling.
- Relay invokes `opencode run --format json --dir <repo> --agent <agent> --model <model> --thinking max` with the compact Agent Prompt piped into stdin.
- Relay captures stdout and stderr as separate artifacts and a combined log.
- Relay records execution status, exit code, start/end timestamps, and error messages in the `agent_executions` table.
- Relay builds a terminal-style output transcript from OpenCode JSONL stdout events (reasoning, tool_use, text, etc.).
- Relay extracts assistant text from JSONL stdout events and persists DONE/BLOCKED results through the agent result path.
- Relay does not persist UNKNOWN results automatically from JSON noise.
- Relay does not run validation commands automatically after OpenCode exits.
- Relay does not create branches.
- Manual result fallback remains available in Step 5.

This adapter path is intended to be verified with a tiny first-run handoff before larger implementation passes.

## OpenCode handoff packet

Relay can generate an `opencode_handoff_packet.json` artifact after an Agent Prompt exists.

The packet includes the run id, local repo path, branch/worktree metadata, selected model, recommended model, agent prompt artifact path, run artifact directory, an explicit artifact manifest listing required and optional artifacts, and an execution status of `configured`.

When generated, the OpenCode packet JSON is previewed inline in the run workbench and remains metadata-only. The packet does not include execution results.

## Handoff preflight

Step 4 (OpenCode Go Handoff) shows a preflight readiness checklist with checks for repo path, .git directory, branch/worktree, selected model, Agent Prompt artifact, Agent Packet artifact, and required artifact readability.

Each check shows pass/warn/block. The handoff readiness status chip reflects the overall result (ready, blocked, or warning). When blocked, a message advises to resolve blocked checks before handoff.

## Manual agent result intake

Relay can store the final output from an external repo agent after the user runs that agent outside Relay.

Expected final output shape:

```text
DONE or BLOCKED
Build status: ...
Test status: ...
Count of LOC changed: ...
Blocker/error only if BLOCKED: ...
```

Relay stores the raw pasted result, records parsed metadata as an agent result check, and updates the run status to `agent_done`, `agent_blocked`, or `agent_result_needs_review`.

Relay can execute OpenCode only through the explicit Step 4 OpenCode adapter. Relay does not execute arbitrary agents, does not auto-run OpenCode, and does not run validation automatically after OpenCode exits.

## Validation command runner

Relay can run validation commands for a run from the selected local repository path.

Commands are extracted from the original handoff's Tests / validation section. If no handoff commands are found, Relay falls back to the selected repo's default validation commands.

Validation command execution is user-triggered and runs asynchronously. Clicking **Run Validation Commands** starts validation in the background and immediately redirects to Step 6, which auto-refreshes via HTMX polling every 2 seconds while validation is running. Relay prevents duplicate validation starts with a DB-backed active execution lock, so rapid or simultaneous clicks cannot launch multiple workers for the same run.

Relay captures stdout, stderr, exit code, duration, and timeout state as run artifacts/checks. Progress is stored as a `validation_progress_json` artifact that updates after each command. Final validation artifacts remain:

- `validation_run_json` — aggregate command results
- `validation_stdout` — combined stdout output
- `validation_stderr` — combined stderr output

Relay does not run validation automatically after OpenCode exits.

## Local repository discovery

Relay discovers local Git repositories from configured scan roots.

The default scan root is:

```text
D:/Code
```

Open `/settings/repos` to:

- add additional roots
- enable or disable roots
- scan now
- review discovered repositories

The New Handoff page lets you select a discovered repo or use manual repo name/path entry. The New Handoff form discovers local Git branches for discovered repositories. Branch selection is populated from the selected local repo. Manual repo entry keeps a manual branch/worktree field because branches cannot be discovered until the repo path is submitted. Branch discovery has a 2-second timeout and fails gracefully if the repo is unavailable or slow.

## New handoff intake

A run can be created from the New Handoff source picker by uploading a `.txt` / `.md` handoff file or switching to Text input to paste the handoff. Upload wins if both upload and text are submitted.

Relay derives the run title from the handoff's first `#` heading. If no H1 heading exists, the run is named `Untitled handoff`.

## Project Structure

## Instruction Assets

Relay exposes canonical project instruction files at `/instructions`:

- **Surgical Chat Instructions** — Canonical handoff structure and rules (`surgical-chat-instructions.txt`)
- **AGENTS.md** — Canonical agent instructions (`AGENTS.md`)
- **.clinerules** — Canonical Cline rules (`.clinerules`)

Each asset provides a View link and a Download link with stable filenames. The root `AGENTS.md` and `.clinerules` files are synchronized with the canonical assets. A test verifies they stay in sync.

## Git Commit Step

Step 8: Git Commit is the final workflow step after validation, diff inspection, and audit handoff generation.

1. After validation passes (or validation failure is explicitly accepted), Relay recommends **Inspect Git Diff** in Step 7 if no diff evidence exists.
2. After diff evidence exists, Relay recommends **Generate Audit Handoff**.
3. After the audit handoff exists, Relay recommends **Prepare Git Commit**.
4. After the commit suggestion is prepared, Relay shows Step 8 with a suggested conventional commit message and a copyable command.

Relay does not stage files, does not run `git commit`, and does not execute any git mutating operations.

The commit message is generated deterministically from the original handoff, audit handoff, and git diff evidence. No external API calls are made. The commit suggestion is stored as two artifacts:
- `commit_message_text` → `commit-message.txt`
- `commit_suggestion_json` → `commit-suggestion.json`

## Project Structure

```
cmd/relay/          Entry point
internal/
  server/           HTTP server + chi routes
  handlers/         Request handlers
  store/            SQLite store + models
  pipeline/         Handoff validation + prompt prep
  artifacts/        Filesystem artifact storage
  instructions/     Source-of-truth instruction templates
  views/            templ templates
  db/
    migrations/     goose SQL migrations
    queries/        sqlc query definitions
web/
  src/              TypeScript + Tailwind sources
  static/           Built assets (gitignored)
data/
  relay.sqlite      SQLite database (gitignored)
  artifacts/        Run artifact files (gitignored)
```
## New TanStack Start frontend prototype

- `apps/web` contains the new Relay frontend prototype.
- Existing Go/templ/htmx UI remains during transition.
- Run the Go backend on port 8080.
- Run the new frontend from `apps/web`.
- Set `VITE_RELAY_API_BASE_URL=http://localhost:8080`.
