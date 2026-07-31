// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RelayProjectPlansPanel } from "./RelayProjectPlansPanel";
import type { WorkflowProjectPlanSummary } from "@/features/relay-projects";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params }: any) => (
    <a href={to} data-plan-id={params?.planId ?? ""}>
      {children}
    </a>
  ),
}));

const plans: WorkflowProjectPlanSummary[] = [
  {
    planId: "plan-1",
    featureSlug: "workflow-pivot",
    status: "active",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
  },
];

describe("RelayProjectPlansPanel", () => {
  it("keeps attached Plans visible as read-only historical records", () => {
    render(<RelayProjectPlansPanel plans={plans} />);

    expect(screen.getByText("workflow-pivot")).toBeInTheDocument();
    const planLink = screen.getByRole("link", { name: /Open Plan/ });
    expect(planLink).toHaveAttribute("data-plan-id", "plan-1");
  });

  // Legacy Plan submission is retired: the Project panel offers no Plan
  // submission entry point in any Project status.
  it("offers no Plan submission entry point", () => {
    render(<RelayProjectPlansPanel plans={plans} />);

    expect(screen.queryByRole("link", { name: /Submit Plan/ })).not.toBeInTheDocument();
    for (const link of screen.queryAllByRole("link")) {
      expect(link.getAttribute("href") ?? "").not.toContain("/plans/new");
    }
  });
});
