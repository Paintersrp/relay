#!/usr/bin/env node

import { existsSync, readFileSync, writeFileSync, appendFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";

const args = process.argv.slice(2);
const statePath = process.env.FAKE_TUNNEL_STATE || resolve(process.cwd(), ".fake-tunnel-state.json");
const logPath = process.env.FAKE_TUNNEL_LOG;

function log() {
  if (logPath) {
    mkdirSync(dirname(logPath), { recursive: true });
    appendFileSync(logPath, `${JSON.stringify(args)}\n`);
  }
}

function loadState() {
  if (!existsSync(statePath)) return { runtimes: {}, calls: {} };
  try { return JSON.parse(readFileSync(statePath, "utf8")); }
  catch { return { runtimes: {}, calls: {} }; }
}

function saveState(state) {
  mkdirSync(dirname(statePath), { recursive: true });
  writeFileSync(statePath, `${JSON.stringify(state, null, 2)}\n`, "utf8");
}

function value(flag) {
  const index = args.indexOf(flag);
  return index < 0 ? null : args[index + 1];
}

function failIfRequested(alias) {
  const requested = process.env.FAKE_TUNNEL_FAIL_ON;
  if (requested && (args.includes(requested) || requested === alias)) {
    console.error("fake tunnel-client failure");
    process.exitCode = 7;
    return true;
  }
  return false;
}

function emit(payload) {
  if (process.env.FAKE_TUNNEL_MALFORMED) {
    process.stdout.write("not-json\n");
    return;
  }
  process.stdout.write(`${JSON.stringify(payload)}\n`);
}

function stopFailure(alias) {
  return process.env.FAKE_TUNNEL_FAIL_STOP_ALIAS === alias || process.env.FAKE_TUNNEL_MALFORMED_STOP_ALIAS === alias;
}

function runtimeHealth(runtime) {
  const notReady = process.env.FAKE_TUNNEL_NOT_READY_ALIAS === runtime.alias;
  const failHealth = process.env.FAKE_TUNNEL_UNHEALTHY_ALIAS === runtime.alias;
  const callCount = runtime.healthCalls || 0;
  const readyAfter = Number(process.env.FAKE_TUNNEL_READY_AFTER || 0);
  const ready = !notReady && (!readyAfter || callCount >= readyAfter);
  return {
    locator: runtime.health_url_file
      ? { kind: "file", path: runtime.health_url_file }
      : { kind: "url", url: runtime.health_url },
    base_url: runtime.health_url?.replace(/\/(?:healthz|readyz)$/u, "") || null,
    healthz: { url: runtime.health_url, ok: runtime.process_running && !failHealth },
    readyz: { url: runtime.health_url, ok: runtime.process_running && !failHealth && ready },
    result: runtime.process_running && !failHealth && ready ? "ok" : "fail",
  };
}

async function main() {
  log();
  const state = loadState();
  const command = args.slice(0, 2).join(" ");
  const alias = value("--alias") || args[2] || null;
  if (Number(process.env.FAKE_TUNNEL_DELAY_MS) > 0) await new Promise((resolvePromise) => setTimeout(resolvePromise, Number(process.env.FAKE_TUNNEL_DELAY_MS)));
  if (failIfRequested(alias)) return;

  if (command === "runtimes connect") {
    if (Number(process.env.FAKE_TUNNEL_CONNECT_DELAY_MS) > 0) await new Promise((resolvePromise) => setTimeout(resolvePromise, Number(process.env.FAKE_TUNNEL_CONNECT_DELAY_MS)));
    const healthPort = 20_000 + Object.keys(state.runtimes).length + 1;
    const healthUrl = `http://127.0.0.1:${healthPort}/readyz`;
    const runtime = {
      alias,
      profile_name: value("--profile"),
      tunnel_id: value("--tunnel-id"),
      health_url: healthUrl,
      health_url_file: process.env.FAKE_TUNNEL_HEALTH_URL_FILE ? `${statePath}.${alias}.health-url` : null,
      process_running: true,
      process: {
        target_kind: "server_url",
        target_value: value("--mcp-server-url"),
        pid: process.pid,
      },
      healthCalls: 0,
    };
    if (runtime.health_url_file) writeFileSync(runtime.health_url_file, `${healthUrl}\n`, "utf8");
    state.runtimes[alias] = runtime;
    saveState(state);
    emit(runtime);
    return;
  }

  if (command === "runtimes status") {
    const runtime = state.runtimes[args[2]];
    if (!runtime) {
      console.error(`alias ${args[2]} is not known; run create or connect first`);
      process.exitCode = 1;
      return;
    }
    runtime.healthCalls = (runtime.healthCalls || 0) + 1;
    state.runtimes[args[2]] = runtime;
    saveState(state);
    emit(runtime);
    return;
  }

  if (command === "runtimes list") {
    emit({ admin_profile: "default", aliases: Object.values(state.runtimes), state_root: dirname(statePath) });
    return;
  }

  if (command === "runtimes stop") {
    const runtime = state.runtimes[args[2]];
    if (!runtime) {
      console.error(`alias ${args[2]} is not known; already stopped`);
      return;
    }
    if (stopFailure(args[2])) {
      if (process.env.FAKE_TUNNEL_MALFORMED_STOP_ALIAS === args[2]) {
        process.stdout.write("not-json\n");
      } else {
        console.error(`stop failed for ${args[2]}`);
        process.exitCode = 9;
      }
      return;
    }
    runtime.process_running = false;
    saveState(state);
    emit({ ...runtime, stopped: true });
    return;
  }

  if (args[0] === "health") {
    const url = value("--url") || (value("--url-file") && readFileSync(value("--url-file"), "utf8").trim());
    const urlFile = value("--url-file");
    const runtime = Object.values(state.runtimes).find((item) => item.health_url === url || (urlFile && item.health_url_file === urlFile));
    if (!runtime) {
      emit({ healthz: { ok: false }, readyz: { ok: false }, result: "fail" });
      process.exitCode = 2;
      return;
    }
    runtime.healthCalls = (runtime.healthCalls || 0) + 1;
    saveState(state);
    const health = runtimeHealth(runtime);
    emit(health);
    if (health.result !== "ok") process.exitCode = 2;
    return;
  }

  emit({ ok: true });
}

await main();
