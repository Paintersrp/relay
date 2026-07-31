// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  planRefetch: vi.fn(),
  useQuery: vi.fn(),
  planDetail: vi.fn(({ detail }: { detail: any }) => (
    <div data-testid="canonical-plan-detail" data-plan-id={detail.plan.planId}>
      Plan detail
    </div>
  )),
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

describe("PlanDetailPage canonical read state", () => {
  beforeEach(() => {
    mocks.planRefetch.mockReset();
    mocks.useQuery.mockReset();
    mocks.planDetail.mockClear();
  });

  it("renders normalized Plan detail with concrete empty pass collections", () => {
    mocks.useQuery.mockReturnValue({
      data: planDetail,
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.planRefetch,
    });

    render(<PlanDetailPage />);

    expect(screen.getByTestId("canonical-plan-detail")).toHaveAttribute(
      "data-plan-id",
      "plan-1",
    );
    expect(mocks.planDetail.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({ detail: planDetail }),
    );
  });

  // Plan movement is retired, so the route loads only the Plan itself and needs
  // no destination-Project context to render.
  it("recovers from a Plan read failure without any destination-Project context", async () => {
    const user = userEvent.setup();
    mocks.useQuery.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: new Error("Plan service unavailable"),
      refetch: mocks.planRefetch,
    });

    render(<PlanDetailPage />);

    expect(screen.getByText("Plan failed to load")).toBeInTheDocument();
    expect(screen.queryByText("Destination Projects failed to load")).toBeNull();
    expect(screen.queryByText("Plan detail")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Retry Plan" }));
    expect(mocks.planRefetch).toHaveBeenCalledTimes(1);
    expect(mocks.useQuery).toHaveBeenCalledTimes(1);
  });
});
