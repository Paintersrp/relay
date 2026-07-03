// Feature: frontend-shell-redesign, Property 3
//
// Property 3: Attention run selection
// Validates: Requirements 3.2
//
// For any list of Runs, `selectAttentionRuns`:
//   - returns only Runs whose canonical `status` is in the CLOSED blocked
//     (`blocked`, `executor_blocked`) or awaiting-review (`intake_needs_review`,
//     `brief_ready_for_review`, `audit_ready`, `audit_ready_for_review`,
//     `revision_required`) set,
//   - returns at most `MAX_ATTENTION_RUNS` (50) items,
//   - reports `totalCount` equal to the full number of qualifying Runs in the
//     input,
//   - and when qualifying <= 50 returns exactly `totalCount` items, while when
//     qualifying > 50 returns exactly 50 items.
//
// The attention set is imported from `statusSets.ts` (single source of truth)
// rather than hardcoded here.

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import {
  MAX_ATTENTION_RUNS,
  selectAttentionRuns,
  type AttentionRunInput,
} from "./attention";
import { AWAITING_REVIEW_STATUSES, BLOCKED_STATUSES } from "./statusSets";

// ------------------------------------------------------------
// Status pools
// ------------------------------------------------------------

// The closed attention classification, imported from the single source of
// truth. A returned item's status must always be a member of this set.
const ATTENTION_STATUSES: readonly string[] = [
  ...BLOCKED_STATUSES,
  ...AWAITING_REVIEW_STATUSES,
];
const ATTENTION_STATUS_SET: ReadonlySet<string> = new Set(ATTENTION_STATUSES);

// Non-attention canonical statuses (a representative sample drawn from the
// canonical `RelayRunStatus` contract, excluding every attention status).
const NON_ATTENTION_STATUSES: readonly string[] = [
  "draft",
  "needs_cleanup",
  "intake_received",
  "validated",
  "approved_for_prepare",
  "packet_validated",
  "approved_for_executor",
  "executor_dispatched",
  "executor_running",
  "executor_done",
  "agent_done",
  "accepted",
  "validation_passed",
  "completed",
];

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const attentionStatusArb: fc.Arbitrary<string> = fc.constantFrom(...ATTENTION_STATUSES);
const nonAttentionStatusArb: fc.Arbitrary<string> = fc.constantFrom(...NON_ATTENTION_STATUSES);

// Arbitrary strings exercise the "unknown status" space; filter out any that
// happen to collide with an attention status so the classification is
// unambiguous for that arm of the mix.
const arbitraryStringArb: fc.Arbitrary<string> = fc
  .string({ minLength: 0, maxLength: 24 })
  .filter((s) => !ATTENTION_STATUS_SET.has(s));

// Draw statuses from a mix of attention, non-attention, and arbitrary strings.
const statusArb: fc.Arbitrary<string> = fc.oneof(
  attentionStatusArb,
  nonAttentionStatusArb,
  arbitraryStringArb,
);

const runArb: fc.Arbitrary<AttentionRunInput> = fc.record({
  id: fc.string({ minLength: 1, maxLength: 12 }),
  label: fc.string({ maxLength: 24 }),
  status: statusArb,
  updatedAt: fc.date({ noInvalidDate: true }).map((d) => d.toISOString()),
});

// Lists large enough to exercise both the under-cap and over-cap regimes.
const runListArb: fc.Arbitrary<AttentionRunInput[]> = fc.array(runArb, {
  minLength: 0,
  maxLength: 120,
});

describe("selectAttentionRuns — Property 3: attention run selection", () => {
  it("returns only Runs whose status is in the closed attention set (Req 3.2)", () => {
    fc.assert(
      fc.property(runListArb, (runs) => {
        const { items } = selectAttentionRuns(runs);
        for (const item of items) {
          expect(ATTENTION_STATUS_SET.has(item.status)).toBe(true);
        }
      }),
      { numRuns: 200 },
    );
  });

  it("returns at most MAX_ATTENTION_RUNS (50) items (Req 3.2)", () => {
    fc.assert(
      fc.property(runListArb, (runs) => {
        const { items } = selectAttentionRuns(runs);
        expect(items.length).toBeLessThanOrEqual(MAX_ATTENTION_RUNS);
      }),
      { numRuns: 200 },
    );
  });

  it("reports totalCount equal to the full number of qualifying Runs (Req 3.2)", () => {
    fc.assert(
      fc.property(runListArb, (runs) => {
        const { totalCount } = selectAttentionRuns(runs);
        const expected = runs.filter((run) => ATTENTION_STATUS_SET.has(run.status)).length;
        expect(totalCount).toBe(expected);
      }),
      { numRuns: 200 },
    );
  });

  it("returns exactly totalCount items when qualifying <= 50, else exactly 50 (Req 3.2)", () => {
    fc.assert(
      fc.property(runListArb, (runs) => {
        const { items, totalCount } = selectAttentionRuns(runs);
        if (totalCount <= MAX_ATTENTION_RUNS) {
          expect(items.length).toBe(totalCount);
        } else {
          expect(items.length).toBe(MAX_ATTENTION_RUNS);
        }
      }),
      { numRuns: 200 },
    );
  });
});
