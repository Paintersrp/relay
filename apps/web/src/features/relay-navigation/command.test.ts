// Feature: frontend-shell-redesign, Property 5
// ============================================================
// Property test — Command-palette recents selection
// ============================================================
//
// Property 5: Command-palette recents selection
// Validates: Requirements 4.2
//
// For any corpora of Runs, Plans, and Projects, the palette recents builder
// returns at most 5 entries per entity group, each group's entries are drawn
// exactly from the 5 most recently updated items of that group by `updatedAt`
// descending, and the three primary-domain navigation entries are always
// present in the fully assembled command entries.

import { describe, expect, it } from "vitest";
import fc from "fast-check";

import {
  PRIMARY_DOMAIN_NAV_ENTRIES,
  RECENTS_PER_GROUP,
  buildCommandEntries,
  buildRecentEntries,
  type CommandActionHandlers,
  type RecentEntityCorpora,
  type RecentEntityInput,
} from "./command";
import type { CommandEntry } from "./types";

// ------------------------------------------------------------
// Generators
// ------------------------------------------------------------

// Timestamp span: epoch (1970) through ~year 2100. Kept as arbitrary millis so
// generated corpora exercise ties and interleaved orderings.
const MIN_MILLIS = 0;
const MAX_MILLIS = 4_102_444_800_000;

/**
 * Builds an arbitrary group of recent entities with unique ids (so returned
 * entries can be unambiguously matched back to their `updatedAt`). Timestamps
 * are free to collide, exercising the stable-sort / tie behavior.
 */
function groupArb(idPrefix: string): fc.Arbitrary<RecentEntityInput[]> {
  return fc
    .array(
      fc.record({
        label: fc.string(),
        millis: fc.integer({ min: MIN_MILLIS, max: MAX_MILLIS }),
      }),
      { maxLength: 20 },
    )
    .map((items) =>
      items.map((item, index) => ({
        id: `${idPrefix}-${index}`,
        label: item.label,
        updatedAt: new Date(item.millis).toISOString(),
      })),
    );
}

const corporaArb: fc.Arbitrary<RecentEntityCorpora> = fc.record({
  runs: groupArb("run"),
  plans: groupArb("plan"),
  projects: groupArb("project"),
});

// ------------------------------------------------------------
// Helpers
// ------------------------------------------------------------

const millisOf = (updatedAt: string): number => new Date(updatedAt).getTime();

/**
 * Asserts that `returned` is a valid "top-N most recently updated" selection of
 * `input`: correct count, sorted descending, and every returned item is at
 * least as recent as every excluded item. This formulation is robust to ties
 * in `updatedAt` (which item wins a tie is not asserted).
 */
function assertNewestSelection(input: RecentEntityInput[], returned: CommandEntry[]): void {
  const expectedCount = Math.min(input.length, RECENTS_PER_GROUP);
  expect(returned.length).toBe(expectedCount);

  const millisById = new Map(input.map((item) => [item.id, millisOf(item.updatedAt)]));
  const returnedIds = new Set<string>();
  const returnedMillis: number[] = [];

  for (const entry of returned) {
    expect(entry.kind).toBe("nav-recent");
    if (entry.kind !== "nav-recent") continue;
    // Every returned entry must originate from the input group.
    expect(millisById.has(entry.id)).toBe(true);
    returnedIds.add(entry.id);
    returnedMillis.push(millisById.get(entry.id) as number);
  }

  // No duplicate entries.
  expect(returnedIds.size).toBe(returned.length);

  // Returned entries are ordered by `updatedAt` descending (non-increasing).
  for (let i = 1; i < returnedMillis.length; i++) {
    expect(returnedMillis[i - 1]).toBeGreaterThanOrEqual(returnedMillis[i]);
  }

  // Boundary invariant: the least-recent returned item is at least as recent as
  // the most-recent excluded item — i.e. the selection is exactly the newest.
  const excluded = input.filter((item) => !returnedIds.has(item.id));
  if (excluded.length > 0 && returnedMillis.length > 0) {
    const minReturned = Math.min(...returnedMillis);
    const maxExcluded = Math.max(...excluded.map((item) => millisOf(item.updatedAt)));
    expect(minReturned).toBeGreaterThanOrEqual(maxExcluded);
  }
}

const noopHandlers: CommandActionHandlers = {
  onNewRun: () => {},
  onNewPlan: () => {},
};

// ------------------------------------------------------------
// Property
// ------------------------------------------------------------

describe("Property 5: Command-palette recents selection", () => {
  it("returns at most 5 newest entries per group and always includes the primary-domain nav entries", () => {
    fc.assert(
      fc.property(corporaArb, (corpora) => {
        const recents = buildRecentEntries(corpora);

        const runEntries = recents.filter(
          (e) => e.kind === "nav-recent" && e.entity === "run",
        );
        const planEntries = recents.filter(
          (e) => e.kind === "nav-recent" && e.entity === "plan",
        );
        const projectEntries = recents.filter(
          (e) => e.kind === "nav-recent" && e.entity === "project",
        );

        // At most 5 per group, drawn exactly from the newest of that group.
        assertNewestSelection(corpora.runs, runEntries);
        assertNewestSelection(corpora.plans, planEntries);
        assertNewestSelection(corpora.projects, projectEntries);

        // recents contains only nav-recent entries (no stray kinds).
        expect(recents.length).toBe(
          runEntries.length + planEntries.length + projectEntries.length,
        );

        // The three primary-domain nav entries are always present in the fully
        // assembled command entries.
        const all = buildCommandEntries(corpora, noopHandlers);
        for (const navEntry of PRIMARY_DOMAIN_NAV_ENTRIES) {
          expect(
            all.some(
              (e) =>
                e.kind === "nav-domain" &&
                navEntry.kind === "nav-domain" &&
                e.id === navEntry.id &&
                e.label === navEntry.label &&
                e.to === navEntry.to,
            ),
          ).toBe(true);
        }
      }),
      { numRuns: 200 },
    );
  });

  it("exposes exactly the three primary domains (Projects, Plans, Runs)", () => {
    const domainIds = PRIMARY_DOMAIN_NAV_ENTRIES.map((e) =>
      e.kind === "nav-domain" ? e.id : null,
    );
    expect(domainIds).toEqual(["projects", "plans", "runs"]);
  });
});
