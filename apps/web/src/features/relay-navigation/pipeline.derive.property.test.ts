// Feature: frontend-shell-redesign, Property 10
//
// Property 10: Pipeline derivation well-formedness and determinism
// Validates: Requirements 6.1, 6.2, 6.3, 6.7
//
// For any canonical `WorkflowRunStage` (and any arbitrary unknown string),
// `derivePipelineStages`:
//   - Returns exactly three stages equal to `PIPELINE_STAGE_ORDER` in order,
//     each carrying the correct label (`PIPELINE_STAGE_LABELS`) and route
//     (`PIPELINE_STAGE_ROUTES`) (Requirement 6.1).
//   - Marks exactly one stage as the CURRENT POSITION,
//     expressed as exactly one stage whose status is in {"current",
//     "attention"}.
//   - Is deterministic: two calls with the same status are deep-equal.
//   - Is total: arbitrary unknown / out-of-enum strings fall back to no
//     current/attention stage and all stages "pending" (Requirement 2.4).

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import type { WorkflowRunStage } from "@/features/relay-runs";
import {
  derivePipelineStages,
  PIPELINE_STAGE_LABELS,
  PIPELINE_STAGE_ORDER,
  PIPELINE_STAGE_ROUTES,
} from "./pipeline";

const ALL_CANONICAL_STAGES: readonly WorkflowRunStage[] = [
  "specification",
  "execute",
  "audit",
];

const canonicalStageArb: fc.Arbitrary<WorkflowRunStage> = fc.constantFrom(
  ...ALL_CANONICAL_STAGES,
);

const anyStringArb: fc.Arbitrary<string> = fc.string({ minLength: 0, maxLength: 24 });

describe("derivePipelineStages — Property 10: derivation well-formedness and determinism", () => {
  it("returns exactly three stages in PIPELINE_STAGE_ORDER with correct labels and routes (Req 6.1)", () => {
    fc.assert(
      fc.property(canonicalStageArb, (stage) => {
        const stages = derivePipelineStages(stage, stage);

        expect(stages).toHaveLength(PIPELINE_STAGE_ORDER.length);

        stages.forEach((s, index) => {
          const expectedStep = PIPELINE_STAGE_ORDER[index];
          expect(s.step).toBe(expectedStep);
          expect(s.label).toBe(PIPELINE_STAGE_LABELS[expectedStep]);
          expect(s.to).toBe(PIPELINE_STAGE_ROUTES[expectedStep]);
        });
      }),
      { numRuns: 200 },
    );
  });

  it("marks exactly one current position when selectedRouteStage matches a stage (Req 6.2)", () => {
    fc.assert(
      fc.property(canonicalStageArb, (stage) => {
        const stages = derivePipelineStages(stage, stage);

        const currentPositionIndices = stages
          .map((s, i) => (s.status === "current" || s.status === "attention" ? i : -1))
          .filter((i) => i !== -1);

        expect(currentPositionIndices).toHaveLength(1);
        const currentIndex = currentPositionIndices[0];

        stages.forEach((s, index) => {
          if (index < currentIndex) {
            expect(s.status).toBe("completed");
          } else if (index > currentIndex) {
            expect(s.status).toBe("pending");
          } else {
            expect(["current", "attention"]).toContain(s.status);
          }
        });
      }),
      { numRuns: 200 },
    );
  });

  it("is deterministic — two calls with the same params are deep-equal (Req 6.3, 6.7)", () => {
    fc.assert(
      fc.property(canonicalStageArb, (stage) => {
        const first = derivePipelineStages(stage, stage);
        const second = derivePipelineStages(stage, stage);
        expect(first).toEqual(second);
      }),
      { numRuns: 200 },
    );
  });

  it("is total — arbitrary unknown strings as durable stage fall back to no current/attention stage and all pending (Req 2.4)", () => {
    fc.assert(
      fc.property(anyStringArb, (durableStr) => {
        // Exclude actual stage strings from the test
        if (ALL_CANONICAL_STAGES.includes(durableStr as any)) return;

        const stages = derivePipelineStages(durableStr as any, "execute");

        const currentPositionIndex = stages.findIndex(
          (s) => s.status === "current" || s.status === "attention",
        );

        // Non-canonical durable stage: all fall back to "pending"
        expect(currentPositionIndex).toBe(-1);
        expect(stages.every((s) => s.status === "pending")).toBe(true);
      }),
      { numRuns: 200 },
    );
  });
});
