// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  planRefetch: vi.fn(),
  projectRefetch: vi.fn(),
  useQuery: vi.fn(),
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
  RelayCanonicalPlanDetail: () => <div>Plan detail</div>,
}));

vi.mock("@/features/relay-plans", () => ({
  workflowPlanDetailQueryOptions: () => ({ queryKey: ["plan"] }),
}));

vi.mock("@/features/relay-projects", () => ({
  workflowProjectsListQueryOptions: () => ({ queryKey: ["projects"] }),
}));

import { PlanDetailPage } from "./$planId";

describe("PlanDetailPage Project context failure", () => {
  beforeEach(() => {
    mocks.planRefetch.mockReset();
    mocks.projectRefetch.mockReset();
    mocks.useQuery.mockReset();
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
