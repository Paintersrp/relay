// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayRepositoryRegistrationDialog } from "./RelayRepositoryRegistrationDialog";

const mocks = vi.hoisted(() => ({
  inspectRepository: vi.fn(),
  confirmRepository: vi.fn(),
  attachRepository: vi.fn(),
}));

vi.mock("@/features/relay-projects", () => ({
  inspectWorkflowRepository: mocks.inspectRepository,
  confirmWorkflowRepository: mocks.confirmRepository,
  attachWorkflowProjectRepository: mocks.attachRepository,
  WorkflowRepositoryConfirmationError: class WorkflowRepositoryConfirmationError extends Error {
    inspection: ReturnType<typeof readyInspection>;

    constructor(inspection: ReturnType<typeof readyInspection>) {
      super("Repository inspection changed. Review the refreshed values and confirm again.");
      this.inspection = inspection;
    }
  },
  workflowProjectKeys: {
    repositories: () => ["workflow-projects", "repository-targets"],
    details: () => ["workflow-projects", "detail"],
  },
}));

function remote(name: string, url = `git@github.com:Paintersrp/${name}.git`) {
  return {
    name,
    url,
    suggestedRepoTarget: name === "origin" ? "relay" : name,
  };
}

function readyInspection(
  overrides: Record<string, unknown> = {},
) {
  return {
    state: "ready",
    selectedPath: "D:/Code/relay/internal",
    resolvedLocalPath: "D:/Code/relay",
    remotes: [remote("origin")],
    selectedRemote: remote("origin"),
    suggestedRepoTarget: "relay",
    repoTarget: "relay",
    repoTargetSource: "remote_basename",
    registrationDisposition: "create",
    confirmationHash: "a".repeat(64),
    notices: [],
    ...overrides,
  };
}

function remoteSelectionInspection() {
  return {
    state: "needs_remote_selection",
    selectedPath: "D:/Code/relay",
    resolvedLocalPath: "D:/Code/relay",
    remotes: [
      remote("upstream", "git@github.com:Paintersrp/relay.git"),
      remote("fork", "git@github.com:fork/relay.git"),
    ],
    notices: ["Select one configured remote."],
  };
}

function targetOverrideInspection(
  reason: "no_usable_remote" | "unsupported_remote" = "no_usable_remote",
) {
  const unsupported = reason === "unsupported_remote";
  return {
    state: "needs_target_override",
    selectedPath: "D:/Code/local-only",
    resolvedLocalPath: "D:/Code/local-only",
    remotes: unsupported
      ? [remote("origin", "owner/relay.git")]
      : [],
    selectedRemote: unsupported
      ? remote("origin", "owner/relay.git")
      : undefined,
    targetOverrideReason: reason,
    notices: unsupported
      ? [
          `Remote "origin" uses URL "owner/relay.git", which Relay cannot normalize into a repository target.`,
        ]
      : [
          "No usable configured Git remote was found. Enter a valid slash-free Relay repository target to continue.",
        ],
  };
}

function registration(outcome: "created" | "reused" = "created") {
  return {
    outcome,
    repository: {
      repoTarget: "relay",
      localPath: "D:/Code/relay",
      createdAt: "2026-07-11T00:00:00Z",
      updatedAt: "2026-07-11T00:00:00Z",
    },
  };
}

function renderDialog(options: {
  projectId?: string;
  onCompleted?: (result: ReturnType<typeof registration>) => void;
  onPartialSuccess?: (
    result: ReturnType<typeof registration>,
    message: string,
  ) => void;
} = {}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const onOpenChange = vi.fn();
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RelayRepositoryRegistrationDialog
        open
        onOpenChange={onOpenChange}
        projectId={options.projectId}
        onCompleted={options.onCompleted}
        onPartialSuccess={options.onPartialSuccess}
      />
    </QueryClientProvider>,
  );
  return { ...result, onOpenChange, queryClient };
}

async function inspectPath(user: ReturnType<typeof userEvent.setup>, path = "D:/Code/relay") {
  await user.type(screen.getByLabelText("Local repository path"), path);
  await user.click(screen.getByRole("button", { name: "Inspect repository" }));
}

describe("RelayRepositoryRegistrationDialog", () => {
  beforeEach(() => {
    mocks.inspectRepository.mockReset();
    mocks.confirmRepository.mockReset();
    mocks.attachRepository.mockReset();
    mocks.inspectRepository.mockResolvedValue(readyInspection());
    mocks.confirmRepository.mockResolvedValue(registration());
    mocks.attachRepository.mockResolvedValue({
      repoTarget: "relay",
      createdAt: "2026-07-11T00:00:00Z",
    });
  });

  it("exposes labelled controls, requires inspection, and invalidates the preview after input changes", async () => {
    const user = userEvent.setup();
    renderDialog();

    const dialog = screen.getByRole("dialog", { name: "Register local repository" });
    expect(within(dialog).getByLabelText("Local repository path")).toBeRequired();
    expect(
      within(dialog).getByRole("button", { name: "Confirm registration" }),
    ).toBeDisabled();

    await inspectPath(user, "D:/Code/relay/internal");

    expect(await within(dialog).findByText("D:/Code/relay")).toBeInTheDocument();
    expect(within(dialog).getByText("Effective target")).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Confirm registration" }),
    ).toBeEnabled();

    await user.type(within(dialog).getByLabelText("Local repository path"), "/changed");
    expect(within(dialog).queryByText("Resolved root")).not.toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Confirm registration" }),
    ).toBeDisabled();
  });

  it("clears the path-scoped remote before inspecting a different repository", async () => {
    const user = userEvent.setup();
    mocks.inspectRepository
      .mockResolvedValueOnce(readyInspection())
      .mockResolvedValueOnce(targetOverrideInspection());
    renderDialog();

    await inspectPath(user, "D:/Code/relay");
    await screen.findByText("Effective target");

    const pathInput = screen.getByLabelText("Local repository path");
    await user.clear(pathInput);
    await user.type(pathInput, "D:/Code/local-only");
    await user.click(screen.getByRole("button", { name: "Inspect repository" }));

    await waitFor(() => {
      expect(mocks.inspectRepository).toHaveBeenLastCalledWith({
        localPath: "D:/Code/local-only",
        remoteName: undefined,
        repoTargetOverride: undefined,
      });
    });
    expect(
      await screen.findByLabelText("Repository target override"),
    ).toBeInTheDocument();
  });

  it("supports explicit remote selection and reinspects with the selected remote", async () => {
    const user = userEvent.setup();
    mocks.inspectRepository
      .mockResolvedValueOnce(remoteSelectionInspection())
      .mockResolvedValueOnce(readyInspection({
        selectedRemote: remote("upstream", "git@github.com:Paintersrp/relay.git"),
      }));
    renderDialog();

    await inspectPath(user);
    const remoteSelect = await screen.findByRole("combobox", {
      name: "Repository remote",
    });
    await user.click(remoteSelect);
    await user.click(await screen.findByRole("option", { name: /upstream/ }));
    expect(screen.queryByText("Resolved root")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Inspect repository" }));
    await waitFor(() => {
      expect(mocks.inspectRepository).toHaveBeenLastCalledWith({
        localPath: "D:/Code/relay",
        remoteName: "upstream",
        repoTargetOverride: undefined,
      });
    });
    expect(await screen.findByText("Effective target")).toBeInTheDocument();
  });

  it.each([
    {
      reason: "no_usable_remote" as const,
      expected:
        "No usable configured Git remote was found. Enter a valid slash-free Relay repository target to continue.",
    },
    {
      reason: "unsupported_remote" as const,
      expected:
        "Remote origin uses a URL that Relay cannot normalize into a repository target.",
    },
  ])("explains $reason before requesting a target override", async ({ reason, expected }) => {
    const user = userEvent.setup();
    mocks.inspectRepository.mockResolvedValueOnce(targetOverrideInspection(reason));
    renderDialog();

    await inspectPath(user, "D:/Code/local-only");

    expect(await screen.findByRole("status")).toHaveTextContent(expected);
    expect(await screen.findByLabelText("Repository target override")).toBeInTheDocument();
  });

  it("supports a target override when local metadata cannot derive a valid target", async () => {
    const user = userEvent.setup();
    mocks.inspectRepository
      .mockResolvedValueOnce(targetOverrideInspection())
      .mockResolvedValueOnce(readyInspection({
        selectedRemote: undefined,
        suggestedRepoTarget: undefined,
        repoTarget: "local-relay",
        repoTargetSource: "operator_override",
      }));
    renderDialog();

    await inspectPath(user, "D:/Code/local-only");
    const override = await screen.findByLabelText("Repository target override");
    await user.type(override, "local-relay");
    await user.click(screen.getByRole("button", { name: "Inspect repository" }));

    await waitFor(() => {
      expect(mocks.inspectRepository).toHaveBeenLastCalledWith({
        localPath: "D:/Code/local-only",
        remoteName: undefined,
        repoTargetOverride: "local-relay",
      });
    });
    expect(await screen.findByText("local-relay")).toBeInTheDocument();
  });

  it.each(["created", "reused"] as const)(
    "returns the actual %s confirmation result and announces it",
    async (outcome) => {
      const user = userEvent.setup();
      const onCompleted = vi.fn();
      mocks.confirmRepository.mockResolvedValue(registration(outcome));
      renderDialog({ onCompleted });

      await inspectPath(user);
      await user.click(await screen.findByRole("button", { name: "Confirm registration" }));

      await waitFor(() => {
        expect(onCompleted).toHaveBeenCalledWith(registration(outcome));
      });
      expect(await screen.findByRole("status")).toHaveTextContent(
        `Repository relay was ${outcome}.`,
      );
    },
  );

  it("refreshes a stale confirmation preview and requires a second explicit confirmation", async () => {
    const user = userEvent.setup();
    const refreshed = readyInspection({
      repoTarget: "relay-local",
      repoTargetSource: "operator_override",
      registrationDisposition: "reuse",
      confirmationHash: "b".repeat(64),
    });
    const ConfirmationError = (
      await import("@/features/relay-projects")
    ).WorkflowRepositoryConfirmationError as unknown as new (
      inspection: ReturnType<typeof readyInspection>,
    ) => Error;
    mocks.confirmRepository.mockRejectedValueOnce(new ConfirmationError(refreshed));
    renderDialog();

    await inspectPath(user);
    await user.click(await screen.findByRole("button", { name: "Confirm registration" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Repository inspection changed",
    );
    expect(screen.getByText("relay-local")).toBeInTheDocument();
    expect(screen.getByText("Reuse the equivalent global registration.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Confirm registration" })).toBeEnabled();
    expect(mocks.confirmRepository).toHaveBeenCalledTimes(1);
  });

  it("registers and attaches from Project context before reporting completion", async () => {
    const user = userEvent.setup();
    const onCompleted = vi.fn();
    renderDialog({ projectId: "project-1", onCompleted });

    await inspectPath(user);
    await user.click(await screen.findByRole("button", { name: "Register and attach" }));

    await waitFor(() => {
      expect(mocks.confirmRepository).toHaveBeenCalledWith({
        localPath: "D:/Code/relay",
        remoteName: "origin",
        repoTargetOverride: undefined,
        expectedConfirmationHash: "a".repeat(64),
      });
      expect(mocks.attachRepository).toHaveBeenCalledWith("project-1", "relay");
      expect(onCompleted).toHaveBeenCalledWith(registration());
    });
    expect(await screen.findByRole("status")).toHaveTextContent(
      "created and attached to this Project",
    );
  });

  it("reports a persistent partial-success result when Project attachment fails", async () => {
    const user = userEvent.setup();
    const onCompleted = vi.fn();
    const onPartialSuccess = vi.fn();
    mocks.attachRepository.mockRejectedValue(new Error("attachment rejected"));
    renderDialog({
      projectId: "project-1",
      onCompleted,
      onPartialSuccess,
    });

    await inspectPath(user);
    await user.click(await screen.findByRole("button", { name: "Register and attach" }));

    const status = await screen.findByRole("status");
    expect(status).toHaveTextContent(
      "was created globally but was not attached to this Project: attachment rejected",
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(onCompleted).not.toHaveBeenCalled();
    expect(onPartialSuccess).toHaveBeenCalledWith(
      registration(),
      expect.stringContaining("was not attached"),
    );
  });
});
