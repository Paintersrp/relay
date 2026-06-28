import { describe, expect, it } from "vitest";

import {
  EXECUTE_PIPELINE_STEPS,
  getExecuteDisplayState,
  getExecutePipelineStatuses,
  getExecuteStateCardCopy,
  type ExecuteVisualStateInput,
} from "./runExecuteVisualState";

function input(
  status: string,
  overrides: Partial<ExecuteVisualStateInput> = {},
): ExecuteVisualStateInput {
  return {
    run: { status, lifecycleState: "execute" },
    ...overrides,
  };
}

describe("runExecuteVisualState", () => {
  it("defines the stable execute pipeline steps", () => {
    expect(EXECUTE_PIPELINE_STEPS.map((step) => step.id)).toEqual([
      "brief-approved",
      "dispatched",
      "running",
      "result-captured",
      "audit-ready",
    ]);
  });

  it("maps approved_for_executor to ready", () => {
    expect(getExecuteDisplayState(input("approved_for_executor"))).toBe(
      "ready",
    );
    expect(getExecutePipelineStatuses(input("approved_for_executor"))).toEqual({
      "brief-approved": "success",
      dispatched: "active",
      running: "waiting",
      "result-captured": "waiting",
      "audit-ready": "waiting",
    });
  });

  it("maps pending and running executor states to running", () => {
    expect(
      getExecuteDisplayState(input("approved_for_executor", { executePending: true })),
    ).toBe("running");
    expect(getExecuteDisplayState(input("executor_dispatched"))).toBe("running");
    expect(getExecuteDisplayState(input("executor_running"))).toBe("running");
    expect(
      getExecutePipelineStatuses(input("executor_running")).running,
    ).toBe("running");
  });

  it("maps validation activity to validating", () => {
    expect(getExecuteDisplayState(input("local_validation_running"))).toBe(
      "validating",
    );
    expect(
      getExecuteDisplayState(input("executor_done", { validatePending: true })),
    ).toBe("validating");
    expect(
      getExecutePipelineStatuses(
        input("executor_done", {
          validatePending: true,
          hasResultArtifacts: true,
        }),
      ),
    ).toMatchObject({
      "result-captured": "success",
      "audit-ready": "running",
    });
  });

  it("maps terminal result statuses to complete", () => {
    expect(getExecuteDisplayState(input("executor_done"))).toBe("complete");
    expect(getExecuteDisplayState(input("agent_done"))).toBe("complete");
    expect(getExecuteDisplayState(input("agent_result_needs_review"))).toBe(
      "complete",
    );
    expect(getExecuteDisplayState(input("validation_passed"))).toBe("complete");
    expect(
      getExecutePipelineStatuses(input("validation_passed"))["audit-ready"],
    ).toBe("success");
  });

  it("maps blocked and failed executor evidence to failed", () => {
    expect(getExecuteDisplayState(input("executor_blocked"))).toBe("failed");
    expect(getExecuteDisplayState(input("agent_blocked"))).toBe("failed");
    expect(
      getExecuteDisplayState(input("executor_running", { preflightBlocked: true })),
    ).toBe("failed");
    expect(
      getExecutePipelineStatuses(
        input("executor_blocked", { hasResultArtifacts: true }),
      ),
    ).toMatchObject({
      running: "failed",
      "result-captured": "success",
    });
  });

  it("maps unrelated statuses to blocked", () => {
    expect(getExecuteDisplayState(input("intake_received"))).toBe("blocked");
    expect(getExecutePipelineStatuses(input("intake_received"))).toMatchObject({
      "brief-approved": "blocked",
      dispatched: "waiting",
    });
  });

  it("returns state card copy for key states", () => {
    expect(getExecuteStateCardCopy("ready")).toMatchObject({
      tone: "info",
      title: "Executor can start",
    });
    expect(getExecuteStateCardCopy("complete")).toMatchObject({
      tone: "success",
      title: "Result evidence is captured",
    });
    expect(getExecuteStateCardCopy("failed")).toMatchObject({
      tone: "danger",
      title: "Executor needs attention",
    });
  });

  it("never maps executor_blocked to ready", () => {
    expect(getExecuteDisplayState(input("executor_blocked"))).not.toBe("ready");
    const cardCopy = getExecuteStateCardCopy("failed");
    expect(cardCopy.eyebrow).not.toBe("READY TO DISPATCH");
    expect(cardCopy.tone).not.toBe("info");
  });

  it("only maps approved_for_executor to ready (no recovery gate)", () => {
    expect(getExecuteDisplayState(input("approved_for_executor"))).toBe("ready");
    expect(getExecuteDisplayState(input("executor_blocked"))).toBe("failed");
    expect(getExecuteDisplayState(input("agent_blocked"))).toBe("failed");
    expect(getExecuteDisplayState(input("executor_running"))).toBe("running");
    expect(getExecuteDisplayState(input("executor_done"))).toBe("complete");
  });

  it("failed state card never says Ready to dispatch", () => {
    const cardCopy = getExecuteStateCardCopy("failed");
    expect(cardCopy.eyebrow).toBe("EXECUTION BLOCKED");
    expect(cardCopy.message).toContain("Review blocker evidence");
    expect(cardCopy.message).not.toContain("Ready");
  });
});
