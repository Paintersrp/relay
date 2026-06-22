import { describe, expect, it } from "vitest";

import type { RelayRun } from "@/features/relay-runs";
import {
  COMPILE_RENDER_PIPELINE_STEPS,
  getCompileRenderDisplayState,
  getCompileRenderPipelineStatuses,
  getCompileRenderStateCardCopy,
  type CompileRenderVisualStateInput,
} from "./runCompileRenderVisualState";

function input(
  status: RelayRun["status"],
  overrides: Partial<CompileRenderVisualStateInput> = {},
): CompileRenderVisualStateInput {
  return {
    run: { status },
    ...overrides,
  };
}

describe("runCompileRenderVisualState", () => {
  it("defines the stable compile/render pipeline steps", () => {
    expect(COMPILE_RENDER_PIPELINE_STEPS.map((step) => step.id)).toEqual([
      "compile",
      "packet-validation",
      "repair",
      "render-brief",
      "brief-validation",
      "approval",
    ]);
  });

  it("maps pre-prepare states to blocked", () => {
    expect(getCompileRenderDisplayState(input("intake_received"))).toBe(
      "blocked",
    );
    expect(getCompileRenderPipelineStatuses(input("intake_received"))).toEqual({
      compile: "blocked",
      "packet-validation": "waiting",
      repair: "na",
      "render-brief": "waiting",
      "brief-validation": "waiting",
      approval: "waiting",
    });
  });

  it("maps approved_for_prepare to ready-to-compile", () => {
    expect(getCompileRenderDisplayState(input("approved_for_prepare"))).toBe(
      "ready_to_compile",
    );
    expect(getCompileRenderPipelineStatuses(input("approved_for_prepare"))).toEqual(
      {
        compile: "active",
        "packet-validation": "waiting",
        repair: "na",
        "render-brief": "waiting",
        "brief-validation": "waiting",
        approval: "waiting",
      },
    );
  });

  it("gives compile pending precedence", () => {
    expect(
      getCompileRenderDisplayState(
        input("approved_for_prepare", { compilePending: true }),
      ),
    ).toBe("compiling");
    expect(
      getCompileRenderPipelineStatuses(
        input("approved_for_prepare", { compilePending: true }),
      ).compile,
    ).toBe("running");
  });

  it("marks packet validation failed with eligible repair as active", () => {
    expect(
      getCompileRenderDisplayState(
        input("packet_validation_failed", { repairEligible: true }),
      ),
    ).toBe("packet_invalid");
    expect(
      getCompileRenderPipelineStatuses(
        input("packet_validation_failed", { repairEligible: true }),
      ),
    ).toMatchObject({
      compile: "success",
      "packet-validation": "failed",
      repair: "active",
    });
  });

  it("marks packet validation failed without eligible repair as blocked", () => {
    expect(
      getCompileRenderPipelineStatuses(
        input("packet_validation_failed", { repairEligible: false }),
      ).repair,
    ).toBe("blocked");
  });

  it("gives repair pending precedence over failed validation", () => {
    expect(
      getCompileRenderDisplayState(
        input("packet_validation_failed", { repairPending: true }),
      ),
    ).toBe("repairing");
    expect(
      getCompileRenderPipelineStatuses(
        input("packet_validation_failed", { repairPending: true }),
      ),
    ).toMatchObject({
      "packet-validation": "failed",
      repair: "running",
      "render-brief": "waiting",
    });
  });

  it("maps repair validation success to brief rendering readiness", () => {
    expect(getCompileRenderDisplayState(input("repair_validated"))).toBe(
      "repair_validated",
    );
    expect(getCompileRenderPipelineStatuses(input("repair_validated"))).toMatchObject(
      {
        "packet-validation": "success",
        repair: "success",
        "render-brief": "active",
      },
    );
  });

  it("maps packet validation success to brief rendering readiness", () => {
    expect(getCompileRenderDisplayState(input("packet_validated"))).toBe(
      "packet_validated",
    );
    expect(getCompileRenderPipelineStatuses(input("packet_validated"))).toMatchObject(
      {
        "packet-validation": "success",
        repair: "na",
        "render-brief": "active",
      },
    );
  });

  it("maps brief ready with validation reports", () => {
    expect(
      getCompileRenderPipelineStatuses(
        input("brief_ready_for_review", {
          hasPassingBriefValidationReport: true,
        }),
      ),
    ).toMatchObject({
      "render-brief": "success",
      "brief-validation": "success",
      approval: "active",
    });

    expect(
      getCompileRenderPipelineStatuses(
        input("brief_ready_for_review", {
          hasFailingBriefValidationReport: true,
        }),
      )["brief-validation"],
    ).toBe("failed");
  });

  it("maps approved_for_executor to accepted approval", () => {
    expect(getCompileRenderDisplayState(input("approved_for_executor"))).toBe(
      "approved",
    );
    expect(
      getCompileRenderPipelineStatuses(input("approved_for_executor")).approval,
    ).toBe("accepted");
  });

  it("returns state card copy for key states", () => {
    expect(getCompileRenderStateCardCopy("packet_invalid")).toMatchObject({
      tone: "danger",
      title: "Packet validation failed",
    });
    expect(getCompileRenderStateCardCopy("approved")).toMatchObject({
      tone: "success",
      title: "Approved for executor",
    });
  });
});
