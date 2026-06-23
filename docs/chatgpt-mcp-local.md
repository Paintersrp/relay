# ChatGPT Local MCP Tunnel

## What this is

This workflow connects ChatGPT to Relay's existing local `/mcp` endpoint by using OpenAI Secure MCP Tunnel.

It does not create a new MCP server, and it does not use `bin/relay-mcpserver.exe` for the ChatGPT tunnel path.

## Required local files

- `scripts/local/chatgpt-mcp.mjs`
- `.env.example`
- `.env` or `.env.local`

## Configure

Copy the root example env file to a root local env file:

```bash
cp .env.example .env
# or
cp .env.example .env.local
```

Windows `cmd.exe` equivalent:

```cmd
copy .env.example .env
copy .env.example .env.local
```

Then fill in:

- `TUNNEL_PROFILE`
- `TUNNEL_ID`
- `RELAY_MCP_URL`
- `CONTROL_PLANE_API_KEY`

Root `.env` and `.env.local` are ignored by git and must not be committed. `.env.local` is a good choice for machine-local overrides.

## Start Relay

Start the existing Relay daemon:

```bash
go run ./cmd/relay
```

Default local MCP URL:

```text
http://127.0.0.1:8080/mcp
```

## Initialize the tunnel profile once

```bash
npm run chatgpt-mcp:init
```

This validates local config, checks the Relay `/mcp` endpoint, initializes the tunnel profile against the existing HTTP MCP endpoint, and runs tunnel diagnostics.

## Run the tunnel for daily use

```bash
npm run chatgpt-mcp:start
```

Keep that terminal open while ChatGPT uses the connector.

## Diagnose failures

```bash
npm run chatgpt-mcp:doctor
```

This reports env-file presence, local endpoint reachability, tunnel-client discovery, and tunnel profile diagnostics without printing secrets.

## ChatGPT connector setup

In ChatGPT connector settings:

- choose Tunnel
- select or paste the tunnel ID
- verify the tunnel is associated with the intended ChatGPT workspace

## Safety

- Do not commit `.env` or `.env.local`.
- Do not paste API keys into Planner handoffs or Relay MCP tool calls.
- The current local `/mcp` no-auth behavior is for local validation only and is not production deployment guidance.
