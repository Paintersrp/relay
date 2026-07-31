// ============================================================
// Relay Navigation — Retired route/navigation constraints
// ============================================================
//
// Static source scans proving that the removed cutover control plane and the
// retired legacy Plan write surface leave no reachable web route, generated
// route-tree entry, navigation action, or API client behind.
//
// These are file scans (Node environment) rather than DOM assertions because
// the invariant is structural: a future regression that re-adds a `/cutover`
// route, a `/plans/new` submission route, a Plan-creating navigation action, or
// a Plan write API client must fail here.

import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "..", "..", ".."); // apps/web
const srcDir = path.join(webRoot, "src");
const routesDir = path.join(srcDir, "routes");
const routeTreeFile = path.join(srcDir, "routeTree.gen.ts");

function walk(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      out.push(...walk(full));
      continue;
    }
    out.push(full);
  }
  return out;
}

// Scan shipped sources only. Test files legitimately name the retired
// surfaces in order to assert their absence.
const sourceFiles = walk(srcDir).filter(
  (file) =>
    /\.(ts|tsx)$/.test(file) &&
    file !== routeTreeFile &&
    !/\.test\.tsx?$/.test(file),
);

describe("removed cutover web surface", () => {
  it("registers no /cutover route in the generated route tree", () => {
    expect(readFileSync(routeTreeFile, "utf8")).not.toMatch(/cutover/i);
  });

  it("declares no cutover route file", () => {
    for (const file of walk(routesDir)) {
      expect(path.basename(file).toLowerCase()).not.toContain("cutover");
    }
  });

  it("references no cutover page, navigation target, or API path in web sources", () => {
    for (const file of sourceFiles) {
      expect(
        readFileSync(file, "utf8"),
        `${path.relative(webRoot, file)} still references cutover`,
      ).not.toMatch(/cutover/i);
    }
  });
});

describe("retired legacy Plan write surface", () => {
  it("registers no /plans/new submission route in the generated route tree", () => {
    expect(readFileSync(routeTreeFile, "utf8")).not.toContain("/plans/new");
  });

  it("exposes no /plans/new navigation target in web sources", () => {
    for (const file of sourceFiles) {
      expect(
        readFileSync(file, "utf8"),
        `${path.relative(webRoot, file)} still navigates to /plans/new`,
      ).not.toContain("/plans/new");
    }
  });

  it("issues no Plan-creating or Plan-mutating request from the plans API client", () => {
    const client = readFileSync(
      path.join(srcDir, "features", "relay-plans", "api.ts"),
      "utf8",
    );
    expect(client).not.toContain("submitWorkflowPlan");
    expect(client).not.toContain("moveWorkflowPlan");
    expect(client).not.toContain("/project`");
    expect(client).not.toMatch(/"(POST|PATCH|PUT|DELETE)",\s*`?\/api\/plans/);
  });
});
