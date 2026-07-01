// Feature: frontend-shell-redesign, Property 2
//
// Property 2: Breadcrumb well-formedness
// Validates: Requirements 2.1, 2.2, 2.3, 2.6, 2.7
//
// For any resolved hierarchy (any subset of Project, Plan, Pass, Run present),
// buildBreadcrumbSegments produces segments that are
//   (a) strictly ordered root-to-leaf by level rank Project < Plan < Pass < Run,
//   (b) a subset of the levels actually present in the input with no fabricated
//       or placeholder ancestors,
//   (c) every non-final segment navigable (defined `to`), and
//   (d) the final segment non-navigable (no `to`).
// In particular, a standalone Run with no Plan/Pass yields no Plan/Pass segments.
//
// Note: the implementation omits a present ancestor level whose route cannot be
// resolved (e.g. a pass present but no plan id). The output levels are therefore
// a *subset* of the present levels (not necessarily equal). This test asserts the
// invariants that hold universally: ordering, subset-of-present, no fabrication,
// non-final navigable, final non-navigable.

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { buildBreadcrumbSegments } from "./breadcrumb";
import type { BreadcrumbSegment, ResolvedHierarchy } from "./types";

type Level = BreadcrumbSegment["level"];

const LEVEL_RANK: Record<Level, number> = {
  project: 0,
  plan: 1,
  pass: 2,
  run: 3,
};

// A non-empty label generator so that "present" (label !== undefined) is
// well-defined. Ids are allowed to be empty strings to exercise the
// unresolvable-ancestor path (an ancestor with no usable id is omitted).
const label = fc.string({ minLength: 1, maxLength: 12 });
const id = fc.string({ maxLength: 12 });

const projectArb = fc.option(fc.record({ id, label }), { nil: undefined });
const planArb = fc.option(fc.record({ id, label }), { nil: undefined });
const passArb = fc.option(
  fc.record({ id, label, sequence: fc.integer({ min: 0, max: 99 }) }),
  { nil: undefined },
);
const runArb = fc.option(fc.record({ id, label }), { nil: undefined });

const hierarchyArb: fc.Arbitrary<ResolvedHierarchy> = fc.record({
  project: projectArb,
  plan: planArb,
  pass: passArb,
  run: runArb,
});

/** Levels actually present (label defined) in the resolved hierarchy. */
function presentLevels(resolved: ResolvedHierarchy): Set<Level> {
  const set = new Set<Level>();
  if (resolved.project !== undefined) set.add("project");
  if (resolved.plan !== undefined) set.add("plan");
  if (resolved.pass !== undefined) set.add("pass");
  if (resolved.run !== undefined) set.add("run");
  return set;
}

describe("buildBreadcrumbSegments — Property 2: well-formedness", () => {
  it("produces ordered, present-subset, correctly-navigable segments for any hierarchy", () => {
    fc.assert(
      fc.property(hierarchyArb, (resolved) => {
        const segments = buildBreadcrumbSegments(resolved);
        const present = presentLevels(resolved);

        // (a) strictly ordered root-to-leaf by level rank.
        for (let i = 1; i < segments.length; i++) {
          expect(LEVEL_RANK[segments[i].level]).toBeGreaterThan(
            LEVEL_RANK[segments[i - 1].level],
          );
        }

        // (b) subset of present levels, no fabrication / no duplicates.
        const seen = new Set<Level>();
        for (const segment of segments) {
          expect(present.has(segment.level)).toBe(true);
          expect(seen.has(segment.level)).toBe(false);
          seen.add(segment.level);
        }

        if (segments.length === 0) {
          // No present level can be rendered only when nothing is present,
          // OR every present level was an unresolvable ancestor. Either way the
          // leaf level (last present) is always renderable, so an empty output
          // implies no present levels at all.
          expect(present.size).toBe(0);
          return;
        }

        // (c) every non-final segment is a navigable ancestor.
        for (let i = 0; i < segments.length - 1; i++) {
          expect(typeof segments[i].to).toBe("string");
          expect(segments[i].params).toBeDefined();
        }

        // (d) the final segment is the non-navigable current leaf.
        const leaf = segments[segments.length - 1];
        expect(leaf.to).toBeUndefined();
      }),
      { numRuns: 300 },
    );
  });

  it("renders a standalone Run without Plan or Pass placeholders", () => {
    const segments = buildBreadcrumbSegments({
      run: { id: "run-1", label: "Standalone Run" },
    });

    expect(segments.map((s) => s.level)).toEqual(["run"]);
    expect(segments[0].to).toBeUndefined();
  });

  it("returns no segments for an empty hierarchy", () => {
    expect(buildBreadcrumbSegments({})).toEqual([]);
  });
});
