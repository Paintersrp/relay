import { describe, expect, it } from "vitest";

import type { PlanAPIPass, PlanAPIReadPlan } from "@/features/relay-plans";
import {
  getCurrentPass,
  getNextRunnablePass,
  getPassStatusCounts,
  getPlanDetailCardState,
  getPlanDetailProgress,
  getPlanProgressSummary,
  getPlanRegistryPassSummary,
  getUnmetDependencies,
} from "./relayPlanVisualState";

function buildPlan(overrides: Partial<PlanAPIReadPlan> = {}): PlanAPIReadPlan {
  return {
    id: "plan-row-1",
    planId: "plan-1",
    schemaVersion: "1.0.0",
    title: "Registry remediation",
    goal: "Match the registry target UI",
    repoTarget: "Paintersrp/relay",
    branchContext: "main",
    status: "active",
    sourceIntentSummary: "Summary",
    createdAt: "2026-06-21T00:00:00Z",
    updatedAt: "2026-06-21T00:00:00Z",
    passCount: 2,
    completionReady: false,
    ...overrides,
  };
}

function buildPass(overrides: Partial<PlanAPIPass> = {}): PlanAPIPass {
  return {
    id: "pass-row-1",
    planRowId: "plan-row-1",
    passId: "pass-1",
    sequence: 1,
    name: "Inspect target",
    goal: "Align the detail layout",
    intendedExecutionScope: ["apps/web/src/components/relay"],
    nonGoals: [],
    dependencies: [],
    status: "planned",
    associatedRunIds: [],
    associatedRuns: [],
    createdAt: "2026-06-21T00:00:00Z",
    updatedAt: "2026-06-21T00:00:00Z",
    ...overrides,
  };
}

describe("relayPlanVisualState", () => {
  it("renders partial progress as 1 / 2", () => {
    const summary = getPlanProgressSummary(
      buildPlan({ completedPassCount: 1, skippedPassCount: 0 }),
    );

    expect(summary).toMatchObject({
      total: 2,
      completed: 1,
      label: "1 / 2",
      filledDots: 1,
      dotCount: 2,
    });
  });

  it("counts skipped terminal passes toward completion-ready progress", () => {
    const summary = getPlanProgressSummary(
      buildPlan({
        passCount: 3,
        completedPassCount: 2,
        skippedPassCount: 1,
        completionReady: true,
      }),
    );

    expect(summary).toMatchObject({
      total: 3,
      completed: 2,
      skipped: 1,
      terminal: 3,
      label: "3 / 3",
      filledDots: 3,
      dotCount: 3,
    });
  });

  it("treats completionReady as fully terminal when counts are missing", () => {
    const summary = getPlanProgressSummary(
      buildPlan({
        completionReady: true,
        completedPassCount: undefined,
        skippedPassCount: undefined,
      }),
    );

    expect(summary.terminal).toBe(2);
    expect(summary.label).toBe("2 / 2");
    expect(summary.filledDots).toBe(2);
  });

  it("defaults missing counts to 0 / 2 when the plan is not completion ready", () => {
    const summary = getPlanProgressSummary(buildPlan());

    expect(summary.label).toBe("0 / 2");
    expect(summary.filledDots).toBe(0);
  });

  it("does not count skipped passes toward visible completed progress", () => {
    const summary = getPlanProgressSummary(
      buildPlan({
        passCount: 3,
        completedPassCount: 1,
        skippedPassCount: 1,
      }),
    );

    expect(summary.label).toBe("1 / 3");
    expect(summary.completed).toBe(1);
    expect(summary.skipped).toBe(1);
    expect(summary.terminal).toBe(2);
    expect(summary.filledDots).toBe(1);
  });

  it("counts completed, in-progress, planned, skipped, and terminal passes accurately", () => {
    const counts = getPassStatusCounts([
      buildPass({ status: "completed" }),
      buildPass({ id: "pass-row-2", passId: "pass-2", sequence: 2, status: "in_progress" }),
      buildPass({ id: "pass-row-3", passId: "pass-3", sequence: 3, status: "planned" }),
      buildPass({ id: "pass-row-4", passId: "pass-4", sequence: 4, status: "skipped" }),
    ]);

    expect(counts).toMatchObject({
      completed: 1,
      inProgress: 1,
      planned: 1,
      skipped: 1,
      terminal: 2,
      total: 4,
    });
  });

  it("clamps overcounts to the total pass count", () => {
    const summary = getPlanProgressSummary(
      buildPlan({
        passCount: 2,
        completedPassCount: 3,
        skippedPassCount: 2,
      }),
    );

    expect(summary.label).toBe("2 / 2");
    expect(summary.filledDots).toBe(2);
  });

  it("prefers current pass summary over next pass summary", () => {
    const summary = getPlanRegistryPassSummary(
      buildPlan({
        currentPassId: "pass-1",
        currentPassName: "Audit",
        currentPassGoal: "Review the generated packet",
        nextPassId: "pass-2",
        nextPassName: "Submit",
      }),
    );

    expect(summary).toMatchObject({
      kind: "current",
      passId: "pass-1",
      title: "Audit",
      subtitle: "Review the generated packet",
    });
  });

  it("uses an em dash fallback when pass summary fields are absent", () => {
    expect(getPlanRegistryPassSummary(buildPlan())).toMatchObject({
      kind: "fallback",
      title: "—",
    });
  });

  it("renders active completion-ready plans as ready for closeout", () => {
    expect(
      getPlanRegistryPassSummary(
        buildPlan({
          status: "active",
          completionReady: true,
          passCount: 3,
          completedPassCount: 2,
          skippedPassCount: 1,
        }),
      ),
    ).toMatchObject({
      kind: "fallback",
      title: "READY FOR CLOSEOUT",
      subtitle: "All passes terminal",
    });
  });

  it("renders complete plans as ALL COMPLETE", () => {
    expect(
      getPlanRegistryPassSummary(buildPlan({ status: "complete", completionReady: true })),
    ).toMatchObject({
      kind: "fallback",
      title: "ALL COMPLETE",
    });
  });

  it("renders abandoned plans as an em dash", () => {
    expect(
      getPlanRegistryPassSummary(buildPlan({ status: "abandoned" })),
    ).toMatchObject({
      kind: "fallback",
      title: "—",
    });
  });

  it("uses next pass summary when current pass fields are absent", () => {
    expect(
      getPlanRegistryPassSummary(
        buildPlan({
          nextPassId: "pass-2",
          nextPassName: "Implementation",
          nextPassGoal: "Apply the scoped UI changes",
        }),
      ),
    ).toMatchObject({
      kind: "next",
      passId: "pass-2",
      title: "Implementation",
      subtitle: "Apply the scoped UI changes",
    });
  });

  it("returns unmet dependencies for missing or non-terminal passes only", () => {
    const passes = [
      buildPass({ passId: "pass-1", status: "completed" }),
      buildPass({ id: "pass-row-2", passId: "pass-2", sequence: 2, status: "planned" }),
      buildPass({
        id: "pass-row-3",
        passId: "pass-3",
        sequence: 3,
        dependencies: ["pass-1", "pass-2", "pass-404"],
      }),
    ];

    expect(getUnmetDependencies(passes[2], passes)).toEqual(["pass-2", "pass-404"]);
  });

  it("selects the first runnable planned pass", () => {
    const passes = [
      buildPass({ passId: "pass-1", status: "completed" }),
      buildPass({
        id: "pass-row-2",
        passId: "pass-2",
        sequence: 2,
        dependencies: ["pass-1"],
      }),
      buildPass({
        id: "pass-row-3",
        passId: "pass-3",
        sequence: 3,
        dependencies: ["pass-9"],
      }),
    ];

    expect(getNextRunnablePass(passes)?.passId).toBe("pass-2");
  });

  it("maps detail progress counts and segments from real pass statuses", () => {
    const progress = getPlanDetailProgress([
      buildPass({ status: "completed" }),
      buildPass({ id: "pass-row-2", passId: "pass-2", sequence: 2, status: "in_progress" }),
      buildPass({ id: "pass-row-3", passId: "pass-3", sequence: 3, status: "skipped" }),
    ]);

    expect(progress).toMatchObject({
      completed: 1,
      inProgress: 1,
      planned: 0,
      skipped: 1,
      terminal: 2,
      total: 3,
      segmentCount: 3,
      completedSegments: 1,
      skippedSegments: 1,
      inProgressSegments: 1,
    });
  });

  it("returns safe zero detail progress for empty pass lists", () => {
    expect(getPlanDetailProgress([])).toMatchObject({
      total: 0,
      completed: 0,
      inProgress: 0,
      planned: 0,
      skipped: 0,
      terminal: 0,
      segmentCount: 0,
      completedSegments: 0,
      skippedSegments: 0,
      inProgressSegments: 0,
    });
  });

  it("maps an active current pass to the plan detail card state", () => {
    const currentPass = buildPass({ status: "in_progress", name: "Execute remediation" });
    const state = getPlanDetailCardState({
      plan: buildPlan(),
      completionReady: false,
      currentPass: getCurrentPass([currentPass]),
    });

    expect(state).toMatchObject({
      key: "active",
      eyebrow: "PLAN ACTIVE",
      title: "Execute remediation",
      subtitle: "Align the detail layout",
    });
  });

  it("maps an active plan without a current pass to the idle active card state", () => {
    const state = getPlanDetailCardState({
      plan: buildPlan(),
      completionReady: false,
    });

    expect(state).toMatchObject({
      key: "active",
      title: "No pass currently in progress",
    });
  });

  it("maps active completion-ready plans to the review-ready card state", () => {
    const plan = buildPlan({ status: "active" });
    const state = getPlanDetailCardState({
      plan,
      completionReady: true,
    });

    expect(plan.status).toBe("active");
    expect(state).toMatchObject({
      key: "completion_ready",
      title: "All passes terminal — ready for closeout review",
      subtitle: "Plan status remains active until a supported completion action exists.",
    });
  });

  it("maps complete plans to the complete card state", () => {
    const state = getPlanDetailCardState({
      plan: buildPlan({ status: "complete" }),
      completionReady: true,
    });

    expect(state).toMatchObject({
      key: "complete",
      title: "All planned passes completed successfully",
    });
  });

  it("maps abandoned plans to the abandoned card state", () => {
    const state = getPlanDetailCardState({
      plan: buildPlan({ status: "abandoned" }),
      completionReady: false,
    });

    expect(state).toMatchObject({
      key: "abandoned",
      title: "This plan is no longer active",
    });
  });
});
