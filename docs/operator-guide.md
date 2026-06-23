# Relay Operator Guide

This guide provides a comprehensive setup, configuration, and workflow reference for a local Relay operator. Relay is a local-first handoff/run orchestration workbench. It helps turn reviewed Planner handoffs into executable Relay runs, run artifacts, validation evidence, and audit logs.

---

## Audience and Scope

This document is intended for local operators, planners, and auditors running Relay on a local machine.

*   **Planners** submit pass plans and handoffs to schedule work.
*   **Operators** configure repositories, manage tunnels, trigger local execution/validation, and review outputs.
*   **Auditors** inspect generated run artifacts, git diffs, and validation results to close out passes.

All procedures in this guide are local-first. Relay does not require or rely on GitHub PRs, GitHub CI, GitHub Actions, or remote repository administration for its local audit workflows.

---

## Local Process and Port Layout

Relay consists of a Go backend, a React/TanStack Start frontend, a stdio/HTTP Model Context Protocol (MCP) server, and a ChatGPT local tunnel client.

### Port Layout

Ensure the following ports are free before starting local processes:

*   **Relay HTTP API Daemon (`cmd/relay`)**: Port `8080` (binds to `http://127.0.0.1:8080` by default). Can be overridden via the `PORT` environment variable.
*   **Relay MCP Server (HTTP Mode)**: Port `8081` (used when running the daemon in HTTP mode; default in the tunnel script is `http://127.0.0.1:8081/mcp`).
*   **ChatGPT Local Tunnel Health/Admin Listener**: Port `8082` (binds to `http://127.0.0.1:8082` by default). Can be overridden via the `TUNNEL_HEALTH_LISTEN_ADDR` environment variable.
*   **React Workbench Dev Server (`apps/web`)**: Served dynamically. The dev server uses the port reported by the startup output of `npm --prefix apps/web run dev` (typically port `3000` or `5173`). Configure your local environment if a fixed port is needed.

### Default Filesystem Paths

*   **SQLite Database**: Defaults to `data/relay.sqlite` in the repository root. Can be overridden via `RELAY_DB_PATH`.
*   **Artifacts Directory**: Defaults to `data/artifacts` in the repository root. Can be overridden via `RELAY_ARTIFACTS_DIR`.

---

## First-Time Setup

1.  **Install Prerequisites**: Ensure you have Go 1.25+, Node.js 18+, `sqlc`, `goose`, and `templ` installed.
2.  **Install Dependencies**:
    ```bash
    npm install
    cd apps/web && npm install && cd ../..
    ```
3.  **Configure Environment**: Copy `.env.example` to `.env` or `.env.local` in the repository root:
    ```bash
    cp .env.example .env.local
    ```
4.  **Run Migrations & Code Generation**:
    ```bash
    make db-migrate
    make sqlc
    make templ
    ```
5.  **Build and Run**:
    *   Start the backend (HTTP API daemon):
        ```bash
        go run ./cmd/relay
        ```
    *   Start the React workbench (in a separate terminal):
        ```bash
        npm run dev:web
        ```

---

## Project and Repository Registration

Relay manages code bases through **Projects** and **Project Repositories**. You do not need to perform manual database edits to register repositories.

### Project and Repository Registry UI
Open the React workbench and navigate to the Projects list (`/projects`). Here you can:
*   Create a new project by specifying its name and configuration.
*   Add a repository to the project, configuring its local filesystem path, git branch context, and role.
*   Manage repository configurations, toggling status or updating settings.

### Project and Repository API Routes
For custom scripting, the Go backend provides the following routes:
*   `GET /api/projects` — List registered projects.
*   `POST /api/projects` — Create a new project.
*   `GET /api/projects/{projectId}` — Get project detail.
*   `POST /api/projects/{projectId}/repositories` — Upsert project repository settings.
*   `POST /api/projects/{projectId}/repositories/{repoId}/update` — Update repository roles or options.
*   `POST /api/projects/{projectId}/repositories/{repoId}/set-enabled` — Enable/disable a repository within a project.

---

## ChatGPT Local MCP Tunnel

For the local ChatGPT tunnel workflow, the default transport is **stdio**, which executes local commands via `mcpserver`. 

> [!NOTE]
> In stdio mode, you do not need to run the Go HTTP daemon (`go run ./cmd/relay`) separately. The tunnel spawns the stdio server directly.

### Running the Tunnel

1.  **Configure credentials**: Fill in `TUNNEL_ID` and `CONTROL_PLANE_API_KEY` in `.env.local`.
2.  **Initialize the tunnel profile**:
    ```bash
    npm run chatgpt-mcp:init
    ```
3.  **Start the tunnel client**:
    ```bash
    npm run chatgpt-mcp:start
    ```
    Keep this terminal open while the tunnel is in use.
4.  **Run diagnostics**:
    ```bash
    npm run chatgpt-mcp:doctor
    ```

### Advanced HTTP Mode

If you explicitly set `TUNNEL_MCP_TRANSPORT=http` in your environment, the tunnel will connect via HTTP POST JSON-RPC. In this mode, you **must** have a separately running Relay HTTP daemon serving `/mcp` (e.g. `go run ./cmd/relay`).

---

## MCP Profiles and Tool Surfaces

Relay controls which tools are exposed to the MCP client using the `RELAY_MCP_PROFILE` environment variable.

### Supported Profiles

1.  **`local-operator` (Default)**:
    Exposes the full MCP tool surface, including context broker tools, local audit tools, and project context memory tools.
2.  **`restricted`**:
    Hides all broker/retrieval tools and exposes only the base submission and debug surface.

### Legacy Configuration Fallback

If `RELAY_MCP_PROFILE` is unset, Relay falls back to checking the legacy `RELAY_MCP_CONTEXT_BROKER_ENABLED` environment variable:
*   `RELAY_MCP_CONTEXT_BROKER_ENABLED=true` maps to `local-operator`.
*   `RELAY_MCP_CONTEXT_BROKER_ENABLED=false` maps to `restricted`.

### Registered Tool Surfaces

| Profile | Registered Tools |
|---|---|
| **Restricted** (Base Tools) | `submit_test_audit_packet`, `create_run_from_planner_handoff`, `submit_planner_pass_plan`, `list_open_runs`, `get_run_status`, `submit_audit_packet` |
| **Local Operator** (Adds Context Broker) | All base tools + `get_project`, `get_plan`, `get_pass`, `get_pass_context`, `create_source_snapshot`, `list_project_files`, `search_project_files`, `read_project_file`, `get_repository_git_status`, `get_repository_recent_commit`, `list_repository_changed_files`, `get_repository_diff`, `create_context_packet`, `get_context_packet`, `create_local_audit`, `get_local_audit`, `list_project_local_audits`, `search_project_context_memory`, `list_project_context_records`, `get_project_context_record`, `create_project_context_record`, `supersede_project_context_record` |

*Note: Exposure of MCP tools as GPT-facing actions is configuration-dependent. By default, only the two submission tools are exposed.*

---

## Managed Plan and Selected-Pass Workflow

Relay supports an optional managed plan orchestration layer where runs can be associated with specific plan passes:

1.  **Submit Plan**: A structured Plan of Passes JSON is submitted using the `submit_planner_pass_plan` tool or via the `POST /api/plans` endpoint. This creates plan/pass records in the database.
2.  **Review Pass**: Operators or Planners inspect the plan detail (`/plans/{planId}`) and select a target pass.
3.  **Associate Run**: A run is created and associated with the target pass by including its `plan_id` and `pass_id` in the handoff metadata or during `create_run_from_planner_handoff`.
4.  **State Transitions**:
    *   Creating an associated run moves the pass from `planned` to `in_progress`.
    *   Accepting the run's audit moves the pass to `completed`.
    *   Requesting revision on the run's audit keeps or returns the pass to `in_progress`.

---

## Source Snapshots and Context Packets

When the context broker is active (`local-operator` profile), the system supports advanced source-tracking services:

*   **Source Snapshot**: Creates a read-only metadata snapshot of a registered repository at a specific commit. No git history is mutated.
*   **Context Packet**: Compiles file inventory, search results, and bounded file contents into a pre-run `handoffs/context` artifact.
*   **Separation of Concerns**: Creating a context packet or source snapshot does **not** create a run, compile a canonical packet, or dispatch an executor. It is an evidence-gathering step.

---

## Handoff and Run Submission Boundaries

It is critical to distinguish the different submission tools available in the MCP surface:

*   **Plan Submission (`submit_planner_pass_plan`)**: Validates the plan format and writes metadata. It does **not** create runs or execute code.
*   **Run Submission (`create_run_from_planner_handoff`)**: Creates a run from reviewed Planner handoff markdown.
    > [!IMPORTANT]
    > Run submission requires explicit user confirmation in the chat before execution.
*   **Context Retrieval**: Tools like `get_pass_context` only gather context and do not perform any submissions or state changes.

---

## Audit and Local Audit Workflows

Relay's audit workflows are local-first and artifact-backed:

*   Audit generation writes local files (`audit_input_summary.md`, `audit_evidence_manifest.json`, and `audit_packet.md`).
*   Manual audit submissions and MCP-based submissions (`submit_audit_packet`) invoke the same backend decision service.
*   Decisions of `blocked` or `manual_review_required` map the run status to `revision_required` while retaining the original decision details in the database.
*   Audit acceptance does **not** automatically close the run or commit code. Operators must manually execute `git commit` using conventional messages suggested by the workbench.
*   Relay never mutates the git repository, pushes branches, or creates PRs.

---

## Smoke Checks

To verify the health of the entire local setup, run the smoke test suite:

```bash
npm run smoke
```

### Component Validation

You can also run narrow validation checks:

*   **Go MCP Tests**: `go test ./internal/mcp`
*   **Go Router & Server Tests**: `go test ./internal/server`
*   **TypeScript Frontend Compilation**: `npm --prefix apps/web run typecheck`
*   **Frontend Unit Tests**: `npm --prefix apps/web test`

---

## Troubleshooting

### Database is out of sync or locked
If the HTTP API daemon or MCP server reports schema mismatches, run migrations manually:
```bash
goose -dir internal/db/migrations sqlite3 data/relay.sqlite up
```

### Tunnel doctor fails with missing `tunnel-client`
Ensure `tunnel-client` (or `tunnel-client.exe` on Windows) is in your system's `PATH`. Alternatively, configure `TUNNEL_CLIENT_PATH` in `.env.local` to point to the absolute path of the binary.

### Port conflicts (e.g. port 8080 already in use)
If the Relay API daemon cannot bind to `8080`, change the port by setting `PORT` in `.env.local` (e.g. `PORT=8085`). Make sure to update `VITE_RELAY_API_BASE_URL` in `apps/web/.env` to point to the new port.

### Stdio self-test missing tools
If `chatgpt-mcp:doctor` complains that required tools are missing, ensure `RELAY_MCP_PROFILE` is set correctly, and the database has been initialized with the correct database migrations.

### HTTP mode connection refused
If you are running the ChatGPT tunnel in HTTP mode and get connection failures, verify that the Go daemon is running (`go run ./cmd/relay`) and that `RELAY_MCP_URL` in `.env.local` points to the correct address/port of the running server.

---

## Safety Boundaries

To maintain a secure local-first workflow, respect these safety limits:

1.  **No Git Mutation**: Relay does not perform git commits, pushes, checkouts, branch creation, or resets. Do not document or attempt to use tools for these actions.
2.  **No Shell/Execution**: Relay does not support running arbitrary shell commands via MCP.
3.  **No Secret Leaks**: Never commit `.env` or `.env.local` files, and never paste API keys, tunnel credentials, or private tokens into planner handoffs or MCP tool parameters.
4.  **Local Isolation**: Ensure all database files (`relay.sqlite`) and artifacts (`data/artifacts/*`) are kept local. Do not attempt to sync these files to remote networks or push them to version control.
