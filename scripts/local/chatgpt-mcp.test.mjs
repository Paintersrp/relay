import { readFile } from "fs/promises";
import { join } from "path";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const scriptPath = join(process.cwd(), "scripts", "local", "chatgpt-mcp.mjs");

describe("chatgpt-mcp local script guardrails", () => {
  it("keeps stdio transport and health listener defaults local", async () => {
    const source = await readFile(scriptPath, "utf8");

    assert.match(
      source,
      /DEFAULT_TUNNEL_MCP_TRANSPORT\s*=\s*"stdio"/,
    );
    assert.match(
      source,
      /DEFAULT_TUNNEL_HEALTH_LISTEN_ADDR\s*=\s*"127\.0\.0\.1:8082"/,
    );
  });

  it("threads the configured health listener through start and doctor flows", async () => {
    const source = await readFile(scriptPath, "utf8");
    const healthFlagUses = source.match(/"--health\.listen-addr"/g) ?? [];
    const healthConfigUses = source.match(/config\.tunnelHealthListenAddr/g) ?? [];

    assert.ok(healthFlagUses.length >= 3);
    assert.ok(healthConfigUses.length >= 5);
  });

  it("keeps doctor local checks explicit and skippable", async () => {
    const source = await readFile(scriptPath, "utf8");

    assert.match(source, /--skip-relay-check/);
    assert.match(source, /config\.tunnelMcpTransport === "stdio"[\s\S]*?runRelayMcpSelfTest\(config\)/);
  });

  it("defaults the canonical Relay MCP profile to planner", async () => {
    const source = await readFile(scriptPath, "utf8");

    assert.match(source, /DEFAULT_RELAY_MCP_PROFILE\s*=\s*"planner"/);
    assert.match(
      source,
      /process\.env\.RELAY_MCP_PROFILE\s*=\s*config\.relayMcpProfile/,
    );
  });

  it("redacts the control-plane API key before echoing tunnel output", async () => {
    const source = await readFile(scriptPath, "utf8");

    assert.match(
      source,
      /return text\.split\(controlPlaneApiKey\)\.join\("\[REDACTED\]"\)/,
    );
    assert.match(source, /redactSecrets\(String\(chunk\), controlPlaneApiKey\)/);
  });
});
