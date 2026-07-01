// Feature: frontend-shell-redesign, Property 8
//
// Property 8: Entity search matching and cap
// Validates: Requirements 5.2 (and 5.8, by confirming matching never extends
// beyond `name`/`id`).
//
// The corpus under test is limited to the entity search corpus — Projects,
// Plans, Passes, and Runs names/titles/labels/identifiers. We deliberately do
// NOT generate or assert against any artifact/log/source/content fields; the
// `SearchableEntity` shape structurally carries only `name` and `id` match
// fields (plus `type`/`to`/`params` for validity).

import { describe, expect, it } from "vitest";
import fc from "fast-check";

import { MAX_SEARCH_RESULTS, searchEntities } from "./search";
import type { SearchableEntity } from "./types";

const entityType = fc.constantFrom<SearchableEntity["type"]>(
  "project",
  "plan",
  "pass",
  "run",
);

/**
 * Arbitrary for a single `SearchableEntity`. Only `name` and `id` are the match
 * fields; `to`/`params` are generated for validity but never asserted against.
 */
function entityArb(nameArb: fc.Arbitrary<string>): fc.Arbitrary<SearchableEntity> {
  return fc.record({
    type: entityType,
    id: fc.string(),
    name: nameArb,
    to: fc.webPath(),
    params: fc.dictionary(fc.string(), fc.string()),
  });
}

/** Queries of at least 2 characters, matching the gate's minimum. */
const matchingQueryArb = fc
  .string({ minLength: 2 })
  .filter((q) => q.trim().toLowerCase().length >= 2);

function matchesQuery(entity: SearchableEntity, normalized: string): boolean {
  return (
    entity.name.toLowerCase().includes(normalized) ||
    entity.id.toLowerCase().includes(normalized)
  );
}

describe("searchEntities — Property 8: entity search matching and cap", () => {
  it("returns only entities whose name or id contains the query (case-insensitive), never exceeding the cap", () => {
    fc.assert(
      fc.property(
        fc.array(entityArb(fc.string())),
        matchingQueryArb,
        (corpus, query) => {
          const normalized = query.trim().toLowerCase();
          const results = searchEntities(query, corpus);

          // Result count never exceeds the cap.
          expect(results.length).toBeLessThanOrEqual(MAX_SEARCH_RESULTS);

          // Every returned entity matches on name or id (never any other field).
          for (const { entity } of results) {
            expect(matchesQuery(entity, normalized)).toBe(true);
          }
        },
      ),
      { numRuns: 200 },
    );
  });

  it("includes every qualifying entity when the qualifying count is at or below the cap", () => {
    fc.assert(
      fc.property(
        fc.array(entityArb(fc.string())),
        matchingQueryArb,
        (corpus, query) => {
          const normalized = query.trim().toLowerCase();
          const qualifying = corpus.filter((e) => matchesQuery(e, normalized));
          const results = searchEntities(query, corpus);

          if (qualifying.length <= MAX_SEARCH_RESULTS) {
            // No qualifying entity is excluded.
            expect(results.length).toBe(qualifying.length);
          } else {
            // Otherwise the cap is hit exactly.
            expect(results.length).toBe(MAX_SEARCH_RESULTS);
          }
        },
      ),
      { numRuns: 200 },
    );
  });

  it("caps at 50 results even when far more than 50 entities match", () => {
    // Build a corpus guaranteed to have > 50 matches by embedding the query
    // substring into each entity name.
    const arb = fc
      .tuple(
        fc.string({ minLength: 2 }).filter((q) => q.trim().toLowerCase().length >= 2),
        fc.integer({ min: MAX_SEARCH_RESULTS + 1, max: MAX_SEARCH_RESULTS + 40 }),
      )
      .chain(([query, count]) =>
        fc
          .array(fc.string(), { minLength: count, maxLength: count })
          .map((suffixes) => {
            const corpus: SearchableEntity[] = suffixes.map((suffix, i) => ({
              type: "run" as const,
              id: `id-${i}`,
              name: `${suffix}${query}${suffix}`,
              to: `/runs/${i}`,
              params: {},
            }));
            return { query, corpus };
          }),
      );

    fc.assert(
      fc.property(arb, ({ query, corpus }) => {
        const results = searchEntities(query, corpus);
        expect(results.length).toBe(MAX_SEARCH_RESULTS);
      }),
      { numRuns: 100 },
    );
  });
});
