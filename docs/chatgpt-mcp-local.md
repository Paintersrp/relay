# Secure Local ChatGPT MCP Tunnel

Relay uses `scripts/local/chatgpt-mcp.mjs` to configure and run the supported tunnel client. Root package scripts are the stable operator interface; they delegate to that helper rather than implementing a second MCP server.

## Commands

```bash
npm run chatgpt-mcp:help
npm run chatgpt-mcp:init
npm run chatgpt-mcp:doctor
npm run chatgpt-mcp:start
```

- `init` writes or refreshes the selected tunnel-client profile and then runs tunnel-client diagnostics.
- `start` runs the configured tunnel for daily use.
- `doctor` checks local configuration, the local Relay MCP path, tunnel-client availability, and tunnel-client diagnostics.
- `help` prints the current setup and command surface.

Add `-- --skip-relay-check` to `init`, `start`, or `doctor` only when intentionally bypassing the local MCP reachability/self-test check.

## Environment

Copy the relevant section of `.env.example` to ignored `.env` or `.env.local`.

Required for tunnel operation:

```dotenv
TUNNEL_PROFILE=relay-mcp
TUNNEL_ID=tunnel_REPLACE_ME
CONTROL_PLANE_API_KEY=sk-REPLACE_ME
```

Default transport and profile:

```dotenv
TUNNEL_MCP_TRANSPORT=stdio
RELAY_MCP_PROFILE=planner
TUNNEL_HEALTH_LISTEN_ADDR=127.0.0.1:8082
```

Optional when the tunnel client is not on `PATH`:

```dotenv
TUNNEL_CLIENT_PATH=C:\Tools\relay-mcp-tunnel\tunnel-client.exe
```

Process environment values take precedence over `.env` and `.env.local`. Never commit real tunnel IDs, control-plane keys, bearer tokens, or signed artifact URLs.

## Profiles

`RELAY_MCP_PROFILE` accepts exactly:

- `planner` — five Plan and Run preparation actions;
- `auditor` — five validation, Run, packet, artifact, and decision actions;
- `local_operator` — ordered eight-action union.

Missing or invalid input fails closed to `planner` and prints an explicit fallback message.

## Default stdio transport

Stdio is the supported default:

1. `chatgpt-mcp:init` supplies the tunnel client with the command for `scripts/local/relay-mcp-stdio.mjs`.
2. The launcher starts `cmd/mcpserver` using the selected profile.
3. JSON-RPC travels through stdin/stdout.
4. The launcher proxies termination signals and keeps server stderr separate from protocol stdout.

The launcher's `--self-test` mode verifies:

- `initialize`;
- `notifications/initialized`;
- `ping`;
- paginated `tools/list`;
- exact ordered profile inventory;
- `artifact_file` OpenAI file-parameter metadata and required fields.

`doctor` runs this self-test in stdio mode unless `--skip-relay-check` is supplied.

## Advanced HTTP transport

HTTP mode is for advanced or development use:

```dotenv
TUNNEL_MCP_TRANSPORT=http
RELAY_MCP_URL=http://127.0.0.1:8080/mcp
```

Start the Relay daemon separately:

```bash
go run ./cmd/relay
```

The helper checks `/mcp` with POST JSON-RPC `ping` before initialization, start, or diagnostics unless the check is skipped.

When `RELAY_MCP_AUTH_TOKEN` is configured on the Relay daemon, `/mcp` requires a bearer token. The current helper reachability check does not attach an Authorization header, so the built-in HTTP tunnel workflow is intended for loopback-only tokenless connector proof. Prefer stdio. Do not expose an unauthenticated HTTP endpoint.

## Health listener

`TUNNEL_HEALTH_LISTEN_ADDR` belongs to tunnel-client, not Relay. Its default `127.0.0.1:8082` is independent of:

- Relay API/MCP on `8080`;
- Relay web on `3000`.

The helper prints the local tunnel-client health/admin UI address when starting.

## Diagnostics

Run:

```bash
npm run chatgpt-mcp:doctor
```

The report includes:

- presence of `.env` and `.env.local`;
- tunnel ID configuration state;
- control-plane key configuration state without printing the key;
- resolved tunnel-client path;
- selected MCP transport and profile;
- stdio self-test or HTTP ping result;
- tunnel health listener.

Tunnel-client stdout and stderr are passed through `CONTROL_PLANE_API_KEY` redaction before display.

## Troubleshooting

### Tunnel ID or key is reported missing

Replace placeholder values in ignored local environment configuration. Do not edit `.env.example` with real values.

### Tunnel client is not found

Install `tunnel-client` on `PATH` or set `TUNNEL_CLIENT_PATH` to the executable.

### Stdio self-test fails

Run the launcher directly to obtain focused diagnostics:

```bash
RELAY_MCP_PROFILE=planner node scripts/local/relay-mcp-stdio.mjs --self-test
```

Confirm Go is available and the workflow database/artifact paths are writable.

### HTTP ping fails

Confirm `go run ./cmd/relay` is running and `RELAY_MCP_URL` points to `http://127.0.0.1:8080/mcp`. HTTP accepts POST only.

### Wrong tool inventory

Check `RELAY_MCP_PROFILE`. The server and launcher require exact profile membership and order; there is no compatibility or legacy profile.
