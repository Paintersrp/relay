// Feature: frontend-shell-redesign, Property 9
//
// Property 9: Short-query search gate
// Validates: Requirements 5.3
//
// For any query whose normalized (trimmed + lowercased) length is under 2
// characters, the gate reports "too-short" and no search is issued over the
// corpus; for queries whose normalized length is >= 2 the gate reports "ok"
// with a normalized value equal to trim+lowercase of the input.
//
// The generators deliberately cover empty, single-character, whitespace-only,
// and >= 2-character inputs so both sides of the MIN_QUERY_LENGTH boundary are
// exercised.

import { describe, expect, it } from "vitest";
import fc from "fast-check";

import {
  MIN_QUERY_LENGTH,
  checkQueryGate,
  isQueryTooShort,
  normalizeQuery,
  searchEntities,
} from "./search";
import type { SearchableEntity } from "./types";

/** A non-empty corpus so "no search issued" is observable as an empty result. */
const nonEmptyCorpusArb: fc.Arbitrary<SearchableEntity[]> = fc.array(
  fc.record({
    type: fc.constantFrom<SearchableEntity["type"]>(
      "project",
      "plan",
      "pass",
      "run",
    ),
    id: fc.string(),
    name: fc.string(),
    to: fc.webPath(),
    params: fc.dictionary(fc.string(), fc.string()),
  }),
  { minLength: 1 },
);

/**
 * Broad query arbitrary spanning both sides of the gate boundary: empty
 * strings, single characters, whitespace-only runs, and arbitrary strings that
 * may normalize to >= 2 characters.
 */
const whitespaceRunArb: fc.Arbitrary<string> = fc
  .array(fc.constantFrom(" ", "\t", "\n", "\r"), { maxLength: 6 })
  .map((chars) => chars.join(""));

const anyQueryArb: fc.Arbitrary<string> = fc.oneof(
  fc.constant(""),
  fc.constant(" "),
  fc.string({ maxLength: 1 }),
  // Whitespace-only strings (normalize to length 0).
  whitespaceRunArb,
  // Single visible char padded with whitespace (normalizes to length 1).
  fc
    .tuple(whitespaceRunArb, fc.string({ minLength: 1, maxLength: 1 }), whitespaceRunArb)
    .map(([lead, c, trail]) => `${lead}${c}${trail}`),
  fc.string(),
);

describe("checkQueryGate — Property 9: short-query search gate", () => {
  it("reports too-short exactly when normalized length < 2, and ok otherwise", () => {
    fc.assert(
      fc.property(anyQueryArb, (query) => {
        const normalizedLength = normalizeQuery(query).length;
        const gate = checkQueryGate(query);

        if (normalizedLength < MIN_QUERY_LENGTH) {
          expect(gate).toEqual({ kind: "too-short" });
          expect(isQueryTooShort(query)).toBe(true);
        } else {
          expect(gate.kind).toBe("ok");
          if (gate.kind === "ok") {
            expect(gate.normalized).toBe(query.trim().toLowerCase());
          }
          expect(isQueryTooShort(query)).toBe(false);
        }
      }),
      { numRuns: 200 },
    );
  });

  it("issues no search over a non-empty corpus for too-short queries", () => {
    fc.assert(
      fc.property(anyQueryArb, nonEmptyCorpusArb, (query, corpus) => {
        if (normalizeQuery(query).length < MIN_QUERY_LENGTH) {
          // Gate short-circuits: empty result even though the corpus is non-empty.
          expect(searchEntities(query, corpus)).toEqual([]);
        }
      }),
      { numRuns: 200 },
    );
  });
});
