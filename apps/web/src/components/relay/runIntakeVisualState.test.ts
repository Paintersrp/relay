import { describe, expect, it } from "vitest";

import type { RelayRun } from "@/features/relay-runs";
import {
  getIntakeDisplayState,
  getIntakePipelineStatuses,
  getIntakeStateCardCopy,
  INTAKE_PIPELINE_STEPS,
} from "./runIntakeVisualState";

type IntakeRunState = Pick<RelayRun, "status" | "activeStep">;

function runState(
  status: RelayRun["status"],
  activeStep: RelayRun["activeStep"] = "intake",
): IntakeRunState {
  return { status, activeStep };
}

describe("runIntakeVisualState", () => {
  it("defines the stable intake pipeline steps", () => {
    expect(INTAKE_PIPELINE_STEPS.map((step) => step.id)).toEqual([
      "handoff-loaded",
      "config-reviewed",
      "executor-selected",
      "model-selected",
      "intake-approved",
    ]);
  });

  it("maps intake_needs_review to review state", () => {
    expect(getIntakeDisplayState(runState("intake_needs_review"))).toBe(
      "review",
    );
  });

  it("keeps review pipeline selections waiting when values are missing", () => {
    expect(
      getIntakePipelineStatuses({
        run: runState("intake_needs_review"),
      }),
    ).toMatchObject({
      "handoff-loaded": "success",
      "config-reviewed": "active",
      "executor-selected": "waiting",
      "model-selected": "waiting",
      "intake-approved": "waiting",
    });
  });

  it("marks review pipeline selections complete when values are selected", () => {
    expect(
      getIntakePipelineStatuses({
        run: runState("intake_needs_review"),
        executorAdapter: "opencode_go",
        model: "deepseek-v4-flash",
      }),
    ).toMatchObject({
      "executor-selected": "success",
      "model-selected": "success",
    });
  });

  it("maps intake_received to received state", () => {
    expect(getIntakeDisplayState(runState("intake_received"))).toBe("received");
  });

  it("maps approved_for_prepare to approved state and accepted approval step", () => {
    expect(getIntakeDisplayState(runState("approved_for_prepare"))).toBe(
      "approved",
    );
    expect(
      getIntakePipelineStatuses({
        run: runState("approved_for_prepare"),
      })["intake-approved"],
    ).toBe("accepted");
  });

  it("maps prepare active step to approved state", () => {
    expect(getIntakeDisplayState(runState("validated", "prepare"))).toBe(
      "approved",
    );
  });

  it("maps blocked state to blocked pipeline", () => {
    expect(getIntakeDisplayState(runState("blocked"))).toBe("blocked");
    expect(
      getIntakePipelineStatuses({
        run: runState("blocked"),
      }),
    ).toMatchObject({
      "handoff-loaded": "blocked",
      "config-reviewed": "waiting",
      "executor-selected": "waiting",
      "model-selected": "waiting",
      "intake-approved": "waiting",
    });
  });

  it("returns default copy and defensive statuses for unknown states", () => {
    expect(getIntakeStateCardCopy("default")).toMatchObject({
      tone: "default",
      eyebrow: "INTAKE",
      title: "Intake status unavailable",
    });
    expect(
      getIntakePipelineStatuses({
        run: runState("validated"),
        executorAdapter: "codex",
      }),
    ).toMatchObject({
      "handoff-loaded": "success",
      "config-reviewed": "waiting",
      "executor-selected": "success",
      "model-selected": "waiting",
      "intake-approved": "waiting",
    });
  });
});
