#!/usr/bin/env node

import { spawn } from "child_process";
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
    healthEnv: "RELAY_MCP_WAYFINDER_HEALTH_ADDR",
    alias: "relay-wayfinder",
    profile: "relay-wayfinder",
    healthAddress: "127.0.0.1:18201",
  },
  {
    key: "planner",
    label: "Planner",
    path: "/mcp/planner",
    idEnv: "RELAY_MCP_PLANNER_TUNNEL_ID",
    aliasEnv: "RELAY_MCP_PLANNER_ALIAS",
    profileEnv: "RELAY_MCP_PLANNER_PROFILE",
    healthEnv: "RELAY_MCP_PLANNER_HEALTH_ADDR",
    alias: "relay-planner",
    profile: "relay-planner",
    healthAddress: "127.0.0.1:18202",
  },
  {
    key: "auditor",
    label: "Auditor",
    path: "/mcp/auditor",
    idEnv: "RELAY_MCP_AUDITOR_TUNNEL_ID",
    aliasEnv: "RELAY_MCP_AUDITOR_ALIAS",
    profileEnv: "RELAY_MCP_AUDITOR_PROFILE",
    healthEnv: "RELAY_MCP_AUDITOR_HEALTH_ADDR",
    alias: "relay-auditor",
    profile: "relay-auditor",
    healthAddress: "127.0.0.1:18203",
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
    healthAddress:
      process.env[definition.healthEnv] || definition.healthAddress,
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
  console.log("Process environment overrides .env.local, which overrides .env, which overrides defaults.");
  console.log("Override names: RELAY_MCP_RELAY_COMMAND, RELAY_MCP_BASE_URL, RELAY_MCP_*_PROFILE, RELAY_MCP_*_ALIAS, RELAY_MCP_*_HEALTH_ADDR, RELAY_MCP_STARTUP_TIMEOUT_MS, RELAY_MCP_PROFILE_DIR, and TUNNEL_CLIENT_PATH.");
}

function validateAggregateConfig(config) {
  const errors = [];
  const ids = new Map();
  const profiles = new Map();
  const aliases = new Map();
  const healthAddresses = new Map();

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
    if (!role.healthAddress) {
      errors.push(`${role.label}: health address is empty`);
    } else if (healthAddresses.has(role.healthAddress)) {
      errors.push(
        `${role.label}: duplicate health address with ${healthAddresses.get(role.healthAddress)}`,
      );
    } else {
      healthAddresses.set(role.healthAddress, role.label);
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
  const tunnelClient = resolveTunnelClient(config);
  const results = [];

  for (const role of config.roles) {
    const result = await runNativeCommand(
      tunnelClient,
      buildNativeRuntimeConnectArgs(config, role),
      config,
      role,
    );
    results.push({ role, ok: result.code === 0, reason: result.code === 0 ? "connected" : summarizeFailure(result) });
  }
  printAggregateResults("init", results);
  if (results.some((result) => !result.ok)) {
    await stopRoles(config, results.filter((result) => result.ok).map((result) => result.role));
    return 1;
  }
  writeAggregateState(config, { relayOwned: false, relayPid: null });
  return 0;
}

async function runStartAll(config) {
  const errors = validateAggregateConfig(config);
  if (errors.length) {
    throw new ValidationError(errors.join("; "));
  }
  requireConfiguredApiKey(config, "start:all");
  const tunnelClient = resolveTunnelClient(config);
  const lockPath = `${config.stateFile}.lock`;
  acquireAggregateLock(lockPath);
  let relayChild = null;
  let relayOwned = false;
  let connectedRoles = [];
  const signalCleanup = installAggregateSignalCleanup(async () => {
    await cleanupAggregate(config, connectedRoles, relayChild, relayOwned);
  });

  try {
    const currentRelay = await checkAllRelayEndpoints(config);
    if (currentRelay.every((result) => result.ok)) {
      console.log("Relay: reusing healthy external daemon.");
    } else if (currentRelay.some((result) => result.ok)) {
      throw new ValidationError(
        "Relay is partially healthy; refusing to start or attach a partial tunnel set.",
      );
    } else {
      relayChild = startRelay(config);
      relayOwned = true;
      await waitForRelay(config);
    }

    relayChild?.unref?.();
    for (const role of config.roles) {
      connectedRoles.push(role);
      const result = await runNativeCommand(
        tunnelClient,
        buildNativeRuntimeConnectArgs(config, role),
        config,
        role,
      );
      if (result.code !== 0) {
        throw new ValidationError(`${role.label} runtime failed: ${summarizeFailure(result)}`);
      }
      console.log(`${role.label}: ${role.alias} connected to ${role.path}`);
    }

    await waitForRuntimeReadiness(config, connectedRoles);
    const finalRelay = await checkAllRelayEndpoints(config);
    if (finalRelay.some((result) => !result.ok)) {
      throw new ValidationError(
        `Relay readiness failed: ${finalRelay.filter((result) => !result.ok).map((result) => result.role.label).join(", ")}`,
      );
    }
    writeAggregateState(config, {
      relayOwned,
      relayPid: relayChild?.pid ?? null,
    });
    console.log("Relay: all three role endpoints healthy; aggregate startup complete.");
    return 0;
  } catch (error) {
    await cleanupAggregate(config, connectedRoles, relayChild, relayOwned);
    throw error;
  } finally {
    signalCleanup?.();
    releaseAggregateLock(lockPath);
  }
}

async function runStopAll(config) {
  const state = readAggregateState(config);
  const results = await stopRoles(config, config.roles);
  if (state?.relayOwned && state.relayPid) {
    await terminateProcessTree(state.relayPid);
    console.log("Relay: stopped launcher-owned daemon.");
  } else {
    console.log("Relay: external daemon preserved.");
  }
  removeAggregateState(config);
  return results.every((result) => result.ok) ? 0 : 1;
}

async function runStatusAll(config) {
  let tunnelClient = null;
  try {
    tunnelClient = resolveTunnelClient(config);
  } catch (error) {
    if (!(error instanceof ValidationError)) throw error;
  }
  const state = readAggregateState(config);
  const runtimeList = tunnelClient
    ? await runNativeCommand(tunnelClient, ["runtimes", "list", "--json"], config)
    : null;
  console.log("component  endpoint                                      tunnel       alias/profile                 runtime/readiness");
  const relayResults = await checkAllRelayEndpoints(config);
  console.log(`Relay      ${config.relayBaseUrl.padEnd(45)} ${"-".padEnd(12)} ${"-".padEnd(28)} ${relayResults.every((result) => result.ok) ? "healthy" : "unhealthy"}`);
  for (const role of config.roles) {
    const relay = relayResults.find((result) => result.role.key === role.key);
    const runtime = tunnelClient
      ? await inspectRuntime(config, tunnelClient, role, runtimeList)
      : { statusOk: false, readyOk: false, reason: "tunnel-client unavailable" };
    const stateMark = state?.relayPid && isProcessAlive(state.relayPid) ? "managed" : "native";
    const detail = runtime.statusOk && runtime.readyOk ? `${stateMark}/ready` : runtime.reason;
    console.log(`${role.label.padEnd(10)} ${role.endpoint.padEnd(45)} ${abbreviateTunnelId(role.tunnelId).padEnd(12)} ${`${role.alias}/${role.profile}`.padEnd(28)} ${relay.ok ? detail : `endpoint: ${relay.reason}`}`);
  }
  return 0;
}

async function runDoctorAll(config) {
  const configErrors = validateAggregateConfig(config);
  let tunnelClient = null;
  let availabilityError = null;
  try {
    tunnelClient = resolveTunnelClient(config);
  } catch (error) {
    availabilityError = error instanceof Error ? error.message : String(error);
  }
  const relayResults = await checkAllRelayEndpoints(config);
  const runtimeList = tunnelClient
    ? await runNativeCommand(tunnelClient, ["runtimes", "list", "--json"], config)
    : null;
  const rows = [];
  for (const role of config.roles) {
    const issues = [];
    for (const error of configErrors.filter((item) => item.startsWith(`${role.label}:`))) {
      issues.push(error.slice(role.label.length + 2));
    }
    const relay = relayResults.find((result) => result.role.key === role.key);
    if (!relay.ok) issues.push(`endpoint ping: ${relay.reason}`);
    let runtime = { statusOk: false, readyOk: false, bindingOk: false, reason: availabilityError || "tunnel-client unavailable" };
    if (tunnelClient) runtime = await inspectRuntime(config, tunnelClient, role, runtimeList);
    if (!runtime.statusOk) issues.push(`runtime status: ${runtime.reason}`);
    if (!runtime.readyOk) issues.push(`runtime readiness: ${runtime.reason}`);
    if (!runtime.bindingOk) issues.push(`role binding: ${runtime.reason}`);
    rows.push({ role, ok: issues.length === 0, reason: issues.join("; ") || "healthy" });
  }
  console.log(`configuration: ${configErrors.length ? configErrors.join("; ") : "ok"}`);
  console.log(`tunnel-client: ${tunnelClient ? tunnelClient : availabilityError}`);
  console.log(`control-plane key: ${isConfiguredApiKey(config.controlPlaneApiKey) ? "configured" : "missing"}`);
  printAggregateResults("doctor", rows);
  return configErrors.length || !isConfiguredApiKey(config.controlPlaneApiKey) || rows.some((row) => !row.ok) ? 1 : 0;
}

async function inspectRuntime(config, tunnelClient, role, runtimeList = null) {
  const status = await runNativeCommand(
    tunnelClient,
    ["runtimes", "status", role.alias, "--json"],
    config,
    role,
  );
  const ready = await runNativeCommand(
    tunnelClient,
    ["health", "--url", healthUrl(role), "--json"],
    config,
    role,
  );
  const combined = `${status.stdout}\n${status.stderr}\n${runtimeList?.stdout || ""}\n${runtimeList?.stderr || ""}`;
  const listOk = !runtimeList || runtimeList.code === 0;
  const state = readAggregateState(config);
  const stateBindingOk = state?.roles?.some((entry) =>
    entry.key === role.key &&
    entry.tunnelId === role.tunnelId &&
    entry.alias === role.alias &&
    entry.profile === role.profile &&
    entry.endpoint === role.endpoint,
  );
  const bindingOk = status.code === 0 && listOk && (bindingMatches(combined, role) || stateBindingOk);
  const reason = status.code !== 0
    ? summarizeFailure(status)
    : ready.code !== 0
      ? summarizeFailure(ready)
      : bindingOk
        ? "healthy"
        : "status did not confirm tunnel/profile/endpoint binding";
  return {
    statusOk: status.code === 0 && listOk,
    readyOk: ready.code === 0,
    bindingOk,
    reason,
  };
}

function bindingMatches(output, role) {
  const normalized = output.toLowerCase();
  return [role.alias, role.profile, role.tunnelId, role.endpoint].every((value) =>
    normalized.includes(String(value).toLowerCase()),
  );
}

async function waitForRelay(config) {
  return waitUntil(
    config.startupTimeoutMs,
    config.pollIntervalMs,
    async () => {
      const results = await checkAllRelayEndpoints(config);
      return results.every((result) => result.ok);
    },
    "Relay role endpoints",
  );
}

async function waitForRuntimeReadiness(config, roles) {
  return waitUntil(
    config.startupTimeoutMs,
    config.pollIntervalMs,
    async () => {
      const tunnelClient = resolveTunnelClient(config);
      const results = await Promise.all(roles.map((role) => inspectRuntime(config, tunnelClient, role)));
      return results.every((result) => result.statusOk && result.readyOk && result.bindingOk);
    },
    "tunnel runtimes",
  );
}

async function waitUntil(timeoutMs, pollIntervalMs, check, label) {
  const deadline = Date.now() + timeoutMs;
  let lastError = "not ready";
  do {
    try {
      if (await check()) return;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    if (Date.now() >= deadline) break;
    await delay(Math.min(pollIntervalMs, Math.max(1, deadline - Date.now())));
  } while (Date.now() < deadline);
  throw new ValidationError(`${label} did not become ready within ${timeoutMs}ms (${lastError}).`);
}

function startRelay(config) {
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
  return child;
}

function installAggregateSignalCleanup(cleanup) {
  let called = false;
  const handler = async (signal) => {
    if (called) return;
    called = true;
    try {
      await cleanup();
    } finally {
      process.exitCode = signal === "SIGINT" ? 130 : 143;
    }
  };
  process.once("SIGINT", handler);
  process.once("SIGTERM", handler);
  return () => {
    process.removeListener("SIGINT", handler);
    process.removeListener("SIGTERM", handler);
  };
}

async function cleanupAggregate(config, roles, relayChild, relayOwned) {
  if (roles.length) await stopRoles(config, roles);
  if (relayOwned) {
    await terminateProcessTree(relayChild?.pid);
  }
  removeAggregateState(config);
}

async function stopRoles(config, roles) {
  const tunnelClient = resolveTunnelClient(config);
  const results = [];
  for (const role of roles) {
    const result = await runNativeCommand(
      tunnelClient,
      ["runtimes", "stop", role.alias, "--json"],
      config,
      role,
    );
    const ok = result.code === 0 || /not found|does not exist|not running|already stopped|stopped/u.test(`${result.stdout} ${result.stderr}`);
    results.push({ role, ok, reason: ok ? "stopped" : summarizeFailure(result) });
    console.log(`${role.label}: ${ok ? "stopped or already stopped" : `stop failed: ${summarizeFailure(result)}`}`);
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
  if (config.tunnelClientProfileDir) args.push("--profile-dir", config.tunnelClientProfileDir);
  if (config.tunnelClientPath) args.push("--tunnel-client-bin", config.tunnelClientPath);
  return args;
}

function checkAllRelayEndpoints(config) {
  return Promise.all(config.roles.map(async (role) => {
    try {
      await assertRelayReachable(role.endpoint);
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

function runNativeCommand(command, args, config, role) {
  const env = {
    ...process.env,
    CONTROL_PLANE_API_KEY: config.controlPlaneApiKey,
    HEALTH_LISTEN_ADDR: role?.healthAddress || process.env.HEALTH_LISTEN_ADDR || "",
  };
  return runCommandCapture(command, [...config.tunnelClientArgs, ...args], env, config.controlPlaneApiKey);
}

function runCommandCapture(command, args, env, secret) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(command, args, { cwd: REPO_ROOT, stdio: ["ignore", "pipe", "pipe"], env });
    const stdoutSink = createRedactedSink(secret, (text) => process.stdout.write(text));
    const stderrSink = createRedactedSink(secret, (text) => process.stderr.write(text));
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => stdoutSink.push(chunk));
    child.stderr.on("data", (chunk) => stderrSink.push(chunk));
    child.on("error", (error) => rejectPromise(new ValidationError(`Failed to start ${command}: ${error.message}`)));
    child.on("close", (code, signal) => resolvePromise({ code: code ?? 1, signal, stdout: stdoutSink.finish(), stderr: stderrSink.finish() }));
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
  return Boolean(value) && value !== "tunnel_REPLACE_ME" && /^tunnel_[A-Za-z0-9_-]+$/u.test(value);
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

async function assertRelayReachable(relayMcpUrl) {
  const response = await postJsonRpcPing(relayMcpUrl);
  if (response.statusCode === 405) throw new ValidationError(`Relay endpoint returned HTTP 405 at ${relayMcpUrl}; use POST JSON-RPC ping.`);
  if (response.statusCode !== 200) throw new ValidationError(`Relay endpoint check failed with HTTP ${response.statusCode} at ${relayMcpUrl}.`);
  let payload;
  try { payload = JSON.parse(response.body); } catch { throw new ValidationError(`Relay endpoint returned HTTP 200 at ${relayMcpUrl}, but the body was not valid JSON.`); }
  if (payload?.jsonrpc !== "2.0" || !Object.prototype.hasOwnProperty.call(payload, "result")) throw new ValidationError(`Relay endpoint returned HTTP 200 at ${relayMcpUrl}, but ping did not return a JSON-RPC result.`);
}

function postJsonRpcPing(relayMcpUrl) {
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

function healthUrl(role) {
  const address = role.healthAddress.startsWith("http") ? role.healthAddress : `http://${role.healthAddress}`;
  return `${stripTrailingSlash(address)}/readyz`;
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

function delay(milliseconds) {
  return new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));
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
  writeFileSync(config.stateFile, JSON.stringify({ version: 1, updatedAt: new Date().toISOString(), relayOwned: Boolean(state.relayOwned), relayPid: state.relayPid ?? null, roles: config.roles.map((role) => ({ key: role.key, tunnelId: role.tunnelId, alias: role.alias, profile: role.profile, endpoint: role.endpoint, healthAddress: role.healthAddress })) }, null, 2) + "\n", "utf8");
}

function removeAggregateState(config) {
  try { unlinkSync(config.stateFile); } catch (error) { if (error?.code !== "ENOENT") throw error; }
}

function isProcessAlive(pid) {
  if (!pid || !Number.isInteger(Number(pid))) return false;
  try { process.kill(Number(pid), 0); return true; } catch { return false; }
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
