// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayProjectDetail } from "./RelayProjectDetail";
import type { WorkflowProjectDetail } from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

const mocks = vi.hoisted(() => ({
  archiveProject: vi.fn(),
  restoreProject: vi.fn(),
  updateProject: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: any) => <a href={to}>{children}</a>,
}));

vi.mock("@/features/relay-projects", () => ({
  archiveWorkflowProject: mocks.archiveProject,
  restoreWorkflowProject: mocks.restoreProject,
  updateWorkflowProject: mocks.updateProject,
  workflowProjectKeys: { all: ["workflow-projects"] },
}));

vi.mock("./RelayProjectPlansPanel", () => ({
  RelayProjectPlansPanel: () => <div>Plans Panel</div>,
}));
vi.mock("./RelayProjectRepositoriesPanel", () => ({
  RelayProjectRepositoriesPanel: () => <div>Repository References Panel</div>,
}));
vi.mock("./RelayProjectNotesPanel", () => ({
  RelayProjectNotesPanel: () => <div>Project Notes Panel</div>,
}));

const detail: WorkflowProjectDetail = {
  project: {
    projectId: "project-1",
    name: "Relay",
    description: "Primary workflow work",
    status: "active",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
  },
  repositories: [],
  notes: [],
  plans: [],
};

function renderDetail(value: WorkflowProjectDetail = detail) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RelayProjectDetail detail={value} />
    </QueryClientProvider>,
  );
}

describe("RelayProjectDetail", () => {
  beforeEach(() => {
    mocks.archiveProject.mockReset();
    mocks.restoreProject.mockReset();
    mocks.updateProject.mockReset();
  });

  it("preserves Project identity and lifecycle controls while removing legacy orchestration", () => {
    renderDetail();

    expect(screen.getByRole("heading", { name: "Relay" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit Project" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Archive Project" })).toBeInTheDocument();
    expect(screen.getByText("Plans Panel")).toBeInTheDocument();
    expect(screen.getByText("Repository References Panel")).toBeInTheDocument();
    expect(screen.getByText("Project Notes Panel")).toBeInTheDocument();

    for (const legacyText of [
      "Delete Project",
      "Refactor Backlog",
      "Plan Seeds",
      "Default Repository ID",
      "Policy Rules",
      "Enabled",
    ]) {
      expect(screen.queryByText(legacyText)).not.toBeInTheDocument();
    }
  });
  it("announces edit failures inside the active dialog and preserves form state", async () => {
    const user = userEvent.setup();
    mocks.updateProject.mockRejectedValue(new RelayApiError(
      "Project update rejected",
      400,
      "/api/projects/project-1",
      "PATCH",
      { error: "BAD_REQUEST", message: "Project update rejected" },
    ));
    renderDetail();

    await user.click(screen.getByRole("button", { name: "Edit Project" }));
    const dialog = screen.getByRole("dialog");
    const nameInput = within(dialog).getByLabelText("Project Name");
    await user.clear(nameInput);
    await user.type(nameInput, "Relay Revised");
    await user.click(within(dialog).getByRole("button", { name: "Save Project" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Project update rejected",
    );
    expect(within(dialog).getByLabelText("Project Name")).toHaveValue("Relay Revised");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("announces archive failures inside the confirmation dialog", async () => {
    const user = userEvent.setup();
    mocks.archiveProject.mockRejectedValue(new RelayApiError(
      "Project archive rejected",
      409,
      "/api/projects/project-1/archive",
      "POST",
      { error: "CONFLICT", message: "Project archive rejected" },
    ));
    renderDetail();

    await user.click(screen.getByRole("button", { name: "Archive Project" }));
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Archive Project" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Project archive rejected",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("requires archive confirmation and exposes restore without any delete action", async () => {
    const user = userEvent.setup();
    mocks.archiveProject.mockResolvedValue({});
    const activeRender = renderDetail();

    await user.click(screen.getByRole("button", { name: "Archive Project" }));
    expect(screen.getByRole("heading", { name: "Archive Project?" })).toBeInTheDocument();
    const archiveButtons = screen.getAllByRole("button", { name: "Archive Project" });
    await user.click(archiveButtons[archiveButtons.length - 1]);
    await waitFor(() => {
      expect(mocks.archiveProject).toHaveBeenCalledWith("project-1");
    });

    activeRender.unmount();
    const archivedDetail: WorkflowProjectDetail = {
      project: {
        projectId: "project-1",
        name: "Relay",
        description: "Primary workflow work",
        status: "archived",
        createdAt: "2026-07-07T00:00:00Z",
        updatedAt: "2026-07-07T01:00:00Z",
      },
      repositories: [],
      notes: [],
      plans: [],
    };
    renderDetail(archivedDetail);

    expect(screen.getByRole("button", { name: "Restore Project" })).toBeInTheDocument();
    expect(screen.queryByText("Delete Project")).not.toBeInTheDocument();
  });

});
