import { describe, expect, it } from "vitest";

import type { PlanAPIPass, PlanAPIPlan } from "@/features/relay-plans";
import {
  buildPassContextText,
  canCreateRunForPass,
  getCreateRunSearch,
  getPassBlockingDependencies,
  getPassDetailState,
} from "./relayPlanPassDetailState";

function buildPlan(overrides: Partial<PlanAPIPlan> = {}): PlanAPIPlan {
  return {
    id: "plan-row-1",
    planId: "plan-1",
    schemaVersion: "1.0.0",
    title: "Plan detail pass wiring",
    goal: "Wire pass-associated run creation",
    repoTarget: "Paintersrp/relay",
    branchContext: "main",
    status: "active",
    sourceIntentSummary: "Source summary",
    createdAt: "2026-06-21T00:00:00Z",
    updatedAt: "2026-06-21T00:00:00Z",
    ...overrides,
  };
}

function buildPass(overrides: Partial<PlanAPIPass> = {}): PlanAPIPass {
  return {
    id: "pass-row-1",
    planRowId: "plan-row-1",
    passId: "pass-1",
    sequence: 1,
    name: "Create pass route",
    goal: "Add nested pass detail route",
    intendedExecutionScope: ["apps/web/src/routes/plans"],
    nonGoals: ["backend schema changes"],
    dependencies: [],
    status: "planned",
    createdAt: "2026-06-21T00:00:00Z",
    updatedAt: "2026-06-21T00:00:00Z",
    ...overrides,
  };
}

describe("relayPlanPassDetailState", () => {
  it("marks a planned pass with no dependencies as ready and runnable", () => {
    const pass = buildPass();

    expect(getPassDetailState(pass, [pass])).toBe("ready");
    expect(canCreateRunForPass(pass, [pass])).toBe(true);
  });

  it("marks a planned pass with unfinished or missing dependencies as blocked", () => {
    const dependency = buildPass({ passId: "pass-1", status: "planned" });
    const pass = buildPass({
      id: "pass-row-2",
      passId: "pass-2",
      sequence: 2,
      dependencies: ["pass-1", "pass-404"],
    });

    expect(getPassDetailState(pass, [dependency, pass])).toBe("blocked");
    expect(canCreateRunForPass(pass, [dependency, pass])).toBe(false);
    expect(getPassBlockingDependencies(pass, [dependency, pass]).map((item) => item.passId)).toEqual([
      "pass-1",
      "pass-404",
    ]);
  });

  it("treats completed and skipped dependencies as terminal", () => {
    const completed = buildPass({ passId: "pass-1", status: "completed" });
    const skipped = buildPass({
      id: "pass-row-2",
      passId: "pass-2",
      sequence: 2,
      status: "skipped",
    });
    const pass = buildPass({
      id: "pass-row-3",
      passId: "pass-3",
      sequence: 3,
      dependencies: ["pass-1", "pass-2"],
    });

    expect(getPassBlockingDependencies(pass, [completed, skipped, pass])).toEqual([]);
    expect(getPassDetailState(pass, [completed, skipped, pass])).toBe("ready");
  });

  it("derives in-progress, completed, and skipped states exactly", () => {
    expect(getPassDetailState(buildPass({ status: "in_progress" }), [])).toBe("in_progress");
    expect(getPassDetailState(buildPass({ status: "completed" }), [])).toBe("completed");
    expect(getPassDetailState(buildPass({ status: "skipped" }), [])).toBe("skipped");
  });

  it("includes plan and pass identifiers in copy context", () => {
    const text = buildPassContextText({
      plan: buildPlan({ planId: "plan-ui-04" }),
      pass: buildPass({ passId: "pass-detail" }),
      blockingDependencies: [],
    });

    expect(text).toContain("Plan ID: plan-ui-04");
    expect(text).toContain("Pass ID: pass-detail");
  });

  it("returns create-run search only with both plan and pass IDs", () => {
    expect(getCreateRunSearch("plan-1", "pass-1")).toEqual({
      planId: "plan-1",
      passId: "pass-1",
    });
  });
});
