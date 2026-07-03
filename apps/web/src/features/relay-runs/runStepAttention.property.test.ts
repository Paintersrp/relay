// Feature: run-workbench-refinement, Property 4: Attention items — categorization, blocking-state inclusion, and ordering
//
// For any combination of `blockers`, `warnings`, and `revisionRequirements`
// arrays plus a blocked/failed display-state flag with its state-card copy,
// `deriveStepAttention` produces exactly one item per source entry tagged
// with that entry's category ("blocker", "warning", "revision requirement"),
// includes exactly one "blocking state" item carrying the Visual_State_Module
// state-card copy if and only if the display state is blocked or failed,
// orders all items by the fixed category sequence blockers → blocking state
// → revision requirements → warnings while preserving source array order
// within each category, and returns an empty list when all arrays are
// empty/absent and the display state is neither blocked nor failed.
//
// Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.7

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { deriveStepAttention } from "./runStepAttention";
import type { RelayRunStep, StepAttentionInput } from "./runWorkbenchViews";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const currentStepArb: fc.Arbitrary<RelayRunStep> = fc.constantFrom(
  "intake",
  "prepare",
  "execute",
  "audit",
);

// String arrays generated with small-to-moderate sizes, including empty.
const stringArrayArb = fc.array(fc.string({ maxLength: 20 }), { maxLength: 6 });

// Arrays are optionally undefined vs present-but-possibly-empty.
const optionalStringArrayArb: fc.Arbitrary<string[] | undefined> = fc.oneof(
  fc.constant(undefined),
  stringArrayArb,
);

const optionalBlockingStateCopyArb: fc.Arbitrary<string | undefined> = fc.oneof(
  fc.constant(undefined),
  fc.string({ maxLength: 20 }),
);

const inputArb: fc.Arbitrary<StepAttentionInput> = fc.record({
  currentStep: currentStepArb,
  blockers: optionalStringArrayArb,
  warnings: optionalStringArrayArb,
  revisionRequirements: optionalStringArrayArb,
  visualStateIsBlockedOrFailed: fc.boolean(),
  blockingStateCopy: optionalBlockingStateCopyArb,
});

describe("deriveStepAttention — Property 4: categorization, blocking-state inclusion, and ordering", () => {
  it("produces categorized items in fixed order with correct blocking-state inclusion (Req 3.1, 3.2, 3.3, 3.4, 3.5, 3.7)", () => {
    fc.assert(
      fc.property(inputArb, (input) => {
        const blockers = input.blockers ?? [];
        const warnings = input.warnings ?? [];
        const revisionRequirements = input.revisionRequirements ?? [];
        const expectsBlockingState = input.visualStateIsBlockedOrFailed;

        const result = deriveStepAttention(input);

        // 1. Total item count equals sum of input array lengths plus
        //    (1 if blocked/failed else 0).
        const expectedCount =
          blockers.length +
          warnings.length +
          revisionRequirements.length +
          (expectsBlockingState ? 1 : 0);
        expect(result.length).toBe(expectedCount);

        // 2. Every blocker/warning/revisionRequirement entry appears as an
        //    item with the matching category, preserving source order
        //    within category.
        const blockerResultItems = result.filter((item) => item.category === "blocker");
        expect(blockerResultItems.map((item) => item.message)).toEqual(blockers);

        const revisionResultItems = result.filter(
          (item) => item.category === "revision requirement",
        );
        expect(revisionResultItems.map((item) => item.message)).toEqual(
          revisionRequirements,
        );

        const warningResultItems = result.filter((item) => item.category === "warning");
        expect(warningResultItems.map((item) => item.message)).toEqual(warnings);

        // 3. Exactly one "blocking state" item exists iff
        //    visualStateIsBlockedOrFailed is true.
        const blockingStateResultItems = result.filter(
          (item) => item.category === "blocking state",
        );
        if (expectsBlockingState) {
          expect(blockingStateResultItems.length).toBe(1);
          expect(blockingStateResultItems[0].message).toBe(
            input.blockingStateCopy ?? "",
          );
        } else {
          expect(blockingStateResultItems.length).toBe(0);
        }

        // 4. Items appear in the fixed order: all blockers, then
        //    blocking-state (if any), then all revision requirements,
        //    then all warnings.
        const expectedCategoryOrder: string[] = [
          ...blockers.map(() => "blocker"),
          ...(expectsBlockingState ? ["blocking state"] : []),
          ...revisionRequirements.map(() => "revision requirement"),
          ...warnings.map(() => "warning"),
        ];
        expect(result.map((item) => item.category)).toEqual(expectedCategoryOrder);

        // 5. Empty list returned when all arrays empty/absent and not
        //    blocked/failed.
        if (
          blockers.length === 0 &&
          warnings.length === 0 &&
          revisionRequirements.length === 0 &&
          !expectsBlockingState
        ) {
          expect(result).toEqual([]);
        }
      }),
      { numRuns: 100 },
    );
  });
});
