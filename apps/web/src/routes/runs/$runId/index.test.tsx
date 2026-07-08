// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useQuery: vi.fn(),
  refetch: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: mocks.useQuery,
  queryOptions: vi.fn((options) => options),
}));

vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (config: Record<string, unknown>) => ({
    ...config,
    useParams: () => ({ runId: "run-1" }),
  }),
  Navigate: (props: Record<string, unknown>) => {
    mocks.navigate(props);
    return <div data-testid="run-index-redirect" />;
  },
}));

import { RunIndexPage } from "./index";

describe("Run index route", () => {
  beforeEach(() => {
    mocks.useQuery.mockReset();
    mocks.refetch.mockReset();
    mocks.navigate.mockReset();
  });

  it("renders the bounded loading state while the durable Run stage resolves", () => {
    mocks.useQuery.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mocks.refetch,
    });

    render(<RunIndexPage />);

    expect(screen.getByText("Loading Run")).toBeInTheDocument();
    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("renders the recoverable Run error state without redirecting", async () => {
    const user = userEvent.setup();
    mocks.useQuery.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error("Run service unavailable"),
      refetch: mocks.refetch,
    });

    render(<RunIndexPage />);

    expect(screen.getByText("Run failed to load")).toBeInTheDocument();
    expect(screen.getByText("Run service unavailable")).toBeInTheDocument();
    expect(mocks.navigate).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Retry Run" }));
    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });

  it("redirects only the exact bare Run URL to the durable stage", () => {
    mocks.useQuery.mockReturnValue({
      data: {
        run: {
          runId: "run-1",
          stage: "execute",
        },
      },
      isLoading: false,
      error: null,
      refetch: mocks.refetch,
    });

    render(<RunIndexPage />);

    expect(screen.getByTestId("run-index-redirect")).toBeInTheDocument();
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/runs/$runId/execute",
      params: { runId: "run-1" },
      replace: true,
    });
  });
});
