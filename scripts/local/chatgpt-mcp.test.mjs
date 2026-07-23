import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
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
  captureProcessIdentity,
  currentProcessStartIdentity,
  createRedactedSink,
  terminateProcessTree,
  verifyProcessIdentity,
  loadEnvFile,
  normalizeRuntimeStatus,
  parseLinuxProcStatStartIdentity,
  parseMacPsOutput,
  parseWindowsCimJson,
  redactSecrets,
  sanitizePersistedValue,
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
    assert.equal(JSON.parse(await readFile(env.RELAY_MCP_STATE_FILE, "utf8")).version, 3);
  });
});

test("malformed native JSON fails clearly and does not write successful aggregate state", async () => {
  await temporaryEnvironment({ FAKE_TUNNEL_MALFORMED: "1" }, async (env) => {
    await assert.rejects(runScript("init:all", env), /malformed JSON|required structured/u);
    await assert.rejects(readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
  });
});

test("nonzero health JSON is classified as a valid unhealthy probe", async () => {
  await temporaryEnvironment({ FAKE_TUNNEL_UNHEALTHY_ALIAS: "relay-planner" }, async (env) => {
    await assert.rejects(runScript("init:all", env), /native healthz is unhealthy|did not become ready/u);
  });
});

test("stop PayloadError with stopped false is a failure, while unknown aliases are idempotent", async () => {
  await temporaryEnvironment({}, async (env) => {
    const before = await runScript("stop:all", env);
    assert.equal(before.code, undefined);
    await runScript("init:all", env);
    await assert.rejects(runScript("stop:all", { ...env, FAKE_TUNNEL_STOP_PAYLOAD_ERROR_ALIAS: "relay-planner" }), /cleanup failed:.*relay-planner/u);
    const repeated = await runScript("stop:all", env);
    assert.match(repeated.stdout, /already stopped|stopped or already stopped/u);
  });
});

test("alias-only failed connect state recovers on the next operation", async () => {
  await temporaryEnvironment({ FAKE_TUNNEL_CONNECT_FAIL_ALIAS: "relay-planner" }, async (env, temp) => {
    await assert.rejects(runScript("init:all", env), /connect failed|structured connect/u);
    const failed = JSON.parse(await readFile(path.join(temp, "native.json"), "utf8"));
    assert.equal(failed.runtimes["relay-planner"].process, null);
    await runScript("init:all", { ...env, FAKE_TUNNEL_CONNECT_FAIL_ALIAS: "" });
    const recovered = JSON.parse(await readFile(path.join(temp, "native.json"), "utf8"));
    assert.equal(recovered.runtimes["relay-planner"].process_running, true);
  });
});

test("native binding is exact and stale aggregate state cannot prove it", () => {
  const role = { alias: "relay-planner", profile: "relay-planner", tunnelId: ids[1], endpoint: "http://127.0.0.1:8080/mcp/planner" };
  const payload = { alias: role.alias, profile_name: role.profile, tunnel_id: role.tunnelId, process: { target_kind: "server_url", target_value: role.endpoint }, process_running: true, health_url: "http://127.0.0.1:23001/readyz" };
  assert.equal(bindingMatches(JSON.stringify(payload), role), true);
  assert.equal(bindingMatches(JSON.stringify({ ...payload, process: { ...payload.process, target_value: "http://wrong/mcp" } }), role), false);
  assert.equal(bindingMatches(JSON.stringify({ roles: [role] }), role), false);
  assert.equal(bindingMatches(JSON.stringify({ ...payload, process: undefined, mcp_server_url: role.endpoint }), role), false);
});

test("production 0.0.9 status fixture is normalized from nested process target", async () => {
  const fixture = JSON.parse(await readFile(path.join(directory, "test-fixtures", "tunnel-client-0.0.9-status.json"), "utf8"));
  const runtime = normalizeRuntimeStatus(fixture);
  assert.equal(runtime.endpoint, fixture.process.target_value);
  assert.equal(runtime.profile, fixture.profile_name);
  assert.equal(runtime.processRunning, fixture.process_running);
  assert.equal(normalizeRuntimeStatus({ ...fixture, process: { ...fixture.process, target_value: "https://wrong.example/mcp" } }).endpoint, "https://wrong.example/mcp");
  assert.equal(normalizeRuntimeStatus({ ...fixture, process: { ...fixture.process, target_kind: "command" } }), null);
  assert.equal(normalizeRuntimeStatus({ ...fixture, process: { target_kind: "server_url" } }), null);
  assert.equal(normalizeRuntimeStatus({ ...fixture, mcp_server_url: fixture.process.target_value, process: undefined }), null);
});

test("process identity parsers enforce platform-neutral fields", () => {
  const stat = "42 (relay worker (test)) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23";
  assert.equal(parseLinuxProcStatStartIdentity(stat), "19");
  const mac = parseMacPsOutput("123 Wed Jul 22 12:34:56 2026 /usr/local/bin/relay /usr/local/bin/relay --serve", 123);
  assert.deepEqual(mac, { startIdentity: "Wed Jul 22 12:34:56 2026", executablePath: "/usr/local/bin/relay", commandLine: "/usr/local/bin/relay --serve" });
  const windows = parseWindowsCimJson(JSON.stringify({ ProcessId: 123, CreationDate: "20260722123456.000000-240", ExecutablePath: "C:\\\\relay.exe", CommandLine: "relay.exe --serve" }), 123);
  assert.equal(windows.startIdentity, "20260722123456.000000-240");
  assert.equal(windows.executablePath, "C:\\\\relay.exe");
  const macWithSpaces = parseMacPsOutput({ pid: "123", startIdentity: "Wed Jul 22 12:34:56 2026", executablePath: "/Applications/Relay Worker/bin/relay", commandLine: "/Applications/Relay Worker/bin/relay --config /tmp/Relay Config/config.toml" }, 123);
  assert.equal(macWithSpaces.executablePath, "/Applications/Relay Worker/bin/relay");
  assert.match(macWithSpaces.commandLine, /Relay Config/u);
});

test("macOS lock start identity uses the C locale and normalized representation", () => {
  let invocation;
  const identity = currentProcessStartIdentity(123, "darwin", {
    execFileSync: (...args) => {
      invocation = args;
      return "  Wed   Jul 22 12:34:56 2026\n";
    },
  });
  assert.equal(identity, "Wed Jul 22 12:34:56 2026");
  assert.equal(invocation[0], "ps");
  assert.equal(invocation[2].env.LC_ALL, "C");
  assert.equal(parseMacPsOutput({ pid: "123", startIdentity: identity, executablePath: "/relay", commandLine: "/relay" }, 123).startIdentity, identity);
});

test("active-platform Relay identity captures and verifies a real child", async () => {
  const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], { detached: true, stdio: "ignore" });
  await once(child, "spawn");
  try {
    const identity = await captureProcessIdentity(child.pid, { executable: process.execPath, args: ["-e", "setInterval(() => {}, 1000)"] });
    assert.ok(identity);
    assert.deepEqual(await verifyProcessIdentity(identity), { ok: true, stopped: false });
    assert.equal((await verifyProcessIdentity({ ...identity, startIdentity: "different" })).ok, false);
    assert.equal((await verifyProcessIdentity({ ...identity, executablePath: "not-the-runtime" })).ok, false);
    assert.equal((await verifyProcessIdentity({ ...identity, commandFingerprint: "different", expectedArguments: ["--not-present"] })).ok, false);
  } finally {
    await terminateProcessTree(child.pid, { wait: true });
  }
});

test("same alias with endpoint drift is stopped and reconnected; correct runtime is reused", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const nativePath = path.join(temp, "native.json");
    const native = JSON.parse(await readFile(nativePath, "utf8"));
    native.runtimes["relay-planner"].process.target_value = "http://wrong/mcp";
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
    native.runtimes["relay-auditor"].profile_name = "old-profile";
    await writeFile(nativePath, `${JSON.stringify(native)}\n`, "utf8");
    const logPath = path.join(temp, "commands.log");
    await runScript("init:all", { ...env, FAKE_TUNNEL_LOG: logPath });
    const calls = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    assert.ok(calls.some((args) => args.includes("stop") && args.includes("relay-auditor")));
    assert.ok(calls.some((args) => args.includes("connect") && args.includes("relay-auditor")));
  });
});

test("endpoint and profile drift stop failure is transactional and never reconnects", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const nativePath = path.join(temp, "native.json");
    const native = JSON.parse(await readFile(nativePath, "utf8"));
    native.runtimes["relay-planner"].process.target_value = "http://wrong/mcp";
    native.runtimes["relay-planner"].profile_name = "wrong-profile";
    await writeFile(nativePath, `${JSON.stringify(native)}\n`, "utf8");
    const logPath = path.join(temp, "drift-failure.log");
    await assert.rejects(runScript("init:all", { ...env, FAKE_TUNNEL_FAIL_STOP_ALIAS: "relay-planner", FAKE_TUNNEL_LOG: logPath }), /stop before reconnect failed|cleanup failed/u);
    const calls = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    assert.ok(calls.some((args) => args.includes("stop") && args.includes("relay-planner")));
    assert.equal(calls.some((args) => args.includes("connect") && args.includes("relay-planner")), false);
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

test("cleanup attempts every changed runtime when one stop fails", async () => {
  await temporaryEnvironment({ FAKE_TUNNEL_NOT_READY_ALIAS: "relay-planner", FAKE_TUNNEL_FAIL_STOP_ALIAS: "relay-wayfinder" }, async (env, temp) => {
    await assert.rejects(runScript("init:all", env), (error) => {
      assert.match(String(error.stderr), /cleanup failed:.*runtime relay-wayfinder/u);
      return true;
    });
    const native = JSON.parse(await readFile(path.join(temp, "native.json"), "utf8"));
    assert.equal(native.runtimes["relay-wayfinder"].process_running, true);
    assert.equal(native.runtimes["relay-planner"].process_running, false);
  });
});

test("stop cleanup shuts down owned Relay after malformed runtime stop output", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    const relayScript = path.join(temp, "relay-child.mjs");
    await writeFile(relayScript, "import http from 'node:http'; http.createServer((request, response) => { request.resume(); request.on('end', () => { response.writeHead(200, {'content-type': 'application/json'}); response.end(JSON.stringify({jsonrpc: '2.0', id: 1, result: {}})); }); }).listen(Number(process.env.RELAY_TEST_PORT), '127.0.0.1');\n", "utf8");
    const portProbe = createServer();
    portProbe.listen(0, "127.0.0.1");
    await once(portProbe, "listening");
    const port = portProbe.address().port;
    await new Promise((resolvePromise) => portProbe.close(resolvePromise));
    const startEnv = { ...env, RELAY_TEST_PORT: String(port), RELAY_MCP_BASE_URL: `http://127.0.0.1:${port}`, RELAY_MCP_RELAY_COMMAND: `${process.execPath} ${relayScript}` };
    await runScript("start:all", startEnv);
    const repeated = await runScript("start:all", startEnv);
    assert.match(repeated.stdout, /launcher-owned daemon/u);
    await runScript("init:all", startEnv);
    assert.equal(JSON.parse(await readFile(startEnv.RELAY_MCP_STATE_FILE, "utf8")).relay.owned, true);
    await assert.rejects(runScript("stop:all", { ...startEnv, FAKE_TUNNEL_MALFORMED_STOP_ALIAS: "relay-planner" }), (error) => {
      assert.match(String(error.stderr), /cleanup failed:.*runtime relay-planner/u);
      assert.match(String(error.stdout), /Relay: stopped verified launcher-owned daemon/u);
      return true;
    });
    await assert.rejects(assertRelayReachable(`http://127.0.0.1:${port}/mcp/planner`));
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
      const doctored = await runScript("doctor:all", env);
      assert.match(doctored.stdout, /doctor: Auditor succeeded/u);
      const status = await runScript("status:all", env);
      assert.match(status.stdout, /Auditor.*native\/ready/u);
      assert.doesNotMatch(status.stdout, /"healthz"|"process_running"|"target_kind"/u);
      const stopped = await runScript("stop:all", env);
      assert.match(stopped.stdout, /external daemon preserved/u);
    });
  } finally { await new Promise((resolvePromise) => server.close(resolvePromise)); }
});

test("failed init preserves the prior aggregate state exactly", async () => {
  await temporaryEnvironment({}, async (env) => {
    const server = createServer((request, response) => {
      request.resume();
      request.on("end", () => { response.writeHead(200, { "content-type": "application/json" }); response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} })); });
    });
    server.listen(0, "127.0.0.1");
    await once(server, "listening");
    const address = server.address();
    try {
      const startedEnv = { ...env, RELAY_MCP_BASE_URL: `http://127.0.0.1:${address.port}` };
      await runScript("init:all", startedEnv);
      const statePath = startedEnv.RELAY_MCP_STATE_FILE;
      const before = await readFile(statePath, "utf8");
      await assert.rejects(runScript("init:all", { ...startedEnv, FAKE_TUNNEL_UNHEALTHY_ALIAS: "relay-planner" }), /unhealthy|did not become ready/u);
      assert.equal(await readFile(statePath, "utf8"), before);
    } finally { await new Promise((resolvePromise) => server.close(resolvePromise)); }
  });
});

test("stop:all uses persisted aliases after environment aliases change", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const logPath = path.join(temp, "persisted-stop.log");
    const changed = { ...env, RELAY_MCP_WAYFINDER_ALIAS: "unrelated-wayfinder", RELAY_MCP_PLANNER_ALIAS: "unrelated-planner", RELAY_MCP_AUDITOR_ALIAS: "unrelated-auditor", FAKE_TUNNEL_LOG: logPath };
    await runScript("stop:all", changed);
    const calls = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    for (const alias of ["relay-wayfinder", "relay-planner", "relay-auditor"]) assert.ok(calls.some((args) => args.includes("stop") && args.includes(alias)));
    assert.equal(calls.some((args) => args.includes("stop") && args.includes("unrelated-planner")), false);
  });
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
    await writeFile(lock, JSON.stringify({ pid: 999999, startIdentity: "dead", ownerToken: "stale" }), "utf8");
    acquireAggregateLock(lock, { inspectStartIdentity: () => "self-start" });
    assert.throws(() => acquireAggregateLock(lock, { inspectStartIdentity: () => "self-start" }), /already running/u);
    releaseAggregateLock(lock);
    releaseAggregateLock(lock);
  } finally { await rm(temp, { recursive: true, force: true }); }
});

test("lock release does not remove a replacement owner token", async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-lock-owner-"));
  const lock = path.join(temp, "aggregate.lock");
  try {
    acquireAggregateLock(lock);
    await writeFile(path.join(lock, "owner.json"), JSON.stringify({ version: 1, pid: process.pid, startIdentity: "replacement-start", ownerToken: "replacement" }), "utf8");
    releaseAggregateLock(lock);
    assert.match(await readFile(path.join(lock, "owner.json"), "utf8"), /replacement/u);
  } finally { await rm(temp, { recursive: true, force: true }); }
});

test("aggregate lock publication is fail-closed for incomplete or unreadable metadata", async () => {
  const cases = [
    ["empty", ""],
    ["partial JSON", "{\"version\":1,\"pid\":"],
    ["malformed JSON", "not-json"],
  ];
  for (const [label, contents] of cases) {
    const temp = await mkdtemp(path.join(os.tmpdir(), `relay-lock-${label.replace(/ /gu, "-")}-`));
    const lock = path.join(temp, "aggregate.lock");
    try {
      await mkdir(lock);
      await writeFile(path.join(lock, "owner.json"), contents, "utf8");
      assert.throws(
        () => acquireAggregateLock(lock, { inspectStartIdentity: () => "self-start" }),
        /ownership metadata could not be verified.*automatic removal was refused/u,
      );
      assert.equal((await readFile(path.join(lock, "owner.json"), "utf8")), contents);
    } finally { await rm(temp, { recursive: true, force: true }); }
  }

  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-lock-unreadable-"));
  const lock = path.join(temp, "aggregate.lock");
  try {
    await mkdir(lock);
    await mkdir(path.join(lock, "owner.json"));
    assert.throws(
      () => acquireAggregateLock(lock, { inspectStartIdentity: () => "self-start" }),
      /ownership metadata could not be verified.*automatic removal was refused/u,
    );
    await assert.rejects(readFile(lock));
  } finally { await rm(temp, { recursive: true, force: true }); }
});

test("atomic aggregate lock contention permits one owner and later reacquisition", async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-lock-contention-"));
  const lock = path.join(temp, "aggregate.lock");
  const options = { inspectStartIdentity: () => "same-process-start" };
  try {
    let acquired = 0;
    acquireAggregateLock(lock, options);
    acquired += 1;
    assert.throws(() => acquireAggregateLock(lock, options), /already running|contested/u);
    assert.equal(acquired, 1);
    releaseAggregateLock(lock);
    acquireAggregateLock(lock, options);
    acquired += 1;
    assert.equal(acquired, 2);
    releaseAggregateLock(lock);
  } finally { await rm(temp, { recursive: true, force: true }); }
});

test("aggregate stale recovery verifies identity and cannot remove a third contender", async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-lock-stale-"));
  const lock = path.join(temp, "aggregate.lock");
  const options = { inspectStartIdentity: (pid) => (pid === 424242 ? "new-start" : "self-start") };
  try {
    await writeFile(lock, JSON.stringify({ pid: 424242, startIdentity: "old-start", ownerToken: "reused-pid" }), "utf8");
    acquireAggregateLock(lock, options);
    releaseAggregateLock(lock);

    await writeFile(lock, JSON.stringify({ pid: 424242, startIdentity: "old-start", ownerToken: "stale-again" }), "utf8");
    assert.throws(() => acquireAggregateLock(lock, { ...options, beforeStaleRemoval: () => {
      rmSync(lock, { recursive: true, force: true });
      mkdirSync(lock);
      writeFileSync(path.join(lock, "owner.json"), JSON.stringify({ version: 1, pid: 525252, startIdentity: "third-start", ownerToken: "third-owner" }));
    } }), /changed during stale-owner recovery/u);
    assert.match(await readFile(path.join(lock, "owner.json"), "utf8"), /third-owner/u);
  } finally { await rm(temp, { recursive: true, force: true }); }
});

test("aggregate lock preserves live and ambiguous owners and protects changed instances", async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-lock-integrity-"));
  const lock = path.join(temp, "aggregate.lock");
  const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], { detached: true, stdio: "ignore" });
  await once(child, "spawn");
  try {
    await mkdir(lock);
    await writeFile(path.join(lock, "owner.json"), JSON.stringify({ version: 1, pid: child.pid, startIdentity: "live-start", ownerToken: "live-owner" }), "utf8");
    assert.throws(() => acquireAggregateLock(lock, { inspectStartIdentity: (pid) => (pid === child.pid ? "live-start" : "self-start") }), /already running/u);
    assert.match(await readFile(path.join(lock, "owner.json"), "utf8"), /live-owner/u);

    await writeFile(path.join(lock, "owner.json"), JSON.stringify({ version: 1, pid: child.pid, ownerToken: "missing-start" }), "utf8");
    assert.throws(() => acquireAggregateLock(lock, { inspectStartIdentity: () => "self-start" }), /ownership metadata could not be verified/u);
    assert.match(await readFile(path.join(lock, "owner.json"), "utf8"), /missing-start/u);

    await writeFile(path.join(lock, "owner.json"), JSON.stringify({ version: 1, pid: child.pid, startIdentity: "live-start", ownerToken: "unavailable-start" }), "utf8");
    assert.throws(() => acquireAggregateLock(lock, { inspectStartIdentity: (pid) => (pid === child.pid ? null : "self-start") }), /ownership metadata could not be verified/u);

    acquireAggregateLock(lock, { inspectStartIdentity: (pid) => (pid === child.pid ? "different-start" : "self-start") });
    const owner = JSON.parse(await readFile(path.join(lock, "owner.json"), "utf8"));
    rmSync(lock, { recursive: true, force: true });
    mkdirSync(lock);
    writeFileSync(path.join(lock, "owner.json"), JSON.stringify({ version: 1, pid: process.pid, startIdentity: "replacement-start", ownerToken: "replacement-owner" }));
    releaseAggregateLock(lock, owner.ownerToken);
    assert.match(await readFile(path.join(lock, "owner.json"), "utf8"), /replacement-owner/u);
  } finally {
    await terminateProcessTree(child.pid, { wait: true });
    await rm(temp, { recursive: true, force: true });
  }
});

test("termination result reports an injected failure without hiding the live PID", async () => {
  const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], { detached: true, stdio: "ignore" });
  await once(child, "spawn");
  try {
    const result = await terminateProcessTree(child.pid, { wait: true, terminationAdapter: async () => ({ ok: false, exited: false, reason: "injected failure" }) });
    assert.deepEqual(result, { ok: false, pid: child.pid, signalAttempted: null, escalated: false, exited: false, reason: "injected failure" });
  } finally { await terminateProcessTree(child.pid, { wait: true }); }
});

test("malformed aggregate ownership state fails closed and is preserved", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await writeFile(env.RELAY_MCP_STATE_FILE, `${JSON.stringify({ version: 3, residualBindings: [], relay: { owned: true, identity: { pid: process.pid, startTime: "wrong", expectedExecutable: "not-this-process.exe", expectedArguments: [], commandFingerprint: "wrong" } } })}\n`, "utf8");
    await assert.rejects(runScript("stop:all", env), /malformed aggregate state/u);
    assert.match(await readFile(env.RELAY_MCP_STATE_FILE, "utf8"), /desiredRoleBindings|relay/u);
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

test("streaming redaction preserves only viable prefixes across arbitrary chunks", () => {
  const secret = "secret-value";
  for (let split = 0; split <= secret.length; split += 1) {
    const sink = createRedactedSink(secret, () => {});
    sink.push(`before ${secret.slice(0, split)}`);
    sink.push(`${secret.slice(split)} after`);
    assert.equal(sink.finish(), "before [REDACTED] after");
  }
  const sink = createRedactedSink(secret, () => {});
  for (const chunk of ["secret-", "valuesecret", "-value", " secre"]) sink.push(chunk);
  assert.equal(sink.finish(), "[REDACTED][REDACTED] secre");
});

test("persisted identity sanitizer recursively redacts without corrupting fields", () => {
  const value = sanitizePersistedValue({ commandLine: "relay --key secret-value", expectedArguments: ["secret-value", { nested: "xsecret-valuey" }], pid: 42 }, "secret-value");
  assert.deepEqual(value, { commandLine: "relay --key [REDACTED]", expectedArguments: ["[REDACTED]", { nested: "x[REDACTED]y" }], pid: 42 });
});

test("failed later role after alias migration cleans up the replacement alias, not the retired one", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const logPath = path.join(temp, "migration-cleanup.log");
    await assert.rejects(
      runScript("init:all", { ...env, RELAY_MCP_PLANNER_ALIAS: "relay-planner-v2", FAKE_TUNNEL_NOT_READY_ALIAS: "relay-auditor", FAKE_TUNNEL_LOG: logPath }),
      /did not become ready/u,
    );
    const calls = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    const connect = calls.findIndex((args) => args.includes("connect") && args.includes("relay-planner-v2"));
    const cleanupStop = calls.findIndex((args, index) => index > connect && args.includes("stop") && args.includes("relay-planner-v2"));
    assert.ok(connect >= 0 && cleanupStop > connect);
    assert.equal(calls.filter((args) => args.includes("stop") && args.includes("relay-planner")).length, 1);
    const native = JSON.parse(await readFile(path.join(temp, "native.json"), "utf8"));
    assert.equal(native.runtimes["relay-planner-v2"].process_running, false);
  });
});

test("failed owned Relay shutdown leaves readable residual state and stop:all retries it", async () => {
  await temporaryEnvironment({}, async (env) => {
    await runScript("init:all", env);
    const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], { detached: true, stdio: "ignore" });
    await once(child, "spawn");
    try {
      const state = JSON.parse(await readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
      state.relay = { owned: true, identity: { pid: child.pid, startTime: "wrong", expectedExecutable: "not-this-process.exe", expectedArguments: [], commandFingerprint: "wrong" } };
      await writeFile(env.RELAY_MCP_STATE_FILE, `${JSON.stringify(state)}\n`, "utf8");
      await assert.rejects(runScript("stop:all", env), (error) => {
        assert.match(String(error.stderr), /cleanup failed:.*Relay/u);
        return true;
      });
      const residual = JSON.parse(await readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
      assert.equal(residual.version, 3);
      assert.deepEqual(residual.desiredRoleBindings, []);
      assert.deepEqual(residual.residualBindings, []);
      assert.equal(residual.relay.owned, true);
    } finally {
      await terminateProcessTree(child.pid, { wait: true });
    }
    await runScript("stop:all", env);
    await assert.rejects(readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
  });
});

test("stop:all fails closed on malformed or tampered residual bindings", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const state = JSON.parse(await readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
    const logPath = path.join(temp, "residual-validation.log");
    const invalidResiduals = [
      [{ alias: "arbitrary-alias" }],
      [{ key: "intruder", tunnelId: ids[0], alias: "arbitrary-alias", profile: "profile", endpoint: "http://127.0.0.1:8080/mcp/planner" }],
      [{ key: "planner", tunnelId: ids[1], alias: "alias", profile: "profile", endpoint: "not-a-url" }],
      [
        { key: "planner", tunnelId: ids[1], alias: "same-alias", profile: "profile-a", endpoint: "http://127.0.0.1:8080/mcp/planner" },
        { key: "auditor", tunnelId: ids[2], alias: "same-alias", profile: "profile-b", endpoint: "http://127.0.0.1:8080/mcp/auditor" },
      ],
      [{ key: "planner", tunnelId: ids[1], alias: "alias", profile: "sk_leaked_credential_0001", endpoint: "http://127.0.0.1:8080/mcp/planner" }],
      [{ key: "planner", tunnelId: ids[1], alias: "a".repeat(1025), profile: "profile", endpoint: "http://127.0.0.1:8080/mcp/planner" }],
    ];
    for (const residualBindings of invalidResiduals) {
      await writeFile(env.RELAY_MCP_STATE_FILE, `${JSON.stringify({ ...state, residualBindings })}\n`, "utf8");
      await assert.rejects(runScript("stop:all", { ...env, FAKE_TUNNEL_LOG: logPath }), (error) => {
        assert.match(String(error.stderr), /malformed aggregate state/u);
        return true;
      });
    }
    await assert.rejects(readFile(logPath, "utf8"));
  });
});

test("version 2 aggregate state migrates to version 3 and remains recoverable by stop:all", async () => {
  await temporaryEnvironment({}, async (env) => {
    await runScript("init:all", env);
    const v3 = JSON.parse(await readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
    const v2 = { version: 2, updatedAt: v3.updatedAt, desiredRoleBindings: v3.desiredRoleBindings, runtimesChangedByOperation: [], relay: { owned: false } };
    await writeFile(env.RELAY_MCP_STATE_FILE, `${JSON.stringify(v2)}\n`, "utf8");
    const migrated = await runScript("init:all", env);
    assert.match(migrated.stdout, /migrated version 2 aggregate state to version 3/u);
    assert.equal(JSON.parse(await readFile(env.RELAY_MCP_STATE_FILE, "utf8")).version, 3);
    await writeFile(env.RELAY_MCP_STATE_FILE, `${JSON.stringify(v2)}\n`, "utf8");
    await runScript("stop:all", env);
    await assert.rejects(readFile(env.RELAY_MCP_STATE_FILE, "utf8"));
  });
});

test("aggregate lock fails closed when process identity cannot be established", async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "relay-lock-identity-"));
  const lock = path.join(temp, "aggregate.lock");
  const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], { detached: true, stdio: "ignore" });
  await once(child, "spawn");
  try {
    assert.throws(() => acquireAggregateLock(lock, { inspectStartIdentity: () => null }), /duplicate-operation protection/u);
    await assert.rejects(readFile(lock, "utf8"));
    await writeFile(lock, JSON.stringify({ pid: child.pid, ownerToken: "held" }), "utf8");
    assert.throws(() => acquireAggregateLock(lock, { inspectStartIdentity: () => "self-start" }), /ownership metadata could not be verified/u);
    assert.match(await readFile(lock, "utf8"), /held/u);
    await writeFile(lock, JSON.stringify({ pid: child.pid, ownerToken: "held", startIdentity: "owner-start" }), "utf8");
    assert.throws(
      () => acquireAggregateLock(lock, { inspectStartIdentity: (pid) => (pid === child.pid ? null : "self-start") }),
      /ownership metadata could not be verified/u,
    );
    assert.match(await readFile(lock, "utf8"), /owner-start/u);
    acquireAggregateLock(lock, { inspectStartIdentity: (pid) => (pid === child.pid ? "different-start" : "self-start") });
    releaseAggregateLock(lock);
  } finally {
    await terminateProcessTree(child.pid, { wait: true });
    await rm(temp, { recursive: true, force: true });
  }
});

test("streaming redaction handles self-overlapping secrets across all chunk boundaries", () => {
  for (const secret of ["aba", "abab", "aaaa"]) {
    const text = `x${secret}${secret}y${secret.slice(0, secret.length - 1)}`;
    for (let split = 0; split <= text.length; split += 1) {
      const sink = createRedactedSink(secret, () => {});
      sink.push(text.slice(0, split));
      sink.push(text.slice(split));
      assert.equal(sink.finish(), redactSecrets(text, secret));
    }
    const single = createRedactedSink(secret, () => {});
    single.push(secret);
    assert.equal(single.finish(), "[REDACTED]");
    const charwise = createRedactedSink(secret, () => {});
    for (const character of text) charwise.push(character);
    assert.equal(charwise.finish(), redactSecrets(text, secret));
  }
});

test("alias migration stops the persisted alias before connecting its replacement", async () => {
  await temporaryEnvironment({}, async (env, temp) => {
    await runScript("init:all", env);
    const logPath = path.join(temp, "migration.log");
    await runScript("init:all", { ...env, RELAY_MCP_PLANNER_ALIAS: "relay-planner-v2", FAKE_TUNNEL_LOG: logPath });
    const calls = (await readFile(logPath, "utf8")).trim().split(/\r?\n/u).map((line) => JSON.parse(line));
    const stop = calls.findIndex((args) => args.includes("stop") && args.includes("relay-planner"));
    const connect = calls.findIndex((args) => args.includes("connect") && args.includes("relay-planner-v2"));
    assert.ok(stop >= 0 && connect > stop);
  });
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
