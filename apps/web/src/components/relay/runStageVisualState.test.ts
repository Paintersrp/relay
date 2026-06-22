import { describe, expect, it } from "vitest";

import {
  getRunStageStepLabel,
  getRunStageStepStatus,
  isRunStageStepAttention,
  isRunStageStepTerminal,
} from "./runStageVisualState";

describe("runStageVisualState", () => {
  it("defaults missing step statuses to waiting", () => {
    expect(getRunStageStepStatus({}, "compile")).toBe("waiting");
  });

  it("returns supplied step statuses", () => {
    expect(getRunStageStepStatus({ compile: "blocked" }, "compile")).toBe(
      "blocked",
    );
  });

  it("identifies terminal display statuses", () => {
    expect(isRunStageStepTerminal("success")).toBe(true);
    expect(isRunStageStepTerminal("accepted")).toBe(true);
    expect(isRunStageStepTerminal("warning")).toBe(true);

    expect(isRunStageStepTerminal("running")).toBe(false);
    expect(isRunStageStepTerminal("blocked")).toBe(false);
    expect(isRunStageStepTerminal("failed")).toBe(false);
    expect(isRunStageStepTerminal("waiting")).toBe(false);
    expect(isRunStageStepTerminal("na")).toBe(false);
    expect(isRunStageStepTerminal("revision")).toBe(false);
  });

  it("identifies attention display statuses", () => {
    expect(isRunStageStepAttention("active")).toBe(true);
    expect(isRunStageStepAttention("running")).toBe(true);
    expect(isRunStageStepAttention("blocked")).toBe(true);
    expect(isRunStageStepAttention("failed")).toBe(true);
    expect(isRunStageStepAttention("revision")).toBe(true);
    expect(isRunStageStepAttention("warning")).toBe(true);
  });

  it("maps display statuses to compact labels", () => {
    expect(getRunStageStepLabel("waiting")).toBeNull();
    expect(getRunStageStepLabel("revision")).toBe("Revision required");
  });
});
