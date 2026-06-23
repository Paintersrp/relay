import { describe, expect, it } from "vitest";

import type { PlanAPIPass } from "@/features/relay-plans";
import {
  getAssociatedRunSummaryLabel,
  getPrimaryAssociatedRun,
} from "./RelayPlanPassTimeline";

function buildPass(partial: Partial<PlanAPIPass>): PlanAPIPass {
  return {
    id: "1",
    planRowId: "10",
    passId: "PASS-009",
    sequence: 9,
    name: "Source visibility",
    goal: "Expose source metadata",
    intendedExecutionScope: [],
    nonGoals: [],
    dependencies: [],
    status: "in_progress",
    associatedRunIds: [],
    associatedRuns: [],
    createdAt: "2026-06-23T00:00:00Z",
    updatedAt: "2026-06-23T00:00:00Z",
    ...partial,
  };
}

describe("RelayPlanPassTimeline helpers", () => {
  it("returns the first associated run and summary label", () => {
    const pass = buildPass({
      associatedRunIds: ["41", "42"],
      associatedRuns: [
        {
          id: "41",
          title: "Run 41",
          status: "intake_needs_review",
          lifecycleState: "intake",
          activeStep: "intake",
          workbenchPath: "/runs/41/intake",
          createdAt: "2026-06-23T00:00:00Z",
          updatedAt: "2026-06-23T00:00:00Z",
        },
        {
          id: "42",
          title: "Run 42",
          status: "approved_for_prepare",
          lifecycleState: "prepare",
          activeStep: "prepare",
          workbenchPath: "/runs/42/prepare",
          createdAt: "2026-06-23T00:00:00Z",
          updatedAt: "2026-06-23T00:00:00Z",
        },
      ],
    });

    expect(getPrimaryAssociatedRun(pass)?.id).toBe("41");
    expect(getAssociatedRunSummaryLabel(pass)).toBe("2 runs • 41");
  });
});
