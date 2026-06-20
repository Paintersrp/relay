# Relay

Relay is a local-first handoff/run orchestration web app for turning reviewed Planner handoffs into Relay runs, run artifacts, validation evidence, audit handoffs, and manual closeout support.

## Current Status

Relay is a local-first handoff/run orchestration workbench.

### Implemented / Current

| Capability | Description |
| --- | --- |
| UI Handoff Intake | Create runs by pasting/uploading handoffs in the React UI |
| MCP Handoff Intake | Create runs via the current Planner Project-facing MCP action (`create_run_from_planner_handoff`) after user confirmation |
| Run Storage | Run metadata and artifact storage |
| Intake Review | Parse and validate handoff structure before execution |
| Agent Prompt | Preparation and handoff transformation for agents |
| Agent Results | Manual agent result intake |
| Validation | Local/user-triggered validation command execution |
| Git Diff | Local git diff inspection |
| Audit Handoff | Generation of audit handoffs for review |
| Commit Support | Manual git commit support |
| React Workbench | Primary workflow UI |
| Go Backend | Ownership of JSON APIs, orchestration, run lifecycle, artifact storage, utility server-rendered pages, and event streaming |

### Not Current / Future

| Capability | Description |
| --- | --- |
| Repo-Agent Execution | Fully automatic repo-agent execution |
| Branch Management | Automatic branch/worktree creation |
| Repair | Automatic validation failure repair |
| Audit | Automatic AI audit/closeout |
| Additional MCP Actions | Additional Planner-facing MCP actions beyond handoff submission are not current |

### Run Actions

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

## Core Concepts

These are human-readable documentation definitions only and do not add schema fields or runtime semantics.

- **Planner handoff**: A reviewed Markdown implementation handoff containing a selected pass, scope, constraints, repo facts, implementation requirements, validation expectations, and audit priorities.
- **Relay run**: A local Relay work item created from a handoff grouping metadata, repository context, lifecycle status, artifacts, validation evidence, diff evidence, audit handoffs, and commit suggestions.
- **Intake Review**: A validation step to review and approve the structure and scope of a new run before processing.
- **Agent Prompt / transformed prompt**: A compact execution prompt generated from the original handoff tailored for a running repo agent, excluding Relay validation commands.
- **Validation evidence**: The captured stdout, stderr, exit code, duration, and timeout state of local/user-triggered validation commands.
- **Audit handoff**: A compact markdown artifact containing run metadata, agent results, validation results, and git diff evidence intended for review.
- **Current Project-facing MCP action**: The single `create_run_from_planner_handoff` action available to the Planner.
- **Local/dev MCP server tool inventory**: Additional MCP tools used for local development and validation, not exposed to the Planner Project.

## Current Workflow

Relay's current workflow is:

1. Create a run from a Planner handoff through the React UI or current Planner Project-facing MCP action.
2. Build Intake Review.
3. Detect model, branch, repo, scoped files, validation commands, final output contract, commit suggestions, warnings, and blockers.
4. Warn or block when the selected repo does not match the handoff scope.
5. Generate a transformed Agent Prompt or execution-preparation artifact for the running repo agent.
6. Store the original handoff and generated artifacts separately.
7. Perform manual agent result intake or current user-triggered execution path.
8. Run validation locally.
9. Store validation evidence.
10. Inspect git diff for local changes.
11. Generate audit handoff for review.
12. Prepare git commit message suggestion based on handoff, audit, and diff evidence.
13. Review and manually run `git commit` in the repo.

Relay does not stage files, commit, push, or mutate git on the user's behalf.

## MCP Bridge & Current Project Action

Relay includes an MCP (Model Context Protocol) integration. The **current Planner Project-facing MCP Action** is exactly as follows:

*   **Action:** `create_run_from_planner_handoff` — submit a reviewed Planner handoff artifact/content to Relay.
*   **Result:** Relay creates and starts a new run from that handoff, and owns all downstream processing.
*   **User Confirmation:** The Planner must explicitly ask for user confirmation after handoff creation before invoking this MCP run-creation action.

No Planner-facing status, list, audit, or dispatch MCP tools are currently available unless Project configuration deliberately changes. The local/dev/server tools documented in `docs/mcp.md` are not automatically Project-facing Planner actions.

## Safety Boundaries

The current Planner Project-facing MCP action does **not** expose or claim availability for:
*   Status queries or run listing
*   Audit packet submission
*   Executor dispatch
*   Shell execution or command running
*   Arbitrary file access or file reads/writes
*   Git operations (commits, pushes, branch creation)
*   Secrets, tokens, auth headers, private keys, signed URLs, tunnel URLs, cookies, credentials, or other sensitive material in handoffs or MCP payloads

Any broader list of tools such as `list_open_runs`, `get_run_status`, `submit_audit_packet`, and `submit_test_audit_packet` that may exist in the `mcpserver` or local development contexts are strictly local/dev/server MCP tool inventory or future/internal capabilities. They are **not** current Planner Project actions unless project configuration explicitly changes. MCP run submission also does not use executor briefs, canonical packets, validation reports, repair reports, audit packets, or surrounding chat context as the payload.

## Stack

**Go backend (primary):**
- Go `net/http` + `chi` router
- `templ` for server-rendered utility views (instructions, settings, artifact viewer)
- SQLite via `database/sql`
- `sqlc` for typed queries
- `goose` for migrations
- Tailwind CSS v4
- TypeScript bundled with esbuild

**React workbench (primary workflow UI):**
- TanStack Start (React + Vite + file-based routing)
- TanStack Router
- TanStack React Query
- Tailwind CSS v4 + shadcn/ui
- TypeScript

## Setup

### Prerequisites

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

### Build & Run

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

### Development live reload

Relay supports a local dev live-reload workflow for server-rendered development.

```bash
make dev
```

This runs Tailwind watch, esbuild watch, templ watch, and an Air-managed Go server with `RELAY_DEV_RELOAD=1`.

If running directly on Windows PowerShell:

```powershell
$env:RELAY_DEV_RELOAD="1"
npm run dev
```

## React Workbench Frontend

`apps/web` is the **primary workflow UI** for Relay. All run creation, intake, prepare, execute, and audit steps are served by the React workbench at port 3000.

The Go backend (`cmd/relay`, port 8080) continues to own:
- All JSON API routes (`/api/*`)
- Orchestration, run lifecycle, and artifact storage
- Utility server-rendered pages: instructions (`/instructions/*`), repo settings (`/settings/repos*`), and raw artifact viewer/download (`/runs/{id}/artifacts/{kind}`, `/runs/{id}/artifacts/{kind}/download`)
- SSE event stream (`/api/runs/{id}/events`)

Old templ/htmx workflow routes now redirect to the React workbench.

Set `RELAY_WEB_BASE_URL` (default `http://localhost:3000`) if you run the React workbench on a different port.

By default, the Go backend allows CORS requests from `http://localhost:3000`, `http://127.0.0.1:3000`, `http://localhost:5173`, and `http://127.0.0.1:5173`. You can override or append additional origins via:
```bash
RELAY_CORS_ALLOWED_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
```

```bash
# Start the React workbench (separate terminal, port 3000):
cd apps/web
cp .env.example .env        # sets VITE_RELAY_API_BASE_URL=http://localhost:8080
npm install
npm run dev
# → http://localhost:3000
```

### React Workbench Workflow

| Path | Step |
|------|------|
| `/runs` | Run list |
| `/runs/new` | Create a run by pasting/uploading a handoff |
| `/runs/{id}/intake` | Intake Review — parse frontmatter, validate, approve |
| `/runs/{id}/prepare` | Prepare — compile handoff packet, render agent brief |
| `/runs/{id}/execute` | Execute — dispatch agent, monitor progress |
| `/runs/{id}/audit` | Audit / Close — generate audit, approve, close |

## Database

Relay uses a local SQLite database at `data/relay.sqlite` by default.

Relay applies bundled SQLite migrations on startup automatically. The `goose` CLI command remains useful for manual repair/debugging, but normal local startup does not require running it separately.

If Relay reports a stale database schema error at startup, run:

```bash
goose -dir internal/db/migrations sqlite3 data/relay.sqlite up
```

## Run Workflow Details

### New handoff intake

A run can be created from the New Handoff source picker by uploading a `.txt` / `.md` handoff file or switching to Text input to paste the handoff. Upload wins if both upload and text are submitted.

Relay derives the run title from the handoff's first `#` heading. If no H1 heading exists, the run is named `Untitled handoff`.

### Local repository discovery

Relay discovers local Git repositories from configured scan roots.
The default scan root is `D:/Code`.
Open `/settings/repos` to manage scan roots and review discovered repositories. The New Handoff page lets you select a discovered repo or use manual repo name/path entry.

### Intake review

The intake review parses the pasted handoff and shows detected model, repo, branch, scoped files, validation commands, final output contract, suggested commit message, blockers and warnings. Relay should warn before validation when the selected repo does not appear to match the handoff scope.

### Intake Remediation Handoff

When Intake Review reports warnings or blockers, Step 1 can generate an `intake_remediation_handoff` artifact. This artifact is a copyable repair prompt for revising the original handoff and resolving intake issues. Missing validation commands produce a remediation handoff that includes an actual `## Relay validation commands` section.

### Replace Original Handoff

If Intake Review finds issues, the original handoff can be replaced on the existing run from Step 1 by pasting the corrected handoff text into the textarea and clicking **Replace Handoff and Re-run Intake Review**. The action writes the new text to the `original_handoff` artifact, clears stale downstream artifacts, and re-runs Intake Review.

### Model selection

When creating a handoff, Relay parses a recommended model from the pasted handoff text automatically. If no model is found and no override is selected, Relay defaults the selected model to DeepSeek V4 Flash. The model override control is optional.

### Agent Prompt

The Agent Prompt is a compact execution prompt for the running repo agent. Relay stores the verbose original handoff as a source artifact, and the Agent Prompt (`agent_prompt`) is compact for repo-agent execution. Relay validation commands stay out of the Agent Prompt, but test implementation instructions are preserved under validation sections. The agent is told not to run validation commands by default.

### OpenCode handoff packet

Relay can generate an `opencode_handoff_packet.json` artifact after an Agent Prompt exists. The packet includes the run id, local repo path, branch/worktree metadata, selected model, recommended model, agent prompt artifact path, run artifact directory, and an explicit artifact manifest.

### Handoff preflight

Step 4 (OpenCode Go Handoff) shows a preflight readiness checklist with checks for repo path, branch, selected model, Agent Prompt artifact, and Agent Packet artifact.

### OpenCode adapter

Relay has a built-in OpenCode adapter that invokes `opencode run` in non-interactive mode with `--format json` and `--thinking max`. The compact Agent Prompt is piped into stdin.
The adapter parses JSONL text events from stdout to extract the final assistant text (DONE/BLOCKED). Execution is manual only. 

### Manual agent result intake

Relay can store the final output from an external repo agent after the user runs that agent outside Relay. It parses metadata like Build status, Test status, Count of LOC changed, and Blocker/error.

### Relay validation commands

Commands Relay should extract and run locally after agent result:

```bash
go fmt ./...
templ generate
npm run build
go test ./...
go vet ./...
```

If RTK is available, Relay or the user may prefer `rtk.exe` first, then `rtk`, then the raw command.

### Validation command runner

Relay can run validation commands for a run from the selected local repository path. Commands are user-triggered and run asynchronously. Relay captures stdout, stderr, exit code, duration, and timeout state as run artifacts/checks.

### Git Diff Evidence

Step 7 can inspect the local git worktree at the selected repo path. When **Inspect Git Diff** is triggered:
- `git status --short` is saved as `git_status_text`
- `git diff --stat` is saved as `git_diff_stat`
- `git diff --numstat` is saved as `git_diff_numstat`
- `git diff --name-status` is saved as `git_diff_name_status`
- `git diff --no-ext-diff --patch` is saved as `git_diff_patch`

Diff evidence is displayed inline in Step 7 and included in the audit handoff.

### Audit Handoff

When Relay Validation passes or after git diff evidence is collected, Step 7 provides actions to **Inspect Git Diff**, **Generate Audit Handoff**, and then proceed to **Step 8: Git Commit**.

The audit handoff generates a compact markdown artifact (`audit_handoff.md`) containing run metadata, agent results, validation results, and git diff evidence. The audit handoff is intended to be copied into GPT for audit/review.

### Git Commit Step

Step 8: Git Commit is the final workflow step. After the commit suggestion is prepared, Relay shows Step 8 with a suggested conventional commit message and a copyable command. Relay does not stage files, does not run `git commit`, and does not execute any git mutating operations. The commit message is generated deterministically and stored as `commit_message_text` and `commit_suggestion_json`.

## Routes and API

### Go backend routes (port 8080)

Workflow entry routes redirect to the React workbench (port 3000):

| Method | Path                       | Behavior                                    |
| ------ | -------------------------- | ------------------------------------------- |
| GET    | `/`                        | Redirect to React `/runs`                   |
| GET    | `/handoffs/new`            | Redirect to React `/runs/new`               |
| POST   | `/handoffs`                | Create run; redirect to React `/runs/{id}/intake` |
| GET    | `/runs/{id}`               | Redirect to React `/runs/{id}/{step}` per run status |
| GET    | `/runs/{id}/agent-run-monitor` | Redirect to React `/runs/{id}/execute`  |

Preserved utility routes:

| Method | Path                                   | Description                |
| ------ | -------------------------------------- | -------------------------- |
| GET    | `/runs/{id}/artifacts/{kind}`          | View raw artifact content  |
| GET    | `/runs/{id}/artifacts/{kind}/download` | Download artifact          |
| GET    | `/instructions`                        | Instruction assets list    |
| GET    | `/instructions/{kind}`                 | View instruction asset     |
| GET    | `/instructions/{kind}/download`        | Download instruction asset |
| GET    | `/settings/repos`                      | Repository settings        |
| POST   | `/settings/repos/roots`                | Add scan root              |
| POST   | `/settings/repos/roots/{id}/toggle`    | Toggle scan root           |
| POST   | `/settings/repos/roots/{id}/delete`    | Delete scan root           |
| POST   | `/settings/repos/scan`                 | Scan repos now             |

### JSON API routes (`/api/*`)

See `docs/api/frontend-api-contract.md` for full endpoint documentation.

## Documentation Index

- `docs/mcp.md`: MCP Server setup and configuration.
- `docs/api/frontend-api-contract.md`: JSON API documentation.
- `docs/frontend-pivot.md`: React Workbench architecture details.

### Project Structure

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

### Instruction Assets

Relay exposes canonical project instruction files at `/instructions`:
- **Surgical Chat Instructions** — Canonical handoff structure and rules (`surgical-chat-instructions.txt`)
- **AGENTS.md** — Canonical agent instructions (`AGENTS.md`)
- **.clinerules** — Canonical Cline rules (`.clinerules`)

## Roadmap / Not Implemented Yet

The current local-first flow does not yet implement:

- automatic repo-agent execution
- automatic branch/worktree creation
- automatic validation failure repair
- automatic diff-based audit / AI audit automation
