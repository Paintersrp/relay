// ============================================================
// Relay Navigation — Constraint / smoke checks (Task 13.5)
// ============================================================
//
// Automated constraint/smoke checks for the frontend-shell-redesign that guard
// the Theme_System and architecture-boundary invariants the design freezes.
// These are static source scans (no DOM, Node environment): they read the
// shell component and relay-navigation selector sources with `fs` and assert
// structural facts, so a future regression that re-introduces a color literal,
// a new endpoint, a lifecycle-mutating palette action, an artifact/log/content
// search field, display-field gating, or an expanded attention set fails here.
//
// Requirements guarded: 4.10 (closed palette action set), 5.8 (entity-only
// search corpus), 6.7 / 6.8 (status-only pipeline gating), 10.3 (no new
// endpoint), 3.11 (closed attention set), plus 7.1/7.2/7.3 (Theme_System
// tokens + dark palette), 10.4 (no orchestration in server functions), 10.5
// (dependency change confined to the two apps/web package files), and 10.6
// (legacy Go-served surfaces not relocated).
//
// If any check surfaces a REAL violation it must be fixed at the source rather
// than the check being weakened.

import { readdirSync, readFileSync, existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import { buildActionEntries } from "./command";
import { AWAITING_REVIEW_STATUSES, BLOCKED_STATUSES } from "./statusSets";

// ------------------------------------------------------------
// Path anchors (resolved from this file, independent of cwd)
// ------------------------------------------------------------

const here = path.dirname(fileURLToPath(import.meta.url));
const navDir = here; // apps/web/src/features/relay-navigation
const webRoot = path.resolve(here, "..", "..", ".."); // apps/web
const repoRoot = path.resolve(webRoot, "..", ".."); // repo root
const shellDir = path.join(webRoot, "src", "components", "relay", "shell");
const routesDir = path.join(webRoot, "src", "routes");
const rootRouteFile = path.join(routesDir, "__root.tsx");
const themeToggleFile = path.join(shellDir, "ThemeToggle.tsx");
const pipelineFile = path.join(navDir, "pipeline.ts");
const searchFile = path.join(navDir, "search.ts");
const typesFile = path.join(navDir, "types.ts");
const pkgFile = path.join(webRoot, "package.json");
const lockFile = path.join(webRoot, "package-lock.json");
const contractDoc = path.join(repoRoot, "docs", "api", "frontend-api-contract.md");

// ------------------------------------------------------------
// Source helpers
// ------------------------------------------------------------

function isScannableSource(name: string): boolean {
  if (!name.endsWith(".ts") && !name.endsWith(".tsx")) return false;
  if (name.endsWith(".d.ts")) return false;
  if (name.endsWith(".test.ts") || name.endsWith(".test.tsx")) return false;
  return true;
}

/** Recursively collect non-test source files under `dir`. */
function collectSources(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...collectSources(full));
    } else if (entry.isFile() && isScannableSource(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

/**
 * Remove comments while preserving line count so scans of source-only text do
 * not false-match on the requirement/field names that the guarded files
 * intentionally document in their comments (e.g. `activeStep`, `rgb`, "logs").
 * Block comments are blanked; line comments are blanked from `//` to EOL.
 */
function stripComments(src: string): string {
  // Blank block comments, preserving newlines.
  let out = src.replace(/\/\*[\s\S]*?\*\//g, (match) =>
    match.replace(/[^\n]/g, " "),
  );
  // Blank line comments (best-effort; safe for these sources which carry no
  // color literals inside string-embedded `//`).
  out = out.replace(/\/\/[^\n]*/g, (match) => " ".repeat(match.length));
  return out;
}

function rel(p: string): string {
  return path.relative(webRoot, p).replace(/\\/g, "/");
}

// ------------------------------------------------------------
// Scan target file sets
// ------------------------------------------------------------

const shellSources = collectSources(shellDir);
const navSources = collectSources(navDir);
const shellAndNavSources = [...shellSources, ...navSources];

// ============================================================
// Theme_System token constraints (Req 7.1, 7.2, 7.3)
// ============================================================

describe("Task 13.5 — Theme_System token constraints", () => {
  it("scans a non-empty set of shell + nav source files", () => {
    // Guard against the checks silently passing because the globs found nothing.
    expect(shellSources.length).toBeGreaterThan(0);
    expect(navSources.length).toBeGreaterThan(0);
  });

  it("contains no disallowed color literals (hex / rgb / rgba / hsl / hsla / named)", () => {
    // Conservative, documented detectors run over comment-stripped source so
    // token references in comments/docstrings never false-match.
    const hex = /#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})\b/;
    const func = /\b(?:rgb|rgba|hsl|hsla)\s*\(/;

    // Named CSS colors are only flagged in an actual CSS-value / color-attr
    // position so Tailwind token classes (e.g. `bg-[var(--relay-...)]`,
    // `text-foreground`, `text-[11px]`) are never mistaken for color literals.
    const namedColors = [
      "black", "white", "red", "green", "blue", "yellow", "orange", "purple",
      "pink", "gray", "grey", "cyan", "magenta", "brown", "teal", "navy",
      "maroon", "olive", "lime", "aqua", "fuchsia", "silver", "gold", "coral",
      "salmon", "crimson", "indigo", "violet", "turquoise", "tan", "beige",
      "ivory", "khaki", "lavender",
    ].join("|");
    const cssProps = [
      "color", "background", "background-color", "border-color",
      "border-top-color", "border-right-color", "border-bottom-color",
      "border-left-color", "outline-color", "fill", "stroke", "caret-color",
      "text-decoration-color", "accent-color", "box-shadow", "text-shadow",
    ].join("|");
    const namedValue = new RegExp(
      `\\b(?:${cssProps})\\s*:\\s*['"]?(?:${namedColors})\\b`,
      "i",
    );
    const namedAttr = new RegExp(
      `\\b(?:color|fill|stroke)\\s*=\\s*['"](?:${namedColors})['"]`,
      "i",
    );

    const violations: string[] = [];
    for (const file of shellAndNavSources) {
      const code = stripComments(readFileSync(file, "utf8"));
      const lines = code.split(/\r?\n/);
      lines.forEach((line, index) => {
        for (const detector of [hex, func, namedValue, namedAttr]) {
          const match = detector.exec(line);
          if (match) {
            violations.push(`${rel(file)}:${index + 1} → ${match[0].trim()}`);
          }
        }
      });
    }

    expect(violations, `disallowed color literals found:\n${violations.join("\n")}`).toEqual([]);
  });

  it("shell components import only Theme_System primitives, lucide-react, tanstack, and internal modules", () => {
    // Any external icon/font/component library would appear as a bare specifier
    // that is not react / react-dom / @tanstack/* / lucide-react — and fail.
    const importRe = /(?:import|export)[^"';]*?from\s*["']([^"']+)["']|import\s*["']([^"']+)["']/g;
    const isAllowed = (spec: string): boolean => {
      if (spec.startsWith(".") || spec.startsWith("@/")) return true; // internal
      if (spec === "react" || spec === "react-dom" || spec === "react/jsx-runtime")
        return true;
      if (spec.startsWith("@tanstack/")) return true;
      if (spec === "lucide-react") return true;
      return false;
    };

    const violations: string[] = [];
    for (const file of shellSources) {
      const code = stripComments(readFileSync(file, "utf8"));
      let m: RegExpExecArray | null;
      while ((m = importRe.exec(code)) !== null) {
        const spec = m[1] ?? m[2];
        if (spec && !isAllowed(spec)) {
          violations.push(`${rel(file)} → imports "${spec}"`);
        }
      }
    }

    expect(
      violations,
      `shell components import a library outside the Theme_System allow-list:\n${violations.join("\n")}`,
    ).toEqual([]);

    // Positively confirm lucide-react is the icon set in use.
    const usesLucide = shellSources.some((file) =>
      /from\s*["']lucide-react["']/.test(readFileSync(file, "utf8")),
    );
    expect(usesLucide).toBe(true);
  });

  it("keeps the dark Tokyo Night palette active", () => {
    // Root document hard-codes the dark class on <html> (Req 7.2)…
    const rootSrc = readFileSync(rootRouteFile, "utf8");
    expect(/<html[^>]*className=["'][^"']*\bdark\b/.test(rootSrc)).toBe(true);

    // …and the Theme_System control defaults to the dark mode.
    const themeSrc = readFileSync(themeToggleFile, "utf8");
    expect(/useState<ThemeMode>\(\s*["']dark["']\s*\)/.test(themeSrc)).toBe(true);
  });
});

// ============================================================
// Boundary preservation constraints
// ============================================================

describe("Task 13.5 — architecture-boundary constraints", () => {
  it("introduces no new backend endpoint in the shell/nav layer (Req 10.3)", () => {
    // The redesign composes existing query helpers only. Assert the shell and
    // navigation sources contain no raw endpoint literals, no direct fetch, no
    // axios, and no server-function orchestration.
    const rawEndpoint = /["'`]\/api\//;
    const directFetch = /\bfetch\s*\(/; // \b excludes `refetch(`
    const axios = /\baxios\b/;
    const serverFn = /\bcreateServerFn\b/;

    const violations: string[] = [];
    for (const file of shellAndNavSources) {
      const code = stripComments(readFileSync(file, "utf8"));
      if (rawEndpoint.test(code)) violations.push(`${rel(file)} → raw /api/ endpoint literal`);
      if (directFetch.test(code)) violations.push(`${rel(file)} → direct fetch() call`);
      if (axios.test(code)) violations.push(`${rel(file)} → axios usage`);
      if (serverFn.test(code)) violations.push(`${rel(file)} → createServerFn (orchestration)`);
    }

    expect(
      violations,
      `shell/nav layer introduced a new fetch/endpoint/server-fn:\n${violations.join("\n")}`,
    ).toEqual([]);

    // Sanity: the documented contract is readable and enumerates endpoints, so
    // this check is comparing against a real, populated API_Contract surface.
    const documented = readFileSync(contractDoc, "utf8").match(/`\/api\/[^`]+`/g) ?? [];
    expect(documented.length).toBeGreaterThan(0);
  });

  it("does not relocate orchestration into TanStack Start server functions (Req 10.4)", () => {
    for (const file of shellAndNavSources) {
      const code = stripComments(readFileSync(file, "utf8"));
      expect(
        /\bcreateServerFn\b/.test(code),
        `${rel(file)} defines a server function`,
      ).toBe(false);
    }
  });

  it("keeps the Global_Search corpus limited to entity name/id/route fields (Req 5.8)", () => {
    // SearchableEntity must carry only type/id/name/to/params — never an
    // artifact/log/content/source field.
    const typesSrc = stripComments(readFileSync(typesFile, "utf8"));
    const block = /export\s+interface\s+SearchableEntity\s*\{([\s\S]*?)\}/.exec(typesSrc);
    expect(block, "SearchableEntity interface not found").not.toBeNull();

    const fields = Array.from(block![1].matchAll(/(\w+)\s*\??\s*:/g)).map((m) => m[1]).sort();
    expect(fields).toEqual(["id", "name", "params", "to", "type"]);

    // searchEntities must read only `name` and `id`, never any content field.
    const searchSrc = stripComments(readFileSync(searchFile, "utf8"));
    expect(searchSrc.includes("entity.name")).toBe(true);
    expect(searchSrc.includes("entity.id")).toBe(true);
    const forbiddenFieldAccess =
      /entity\.(artifact|artifacts|log|logs|content|contents|body|text|output|source|packet|markdown|diff|validation)/i;
    expect(
      forbiddenFieldAccess.test(searchSrc),
      "searchEntities reads a non-entity (artifact/log/content) field",
    ).toBe(false);
  });

  it("keeps the Command_Palette action set closed to New Run / New Plan (Req 4.10)", () => {
    // Runtime: the built action entries expose exactly new-run and new-plan.
    const ids = buildActionEntries({ onNewRun: () => {}, onNewPlan: () => {} })
      .map((entry) => (entry.kind === "action" ? entry.id : entry.kind))
      .sort();
    expect(ids).toEqual(["new-plan", "new-run"]);

    // Type-level: the CommandEntry `action` variant's id union is frozen.
    const typesSrc = stripComments(readFileSync(typesFile, "utf8"));
    const actionUnion = /kind:\s*"action";\s*id:\s*([^;]+);/.exec(typesSrc);
    expect(actionUnion, "CommandEntry action variant not found").not.toBeNull();
    const literals = Array.from(actionUnion![1].matchAll(/"([^"]+)"/g)).map((m) => m[1]).sort();
    expect(literals).toEqual(["new-plan", "new-run"]);

    // No lifecycle-mutating action id may appear in the action variant.
    const forbiddenActionIds = [
      "approve-intake", "prepare", "compile", "render-brief", "approve-brief",
      "dispatch", "dispatch-executor", "cancel", "cancel-executor", "validate",
      "repair", "generate-audit", "approve-audit", "request-revision",
      "close-run", "delete-artifact",
    ];
    for (const forbidden of forbiddenActionIds) {
      expect(
        actionUnion![1].includes(`"${forbidden}"`),
        `forbidden lifecycle action id "${forbidden}" present in CommandEntry`,
      ).toBe(false);
    }
  });

  it("derives pipeline stage status from canonical `status` only — no display-field gating (Req 6.7, 6.8)", () => {
    const code = stripComments(readFileSync(pipelineFile, "utf8"));

    // The three display fields are unambiguous identifiers; none may appear in
    // the pipeline derivation code.
    for (const field of ["activeStep", "lifecycleState", "statusSeverity"]) {
      expect(
        new RegExp(`\\b${field}\\b`).test(code),
        `pipeline.ts gates/derives on display field \`${field}\``,
      ).toBe(false);
    }
    // `state` is a common word; flag only property-access on a `.state` display
    // field (the canonical field is `status`, never `state`).
    expect(/\.state\b/.test(code), "pipeline.ts reads a `.state` display field").toBe(false);

    // The derivation must be based on the canonical `status` field.
    expect(code.includes("status")).toBe(true);

    // Broader nav sweep: none of the unambiguous display fields leak into any
    // other navigation selector either.
    for (const file of navSources) {
      const navCode = stripComments(readFileSync(file, "utf8"));
      for (const field of ["activeStep", "lifecycleState", "statusSeverity"]) {
        expect(
          new RegExp(`\\b${field}\\b`).test(navCode),
          `${rel(file)} references display field \`${field}\``,
        ).toBe(false);
      }
    }
  });

  it("keeps the attention status set at exactly the closed Req 3.2 / 3.11 enumeration", () => {
    const actual = [...BLOCKED_STATUSES, ...AWAITING_REVIEW_STATUSES].sort();
    const expected = [
      "audit_ready",
      "cancelled",
      "execution_failed",
      "needs_revision",
    ];
    expect(actual).toEqual(expected);
  });
});

// ============================================================
// Change-confinement constraints (Req 10.5, 10.6)
// ============================================================

describe("Task 13.5 — change-confinement constraints", () => {
  it("records the fast-check dependency only in the apps/web package files (Req 10.5)", () => {
    const pkg = JSON.parse(readFileSync(pkgFile, "utf8")) as {
      devDependencies?: Record<string, string>;
      dependencies?: Record<string, string>;
    };
    expect(pkg.devDependencies?.["fast-check"]).toBeTruthy();

    const lock = readFileSync(lockFile, "utf8");
    expect(lock.includes("fast-check")).toBe(true);

    // The repo-root manifests (Go module / npm) must NOT carry the frontend
    // test dependency — it stays confined to apps/web.
    const rootPkgPath = path.join(repoRoot, "package.json");
    if (existsSync(rootPkgPath)) {
      const rootPkg = JSON.parse(readFileSync(rootPkgPath, "utf8")) as {
        devDependencies?: Record<string, string>;
        dependencies?: Record<string, string>;
      };
      expect(rootPkg.devDependencies?.["fast-check"]).toBeUndefined();
      expect(rootPkg.dependencies?.["fast-check"]).toBeUndefined();
    }
  });

  it("does not relocate legacy Go-served surfaces into Relay_Web (Req 10.6)", () => {
    // Instructions / settings / raw artifact viewer remain Go-served: no React
    // route file should exist for them under apps/web/src/routes.
    const routeFiles = collectSources(routesDir).map((f) => rel(f).toLowerCase());
    const relocated = routeFiles.filter((f) =>
      /(instructions|settings|raw-artifact|artifact-viewer)/.test(f),
    );
    expect(
      relocated,
      `legacy Go-served surface appears relocated into a React route:\n${relocated.join("\n")}`,
    ).toEqual([]);
  });
});
