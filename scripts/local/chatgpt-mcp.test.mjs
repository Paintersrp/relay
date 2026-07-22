import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { once } from "node:events";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import {
  acquireAggregateLock,
  assertRelayReachable,
  bindingMatches,
  buildNativeRuntimeConnectArgs,
  getConfig,
  loadEnvFile,
  redactSecrets,
  releaseAggregateLock,
  validateAggregateConfig,
} from "./chatgpt-mcp.mjs";

const execFileAsync = promisify(execFile);
const directory = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(directory, "..", "..");
const script = path.join(directory, "chatgpt-mcp.mjs");
const fake = path.join(directory, "test-fixtures", "fake-tunnel-client.mjs");
const npm = process.platform === "win32" ? "npm.cmd" : "npm";
const ids = [
  "tunnel_11111111111111111111111111111111",
  "tunnel_22222222222222222222222222222222",
  "tunnel_33333333333333333333333333333333",
];

const baseEnvironment = {
  ...process.env,
  npm_config_loglevel: "silent",
  CONTROL_PLANE_API_KEY: "sk_test_runtime_key",
  TUNNEL_CLIENT_PATH: process.execPath,
  TUNNEL_CLIENT_ARGS: fake,
  RELAY_MCP_WAYFINDER_TUNNEL_ID: ids[0],
  RELAY_MCP_PLANNER_TUNNEL_ID: ids[1],
  RELAY_MCP_AUDITOR_TUNNEL_ID: ids[2],
};

async function temporaryEnvironment(extra, run) {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-chatgpt-mcp-"));
  const env = {
    ...baseEnvironment,
    FAKE_TUNNEL_STATE: path.join(temp, "native.json"),
    RELAY_MCP_STATE_FILE: path.join(temp, "aggregate.json"),
    RELAY_MCP_STARTUP_TIMEOUT_MS: "100",
    RELAY_MCP_POLL_INTERVAL_MS: "5",
    ...extra,
  };
  try { return await run(env, temp); }
  finally { await rm(temp, { recursive: true, force: true }); }
}

async function runScript(command, env) {
  return execFileAsync(process.execPath, [script, command], { cwd: root, env });
}

function idsFor(values) {
  return {
    RELAY_MCP_WAYFINDER_TUNNEL_ID: values[0],
    RELAY_MCP_PLANNER_TUNNEL_ID: values[1],
    RELAY_MCP_AUDITOR_TUNNEL_ID: values[2],
  };
}

test("aggregate config requires three distinct tunnel_ plus 32 lowercase hexadecimal IDs", () => {
  const valid = getConfig();
  Object.assign(valid.roles[0], { tunnelId: ids[0] });
  Object.assign(valid.roles[1], { tunnelId: ids[1] });
  Object.assign(valid.roles[2], { tunnelId: ids[2] });
  assert.deepEqual(validateAggregateConfig(valid), []);
  for (const invalid of ["tunnel_ABCDEFabcdefABCDEFabcdefABCDEF12", "tunnel_111", "tunnel_111111111111111111111111111111111", "tunnel_1111111111111111111111111111111_", "test-id"]) {
    const config = getConfig();
    config.roles[0].tunnelId = invalid;
    assert.ok(validateAggregateConfig(config).some((error) => error.includes("Wayfinder")));
  }
  const duplicate = getConfig();
  for (const [index, role] of duplicate.roles.entries()) role.tunnelId = ids[0];
  assert.equal(validateAggregateConfig(duplicate).filter((error) => /duplicate tunnel ID/u.test(error)).length, 2);
});

test("aggregate native connect uses JSON and never configures a fixed health address", () => {
  const args = buildNativeRuntimeConnectArgs({}, { alias: "relay-planner", profile: "relay-planner", tunnelId: ids[1], endpoint: "http://127.0.0.1:8080/mcp/planner" });
  assert.ok(args.includes("--json"));
  assert.equal(args.some((arg) => arg.includes("health")), false);
});

test("native fake state generates and consumes three distinct health URLs", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    const result = await runScript("init:all", env);
    assert.match(result.stdout, /init: Auditor succeeded/u);
    const state = JSON.parse(await readFile(path.join(temp, "native.json"), "utf8"));
    const runtimes = Object.values(state.runtimes);
    assert.equal(new Set(runtimes.map((runtime) => runtime.health_url)).size, 3);
    assert.equal(JSON.parse(await readFile(env.RELAY_MCP_STATE_FILE, "utf8")).version, 2);
  });
});

test("malformed native JSON fails clearly and does not write successful aggregate state", async () => {
  await temporaryEnvironment({ FAKE_TUNNEL_MALFORMED: "1" }, async (env) => {
    await assert.rejects(runScript("init:all", env), /malformed JSON|required structured/u);
    await assert.rejects(readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
  });
});

test("native binding is exact and stale aggregate state cannot prove it", () => {
  const role = { alias: "relay-planner", profile: "relay-planner", tunnelId: ids[1], endpoint: "http://127.0.0.1:8080/mcp/planner" };
  const payload = { alias: role.alias, profile: role.profile, tunnel_id: role.tunnelId, mcp_server_url: role.endpoint, process_running: true, health_url: "http://127.0.0.1:23001/readyz" };
  assert.equal(bindingMatches(JSON.stringify(payload), role), true);
  assert.equal(bindingMatches(JSON.stringify({ ...payload, mcp_server_url: "http://wrong/mcp" }), role), false);
  assert.equal(bindingMatches(JSON.stringify({ roles: [role] }), role), false);
});

test("same alias with endpoint drift is stopped and reconnected; correct runtime is reused", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const nativePath = path.join(temp, "native.json");
    const native = JSON.parse(await readFile(nativePath, "utf8"));
    native.runtimes["relay-planner"].mcp_server_url = "http://wrong/mcp";
    await writeFile(nativePath, `${JSON.stringify(native)}\n`, "utf8");
    const logPath = path.join(temp, "commands.log");
    const second = await runScript("init:all", { ...env, FAKE_TUNNEL_LOG: logPath });
    assert.match(second.stdout, /init: Wayfinder succeeded/u);
    const calls = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    assert.ok(calls.some((args) => args.includes("stop") && args.includes("relay-planner")));
    assert.ok(calls.some((args) => args.includes("connect") && args.includes("relay-planner")));
    assert.equal(calls.filter((args) => args.includes("connect") && args.includes("relay-wayfinder")).length, 0);
  });
});

test("changed profile forces replacement", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const nativePath = path.join(temp, "native.json");
    const native = JSON.parse(await readFile(nativePath, "utf8"));
    native.runtimes["relay-auditor"].profile = "old-profile";
    await writeFile(nativePath, `${JSON.stringify(native)}\n`, "utf8");
    const logPath = path.join(temp, "commands.log");
    await runScript("init:all", { ...env, FAKE_TUNNEL_LOG: logPath });
    const calls = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    assert.ok(calls.some((args) => args.includes("stop") && args.includes("relay-auditor")));
    assert.ok(calls.some((args) => args.includes("connect") && args.includes("relay-auditor")));
  });
});

test("not-ready role fails initialization and cleans all changed roles", async () => {
  await temporaryEnvironment({ FAKE_TUNNEL_NOT_READY_ALIAS: "relay-planner" }, async (env, temp) => {
    await assert.rejects(runScript("init:all", env), /did not become ready/u);
    const native = JSON.parse(await readFile(path.join(temp, "native.json"), "utf8"));
    assert.equal(Object.values(native.runtimes).every((runtime) => !runtime.process_running), true);
    await assert.rejects(readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
  });
});

test("start reuses an external Relay and stop preserves it", async () => {
  const server = createServer((request, response) => {
    request.resume();
    request.on("end", () => {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }));
    });
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  try {
    await temporaryEnvironment({ RELAY_MCP_BASE_URL: `http://127.0.0.1:${address.port}` }, async (env) => {
      const started = await runScript("start:all", env);
      assert.match(started.stdout, /reusing healthy external daemon/u);
      const stopped = await runScript("stop:all", env);
      assert.match(stopped.stdout, /external daemon preserved/u);
    });
  } finally { await new Promise((resolvePromise) => server.close(resolvePromise)); }
});

test("environment precedence remains process over .env.local over .env", async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-env-precedence-"));
  try {
    const env = path.join(temp, ".env");
    const local = path.join(temp, ".env.local");
    await writeFile(env, "RELAY_MCP_BASE_URL=http://from-env\n", "utf8");
    await writeFile(local, "RELAY_MCP_BASE_URL=http://from-local\n", "utf8");
    const previous = process.env.RELAY_MCP_BASE_URL;
    process.env.RELAY_MCP_BASE_URL = "http://from-process";
    const keys = new Set(Object.keys(process.env));
    loadEnvFile(env, keys);
    loadEnvFile(local, keys);
    assert.equal(process.env.RELAY_MCP_BASE_URL, "http://from-process");
    if (previous === undefined) delete process.env.RELAY_MCP_BASE_URL;
    else process.env.RELAY_MCP_BASE_URL = previous;
  } finally { await rm(temp, { recursive: true, force: true }); }
});

test("stale aggregate lock is recovered and cancellation-safe release is deterministic", async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-lock-"));
  const lock = path.join(temp, "aggregate.lock");
  try {
    await writeFile(lock, JSON.stringify({ pid: 999999 }), "utf8");
    acquireAggregateLock(lock);
    assert.throws(() => acquireAggregateLock(lock), /already running/u);
    releaseAggregateLock(lock);
    releaseAggregateLock(lock);
  } finally { await rm(temp, { recursive: true, force: true }); }
});

test("stale Relay identity is preserved safely and state is removed", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await writeFile(env.RELAY_MCP_STATE_FILE, `${JSON.stringify({ version: 2, relay: { owned: true, identity: { pid: process.pid, startTime: "wrong", expectedExecutable: "not-this-process.exe", expectedArguments: [], commandFingerprint: "wrong" } } })}\n`, "utf8");
    await assert.rejects(runScript("stop:all", env), (error) => {
      assert.match(String(error.stderr), /preserved PID|ownership/u);
      return true;
    });
    await assert.rejects(readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
  });
});

test("SIGTERM during the first native connection stops work and releases the lock", { skip: process.platform === "win32" }, async () => {
  await temporaryEnvironment({ FAKE_TUNNEL_CONNECT_DELAY_MS: "1000" }, async (env, temp) => {
    const child = spawn(process.execPath, [script, "init:all"], { cwd: root, env, stdio: ["ignore", "pipe", "pipe"] });
    const stderr = [];
    let signalled = false;
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk) => {
      stderr.push(chunk);
      if (!signalled && String(chunk).includes("is not known")) {
        signalled = true;
        child.kill("SIGTERM");
      }
    });
    const [code, signal] = await once(child, "close");
    assert.equal(signalled, true);
    assert.ok(code === 143 || code === 130 || code === 1 || code === null || signal === "SIGTERM");
    await assert.rejects(readFile(path.join(temp, "native.json"), "utf8"));
    await assert.rejects(readFile(`${env.RELAY_MCP_STATE_FILE}.lock`, "utf8"));
    assert.match(stderr.join(""), /cancelled|not known/u);
  });
});

test("SIGINT during Relay readiness prevents runtime connections and stops owned Relay", { skip: process.platform === "win32" }, async () => {
  await temporaryEnvironment({ RELAY_MCP_RELAY_COMMAND: `${process.execPath} -e "setTimeout(() => {}, 5000)"` }, async (env, temp) => {
    const child = spawn(process.execPath, [script, "start:all"], { cwd: root, env, stdio: ["ignore", "pipe", "pipe"] });
    let signalled = false;
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      if (!signalled && String(chunk).includes("Relay: starting")) {
        signalled = true;
        child.kill("SIGINT");
      }
    });
    const [code, signal] = await once(child, "close");
    assert.equal(signalled, true);
    assert.ok(code === 130 || code === 1 || code === null || signal === "SIGINT");
    await assert.rejects(readFile(path.join(temp, "native.json"), "utf8"));
    await assert.rejects(readFile(`${env.RELAY_MCP_STATE_FILE}.lock`, "utf8"));
  });
});

test("secret redaction covers repeated split-output secrets", () => {
  assert.equal(redactSecrets("secret-value secret-value", "secret-value"), "[REDACTED] [REDACTED]");
});

test("package help keeps aggregate and single-profile commands stable", async () => {
  const result = await execFileAsync(npm, ["run", "chatgpt-mcp:help"], { cwd: root, env: baseEnvironment, shell: process.platform === "win32" });
  const output = `${result.stdout}\n${result.stderr}`;
  for (const command of ["chatgpt-mcp:init", "chatgpt-mcp:doctor", "chatgpt-mcp:init:all", "chatgpt-mcp:doctor:all", "chatgpt-mcp:start:all", "chatgpt-mcp:stop:all", "chatgpt-mcp:status:all"]) assert.ok(output.includes(command));
  assert.doesNotMatch(output, /HEALTH_ADDR|1820[123]/u);
});

test("HTTP readiness remains JSON-RPC POST based", async () => {
  const server = createServer((request, response) => {
    request.resume();
    request.on("end", () => {
      assert.equal(request.method, "POST");
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }));
    });
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  try { await assertRelayReachable(`http://127.0.0.1:${address.port}/mcp`); }
  finally { await new Promise((resolvePromise) => server.close(resolvePromise)); }
});
