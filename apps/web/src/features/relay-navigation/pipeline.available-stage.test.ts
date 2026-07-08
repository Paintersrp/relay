import { describe, expect, it } from "vitest";

import {
  derivePipelineStages,
  resolveWorkflowAvailableThroughStage,
  resolveWorkflowStage,
} from "./pipeline";

describe("canonical Run stage availability", () => {
  it("keeps setup_ready durably at Specification while making Execute reachable", () => {
    const durableStage = resolveWorkflowStage("setup_ready");
    const availableThroughStage = resolveWorkflowAvailableThroughStage(
      "setup_ready",
      durableStage,
    );
    const stages = derivePipelineStages(
      durableStage,
      "specification",
      "setup_ready",
      availableThroughStage,
    );

    expect(durableStage).toBe("specification");
    expect(availableThroughStage).toBe("execute");
    expect(stages.map((stage) => [stage.step, stage.status, stage.navigable])).toEqual([
      ["specification", "current", true],
      ["execute", "pending", true],
      ["audit", "pending", false],
    ]);
  });

  it("keeps created Runs limited to Specification", () => {
    const durableStage = resolveWorkflowStage("created");
    const availableThroughStage = resolveWorkflowAvailableThroughStage(
      "created",
      durableStage,
    );
    const stages = derivePipelineStages(
      durableStage,
      "specification",
      "created",
      availableThroughStage,
    );

    expect(availableThroughStage).toBe("specification");
    expect(stages.find((stage) => stage.step === "execute")?.navigable).toBe(false);
  });

  it("uses the durable stage as the availability ceiling for all other statuses", () => {
    expect(resolveWorkflowAvailableThroughStage("executing")).toBe("execute");
    expect(resolveWorkflowAvailableThroughStage("audit_ready")).toBe("audit");
    expect(resolveWorkflowAvailableThroughStage("unknown-status")).toBeUndefined();
  });
});