// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayCanonicalPlanDetail } from "./RelayCanonicalPlanDetail";
import type { WorkflowPlanDetail } from "@/features/relay-plans";

const mocks = vi.hoisted(() => ({
  movePlan: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: any) => <a href={to}>{children}</a>,
}));

vi.mock("@/features/relay-plans", async (importOriginal) => {
  const original = await importOriginal<Record<string, unknown>>();
  return {
    ...original,
    moveWorkflowPlan: mocks.movePlan,
    workflowPlanKeys: {
      all: ["workflow-plans"],
      details: () => ["workflow-plans", "detail"],
    },
  };
});

const detail: WorkflowPlanDetail = {
  plan: {
    planId: "plan-1",
    project: {
      projectId: "project-1",
      name: "Current Project",
      status: "active",
    },
    featureSlug: "feature",
    status: "active",
    canonicalSha256: "a".repeat(64),
    createdAt: "2026-07-08T00:00:00Z",
    updatedAt: "2026-07-08T00:00:00Z",
    passCount: 1,
    completedPassCount: 0,
    inProgressPassCount: 0,
    plannedPassCount: 1,
    currentPassId: "pass-1",
  },
  repositories: [
    {
      repoTarget: "relay",
      branch: "feat/simplification",
      planningBaseCommit: "b".repeat(40),
      sequence: 1,
    },
  ],
  passes: [
    {
      passId: "pass-1",
      number: 1,
      name: "First",
      repoTarget: "relay",
      status: "planned",
      dependsOn: [],
      createdAt: "2026-07-08T00:00:00Z",
      updatedAt: "2026-07-08T00:00:00Z",
      runs: [],
    },
  ],
  artifacts: [],
};

function renderDetail() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RelayCanonicalPlanDetail
        detail={detail}
        activeProjects={[
          {
            projectId: "project-1",
            name: "Current Project",
            description: "",
            status: "active",
            createdAt: "2026-07-08T00:00:00Z",
            updatedAt: "2026-07-08T00:00:00Z",
          },
          {
            projectId: "project-2",
            name: "Destination Project",
            description: "",
            status: "active",
            createdAt: "2026-07-08T00:00:00Z",
            updatedAt: "2026-07-08T00:00:00Z",
          },
        ]}
      />
    </QueryClientProvider>,
  );
}

describe("RelayCanonicalPlanDetail", () => {
  beforeEach(() => {
    mocks.movePlan.mockReset();
  });

  it("contains dialog focus and restores focus to the opener on Escape", async () => {
    const user = userEvent.setup();
    renderDetail();

    const opener = screen.getByRole("button", { name: "Move Plan" });
    opener.focus();
    await user.click(opener);

    const dialog = screen.getByRole("dialog");
    await waitFor(() => {
      expect(dialog).toContainElement(document.activeElement as HTMLElement);
    });

    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("keeps movement errors announced inside the active dialog", async () => {
    const user = userEvent.setup();
    mocks.movePlan.mockRejectedValue(new Error("Destination rejected"));
    renderDetail();

    await user.click(screen.getByRole("button", { name: "Move Plan" }));
    const dialog = screen.getByRole("dialog");
    await user.selectOptions(
      within(dialog).getByLabelText("Active destination Project"),
      "project-2",
    );
    await user.click(within(dialog).getByRole("button", { name: "Move Plan" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Destination rejected",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});
