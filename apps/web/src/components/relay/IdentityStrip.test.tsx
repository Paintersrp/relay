// @vitest-environment jsdom

import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IdentityStrip } from "./IdentityStrip";

const mocks = vi.hoisted(() => ({ navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

function run(status: string) {
  return {
    runId: "run-1",
    featureSlug: "workflow-pivot",
    repoTarget: "relay",
    branch: "feat/simplification",
    status,
  };
}

describe("IdentityStrip canonical three-stage navigation", () => {
  beforeEach(() => {
    mocks.navigate.mockReset();
  });

  it("renders canonical identity and exactly three stage controls", () => {
    render(<IdentityStrip run={run("executing")} selectedStage="execute" />);

    expect(screen.getByRole("heading", { name: "workflow-pivot" })).toBeInTheDocument();
    expect(screen.getByText("run-1")).toBeInTheDocument();
    expect(screen.getByText("relay")).toBeInTheDocument();
    expect(
      within(screen.getByRole("group", { name: "Pipeline position" })).getAllByRole(
        "button",
      ),
    ).toHaveLength(3);
  });

  it("blocks stages ahead of the durable stage and navigates to an available stage", async () => {
    const user = userEvent.setup();
    render(
      <IdentityStrip run={run("executing")} selectedStage="specification" />,
    );

    const group = screen.getByRole("group", { name: "Pipeline position" });
    await user.click(within(group).getByRole("button", { name: "Audit" }));
    expect(mocks.navigate).not.toHaveBeenCalled();

    await user.click(within(group).getByRole("button", { name: "Execute" }));
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/runs/$runId/execute",
      params: { runId: "run-1" },
    });
  });

  it("allows backward review followed by return to the durable Audit stage", async () => {
    const user = userEvent.setup();
    render(
      <IdentityStrip run={run("audit_ready")} selectedStage="specification" />,
    );

    await user.click(
      within(screen.getByRole("group", { name: "Pipeline position" })).getByRole(
        "button",
        { name: "Audit" },
      ),
    );
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/runs/$runId/audit",
      params: { runId: "run-1" },
    });
  });

  it("marks execution failure as attention without restoring legacy stages", () => {
    render(
      <IdentityStrip run={run("execution_failed")} selectedStage="execute" />,
    );

    const group = screen.getByRole("group", { name: "Pipeline position" });
    expect(within(group).getAllByRole("button")).toHaveLength(3);
    expect(within(group).getByRole("button", { name: "Execute" })).toHaveAttribute(
      "data-stage-status",
      "attention",
    );
    expect(within(group).queryByRole("button", { name: /Intake|Prepare/ })).toBeNull();
  });
});
