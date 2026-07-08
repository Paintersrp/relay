// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RunStepper } from "./RunStepper";

const mocks = vi.hoisted(() => ({ navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useRouter: () => ({ navigate: mocks.navigate }),
}));

describe("RunStepper canonical durable-stage gating", () => {
  beforeEach(() => {
    mocks.navigate.mockReset();
  });

  it("renders exactly Specification, Execute, and Audit", () => {
    render(
      <RunStepper
        runId="run-1"
        status="executing"
        selectedStage="execute"
      />,
    );

    expect(screen.getAllByRole("button")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "Specification" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Execute" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Audit" })).toBeInTheDocument();
  });

  it("exposes Execute as the setup-ready next stage and still blocks Audit", async () => {
    const user = userEvent.setup();
    render(
      <RunStepper
        runId="run-1"
        status="setup_ready"
        selectedStage="specification"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Audit" }));
    expect(mocks.navigate).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Execute" }));
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/runs/$runId/execute",
      params: { runId: "run-1" },
    });
  });

  it("returns from backward review to the durable Audit stage", async () => {
    const user = userEvent.setup();
    render(
      <RunStepper
        runId="run-1"
        status="audit_ready"
        selectedStage="specification"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Audit" }));
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/runs/$runId/audit",
      params: { runId: "run-1" },
    });
  });

  it("marks the Execute stage for attention after execution failure", () => {
    render(
      <RunStepper
        runId="run-1"
        status="execution_failed"
        selectedStage="execute"
      />,
    );

    expect(screen.getByRole("button", { name: "Execute" })).toHaveAttribute(
      "data-stage-status",
      "attention",
    );
  });
});
