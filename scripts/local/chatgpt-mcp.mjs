#!/usr/bin/env node

import { spawn } from 'child_process';
import { accessSync, constants, existsSync, readFileSync } from 'fs';
import { request as httpRequest } from 'http';
import { request as httpsRequest } from 'https';
import { dirname, delimiter, isAbsolute, join, resolve } from 'path';
import process from 'process';
import { fileURLToPath } from 'url';

const SCRIPT_PATH = fileURLToPath(import.meta.url);
const SCRIPT_DIR = dirname(SCRIPT_PATH);
const REPO_ROOT = resolve(SCRIPT_DIR, '..', '..');
const ENV_PATH = join(REPO_ROOT, '.env');
const ENV_LOCAL_PATH = join(REPO_ROOT, '.env.local');
const ENV_FILE_PATHS = [ENV_PATH, ENV_LOCAL_PATH];
const ENV_EXAMPLE_PATH = join(REPO_ROOT, '.env.example');
const DEFAULT_PROFILE = 'relay-mcp';
const DEFAULT_RELAY_MCP_URL = 'http://127.0.0.1:8081/mcp';
const DEFAULT_TUNNEL_MCP_TRANSPORT = 'stdio';
const RELAY_MCP_STDIO_LAUNCHER_PATH = join(REPO_ROOT, 'scripts', 'local', 'relay-mcp-stdio.mjs');
const ALLOWED_TUNNEL_MCP_TRANSPORTS = new Set(['stdio', 'http']);

class ValidationError extends Error {
  constructor(message) {
    super(message);
    this.name = 'ValidationError';
  }
}

function main() {
  const originalEnvKeys = new Set(Object.keys(process.env));
  for (const envFilePath of ENV_FILE_PATHS) {
    loadEnvFile(envFilePath, originalEnvKeys);
  }

  const [command = 'help', ...restArgs] = process.argv.slice(2);
  const options = parseOptions(restArgs);
  const config = getConfig();

  runCommand(command, config, options).then(
    (exitCode) => {
      process.exitCode = exitCode;
    },
    (error) => {
      handleError(error);
    }
  );
}

function loadEnvFile(filePath, originalEnvKeys) {
  if (!existsSync(filePath)) {
    return;
  }

  const content = readFileSync(filePath, 'utf8');
  for (const rawLine of content.split(/\r?\n/u)) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) {
      continue;
    }

    const separatorIndex = line.indexOf('=');
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

function parseOptions(args) {
  const options = {
    skipRelayCheck: false,
  };

  for (const arg of args) {
    if (arg === '--skip-relay-check') {
      options.skipRelayCheck = true;
      continue;
    }

    throw new ValidationError(`Unknown option: ${arg}`);
  }

  return options;
}

function getConfig() {
  const tunnelMcpTransport = process.env.TUNNEL_MCP_TRANSPORT || DEFAULT_TUNNEL_MCP_TRANSPORT;
  if (!ALLOWED_TUNNEL_MCP_TRANSPORTS.has(tunnelMcpTransport)) {
    throw new ValidationError(`TUNNEL_MCP_TRANSPORT must be one of: ${Array.from(ALLOWED_TUNNEL_MCP_TRANSPORTS).join(', ')}`);
  }

  return {
    envPath: ENV_PATH,
    envLocalPath: ENV_LOCAL_PATH,
    envExamplePath: ENV_EXAMPLE_PATH,
    tunnelProfile: process.env.TUNNEL_PROFILE || DEFAULT_PROFILE,
    tunnelId: process.env.TUNNEL_ID || '',
    tunnelMcpTransport,
    relayMcpUrl: process.env.RELAY_MCP_URL || DEFAULT_RELAY_MCP_URL,
    relayMcpStdioCommand: process.env.RELAY_MCP_STDIO_COMMAND || buildDefaultRelayMcpCommand(),
    relayMcpStdioLauncherPath: RELAY_MCP_STDIO_LAUNCHER_PATH,
    tunnelClientPath: process.env.TUNNEL_CLIENT_PATH || '',
    controlPlaneApiKey: process.env.CONTROL_PLANE_API_KEY || '',
  };
}

async function runCommand(command, config, options) {
  switch (command) {
    case 'help':
      printHelp(config);
      return 0;
    case 'init':
      return runInit(config, options);
    case 'start':
      return runStart(config, options);
    case 'doctor':
      return runDoctor(config, options);
    default:
      throw new ValidationError(`Unknown command: ${command}`);
  }
}

function printHelp(config) {
  console.log('ChatGPT Local MCP Tunnel');
  console.log('');
  console.log('Setup flow:');
  console.log(`1. Copy ${config.envExamplePath} to ${config.envPath} or ${config.envLocalPath}.`);
  console.log('2. Fill TUNNEL_ID and CONTROL_PLANE_API_KEY.');
  console.log('3. Run npm run chatgpt-mcp:init once.');
  console.log('4. Run npm run chatgpt-mcp:start for daily use.');
  console.log('5. Keep that single terminal open while ChatGPT uses the connector.');
  console.log('');
  console.log('Default transport: stdio');
  console.log(`Relay MCP command: ${config.relayMcpStdioCommand}`);
  console.log('HTTP mode is available for advanced/dev use by setting TUNNEL_MCP_TRANSPORT=http and RELAY_MCP_URL.');
  console.log('');
  console.log('Commands:');
  console.log('  node scripts/local/chatgpt-mcp.mjs init [--skip-relay-check]');
  console.log('  node scripts/local/chatgpt-mcp.mjs start [--skip-relay-check]');
  console.log('  node scripts/local/chatgpt-mcp.mjs doctor [--skip-relay-check]');
  console.log('  node scripts/local/chatgpt-mcp.mjs help');
}

async function runInit(config, options) {
  requireConfiguredTunnelId(config);
  requireConfiguredApiKey(config, 'init');

  const tunnelClient = resolveTunnelClient(config);
  if (config.tunnelMcpTransport === 'http' && !options.skipRelayCheck) {
    await assertRelayReachable(config.relayMcpUrl);
  }

  const initArgs = ['init', '--force', '--profile', config.tunnelProfile, '--tunnel-id', config.tunnelId];
  if (config.tunnelMcpTransport === 'stdio') {
    initArgs.push('--mcp-command', config.relayMcpStdioCommand);
  } else {
    initArgs.push('--mcp-server-url', config.relayMcpUrl);
  }

  let exitCode = await runTunnelClient(tunnelClient, initArgs, config.controlPlaneApiKey);
  if (exitCode !== 0) {
    return exitCode;
  }

  exitCode = await runTunnelClient(
    tunnelClient,
    ['doctor', '--profile', config.tunnelProfile, '--explain'],
    config.controlPlaneApiKey
  );
  return exitCode;
}

async function runStart(config, options) {
  requireConfiguredApiKey(config, 'start');

  const tunnelClient = resolveTunnelClient(config);
  if (config.tunnelMcpTransport === 'http' && !options.skipRelayCheck) {
    await assertRelayReachable(config.relayMcpUrl);
  }

  console.log('command: start');
  console.log(`profile: ${config.tunnelProfile}`);
  console.log(`MCP transport: ${config.tunnelMcpTransport}`);
  if (config.tunnelMcpTransport === 'stdio') {
    console.log(`Relay MCP command: ${config.relayMcpStdioCommand}`);
  } else {
    console.log(`Relay MCP URL: ${config.relayMcpUrl}`);
  }
  console.log(`tunnel ID configured: ${isConfiguredTunnelId(config.tunnelId) ? 'yes' : 'no'}`);

  return runTunnelClient(
    tunnelClient,
    ['run', '--profile', config.tunnelProfile],
    config.controlPlaneApiKey
  );
}

async function runDoctor(config, options) {
  const diagnostics = {
    envPathPresent: existsSync(config.envPath),
    envLocalPathPresent: existsSync(config.envLocalPath),
    tunnelIdConfigured: isConfiguredTunnelId(config.tunnelId),
    controlPlaneApiKeyConfigured: isConfiguredApiKey(config.controlPlaneApiKey),
    tunnelClientPath: null,
    tunnelClientResolved: false,
    localCheck: null,
  };

  try {
    diagnostics.tunnelClientPath = resolveTunnelClient(config);
    diagnostics.tunnelClientResolved = true;
  } catch (error) {
    if (error instanceof ValidationError) {
      diagnostics.tunnelClientPath = error.message;
    } else {
      throw error;
    }
  }

  if (options.skipRelayCheck) {
    diagnostics.localCheck = 'skipped (--skip-relay-check)';
  } else if (config.tunnelMcpTransport === 'stdio') {
    try {
      await runRelayMcpSelfTest(config);
      diagnostics.localCheck = 'ok';
    } catch (error) {
      if (error instanceof ValidationError) {
        diagnostics.localCheck = error.message;
      } else {
        throw error;
      }
    }
  } else {
    try {
      await assertRelayReachable(config.relayMcpUrl);
      diagnostics.localCheck = 'ok';
    } catch (error) {
      if (error instanceof ValidationError) {
        diagnostics.localCheck = error.message;
      } else {
        throw error;
      }
    }
  }

  printDiagnostics(config, diagnostics);

  if (!diagnostics.controlPlaneApiKeyConfigured) {
    console.error('CONTROL_PLANE_API_KEY is required for tunnel-client doctor. Set it in .env, .env.local, or the process environment.');
    return 1;
  }

  if (!diagnostics.tunnelClientResolved) {
    return 1;
  }

  if (!options.skipRelayCheck && diagnostics.localCheck !== 'ok') {
    return 1;
  }

  return runTunnelClient(
    diagnostics.tunnelClientPath,
    ['doctor', '--profile', config.tunnelProfile, '--explain'],
    config.controlPlaneApiKey
  );
}

function printDiagnostics(config, diagnostics) {
  console.log(`env file (.env): ${diagnostics.envPathPresent ? 'present' : 'missing'}`);
  console.log(`env file (.env.local): ${diagnostics.envLocalPathPresent ? 'present' : 'missing'}`);
  console.log(`profile: ${config.tunnelProfile}`);
  console.log(`MCP transport: ${config.tunnelMcpTransport}`);
  if (config.tunnelMcpTransport === 'stdio') {
    console.log(`Relay MCP command: ${config.relayMcpStdioCommand}`);
    console.log(`local stdio self-test: ${diagnostics.localCheck ?? 'not run'}`);
  } else {
    console.log(`Relay MCP URL: ${config.relayMcpUrl}`);
    console.log(`local /mcp ping: ${diagnostics.localCheck ?? 'not run'}`);
  }
  console.log(`tunnel ID configured: ${diagnostics.tunnelIdConfigured ? 'yes' : 'no'}`);
  console.log(`control-plane key configured: ${diagnostics.controlPlaneApiKeyConfigured ? 'yes' : 'no'}`);
  console.log(`tunnel-client path: ${diagnostics.tunnelClientPath ?? 'unresolved'}`);
}

function requireConfiguredTunnelId(config) {
  if (!isConfiguredTunnelId(config.tunnelId)) {
    throw new ValidationError('TUNNEL_ID is required for init. Set it in .env, .env.local, or the process environment.');
  }
}

function requireConfiguredApiKey(config, commandName) {
  if (!isConfiguredApiKey(config.controlPlaneApiKey)) {
    throw new ValidationError(`CONTROL_PLANE_API_KEY is required for ${commandName}. Set it in .env, .env.local, or the process environment.`);
  }
}

function isConfiguredTunnelId(value) {
  return Boolean(value) && value !== 'tunnel_REPLACE_ME';
}

function isConfiguredApiKey(value) {
  return Boolean(value) && value !== 'sk-REPLACE_ME';
}

function resolveTunnelClient(config) {
  if (config.tunnelClientPath) {
    const explicitPath = isAbsolute(config.tunnelClientPath)
      ? config.tunnelClientPath
      : resolve(process.cwd(), config.tunnelClientPath);
    if (!existsSync(explicitPath)) {
      throw new ValidationError(`TUNNEL_CLIENT_PATH does not exist: ${explicitPath}`);
    }
    return explicitPath;
  }

  const resolvedPath = findOnPath(['tunnel-client', 'tunnel-client.exe']);
  if (!resolvedPath) {
    throw new ValidationError('Set TUNNEL_CLIENT_PATH in .env, .env.local, or add tunnel-client to PATH.');
  }

  return resolvedPath;
}

function findOnPath(commandNames) {
  const pathValue = process.env.PATH || '';
  if (!pathValue) {
    return null;
  }

  const extensions = process.platform === 'win32'
    ? (process.env.PATHEXT || '.COM;.EXE;.BAT;.CMD')
        .split(';')
        .filter(Boolean)
    : [''];

  for (const directory of pathValue.split(delimiter)) {
    if (!directory) {
      continue;
    }

    for (const commandName of commandNames) {
      for (const candidate of buildCommandCandidates(commandName, extensions)) {
        const candidatePath = join(directory, candidate);
        try {
          accessSync(candidatePath, constants.F_OK);
          return candidatePath;
        } catch {
          // Keep searching.
        }
      }
    }
  }

  return null;
}

function buildCommandCandidates(commandName, extensions) {
  if (process.platform !== 'win32') {
    return [commandName];
  }

  const lowerName = commandName.toLowerCase();
  const hasKnownExtension = extensions.some((extension) => lowerName.endsWith(extension.toLowerCase()));
  if (hasKnownExtension) {
    return [commandName];
  }

  return [commandName, ...extensions.map((extension) => `${commandName}${extension}`)];
}

async function assertRelayReachable(relayMcpUrl) {
  const response = await postJsonRpcPing(relayMcpUrl);

  if (response.statusCode === 405) {
    throw new ValidationError(`Relay /mcp returned HTTP 405 at ${relayMcpUrl}. The script must use HTTP POST JSON-RPC for this endpoint.`);
  }

  if (response.statusCode !== 200) {
    throw new ValidationError(`Relay /mcp check failed with HTTP ${response.statusCode} at ${relayMcpUrl}.`);
  }

  let payload;
  try {
    payload = JSON.parse(response.body);
  } catch {
    throw new ValidationError(`Relay /mcp returned HTTP 200 at ${relayMcpUrl}, but the body was not valid JSON.`);
  }

  if (payload?.jsonrpc !== '2.0' || !Object.prototype.hasOwnProperty.call(payload, 'result')) {
    throw new ValidationError(`Relay /mcp returned HTTP 200 at ${relayMcpUrl}, but ping did not return a JSON-RPC result.`);
  }
}

function postJsonRpcPing(relayMcpUrl) {
  return new Promise((resolvePromise, rejectPromise) => {
    let targetUrl;
    try {
      targetUrl = new URL(relayMcpUrl);
    } catch {
      rejectPromise(new ValidationError(`RELAY_MCP_URL is not a valid URL: ${relayMcpUrl}`));
      return;
    }

    const requestImpl = targetUrl.protocol === 'https:' ? httpsRequest : httpRequest;
    const body = JSON.stringify({
      jsonrpc: '2.0',
      id: 1,
      method: 'ping',
      params: {},
    });

    const request = requestImpl(
      targetUrl,
      {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          'content-length': Buffer.byteLength(body),
        },
        timeout: 5000,
      },
      (response) => {
        const chunks = [];
        response.setEncoding('utf8');
        response.on('data', (chunk) => chunks.push(chunk));
        response.on('end', () => {
          resolvePromise({
            statusCode: response.statusCode ?? 0,
            body: chunks.join(''),
          });
        });
      }
    );

    request.on('timeout', () => {
      request.destroy(new ValidationError(`Relay /mcp is not reachable at ${relayMcpUrl}. Start the Relay HTTP daemon first, for example: go run ./cmd/relay`));
    });

    request.on('error', (error) => {
      if (error instanceof ValidationError) {
        rejectPromise(error);
        return;
      }

      if (error && typeof error === 'object' && 'code' in error) {
        const errorCode = String(error.code);
        if (errorCode === 'ECONNREFUSED' || errorCode === 'ETIMEDOUT') {
          rejectPromise(new ValidationError(`Relay /mcp is not reachable at ${relayMcpUrl}. Start the Relay HTTP daemon first, for example: go run ./cmd/relay`));
          return;
        }
      }

      rejectPromise(new ValidationError(`Relay /mcp check failed at ${relayMcpUrl}: ${error.message}`));
    });

    request.write(body);
    request.end();
  });
}

function buildDefaultRelayMcpCommand() {
  const nodePath = normalizeCommandPathForTunnel(process.execPath);
  const launcherPath = normalizeCommandPathForTunnel(RELAY_MCP_STDIO_LAUNCHER_PATH);
  return `${quoteCommandArgument(nodePath)} ${quoteCommandArgument(launcherPath)}`;
}

function quoteCommandArgument(value) {
  if (value === '') {
    return '""';
  }

  if (process.platform !== 'win32' && !/[ \t"\n]/u.test(value)) {
    return value;
  }

  return `"${value.replace(/(\\*)"/g, '$1$1\\"').replace(/(\\+)$/g, '$1$1')}"`;
}

function normalizeCommandPathForTunnel(value) {
  if (process.platform !== 'win32') {
    return value;
  }

  return value.replace(/\\/g, '/');
}

function runRelayMcpSelfTest(config) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(process.execPath, [config.relayMcpStdioLauncherPath, '--self-test'], {
      stdio: ['ignore', 'pipe', 'pipe'],
      env: process.env,
    });

    let stdoutBuffer = '';
    let stderrBuffer = '';

    child.stdout.setEncoding('utf8');
    child.stdout.on('data', (chunk) => {
      stdoutBuffer += chunk;
    });

    child.stderr.setEncoding('utf8');
    child.stderr.on('data', (chunk) => {
      stderrBuffer += chunk;
      process.stderr.write(chunk);
    });

    child.on('error', (error) => {
      rejectPromise(new ValidationError(`Failed to start Relay MCP stdio self-test: ${error.message}`));
    });

    child.on('close', (code, signal) => {
      if (stdoutBuffer.trim()) {
        rejectPromise(new ValidationError(`Relay MCP stdio self-test wrote unexpected stdout: ${stdoutBuffer.trim()}`));
        return;
      }

      if (signal) {
        rejectPromise(new ValidationError(`Relay MCP stdio self-test exited due to signal ${signal}.`));
        return;
      }

      if ((code ?? 1) !== 0) {
        const suffix = stderrBuffer.trim() ? ` ${stderrBuffer.trim()}` : '';
        rejectPromise(new ValidationError(`Relay MCP stdio self-test failed with exit code ${code ?? 1}.${suffix}`));
        return;
      }

      resolvePromise();
    });
  });
}

function runTunnelClient(command, args, controlPlaneApiKey) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(command, args, {
      stdio: ['inherit', 'pipe', 'pipe'],
      env: {
        ...process.env,
        CONTROL_PLANE_API_KEY: controlPlaneApiKey,
      },
    });

    child.stdout.on('data', (chunk) => {
      process.stdout.write(redactSecrets(String(chunk), controlPlaneApiKey));
    });

    child.stderr.on('data', (chunk) => {
      process.stderr.write(redactSecrets(String(chunk), controlPlaneApiKey));
    });

    child.on('error', (error) => {
      rejectPromise(new ValidationError(`Failed to start tunnel-client: ${error.message}`));
    });

    child.on('close', (code, signal) => {
      if (signal) {
        rejectPromise(new ValidationError(`tunnel-client exited due to signal ${signal}.`));
        return;
      }

      resolvePromise(code ?? 1);
    });
  });
}

function redactSecrets(text, controlPlaneApiKey) {
  if (!controlPlaneApiKey) {
    return text;
  }

  return text.split(controlPlaneApiKey).join('[REDACTED]');
}

function handleError(error) {
  if (error instanceof ValidationError) {
    console.error(error.message);
    process.exitCode = 1;
    return;
  }

  const message = error instanceof Error ? error.message : String(error);
  console.error(message);
  process.exitCode = 1;
}

main();
