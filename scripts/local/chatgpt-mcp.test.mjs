import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, "..", "..");

const chatgptScriptPath = path.join(scriptDirectory, "chatgpt-mcp.mjs");
const stdioScriptPath = path.join(scriptDirectory, "relay-mcp-stdio.mjs");
const packagePath = path.join(repositoryRoot, "package.json");

const [chatgptSource, stdioSource, packageSource] = await Promise.all([
  readFile(chatgptScriptPath, "utf8"),
  readFile(stdioScriptPath, "utf8"),
  readFile(packagePath, "utf8"),
]);
const rootPackage = JSON.parse(packageSource);

const expectedToolsByProfile = Object.freeze({
  planner: Object.freeze([
    "validate_artifact",
    "list_projects",
    "submit_plan",
    "get_plan",
    "create_run",
  ]),
  auditor: Object.freeze([
    "validate_artifact",
    "create_run",
    "get_audit_packet",
    "get_run_artifact",
    "record_audit_decision",
  ]),
  local_operator: Object.freeze([
    "validate_artifact",
    "list_projects",
    "submit_plan",
    "get_plan",
    "create_run",
    "get_audit_packet",
    "get_run_artifact",
    "record_audit_decision",
  ]),
});

function profileInventoryBlock(source) {
  const match = source.match(
    /const TOOL_NAMES_BY_PROFILE\s*=\s*Object\.freeze\(\{([\s\S]*?)\}\);/u,
  );
  assert.ok(match, "missing TOOL_NAMES_BY_PROFILE inventory");
  return match[1];
}

function extractProfileTools(source, profile) {
  const block = profileInventoryBlock(source);
  const pattern = new RegExp(
    `(?:^|\\n)\\s*${profile}\\s*:\\s*(?:Object\\.freeze\\(\\s*)?\\[([\\s\\S]*?)\\]\\s*\\)?\\s*,`,
    "u",
  );
  const match = block.match(pattern);
  assert.ok(match, `missing ${profile} profile inventory`);
  return [...match[1].matchAll(/["']([^"']+)["']/gu)].map(
    (entry) => entry[1],
  );
}

function extractProfileNames(source) {
  const block = profileInventoryBlock(source);
  return [...block.matchAll(/^\s*([a-z_]+)\s*:/gmu)].map(
    (entry) => entry[1],
  );
}

function extractStringSet(source, constantName) {
  const pattern = new RegExp(
    `const ${constantName}\\s*=\\s*new Set\\(\\[([\\s\\S]*?)\\]\\);`,
    "u",
  );
  const match = source.match(pattern);
  assert.ok(match, `missing ${constantName}`);
  return [...match[1].matchAll(/["']([^"']+)["']/gu)].map(
    (entry) => entry[1],
  );
}

const controlledHelpEnv = {
  ...process.env,
  TUNNEL_MCP_TRANSPORT: "stdio",
  RELAY_MCP_PROFILE: "planner",
  RELAY_MCP_URL: "http://127.0.0.1:8080/mcp",
  TUNNEL_HEALTH_LISTEN_ADDR: "127.0.0.1:8082",
};

test("root package owns stable local MCP wrappers", () => {
  assert.equal(
    rootPackage.scripts?.["test:local-scripts"],
    "node --test scripts/local/chatgpt-mcp.test.mjs",
  );
  assert.equal(
    rootPackage.scripts?.["chatgpt-mcp:init"],
    "node scripts/local/chatgpt-mcp.mjs init",
  );
  assert.equal(
    rootPackage.scripts?.["chatgpt-mcp:start"],
    "node scripts/local/chatgpt-mcp.mjs start",
  );
  assert.equal(
    rootPackage.scripts?.["chatgpt-mcp:doctor"],
    "node scripts/local/chatgpt-mcp.mjs doctor",
  );
  assert.equal(
    rootPackage.scripts?.["chatgpt-mcp:help"],
    "node scripts/local/chatgpt-mcp.mjs help",
  );
});

test("local MCP help presents the stable package interface", async () => {
  const { stdout, stderr } = await execFileAsync(
    process.execPath,
    [chatgptScriptPath, "help"],
    {
      cwd: repositoryRoot,
      env: controlledHelpEnv,
    },
  );
  const output = `${stdout}\n${stderr}`;

  for (const command of [
    "npm run chatgpt-mcp:init",
    "npm run chatgpt-mcp:start",
    "npm run chatgpt-mcp:doctor",
    "npm run chatgpt-mcp:help",
  ]) {
    assert.match(output, new RegExp(command.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&")));
  }
  assert.doesNotMatch(output, /node scripts\/local\/chatgpt-mcp\.mjs/u);
});

test("stdio verification inventories match the canonical ordered profiles", () => {
  assert.deepEqual(extractProfileNames(stdioSource), [
    "planner",
    "auditor",
    "local_operator",
  ]);
  for (const [profile, expected] of Object.entries(expectedToolsByProfile)) {
    assert.deepEqual(extractProfileTools(stdioSource, profile), expected);
  }
});

test("local scripts retain canonical profile and transport guardrails", () => {
  assert.deepEqual(
    extractStringSet(chatgptSource, "ALLOWED_RELAY_MCP_PROFILES"),
    ["planner", "auditor", "local_operator"],
  );
  assert.deepEqual(
    extractStringSet(chatgptSource, "ALLOWED_TUNNEL_MCP_TRANSPORTS"),
    ["stdio", "http"],
  );

  assert.match(
    chatgptSource,
    /const DEFAULT_RELAY_MCP_URL = "http:\/\/127\.0\.0\.1:8080\/mcp";/u,
  );
  assert.match(
    chatgptSource,
    /const DEFAULT_TUNNEL_HEALTH_LISTEN_ADDR = "127\.0\.0\.1:8082";/u,
  );
  assert.match(chatgptSource, /process\.env\.CONTROL_PLANE_API_KEY/u);
  assert.match(chatgptSource, /redactSecrets/u);
  assert.match(chatgptSource, /defaulting to planner/iu);
  assert.match(stdioSource, /defaulting to planner/iu);
});

test("stdio self-test retains protocol and file-parameter checks", () => {
  for (const token of [
    "initialize",
    "notifications/initialized",
    "ping",
    "tools/list",
    "artifact_file",
    "openai/fileParams",
    "download_url",
    "file_id",
    "file_name",
  ]) {
    assert.match(stdioSource, new RegExp(token.replace("/", "\\/"), "u"));
  }
  assert.match(stdioSource, /JSON\.stringify\(actualToolNames\)/u);
  assert.match(stdioSource, /JSON\.stringify\(expectedToolNames\)/u);
});

test("local inventory oracle contains no representative retired action", () => {
  const allTools = Object.keys(expectedToolsByProfile).flatMap((profile) =>
    extractProfileTools(stdioSource, profile),
  );
  for (const retired of [
    "create_run_from_planner_handoff",
    "validate_planner_handoff_for_compile",
    "create_plan_attempt_with_intent",
    "create_plan_seed",
    "get_pass_context",
    "create_context_packet",
    "create_local_audit",
    "list_refactor_candidates",
  ]) {
    assert.equal(allTools.includes(retired), false, retired);
  }
});
