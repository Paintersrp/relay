// Feature: run-workbench-refinement, Property 3: For any canonical WorkflowRunStatus,
// the status-to-step mapping consumed by the RunStepper (derivePipelineStages)
// returns exactly the three canonical steps in specification, execute, audit
// order, marks exactly one step as the active position (current or attention),
// classifies every step ordered before the active one as completed and every
// step ordered after it as upcoming (pending).
//
// Validates: Requirements 2.1, 2.2, 2.3

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import type { WorkflowRunStatus } from "@/features/relay-runs";
import { derivePipelineStages, PIPELINE_STAGE_ORDER, resolveWorkflowStage } from "./pipeline";

const ALL_CANONICAL_STATUSES: readonly WorkflowRunStatus[] = [
  "created",
  "setup_ready",
  "executing",
  "execution_failed",
  "cancelled",
  "validating",
  "validation_failed",
  "audit_ready",
  "needs_revision",
  "completed",
];

const canonicalStatusArb: fc.Arbitrary<WorkflowRunStatus> = fc.constantFrom(
  ...ALL_CANONICAL_STATUSES,
);

describe("derivePipelineStages — Property 3: pipeline classification over canonical statuses", () => {
  it("returns exactly the three canonical steps in specification, execute, audit order", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(durableStage, durableStage, status);

        expect(stages).toHaveLength(3);
        expect(stages.map((s) => s.step)).toEqual(["specification", "execute", "audit"]);
        expect(stages.map((s) => s.step)).toEqual(PIPELINE_STAGE_ORDER);
      }),
      { numRuns: 100 },
    );
  });

  it("marks exactly one step as the active position (current or attention) when selectedRouteStage is provided", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(durableStage, durableStage, status);

        const activeIndices = stages
          .map((stage, index) => ({ stage, index }))
          .filter(({ stage }) => stage.status === "current" || stage.status === "attention")
          .map(({ index }) => index);

        expect(activeIndices).toHaveLength(1);
      }),
      { numRuns: 100 },
    );
  });

  it("classifies steps before the active position as completed and steps after it as upcoming (pending)", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(durableStage, durableStage, status);

        const activeIndex = stages.findIndex(
          (stage) => stage.status === "current" || stage.status === "attention",
        );
        expect(activeIndex).toBeGreaterThanOrEqual(0);

        stages.forEach((stage, index) => {
          if (index < activeIndex) {
            expect(stage.status).toBe("completed");
          } else if (index > activeIndex) {
            expect(stage.status).toBe("pending");
          } else {
            expect(["current", "attention"]).toContain(stage.status);
          }
        });
      }),
      { numRuns: 100 },
    );
  });

  it("assigns each step exactly one of the three mutually exclusive states", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(durableStage, durableStage, status);

        stages.forEach((stage) => {
          expect(["completed", "current", "attention", "pending"]).toContain(stage.status);
        });

        const completedCount = stages.filter((s) => s.status === "completed").length;
        const activeCount = stages.filter((s) => s.status === "current" || s.status === "attention")
          .length;
        const upcomingCount = stages.filter((s) => s.status === "pending").length;

        expect(completedCount + activeCount + upcomingCount).toBe(3);
        expect(activeCount).toBe(1);
      }),
      { numRuns: 100 },
    );
  });
});
