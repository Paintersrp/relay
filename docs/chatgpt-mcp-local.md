# Secure Local ChatGPT MCP Tunnel

Relay owns one HTTP daemon and three role-app MCP endpoints. The supported operator workflow is in `scripts/local/chatgpt-mcp.mjs`; the aggregate commands use the installed tunnel-client native runtime supervisor.

## Normal flow

```text
one-time: npm run chatgpt-mcp:init:all
check:    npm run chatgpt-mcp:doctor:all
daily:    npm run chatgpt-mcp:start:all
status:   npm run chatgpt-mcp:status:all
stop:     npm run chatgpt-mcp:stop:all
```

`start:all` is the daily one-command workflow. It reuses a healthy Relay daemon, starts Relay only when none of the three role endpoints are healthy, verifies each native runtime from structured status and health output, and fails closed on partial health. If a verified owned Relay is alive but unhealthy, it is stopped and confirmed before a controlled restart; an identity mismatch or inspection failure fails closed. Native runtimes remain active after successful startup; the short-lived startup lock covers state read, orchestration, and commit. `stop:all` stops persisted aliases (including only unresolved aliases on a retry) and only stops Relay when this workflow started it and its recorded process identity still matches.

Three ChatGPT app registrations are required. Each registration selects a distinct tunnel ID: Wayfinder selects the Wayfinder tunnel, Planner selects the Planner tunnel, and Auditor selects the Auditor tunnel. A tunnel object is registered as a whole; the roles cannot be separate channels inside one tunnel.

The single-profile commands remain supported:

```bash
npm run chatgpt-mcp:init
npm run chatgpt-mcp:doctor
npm run chatgpt-mcp:start
npm run chatgpt-mcp:help
```

## Configuration

Copy values to ignored `.env` or `.env.local`. Process environment has highest precedence, followed by `.env.local`, `.env`, and defaults.

```dotenv
CONTROL_PLANE_API_KEY=sk_REPLACE_ME

RELAY_MCP_WAYFINDER_TUNNEL_ID=tunnel_REPLACE_ME
RELAY_MCP_PLANNER_TUNNEL_ID=tunnel_REPLACE_ME
RELAY_MCP_AUDITOR_TUNNEL_ID=tunnel_REPLACE_ME
```

The three IDs must be unique and operator-provided. Each must match `tunnel_` followed by exactly 32 lowercase hexadecimal characters, for example `tunnel_11111111111111111111111111111111`. `init:all` attaches each existing ID to its exact role endpoint; it does not create remote tunnel objects.

Optional aggregate overrides:

```dotenv
# TUNNEL_CLIENT_PATH=C:\Tools\relay-mcp-tunnel\tunnel-client.exe
# RELAY_MCP_RELAY_COMMAND=go run ./cmd/relay
# RELAY_MCP_BASE_URL=http://127.0.0.1:8080
# RELAY_MCP_PROFILE_DIR=C:\Tools\relay-mcp-tunnel\profiles
# RELAY_MCP_WAYFINDER_ALIAS=relay-wayfinder
# RELAY_MCP_PLANNER_ALIAS=relay-planner
# RELAY_MCP_AUDITOR_ALIAS=relay-auditor
# RELAY_MCP_WAYFINDER_PROFILE=relay-wayfinder
# RELAY_MCP_PLANNER_PROFILE=relay-planner
# RELAY_MCP_AUDITOR_PROFILE=relay-auditor
# RELAY_MCP_STARTUP_TIMEOUT_MS=30000
# RELAY_MCP_POLL_INTERVAL_MS=250
# RELAY_MCP_STATE_FILE=data/transport/chatgpt-mcp-all.json
```

The aggregate defaults are:

| Role | Local endpoint | Native alias | Native profile | Health URL |
| --- | --- | --- | --- | --- |
| Wayfinder | `http://127.0.0.1:8080/mcp/wayfinder` | `relay-wayfinder` | `relay-wayfinder` | native generated URL |
| Planner | `http://127.0.0.1:8080/mcp/planner` | `relay-planner` | `relay-planner` | native generated URL |
| Auditor | `http://127.0.0.1:8080/mcp/auditor` | `relay-auditor` | `relay-auditor` | native generated URL |

No aggregate `/mcp` URL or retired `/mcp/v1/...` URL is used for the three app registrations. The role paths are fixed and derived from one role definition in the local Node tooling.

## Native runtime supervision

The installed supported binary is tunnel-client `0.0.9+62b9b42f698ec5319d2115e0c0ff1dcf6557d7ae`. Its `runtimes` commands provide `connect`, `list`, `status`, `stop`, and `rm`; `connect` accepts `--tunnel-id`, `--mcp-server-url`, `--alias`, `--profile`, and `--runtime-api-key`, and emits JSON when requested. Native runtime profiles generate an ephemeral loopback health listener and expose the effective URL or URL file through runtime state. Relay passes that URL to `tunnel-client health --url ... --json` or `--url-file ... --json`; it never assigns aggregate health ports.

`init:all` runs one `runtimes connect` command per role and requires tunnel-client 0.0.9 status JSON to contain the exact fields `alias`, `tunnel_id`, `profile_name`, `health_url`, `health_url_file`, `process_running`, and nested `process.target_kind`/`process.target_value` when a process is active. The nested target kind must be `server_url`, and `process.target_value` is the exact HTTP(S) MCP endpoint used for binding checks. A known alias with `process: null` is recoverable launch residue, not malformed state. A zero connect exit alone is not success; valid JSON is classified independently from exit status. `doctor:all` and `status:all` use the same adapter. Tunnel IDs are abbreviated and secrets are redacted from child output. Existing correctly configured runtimes are reused; endpoint or profile drift stops and reconnects that alias before verification. The checked-in sanitized status fixture in `scripts/local/test-fixtures/tunnel-client-0.0.9-status.json` mirrors this production shape and reports `/healthz`.

The version-3 aggregate state file is local-only and records desired bindings, retired/residual bindings awaiting cleanup, and bounded verified Relay ownership metadata. Identity values are recursively redacted before persistence; command display and child output use the same key redaction. State writes use a same-directory temporary file, flush, and atomic replacement; version-2, malformed, or unsupported state fails closed and requires an explicit `init:all` migration. Alias changes are reconciled by role key: the persisted alias is confirmed stopped before the replacement is connected. Partial `stop:all` writes retry state containing only unconfirmed runtimes and any still-owned Relay. Relay identity adapters use `/proc/<pid>/stat`, controlled `ps` start identity on macOS, and CIM `CreationDate` on Windows. A lock file records a PID, platform creation identity, and owner token; a missing or mismatched identity is stale rather than proof of ownership. `SIGINT`, `SIGTERM`, startup failure, and `stop:all` perform exhaustive journaled cleanup and release only the current lock owner. POSIX process groups and Windows process trees use platform-specific shutdown adapters.

## Single-profile compatibility

The legacy single-profile interface still supports the aggregate stdio MCP launcher:

```dotenv
TUNNEL_PROFILE=relay-mcp
TUNNEL_ID=tunnel_REPLACE_ME
RELAY_MCP_PROFILE=planner
TUNNEL_MCP_TRANSPORT=stdio
TUNNEL_HEALTH_LISTEN_ADDR=127.0.0.1:8082
```

For advanced single-profile HTTP use, select one role endpoint explicitly:

```dotenv
TUNNEL_MCP_TRANSPORT=http
RELAY_MCP_URL=http://127.0.0.1:8080/mcp/planner
```

The old `/mcp/v1/...` routes remain removed. HTTP checks use POST JSON-RPC `ping`; other methods are not a readiness signal.

## Troubleshooting

Run `npm run chatgpt-mcp:doctor:all` first. It reports missing or duplicate IDs, duplicate aliases/profiles, unavailable binaries, failed role pings, missing runtimes or native health URLs, readiness failures, and binding mismatches without printing secrets.

Automated tests cover the production-shaped adapter and fake lifecycle, but do not cover real credentials or real tunnel objects. Do not claim live tunnel success without a valid operator runtime key and three existing tunnel IDs. Do not put real IDs or keys in `.env.example` or committed documentation.
