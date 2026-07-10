import { describe, expect, it } from "vitest";

import { workflowImplementationActorLabel } from "./api";

describe("workflowImplementationActorLabel", () => {
  it("labels applier-only implementation evidence without implying Executor execution", () => {
    expect(workflowImplementationActorLabel("applier")).toBe("Deterministic applier");
  });

  it("labels executor-only implementation evidence", () => {
    expect(workflowImplementationActorLabel("executor")).toBe("Executor");
  });

  it("labels hybrid deterministic plus Executor implementation evidence", () => {
    expect(workflowImplementationActorLabel("hybrid")).toBe("Deterministic applier + Executor");
  });
});
