// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  planRefetch: vi.fn(),
  projectRefetch: vi.fn(),
  useQuery: vi.fn(),
  planDetail: vi.fn(
    ({ detail, activeProjects }: { detail: any; activeProjects: any[] }) => (
      <div
        data-testid="canonical-plan-detail"
        data-plan-id={detail.plan.planId}
        data-project-count={activeProjects.length}
      >
        Plan detail
      </div>
    ),
  ),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: mocks.useQuery,
}));

vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (config: Record<string, unknown>) => ({
    ...config,
    useParams: () => ({ planId: "plan-1" }),
  }),
  Link: ({ children, to }: any) => <a href={to}>{children}</a>,
  Outlet: () => <div>Outlet</div>,
  useRouterState: () => "/plans/plan-1",
}));

vi.mock("@/components/relay/RelayCanonicalPlanDetail", () => ({
  RelayCanonicalPlanDetail: mocks.planDetail,
}));

vi.mock("@/features/relay-plans", () => ({
  workflowPlanDetailQueryOptions: () => ({ queryKey: ["plan"] }),
}));

vi.mock("@/features/relay-projects", () => ({
  workflowProjectsListQueryOptions: () => ({ queryKey: ["projects"] }),
}));

import { PlanDetailPage } from "./$planId";

const planDetail = {
  plan: {
    planId: "plan-1",
    project: {
      projectId: "project-1",
      name: "Relay",
      status: "active",
    },
    featureSlug: "relay-specification-workflow-pivot",
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
  repositories: [],
  passes: [
    {
      passId: "pass-1",
      number: 1,
      name: "Canonical frontend",
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

const activeProject = {
  projectId: "project-1",
  name: "Relay",
  description: "",
  status: "active",
  createdAt: "2026-07-08T00:00:00Z",
  updatedAt: "2026-07-08T00:00:00Z",
};

describe("PlanDetailPage canonical read state", () => {
  beforeEach(() => {
    mocks.planRefetch.mockReset();
    mocks.projectRefetch.mockReset();
    mocks.useQuery.mockReset();
    mocks.planDetail.mockClear();
  });

  it("renders normalized Plan detail with concrete empty pass collections", () => {
    mocks.useQuery
      .mockReturnValueOnce({
        data: planDetail,
        isLoading: false,
        isError: false,
        error: null,
        refetch: mocks.planRefetch,
      })
      .mockReturnValueOnce({
        data: { projects: [activeProject], count: 1 },
        isLoading: false,
        isError: false,
        error: null,
        refetch: mocks.projectRefetch,
      });

    render(<PlanDetailPage />);

    expect(screen.getByTestId("canonical-plan-detail")).toHaveAttribute(
      "data-plan-id",
      "plan-1",
    );
    expect(screen.getByTestId("canonical-plan-detail")).toHaveAttribute(
      "data-project-count",
      "1",
    );
    expect(mocks.planDetail.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        detail: planDetail,
        activeProjects: [activeProject],
      }),
    );
  });

  it("shows a recoverable destination-Project error instead of an empty movement set", async () => {
    const user = userEvent.setup();
    mocks.useQuery
      .mockReturnValueOnce({
        data: {
          plan: { planId: "plan-1" },
          passes: [],
          repositories: [],
          artifacts: [],
        },
        isLoading: false,
        error: null,
        refetch: mocks.planRefetch,
      })
      .mockReturnValueOnce({
        data: undefined,
        isLoading: false,
        isError: true,
        error: new Error("Project service unavailable"),
        refetch: mocks.projectRefetch,
      });

    render(<PlanDetailPage />);

    expect(
      screen.getByText("Destination Projects failed to load"),
    ).toBeInTheDocument();
    expect(screen.getByText(/required context failed/)).toBeInTheDocument();
    expect(screen.queryByText("Plan detail")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Retry Projects" }));
    expect(mocks.projectRefetch).toHaveBeenCalledTimes(1);
  });
});
