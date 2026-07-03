// Feature: run-status-tracker-redesign, Property 5: Progression log fidelity
//
// For any arbitrary `RelayRunEvent[]`, `deriveProgressionLog` returns output
// whose length equals the input length (no dropping, no synthesizing), each
// output entry's `timestamp` equals its corresponding input event's
// `createdAt`, the output is ordered most-recent-first by `timestamp`, and
// each entry's `tone` matches `classifyEventTone` applied to the
// corresponding input event. Empty input yields `[]`.
//
// Validates: Requirements 4.1, 4.2, 4.6, 4.7

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import {
  classifyEventTone,
  deriveProgressionLog,
} from "./deriveProgressionLog";
import {
  AWAITING_REVIEW_STATUSES,
  BLOCKED_STATUSES,
} from "../relay-navigation/statusSets";
import type { RelayRunEvent, RelayRunEventKind } from "./types";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const EVENT_KINDS: RelayRunEventKind[] = [
  "log",
  "status_change",
  "artifact_created",
  "validation_run",
  "step_transition",
];

// Target status arbitrary spanning blocked statuses, awaiting-review
// statuses, other arbitrary strings, and "absent" (undefined) so
// `classifyEventTone`'s three branches (danger / warning / neutral) are all
// exercised, including the neutral fallback when `details.status` is
// missing or not a string.
const targetStatusArb: fc.Arbitrary<string | undefined> = fc.oneof(
  fc.constantFrom(...BLOCKED_STATUSES),
  fc.constantFrom(...AWAITING_REVIEW_STATUSES),
  fc.string(),
  fc.constant(undefined),
);

// ISO-8601 timestamp arbitrary. Uses a bounded date range so timestamps
// remain valid and comparable via Date.parse.
const isoTimestampArb: fc.Arbitrary<string> = fc
  .date({
    min: new Date("2020-01-01T00:00:00.000Z"),
    max: new Date("2030-01-01T00:00:00.000Z"),
    noInvalidDate: true,
  })
  .map((d) => d.toISOString());

function eventArb(id: string): fc.Arbitrary<RelayRunEvent> {
  return fc.record({
    id: fc.constant(id),
    runId: fc.constant("run-1"),
    kind: fc.constantFrom(...EVENT_KINDS),
    message: fc.string(),
    createdAt: isoTimestampArb,
    details: fc.oneof(
      fc.constant(undefined),
      targetStatusArb.map((status) =>
        status === undefined ? {} : { status },
      ),
    ),
  });
}

// Array of events with unique ids (required to correlate output entries
// back to their originating input event after sorting).
const eventsArb: fc.Arbitrary<RelayRunEvent[]> = fc
  .integer({ min: 0, max: 20 })
  .chain((length) =>
    fc.tuple(
      ...Array.from({ length }, (_, index) => eventArb(`event-${index}`)),
    ),
  )
  .map((events) => [...events]);

describe("deriveProgressionLog — Property 5: Progression log fidelity", () => {
  it("preserves length, timestamps, tone classification, and most-recent-first order for arbitrary event arrays (Req 4.1, 4.2, 4.6, 4.7)", () => {
    fc.assert(
      fc.property(eventsArb, (events) => {
        const result = deriveProgressionLog(events);

        // (1) No dropping, no synthesizing — output length equals input length.
        expect(result).toHaveLength(events.length);

        const eventsById = new Map(events.map((event) => [event.id, event]));

        // Every output entry corresponds to a real input event (no synthesized
        // entries), and every input event is represented exactly once.
        const outputIds = result.map((entry) => entry.id);
        expect(new Set(outputIds)).toEqual(new Set(events.map((e) => e.id)));
        expect(outputIds).toHaveLength(events.length);

        for (const entry of result) {
          const sourceEvent = eventsById.get(entry.id);
          expect(sourceEvent).toBeDefined();

          // (2) timestamp equals the corresponding input event's createdAt.
          expect(entry.timestamp).toBe(sourceEvent!.createdAt);

          // (4) tone matches classifyEventTone applied to the corresponding
          // input event.
          expect(entry.tone).toBe(classifyEventTone(sourceEvent!));
        }

        // (3) Output is sorted most-recent-first by timestamp.
        for (let i = 0; i < result.length - 1; i++) {
          const current = Date.parse(result[i].timestamp);
          const next = Date.parse(result[i + 1].timestamp);
          expect(current).toBeGreaterThanOrEqual(next);
        }
      }),
      { numRuns: 100 },
    );
  });

  it("returns [] for empty input (Req 4.6)", () => {
    expect(deriveProgressionLog([])).toEqual([]);
  });
});
