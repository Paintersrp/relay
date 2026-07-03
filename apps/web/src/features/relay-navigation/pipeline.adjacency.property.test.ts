// Feature: frontend-shell-redesign, Property 7
//
// Property 7: Clamped stage adjacency
// Validates: Requirements 4.8, 4.9
//
// For any stage in `PIPELINE_STAGE_ORDER` and any direction ("next" |
// "previous"), `adjacentStage` returns the immediately adjacent stage in
// pipeline order, clamped to the range [0, len - 1]:
//   - "previous" from the first stage (Intake) stays Intake.
//   - "next" from the last stage (Audit) stays Audit.
// The result is always one of the four valid, in-range stages.

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { adjacentStage, PIPELINE_STAGE_ORDER } from "./pipeline";

// ------------------------------------------------------------
// Independent reference implementation
// ------------------------------------------------------------

// Compute the expected adjacent stage from first principles: locate the stage's
// index, step +/- 1 based on direction, then clamp into [0, len - 1]. This is
// deliberately independent of the implementation under test.
function expectedAdjacent(
  current: (typeof PIPELINE_STAGE_ORDER)[number],
  direction: "next" | "previous",
): (typeof PIPELINE_STAGE_ORDER)[number] {
  const index = PIPELINE_STAGE_ORDER.indexOf(current);
  const stepped = direction === "next" ? index + 1 : index - 1;
  const clamped = Math.min(Math.max(stepped, 0), PIPELINE_STAGE_ORDER.length - 1);
  return PIPELINE_STAGE_ORDER[clamped];
}

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const stageArb: fc.Arbitrary<(typeof PIPELINE_STAGE_ORDER)[number]> = fc.constantFrom(
  ...PIPELINE_STAGE_ORDER,
);

const directionArb: fc.Arbitrary<"next" | "previous"> = fc.constantFrom(
  "next",
  "previous",
);

describe("adjacentStage — Property 7: clamped stage adjacency", () => {
  it("returns the immediately adjacent stage, clamped, matching an independent reference", () => {
    fc.assert(
      fc.property(stageArb, directionArb, (current, direction) => {
        const result = adjacentStage(current, direction);

        // Matches the independent index +/- 1 clamped reference.
        expect(result).toBe(expectedAdjacent(current, direction));

        // The result is always a valid, in-range stage.
        expect(PIPELINE_STAGE_ORDER).toContain(result);
        expect(PIPELINE_STAGE_ORDER.indexOf(result)).toBeGreaterThanOrEqual(0);
        expect(PIPELINE_STAGE_ORDER.indexOf(result)).toBeLessThanOrEqual(
          PIPELINE_STAGE_ORDER.length - 1,
        );
      }),
      { numRuns: 200 },
    );
  });

  // Explicit boundary example cases.
  it("clamps 'previous' at the first stage (Intake stays Intake)", () => {
    expect(adjacentStage("intake", "previous")).toBe("intake");
  });

  it("clamps 'next' at the last stage (Audit stays Audit)", () => {
    expect(adjacentStage("audit", "next")).toBe("audit");
  });

  it("steps forward through interior stages", () => {
    expect(adjacentStage("intake", "next")).toBe("prepare");
    expect(adjacentStage("prepare", "next")).toBe("execute");
    expect(adjacentStage("execute", "next")).toBe("audit");
  });

  it("steps backward through interior stages", () => {
    expect(adjacentStage("audit", "previous")).toBe("execute");
    expect(adjacentStage("execute", "previous")).toBe("prepare");
    expect(adjacentStage("prepare", "previous")).toBe("intake");
  });
});
