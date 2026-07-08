// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayProjectForm } from "./RelayProjectForm";

const mocks = vi.hoisted(() => ({
  createProject: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/features/relay-projects", () => ({
  createWorkflowProject: mocks.createProject,
  workflowProjectKeys: {
    all: ["workflow-projects"],
  },
}));

function renderForm() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RelayProjectForm />
    </QueryClientProvider>,
  );
}

describe("RelayProjectForm", () => {
  beforeEach(() => {
    mocks.createProject.mockReset();
    mocks.navigate.mockReset();
  });

  it("creates an active server-identified Project from name and description only", async () => {
    const user = userEvent.setup();
    mocks.createProject.mockResolvedValue({
      projectId: "project-generated",
      name: "Relay",
      description: "Primary work",
      status: "active",
      createdAt: "2026-07-07T00:00:00Z",
      updatedAt: "2026-07-07T00:00:00Z",
    });
    renderForm();

    expect(screen.queryByLabelText(/Project ID/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Status/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Default Repository/)).not.toBeInTheDocument();

    await user.type(screen.getByLabelText(/Project Name/), "Relay");
    await user.type(screen.getByLabelText("Description"), "Primary work");
    await user.click(screen.getByRole("button", { name: "Create Project" }));

    await waitFor(() => {
      expect(mocks.createProject).toHaveBeenCalled();
    });
    expect(mocks.createProject.mock.calls[0]?.[0]).toEqual({
      name: "Relay",
      description: "Primary work",
    });
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/projects/$projectId",
      params: { projectId: "project-generated" },
    });
  });

  it("preserves entered values after a recoverable mutation failure", async () => {
    const user = userEvent.setup();
    mocks.createProject.mockRejectedValue(new Error("Project operation conflicts with current state"));
    renderForm();

    const nameInput = screen.getByLabelText(/Project Name/);
    const descriptionInput = screen.getByLabelText("Description");
    await user.type(nameInput, "Relay");
    await user.type(descriptionInput, "Keep this text");
    await user.click(screen.getByRole("button", { name: "Create Project" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Project operation conflicts with current state",
    );
    expect(nameInput).toHaveValue("Relay");
    expect(descriptionInput).toHaveValue("Keep this text");
  });
});
