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

function extractProfileTools(source, profile) {
  const pattern = new RegExp(
    `${profile}:\\s*Object\\.freeze\\(\\[([\\s\\S]*?)\\]\\)`,
  );
  const match = source.match(pattern);
  assert.ok(match, `missing ${profile} profile inventory`);
  return [...match[1].matchAll(/"([^"]+)"/g)].map((entry) => entry[1]);
}

test("local MCP help uses executable direct Node entry points", async () => {
  const { stdout, stderr } = await execFileAsync(
    process.execPath,
    [chatgptScriptPath, "help"],
    {
      cwd: repositoryRoot,
      env: { ...process.env },
    },
  );
  const output = `${stdout}\n${stderr}`;

  assert.match(output, /node scripts\/local\/chatgpt-mcp\.mjs init/);
  assert.match(output, /node scripts\/local\/chatgpt-mcp\.mjs start/);
  assert.doesNotMatch(output, /npm run chatgpt-mcp:(?:init|start|doctor)/);
});

test("root package owns the retained local-script test entry only", () => {
  assert.equal(
    rootPackage.scripts?.["test:local-scripts"],
    "node --test scripts/local/chatgpt-mcp.test.mjs",
  );
  assert.equal(rootPackage.scripts?.["chatgpt-mcp:init"], undefined);
  assert.equal(rootPackage.scripts?.["chatgpt-mcp:start"], undefined);
  assert.equal(rootPackage.scripts?.["chatgpt-mcp:doctor"], undefined);
});

test("stdio verification inventories match the canonical ordered profiles", () => {
  for (const [profile, expected] of Object.entries(expectedToolsByProfile)) {
    assert.deepEqual(extractProfileTools(stdioSource, profile), expected);
  }
});

test("local scripts retain canonical profile and transport guardrails", () => {
  for (const profile of ["planner", "auditor", "local_operator"]) {
    assert.match(chatgptSource, new RegExp(`["']${profile}["']`));
    assert.match(stdioSource, new RegExp(`["']${profile}["']`));
  }
  for (const transport of ["stdio", "http"]) {
    assert.match(chatgptSource, new RegExp(`["']${transport}["']`));
  }

  assert.match(chatgptSource, /127\.0\.0\.1:3000/);
  assert.match(chatgptSource, /doctor/);
  assert.match(chatgptSource, /TUNNEL_CONTROL_API_KEY/);
  assert.match(chatgptSource, /redact/i);
  assert.match(stdioSource, /defaulting to planner/i);
});

test("stdio self-test retains protocol and file-parameter checks", () => {
  for (const token of [
    "initialize",
    "notifications/initialized",
    "ping",
    "tools/list",
    "artifact_file",
    "openaiFileIdRefs",
  ]) {
    assert.match(stdioSource, new RegExp(token.replace("/", "\\/")));
  }
  assert.match(stdioSource, /tool inventory mismatch/i);
});

test("local inventory oracle contains no representative retired action", () => {
  const allTools = Object.keys(expectedToolsByProfile).flatMap((profile) =>
    extractProfileTools(stdioSource, profile),
  );
  for (const retired of [
    "create_run_from_planner_handoff",
    "create_plan_seed",
    "get_pass_context",
    "create_context_packet",
    "create_local_audit",
    "list_refactor_candidates",
  ]) {
    assert.equal(allTools.includes(retired), false, retired);
  }
});
