// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RelayProjectPlansPanel } from "./RelayProjectPlansPanel";
import type { WorkflowProject, WorkflowProjectPlanSummary } from "@/features/relay-projects";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, search }: any) => (
    <a
      href={to}
      data-plan-id={params?.planId ?? ""}
      data-project-id={search?.projectId ?? ""}
    >
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

function project(status: WorkflowProject["status"]): WorkflowProject {
  return {
    projectId: "project-1",
    name: "Relay",
    description: "Relay work",
    status,
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
  };
}

describe("RelayProjectPlansPanel", () => {
  it("preselects an active Project for Plan submission without locking the workbench", () => {
    render(<RelayProjectPlansPanel project={project("active")} plans={plans} />);

    const submitLink = screen.getByRole("link", { name: /Submit Plan/ });
    expect(submitLink).toHaveAttribute("data-project-id", "project-1");
    expect(screen.getByText("workflow-pivot")).toBeInTheDocument();
  });

  it("keeps existing Plans visible while preventing submission into an archived Project", () => {
    render(<RelayProjectPlansPanel project={project("archived")} plans={plans} />);

    expect(screen.queryByRole("link", { name: /Submit Plan/ })).not.toBeInTheDocument();
    expect(screen.getByText("workflow-pivot")).toBeInTheDocument();
    expect(screen.getByText(/Restore this Project/)).toBeInTheDocument();
  });
});
