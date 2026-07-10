// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RelayProjectsRegistry } from "./RelayProjectsRegistry";
import type { WorkflowProject } from "@/features/relay-projects";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params }: any) => {
    const suffix = params?.projectId ? `/${params.projectId}` : "";
    return <a href={`${to}${suffix}`}>{children}</a>;
  },
}));

const projects: WorkflowProject[] = [
  {
    projectId: "project-active",
    name: "Active Project",
    description: "Current work",
    status: "active",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T02:00:00Z",
  },
  {
    projectId: "project-archived",
    name: "Archived Project",
    description: "Historical work",
    status: "archived",
    createdAt: "2026-07-06T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
  },
];

describe("RelayProjectsRegistry", () => {
  it("defaults to active Projects and exposes archived counts and filtering", async () => {
    const user = userEvent.setup();
    render(<RelayProjectsRegistry projects={projects} />);

    expect(screen.getAllByText("Active Project").length).toBeGreaterThan(0);
    expect(screen.queryByText("Archived Project")).not.toBeInTheDocument();
    expect(screen.getByText((_content, element) =>
      element?.textContent === "1 active")).toBeInTheDocument();
    expect(screen.getByText((_content, element) =>
      element?.textContent === "1 archived")).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /Archived/ }));

    expect(screen.getAllByText("Archived Project").length).toBeGreaterThan(0);
    expect(screen.queryByText("Active Project")).not.toBeInTheDocument();
  });

  it("does not expose copied repository configuration columns", () => {
    render(<RelayProjectsRegistry projects={projects} />);

    expect(screen.queryByText("Default Repo")).not.toBeInTheDocument();
    expect(screen.queryByText("Policy Rules")).not.toBeInTheDocument();
    expect(screen.queryByText("Enabled")).not.toBeInTheDocument();
  });
});
