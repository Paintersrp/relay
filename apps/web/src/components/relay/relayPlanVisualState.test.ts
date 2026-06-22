import { describe, expect, it } from "vitest";

import type { PlanAPIReadPlan } from "@/features/relay-plans";
import {
  getPlanProgressSummary,
  getPlanRegistryPassSummary,
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

  it("treats completionReady as fully complete when counts are missing", () => {
    const summary = getPlanProgressSummary(
      buildPlan({ completionReady: true, completedPassCount: undefined }),
    );

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
    expect(summary.terminal).toBe(1);
    expect(summary.filledDots).toBe(1);
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
});
