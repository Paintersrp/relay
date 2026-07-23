#!/usr/bin/env node

import { execFileSync, spawn } from "child_process";
import { createHash, randomUUID } from "crypto";
import {
  accessSync,
  constants,
  closeSync,
  existsSync,
  fsyncSync,
  mkdirSync,
  openSync,
  readFileSync,
  readlinkSync,
  renameSync,
  rmdirSync,
  statSync,
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
  let priorState = null;
  const journal = new Map();
  let cleanupResult = null;
  const cleanup = onceAsync(async () => {
    cleanupResult = await cleanupAggregate(config, { journal, relay: null });
    reportCleanupFailures(cleanupResult);
    return cleanupResult;
  });
  const removeSignals = installAggregateSignalCleanup(cancellation, cleanup);
  const results = [];
  let operationError = null;
  let exitCode = 0;
  try {
    priorState = requireReadableAggregateState(config, "init:all");
    const adapter = createNativeRuntimeAdapter(config);
    await reconcileRetiredAliases(adapter, config, priorState, journal, cancellation);
    for (const role of config.roles) {
      cancellation.check();
      const prepared = await prepareRuntime(adapter, config, role, journal, cancellation);
      if (!prepared.ready) await waitForRuntimeReadiness(config, [role], adapter, cancellation);
      results.push({ role, ok: true, reason: prepared.reused ? "already configured and ready" : "connected and verified" });
    }
    printAggregateResults("init", results);
    writeAggregateState(config, { relay: priorState?.relay || { owned: false }, residualBindings: [] });
    exitCode = 0;
  } catch (error) {
    cleanupResult = await cleanup();
    if (cancellation.cancelled) {
      console.error(`init:all cancelled by ${cancellation.signalName}`);
      operationError = new ValidationError(`init:all cancelled by ${cancellation.signalName}`);
      exitCode = cancellation.exitCode;
    } else if (error instanceof RuntimeCheckError) {
      results.push({ role: error.role, ok: false, reason: error.message });
      printAggregateResults("init", results);
      operationError = withCleanupFailures(new ValidationError(error.message), cleanupResult);
    } else {
      operationError = withCleanupFailures(error, cleanupResult);
    }
  } finally {
    removeSignals();
    const releaseResult = releaseAggregateLock(lockPath);
    operationError = withLockReleaseFailure(operationError, releaseResult);
    if (cancellation.cancelled && releaseResult.ok) operationError = null;
  }
  if (operationError) throw operationError;
  return exitCode;
}

function requireReadableAggregateState(config, operation) {
  const result = readAggregateState(config);
  if (result.kind === "malformed" || result.kind === "unsupported version" || result.kind === "read failure") {
    throw new ValidationError(`Cannot begin ${operation} with ${result.kind} aggregate state: ${result.reason}`);
  }
  if (result.kind === "valid" && result.migrated) {
    // The caller already holds the aggregate lock, so persisting the migrated
    // version 3 state here is race-free.
    writeAggregateState(config, { desiredRoleBindings: result.state.desiredRoleBindings, residualBindings: result.state.residualBindings, relay: result.state.relay });
    console.log(`${operation}: migrated version 2 aggregate state to version 3.`);
  }
  return result.kind === "valid" ? result.state : null;
}

async function runStartAll(config) {
  const errors = validateAggregateConfig(config);
  if (errors.length) throw new ValidationError(errors.join("; "));
  requireConfiguredApiKey(config, "start:all");
  const lockPath = `${config.stateFile}.lock`;
  acquireAggregateLock(lockPath);
  const cancellation = createAggregateCancellation();
  let priorState = null;
  const journal = new Map();
  let relay = null;
  let adapter = null;
  let cleanupResult = null;
  const cleanup = onceAsync(async () => {
    cleanupResult = await cleanupAggregate(config, { journal, relay });
    reportCleanupFailures(cleanupResult);
    return cleanupResult;
  });
  const removeSignals = installAggregateSignalCleanup(cancellation, cleanup);
  let operationError = null;
  let exitCode = 0;

  try {
    priorState = requireReadableAggregateState(config, "start:all");
    cancellation.check();
    const currentRelay = await checkAllRelayEndpoints(config, cancellation.signal);
    if (currentRelay.every((result) => result.ok)) {
      if (priorState?.relay?.owned && priorState.relay.identity && (await verifyProcessIdentity(priorState.relay.identity)).ok) {
        relay = { owned: true, identity: priorState.relay.identity, preserved: true };
        console.log("Relay: reusing healthy launcher-owned daemon.");
      } else {
        console.log("Relay: reusing healthy external daemon.");
      }
    } else if (currentRelay.some((result) => result.ok)) {
      throw new ValidationError("Relay is partially healthy; refusing to start or attach a partial tunnel set.");
    } else {
      if (priorState?.relay?.owned && priorState.relay.identity) {
        const ownership = await verifyProcessIdentity(priorState.relay.identity);
        if (ownership.ok) {
          const stopped = await stopOwnedRelay(priorState.relay.identity);
          if (!stopped.ok) throw new ValidationError(`Relay is verified alive but unhealthy; controlled restart failed: ${stopped.reason}`);
        } else if (!ownership.stopped) {
          throw new ValidationError(`Relay is unhealthy and prior owned process cannot be safely inspected: ${ownership.reason}`);
        }
      }
      relay = await startRelay(config);
      relay.identity = await captureProcessIdentity(relay.child.pid, relay.expectedIdentity);
      if (!relay.identity) {
        const termination = await terminateProcessTree(relay.child.pid, { wait: true });
        relay.terminatedOnStartup = termination.ok;
        if (!termination.ok) throw new ValidationError(`Relay started, but its process identity could not be captured and PID ${relay.child.pid} survived cleanup: ${termination.reason}`);
        throw new ValidationError("Relay started, but its process identity could not be captured; the new process was terminated.");
      }
      await waitForRelay(config, cancellation);
      relay.child.unref?.();
    }

    adapter = createNativeRuntimeAdapter(config);
    await reconcileRetiredAliases(adapter, config, priorState, journal, cancellation);
    for (const role of config.roles) {
      cancellation.check();
      const prepared = await prepareRuntime(adapter, config, role, journal, cancellation);
      if (!prepared.ready) await waitForRuntimeReadiness(config, [role], adapter, cancellation);
      console.log(`${role.label}: ${prepared.reused ? "reused" : "connected"} ${role.alias} at ${role.path}`);
    }
    cancellation.check();
    const finalRelay = await checkAllRelayEndpoints(config, cancellation.signal);
    if (finalRelay.some((result) => !result.ok)) {
      throw new ValidationError(`Relay readiness failed: ${finalRelay.filter((result) => !result.ok).map((result) => result.role.label).join(", ")}`);
    }
    writeAggregateState(config, { relay: relay || { owned: false }, residualBindings: [] });
    console.log("Relay: all three role endpoints healthy; aggregate startup complete.");
    exitCode = 0;
  } catch (error) {
    cleanupResult = await cleanup();
    if (cancellation.cancelled) {
      console.error(`start:all cancelled by ${cancellation.signalName}`);
      operationError = new ValidationError(`start:all cancelled by ${cancellation.signalName}`);
      exitCode = cancellation.exitCode;
    } else {
      operationError = withCleanupFailures(error, cleanupResult);
    }
  } finally {
    removeSignals();
    const releaseResult = releaseAggregateLock(lockPath);
    operationError = withLockReleaseFailure(operationError, releaseResult);
    if (cancellation.cancelled && releaseResult.ok) operationError = null;
  }
  if (operationError) throw operationError;
  return exitCode;
}

async function runStopAll(config) {
  const lockPath = `${config.stateFile}.lock`;
  acquireAggregateLock(lockPath);
  let operationError = null;
  let exitCode = 0;
  try {
    const stateResult = readAggregateState(config);
    if (stateResult.kind === "malformed" || stateResult.kind === "unsupported version" || stateResult.kind === "read failure") {
      console.error(`stop:all refused to act on ${stateResult.kind} aggregate state: ${stateResult.reason}`);
      exitCode = 1;
    } else {
      const state = stateResult.kind === "valid" ? stateResult.state : null;
      const persistedRoles = state ? rolesFromPersistedState(config, state) : config.roles;
      if (stateResult.kind === "valid" && !persistedRoles) {
        console.error("stop:all refused to act on structurally invalid persisted role bindings.");
        exitCode = 1;
      } else {
        const residualRoles = state ? materializePersistedRoles(config, state.residualBindings) : [];
        const roles = residualRoles.length ? residualRoles : persistedRoles;
        const journal = new Map(roles.map((role) => [journalKey(role), { role, connectMayHaveMutated: true, replacementVerified: true }]));
        for (const role of residualRoles) journal.set(journalKey(role), { role, connectMayHaveMutated: true, replacementVerified: true, residual: true });
        const cleanupResult = await cleanupAggregate(config, {
          journal,
          relay: state?.relay?.owned ? state.relay : null,
          removeState: true,
        });
        reportCleanupFailures(cleanupResult);
        exitCode = cleanupResult.ok ? 0 : 1;
        if (!cleanupResult.ok) operationError = new ValidationError(`cleanup failed: ${cleanupFailureMessages(cleanupResult).join("; ")}`);
      }
    }
  } catch (error) {
    operationError = error;
    exitCode = 1;
  } finally {
    operationError = withLockReleaseFailure(operationError, releaseAggregateLock(lockPath));
  }
  if (operationError) throw operationError;
  return exitCode;
}

async function runStatusAll(config) {
  let adapter = null;
  let availabilityError = null;
  try { adapter = createNativeRuntimeAdapter(config); }
  catch (error) { availabilityError = error instanceof Error ? error.message : String(error); }
  const stateResult = readAggregateState(config);
  const state = stateResult.kind === "valid" ? stateResult.state : null;
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

function journalKey(role) {
  // The retired alias and its replacement share a role key but are distinct
  // runtimes; the journal must never let one identity shadow the other.
  return `${role.key}\u0000${role.alias}`;
}

function journalFor(journal, role) {
  if (!journal.has(journalKey(role))) journal.set(journalKey(role), { role, preExisting: false, stopAttempted: false, stopConfirmed: false, connectAttempted: false, connectMayHaveMutated: false, replacementVerified: false });
  return journal.get(journalKey(role));
}

async function reconcileRetiredAliases(adapter, config, priorState, journal, cancellation) {
  if (!priorState) return;
  const prior = rolesFromPersistedState(config, priorState) || [];
  const desired = new Map(config.roles.map((role) => [role.key, role]));
  for (const oldRole of prior) {
    const replacement = desired.get(oldRole.key);
    if (!replacement || oldRole.alias === replacement.alias) continue;
    // The old binding is owned only because the persisted role key says so; never
    // infer ownership from an alias that merely happens to be in the environment.
    const entry = journalFor(journal, oldRole);
    entry.preExisting = true;
    entry.retired = true;
    entry.stopAttempted = true;
    cancellation.check();
    const stopped = await adapter.stopRuntime(oldRole, cancellation.signal);
    if (!stopped.ok) throw new RuntimeCheckError(oldRole, `retired alias ${oldRole.alias} stop failed: ${stopped.reason}`);
    entry.stopConfirmed = true;
  }
}

async function prepareRuntime(adapter, config, role, journal, cancellation) {
  const current = await inspectRuntime(adapter, role, cancellation.signal);
  if (current.complete) return { ready: true, reused: true };
  if (current.state === "malformed native state" || current.state === "native command failure") {
    throw new RuntimeCheckError(role, current.reason);
  }
  const entry = journalFor(journal, role);
  if (current.found) {
    entry.preExisting = true;
    cancellation.check();
    entry.stopAttempted = true;
    const stopped = await adapter.stopRuntime(role, cancellation.signal);
    if (!stopped.ok) throw new RuntimeCheckError(role, `stop before reconnect failed: ${stopped.reason}`);
    entry.stopConfirmed = true;
  }
  cancellation.check();
  entry.connectAttempted = true;
  // Native connect can create or mutate its alias before returning an error.
  entry.connectMayHaveMutated = true;
  const connected = await adapter.connectRuntime(role, cancellation.signal);
  if (!connected.ok) throw new RuntimeCheckError(role, connected.reason);
  entry.replacementVerified = true;
  return { ready: false, reused: false };
}

async function inspectRuntime(adapter, role, signal) {
  const status = await adapter.getRuntimeStatus(role, signal);
  if (!status.ok) {
    return {
      statusOk: false,
      readyOk: false,
      bindingOk: false,
      found: !status.missing,
      state: status.missing ? "missing alias" : status.kind === "malformed" ? "malformed native state" : "native command failure",
      malformed: status.kind === "malformed",
      failed: status.kind === "command" && !status.missing,
      reason: status.reason,
    };
  }
  if (!status.runtime.processRunning) {
    return { statusOk: true, readyOk: false, bindingOk: runtimeMatchesRole(status.runtime, role), found: true, state: "known alias without process metadata", runtime: status.runtime, reason: "known alias has no active process" };
  }
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
  return {
    statusOk: true,
    readyOk,
    bindingOk,
    complete: status.processRunning && bindingOk && readyOk,
    found: true,
    state: !bindingOk ? "binding drift" : !health.ok ? health.state || "native command failure" : !health.healthy ? "unhealthy runtime" : !health.ready ? "not-ready runtime" : "running runtime",
    malformed: health.kind === "malformed",
    runtime: status.runtime,
    reason,
  };
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
  console.log(`Relay: starting ${redactSecrets(`${file} ${args.join(" ")}`, config.controlPlaneApiKey)}`);
  const child = spawn(file, args, {
    cwd: REPO_ROOT,
    detached: true,
    // The daemon is intentionally detached. Ignored stdio prevents its inherited
    // terminal/test handles from keeping the aggregate launcher alive.
    stdio: ["ignore", "ignore", "ignore"],
    env: process.env,
  });
  await new Promise((resolvePromise, rejectPromise) => {
    child.once("spawn", resolvePromise);
    child.once("error", (error) => rejectPromise(new ValidationError(`Failed to start Relay: ${error.message}`)));
  });
  return { child, owned: true, expectedIdentity: { pid: child.pid, executable: file, args } };
}

async function stopRoles(config, roles, adapter = null) {
  if (!roles.length) return [];
  let runtimeAdapter = adapter;
  let resolutionError = null;
  if (!runtimeAdapter) {
    try { runtimeAdapter = createNativeRuntimeAdapter(config); }
    catch (error) { resolutionError = error instanceof Error ? error.message : String(error); }
  }
  const results = [];
  for (const role of roles) {
    let result;
    if (resolutionError) result = { ok: false, reason: resolutionError };
    else {
      try { result = await runtimeAdapter.stopRuntime(role); }
      catch (error) { result = { ok: false, reason: error instanceof Error ? error.message : String(error) }; }
    }
    const ok = result.ok === true;
    results.push({ role, ok, reason: ok ? "stopped or already stopped" : result.reason });
    console.log(`${role.label}: ${ok ? "stopped or already stopped" : `stop failed: ${result.reason}`}`);
  }
  return results;
}

async function cleanupAggregate(config, { journal = new Map(), relay, removeState = false }) {
  // A failed stop of a pre-existing runtime does not make it cleanup-owned. A
  // confirmed replacement or a connect attempt that may have created state does.
  const ownedEntries = Array.from(journal.values()).filter((entry) => entry.connectMayHaveMutated && (!entry.preExisting || entry.stopConfirmed || entry.replacementVerified));
  const runtimeResults = await stopRoles(config, ownedEntries.map((entry) => entry.role));
  let relayResult = { ok: true, reason: "external Relay preserved" };
  if (relay?.owned && !relay.preserved) {
    if (relay.terminatedOnStartup) {
      relayResult = { ok: true, reason: "new Relay process terminated after identity capture failure" };
    } else if (!relay.identity) {
      relayResult = { ok: false, reason: "Relay ownership state lacks a verifiable process identity; live process preserved" };
    } else {
      try {
        relayResult = await stopOwnedRelay(relay.identity);
      } catch (error) {
        relayResult = { ok: false, reason: error instanceof Error ? error.message : String(error) };
      }
    }
    if (relayResult.ok) console.log("Relay: stopped verified launcher-owned daemon.");
    else console.error(`Relay cleanup failed: ${relayResult.reason}`);
  } else {
    console.log("Relay: external daemon preserved.");
  }
  let stateResult = { ok: true, kind: "preserved", reason: "prior aggregate state preserved" };
  if (removeState) {
    const unresolved = runtimeResults.filter((result) => !result.ok).map((result) => result.role);
    const keepRelay = relay?.owned && !relayResult.ok ? relay : { owned: false };
    if (!unresolved.length && relayResult.ok) stateResult = removeAggregateState(config);
    else {
      try {
        writeAggregateState(config, { desiredRoleBindings: unresolved, residualBindings: unresolved, relay: keepRelay });
        stateResult = { ok: true, kind: "residual", reason: "unconfirmed components retained for stop retry" };
      } catch (error) {
        stateResult = { ok: false, kind: "write failure", reason: error instanceof Error ? error.message : String(error) };
      }
    }
  }
  return {
    runtimeResults,
    relayResult,
    stateResult,
    ok: runtimeResults.every((result) => result.ok) && relayResult.ok && stateResult.ok,
  };
}

function cleanupFailureMessages(result) {
  const failures = result.runtimeResults.filter((item) => !item.ok).map((item) => `runtime ${item.role.alias}: ${item.reason}`);
  if (!result.relayResult.ok) failures.push(`Relay: ${result.relayResult.reason}`);
  if (!result.stateResult.ok) failures.push(`state: ${result.stateResult.reason}`);
  return failures;
}

function reportCleanupFailures(result) {
  const failures = cleanupFailureMessages(result);
  if (failures.length) console.error(`cleanup failed: ${failures.join("; ")}`);
}

function withCleanupFailures(error, result) {
  const failures = cleanupFailureMessages(result);
  if (!failures.length) return error;
  return new ValidationError(`${error instanceof Error ? error.message : String(error)} Cleanup failed: ${failures.join("; ")}`);
}

function withLockReleaseFailure(error, result) {
  if (result.ok) return error;
  const detail = `Aggregate lock release failed: ${result.reason}${result.residuePath ? ` (${result.residuePath})` : ""}`;
  return new ValidationError(error ? `${error instanceof Error ? error.message : String(error)} ${detail}` : detail);
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
      const parsed = parse(result, `connect ${role.alias}`);
      if (!parsed.ok) return parsed;
      const runtime = normalizeRuntimeStatus(parsed.value);
      if (result.code !== 0) return { ok: false, kind: "command", reason: `${summarizeFailure(result)}; structured connect payload was returned`, raw: parsed.raw, diagnostics: parsed.value };
      if (!runtime && parsed.value?.ok !== true && parsed.value?.connected !== true) return { ok: false, kind: "malformed", reason: `connect ${role.alias} returned no structured runtime confirmation`, raw: parsed.raw, diagnostics: parsed.value };
      return { ...parsed, runtime, diagnostics: parsed.value };
    },
    async getRuntimeStatus(role, signal) {
      const result = await invoke(["runtimes", "status", role.alias, "--json"], role, signal);
      const parsed = parse(result, `status ${role.alias}`);
      if (parsed.ok) {
        const runtime = normalizeRuntimeStatus(parsed.value);
        if (!runtime) return { ok: false, kind: "malformed", missing: false, reason: `status ${role.alias} omitted required structured runtime fields` };
        if (result.code !== 0) return { ok: false, kind: "command", missing: false, runtime, reason: `${summarizeFailure(result)}; structured status was returned with nonzero exit` };
        return { ok: true, runtime, processRunning: runtime.processRunning };
      }
      const reason = summarizeFailure(result);
      if (isExactUnknownAliasError(result.stderr, role.alias)) return { ok: false, kind: "missing", missing: true, reason: `alias ${role.alias} is absent` };
      return { ...parsed, kind: result.signal ? "command" : parsed.kind, missing: false, reason: parsed.reason || reason };
    },
    async getRuntimeHealth(role, runtime, signal) {
      let args;
      if (runtime.healthUrlFile) args = ["health", "--url-file", runtime.healthUrlFile, "--json"];
      else if (runtime.healthUrl) args = ["health", "--url", runtime.healthUrl, "--json"];
      else return { ok: false, kind: "malformed", reason: `status ${role.alias} omitted health_url and health_url_file` };
      const result = await invoke(args, role, signal);
      const parsed = parse(result, `health ${role.alias}`);
      if (!parsed.ok) return parsed;
      const health = normalizeRuntimeHealth(parsed.value);
      if (!health) return { ok: false, kind: "malformed", state: "malformed native state", reason: `health ${role.alias} omitted structured healthz/readyz fields` };
      if (result.code === 0 || result.code === 2) return { ok: true, kind: "probe", state: health.healthy ? (health.ready ? "running runtime" : "not-ready runtime") : "unhealthy runtime", ...health };
      return { ok: false, kind: "command", state: "native command failure", reason: `${summarizeFailure(result)}; structured health was returned with unexpected exit ${result.code}` };
    },
    async stopRuntime(role, signal) {
      const result = await invoke(["runtimes", "stop", role.alias, "--json"], role, signal);
      const parsed = parse(result, `stop ${role.alias}`);
      if (parsed.ok) {
        if (parsed.value?.stopped === true) return { ok: true, reason: "stopped", raw: parsed.raw };
        if (parsed.value?.already_stopped === true) return { ok: true, reason: "already stopped", raw: parsed.raw };
        const stopError = typeof parsed.value?.stop_error === "string" ? parsed.value.stop_error : "";
        return { ok: false, reason: stopError || `stop ${role.alias} returned no confirmed stopped state`, raw: parsed.raw };
      }
      if (isExactUnknownAliasError(result.stderr, role.alias)) return { ok: true, reason: "alias absent", raw: result };
      return { ok: false, reason: parsed.reason || summarizeFailure(result), raw: parsed.raw || result };
    },
    async listRuntimes(signal) {
      const result = await invoke(["runtimes", "list", "--json"], null, signal);
      return parse(result, "list runtimes");
    },
  };
}

function parseNativeJsonResult(result, operation) {
  if (result.signal) return { ok: false, kind: "command", reason: `${operation} exited due to signal ${result.signal}`, raw: result };
  const output = result.stdout.trim();
  if (output) {
    try { return { ok: true, value: JSON.parse(output), raw: result }; }
    catch { return { ok: false, kind: "malformed", reason: `${operation} returned malformed JSON`, raw: result }; }
  }
  if (result.code !== 0) return { ok: false, kind: "command", reason: summarizeFailure(result), raw: result };
  return { ok: false, kind: "malformed", reason: `${operation} returned empty JSON output`, raw: result };
}

const UNKNOWN_ALIAS_ERROR = (alias) => `alias ${alias} is not known; run create or connect first`;

function isExactUnknownAliasError(stderr, alias) {
  return String(stderr || "").trim() === UNKNOWN_ALIAS_ERROR(alias);
}

function normalizeRuntimeStatus(payload) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return null;
  const processState = payload.process;
  const alias = payload.alias;
  const profile = payload.profile_name;
  const tunnelId = payload.tunnel_id;
  const endpoint = processState?.target_value;
  const healthUrl = payload.health_url ?? null;
  const healthUrlFile = payload.health_url_file ?? null;
  const processRunning = payload.process_running;
  if (typeof alias !== "string" || typeof tunnelId !== "string" || typeof profile !== "string") return null;
  if (processState !== null && (typeof processState !== "object" || Array.isArray(processState))) return null;
  if (processState !== null && (processState.target_kind !== "server_url" || !isHttpUrl(endpoint))) return null;
  if (healthUrl !== null && typeof healthUrl !== "string") return null;
  if (healthUrlFile !== null && typeof healthUrlFile !== "string") return null;
  if (typeof processRunning !== "boolean") return null;
  return {
    alias,
    profile,
    tunnelId,
    endpoint: processState ? endpoint : null,
    processRunning,
    pid: processState?.pid ?? null,
    healthUrl: typeof healthUrl === "string" ? healthUrl : null,
    healthUrlFile: typeof healthUrlFile === "string" ? healthUrlFile : null,
    raw: payload,
  };
}

function isHttpUrl(value) {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

function normalizeRuntimeHealth(payload) {
  if (!payload || typeof payload !== "object" || !payload.healthz || !payload.readyz || (payload.result !== "ok" && payload.result !== "fail")) return null;
  if (typeof payload.healthz.ok !== "boolean" || typeof payload.readyz.ok !== "boolean") return null;
  return { healthy: payload.healthz.ok, ready: payload.readyz.ok, result: payload.result, raw: payload };
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
    if (signal?.aborted) {
      rejectPromise(new ValidationError("aggregate operation cancelled before command start"));
      return;
    }
    const child = spawn(command, args, { cwd: REPO_ROOT, stdio: ["ignore", "pipe", "pipe"], env });
    let settled = false;
    const abort = () => {
      if (!settled) {
        try { child.kill(); } catch { /* the close event remains authoritative */ }
      }
    };
    signal?.addEventListener("abort", abort, { once: true });
    const stdoutSink = createRedactedSink(secret, () => {});
    const stderrSink = createRedactedSink(secret, () => {});
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
      const stdout = stdoutSink.finish();
      const stderr = stderrSink.finish();
      resolvePromise({ code: code ?? 1, signal: childSignal, stdout, stderr, json: parseCapturedJson(stdout), jsonError: parseCapturedJsonError(stdout) });
    });
  });
}

function parseCapturedJson(output) {
  const text = String(output || "").trim();
  if (!text) return null;
  try { return JSON.parse(text); } catch { return null; }
}

function parseCapturedJsonError(output) {
  const text = String(output || "").trim();
  if (!text) return null;
  try { JSON.parse(text); return null; } catch (error) { return error instanceof Error ? error.message : String(error); }
}

function createRedactedSink(secret, write) {
  let pending = "";
  let output = "";
  const emit = (safe) => {
    if (!safe) return;
    output += safe;
    write(safe);
  };
  const drain = (text) => {
    if (!secret) return { safe: text, rest: "" };
    // Redact every complete occurrence first so that a self-overlapping secret
    // (for example "aba" or "aaaa") is recognized before any suffix of it is
    // retained as a potential prefix of a later occurrence.
    let safe = "";
    let index = text.indexOf(secret);
    while (index !== -1) {
      safe += `${text.slice(0, index)}[REDACTED]`;
      text = text.slice(index + secret.length);
      index = text.indexOf(secret);
    }
    let keep = 0;
    for (let length = Math.min(secret.length - 1, text.length); length > 0; length -= 1) {
      if (text.endsWith(secret.slice(0, length))) { keep = length; break; }
    }
    safe += text.slice(0, text.length - keep);
    return { safe, rest: text.slice(text.length - keep) };
  };
  return {
    push(chunk) {
      const { safe, rest } = drain(pending + String(chunk));
      pending = rest;
      emit(safe);
    },
    finish() {
      emit(redactSecrets(pending, secret));
      pending = "";
      return output;
    },
    get output() { return output + redactSecrets(pending, secret); },
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
  const stderr = String(result.stderr || "").replace(/\s+/gu, " ").trim();
  if (stderr) return stderr.slice(0, 240);
  if (result.json !== null && result.json !== undefined) return `exit ${result.code} with structured JSON diagnostics`;
  if (String(result.stdout || "").trim()) return `exit ${result.code} with non-JSON native output`;
  return `exit ${result.code}`;
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

const aggregateLockOwners = new Map();

const defaultAggregateLockFs = { mkdirSync, openSync, writeFileSync, fsyncSync, closeSync, renameSync, readFileSync, unlinkSync, rmdirSync, statSync };

const IDENTITY_COMMAND_TIMEOUT_MS = 5000;

function normalizeProcessStartIdentity(value) {
  return String(value ?? "").trim().replace(/\s+/gu, " ") || null;
}

function currentProcessStartIdentity(pid = process.pid, platform = process.platform, options = {}) {
  const runExecFileSync = options.execFileSync || execFileSync;
  try {
    if (platform === "linux") return parseLinuxProcStatStartIdentity(readFileSync(`/proc/${Number(pid)}/stat`, "utf8"));
    if (platform === "darwin") return normalizeProcessStartIdentity(runExecFileSync("ps", ["-ww", "-p", String(pid), "-o", "lstart="], { encoding: "utf8", timeout: IDENTITY_COMMAND_TIMEOUT_MS, killSignal: "SIGKILL", env: { ...process.env, LC_ALL: "C" } }));
    if (platform === "win32") {
      const script = "$p=Get-CimInstance Win32_Process -Filter 'ProcessId = " + Number(pid) + "'; if ($p) { $p.CreationDate }";
      return normalizeProcessStartIdentity(runExecFileSync("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", script], { encoding: "utf8", timeout: IDENTITY_COMMAND_TIMEOUT_MS, killSignal: "SIGKILL" }));
    }
  } catch { /* inspection failure is deliberately not proof of ownership */ }
  return null;
}

function aggregateLockMetadataPath(lockPath) {
  return join(lockPath, "owner.json");
}

function aggregateRecoveryGatePath(lockPath) {
  return `${lockPath}-recovery`;
}

function aggregateLockAmbiguousError(lockPath, reason) {
  return new ValidationError(`Aggregate lock ${lockPath} ownership metadata could not be verified (${reason}); automatic removal was refused. Confirm no aggregate operation is running, then remove the lock manually only after independent confirmation.`);
}

function readAggregateLock(lockPath, fs = defaultAggregateLockFs) {
  let lockStat;
  try { lockStat = fs.statSync(lockPath); }
  catch (error) {
    if (error?.code === "ENOENT") return { kind: "absent" };
    return { kind: "ambiguous", reason: "the lock path could not be inspected" };
  }
  const directory = lockStat.isDirectory();
  const metadataPath = directory ? aggregateLockMetadataPath(lockPath) : lockPath;
  let text;
  try { text = fs.readFileSync(metadataPath, "utf8"); }
  catch { return { kind: "ambiguous", reason: "the owner record could not be read" }; }
  let owner;
  try { owner = JSON.parse(text); }
  catch { return { kind: "ambiguous", reason: "the owner record is malformed" }; }
  if (!owner || typeof owner !== "object" || Array.isArray(owner)) return { kind: "ambiguous", reason: "the owner record is not an object" };
  if (directory && owner.version !== 1) return { kind: "ambiguous", reason: "the owner record version is unsupported" };
  if (!Number.isInteger(owner.pid) || owner.pid <= 0) return { kind: "ambiguous", reason: "the owner record has no valid PID" };
  if (typeof owner.startIdentity !== "string" || !normalizeProcessStartIdentity(owner.startIdentity)) return { kind: "ambiguous", reason: "the owner record has no valid start identity" };
  if (typeof owner.ownerToken !== "string" || !owner.ownerToken) return { kind: "ambiguous", reason: "the owner record has no valid owner token" };
  return { kind: "recorded", owner, lockStat, directory };
}

function sameAggregateLockInstance(left, right) {
  return left?.dev === right?.dev && left?.ino === right?.ino && left?.birthtimeMs === right?.birthtimeMs;
}

function writeAggregateLockOwner(lockPath, owner, fs = defaultAggregateLockFs) {
  const metadataPath = aggregateLockMetadataPath(lockPath);
  const temporary = `${metadataPath}.${randomUUID()}.tmp`;
  let descriptor = null;
  try {
    descriptor = fs.openSync(temporary, "wx", 0o600);
    fs.writeFileSync(descriptor, `${JSON.stringify(owner)}\n`, "utf8");
    fs.fsyncSync(descriptor);
    fs.closeSync(descriptor);
    descriptor = null;
    fs.renameSync(temporary, metadataPath);
  } finally {
    if (descriptor !== null) fs.closeSync(descriptor);
    try { fs.unlinkSync(temporary); } catch (error) { if (error?.code !== "ENOENT") throw error; }
  }
}

function removeAggregateLockInstance(lockPath, lockInfo, fs = defaultAggregateLockFs) {
  let currentStat;
  try { currentStat = fs.statSync(lockPath); }
  catch (error) { if (error?.code === "ENOENT") return false; throw error; }
  if (!sameAggregateLockInstance(lockInfo.lockStat, currentStat)) throw new ValidationError(`Aggregate lock ${lockPath} changed during stale-owner recovery; refusing to remove a replacement lock.`);
  if (lockInfo.directory) {
    const current = readAggregateLock(lockPath, fs);
    if (current.kind !== "recorded" || !sameAggregateLockInstance(lockInfo.lockStat, current.lockStat) || current.owner.ownerToken !== lockInfo.owner.ownerToken) {
      throw new ValidationError(`Aggregate lock ${lockPath} changed during stale-owner recovery; refusing to remove a replacement lock.`);
    }
    fs.unlinkSync(aggregateLockMetadataPath(lockPath));
    fs.rmdirSync(lockPath);
  } else {
    fs.unlinkSync(lockPath);
  }
  return true;
}

function releaseDirectory(lockPath, owner, fs = defaultAggregateLockFs, label = "aggregate lock") {
  const absent = { ok: true, released: true, ownerRecordRemoved: false, directoryRemoved: true, lockPreserved: false, reason: `${label} was already absent` };
  let lockStat;
  try { lockStat = fs.statSync(lockPath); }
  catch (error) { if (error?.code === "ENOENT") return absent; return { ok: false, released: false, ownerRecordRemoved: false, directoryRemoved: false, lockPreserved: true, reason: `${label} could not be inspected: ${error.message}` }; }
  if (!sameAggregateLockInstance(owner.lockStat, lockStat)) return { ok: true, released: false, ownerRecordRemoved: false, directoryRemoved: false, lockPreserved: true, reason: `${label} instance changed; replacement preserved` };
  const current = readAggregateLock(lockPath, fs);
  if (current.kind !== "recorded" || !current.directory || current.owner.ownerToken !== owner.ownerToken || !sameAggregateLockInstance(owner.lockStat, current.lockStat)) {
    return { ok: true, released: false, ownerRecordRemoved: false, directoryRemoved: false, lockPreserved: true, reason: `${label} owner token or instance no longer matches; replacement preserved` };
  }
  const privatePath = `${lockPath}.release-${owner.ownerToken}`;
  try {
    fs.renameSync(lockPath, privatePath);
  } catch (error) {
    return { ok: false, released: false, ownerRecordRemoved: false, directoryRemoved: false, lockPreserved: true, reason: `${label} public-path rename failed: ${error.message}` };
  }
  const privateInfo = readAggregateLock(privatePath, fs);
  if (privateInfo.kind !== "recorded" || privateInfo.owner.ownerToken !== owner.ownerToken) {
    return { ok: false, released: true, ownerRecordRemoved: false, directoryRemoved: false, lockPreserved: false, residuePath: privatePath, reason: `${label} private release residue did not retain the expected owner token` };
  }
  try {
    fs.unlinkSync(aggregateLockMetadataPath(privatePath));
  } catch (error) {
    return { ok: false, released: true, ownerRecordRemoved: false, directoryRemoved: false, lockPreserved: false, residuePath: privatePath, reason: `${label} owner-record removal failed at private release path: ${error.message}` };
  }
  try {
    fs.rmdirSync(privatePath);
    return { ok: true, released: true, ownerRecordRemoved: true, directoryRemoved: true, lockPreserved: false, reason: `${label} released` };
  } catch (error) {
    return { ok: false, released: true, ownerRecordRemoved: true, directoryRemoved: false, lockPreserved: false, residuePath: privatePath, reason: `${label} private cleanup-directory removal failed: ${error.message}` };
  }
}

function acquireRecoveryGate(lockPath, owner, fs = defaultAggregateLockFs) {
  const gatePath = aggregateRecoveryGatePath(lockPath);
  try {
    fs.mkdirSync(gatePath);
    try {
      writeAggregateLockOwner(gatePath, owner, fs);
    } catch (error) {
      try { fs.rmdirSync(gatePath); } catch { /* preserve an ambiguous gate for safe operator recovery */ }
      throw error;
    }
    return { gatePath, owner, lockStat: fs.statSync(gatePath) };
  } catch (error) {
    if (error?.code !== "EEXIST") throw error;
    const gate = readAggregateLock(gatePath, fs);
    if (gate.kind === "ambiguous") throw aggregateLockAmbiguousError(gatePath, `recovery gate metadata is ambiguous: ${gate.reason}`);
    if (gate.kind === "absent") throw new ValidationError(`Aggregate recovery gate ${gatePath} changed while it was being inspected; refusing unsafe acquisition.`);
    throw new ValidationError(`Aggregate recovery gate ${gatePath} is active; refusing concurrent acquisition.`);
  }
}

function releaseRecoveryGate(gate, fs = defaultAggregateLockFs) {
  return releaseDirectory(gate.gatePath, { ...gate.owner, lockStat: gate.lockStat }, fs, "aggregate recovery gate");
}

function acquireAggregateLock(lockPath, options = {}) {
  const inspectStartIdentity = options.inspectStartIdentity || currentProcessStartIdentity;
  mkdirSync(dirname(lockPath), { recursive: true });
  const startIdentity = normalizeProcessStartIdentity(inspectStartIdentity(process.pid));
  if (!startIdentity) throw new ValidationError("Aggregate lock unavailable: this process's start identity could not be established, so duplicate-operation protection cannot be guaranteed.");
  const owner = { version: 1, pid: process.pid, startIdentity, ownerToken: randomUUID() };
  const fs = { ...defaultAggregateLockFs, ...(options.fs || {}) };
  const gate = acquireRecoveryGate(lockPath, owner, fs);
  let acquired = null;
  let acquisitionError = null;
  const maxRetries = Number.isInteger(options.maxRetries) && options.maxRetries >= 0 ? options.maxRetries : 3;
  try {
    options.onRecoveryAuthorityAcquired?.({ gatePath: gate.gatePath, lockPath });
    for (let attempt = 0; attempt <= maxRetries; attempt += 1) {
      try {
        fs.mkdirSync(lockPath);
        try {
          writeAggregateLockOwner(lockPath, owner, fs);
        } catch (error) {
          try { fs.rmdirSync(lockPath); } catch { /* leave an ambiguous lock for safe operator recovery */ }
          throw error;
        }
        const record = { ...owner, lockStat: fs.statSync(lockPath) };
        aggregateLockOwners.set(lockPath, record);
        acquired = record;
        break;
      } catch (error) {
        if (error?.code !== "EEXIST") throw error;
        const lockInfo = readAggregateLock(lockPath, fs);
        if (lockInfo.kind === "absent") continue;
        if (lockInfo.kind === "ambiguous") throw aggregateLockAmbiguousError(lockPath, lockInfo.reason);
        if (isProcessAlive(lockInfo.owner.pid)) {
          const observedStart = inspectStartIdentity(lockInfo.owner.pid);
          if (!observedStart) throw aggregateLockAmbiguousError(lockPath, "the live owner's start identity could not be inspected");
          if (normalizeProcessStartIdentity(lockInfo.owner.startIdentity) === normalizeProcessStartIdentity(observedStart)) throw new ValidationError(`Aggregate startup is already running; lock ${lockPath} is held by a live owner.`);
        }
        if (attempt === maxRetries) throw new ValidationError(`Aggregate lock ${lockPath} remained contested after ${maxRetries + 1} bounded acquisition attempts; refusing unsafe recovery.`);
        options.beforeStaleRemoval?.({ lockPath, owner: lockInfo.owner });
        removeAggregateLockInstance(lockPath, lockInfo, fs);
      }
    }
  } catch (error) {
    acquisitionError = error;
  }
  const gateRelease = releaseRecoveryGate(gate, fs);
  if (!gateRelease.ok) {
    const primaryRelease = acquired ? releaseAggregateLock(lockPath, acquired.ownerToken, { fs }) : { ok: true, reason: "primary lock was not acquired" };
    const details = `Aggregate recovery gate release failed: ${gateRelease.reason}${gateRelease.residuePath ? ` (${gateRelease.residuePath})` : ""}`;
    const primaryDetails = !primaryRelease.ok ? ` Primary lock release also failed: ${primaryRelease.reason}` : "";
    throw new ValidationError(`${acquisitionError?.message || "Aggregate lock acquisition failed."} ${details}.${primaryDetails}`);
  }
  if (acquisitionError) throw acquisitionError;
  if (!acquired) throw new ValidationError(`Aggregate lock ${lockPath} could not be acquired safely.`);
  return acquired;
}

function releaseAggregateLock(lockPath, ownerToken = aggregateLockOwners.get(lockPath)?.ownerToken, options = {}) {
  const owner = aggregateLockOwners.get(lockPath);
  if (!owner || !ownerToken || owner.ownerToken !== ownerToken) return { ok: true, released: false, ownerRecordRemoved: false, directoryRemoved: false, lockPreserved: true, reason: "not the current in-memory lock owner" };
  const fs = { ...defaultAggregateLockFs, ...(options.fs || {}) };
  const result = releaseDirectory(lockPath, owner, fs);
  if (result.released && result.directoryRemoved) {
    aggregateLockOwners.delete(lockPath);
  }
  return result;
}

function readAggregateState(config) {
  let text;
  try { text = readFileSync(config.stateFile, "utf8"); }
  catch (error) {
    if (error?.code === "ENOENT") return { kind: "absent", state: null, reason: "aggregate state is absent" };
    return { kind: "read failure", state: null, reason: error instanceof Error ? error.message : String(error) };
  }
  let state;
  try { state = JSON.parse(text); }
  catch (error) { return { kind: "malformed", state: null, reason: error instanceof Error ? error.message : "invalid JSON" }; }
  if (!state || typeof state !== "object" || Array.isArray(state)) return { kind: "malformed", state: null, reason: "aggregate state is not an object" };
  if (JSON.stringify(state).match(/CONTROL_PLANE_API_KEY|sk[-_][A-Za-z0-9_-]{8,}/u)) return { kind: "malformed", state: null, reason: "aggregate state contains secret-like data" };
  if (state.version === 2) {
    const migrated = migrateAggregateStateVersion2(state);
    if (!migrated) return { kind: "malformed", state: null, reason: "version 2 aggregate state is structurally invalid and cannot be migrated" };
    const migratedValidation = validatePersistedState(migrated);
    if (!migratedValidation.ok) return { kind: "malformed", state: null, reason: `version 2 aggregate state cannot be migrated: ${migratedValidation.reason}` };
    return { kind: "valid", state: migrated, migrated: true, reason: "version 2 aggregate state migrated to version 3" };
  }
  if (state.version !== 3) return { kind: "unsupported version", state: null, reason: `expected version 3, found ${String(state.version)}` };
  const validation = validatePersistedState(state);
  return validation.ok ? { kind: "valid", state, reason: "aggregate state is valid" } : { kind: "malformed", state: null, reason: validation.reason };
}

function migrateAggregateStateVersion2(state) {
  if (!Array.isArray(state.desiredRoleBindings) || !(state.relay && typeof state.relay === "object")) return null;
  // Version 2 tracked runtimesChangedByOperation only for successful writes, so
  // a readable version 2 state never carries residual cleanup work.
  return {
    version: 3,
    updatedAt: typeof state.updatedAt === "string" ? state.updatedAt : new Date().toISOString(),
    desiredRoleBindings: state.desiredRoleBindings,
    residualBindings: [],
    relay: state.relay,
  };
}

const MAX_PERSISTED_BINDING_FIELD_LENGTH = 1024;

function validateBindingList(bindings, label) {
  if (!Array.isArray(bindings) || bindings.length > 3) return `${label} must contain at most three roles`;
  const expected = new Set(["wayfinder", "planner", "auditor"]);
  const keys = new Set();
  const aliases = new Set();
  const bounded = (value) => typeof value === "string" && value.length > 0 && value.length <= MAX_PERSISTED_BINDING_FIELD_LENGTH;
  for (const binding of bindings) {
    if (!binding || typeof binding !== "object" || Array.isArray(binding) || !expected.has(binding.key) || keys.has(binding.key)) return `${label} has invalid or duplicate role keys`;
    if (!bounded(binding.alias) || aliases.has(binding.alias) || !bounded(binding.profile) || !bounded(binding.tunnelId) || !bounded(binding.endpoint) || !isHttpUrl(binding.endpoint)) return `${label} contains invalid bindings`;
    if (JSON.stringify(binding).match(/CONTROL_PLANE_API_KEY|sk[-_][A-Za-z0-9_-]{8,}/u)) return `${label} contains secret-like data`;
    keys.add(binding.key); aliases.add(binding.alias);
  }
  return null;
}

function validatePersistedState(state) {
  const desiredReason = validateBindingList(state.desiredRoleBindings, "desiredRoleBindings");
  if (desiredReason) return { ok: false, reason: desiredReason };
  const residualReason = validateBindingList(state.residualBindings, "residualBindings");
  if (residualReason) return { ok: false, reason: residualReason };
  if (!(state.relay && typeof state.relay === "object") || typeof state.relay.owned !== "boolean") return { ok: false, reason: "relay ownership metadata is invalid" };
  if (state.relay.owned && (!state.relay.identity || typeof state.relay.identity !== "object")) return { ok: false, reason: "owned Relay state lacks identity metadata" };
  // A Relay-only residual state is valid: every runtime stopped, but the owned
  // Relay shutdown still needs a retry.
  if (!state.desiredRoleBindings.length && !state.residualBindings.length && state.relay.owned !== true) return { ok: false, reason: "desiredRoleBindings must contain one to three roles unless owned Relay residual state remains" };
  return { ok: true };
}

function materializePersistedRoles(config, bindings) {
  const byKey = new Map(config.roles.map((role) => [role.key, role]));
  return bindings.map((binding) => {
    const definition = byKey.get(binding.key);
    return { ...definition, tunnelId: binding.tunnelId, alias: binding.alias, profile: binding.profile, endpoint: binding.endpoint };
  });
}

function rolesFromPersistedState(config, state) {
  if (validatePersistedState(state).ok === false) return null;
  return materializePersistedRoles(config, state.desiredRoleBindings);
}

function writeAggregateState(config, state) {
  mkdirSync(dirname(config.stateFile), { recursive: true });
  const payload = {
    version: 3,
    updatedAt: new Date().toISOString(),
    desiredRoleBindings: (state.desiredRoleBindings || config.roles).map((role) => ({ key: role.key, tunnelId: role.tunnelId, alias: role.alias, profile: role.profile, endpoint: role.endpoint })),
    residualBindings: (state.residualBindings || []).map((role) => ({ key: role.key, tunnelId: role.tunnelId, alias: role.alias, profile: role.profile, endpoint: role.endpoint })),
    relay: state.relay?.owned ? { owned: true, identity: redactIdentity(state.relay.identity, config.controlPlaneApiKey) } : { owned: false },
  };
  const temporary = `${config.stateFile}.${process.pid}.${randomUUID()}.tmp`;
  let descriptor = null;
  try {
    descriptor = openSync(temporary, "wx", 0o600);
    writeFileSync(descriptor, `${JSON.stringify(payload, null, 2)}\n`, "utf8");
    fsyncSync(descriptor);
    closeSync(descriptor);
    descriptor = null;
    renameSync(temporary, config.stateFile);
  } catch (error) {
    if (descriptor !== null) { try { closeSync(descriptor); } catch { /* best effort */ } }
    throw new ValidationError(`aggregate state write failure: ${error instanceof Error ? error.message : String(error)}`);
  } finally {
    try { if (existsSync(temporary)) unlinkSync(temporary); } catch (error) { if (error?.code !== "ENOENT") console.error(`aggregate state temporary cleanup failed: ${error.message}`); }
  }
}

function redactIdentity(identity, secret) {
  return sanitizePersistedValue(identity, secret);
}

function sanitizePersistedValue(value, secret, depth = 0) {
  if (depth > 12) return "[TRUNCATED]";
  if (typeof value === "string") return redactSecrets(value, secret).slice(0, 1024);
  if (Array.isArray(value)) return value.slice(0, 128).map((item) => sanitizePersistedValue(item, secret, depth + 1));
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).slice(0, 128).map(([key, item]) => [key, sanitizePersistedValue(item, secret, depth + 1)]));
  return value;
}

function removeAggregateState(config) {
  try {
    unlinkSync(config.stateFile);
    return { ok: true, kind: "absent", reason: "aggregate state removed" };
  } catch (error) {
    if (error?.code === "ENOENT") return { ok: true, kind: "absent", reason: "aggregate state already absent" };
    return { ok: false, kind: "remove failure", reason: error instanceof Error ? error.message : String(error) };
  }
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
  if (!observed || !observed.startIdentity || !observed.executablePath || !observed.commandLine) return null;
  return {
    pid: Number(pid),
    startIdentity: boundText(observed.startIdentity),
    executablePath: boundText(observed.executablePath || expected.executable),
    commandLine: boundText(observed.commandLine),
    commandFingerprint: fingerprintCommandLine(observed.commandLine),
    expectedExecutable: boundText(expected.executable),
    expectedArguments: expected.args.map((argument) => boundText(argument)),
    launchToken: randomUUID(),
  };
}

async function inspectProcessIdentity(pid) {
  if (!isProcessAlive(pid)) return null;
  if (process.platform === "linux") {
    try {
      const stat = readFileSync(`/proc/${Number(pid)}/stat`, "utf8");
      const commandLine = readFileSync(`/proc/${Number(pid)}/cmdline`, "utf8");
      const executablePath = readlinkSync(`/proc/${Number(pid)}/exe`);
      return {
        startIdentity: parseLinuxProcStatStartIdentity(stat),
        executablePath,
        commandLine: commandLine.replace(/\0/gu, " ").trim(),
      };
    } catch { return null; }
  }
  if (process.platform === "darwin") {
    const [pidOutput, startOutput, executableOutput, argsOutput] = await Promise.all([
      captureSimpleCommand("ps", ["-ww", "-p", String(pid), "-o", "pid="], { LC_ALL: "C" }),
      captureSimpleCommand("ps", ["-ww", "-p", String(pid), "-o", "lstart="], { LC_ALL: "C" }),
      captureSimpleCommand("ps", ["-ww", "-p", String(pid), "-o", "comm="], { LC_ALL: "C" }),
      captureSimpleCommand("ps", ["-ww", "-p", String(pid), "-o", "args="], { LC_ALL: "C" }),
    ]);
    return parseMacPsOutput({ pid: pidOutput, startIdentity: startOutput, executablePath: executableOutput, commandLine: argsOutput }, pid);
  }
  const script = "$p=Get-CimInstance Win32_Process -Filter 'ProcessId = " + Number(pid) + "'; if ($p) { $p | Select-Object ProcessId,CreationDate,ExecutablePath,CommandLine | ConvertTo-Json -Compress }";
  const output = await captureSimpleCommand("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", script]);
  return parseWindowsCimJson(output, pid);
}

function parseLinuxProcStatStartIdentity(stat) {
  const close = stat.lastIndexOf(")");
  if (close < 0 || stat.slice(close + 1, close + 2) !== " ") return null;
  const fields = stat.slice(close + 2).trim().split(/\s+/u);
  return fields[19] || null;
}

function parseMacPsOutput(output, expectedPid = null) {
  if (output && typeof output === "object" && !Array.isArray(output)) {
    const pid = Number(String(output.pid || "").trim());
    if (!Number.isInteger(pid) || (expectedPid !== null && pid !== Number(expectedPid))) return null;
    const value = (item) => String(item || "").trim();
    const startIdentity = normalizeProcessStartIdentity(output.startIdentity);
    const executablePath = value(output.executablePath);
    const commandLine = value(output.commandLine);
    if (!startIdentity || !executablePath || !commandLine) return null;
    return { startIdentity, executablePath, commandLine };
  }
  // Compatibility for callers using the old fixture shape; production capture uses
  // the separate queries above so spaces in comm and args are unambiguous.
  const line = String(output).trim();
  const match = line.match(/^(\d+)\s+(.{24})\s+(\S+)\s+(.+)$/u);
  if (!match || (expectedPid !== null && Number(match[1]) !== Number(expectedPid))) return null;
  return { startIdentity: normalizeProcessStartIdentity(match[2]), executablePath: match[3], commandLine: match[4].trim() };
}

function parseWindowsCimJson(output, expectedPid = null) {
  if (!String(output).trim()) return null;
  try {
    const value = JSON.parse(output);
    if (!value || (expectedPid !== null && Number(value.ProcessId) !== Number(expectedPid))) return null;
    return {
      startIdentity: value.CreationDate || null,
      executablePath: value.ExecutablePath || null,
      commandLine: value.CommandLine || null,
    };
  } catch { return null; }
}

async function verifyProcessIdentity(identity) {
  if (!identity?.pid) return { ok: false, reason: "no recorded Relay process identity" };
  if (!isProcessAlive(identity.pid)) return { ok: false, stopped: true, reason: "recorded process is no longer alive" };
  const observed = await inspectProcessIdentity(identity.pid);
  if (!observed) return { ok: false, reason: "live process identity could not be inspected" };
  const expectedStart = identity.startIdentity ?? identity.startTime;
  if (!expectedStart || !observed.startIdentity || String(expectedStart) !== String(observed.startIdentity)) return { ok: false, reason: "process start identity does not match" };
  const expectedExecutable = normalizeIdentityPath(identity.executablePath || identity.expectedExecutable || identity.executable);
  const observedExecutable = normalizeIdentityPath(observed.executablePath);
  if (!expectedExecutable || !observedExecutable || !sameExecutable(expectedExecutable, observedExecutable)) return { ok: false, reason: "executable identity does not match" };
  const observedCommand = String(observed.commandLine || "");
  if (!observedCommand) return { ok: false, reason: "command line identity could not be inspected" };
  const fingerprintMatches = identity.commandFingerprint && fingerprintCommandLine(observedCommand) === identity.commandFingerprint;
  const argumentsMatch = Array.isArray(identity.expectedArguments) && identity.expectedArguments.length > 0 && identity.expectedArguments.every((argument) => observedCommand.replace(/\\/gu, "/").includes(String(argument).replace(/\\/gu, "/")));
  if (!fingerprintMatches && !argumentsMatch) return { ok: false, reason: "command fingerprint or expected command arguments do not match" };
  return { ok: true, stopped: false };
}

function normalizeIdentityPath(value) {
  return String(value || "").replace(/\\/gu, "/").toLowerCase();
}

function sameExecutable(expected, observed) {
  if (expected === observed) return true;
  return expected.split("/").pop().replace(/\.exe$/u, "") === observed.split("/").pop().replace(/\.exe$/u, "");
}

function boundText(value) {
  return String(value ?? "").slice(0, 1024);
}

function captureSimpleCommand(command, args, environment = null) {
  return new Promise((resolvePromise) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "ignore"], env: environment ? { ...process.env, ...environment } : process.env, timeout: IDENTITY_COMMAND_TIMEOUT_MS, killSignal: "SIGKILL" });
    const chunks = [];
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => chunks.push(chunk));
    child.on("error", () => resolvePromise(""));
    child.on("close", () => resolvePromise(chunks.join("")));
  });
}

async function stopOwnedRelay(identity, options = {}) {
  const ownership = await verifyProcessIdentity(identity);
  if (!ownership.ok) {
    if (ownership.stopped) return { ok: true, pid: identity.pid, stopped: true, reason: "recorded Relay process already exited" };
    console.error(`Relay ownership could not be confirmed; preserving live process: ${ownership.reason}`);
    return ownership;
  }
  if (!ownership.stopped) {
    const termination = await terminateProcessTree(identity.pid, { wait: true, terminationAdapter: options.terminationAdapter });
    if (!termination.ok) return { ...termination, ok: false, reason: `Relay process termination could not be confirmed: ${termination.reason}` };
  }
  return { ok: true, pid: identity.pid, stopped: true, reason: "Relay process stopped" };
}

async function terminateProcessTree(pid, { wait = false, terminationAdapter = null } = {}) {
  const result = { ok: false, pid: Number(pid), signalAttempted: null, escalated: false, exited: false, reason: "" };
  if (!pid || !isProcessAlive(pid)) return { ...result, ok: true, exited: true, reason: "process already exited" };
  if (terminationAdapter) {
    const injected = await terminationAdapter(Number(pid), { wait });
    return { ...result, ...injected, pid: Number(pid), ok: injected?.ok === true, exited: injected?.exited === true, reason: injected?.reason || "injected termination result" };
  }
  const shutdown = buildProcessShutdownPlan(pid, process.platform);
  result.signalAttempted = process.platform === "win32" ? "taskkill" : "SIGTERM";
  if (shutdown.command) {
    const commandResult = await runSimpleCommand(shutdown.command, shutdown.args);
    if (!commandResult.ok) { result.reason = commandResult.reason || "taskkill failed"; return result; }
  } else {
    try { process.kill(-Number(pid), "SIGTERM"); }
    catch { try { process.kill(Number(pid), "SIGTERM"); } catch { /* process may have exited */ } }
  }
  if (!wait) return { ...result, ok: true, reason: "termination signal sent" };
  result.exited = await waitForProcessExit(pid);
  if (!result.exited && process.platform !== "win32") {
    result.escalated = true;
    result.signalAttempted = "SIGKILL";
    try { process.kill(-Number(pid), "SIGKILL"); } catch { try { process.kill(Number(pid), "SIGKILL"); } catch { /* already stopped */ } }
    result.exited = await waitForProcessExit(pid, 1000);
  }
  result.ok = result.exited;
  result.reason = result.exited ? "process exited" : "process remains alive after termination attempts";
  return result;
}

async function waitForProcessExit(pid, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (isProcessAlive(pid) && Date.now() < deadline) await delay(25);
  return !isProcessAlive(pid);
}

function buildProcessShutdownPlan(pid, platform = process.platform) {
  if (platform === "win32") return { command: "taskkill", args: ["/PID", String(pid), "/T", "/F"] };
  return { command: null, args: [-Number(pid), "SIGTERM"] };
}

function runSimpleCommand(command, args) {
  return new Promise((resolvePromise) => {
    const child = spawn(command, args, { stdio: "ignore" });
    child.on("error", (error) => resolvePromise({ ok: false, reason: error.message }));
    child.on("close", (code, signal) => resolvePromise({ ok: (code ?? 1) === 0 && !signal, code, signal, reason: (code ?? 1) === 0 ? "ok" : `exit ${code ?? 1}` }));
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
  captureProcessIdentity,
  getConfig,
  loadEnvFile,
  acquireAggregateLock,
  createRedactedSink,
  currentProcessStartIdentity,
  releaseAggregateLock,
  normalizeRelayMcpProfile,
  normalizeRuntimeHealth,
  normalizeRuntimeStatus,
  parseLinuxProcStatStartIdentity,
  parseMacPsOutput,
  parseNativeJsonResult,
  parseWindowsCimJson,
  redactSecrets,
  sanitizePersistedValue,
  runCommandCapture,
  stopOwnedRelay,
  inspectProcessIdentity,
  terminateProcessTree,
  verifyProcessIdentity,
  validateAggregateConfig,
};

if (process.argv[1] && resolve(process.argv[1]) === SCRIPT_PATH) main();
