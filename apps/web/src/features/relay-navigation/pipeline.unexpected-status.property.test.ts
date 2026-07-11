import fc from "fast-check";
import { describe, expect, it } from "vitest";

import type { WorkflowRunStatus } from "@/features/relay-runs";
import {
  derivePipelineStages,
  PIPELINE_STAGE_ORDER,
  resolveWorkflowStage,
} from "./pipeline";

const CANONICAL_STATUSES: readonly WorkflowRunStatus[] = [
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

const unexpectedStatusArbitrary = fc
  .string()
  .filter(
    (value) =>
      !CANONICAL_STATUSES.includes(value as WorkflowRunStatus),
  );

describe("unexpected workflow status fallback", () => {
  it("fails closed for every generated noncanonical status string", () => {
    fc.assert(
      fc.property(unexpectedStatusArbitrary, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(
          durableStage,
          undefined,
          status,
          durableStage,
        );

        expect(durableStage).toBeUndefined();
        expect(stages.map((stage) => stage.step)).toEqual(
          PIPELINE_STAGE_ORDER,
        );
        expect(stages).toHaveLength(3);
        expect(stages.every((stage) => stage.status === "pending")).toBe(
          true,
        );
        expect(stages.every((stage) => !stage.navigable)).toBe(true);
        expect(
          stages.some(
            (stage) =>
              stage.status === "current" ||
              stage.status === "attention",
          ),
        ).toBe(false);
      }),
      { numRuns: 200 },
    );
  });
});
