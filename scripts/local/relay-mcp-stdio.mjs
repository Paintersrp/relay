#!/usr/bin/env node

import { spawn } from 'child_process';
import { accessSync, constants, existsSync, readFileSync } from 'fs';
import { dirname, isAbsolute, join, resolve } from 'path';
import process from 'process';
import { fileURLToPath } from 'url';

const SCRIPT_PATH = fileURLToPath(import.meta.url);
const SCRIPT_DIR = dirname(SCRIPT_PATH);
const REPO_ROOT = resolve(SCRIPT_DIR, '..', '..');
const ENV_FILE_PATHS = [join(REPO_ROOT, '.env'), join(REPO_ROOT, '.env.local')];
const DEFAULT_RELAY_MCP_PROFILE = 'planner';
const TOOL_NAMES_BY_PROFILE = Object.freeze({
  planner: ['validate_artifact', 'submit_plan', 'get_plan', 'create_run'],
  auditor: [
    'validate_artifact',
    'create_run',
    'get_audit_packet',
    'record_audit_decision',
  ],
  local_operator: [
    'validate_artifact',
    'submit_plan',
    'get_plan',
    'create_run',
    'get_audit_packet',
    'record_audit_decision',
  ],
});

class ValidationError extends Error {
  constructor(message) {
    super(message);
    this.name = 'ValidationError';
  }
}

async function main() {
  const originalEnvKeys = new Set(Object.keys(process.env));
  for (const envFilePath of ENV_FILE_PATHS) {
    loadEnvFile(envFilePath, originalEnvKeys);
  }

  const profile = resolveRelayMcpProfile(process.env.RELAY_MCP_PROFILE);
  process.env.RELAY_MCP_PROFILE = profile;

  const args = process.argv.slice(2);
  if (args.length > 1 || (args.length === 1 && args[0] !== '--self-test')) {
    throw new ValidationError('Usage: node scripts/local/relay-mcp-stdio.mjs [--self-test]');
  }

  const commandSpec = resolveRelayMcpServerCommand();
  if (args[0] === '--self-test') {
    await runSelfTest(commandSpec, profile);
    return;
  }

  const exitCode = await proxyStdio(commandSpec);
  process.exitCode = exitCode;
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

function resolveRelayMcpProfile(raw) {
  const profile = String(raw || DEFAULT_RELAY_MCP_PROFILE).trim().toLowerCase();
  if (Object.prototype.hasOwnProperty.call(TOOL_NAMES_BY_PROFILE, profile)) {
    return profile;
  }
  console.error(
    `Unsupported RELAY_MCP_PROFILE ${JSON.stringify(profile)}; defaulting to planner.`,
  );
  return DEFAULT_RELAY_MCP_PROFILE;
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

function resolveRelayMcpServerCommand() {
  const explicitBinary = process.env.RELAY_MCP_SERVER_BIN || '';
  if (explicitBinary) {
    const explicitPath = isAbsolute(explicitBinary)
      ? explicitBinary
      : resolve(REPO_ROOT, explicitBinary);
    ensureFileExists(explicitPath, 'RELAY_MCP_SERVER_BIN');
    return {
      command: explicitPath,
      args: [],
      description: explicitPath,
    };
  }

	return {
		command: 'go',
		args: ['run', './cmd/mcpserver'],
    description: 'go run ./cmd/mcpserver',
  };
}

function ensureFileExists(filePath, envVarName) {
  try {
    accessSync(filePath, constants.F_OK);
  } catch {
    throw new ValidationError(`${envVarName} does not exist: ${filePath}`);
  }
}

function proxyStdio(commandSpec) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(commandSpec.command, commandSpec.args, {
      cwd: REPO_ROOT,
      env: process.env,
      stdio: ['pipe', 'pipe', 'pipe'],
    });

    const signalHandlers = new Map();
    for (const signal of ['SIGINT', 'SIGTERM']) {
      const handler = () => {
        if (!child.killed) {
          child.kill(signal);
        }
      };
      signalHandlers.set(signal, handler);
      process.on(signal, handler);
    }

    process.stdin.pipe(child.stdin);
    child.stdout.pipe(process.stdout);
    child.stderr.pipe(process.stderr);

    child.stdin.on('error', (error) => {
      if (error && typeof error === 'object' && 'code' in error && error.code === 'EPIPE') {
        return;
      }
      rejectPromise(new ValidationError(`Relay MCP stdin proxy failed: ${error.message}`));
    });

    child.on('error', (error) => {
      rejectPromise(new ValidationError(`Failed to start Relay MCP server (${commandSpec.description}): ${error.message}`));
    });

    child.on('close', (code, signal) => {
      for (const [registeredSignal, handler] of signalHandlers.entries()) {
        process.off(registeredSignal, handler);
      }

      process.stdin.unpipe(child.stdin);

      if (signal) {
        resolvePromise(exitCodeFromSignal(signal));
        return;
      }

      resolvePromise(code ?? 1);
    });
  });
}

function runSelfTest(commandSpec, profile) {
  return new Promise((resolvePromise, rejectPromise) => {
    console.error(`Relay MCP stdio self-test command: ${commandSpec.description}`);

    const child = spawn(commandSpec.command, commandSpec.args, {
      cwd: REPO_ROOT,
      env: process.env,
      stdio: ['pipe', 'pipe', 'pipe'],
    });

    let settled = false;
    let stdoutBuffer = '';
    const pendingResponses = new Map();
    const stderrChunks = [];
    const timeouts = new Set();

    const finish = (error) => {
      if (settled) {
        return;
      }
      settled = true;

      for (const timeout of timeouts) {
        clearTimeout(timeout);
      }
      timeouts.clear();

      for (const waiter of pendingResponses.values()) {
        waiter.reject(error);
      }
      pendingResponses.clear();

      if (!child.killed) {
        child.kill('SIGTERM');
      }

      if (error) {
        rejectPromise(error);
      } else {
        resolvePromise();
      }
    };

    const awaitResponse = (id, label) =>
      new Promise((resolveResponse, rejectResponse) => {
        const timeout = setTimeout(() => {
          pendingResponses.delete(id);
          rejectResponse(new ValidationError(`Timed out waiting for ${label} response from Relay MCP server.`));
        }, 10000);
        timeouts.add(timeout);

        pendingResponses.set(id, {
          resolve: (response) => {
            clearTimeout(timeout);
            timeouts.delete(timeout);
            resolveResponse(response);
          },
          reject: (error) => {
            clearTimeout(timeout);
            timeouts.delete(timeout);
            rejectResponse(error);
          },
        });
      });

    const dispatchLine = (line) => {
      const trimmedLine = line.trim();
      if (!trimmedLine) {
        return;
      }

      let parsed;
      try {
        parsed = JSON.parse(trimmedLine);
      } catch {
        finish(new ValidationError(`Relay MCP stdout contained a non-JSON line during self-test: ${trimmedLine}`));
        return;
      }

      const hasID = Object.prototype.hasOwnProperty.call(parsed, 'id');
      const id = parsed?.id;
      if (!hasID || id === null || id === undefined) {
        finish(new ValidationError(`Relay MCP produced an unexpected response without a valid id during self-test: ${trimmedLine}`));
        return;
      }

      const waiter = pendingResponses.get(id);
      if (!waiter) {
        finish(new ValidationError(`Received unexpected JSON-RPC response id ${String(id)} during self-test.`));
        return;
      }

      pendingResponses.delete(id);
      waiter.resolve(parsed);
    };

    child.stdout.setEncoding('utf8');
    child.stdout.on('data', (chunk) => {
      stdoutBuffer += chunk;

      let newlineIndex = stdoutBuffer.indexOf('\n');
      while (newlineIndex !== -1) {
        const line = stdoutBuffer.slice(0, newlineIndex);
        stdoutBuffer = stdoutBuffer.slice(newlineIndex + 1);
        dispatchLine(line);
        if (settled) {
          return;
        }
        newlineIndex = stdoutBuffer.indexOf('\n');
      }
    });

    child.stderr.setEncoding('utf8');
    child.stderr.on('data', (chunk) => {
      stderrChunks.push(chunk);
      process.stderr.write(chunk);
    });

    child.on('error', (error) => {
      finish(new ValidationError(`Failed to start Relay MCP server (${commandSpec.description}): ${error.message}`));
    });

    child.on('close', (code, signal) => {
      if (settled) {
        return;
      }

      if (stdoutBuffer.trim()) {
        dispatchLine(stdoutBuffer);
        stdoutBuffer = '';
        if (settled) {
          return;
        }
      }

      const stderrSummary = stderrChunks.join('').trim();
      let message = `Relay MCP server exited before self-test completed (code=${code ?? 'null'}, signal=${signal ?? 'null'}).`;
      if (stderrSummary) {
        message += ` Stderr: ${stderrSummary}`;
      }
      finish(new ValidationError(message));
    });

    const send = (payload) => {
      child.stdin.write(`${JSON.stringify(payload)}\n`);
    };

    (async () => {
      try {
        const initializeResponsePromise = awaitResponse(1, 'initialize');
        send({
          jsonrpc: '2.0',
          id: 1,
          method: 'initialize',
          params: {},
        });
        const initializeResponse = await initializeResponsePromise;
        if (initializeResponse?.jsonrpc !== '2.0' || !initializeResponse.result) {
          throw new ValidationError('Relay MCP initialize response was missing a JSON-RPC result.');
        }
        console.error('initialize: ok');

        send({
          jsonrpc: '2.0',
          method: 'notifications/initialized',
          params: {},
        });

        const pingResponsePromise = awaitResponse(2, 'ping');
        send({
          jsonrpc: '2.0',
          id: 2,
          method: 'ping',
          params: {},
        });
        const pingResponse = await pingResponsePromise;
        if (pingResponse?.jsonrpc !== '2.0' || !Object.prototype.hasOwnProperty.call(pingResponse, 'result')) {
          throw new ValidationError('Relay MCP ping response was missing a JSON-RPC result.');
        }
        console.error('ping: ok');

        const tools = [];
        let cursor = '';
        let requestID = 3;
        do {
          const toolsListResponsePromise = awaitResponse(requestID, 'tools/list');
          send({
            jsonrpc: '2.0',
            id: requestID,
            method: 'tools/list',
            params: cursor ? { cursor } : {},
          });
          const toolsListResponse = await toolsListResponsePromise;
          const page = toolsListResponse?.result?.tools;
          if (!Array.isArray(page)) {
            throw new ValidationError('Relay MCP tools/list response did not include a tools array.');
          }
          tools.push(...page);
          cursor = String(toolsListResponse?.result?.nextCursor || '');
          requestID += 1;
        } while (cursor);

        const actualToolNames = tools
          .map((tool) => tool?.name)
          .filter((toolName) => typeof toolName === 'string');
        const expectedToolNames = TOOL_NAMES_BY_PROFILE[profile];
        if (JSON.stringify(actualToolNames) !== JSON.stringify(expectedToolNames)) {
          throw new ValidationError(
            `Relay MCP tools/list for profile ${profile} returned ${actualToolNames.join(', ')}; expected ${expectedToolNames.join(', ')}.`,
          );
        }
        for (const fileToolName of expectedToolNames.filter((name) =>
          ['validate_artifact', 'submit_plan', 'create_run'].includes(name)
        )) {
          const tool = tools.find((candidate) => candidate?.name === fileToolName);
          assertCanonicalFileParameterTool(tool, fileToolName);
        }

        console.error(`tools/list: ok (${tools.length} tools)`);
        console.error(`profile: ${profile}`);
        console.error(`tools: ${expectedToolNames.join(', ')}`);

        child.stdin.end();
        finish(null);
      } catch (error) {
        finish(error instanceof ValidationError ? error : new ValidationError(error.message));
      }
    })();
  });
}

function assertCanonicalFileParameterTool(tool, toolName) {
  const params = tool?._meta?.['openai/fileParams'];
  if (!Array.isArray(params) || params.length !== 1 || params[0] !== 'artifact_file') {
    throw new ValidationError(`${toolName} is missing _meta.openai/fileParams=["artifact_file"].`);
  }
  const fileSchema = tool?.inputSchema?.properties?.artifact_file;
  if (!fileSchema || fileSchema.type !== 'object') {
    throw new ValidationError(`${toolName} artifact_file schema must be an object.`);
  }
  if (fileSchema.additionalProperties !== false) {
    throw new ValidationError(`${toolName} artifact_file schema must set additionalProperties=false.`);
  }
  const required = Array.isArray(fileSchema.required) ? new Set(fileSchema.required) : new Set();
  if (!required.has('download_url') || !required.has('file_id') || !required.has('file_name')) {
    throw new ValidationError(`${toolName} artifact_file schema must require download_url, file_id, and file_name.`);
  }
}

function exitCodeFromSignal(signal) {
  switch (signal) {
    case 'SIGINT':
      return 130;
    case 'SIGTERM':
      return 143;
    default:
      return 1;
  }
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

main().catch((error) => {
  handleError(error);
});
