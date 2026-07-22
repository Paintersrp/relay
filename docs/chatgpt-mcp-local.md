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

`start:all` is the daily one-command workflow. It reuses a healthy Relay daemon, starts Relay only when none of the three role endpoints are healthy, connects all three native runtimes, waits for readiness, and fails closed on partial health. `stop:all` stops the three owned aliases and only stops Relay when this workflow started it.

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

The three IDs must be unique and operator-provided. `init:all` attaches each existing ID to its exact role endpoint; it does not create remote tunnel objects.

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
# RELAY_MCP_WAYFINDER_HEALTH_ADDR=127.0.0.1:18201
# RELAY_MCP_PLANNER_HEALTH_ADDR=127.0.0.1:18202
# RELAY_MCP_AUDITOR_HEALTH_ADDR=127.0.0.1:18203
# RELAY_MCP_STARTUP_TIMEOUT_MS=30000
# RELAY_MCP_POLL_INTERVAL_MS=250
# RELAY_MCP_STATE_FILE=data/transport/chatgpt-mcp-all.json
```

The aggregate defaults are:

| Role | Local endpoint | Native alias | Native profile | Health address |
| --- | --- | --- | --- | --- |
| Wayfinder | `http://127.0.0.1:8080/mcp/wayfinder` | `relay-wayfinder` | `relay-wayfinder` | `127.0.0.1:18201` |
| Planner | `http://127.0.0.1:8080/mcp/planner` | `relay-planner` | `relay-planner` | `127.0.0.1:18202` |
| Auditor | `http://127.0.0.1:8080/mcp/auditor` | `relay-auditor` | `relay-auditor` | `127.0.0.1:18203` |

No aggregate `/mcp` URL or retired `/mcp/v1/...` URL is used for the three app registrations. The role paths are fixed and derived from one role definition in the local Node tooling.

## Native runtime supervision

The installed supported binary was inspected before implementation. Its `runtimes` commands provide `connect`, `list`, `status`, `stop`, and `rm`; `connect` accepts `--tunnel-id`, `--mcp-server-url`, `--alias`, `--profile`, and `--runtime-api-key`. Relay therefore uses native runtime supervision rather than maintaining a second Node process supervisor. The runtime key is passed as the supported `env:CONTROL_PLANE_API_KEY` reference, never as a command-line secret.

`init:all` runs one `runtimes connect` command per role and reports every result. `doctor:all` checks configuration, tunnel-client availability, all three JSON-RPC `ping` endpoints, native runtime status/readiness, and role binding. `status:all` prints one redacted row for Relay and each role. Tunnel IDs are abbreviated and secrets are redacted from child output.

The aggregate state file is local-only and records aliases, profiles, endpoints, health addresses, Relay ownership, and the Relay PID; it never records the control-plane key. A lock file prevents duplicate aggregate startup. Stale lock/state data is recoverable: a dead lock owner is removed, and `stop:all` is safe when runtimes are already stopped. On startup failure or termination, all successfully connected aliases are stopped and a launcher-owned Relay process is terminated. POSIX process groups and Windows process trees use platform-specific shutdown adapters.

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

Run `npm run chatgpt-mcp:doctor:all` first. It reports missing or duplicate IDs, duplicate aliases/profiles/health addresses, unavailable binaries, failed role pings, missing runtimes, readiness failures, and binding mismatches without printing secrets.

Do not claim live tunnel success without a valid operator runtime key and three existing tunnel IDs. Do not put real IDs or keys in `.env.example` or committed documentation.
