// Feature: frontend-shell-redesign, Property 4
//
// Property 4: Recent-activity selection
// Validates: Requirements 3.3
//
// For any mixed list of Runs / Plans / Projects (as `RecentActivityItem`),
// `selectRecentActivity`:
//   - Returns at most `MAX_RECENT_ACTIVITY` (10) items.
//   - Returns items ordered by `updatedAt` descending (non-increasing).
//   - Returns exactly the prefix of the input sorted by `updatedAt` descending
//     — i.e. the 10 most recently updated items. Ties on `updatedAt` are handled
//     tie-robustly: the least-recent returned item is no older than the
//     most-recent excluded item.

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { MAX_RECENT_ACTIVITY, selectRecentActivity } from "./attention";
import type { RecentActivityItem } from "./types";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

// Draw `updatedAt` from a bounded window of epoch-millis so that the generated
// lists frequently contain repeated timestamps. This deliberately exercises the
// tie-handling behavior of the descending sort/prefix selection.
const updatedAtArb: fc.Arbitrary<string> = fc
  .integer({ min: 0, max: 20 })
  // Base epoch: 2023-01-01T00:00:00.000Z, stepped by whole days.
  .map((dayOffset) => new Date(1672531200000 + dayOffset * 86400000).toISOString());

const recentActivityItemArb: fc.Arbitrary<RecentActivityItem> = fc
  .record({
    type: fc.constantFrom<"run" | "plan" | "project">("run", "plan", "project"),
    id: fc.string({ minLength: 1, maxLength: 8 }),
    label: fc.string({ minLength: 0, maxLength: 20 }),
    updatedAt: updatedAtArb,
  })
  .map(({ type, id, label, updatedAt }): RecentActivityItem => {
    // Build a type-appropriate navigation target/params so the generated items
    // reflect the real mixed shape the selector receives.
    switch (type) {
      case "run":
        return { type, id, label, updatedAt, to: "/runs/$runId", params: { runId: id } };
      case "plan":
        return { type, id, label, updatedAt, to: "/plans/$planId", params: { planId: id } };
      case "project":
        return {
          type,
          id,
          label,
          updatedAt,
          to: "/projects/$projectId",
          params: { projectId: id },
        };
    }
  });

// Vary list length across, at, and beyond the cap so both the "fewer than cap"
// and "overflow" branches are covered.
const itemsArb = fc.array(recentActivityItemArb, { minLength: 0, maxLength: 40 });

function millis(updatedAt: string): number {
  return new Date(updatedAt).getTime();
}

describe("selectRecentActivity — Property 4: recent-activity selection", () => {
  it("returns at most MAX_RECENT_ACTIVITY items ordered by updatedAt descending, equal to the top-N prefix", () => {
    fc.assert(
      fc.property(itemsArb, (items) => {
        const result = selectRecentActivity(items);

        // Length is exactly min(input length, cap).
        expect(result).toHaveLength(Math.min(items.length, MAX_RECENT_ACTIVITY));

        // Result is non-increasing by updatedAt.
        for (let i = 1; i < result.length; i++) {
          expect(millis(result[i - 1].updatedAt)).toBeGreaterThanOrEqual(
            millis(result[i].updatedAt),
          );
        }

        // Result equals the top-N: every excluded item is no more recent than
        // every returned item (tie-robust boundary check). Concretely, the
        // least-recent returned item is no older than the most-recent excluded
        // item.
        const returnedSet = new Set(result);
        const excluded = items.filter((item) => !returnedSet.has(item));

        if (result.length > 0 && excluded.length > 0) {
          const leastRecentReturned = Math.min(...result.map((r) => millis(r.updatedAt)));
          const mostRecentExcluded = Math.max(...excluded.map((e) => millis(e.updatedAt)));
          expect(leastRecentReturned).toBeGreaterThanOrEqual(mostRecentExcluded);
        }

        // The result is a multiset-subset of the input (no fabricated items):
        // every returned item is present in the input.
        for (const item of result) {
          expect(items).toContain(item);
        }
      }),
      { numRuns: 300 },
    );
  });

  it("returns all items unchanged in count when input is at or below the cap", () => {
    fc.assert(
      fc.property(
        fc.array(recentActivityItemArb, { minLength: 0, maxLength: MAX_RECENT_ACTIVITY }),
        (items) => {
          const result = selectRecentActivity(items);
          expect(result).toHaveLength(items.length);
          // Same multiset of items — sorting reorders but never drops/adds.
          const byJson = (a: RecentActivityItem, b: RecentActivityItem) =>
            JSON.stringify(a).localeCompare(JSON.stringify(b));
          expect([...result].sort(byJson)).toEqual([...items].sort(byJson));
        },
      ),
      { numRuns: 150 },
    );
  });
});
