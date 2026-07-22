import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { once } from "node:events";
import { createServer } from "node:http";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import {
  assertRelayReachable,
  acquireAggregateLock,
  buildNativeRuntimeConnectArgs,
  buildProcessShutdownPlan,
  getConfig,
  loadEnvFile,
  normalizeRelayMcpProfile,
  redactSecrets,
  releaseAggregateLock,
  ROLE_DEFINITIONS,
  validateAggregateConfig,
} from "./chatgpt-mcp.mjs";

const execFileAsync = promisify(execFile);
const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, "..", "..");
const chatgptScriptPath = path.join(scriptDirectory, "chatgpt-mcp.mjs");
const npmCommand = process.platform === "win32" ? "npm.cmd" : "npm";
const fakeTunnelClientPath = path.join(scriptDirectory, "test-fixtures", "fake-tunnel-client.mjs");
const fakeRelayServerPath = path.join(scriptDirectory, "test-fixtures", "fake-relay-server.mjs");

async function withEnvironment(values, run) {
  const previous = new Map();
  for (const [key, value] of Object.entries(values)) {
    previous.set(key, process.env[key]);
    if (value === undefined) delete process.env[key];
    else process.env[key] = value;
  }
  try {
    return await run();
  } finally {
    for (const [key, value] of previous) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}

function aggregateEnvironment(extra = {}) {
  const ids = {
    RELAY_MCP_WAYFINDER_TUNNEL_ID: "tunnel_wayfinder_test",
    RELAY_MCP_PLANNER_TUNNEL_ID: "tunnel_planner_test",
    RELAY_MCP_AUDITOR_TUNNEL_ID: "tunnel_auditor_test",
  };
  return {
    ...controlledEnv,
    CONTROL_PLANE_API_KEY: "sk_test_runtime_key",
    TUNNEL_CLIENT_PATH: process.execPath,
    TUNNEL_CLIENT_ARGS: fakeTunnelClientPath,
    RELAY_MCP_STATE_FILE: path.join(os.tmpdir(), `relay-mcp-test-${process.pid}.json`),
    ...ids,
    ...extra,
  };
}

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

test("aggregate configuration derives exactly three distinct role bindings", async () => {
  await withEnvironment({
    RELAY_MCP_BASE_URL: "http://127.0.0.1:9911",
    RELAY_MCP_WAYFINDER_TUNNEL_ID: "tunnel_wayfinder",
    RELAY_MCP_PLANNER_TUNNEL_ID: "tunnel_planner",
    RELAY_MCP_AUDITOR_TUNNEL_ID: "tunnel_auditor",
  }, async () => {
    const config = getConfig();
    assert.equal(config.roles.length, 3);
    assert.deepEqual(config.roles.map((role) => role.endpoint), [
      "http://127.0.0.1:9911/mcp/wayfinder",
      "http://127.0.0.1:9911/mcp/planner",
      "http://127.0.0.1:9911/mcp/auditor",
    ]);
    assert.deepEqual(config.roles.map((role) => role.alias), [
      "relay-wayfinder",
      "relay-planner",
      "relay-auditor",
    ]);
    assert.deepEqual(config.roles.map((role) => role.profile), [
      "relay-wayfinder",
      "relay-planner",
      "relay-auditor",
    ]);
    assert.deepEqual(validateAggregateConfig(config), []);
    assert.deepEqual(ROLE_DEFINITIONS.map((role) => role.key), ["wayfinder", "planner", "auditor"]);
  });
});

test("aggregate configuration rejects missing and duplicate tunnel IDs", async () => {
  await withEnvironment({
    RELAY_MCP_WAYFINDER_TUNNEL_ID: "tunnel_same",
    RELAY_MCP_PLANNER_TUNNEL_ID: "tunnel_same",
    RELAY_MCP_AUDITOR_TUNNEL_ID: undefined,
  }, async () => {
    const errors = validateAggregateConfig(getConfig());
    assert.ok(errors.some((error) => /duplicate tunnel ID/u.test(error)));
    assert.ok(errors.some((error) => /AUDITOR_TUNNEL_ID.*missing/u.test(error)));
  });
});

test("process environment values override local configuration values", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "relay-env-") );
  const envFile = path.join(directory, ".env");
  await writeFile(envFile, "RELAY_MCP_BASE_URL=http://from-file\nRELAY_MCP_WAYFINDER_ALIAS=file-alias\n", "utf8");
  await withEnvironment({
    RELAY_MCP_BASE_URL: "http://from-process",
    RELAY_MCP_WAYFINDER_ALIAS: "process-alias",
  }, async () => {
    const originalKeys = new Set(Object.keys(process.env));
    loadEnvFile(envFile, originalKeys);
    assert.equal(process.env.RELAY_MCP_BASE_URL, "http://from-process");
    assert.equal(process.env.RELAY_MCP_WAYFINDER_ALIAS, "process-alias");
  });
  await rm(directory, { recursive: true, force: true });
  assert.equal(envFile.endsWith(".env"), true);
});

test("native connect commands bind each tunnel ID to its exact role URL", async () => {
  const config = {
    tunnelClientProfileDir: "C:/profiles",
    tunnelClientPath: "C:/bin/tunnel-client.exe",
  };
  const args = buildNativeRuntimeConnectArgs(config, {
    alias: "relay-planner",
    profile: "relay-planner",
    tunnelId: "tunnel_planner",
    endpoint: "http://127.0.0.1:8080/mcp/planner",
  });
  assert.deepEqual(args, [
    "runtimes", "connect", "--alias", "relay-planner", "--profile", "relay-planner",
    "--tunnel-id", "tunnel_planner", "--mcp-server-url", "http://127.0.0.1:8080/mcp/planner",
    "--runtime-api-key", "env:CONTROL_PLANE_API_KEY", "--profile-dir", "C:/profiles",
    "--tunnel-client-bin", "C:/bin/tunnel-client.exe",
  ]);
  assert.equal(args.includes("sk_test_runtime_key"), false);
});

test("aggregate init uses a fake executable for all roles and redacts keys", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "relay-aggregate-") );
  const logPath = path.join(directory, "tunnel.log");
  const statePath = path.join(directory, "state.json");
  const { stdout, stderr } = await execFileAsync(process.execPath, [chatgptScriptPath, "init:all"], {
    cwd: repositoryRoot,
    env: aggregateEnvironment({ FAKE_TUNNEL_LOG: logPath, RELAY_MCP_STATE_FILE: statePath }),
  });
  const lines = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
  assert.equal(lines.length, 3);
  assert.deepEqual(lines.map((args) => args[args.indexOf("--alias") + 1]), ["relay-wayfinder", "relay-planner", "relay-auditor"]);
  assert.match(stdout, /init: Wayfinder succeeded/u);
  assert.match(stdout, /init: Auditor succeeded/u);
  assert.doesNotMatch(`${stdout}\n${stderr}`, /sk_test_runtime_key/u);
  await rm(directory, { recursive: true, force: true });
});

test("aggregate init cleans successful native runtimes after partial failure", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "relay-partial-") );
  const logPath = path.join(directory, "tunnel.log");
  await assert.rejects(execFileAsync(process.execPath, [chatgptScriptPath, "init:all"], {
    cwd: repositoryRoot,
    env: aggregateEnvironment({
      FAKE_TUNNEL_LOG: logPath,
      FAKE_TUNNEL_FAIL_ON: "relay-planner",
      RELAY_MCP_STATE_FILE: path.join(directory, "state.json"),
    }),
  }));
  const lines = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
  assert.ok(lines.some((args) => args.includes("stop") && args.includes("relay-wayfinder")));
  await rm(directory, { recursive: true, force: true });
});

test("aggregate start reuses an externally healthy Relay and stop preserves it", async () => {
  await withLoopbackServer((request, response) => {
    if (request.method !== "POST") { response.writeHead(405); response.end(); return; }
    request.resume();
    request.on("end", () => {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }));
    });
  }, async (url) => {
    const baseUrl = url.replace(/\/mcp$/u, "");
    const directory = await mkdtemp(path.join(os.tmpdir(), "relay-reuse-") );
    const logPath = path.join(directory, "tunnel.log");
    const statusOutput = JSON.stringify({ bindings: [
      { alias: "relay-wayfinder", profile: "relay-wayfinder", tunnel_id: "tunnel_wayfinder_test", mcp_server_url: `${baseUrl}/mcp/wayfinder` },
      { alias: "relay-planner", profile: "relay-planner", tunnel_id: "tunnel_planner_test", mcp_server_url: `${baseUrl}/mcp/planner` },
      { alias: "relay-auditor", profile: "relay-auditor", tunnel_id: "tunnel_auditor_test", mcp_server_url: `${baseUrl}/mcp/auditor` },
    ] });
    const env = aggregateEnvironment({
      RELAY_MCP_BASE_URL: baseUrl,
      FAKE_TUNNEL_LOG: logPath,
      FAKE_TUNNEL_STATUS_OUTPUT: statusOutput,
      RELAY_MCP_STATE_FILE: path.join(directory, "state.json"),
    });
    const started = await execFileAsync(process.execPath, [chatgptScriptPath, "start:all"], { cwd: repositoryRoot, env });
    assert.match(started.stdout, /reusing healthy external daemon/u);
    const stopped = await execFileAsync(process.execPath, [chatgptScriptPath, "stop:all"], { cwd: repositoryRoot, env });
    assert.match(stopped.stdout, /external daemon preserved/u);
    await rm(directory, { recursive: true, force: true });
  });
});

test("aggregate start rejects partial Relay health before connecting runtimes", async () => {
  await withLoopbackServer((request, response) => {
    request.resume();
    request.on("end", () => {
      response.writeHead(request.url === "/mcp/wayfinder" ? 200 : 503, { "content-type": "application/json" });
      response.end(request.url === "/mcp/wayfinder" ? JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }) : "");
    });
  }, async (url) => {
    const directory = await mkdtemp(path.join(os.tmpdir(), "relay-partial-health-"));
    const logPath = path.join(directory, "tunnel.log");
    await assert.rejects(execFileAsync(process.execPath, [chatgptScriptPath, "start:all"], {
      cwd: repositoryRoot,
      env: aggregateEnvironment({
        RELAY_MCP_BASE_URL: url.replace(/\/mcp$/u, ""),
        FAKE_TUNNEL_LOG: logPath,
        RELAY_MCP_STATE_FILE: path.join(directory, "state.json"),
      }),
    }), /partially healthy/u);
    await assert.rejects(readFile(logPath, "utf8"));
    await rm(directory, { recursive: true, force: true });
  });
});

test("aggregate start reports bounded runtime readiness timeout and cleans up", async () => {
  await withLoopbackServer((request, response) => {
    request.resume();
    request.on("end", () => {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }));
    });
  }, async (url) => {
    const directory = await mkdtemp(path.join(os.tmpdir(), "relay-readiness-timeout-"));
    const logPath = path.join(directory, "tunnel.log");
    await assert.rejects(execFileAsync(process.execPath, [chatgptScriptPath, "start:all"], {
      cwd: repositoryRoot,
      env: aggregateEnvironment({
        RELAY_MCP_BASE_URL: url.replace(/\/mcp$/u, ""),
        FAKE_TUNNEL_LOG: logPath,
        FAKE_TUNNEL_STATUS_OUTPUT: "{}",
        RELAY_MCP_STARTUP_TIMEOUT_MS: "40",
        RELAY_MCP_POLL_INTERVAL_MS: "5",
        RELAY_MCP_STATE_FILE: path.join(directory, "state.json"),
      }),
    }), /did not become ready within 40ms/u);
    const lines = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    assert.equal(lines.filter((args) => args.includes("stop")).length, 3);
    await rm(directory, { recursive: true, force: true });
  });
});

test("aggregate start owns and later stops a Relay it launched", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "relay-owned-"));
  const port = 19_000 + (process.pid % 500);
  const baseUrl = `http://127.0.0.1:${port}`;
  const logPath = path.join(directory, "tunnel.log");
  const statusOutput = JSON.stringify({ bindings: [
    { alias: "relay-wayfinder", profile: "relay-wayfinder", tunnel_id: "tunnel_wayfinder_test", mcp_server_url: `${baseUrl}/mcp/wayfinder` },
    { alias: "relay-planner", profile: "relay-planner", tunnel_id: "tunnel_planner_test", mcp_server_url: `${baseUrl}/mcp/planner` },
    { alias: "relay-auditor", profile: "relay-auditor", tunnel_id: "tunnel_auditor_test", mcp_server_url: `${baseUrl}/mcp/auditor` },
  ] });
  const relayCommand = `${process.execPath.includes(" ") ? `"${process.execPath}"` : process.execPath} ${fakeRelayServerPath}`;
  const env = aggregateEnvironment({
    RELAY_MCP_BASE_URL: baseUrl,
    RELAY_MCP_RELAY_COMMAND: relayCommand,
    FAKE_RELAY_PORT: String(port),
    FAKE_TUNNEL_LOG: logPath,
    FAKE_TUNNEL_STATUS_OUTPUT: statusOutput,
    RELAY_MCP_STARTUP_TIMEOUT_MS: "2000",
    RELAY_MCP_POLL_INTERVAL_MS: "10",
    RELAY_MCP_STATE_FILE: path.join(directory, "state.json"),
  });
  try {
    const started = await execFileAsync(process.execPath, [chatgptScriptPath, "start:all"], { cwd: repositoryRoot, env });
    assert.match(started.stdout, /aggregate startup complete/u);
    const stopped = await execFileAsync(process.execPath, [chatgptScriptPath, "stop:all"], { cwd: repositoryRoot, env });
    assert.match(stopped.stdout, /stopped launcher-owned daemon/u);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("aggregate lock rejects a live duplicate and shutdown adapters are platform-specific", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "relay-lock-") );
  const lockPath = path.join(directory, "aggregate.lock");
  acquireAggregateLock(lockPath);
  assert.throws(() => acquireAggregateLock(lockPath), /already running/u);
  releaseAggregateLock(lockPath);
  assert.deepEqual(buildProcessShutdownPlan(42, "win32"), { command: "taskkill", args: ["/PID", "42", "/T", "/F"] });
  assert.deepEqual(buildProcessShutdownPlan(42, "linux"), { command: null, args: [-42, "SIGTERM"] });
  await rm(directory, { recursive: true, force: true });
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
