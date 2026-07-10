// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayProjectRepositoriesPanel } from "./RelayProjectRepositoriesPanel";
import { RelayApiError } from "@/features/relay-runs";

const mocks = vi.hoisted(() => ({
  attachRepository: vi.fn(),
  detachRepository: vi.fn(),
}));

vi.mock("@/features/relay-projects", () => ({
  attachWorkflowProjectRepository: mocks.attachRepository,
  detachWorkflowProjectRepository: mocks.detachRepository,
  workflowProjectKeys: {
    all: ["workflow-projects"],
    details: () => ["workflow-projects", "detail"],
  },
  workflowRepositoryTargetsQueryOptions: () => ({
    queryKey: ["workflow-projects", "repository-targets"],
    queryFn: async () => ({
      count: 1,
      repositories: [
        {
          repoTarget: "relay",
          localPath: "D:/Code/relay",
          createdAt: "2026-07-07T00:00:00Z",
          updatedAt: "2026-07-07T01:00:00Z",
        },
      ],
    }),
  }),
}));

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RelayProjectRepositoriesPanel
        projectId="project-1"
        repositories={[
          { repoTarget: "relay", createdAt: "2026-07-07T00:00:00Z" },
        ]}
      />
    </QueryClientProvider>,
  );
  return { ...result, invalidateSpy };
}

describe("RelayProjectRepositoriesPanel", () => {
  beforeEach(() => {
    mocks.attachRepository.mockReset();
    mocks.detachRepository.mockReset();
    mocks.detachRepository.mockResolvedValue(undefined);
  });

  it("shows plain global references and detaches them without copied policy controls", async () => {
    const user = userEvent.setup();
    const { invalidateSpy } = renderPanel();

    expect(await screen.findByText("D:/Code/relay")).toBeInTheDocument();
    for (const legacyText of ["Role", "Default Repository", "Policy Rules", "Enabled"]) {
      expect(screen.queryByText(legacyText)).not.toBeInTheDocument();
    }

    await user.click(screen.getByRole("button", { name: "Detach" }));
    const detachButtons = screen.getAllByRole("button", { name: "Detach" });
    await user.click(detachButtons[detachButtons.length - 1]);

    await waitFor(() => {
      expect(mocks.detachRepository).toHaveBeenCalledWith("project-1", "relay");
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ["workflow-projects", "detail"],
      });
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("announces detach failures inside the active confirmation dialog", async () => {
    const user = userEvent.setup();
    mocks.detachRepository.mockRejectedValue(new RelayApiError(
      "Repository detach rejected",
      409,
      "/api/projects/project-1/repositories/relay",
      "DELETE",
      { error: "CONFLICT", message: "Repository detach rejected" },
    ));
    renderPanel();

    await user.click(screen.getByRole("button", { name: "Detach" }));
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Detach" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Repository detach rejected",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});
