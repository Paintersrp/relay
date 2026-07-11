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
  inspectRepository: vi.fn(),
  confirmRepository: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    children,
  }: {
    to: string;
    children: React.ReactNode;
  }) => <a href={to}>{children}</a>,
}));

vi.mock("@/features/relay-projects", () => ({
  attachWorkflowProjectRepository: mocks.attachRepository,
  detachWorkflowProjectRepository: mocks.detachRepository,
  inspectWorkflowRepository: mocks.inspectRepository,
  confirmWorkflowRepository: mocks.confirmRepository,
  WorkflowRepositoryConfirmationError: class WorkflowRepositoryConfirmationError extends Error {
    inspection: unknown;
    constructor(inspection: unknown) {
      super("stale inspection");
      this.inspection = inspection;
    }
  },
  workflowProjectKeys: {
    all: ["workflow-projects"],
    details: () => ["workflow-projects", "detail"],
    repositories: () => ["workflow-projects", "repository-targets"],
  },
  workflowRepositoryTargetsQueryOptions: () => ({
    queryKey: ["workflow-projects", "repository-targets"],
    queryFn: async () => ({
      count: 2,
      repositories: [
        {
          repoTarget: "relay",
          localPath: "D:/Code/relay",
          createdAt: "2026-07-07T00:00:00Z",
          updatedAt: "2026-07-07T01:00:00Z",
        },
        {
          repoTarget: "relay-specs",
          localPath: "D:/Code/relay-specs",
          createdAt: "2026-07-07T00:00:00Z",
          updatedAt: "2026-07-07T01:00:00Z",
        },
      ],
    }),
  }),
}));

function readyInspection(disposition: "create" | "reuse" = "reuse") {
  return {
    state: "ready",
    selectedPath: "D:/Code/relay",
    resolvedLocalPath: "D:/Code/relay",
    remotes: [
      {
        name: "origin",
        url: "git@github.com:Paintersrp/relay.git",
        suggestedRepoTarget: "relay",
      },
    ],
    selectedRemote: {
      name: "origin",
      url: "git@github.com:Paintersrp/relay.git",
      suggestedRepoTarget: "relay",
    },
    suggestedRepoTarget: "relay",
    repoTarget: "relay",
    repoTargetSource: "remote_basename",
    registrationDisposition: disposition,
    existingRepository:
      disposition === "reuse"
        ? {
            repoTarget: "relay",
            localPath: "D:/Code/relay",
            createdAt: "2026-07-07T00:00:00Z",
            updatedAt: "2026-07-07T01:00:00Z",
          }
        : undefined,
    confirmationHash: "a".repeat(64),
    notices: [],
  };
}

function conflictInspection() {
  return {
    state: "conflict",
    selectedPath: "D:/Other/relay",
    resolvedLocalPath: "D:/Other/relay",
    remotes: [
      {
        name: "origin",
        url: "git@github.com:Paintersrp/relay.git",
        suggestedRepoTarget: "relay",
      },
    ],
    selectedRemote: {
      name: "origin",
      url: "git@github.com:Paintersrp/relay.git",
      suggestedRepoTarget: "relay",
    },
    suggestedRepoTarget: "relay",
    repoTarget: "relay",
    repoTargetSource: "remote_basename",
    existingRepository: {
      repoTarget: "relay",
      localPath: "D:/Code/relay",
      createdAt: "2026-07-07T00:00:00Z",
      updatedAt: "2026-07-07T01:00:00Z",
    },
    conflictKind: "target",
    notices: [],
  };
}

function registration(outcome: "created" | "reused" = "reused") {
  return {
    outcome,
    repository: {
      repoTarget: "relay",
      localPath: "D:/Code/relay",
      createdAt: "2026-07-07T00:00:00Z",
      updatedAt: "2026-07-07T01:00:00Z",
    },
  };
}

function renderPanel(
  repositories = [
    { repoTarget: "relay", createdAt: "2026-07-07T00:00:00Z" },
  ],
) {
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
        repositories={repositories}
      />
    </QueryClientProvider>,
  );
  return { ...result, invalidateSpy };
}

async function openRegistration(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Register repository" }));
  return screen.getByRole("dialog", { name: "Register local repository" });
}

describe("RelayProjectRepositoriesPanel", () => {
  beforeEach(() => {
    mocks.attachRepository.mockReset();
    mocks.detachRepository.mockReset();
    mocks.inspectRepository.mockReset();
    mocks.confirmRepository.mockReset();
    mocks.attachRepository.mockResolvedValue({
      repoTarget: "relay",
      createdAt: "2026-07-07T00:00:00Z",
    });
    mocks.detachRepository.mockResolvedValue(undefined);
    mocks.inspectRepository.mockResolvedValue(readyInspection());
    mocks.confirmRepository.mockResolvedValue(registration());
  });

  it("keeps existing global attachment and detach behavior", async () => {
    const user = userEvent.setup();
    const { invalidateSpy } = renderPanel();

    expect(await screen.findByText("D:/Code/relay")).toBeInTheDocument();

    const targetSelect = screen.getByRole("combobox", {
      name: "Global repository target",
    });
    await user.click(targetSelect);
    await user.click(await screen.findByRole("option", { name: "relay-specs" }));
    await user.click(screen.getByRole("button", { name: "Attach" }));

    await waitFor(() => {
      expect(mocks.attachRepository).toHaveBeenCalledWith(
        "project-1",
        "relay-specs",
      );
    });
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Repository relay-specs was attached.",
    );

    await user.click(screen.getByRole("button", { name: "Detach" }));
    const detachDialog = screen.getByRole("dialog", {
      name: "Detach repository target?",
    });
    await user.click(within(detachDialog).getByRole("button", { name: "Detach" }));

    await waitFor(() => {
      expect(mocks.detachRepository).toHaveBeenCalledWith("project-1", "relay");
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ["workflow-projects", "detail"],
      });
    });
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Repository relay was detached.",
    );
  });

  it.each(["created", "reused"] as const)(
    "%s registration attaches and persists the actual Project-panel result",
    async (outcome) => {
      const user = userEvent.setup();
      mocks.inspectRepository.mockResolvedValue(
        readyInspection(outcome === "created" ? "create" : "reuse"),
      );
      mocks.confirmRepository.mockResolvedValue(registration(outcome));
      renderPanel([]);
      expect(
        screen.getByRole("link", { name: "Manage repositories" }),
      ).toHaveAttribute("href", "/repositories");

      const registrationDialog = await openRegistration(user);
      await user.type(
        within(registrationDialog).getByLabelText("Local repository path"),
        "D:/Code/relay",
      );
      await user.click(
        within(registrationDialog).getByRole("button", {
          name: "Inspect repository",
        }),
      );
      await user.click(
        await within(registrationDialog).findByRole("button", {
          name: "Register and attach",
        }),
      );

      await waitFor(() => {
        expect(mocks.confirmRepository).toHaveBeenCalled();
        expect(mocks.attachRepository).toHaveBeenCalledWith("project-1", "relay");
      });
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(screen.getByRole("status")).toHaveTextContent(
        `Repository relay was ${outcome} and attached to this Project.`,
      );
    },
  );

  it("keeps blocked registration in the dialog without changing attachments", async () => {
    const user = userEvent.setup();
    mocks.inspectRepository.mockResolvedValue(conflictInspection());
    renderPanel();

    const registrationDialog = await openRegistration(user);
    await user.type(
      within(registrationDialog).getByLabelText("Local repository path"),
      "D:/Other/relay",
    );
    await user.click(
      within(registrationDialog).getByRole("button", {
        name: "Inspect repository",
      }),
    );

    expect(await within(registrationDialog).findByText("Registration conflict")).toBeInTheDocument();
    expect(
      within(registrationDialog).getByRole("button", {
        name: "Register and attach",
      }),
    ).toBeDisabled();
    expect(mocks.confirmRepository).not.toHaveBeenCalled();
    expect(mocks.attachRepository).not.toHaveBeenCalled();
    expect(screen.getByText("D:/Code/relay")).toBeInTheDocument();
  });

  it("persists the global-registration partial success after attachment failure closes the dialog", async () => {
    const user = userEvent.setup();
    mocks.attachRepository.mockRejectedValue(new Error("attachment rejected"));
    mocks.confirmRepository.mockResolvedValue(registration("created"));
    mocks.inspectRepository.mockResolvedValue(readyInspection("create"));
    renderPanel([]);

    const registrationDialog = await openRegistration(user);
    await user.type(
      within(registrationDialog).getByLabelText("Local repository path"),
      "D:/Code/relay",
    );
    await user.click(
      within(registrationDialog).getByRole("button", {
        name: "Inspect repository",
      }),
    );
    await user.click(
      await within(registrationDialog).findByRole("button", {
        name: "Register and attach",
      }),
    );

    await waitFor(() => {
      expect(mocks.attachRepository).toHaveBeenCalledWith("project-1", "relay");
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Repository relay was created globally but was not attached to this Project: attachment rejected",
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("announces detach failures inside the active confirmation dialog", async () => {
    const user = userEvent.setup();
    mocks.detachRepository.mockRejectedValue(
      new RelayApiError(
        "Repository detach rejected",
        409,
        "/api/projects/project-1/repositories/relay",
        "DELETE",
        { error: "CONFLICT", message: "Repository detach rejected" },
      ),
    );
    renderPanel();

    await user.click(screen.getByRole("button", { name: "Detach" }));
    const dialog = screen.getByRole("dialog", {
      name: "Detach repository target?",
    });
    await user.click(within(dialog).getByRole("button", { name: "Detach" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Repository detach rejected",
    );
  });
});
