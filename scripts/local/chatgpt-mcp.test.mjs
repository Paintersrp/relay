import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:http";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import {
  assertRelayReachable,
  normalizeRelayMcpProfile,
  redactSecrets,
} from "./chatgpt-mcp.mjs";

const execFileAsync = promisify(execFile);
const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, "..", "..");
const chatgptScriptPath = path.join(scriptDirectory, "chatgpt-mcp.mjs");
const npmCommand = process.platform === "win32" ? "npm.cmd" : "npm";

const controlledEnv = {
  ...process.env,
  npm_config_loglevel: "silent",
  TUNNEL_MCP_TRANSPORT: "stdio",
  RELAY_MCP_PROFILE: "planner",
  RELAY_MCP_URL: "http://127.0.0.1:8080/mcp",
  TUNNEL_HEALTH_LISTEN_ADDR: "127.0.0.1:8082",
};

async function expectCommandFailure(args, env, messagePattern) {
  await assert.rejects(
    execFileAsync(process.execPath, [chatgptScriptPath, ...args], {
      cwd: repositoryRoot,
      env,
    }),
    (error) => {
      assert.equal(error.code, 1);
      assert.match(String(error.stderr), messagePattern);
      return true;
    },
  );
}

async function withLoopbackServer(handler, run) {
  const server = createServer(handler);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  assert.ok(address && typeof address === "object");
  try {
    await run(`http://127.0.0.1:${address.port}/mcp`);
  } finally {
    await new Promise((resolveClose, rejectClose) => {
      server.close((error) => {
        if (error) {
          rejectClose(error);
          return;
        }
        resolveClose();
      });
    });
  }
}

test("local MCP help presents the stable package interface", async () => {
  const { stdout, stderr } = await execFileAsync(
    npmCommand,
    ["run", "chatgpt-mcp:help"],
    {
      cwd: repositoryRoot,
      env: controlledEnv,
      shell: process.platform === "win32",
    },
  );
  const output = `${stdout}\n${stderr}`;

  for (const command of [
    "npm run chatgpt-mcp:init",
    "npm run chatgpt-mcp:start",
    "npm run chatgpt-mcp:doctor",
    "npm run chatgpt-mcp:help",
  ]) {
    assert.ok(output.includes(command), command);
  }
  assert.doesNotMatch(output, /node scripts\/local\/chatgpt-mcp\.mjs/u);
});

test("local MCP command line rejects unknown options and commands", async () => {
  await expectCommandFailure(
    ["help", "--unknown"],
    controlledEnv,
    /Unknown option: --unknown/u,
  );
  await expectCommandFailure(
    ["not-a-command"],
    controlledEnv,
    /Unknown command: not-a-command/u,
  );
});

test("local MCP command line rejects an unsupported transport", async () => {
  await expectCommandFailure(
    ["help"],
    {
      ...controlledEnv,
      TUNNEL_MCP_TRANSPORT: "invalid",
    },
    /TUNNEL_MCP_TRANSPORT must be one of: stdio, http/u,
  );
});

test("unsupported Relay MCP profiles fail closed to planner", async () => {
  let diagnostic = "";
  const originalError = console.error;
  console.error = (message) => {
    diagnostic += String(message);
  };
  try {
    assert.equal(normalizeRelayMcpProfile("unsupported"), "planner");
  } finally {
    console.error = originalError;
  }
  assert.match(diagnostic, /defaulting to planner/iu);

  const { stdout, stderr } = await execFileAsync(
    process.execPath,
    [chatgptScriptPath, "help"],
    {
      cwd: repositoryRoot,
      env: {
        ...controlledEnv,
        RELAY_MCP_PROFILE: "unsupported",
      },
    },
  );
  assert.match(stderr, /defaulting to planner/iu);
  assert.match(stdout, /Relay MCP profile: planner/u);
});

test("secret redaction replaces every exact configured secret", () => {
  assert.equal(
    redactSecrets("before secret-value middle secret-value after", "secret-value"),
    "before [REDACTED] middle [REDACTED] after",
  );
  assert.equal(redactSecrets("unchanged", ""), "unchanged");
});

test("HTTP reachability sends a JSON-RPC POST ping", async () => {
  await withLoopbackServer((request, response) => {
    const chunks = [];
    request.setEncoding("utf8");
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      assert.equal(request.method, "POST");
      assert.equal(request.headers["content-type"], "application/json");
      const payload = JSON.parse(chunks.join(""));
      assert.deepEqual(payload, {
        jsonrpc: "2.0",
        id: 1,
        method: "ping",
        params: {},
      });
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }));
    });
  }, async (url) => {
    await assertRelayReachable(url);
  });
});

test("HTTP reachability rejects invalid transport responses", async (t) => {
  const cases = [
    {
      name: "method not allowed",
      status: 405,
      body: "",
      pattern: /returned HTTP 405/u,
    },
    {
      name: "unexpected status",
      status: 503,
      body: "",
      pattern: /failed with HTTP 503/u,
    },
    {
      name: "invalid JSON",
      status: 200,
      body: "not-json",
      pattern: /body was not valid JSON/u,
    },
    {
      name: "missing JSON-RPC result",
      status: 200,
      body: JSON.stringify({ jsonrpc: "2.0", id: 1 }),
      pattern: /ping did not return a JSON-RPC result/u,
    },
  ];

  for (const testCase of cases) {
    await t.test(testCase.name, async () => {
      await withLoopbackServer((_request, response) => {
        response.writeHead(testCase.status, {
          "content-type": "application/json",
        });
        response.end(testCase.body);
      }, async (url) => {
        await assert.rejects(assertRelayReachable(url), testCase.pattern);
      });
    });
  }
});
