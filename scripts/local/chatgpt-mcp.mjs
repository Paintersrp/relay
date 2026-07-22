#!/usr/bin/env node

import { spawn } from "child_process";
import { createHash, randomUUID } from "crypto";
import {
  accessSync,
  constants,
  existsSync,
  mkdirSync,
  readFileSync,
  unlinkSync,
  writeFileSync,
} from "fs";
import { request as httpRequest } from "http";
import { request as httpsRequest } from "https";
import { dirname, delimiter, isAbsolute, join, resolve } from "path";
import process from "process";
import { fileURLToPath } from "url";

const SCRIPT_PATH = fileURLToPath(import.meta.url);
const SCRIPT_DIR = dirname(SCRIPT_PATH);
const REPO_ROOT = resolve(SCRIPT_DIR, "..", "..");
const ENV_PATH = join(REPO_ROOT, ".env");
const ENV_LOCAL_PATH = join(REPO_ROOT, ".env.local");
const ENV_FILE_PATHS = [ENV_PATH, ENV_LOCAL_PATH];
const ENV_EXAMPLE_PATH = join(REPO_ROOT, ".env.example");
const DEFAULT_PROFILE = "relay-mcp";
const DEFAULT_RELAY_BASE_URL = "http://127.0.0.1:8080";
const DEFAULT_RELAY_MCP_URL = `${DEFAULT_RELAY_BASE_URL}/mcp/planner`;
const DEFAULT_TUNNEL_MCP_TRANSPORT = "stdio";
const DEFAULT_TUNNEL_HEALTH_LISTEN_ADDR = "127.0.0.1:18200";
const DEFAULT_STARTUP_TIMEOUT_MS = 30_000;
const DEFAULT_POLL_INTERVAL_MS = 250;
const DEFAULT_STATE_FILE = join(
  REPO_ROOT,
  "data",
  "transport",
  "chatgpt-mcp-all.json",
);
const DEFAULT_RELAY_COMMAND = "go run ./cmd/relay";
const DEFAULT_RELAY_MCP_PROFILE = "planner";
const ALLOWED_RELAY_MCP_PROFILES = new Set(["planner", "auditor", "local_operator"]);
const RELAY_MCP_STDIO_LAUNCHER_PATH = join(
  REPO_ROOT,
  "scripts",
  "local",
  "relay-mcp-stdio.mjs",
);
const ALLOWED_TUNNEL_MCP_TRANSPORTS = new Set(["stdio", "http"]);
const NATIVE_RUNTIME_CAPABILITIES =
  "runtimes connect/list/status/stop/rm (tunnel-client 0.0.9+)";

const ROLE_DEFINITIONS = [
  {
    key: "wayfinder",
    label: "Wayfinder",
    path: "/mcp/wayfinder",
    idEnv: "RELAY_MCP_WAYFINDER_TUNNEL_ID",
    aliasEnv: "RELAY_MCP_WAYFINDER_ALIAS",
    profileEnv: "RELAY_MCP_WAYFINDER_PROFILE",
    alias: "relay-wayfinder",
    profile: "relay-wayfinder",
  },
  {
    key: "planner",
    label: "Planner",
    path: "/mcp/planner",
    idEnv: "RELAY_MCP_PLANNER_TUNNEL_ID",
    aliasEnv: "RELAY_MCP_PLANNER_ALIAS",
    profileEnv: "RELAY_MCP_PLANNER_PROFILE",
    alias: "relay-planner",
    profile: "relay-planner",
  },
  {
    key: "auditor",
    label: "Auditor",
    path: "/mcp/auditor",
    idEnv: "RELAY_MCP_AUDITOR_TUNNEL_ID",
    aliasEnv: "RELAY_MCP_AUDITOR_ALIAS",
    profileEnv: "RELAY_MCP_AUDITOR_PROFILE",
    alias: "relay-auditor",
    profile: "relay-auditor",
  },
];

class ValidationError extends Error {
  constructor(message) {
    super(message);
    this.name = "ValidationError";
  }
}

function main() {
  const originalEnvKeys = new Set(Object.keys(process.env));
  for (const envFilePath of ENV_FILE_PATHS) {
    loadEnvFile(envFilePath, originalEnvKeys);
  }

  const [command = "help", ...restArgs] = process.argv.slice(2);
  const options = parseOptions(restArgs);
  const config = getConfig();
  process.env.RELAY_MCP_PROFILE = config.relayMcpProfile;

  runCommand(command, config, options).then(
    (exitCode) => {
      process.exitCode = exitCode;
    },
    (error) => {
      handleError(error);
    },
  );
}

function loadEnvFile(filePath, originalEnvKeys) {
  if (!existsSync(filePath)) {
    return;
  }

  const content = readFileSync(filePath, "utf8");
  for (const rawLine of content.split(/\r?\n/u)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }

    const separatorIndex = line.indexOf("=");
    if (separatorIndex <= 0) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim();
    let value = line.slice(separatorIndex + 1).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/u.test(key)) {
      continue;
    }

    value = stripMatchingQuotes(value);
    if (!originalEnvKeys.has(key)) {
      process.env[key] = value;
    }
  }
}

function stripMatchingQuotes(value) {
  if (value.length < 2) {
    return value;
  }

  const first = value[0];
  const last = value[value.length - 1];
  if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
    return value.slice(1, -1);
  }

  return value;
}

function normalizeRelayMcpProfile(raw) {
  const profile = String(raw || DEFAULT_RELAY_MCP_PROFILE).trim().toLowerCase();
  if (ALLOWED_RELAY_MCP_PROFILES.has(profile)) {
    return profile;
  }
  console.error(
    `Unsupported RELAY_MCP_PROFILE ${JSON.stringify(profile)}; defaulting to planner.`,
  );
  return DEFAULT_RELAY_MCP_PROFILE;
}

function parseOptions(args) {
  const options = { skipRelayCheck: false };
  for (const arg of args) {
    if (arg === "--skip-relay-check") {
      options.skipRelayCheck = true;
      continue;
    }
    throw new ValidationError(`Unknown option: ${arg}`);
  }
  return options;
}

function getConfig() {
  const tunnelMcpTransport =
    process.env.TUNNEL_MCP_TRANSPORT || DEFAULT_TUNNEL_MCP_TRANSPORT;
  if (!ALLOWED_TUNNEL_MCP_TRANSPORTS.has(tunnelMcpTransport)) {
    throw new ValidationError(
      `TUNNEL_MCP_TRANSPORT must be one of: ${Array.from(ALLOWED_TUNNEL_MCP_TRANSPORTS).join(", ")}`,
    );
  }

  const relayBaseUrl = stripTrailingSlash(
    process.env.RELAY_MCP_BASE_URL || DEFAULT_RELAY_BASE_URL,
  );
  const roles = ROLE_DEFINITIONS.map((definition) => ({
    ...definition,
    tunnelId: process.env[definition.idEnv] || "",
    alias: process.env[definition.aliasEnv] || definition.alias,
    profile: process.env[definition.profileEnv] || definition.profile,
    endpoint: `${relayBaseUrl}${definition.path}`,
  }));

  return {
    envPath: ENV_PATH,
    envLocalPath: ENV_LOCAL_PATH,
    envExamplePath: ENV_EXAMPLE_PATH,
    tunnelProfile: process.env.TUNNEL_PROFILE || DEFAULT_PROFILE,
    tunnelId: process.env.TUNNEL_ID || "",
    tunnelMcpTransport,
    relayMcpUrl: process.env.RELAY_MCP_URL || DEFAULT_RELAY_MCP_URL,
    relayBaseUrl,
    relayCommand: process.env.RELAY_MCP_RELAY_COMMAND || DEFAULT_RELAY_COMMAND,
    relayMcpStdioCommand:
      process.env.RELAY_MCP_STDIO_COMMAND || buildDefaultRelayMcpCommand(),
    relayMcpStdioLauncherPath: RELAY_MCP_STDIO_LAUNCHER_PATH,
    relayMcpProfile: normalizeRelayMcpProfile(process.env.RELAY_MCP_PROFILE),
    tunnelClientPath: process.env.TUNNEL_CLIENT_PATH || "",
    tunnelClientArgs: parseCommandLine(process.env.TUNNEL_CLIENT_ARGS || ""),
    tunnelClientProfileDir: process.env.RELAY_MCP_PROFILE_DIR || "",
    controlPlaneApiKey: process.env.CONTROL_PLANE_API_KEY || "",
    tunnelHealthListenAddr:
      process.env.TUNNEL_HEALTH_LISTEN_ADDR ||
      DEFAULT_TUNNEL_HEALTH_LISTEN_ADDR,
    startupTimeoutMs: parsePositiveInteger(
      process.env.RELAY_MCP_STARTUP_TIMEOUT_MS,
      DEFAULT_STARTUP_TIMEOUT_MS,
    ),
    pollIntervalMs: parsePositiveInteger(
      process.env.RELAY_MCP_POLL_INTERVAL_MS,
      DEFAULT_POLL_INTERVAL_MS,
    ),
    stateFile: process.env.RELAY_MCP_STATE_FILE || DEFAULT_STATE_FILE,
    roles,
  };
}

async function runCommand(command, config, options) {
  switch (command) {
    case "help":
      printHelp(config);
      return 0;
    case "init":
      return runInit(config, options);
    case "start":
      return runStart(config, options);
    case "doctor":
      return runDoctor(config, options);
    case "init:all":
      return runInitAll(config);
    case "start:all":
      return runStartAll(config);
    case "stop:all":
      return runStopAll(config);
    case "status:all":
      return runStatusAll(config);
    case "doctor:all":
      return runDoctorAll(config);
    default:
      throw new ValidationError(`Unknown command: ${command}`);
  }
}

function printHelp(config) {
  console.log("ChatGPT Local MCP Tunnel");
  console.log("");
  console.log("Aggregate topology: one Relay daemon, three tunnel IDs, three ChatGPT apps.");
  console.log("Native runtime supervision: enabled");
  console.log(`Installed capability required: ${NATIVE_RUNTIME_CAPABILITIES}`);
  console.log("");
  console.log("Normal flow:");
  console.log("  one-time: npm run chatgpt-mcp:init:all");
  console.log("  check:    npm run chatgpt-mcp:doctor:all");
  console.log("  daily:    npm run chatgpt-mcp:start:all");
  console.log("  status:   npm run chatgpt-mcp:status:all");
  console.log("  stop:     npm run chatgpt-mcp:stop:all");
  console.log("");
  console.log("Commands:");
  console.log("  npm run chatgpt-mcp:init");
  console.log("  npm run chatgpt-mcp:start");
  console.log("  npm run chatgpt-mcp:doctor");
  console.log("  npm run chatgpt-mcp:help");
  console.log("  npm run chatgpt-mcp:init:all");
  console.log("  npm run chatgpt-mcp:doctor:all");
  console.log("  npm run chatgpt-mcp:start:all");
  console.log("  npm run chatgpt-mcp:stop:all");
  console.log("  npm run chatgpt-mcp:status:all");
  console.log("");
  console.log(`Relay command: ${redactSecrets(config.relayCommand, config.controlPlaneApiKey)}`);
  console.log(`Relay base URL: ${config.relayBaseUrl}`);
  console.log(`Relay MCP profile: ${config.relayMcpProfile}`);
  console.log(`Aggregate state file: ${config.stateFile}`);
  console.log("The three ChatGPT app registrations must select three distinct tunnel IDs.");
  console.log("Native runtimes generate their own ephemeral loopback health URLs; no aggregate health ports are configured.");
  console.log("Process environment overrides .env.local, which overrides .env, which overrides defaults.");
  console.log("Override names: RELAY_MCP_RELAY_COMMAND, RELAY_MCP_BASE_URL, RELAY_MCP_*_PROFILE, RELAY_MCP_*_ALIAS, RELAY_MCP_STARTUP_TIMEOUT_MS, RELAY_MCP_PROFILE_DIR, and TUNNEL_CLIENT_PATH.");
}

function validateAggregateConfig(config) {
  const errors = [];
  const ids = new Map();
  const profiles = new Map();
  const aliases = new Map();

  for (const role of config.roles) {
    if (!isConfiguredTunnelId(role.tunnelId)) {
      errors.push(`${role.label}: ${role.idEnv} is missing or still a placeholder`);
    } else if (ids.has(role.tunnelId)) {
      errors.push(`${role.label}: duplicate tunnel ID with ${ids.get(role.tunnelId)}`);
    } else {
      ids.set(role.tunnelId, role.label);
    }
    if (!role.alias) {
      errors.push(`${role.label}: alias is empty`);
    } else if (aliases.has(role.alias)) {
      errors.push(`${role.label}: duplicate alias with ${aliases.get(role.alias)}`);
    } else {
      aliases.set(role.alias, role.label);
    }
    if (!role.profile) {
      errors.push(`${role.label}: profile is empty`);
    } else if (profiles.has(role.profile)) {
      errors.push(`${role.label}: duplicate profile with ${profiles.get(role.profile)}`);
    } else {
      profiles.set(role.profile, role.label);
    }
  }

  return errors;
}

async function runInitAll(config) {
  const errors = validateAggregateConfig(config);
  if (errors.length) {
    printAggregateResults("init", config.roles.map((role) => ({ role, ok: false, reason: "configuration invalid" })));
    throw new ValidationError(errors.join("; "));
  }
  requireConfiguredApiKey(config, "init:all");
  const lockPath = `${config.stateFile}.lock`;
  acquireAggregateLock(lockPath);
  const cancellation = createAggregateCancellation();
  const changedRoles = [];
  const cleanup = onceAsync(() => stopRoles(config, changedRoles));
  const removeSignals = installAggregateSignalCleanup(cancellation, cleanup);
  const adapter = createNativeRuntimeAdapter(config);
  const results = [];
  try {
    for (const role of config.roles) {
      cancellation.check();
      const prepared = await prepareRuntime(adapter, config, role, changedRoles, cancellation);
      if (!prepared.ready) await waitForRuntimeReadiness(config, [role], adapter, cancellation);
      results.push({ role, ok: true, reason: prepared.reused ? "already configured and ready" : "connected and verified" });
    }
    printAggregateResults("init", results);
    writeAggregateState(config, { changedRoles, relay: null });
    return 0;
  } catch (error) {
    await cleanup();
    if (cancellation.cancelled) {
      console.error(`init:all cancelled by ${cancellation.signalName}`);
      return cancellation.exitCode;
    }
    if (error instanceof RuntimeCheckError) {
      results.push({ role: error.role, ok: false, reason: error.message });
      printAggregateResults("init", results);
      throw new ValidationError(error.message);
    }
    throw error;
  } finally {
    removeSignals();
    releaseAggregateLock(lockPath);
  }
}

async function runStartAll(config) {
  const errors = validateAggregateConfig(config);
  if (errors.length) throw new ValidationError(errors.join("; "));
  requireConfiguredApiKey(config, "start:all");
  const lockPath = `${config.stateFile}.lock`;
  acquireAggregateLock(lockPath);
  const cancellation = createAggregateCancellation();
  const changedRoles = [];
  let relay = null;
  const adapter = createNativeRuntimeAdapter(config);
  const cleanup = onceAsync(async () => {
    await stopRoles(config, changedRoles);
    if (relay?.owned) await stopOwnedRelay(relay.identity);
    removeAggregateState(config);
  });
  const removeSignals = installAggregateSignalCleanup(cancellation, cleanup);

  try {
    cancellation.check();
    const currentRelay = await checkAllRelayEndpoints(config, cancellation.signal);
    if (currentRelay.every((result) => result.ok)) {
      console.log("Relay: reusing healthy external daemon.");
    } else if (currentRelay.some((result) => result.ok)) {
      throw new ValidationError("Relay is partially healthy; refusing to start or attach a partial tunnel set.");
    } else {
      relay = await startRelay(config);
      relay.identity = await captureProcessIdentity(relay.child.pid, relay.expectedIdentity);
      if (!relay.identity) throw new ValidationError("Relay started, but its process identity could not be verified.");
      await waitForRelay(config, cancellation);
      relay.child.unref?.();
    }

    for (const role of config.roles) {
      cancellation.check();
      const prepared = await prepareRuntime(adapter, config, role, changedRoles, cancellation);
      if (!prepared.ready) await waitForRuntimeReadiness(config, [role], adapter, cancellation);
      console.log(`${role.label}: ${prepared.reused ? "reused" : "connected"} ${role.alias} at ${role.path}`);
    }
    cancellation.check();
    const finalRelay = await checkAllRelayEndpoints(config, cancellation.signal);
    if (finalRelay.some((result) => !result.ok)) {
      throw new ValidationError(`Relay readiness failed: ${finalRelay.filter((result) => !result.ok).map((result) => result.role.label).join(", ")}`);
    }
    writeAggregateState(config, { changedRoles, relay });
    console.log("Relay: all three role endpoints healthy; aggregate startup complete.");
    return 0;
  } catch (error) {
    await cleanup();
    if (cancellation.cancelled) {
      console.error(`start:all cancelled by ${cancellation.signalName}`);
      return cancellation.exitCode;
    }
    throw error;
  } finally {
    removeSignals();
    releaseAggregateLock(lockPath);
  }
}

async function runStopAll(config) {
  const state = readAggregateState(config);
  const results = await stopRoles(config, config.roles);
  let relayOk = true;
  if (state?.relayOwned && !state.relay) {
    relayOk = false;
    console.error("Relay ownership state is stale and lacks a verifiable process identity; preserved any live process.");
  } else if (state?.relay?.owned) {
    const ownership = await verifyProcessIdentity(state.relay.identity);
    if (ownership.ok) {
      await terminateProcessTree(state.relay.identity.pid);
      console.log("Relay: stopped verified launcher-owned daemon.");
    } else {
      relayOk = false;
      console.error(`Relay ownership is stale; preserved PID ${state.relay.identity?.pid ?? "unknown"}: ${ownership.reason}`);
    }
  } else {
    console.log("Relay: external daemon preserved.");
  }
  removeAggregateState(config);
  return results.every((result) => result.ok) && relayOk ? 0 : 1;
}

async function runStatusAll(config) {
  let adapter = null;
  let availabilityError = null;
  try { adapter = createNativeRuntimeAdapter(config); }
  catch (error) { availabilityError = error instanceof Error ? error.message : String(error); }
  const state = readAggregateState(config);
  console.log("component  endpoint                                      tunnel       alias/profile                 runtime/readiness");
  const relayResults = await checkAllRelayEndpoints(config);
  console.log(`Relay      ${config.relayBaseUrl.padEnd(45)} ${"-".padEnd(12)} ${"-".padEnd(28)} ${relayResults.every((result) => result.ok) ? "healthy" : "unhealthy"}`);
  for (const role of config.roles) {
    const relayResult = relayResults.find((result) => result.role.key === role.key);
    const runtime = adapter ? await inspectRuntime(adapter, role) : { statusOk: false, readyOk: false, bindingOk: false, reason: availabilityError };
    const stateMark = state?.relay?.owned ? "managed" : "native";
    const detail = runtime.statusOk && runtime.readyOk && runtime.bindingOk ? `${stateMark}/ready` : runtime.reason;
    console.log(`${role.label.padEnd(10)} ${role.endpoint.padEnd(45)} ${abbreviateTunnelId(role.tunnelId).padEnd(12)} ${`${role.alias}/${role.profile}`.padEnd(28)} ${relayResult.ok ? detail : `endpoint: ${relayResult.reason}`}`);
  }
  return 0;
}

async function runDoctorAll(config) {
  const configErrors = validateAggregateConfig(config);
  let adapter = null;
  let availabilityError = null;
  try { adapter = createNativeRuntimeAdapter(config); }
  catch (error) { availabilityError = error instanceof Error ? error.message : String(error); }
  const relayResults = await checkAllRelayEndpoints(config);
  const rows = [];
  for (const role of config.roles) {
    const issues = configErrors.filter((item) => item.startsWith(`${role.label}:`)).map((item) => item.slice(role.label.length + 2));
    const relayResult = relayResults.find((result) => result.role.key === role.key);
    if (!relayResult.ok) issues.push(`endpoint ping: ${relayResult.reason}`);
    const runtime = adapter ? await inspectRuntime(adapter, role) : { statusOk: false, readyOk: false, bindingOk: false, reason: availabilityError || "tunnel-client unavailable" };
    if (!runtime.statusOk) issues.push(`runtime status: ${runtime.reason}`);
    if (!runtime.readyOk) issues.push(`runtime readiness: ${runtime.reason}`);
    if (!runtime.bindingOk) issues.push(`role binding: ${runtime.reason}`);
    rows.push({ role, ok: issues.length === 0, reason: issues.join("; ") || "healthy" });
  }
  console.log(`configuration: ${configErrors.length ? configErrors.join("; ") : "ok"}`);
  console.log(`tunnel-client: ${adapter ? adapter.command : availabilityError}`);
  console.log(`control-plane key: ${isConfiguredApiKey(config.controlPlaneApiKey) ? "configured" : "missing"}`);
  printAggregateResults("doctor", rows);
  return configErrors.length || !isConfiguredApiKey(config.controlPlaneApiKey) || rows.some((row) => !row.ok) ? 1 : 0;
}

class RuntimeCheckError extends Error {
  constructor(role, message) { super(message); this.name = "RuntimeCheckError"; this.role = role; }
}

function createAggregateCancellation() {
  const controller = new AbortController();
  return {
    signal: controller.signal,
    cancelled: false,
    signalName: null,
    exitCode: 1,
    cancel(signalName) {
      if (this.cancelled) return;
      this.cancelled = true;
      this.signalName = signalName;
      this.exitCode = signalName === "SIGINT" ? 130 : 143;
      controller.abort(new Error(`cancelled by ${signalName}`));
    },
    check() {
      if (this.cancelled) throw new ValidationError(`aggregate operation cancelled by ${this.signalName}`);
    },
  };
}

function installAggregateSignalCleanup(cancellation, cleanup) {
  const handler = (signal) => {
    cancellation.cancel(signal);
    void cleanup().catch((error) => console.error(`cleanup after ${signal} failed: ${error.message}`));
  };
  process.once("SIGINT", handler);
  process.once("SIGTERM", handler);
  return () => {
    process.removeListener("SIGINT", handler);
    process.removeListener("SIGTERM", handler);
  };
}

function onceAsync(operation) {
  let promise = null;
  return () => promise || (promise = Promise.resolve().then(operation));
}

async function prepareRuntime(adapter, config, role, changedRoles, cancellation) {
  const current = await inspectRuntime(adapter, role, cancellation.signal);
  if (current.complete) return { ready: true, reused: true };
  if (current.malformed || current.failed) {
    throw new RuntimeCheckError(role, current.reason);
  }
  if (current.found) {
    cancellation.check();
    await adapter.stopRuntime(role, cancellation.signal);
    addRoleOnce(changedRoles, role);
  }
  cancellation.check();
  addRoleOnce(changedRoles, role);
  const connected = await adapter.connectRuntime(role, cancellation.signal);
  if (!connected.ok) throw new RuntimeCheckError(role, connected.reason);
  return { ready: false, reused: false };
}

function addRoleOnce(roles, role) {
  if (!roles.some((item) => item.key === role.key)) roles.push(role);
}

async function inspectRuntime(adapter, role, signal) {
  const status = await adapter.getRuntimeStatus(role, signal);
  if (!status.ok) return { statusOk: false, readyOk: false, bindingOk: false, found: !status.missing, malformed: status.kind === "malformed", failed: status.kind === "command" && !status.missing, reason: status.reason };
  const bindingOk = runtimeMatchesRole(status.runtime, role);
  const health = await adapter.getRuntimeHealth(role, status.runtime, signal);
  const readyOk = health.ok && health.healthy && health.ready;
  const reason = !bindingOk
    ? "native status binding does not exactly match tunnel ID, profile, or MCP endpoint"
    : !health.ok
      ? health.reason
      : !health.healthy
        ? "native healthz is unhealthy"
        : !health.ready
          ? "native readyz is not ready"
          : "healthy";
  return { statusOk: status.processRunning, readyOk, bindingOk, complete: status.processRunning && bindingOk && readyOk, found: true, malformed: health.kind === "malformed", runtime: status.runtime, reason };
}

function runtimeMatchesRole(runtime, role) {
  return runtime.alias === role.alias && runtime.tunnelId === role.tunnelId && runtime.profile === role.profile && runtime.endpoint === role.endpoint;
}

function bindingMatches(output, role) {
  try {
    const payload = typeof output === "string" ? JSON.parse(output) : output;
    const runtime = normalizeRuntimeStatus(payload);
    return Boolean(runtime && runtimeMatchesRole(runtime, role));
  } catch { return false; }
}

async function waitForRelay(config, cancellation) {
  return waitUntil(config.startupTimeoutMs, config.pollIntervalMs, async () => {
    cancellation.check();
    const results = await checkAllRelayEndpoints(config, cancellation.signal);
    return results.every((result) => result.ok);
  }, "Relay role endpoints", cancellation);
}

async function waitForRuntimeReadiness(config, roles, adapter, cancellation) {
  return waitUntil(config.startupTimeoutMs, config.pollIntervalMs, async () => {
    cancellation.check();
    const results = await Promise.all(roles.map((role) => inspectRuntime(adapter, role, cancellation.signal)));
    return results.every((result) => result.complete);
  }, "tunnel runtimes", cancellation);
}

async function waitUntil(timeoutMs, pollIntervalMs, check, label, cancellation = null) {
  const deadline = Date.now() + timeoutMs;
  let lastError = "not ready";
  do {
    try {
      cancellation?.check();
      if (await check()) return;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    if (Date.now() >= deadline) break;
    await delay(Math.min(pollIntervalMs, Math.max(1, deadline - Date.now())), cancellation?.signal);
  } while (Date.now() < deadline);
  throw new ValidationError(`${label} did not become ready within ${timeoutMs}ms (${lastError}).`);
}

async function startRelay(config) {
  const command = parseCommandLine(config.relayCommand);
  if (!command.length) throw new ValidationError("RELAY_MCP_RELAY_COMMAND cannot be empty.");
  const [file, ...args] = command;
  console.log(`Relay: starting ${file} ${args.join(" ")}`);
  const child = spawn(file, args, {
    cwd: REPO_ROOT,
    detached: true,
    // The daemon is intentionally detached. Ignored stdio prevents its inherited
    // terminal/test handles from keeping the aggregate launcher alive.
    stdio: ["ignore", "ignore", "ignore"],
    env: process.env,
  });
  child.on("error", () => {});
  return { child, owned: true, expectedIdentity: { pid: child.pid, executable: file, args, commandFingerprint: fingerprintCommand(file, args) } };
}

async function stopRoles(config, roles) {
  if (!roles.length) return [];
  const adapter = createNativeRuntimeAdapter(config);
  const results = [];
  for (const role of roles) {
    const result = await adapter.stopRuntime(role);
    const ok = result.ok || /not found|does not exist|not running|already stopped|stopped/u.test(result.reason || "");
    results.push({ role, ok, reason: ok ? "stopped" : result.reason });
    console.log(`${role.label}: ${ok ? "stopped or already stopped" : `stop failed: ${result.reason}`}`);
  }
  return results;
}

function buildNativeRuntimeConnectArgs(config, role) {
  const args = [
    "runtimes",
    "connect",
    "--alias",
    role.alias,
    "--profile",
    role.profile,
    "--tunnel-id",
    role.tunnelId,
    "--mcp-server-url",
    role.endpoint,
    "--runtime-api-key",
    "env:CONTROL_PLANE_API_KEY",
  ];
  args.push("--json");
  if (config.tunnelClientProfileDir) args.push("--profile-dir", config.tunnelClientProfileDir);
  if (config.tunnelClientPath) args.push("--tunnel-client-bin", config.tunnelClientPath);
  return args;
}

function createNativeRuntimeAdapter(config) {
  const command = resolveTunnelClient(config);
  const invoke = (args, role, signal) => runNativeCommand(command, args, config, role, signal);
  const parse = (result, operation) => parseNativeJsonResult(result, operation);
  return {
    command,
    async connectRuntime(role, signal) {
      const result = await invoke(buildNativeRuntimeConnectArgs(config, role), role, signal);
      return parse(result, `connect ${role.alias}`);
    },
    async getRuntimeStatus(role, signal) {
      const result = await invoke(["runtimes", "status", role.alias, "--json"], role, signal);
      if (result.code !== 0) {
        const reason = summarizeFailure(result);
        return { ok: false, kind: "command", missing: /not known|not found|does not exist/u.test(reason), reason };
      }
      const parsed = parse(result, `status ${role.alias}`);
      if (!parsed.ok) return parsed;
      const runtime = normalizeRuntimeStatus(parsed.value);
      if (!runtime) return { ok: false, kind: "malformed", missing: false, reason: `status ${role.alias} omitted required structured runtime fields` };
      return { ok: true, runtime, processRunning: runtime.processRunning };
    },
    async getRuntimeHealth(role, runtime, signal) {
      let args;
      if (runtime.healthUrlFile) args = ["health", "--url-file", runtime.healthUrlFile, "--json"];
      else if (runtime.healthUrl) args = ["health", "--url", runtime.healthUrl, "--json"];
      else return { ok: false, kind: "malformed", reason: `status ${role.alias} omitted health_url and health_url_file` };
      const result = await invoke(args, role, signal);
      if (result.code !== 0) return { ok: false, kind: "command", reason: summarizeFailure(result) };
      const parsed = parse(result, `health ${role.alias}`);
      if (!parsed.ok) return parsed;
      const health = normalizeRuntimeHealth(parsed.value);
      return health ? { ok: true, ...health } : { ok: false, kind: "malformed", reason: `health ${role.alias} omitted structured healthz/readyz fields` };
    },
    async stopRuntime(role, signal) {
      const result = await invoke(["runtimes", "stop", role.alias, "--json"], role, signal);
      const parsed = parse(result, `stop ${role.alias}`);
      if (parsed.ok) return { ok: true, reason: "stopped", raw: parsed.raw };
      return { ok: false, reason: parsed.reason, raw: parsed.raw };
    },
    async listRuntimes(signal) {
      const result = await invoke(["runtimes", "list", "--json"], null, signal);
      return parse(result, "list runtimes");
    },
  };
}

function parseNativeJsonResult(result, operation) {
  if (result.signal) return { ok: false, kind: "command", reason: `${operation} exited due to signal ${result.signal}`, raw: result };
  if (result.code !== 0) return { ok: false, kind: "command", reason: summarizeFailure(result), raw: result };
  const output = result.stdout.trim();
  if (!output) return { ok: false, kind: "malformed", reason: `${operation} returned empty JSON output`, raw: result };
  try {
    return { ok: true, value: JSON.parse(output), raw: result };
  } catch {
    return { ok: false, kind: "malformed", reason: `${operation} returned malformed JSON`, raw: result };
  }
}

function normalizeRuntimeStatus(payload) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return null;
  const roots = [payload, payload.runtime, payload.state, payload.binding, payload.config].filter((value) => value && typeof value === "object");
  const read = (...keys) => {
    for (const root of roots) for (const key of keys) if (root[key] !== undefined && root[key] !== null) return root[key];
    return undefined;
  };
  const alias = read("alias");
  const profile = read("profile", "profile_name", "profileName");
  const tunnelId = read("tunnel_id", "tunnelId");
  const endpoint = read("mcp_server_url", "mcpServerUrl", "endpoint", "server_url");
  const healthUrl = read("health_url", "current_health_url", "healthUrl");
  const healthUrlFile = read("health_url_file", "healthUrlFile");
  const processRunning = read("process_running", "processRunning", "running") === true || read("runtime_state") === "running";
  if (typeof alias !== "string" || typeof profile !== "string" || typeof tunnelId !== "string" || typeof endpoint !== "string") return null;
  if (typeof healthUrl !== "string" && typeof healthUrlFile !== "string") return null;
  return {
    alias,
    profile,
    tunnelId,
    endpoint,
    processRunning,
    pid: read("pid", "process_id", "processId") ?? null,
    healthUrl: typeof healthUrl === "string" ? healthUrl : null,
    healthUrlFile: typeof healthUrlFile === "string" ? healthUrlFile : null,
    raw: payload,
  };
}

function normalizeRuntimeHealth(payload) {
  if (!payload || typeof payload !== "object" || !payload.healthz || !payload.readyz) return null;
  if (typeof payload.healthz.ok !== "boolean" || typeof payload.readyz.ok !== "boolean") return null;
  return { healthy: payload.healthz.ok, ready: payload.readyz.ok, raw: payload };
}

function checkAllRelayEndpoints(config, signal = null) {
  return Promise.all(config.roles.map(async (role) => {
    if (signal?.aborted) throw new ValidationError("aggregate operation cancelled");
    try {
      await assertRelayReachable(role.endpoint, signal);
      return { role, ok: true, reason: "healthy" };
    } catch (error) {
      return { role, ok: false, reason: error instanceof Error ? error.message : String(error) };
    }
  }));
}

function printAggregateResults(command, results) {
  for (const result of results) {
    console.log(`${command}: ${result.role.label} ${result.ok ? "succeeded" : `failed (${result.reason})`}`);
  }
}

function runNativeCommand(command, args, config, role, signal = null) {
  const env = {
    ...process.env,
    CONTROL_PLANE_API_KEY: config.controlPlaneApiKey,
  };
  return runCommandCapture(command, [...config.tunnelClientArgs, ...args], env, config.controlPlaneApiKey, signal);
}

function runCommandCapture(command, args, env, secret, signal = null) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(command, args, { cwd: REPO_ROOT, stdio: ["ignore", "pipe", "pipe"], env });
    let settled = false;
    const abort = () => {
      if (!settled) child.kill();
    };
    signal?.addEventListener("abort", abort, { once: true });
    const stdoutSink = createRedactedSink(secret, (text) => process.stdout.write(text));
    const stderrSink = createRedactedSink(secret, (text) => process.stderr.write(text));
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => stdoutSink.push(chunk));
    child.stderr.on("data", (chunk) => stderrSink.push(chunk));
    child.on("error", (error) => {
      settled = true;
      signal?.removeEventListener("abort", abort);
      rejectPromise(new ValidationError(`Failed to start ${command}: ${error.message}`));
    });
    child.on("close", (code, childSignal) => {
      settled = true;
      signal?.removeEventListener("abort", abort);
      resolvePromise({ code: code ?? 1, signal: childSignal, stdout: stdoutSink.finish(), stderr: stderrSink.finish() });
    });
  });
}

function createRedactedSink(secret, write) {
  let pending = "";
  let output = "";
  return {
    push(chunk) {
      const text = pending + String(chunk);
      const keep = secret ? Math.max(secret.length - 1, 0) : 0;
      const visible = keep ? text.slice(0, -keep) : text;
      pending = keep ? text.slice(-keep) : "";
      const safe = redactSecrets(visible, secret);
      output += safe;
      write(safe);
    },
    finish() {
      if (pending) {
        const safe = redactSecrets(pending, secret);
        output += safe;
        write(safe);
        pending = "";
      }
      return output;
    },
  };
}

function runTunnelClient(command, args, controlPlaneApiKey) {
  return runCommandCapture(command, args, { ...process.env, CONTROL_PLANE_API_KEY: controlPlaneApiKey }, controlPlaneApiKey).then((result) => {
    if (result.signal) throw new ValidationError(`tunnel-client exited due to signal ${result.signal}.`);
    return result.code;
  });
}

async function runInit(config, options) {
  requireConfiguredTunnelId(config);
  requireConfiguredApiKey(config, "init");
  const tunnelClient = resolveTunnelClient(config);
  if (config.tunnelMcpTransport === "http" && !options.skipRelayCheck) await assertRelayReachable(config.relayMcpUrl);
  const initArgs = ["init", "--force", "--profile", config.tunnelProfile, "--tunnel-id", config.tunnelId];
  if (config.tunnelMcpTransport === "stdio") initArgs.push("--mcp-command", config.relayMcpStdioCommand);
  else initArgs.push("--mcp-server-url", config.relayMcpUrl);
  let exitCode = await runTunnelClient(tunnelClient, [...config.tunnelClientArgs, ...initArgs], config.controlPlaneApiKey);
  if (exitCode !== 0) return exitCode;
  return runTunnelClient(tunnelClient, [...config.tunnelClientArgs, "doctor", "--profile", config.tunnelProfile, "--explain", "--health.listen-addr", config.tunnelHealthListenAddr], config.controlPlaneApiKey);
}

async function runStart(config, options) {
  requireConfiguredApiKey(config, "start");
  const tunnelClient = resolveTunnelClient(config);
  if (config.tunnelMcpTransport === "http" && !options.skipRelayCheck) await assertRelayReachable(config.relayMcpUrl);
  console.log(`command: start\nprofile: ${config.tunnelProfile}\nMCP transport: ${config.tunnelMcpTransport}`);
  console.log(`tunnel ID configured: ${isConfiguredTunnelId(config.tunnelId) ? "yes" : "no"}`);
  return runTunnelClient(tunnelClient, [...config.tunnelClientArgs, "run", "--profile", config.tunnelProfile, "--health.listen-addr", config.tunnelHealthListenAddr], config.controlPlaneApiKey);
}

async function runDoctor(config, options) {
  const diagnostics = {
    envPathPresent: existsSync(config.envPath),
    envLocalPathPresent: existsSync(config.envLocalPath),
    tunnelIdConfigured: isConfiguredTunnelId(config.tunnelId),
    controlPlaneApiKeyConfigured: isConfiguredApiKey(config.controlPlaneApiKey),
    tunnelClientPath: null,
    localCheck: null,
  };
  try { diagnostics.tunnelClientPath = resolveTunnelClient(config); }
  catch (error) { diagnostics.tunnelClientPath = error instanceof Error ? error.message : String(error); }
  if (options.skipRelayCheck) diagnostics.localCheck = "skipped (--skip-relay-check)";
  else if (config.tunnelMcpTransport === "stdio") {
    try { await runRelayMcpSelfTest(config); diagnostics.localCheck = "ok"; }
    catch (error) { diagnostics.localCheck = error instanceof Error ? error.message : String(error); }
  } else {
    try { await assertRelayReachable(config.relayMcpUrl); diagnostics.localCheck = "ok"; }
    catch (error) { diagnostics.localCheck = error instanceof Error ? error.message : String(error); }
  }
  printDiagnostics(config, diagnostics);
  if (!diagnostics.controlPlaneApiKeyConfigured || !diagnostics.tunnelClientPath || (!options.skipRelayCheck && diagnostics.localCheck !== "ok")) return 1;
  return runTunnelClient(diagnostics.tunnelClientPath, [...config.tunnelClientArgs, "doctor", "--profile", config.tunnelProfile, "--explain", "--health.listen-addr", config.tunnelHealthListenAddr], config.controlPlaneApiKey);
}

function printDiagnostics(config, diagnostics) {
  console.log(`env file (.env): ${diagnostics.envPathPresent ? "present" : "missing"}`);
  console.log(`env file (.env.local): ${diagnostics.envLocalPathPresent ? "present" : "missing"}`);
  console.log(`profile: ${config.tunnelProfile}`);
  console.log(`MCP transport: ${config.tunnelMcpTransport}`);
  console.log(`Relay MCP profile: ${config.relayMcpProfile}`);
  console.log(`local MCP check: ${diagnostics.localCheck ?? "not run"}`);
  console.log(`tunnel ID configured: ${diagnostics.tunnelIdConfigured ? "yes" : "no"}`);
  console.log(`control-plane key configured: ${diagnostics.controlPlaneApiKeyConfigured ? "yes" : "no"}`);
  console.log(`tunnel-client path: ${diagnostics.tunnelClientPath ?? "unresolved"}`);
}

function requireConfiguredTunnelId(config) {
  if (!isConfiguredTunnelId(config.tunnelId)) throw new ValidationError("TUNNEL_ID is required for init. Set it in .env, .env.local, or the process environment.");
}

function requireConfiguredApiKey(config, commandName) {
  if (!isConfiguredApiKey(config.controlPlaneApiKey)) throw new ValidationError(`CONTROL_PLANE_API_KEY is required for ${commandName}. Set it in .env, .env.local, or the process environment.`);
}

function isConfiguredTunnelId(value) {
  return Boolean(value) && value !== "tunnel_REPLACE_ME" && /^tunnel_[0-9a-f]{32}$/u.test(value);
}

function isConfiguredApiKey(value) {
  return Boolean(value) && value !== "sk-REPLACE_ME" && value !== "sk_REPLACE_ME";
}

function resolveTunnelClient(config) {
  if (config.tunnelClientPath) {
    const explicitPath = isAbsolute(config.tunnelClientPath) ? config.tunnelClientPath : resolve(process.cwd(), config.tunnelClientPath);
    if (!existsSync(explicitPath)) throw new ValidationError(`TUNNEL_CLIENT_PATH does not exist: ${explicitPath}`);
    return explicitPath;
  }
  const resolvedPath = findOnPath(["tunnel-client", "tunnel-client.exe"]);
  if (!resolvedPath) throw new ValidationError("Set TUNNEL_CLIENT_PATH in .env, .env.local, or add tunnel-client to PATH.");
  return resolvedPath;
}

function findOnPath(commandNames) {
  const pathValue = process.env.PATH || "";
  const extensions = process.platform === "win32" ? (process.env.PATHEXT || ".COM;.EXE;.BAT;.CMD").split(";").filter(Boolean) : [""];
  for (const directory of pathValue.split(delimiter)) {
    if (!directory) continue;
    for (const commandName of commandNames) {
      for (const candidate of buildCommandCandidates(commandName, extensions)) {
        const candidatePath = join(directory, candidate);
        try { accessSync(candidatePath, constants.F_OK); return candidatePath; } catch { /* continue */ }
      }
    }
  }
  return null;
}

function buildCommandCandidates(commandName, extensions) {
  if (process.platform !== "win32") return [commandName];
  if (extensions.some((extension) => commandName.toLowerCase().endsWith(extension.toLowerCase()))) return [commandName];
  return [commandName, ...extensions.map((extension) => `${commandName}${extension}`)];
}

async function assertRelayReachable(relayMcpUrl, signal = null) {
  const response = await postJsonRpcPing(relayMcpUrl, signal);
  if (response.statusCode === 405) throw new ValidationError(`Relay endpoint returned HTTP 405 at ${relayMcpUrl}; use POST JSON-RPC ping.`);
  if (response.statusCode !== 200) throw new ValidationError(`Relay endpoint check failed with HTTP ${response.statusCode} at ${relayMcpUrl}.`);
  let payload;
  try { payload = JSON.parse(response.body); } catch { throw new ValidationError(`Relay endpoint returned HTTP 200 at ${relayMcpUrl}, but the body was not valid JSON.`); }
  if (payload?.jsonrpc !== "2.0" || !Object.prototype.hasOwnProperty.call(payload, "result")) throw new ValidationError(`Relay endpoint returned HTTP 200 at ${relayMcpUrl}, but ping did not return a JSON-RPC result.`);
}

function postJsonRpcPing(relayMcpUrl, signal = null) {
  return new Promise((resolvePromise, rejectPromise) => {
    let targetUrl;
    try { targetUrl = new URL(relayMcpUrl); }
    catch { rejectPromise(new ValidationError(`RELAY_MCP_URL is not a valid URL: ${relayMcpUrl}`)); return; }
    const requestImpl = targetUrl.protocol === "https:" ? httpsRequest : httpRequest;
    const body = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "ping", params: {} });
    const request = requestImpl(targetUrl, { method: "POST", headers: { "content-type": "application/json", "content-length": Buffer.byteLength(body) }, timeout: 5000 }, (response) => {
      const chunks = [];
      response.setEncoding("utf8");
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => resolvePromise({ statusCode: response.statusCode ?? 0, body: chunks.join("") }));
    });
    request.on("timeout", () => request.destroy(new ValidationError(`Relay endpoint is not reachable at ${relayMcpUrl}.`)));
    request.on("error", (error) => {
      if (error instanceof ValidationError) { rejectPromise(error); return; }
      rejectPromise(new ValidationError(`Relay endpoint is not reachable at ${relayMcpUrl}.`));
    });
    const abort = () => request.destroy(new ValidationError("aggregate operation cancelled"));
    signal?.addEventListener("abort", abort, { once: true });
    request.on("close", () => signal?.removeEventListener("abort", abort));
    request.write(body);
    request.end();
  });
}

function runRelayMcpSelfTest(config) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(process.execPath, [config.relayMcpStdioLauncherPath, "--self-test"], { stdio: ["ignore", "pipe", "pipe"], env: process.env });
    const stdoutSink = createRedactedSink(config.controlPlaneApiKey, () => {});
    const stderrSink = createRedactedSink(config.controlPlaneApiKey, (text) => process.stderr.write(text));
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => stdoutSink.push(chunk));
    child.stderr.on("data", (chunk) => stderrSink.push(chunk));
    child.on("error", (error) => rejectPromise(new ValidationError(`Failed to start Relay MCP stdio self-test: ${error.message}`)));
    child.on("close", (code, signal) => {
      const stdout = stdoutSink.finish();
      const stderr = stderrSink.finish();
      if (stdout.trim()) { rejectPromise(new ValidationError(`Relay MCP stdio self-test wrote unexpected stdout: ${stdout.trim()}`)); return; }
      if (signal || (code ?? 1) !== 0) { rejectPromise(new ValidationError(`Relay MCP stdio self-test failed with exit code ${code ?? 1}.${stderr.trim() ? ` ${stderr.trim()}` : ""}`)); return; }
      resolvePromise();
    });
  });
}

function buildDefaultRelayMcpCommand() {
  return `${quoteCommandArgument(normalizeCommandPathForTunnel(process.execPath))} ${quoteCommandArgument(normalizeCommandPathForTunnel(RELAY_MCP_STDIO_LAUNCHER_PATH))}`;
}

function quoteCommandArgument(value) {
  if (value === "") return '""';
  if (process.platform !== "win32" && !/[ \t"\n]/u.test(value)) return value;
  return `"${value.replace(/(\\*)"/g, "$1$1\\\"").replace(/(\\+)$/g, "$1$1")}"`;
}

function normalizeCommandPathForTunnel(value) {
  return process.platform === "win32" ? value.replace(/\\/g, "/") : value;
}

function redactSecrets(text, controlPlaneApiKey) {
  if (!controlPlaneApiKey) return text;
  return text.split(controlPlaneApiKey).join("[REDACTED]");
}

function parseCommandLine(value) {
  const tokens = [];
  const pattern = /"([^"\\]*(?:\\.[^"\\]*)*)"|'([^']*)'|([^\s]+)/gu;
  for (const match of String(value).matchAll(pattern)) tokens.push(match[1] ?? match[2] ?? match[3]);
  return tokens;
}

function parsePositiveInteger(value, fallback) {
  const parsed = Number.parseInt(value || "", 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function stripTrailingSlash(value) {
  return String(value).replace(/\/+$/u, "");
}

function summarizeFailure(result) {
  const message = `${result.stderr} ${result.stdout}`.replace(/\s+/gu, " ").trim();
  return message ? message.slice(0, 240) : `exit ${result.code}`;
}

function abbreviateTunnelId(value) {
  if (!value) return "missing";
  if (value.length <= 16) return `${value.slice(0, 8)}…`;
  return `${value.slice(0, 11)}…${value.slice(-4)}`;
}

function delay(milliseconds, signal = null) {
  return new Promise((resolvePromise, rejectPromise) => {
    if (signal?.aborted) { rejectPromise(new ValidationError("aggregate operation cancelled")); return; }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolvePromise();
    }, milliseconds);
    const abort = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", abort);
      rejectPromise(new ValidationError("aggregate operation cancelled"));
    };
    signal?.addEventListener("abort", abort, { once: true });
  });
}

function acquireAggregateLock(lockPath) {
  mkdirSync(dirname(lockPath), { recursive: true });
  try {
    writeFileSync(lockPath, JSON.stringify({ pid: process.pid }), { encoding: "utf8", flag: "wx" });
  } catch (error) {
    if (error?.code === "EEXIST") {
      let stale = false;
      try { stale = !isProcessAlive(JSON.parse(readFileSync(lockPath, "utf8")).pid); } catch { stale = true; }
      if (stale) { unlinkSync(lockPath); writeFileSync(lockPath, JSON.stringify({ pid: process.pid }), { encoding: "utf8", flag: "wx" }); return; }
      throw new ValidationError("Aggregate startup is already running; refusing duplicate launch.");
    }
    throw error;
  }
}

function releaseAggregateLock(lockPath) {
  try { unlinkSync(lockPath); } catch (error) { if (error?.code !== "ENOENT") throw error; }
}

function readAggregateState(config) {
  try {
    const state = JSON.parse(readFileSync(config.stateFile, "utf8"));
    return state && typeof state === "object" ? state : null;
  } catch { return null; }
}

function writeAggregateState(config, state) {
  mkdirSync(dirname(config.stateFile), { recursive: true });
  writeFileSync(config.stateFile, JSON.stringify({
    version: 2,
    updatedAt: new Date().toISOString(),
    desiredRoleBindings: config.roles.map((role) => ({ key: role.key, tunnelId: role.tunnelId, alias: role.alias, profile: role.profile, endpoint: role.endpoint })),
    runtimesChangedByOperation: (state.changedRoles || []).map((role) => role.alias),
    relay: state.relay?.owned ? { owned: true, identity: state.relay.identity } : { owned: false },
  }, null, 2) + "\n", "utf8");
}

function removeAggregateState(config) {
  try { unlinkSync(config.stateFile); } catch (error) { if (error?.code !== "ENOENT") throw error; }
}

function isProcessAlive(pid) {
  if (!pid || !Number.isInteger(Number(pid))) return false;
  try { process.kill(Number(pid), 0); return true; } catch { return false; }
}

function fingerprintCommand(value) {
  return createHash("sha256").update(String(value).replace(/\\/gu, "/").toLowerCase()).digest("hex");
}

function fingerprintCommandLine(value) {
  return fingerprintCommand(String(value).replace(/\0/gu, " ").replace(/\s+/gu, " ").trim());
}

async function captureProcessIdentity(pid, expected) {
  const observed = await inspectProcessIdentity(pid);
  if (!observed) return null;
  return {
    pid: Number(pid),
    startTime: observed.startTime,
    executable: observed.executable || expected.executable,
    commandFingerprint: fingerprintCommandLine(observed.commandLine),
    expectedExecutable: expected.executable,
    expectedArguments: expected.args,
    launchToken: randomUUID(),
  };
}

async function inspectProcessIdentity(pid) {
  if (!isProcessAlive(pid)) return null;
  if (process.platform !== "win32") {
    try {
      const stat = readFileSync(`/proc/${Number(pid)}/stat`, "utf8");
      const commandLine = readFileSync(`/proc/${Number(pid)}/cmdline`, "utf8");
      const executable = readFileSync(`/proc/${Number(pid)}/exe`, "utf8");
      const fields = stat.slice(stat.lastIndexOf(")") + 2).split(/\s+/u);
      return { startTime: fields[19] || null, executable, commandLine };
    } catch { return null; }
  }
  const script = "$p=Get-CimInstance Win32_Process -Filter 'ProcessId = " + Number(pid) + "'; if ($p) { $p | Select-Object ProcessId,CreationDate,ExecutablePath,CommandLine | ConvertTo-Json -Compress }";
  const output = await captureSimpleCommand("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", script]);
  if (!output.trim()) return null;
  try {
    const value = JSON.parse(output);
    return { startTime: value.CreationDate || null, executable: value.ExecutablePath || "", commandLine: value.CommandLine || "" };
  } catch { return null; }
}

async function verifyProcessIdentity(identity) {
  if (!identity?.pid) return { ok: false, reason: "no recorded Relay process identity" };
  if (!isProcessAlive(identity.pid)) return { ok: false, stopped: true, reason: "recorded process is no longer alive" };
  const observed = await inspectProcessIdentity(identity.pid);
  if (!observed) return { ok: false, reason: "live process identity could not be inspected" };
  if (identity.startTime && observed.startTime && String(identity.startTime) !== String(observed.startTime)) return { ok: false, reason: "process start time does not match" };
  const expectedExecutable = String(identity.expectedExecutable || identity.executable || "").replace(/\\/gu, "/").toLowerCase();
  const expectedName = expectedExecutable.split("/").pop().replace(/\.exe$/u, "");
  const observedName = String(observed.executable).replace(/\\/gu, "/").toLowerCase().split("/").pop().replace(/\.exe$/u, "");
  if (expectedName && observedName !== expectedName) return { ok: false, reason: "executable does not match" };
  for (const argument of identity.expectedArguments || []) {
    if (!String(observed.commandLine).replace(/\\/gu, "/").includes(String(argument).replace(/\\/gu, "/"))) return { ok: false, reason: "command fingerprint does not match" };
  }
  if (identity.commandFingerprint && fingerprintCommandLine(observed.commandLine) !== identity.commandFingerprint) return { ok: false, reason: "command fingerprint does not match" };
  return { ok: true, stopped: false };
}

function captureSimpleCommand(command, args) {
  return new Promise((resolvePromise) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "ignore"] });
    const chunks = [];
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => chunks.push(chunk));
    child.on("error", () => resolvePromise(""));
    child.on("close", () => resolvePromise(chunks.join("")));
  });
}

async function stopOwnedRelay(identity) {
  const ownership = await verifyProcessIdentity(identity);
  if (!ownership.ok) {
    console.error(`Relay ownership could not be confirmed; preserving live process: ${ownership.reason}`);
    return ownership;
  }
  if (!ownership.stopped) await terminateProcessTree(identity.pid);
  return { ok: true };
}

async function terminateProcessTree(pid) {
  if (!pid || !isProcessAlive(pid)) return;
  const shutdown = buildProcessShutdownPlan(pid, process.platform);
  if (shutdown.command) {
    await runSimpleCommand(shutdown.command, shutdown.args);
    return;
  }
  try { process.kill(-Number(pid), "SIGTERM"); } catch { try { process.kill(Number(pid), "SIGTERM"); } catch { /* already stopped */ } }
}

function buildProcessShutdownPlan(pid, platform = process.platform) {
  if (platform === "win32") return { command: "taskkill", args: ["/PID", String(pid), "/T", "/F"] };
  return { command: null, args: [-Number(pid), "SIGTERM"] };
}

function runSimpleCommand(command, args) {
  return new Promise((resolvePromise) => {
    const child = spawn(command, args, { stdio: "ignore" });
    child.on("error", () => resolvePromise());
    child.on("close", () => resolvePromise());
  });
}

function handleError(error) {
  if (error instanceof ValidationError) { console.error(error.message); process.exitCode = 1; return; }
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}

export {
  ROLE_DEFINITIONS,
  assertRelayReachable,
  bindingMatches,
  buildNativeRuntimeConnectArgs,
  buildProcessShutdownPlan,
  getConfig,
  loadEnvFile,
  acquireAggregateLock,
  releaseAggregateLock,
  normalizeRelayMcpProfile,
  redactSecrets,
  validateAggregateConfig,
};

if (process.argv[1] && resolve(process.argv[1]) === SCRIPT_PATH) main();
