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
10. Later: inspect git diff.
11. Later: generate audit packet for GPT review.

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
- automatic commits
- automatic validation failure repair
- git diff inspection

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

## Routes

| Method | Path                                   | Description                |
| ------ | -------------------------------------- | -------------------------- |
| GET    | `/`                                    | Dashboard with recent runs |
| GET    | `/handoffs/new`                        | New handoff form           |
| POST   | `/handoffs`                            | Create handoff run         |
| GET    | `/runs/{id}`                           | Run detail workbench       |
| POST   | `/runs/{id}/actions`                   | Execute run action         |
| GET    | `/runs/{id}/artifacts/{kind}`          | View artifact              |
| GET    | `/runs/{id}/artifacts/{kind}/download` | Download artifact          |
| GET    | `/settings/repos`                      | Repository settings        |
| POST   | `/settings/repos/roots`                | Add scan root              |
| POST   | `/settings/repos/scan`                 | Scan repos now             |

## Run Actions

| Action                     | Status                               |
| -------------------------- | ------------------------------------ |
| `validate-handoff`         | Implemented                          |
| `prepare-prompt`           | Implemented (generates Agent Prompt) |
| `mark-accepted`            | Implemented                          |
| `mark-needs-cleanup`       | Implemented                          |
| `generate-opencode-packet` | Implemented                          |
| `start-opencode-go`        | Implemented                          |
| `dry-run-opencode-go`      | Implemented                          |
| `check-opencode-cli`       | Implemented                          |
| `submit-agent-result`      | Implemented                          |
| `run-agent`                | Future                               |
| `run-validation`           | Implemented                          |
| `inspect-diff`             | Future                               |
| `generate-audit-packet`    | Future                               |

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

After handoff creation, Relay automatically runs Intake Review and, when there are no blockers, prepares the Agent Prompt and Agent Packet. The run workbench is then used for review: Intake Review → Agent Prompt → Agent Packet → OpenCode Go Handoff → Relay Validation.

Run detail defaults to Step 1 Intake Review. The user intentionally reviews each step and navigates forward using the step navigation links. Explicit `?step=` query parameter navigation is also supported.

Run detail is organized as a guided step-by-step workbench with a single active review step. Step navigation is server-rendered using query parameters (`?step=intake`, `?step=prompt`, `?step=handoff`, etc.). Each step panel includes a status chip, purpose description, and actionable controls based on current run state:

- **Step 1 — Intake Review**: Default active step. Shows validation checks, warnings, blockers, and the Intake Review panel. The "Run Intake Review" button changes to "Re-run Intake Review" after first run. Original handoff is available collapsed.
- **Step 2 — Agent Prompt**: Shows the Original → Agent Prompt hunk diff inline with View/Download links. Buttons toggle between "Generate Agent Prompt" and "Regenerate Agent Prompt".
- **Step 3 — Agent Packet**: Shows packet preview, View/Download links. Buttons toggle between "Generate Agent Packet" and "Regenerate Agent Packet".
- **Step 4 — OpenCode Go Handoff**: Shows preflight readiness, OpenCode adapter configuration (binary, model, agent, working directory, command preview), and an explicit "Start OpenCode Go" button. Adapter readiness/blockers are visible at the top of the adapter section. Execution status and captured artifacts (stdout, stderr, combined log) are displayed after a run. Manual agent result intake remains available as a collapsed fallback section.
- **Step 5 — Relay Validation**: Runs Relay-extracted validation commands locally after agent result. Requires an agent result before the validation button is enabled.
- **Step 6 — Diff/Audit**: Future, grayed out.

Clarifications:

- Relay does not execute OpenCode automatically. Execution only starts when the user explicitly clicks "Start OpenCode Go".
- Step 4 shows the OpenCode adapter configuration (binary, args, model, agent, working directory, command preview) in a details panel.
- If the adapter is blocked (e.g., missing model mapping), an error message is shown with the specific env var to set.
- If preflight checks are blocked, the Start button remains disabled.
- Relay captures stdout, stderr, and a combined log as run artifacts after execution.
- Relay extracts assistant text from JSONL stdout events and parses DONE/BLOCKED final output automatically.
- Relay does not persist UNKNOWN results automatically from JSON noise.
- Relay Validation remains user-triggered after agent result.
- Manual agent result intake remains available as a fallback.
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

### Dry Run / Preview

Step 4 provides a **Dry Run / Preview Command** button that builds the same OpenCode invocation that Start will use, but does not execute it. The preview is saved as an `opencode_dry_run_json` artifact for review.

Dry Run never calls the command runner.

### Start behavior

- Execution is manual only. Relay never starts OpenCode automatically.
- Relay invokes `opencode run --format json --dir <repo> --agent <agent> --model <model> --thinking max` with the compact Agent Prompt piped into stdin.
- Relay captures stdout and stderr as separate artifacts and a combined log.
- Relay records execution status, exit code, start/end timestamps, and error messages in the `agent_executions` table.
- Relay extracts assistant text from JSONL stdout events and persists DONE/BLOCKED results through the agent result path.
- Relay does not persist UNKNOWN results automatically from JSON noise.
- Relay does not run validation commands after OpenCode exits.
- Relay does not inspect git diffs or create branches.
- Manual result fallback remains available.

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

Validation command execution is user-triggered. Relay captures stdout, stderr, exit code, duration, and timeout state as run artifacts/checks. Relay can execute OpenCode only through the explicit Step 4 OpenCode adapter. Relay does not run validation automatically after OpenCode exits and does not inspect diffs.

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
