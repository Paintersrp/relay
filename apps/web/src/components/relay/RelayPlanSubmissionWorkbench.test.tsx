// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayPlanSubmissionWorkbench } from "./RelayPlanSubmissionWorkbench";

const mocks = vi.hoisted(() => ({
  listProjects: vi.fn(),
  submitPlan: vi.fn(),
  validatePlan: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/features/relay-projects", () => ({
  workflowProjectsListQueryOptions: () => ({
    queryKey: ["workflow-projects", "list", { status: "active", limit: 100 }],
    queryFn: mocks.listProjects,
    retry: false,
  }),
}));

vi.mock("@/features/relay-plans", () => ({
  submitWorkflowPlan: mocks.submitPlan,
  validateWorkflowPlan: mocks.validatePlan,
  workflowPlanKeys: { all: ["workflow-plans"] },
}));

vi.mock("@/features/relay-runs", () => ({
  RelayApiError: class RelayApiError extends Error {
    errorShape?: { message?: string };
  },
}));

function renderWorkbench() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RelayPlanSubmissionWorkbench />
    </QueryClientProvider>,
  );
}

describe("RelayPlanSubmissionWorkbench", () => {
  beforeEach(() => {
    mocks.listProjects.mockReset();
    mocks.submitPlan.mockReset();
    mocks.validatePlan.mockReset();
    mocks.navigate.mockReset();
  });

  it("surfaces active-Project query failure as a recoverable error and retries", async () => {
    const user = userEvent.setup();
    mocks.listProjects
      .mockRejectedValueOnce(new Error("Project service unavailable"))
      .mockResolvedValueOnce({
        count: 1,
        projects: [
          {
            projectId: "project-1",
            name: "Relay",
            description: "",
            status: "active",
            createdAt: "2026-07-08T00:00:00Z",
            updatedAt: "2026-07-08T00:00:00Z",
          },
        ],
      });

    renderWorkbench();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Active Projects failed to load");
    expect(alert).toHaveTextContent("Required Project context failed to load");
    expect(screen.getByRole("button", { name: "Submit Plan" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Retry Projects" }));

    await waitFor(() => expect(mocks.listProjects).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(
        screen.queryByText("Active Projects failed to load"),
      ).not.toBeInTheDocument(),
    );
  });

  it("binds preview, diagnostics, notices, and hash to the exact current validated snapshot", async () => {
    const user = userEvent.setup();
    const firstContent = '{"feature_slug":"alpha","passes":[]}';
    const secondContent = '{"feature_slug":"beta","passes":[]}';
    const sha256 = "a".repeat(64);
    mocks.listProjects.mockResolvedValue({
      count: 1,
      projects: [
        {
          projectId: "project-1",
          name: "Relay",
          description: "",
          status: "active",
          createdAt: "2026-07-08T00:00:00Z",
          updatedAt: "2026-07-08T00:00:00Z",
        },
      ],
    });
    mocks.validatePlan.mockResolvedValue({
      ok: true,
      status: "valid",
      kind: "plan",
      sha256,
      diagnostics: [
        { code: "PLAN_WARNING", path: "$.passes", message: "Review pass order" },
      ],
      notices: [{ code: "NORMALIZED", message: "Canonical order preserved" }],
    });

    renderWorkbench();

    fireEvent.change(await screen.findByLabelText("Canonical filename"), {
      target: { value: "alpha.plan.json" },
    });
    fireEvent.change(screen.getByLabelText("Canonical Plan JSON"), {
      target: { value: firstContent },
    });
    await user.click(screen.getByRole("button", { name: "Validate Plan" }));

    expect(await screen.findByTestId("validated-plan-preview")).toHaveTextContent(
      firstContent,
    );
    expect(screen.getByText(/PLAN_WARNING/)).toHaveTextContent(
      "Review pass order",
    );
    expect(screen.getByText(/NORMALIZED/)).toHaveTextContent(
      "Canonical order preserved",
    );
    expect(screen.getByTestId("validated-plan-sha")).toHaveTextContent(sha256);
    expect(mocks.validatePlan).toHaveBeenCalledWith(
      "alpha.plan.json",
      firstContent,
    );

    fireEvent.change(screen.getByLabelText("Canonical filename"), {
      target: { value: "renamed.plan.json" },
    });
    expect(screen.queryByTestId("validated-plan-snapshot")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Canonical filename"), {
      target: { value: "alpha.plan.json" },
    });
    await user.click(screen.getByRole("button", { name: "Validate Plan" }));
    expect(await screen.findByTestId("validated-plan-snapshot")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Canonical Plan JSON"), {
      target: { value: secondContent },
    });
    expect(screen.queryByTestId("validated-plan-snapshot")).not.toBeInTheDocument();
    expect(screen.queryByText(/PLAN_WARNING/)).not.toBeInTheDocument();
    expect(screen.queryByText(sha256)).not.toBeInTheDocument();
  });

  it("keeps file input accessible and exposes the responsive one-to-two-column contract", async () => {
    mocks.listProjects.mockResolvedValue({ count: 0, projects: [] });
    renderWorkbench();

    expect(await screen.findByLabelText("Load Plan file")).toHaveAttribute(
      "type",
      "file",
    );
    expect(screen.getByTestId("plan-submission-layout")).toHaveClass(
      "grid-cols-1",
      "lg:grid-cols-[minmax(0,1fr)_22rem]",
    );
    expect(screen.getByRole("status")).toHaveAttribute("aria-live", "polite");
  });
});
