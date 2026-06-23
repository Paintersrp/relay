# ChatGPT Local MCP Tunnel

## Default workflow

The default ChatGPT local tunnel path uses Relay's existing stdio MCP server through `cmd/mcpserver`.

After one-time setup, the happy path is a single terminal:

1. Configure `.env.local`.
2. Run `npm run chatgpt-mcp:init` once.
3. Run `npm run chatgpt-mcp:start` whenever you want ChatGPT connected.
4. Keep that one terminal open while ChatGPT uses the connector.

You do not manually start `go run ./cmd/relay` for the default ChatGPT tunnel workflow.

## Configure `.env.local`

Use the root example as the template and keep secrets in ignored local env files only.

Required values:

- `TUNNEL_PROFILE`
- `TUNNEL_ID`
- `CONTROL_PLANE_API_KEY`

Default transport:

```dotenv
TUNNEL_MCP_TRANSPORT=stdio
```

Optional values:

- `TUNNEL_CLIENT_PATH` if `tunnel-client` is not already on `PATH`
- `RELAY_MCP_SERVER_BIN` if you want the stdio launcher to use a prebuilt Relay MCP binary instead of `go run ./cmd/mcpserver`
- `RELAY_MCP_STDIO_COMMAND` only if you need to override the generated launcher command

Root `.env` and `.env.local` are ignored by git and must not be committed.

## Initialize once

```bash
npm run chatgpt-mcp:init
```

In the default `stdio` mode this configures the tunnel profile with an MCP command that launches:

```text
node scripts/local/relay-mcp-stdio.mjs
```

That launcher resolves the repo root, loads root `.env` and `.env.local`, and starts the real Relay MCP stdio server.

## Start for daily use

```bash
npm run chatgpt-mcp:start
```

This starts `tunnel-client run --profile <profile>`. The tunnel profile then launches Relay MCP through stdio for you. No second Relay terminal is required in the default path.

The tunnel-client local admin UI may still be available at `http://127.0.0.1:8080/ui`. That UI is not Relay MCP.

## Diagnose failures

```bash
npm run chatgpt-mcp:doctor
```

In the default mode this runs the local stdio launcher self-test first, then runs `tunnel-client doctor`. It does not require a local HTTP `/mcp` endpoint unless you explicitly switch to HTTP mode.

## Optional HTTP mode

HTTP mode remains available for advanced or local dev cases:

```dotenv
TUNNEL_MCP_TRANSPORT=http
RELAY_MCP_URL=http://127.0.0.1:8081/mcp
```

When you choose HTTP mode, you must separately run the Relay HTTP daemon, for example:

```bash
go run ./cmd/relay
```

HTTP mode is optional and is not the default ChatGPT tunnel workflow.

## ChatGPT connector setup

In ChatGPT connector settings:

- choose Tunnel
- select or paste the tunnel ID
- verify the tunnel is associated with the intended ChatGPT workspace

## Safety

- Do not commit `.env` or `.env.local`.
- Do not commit tunnel IDs, control-plane keys, or other secrets.
- Do not paste secrets into Planner handoffs or Relay MCP tool arguments.
- The current HTTP `/mcp` no-auth behavior is for local validation only and is not production deployment guidance.
