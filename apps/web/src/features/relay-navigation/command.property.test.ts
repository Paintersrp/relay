// Feature: frontend-shell-redesign, Property 6
//
// Property 6: Command-filter case-insensitive substring
// Validates: Requirements 4.4
//
// For any query string and any set of command entries, `filterCommandEntries`
// returns exactly the entries whose visible label contains the query as a
// case-insensitive substring — no qualifying entry is omitted and no
// non-qualifying entry is included. An empty query returns all entries.

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { filterCommandEntries } from "./command";
import type { CommandEntry } from "./types";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

// A label arbitrary that mixes casing, letters, digits, whitespace, and some
// unicode so the case-insensitive comparison is exercised across variety.
const labelArb = fc.string({ minLength: 0, maxLength: 20 });

// Generate an arbitrary CommandEntry across all three variants (nav-domain,
// nav-recent, action) with arbitrary labels. Only the `label` field matters
// for filtering, but we vary the full shape to reflect the real entry space.
const commandEntryArb: fc.Arbitrary<CommandEntry> = fc.oneof(
  labelArb.map(
    (label): CommandEntry => ({
      kind: "nav-domain",
      id: "projects",
      label,
      to: "/projects",
    }),
  ),
  labelArb.map(
    (label): CommandEntry => ({
      kind: "nav-recent",
      entity: "run",
      id: "r1",
      label,
      to: "/runs/$runId",
      params: { runId: "r1" },
    }),
  ),
  labelArb.map(
    (label): CommandEntry => ({
      kind: "action",
      id: "new-run",
      label,
      run: () => {},
    }),
  ),
);

const entriesArb = fc.array(commandEntryArb, { maxLength: 30 });

// A query arbitrary that includes mixed-case strings, the empty string, and
// strings likely and unlikely to match generated labels.
const queryArb = fc.oneof(
  fc.string({ minLength: 0, maxLength: 8 }),
  fc.constant(""),
);

// Reference implementation of the case-insensitive substring predicate.
function referenceMatches(query: string, label: string): boolean {
  return label.toLowerCase().includes(query.toLowerCase());
}

describe("filterCommandEntries — Property 6: case-insensitive substring", () => {
  it("returns exactly the entries whose label contains the query case-insensitively", () => {
    fc.assert(
      fc.property(queryArb, entriesArb, (query, entries) => {
        const result = filterCommandEntries(query, entries);

        // The reference set: entries qualifying under the case-insensitive
        // substring predicate, preserving input order.
        const expected = entries.filter((entry) => referenceMatches(query, entry.label));

        // Exactly the qualifying entries, in the same order — no qualifying
        // entry omitted and no non-qualifying entry included.
        expect(result).toEqual(expected);

        // Every returned entry must satisfy the predicate (soundness).
        for (const entry of result) {
          expect(referenceMatches(query, entry.label)).toBe(true);
        }

        // Every non-returned entry must NOT satisfy the predicate
        // (completeness).
        const returnedSet = new Set(result);
        for (const entry of entries) {
          if (!returnedSet.has(entry)) {
            expect(referenceMatches(query, entry.label)).toBe(false);
          }
        }
      }),
      { numRuns: 300 },
    );
  });

  it("returns all entries for an empty query", () => {
    fc.assert(
      fc.property(entriesArb, (entries) => {
        expect(filterCommandEntries("", entries)).toEqual(entries);
      }),
      { numRuns: 100 },
    );
  });

  it("matches regardless of query casing when the label contains the substring", () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1, maxLength: 6 }),
        labelArb,
        labelArb,
        (needle, prefix, suffix) => {
          // Construct a label guaranteed to contain `needle` (in its own
          // casing). Any casing of the query for that same substring must
          // still match.
          const label = `${prefix}${needle}${suffix}`;
          const entry: CommandEntry = {
            kind: "nav-domain",
            id: "runs",
            label,
            to: "/runs",
          };

          const upperResult = filterCommandEntries(needle.toUpperCase(), [entry]);
          const lowerResult = filterCommandEntries(needle.toLowerCase(), [entry]);

          expect(upperResult).toEqual([entry]);
          expect(lowerResult).toEqual([entry]);
        },
      ),
      { numRuns: 200 },
    );
  });
});
