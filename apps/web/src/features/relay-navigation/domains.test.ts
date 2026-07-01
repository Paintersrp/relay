import { describe, expect, it } from "vitest";
import fc from "fast-check";

import { PRIMARY_DOMAINS, resolveActiveDomain } from "./domains";
import type { PrimaryDomain } from "./types";

// The three primary-domain identifiers, derived from the registry so the test
// stays in sync with the source of truth.
const DOMAIN_IDS = PRIMARY_DOMAINS.map((d) => d.id);
const DOMAIN_NAMES = PRIMARY_DOMAINS.map((d) => d.basePath.replace(/^\//, ""));

// ---------------------------------------------------------------------------
// Arbitrary building blocks
// ---------------------------------------------------------------------------

// A single URL path segment made only of safe characters (no "/", "?" or "#"),
// so composing segments never accidentally changes the path structure.
const segmentChar = fc.constantFrom(
  ..."abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_".split(
    "",
  ),
);
const segment = fc
  .array(segmentChar, { minLength: 1, maxLength: 12 })
  .map((cs) => cs.join(""));

// Optional query string / hash fragment suffixes that must not affect matching.
const suffix = fc.constantFrom(
  "",
  "?",
  "?q=hello",
  "?a=1&b=2",
  "#top",
  "?filter=blocked#section",
);

const trailingSlash = fc.boolean();

// Independent (non-SUT) computation of the first path segment, used only to
// classify generated non-domain pathnames without reusing resolveActiveDomain.
function firstSegmentOf(pathname: string): string {
  const clean = pathname.split(/[?#]/)[0] ?? "";
  const trimmed = clean.replace(/\/+$/, "");
  const segs = trimmed.split("/").filter(Boolean);
  return segs[0] ?? "";
}

// A pathname guaranteed to be owned by one primary domain: the domain base,
// optionally followed by nested segments, an optional trailing slash, and an
// optional query/hash suffix.
const domainOwnedPath = fc
  .tuple(
    fc.constantFrom(...PRIMARY_DOMAINS),
    fc.array(segment, { maxLength: 4 }),
    trailingSlash,
    suffix,
  )
  .map(([domain, rest, slash, suf]) => {
    let path = domain.basePath;
    if (rest.length > 0) {
      path += `/${rest.join("/")}`;
    }
    if (slash) {
      path += "/";
    }
    return { pathname: path + suf, expected: domain.id as PrimaryDomain };
  });

// A pathname guaranteed NOT to belong to any primary domain. The first segment
// is drawn from non-domain constants (including look-alikes such as
// "projects-archive") or a random segment, then filtered to exclude the exact
// domain names.
const nonDomainPath = fc
  .tuple(
    fc.oneof(
      fc.constantFrom(
        "settings",
        "about",
        "home",
        "dashboard",
        "projects-archive",
        "plans-old",
        "runsx",
        "project",
        "plan",
        "run",
      ),
      segment,
    ),
    fc.array(segment, { maxLength: 3 }),
    trailingSlash,
    suffix,
  )
  .map(([first, rest, slash, suf]) => {
    const parts = [first, ...rest].filter((p) => p.length > 0);
    let path = `/${parts.join("/")}`;
    if (slash && path.length > 1) {
      path += "/";
    }
    return path + suf;
  })
  .filter((p) => !DOMAIN_NAMES.includes(firstSegmentOf(p)));

// ---------------------------------------------------------------------------
// Property 1: Active-domain resolution
// Validates: Requirements 1.4, 1.5
// ---------------------------------------------------------------------------

describe("resolveActiveDomain — Property 1: Active-domain resolution", () => {
  // Feature: frontend-shell-redesign, Property 1
  it("returns exactly the one owning domain for any domain-owned pathname (Req 1.4)", () => {
    fc.assert(
      fc.property(domainOwnedPath, ({ pathname, expected }) => {
        const active = resolveActiveDomain(pathname);

        // Exactly the owning domain is active ...
        expect(active).toBe(expected);
        // ... and no other primary domain is reported active.
        for (const id of DOMAIN_IDS) {
          if (id !== expected) {
            expect(active).not.toBe(id);
          }
        }
      }),
      { numRuns: 200 },
    );
  });

  // Feature: frontend-shell-redesign, Property 1
  it("returns null for any pathname that belongs to no primary domain (Req 1.5)", () => {
    fc.assert(
      fc.property(nonDomainPath, (pathname) => {
        expect(resolveActiveDomain(pathname)).toBeNull();
      }),
      { numRuns: 200 },
    );
  });
});

// ---------------------------------------------------------------------------
// Example-based edge cases complementing the property (look-alikes, root,
// trailing slashes, query strings).
// ---------------------------------------------------------------------------

describe("resolveActiveDomain — edge cases", () => {
  it("resolves each domain root exactly", () => {
    expect(resolveActiveDomain("/projects")).toBe("projects");
    expect(resolveActiveDomain("/plans")).toBe("plans");
    expect(resolveActiveDomain("/runs")).toBe("runs");
  });

  it("resolves nested domain routes to the owning domain", () => {
    expect(resolveActiveDomain("/runs/abc/intake")).toBe("runs");
    expect(resolveActiveDomain("/plans/p1/passes/x")).toBe("plans");
    expect(resolveActiveDomain("/projects/new")).toBe("projects");
  });

  it("returns null for the application root", () => {
    expect(resolveActiveDomain("/")).toBeNull();
  });

  it("returns null for non-domain authenticated routes", () => {
    expect(resolveActiveDomain("/settings")).toBeNull();
  });

  it("does not match domain look-alike prefixes", () => {
    expect(resolveActiveDomain("/projects-archive")).toBeNull();
    expect(resolveActiveDomain("/plans-old")).toBeNull();
    expect(resolveActiveDomain("/runsx")).toBeNull();
  });

  it("ignores trailing slashes, query strings, and hash fragments", () => {
    expect(resolveActiveDomain("/runs/")).toBe("runs");
    expect(resolveActiveDomain("/runs?status=blocked")).toBe("runs");
    expect(resolveActiveDomain("/plans#top")).toBe("plans");
    expect(resolveActiveDomain("/projects/123/?tab=x#h")).toBe("projects");
  });
});
