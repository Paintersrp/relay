// @vitest-environment jsdom
//
// ============================================================
// Example/unit tests — RunStatusTrackerLayout load-failed state and
// action-failure escalation (task 10.3)
// ============================================================
//
// Covers Requirement 6.5: when the run detail query fails to load or
// returns no run, `RunStatusTrackerLayout` renders the existing
// load-failed state (with a link back to the runs registry) instead of
// any of the five tracker regions (`IdentityStrip`, `CurrentStatusBlock`,
// `NextActionArea`, `ProgressionRail`, `DetailDisclosure`).
//
// Covers Requirement 6.6: when an action invocation triggered from
// `NextActionArea` fails (the `onActionClick` callback throws
// synchronously, or returns a Promise that rejects),
// `RunStatusTrackerLayout` escalates `CurrentStatusBlock`'s rendered tone
// to "danger" with a short failure sentence, and returns the failing
// control to enabled rather than leaving it disabled.

import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

import { RunStatusTrackerLayout } from "./RunStatusTrackerLayout";
import type {
  CurrentStatusView,
  RelayRun,
  StepActionsView,
} from "@/features/relay-runs/runStatusTrackerViews";

// ------------------------------------------------------------
// Fixtures
// ------------------------------------------------------------

const BASE_RUN: RelayRun = {
  id: "run-tracker-1",
  title: "Ship the tracker redesign",
  name: "Ship the tracker redesign",
  repo: "acme/relay-ui",
  branch: "feature/tracker",
  activeStep: "execute",
  status: "executor_running",
  lifecycleState: "execute",
  createdAt: "2024-01-01T00:00:00.000Z",
  updatedAt: "2024-01-01T00:00:00.000Z",
  summary: "",
  model: "claude-sonnet",
  riskLevel: "low",
  validation: { errors: 0, warnings: 0 },
  artifacts: [],
  latestEvents: [],
  statusSeverity: "info",
  state: "running",
} as unknown as RelayRun;

const BASE_STATUS: CurrentStatusView = {
  tone: "info",
  headline: "Executor is running.",
  updatedAt: "2024-01-01T00:00:00.000Z",
};

/**
 * Mount `RunStatusTrackerLayout` inside a small in-memory router that
 * registers the starting run route plus the "/runs" registry destination —
 * mirroring the router-setup convention used by `PlanPassJumpLink.test.tsx`
 * and `RunWorkbenchLayout.test.tsx` — since `RunWorkbenchLoadFailedState`
 * renders a `Link to="/runs"` and `IdentityStrip`/`DetailDisclosure`
 * (reached via the happy-path render) may also depend on router context.
 */
async function renderLayout(props: {
  run: RelayRun | null | undefined;
  loadFailed?: boolean;
  currentStatus: CurrentStatusView;
  actionsView?: StepActionsView;
  onActionClick?: (id: string) => void | Promise<unknown>;
}) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const runRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/execute",
    component: () => (
      <RunStatusTrackerLayout
        run={props.run}
        loadFailed={props.loadFailed}
        currentStep="execute"
        currentStatus={props.currentStatus}
        actionsView={props.actionsView}
        onActionClick={props.onActionClick}
        progression={[]}
        detailSections={[]}
      />
    ),
  });

  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: () => <div>runs-registry-destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([runRoute, runsRoute]),
    history: createMemoryHistory({
      initialEntries: [`/runs/${props.run?.id ?? "missing-run"}/execute`],
    }),
    defaultPendingMinMs: 0,
  });
  await router.load();

  return render(<RouterProvider router={router} />);
}

// ------------------------------------------------------------
// Requirement 6.5 — load-failed state
// ------------------------------------------------------------

describe("RunStatusTrackerLayout — load-failed state (Req 6.5)", () => {
  it("renders the load-failed state and none of the five regions when run is null", async () => {
    await renderLayout({ run: null, currentStatus: BASE_STATUS });

    expect(screen.getByText(/run failed to load/i)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /back to runs/i }),
    ).toBeInTheDocument();

    expect(screen.queryByTestId("run-status-tracker-layout")).not.toBeInTheDocument();
    expect(screen.queryByTestId("current-status-block")).not.toBeInTheDocument();
    expect(screen.queryByTestId("progression-rail")).not.toBeInTheDocument();
    expect(screen.queryByTestId("detail-disclosure")).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: /pipeline position/i })).not.toBeInTheDocument();
  });

  it("renders the load-failed state and none of the five regions when run is undefined", async () => {
    await renderLayout({ run: undefined, currentStatus: BASE_STATUS });

    expect(screen.getByText(/run failed to load/i)).toBeInTheDocument();
    expect(screen.queryByTestId("run-status-tracker-layout")).not.toBeInTheDocument();
    expect(screen.queryByTestId("current-status-block")).not.toBeInTheDocument();
  });

  it("renders the load-failed state when loadFailed is true even with a stale run value present", async () => {
    await renderLayout({
      run: BASE_RUN,
      loadFailed: true,
      currentStatus: BASE_STATUS,
    });

    expect(screen.getByText(/run failed to load/i)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /back to runs/i }),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("run-status-tracker-layout")).not.toBeInTheDocument();
    expect(screen.queryByTestId("current-status-block")).not.toBeInTheDocument();
    expect(screen.queryByTestId("progression-rail")).not.toBeInTheDocument();
    expect(screen.queryByTestId("detail-disclosure")).not.toBeInTheDocument();
  });

  it("navigates back to the runs registry when the load-failed link is activated", async () => {
    const user = userEvent.setup();
    await renderLayout({ run: null, currentStatus: BASE_STATUS });

    await user.click(screen.getByRole("link", { name: /back to runs/i }));

    expect(await screen.findByText("runs-registry-destination")).toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// Requirement 6.6 — action-failure escalation
// ------------------------------------------------------------

function buildSinglePrimaryActionsView(id = "start"): StepActionsView {
  return {
    controls: [
      {
        id,
        label: "Start executor",
        enabled: true,
        isPrimary: true,
      },
    ],
    nextSafeActionId: id,
  };
}

describe("RunStatusTrackerLayout — action-failure escalation (Req 6.6)", () => {
  it("escalates tone to danger with a failure sentence when onActionClick throws synchronously, and returns the control to enabled", async () => {
    const user = userEvent.setup();
    const onActionClick = vi.fn(() => {
      throw new Error("boom");
    });

    await renderLayout({
      run: BASE_RUN,
      currentStatus: BASE_STATUS,
      actionsView: buildSinglePrimaryActionsView(),
      onActionClick,
    });

    const statusBlock = screen.getByTestId("current-status-block");
    expect(statusBlock).toHaveAttribute("data-tone", "info");

    const primaryButton = screen.getByRole("button", { name: /start executor/i });
    expect(primaryButton).not.toBeDisabled();

    await user.click(primaryButton);

    expect(onActionClick).toHaveBeenCalledWith("start");

    const escalated = screen.getByTestId("current-status-block");
    expect(escalated).toHaveAttribute("data-tone", "danger");
    expect(screen.getByTestId("current-status-detail")).toHaveTextContent(
      /didn.?t go through/i,
    );

    // The failing control returns to enabled, not left stuck disabled.
    const buttonAfterFailure = screen.getByRole("button", { name: /start executor/i });
    expect(buttonAfterFailure).not.toBeDisabled();
  });

  it("escalates tone to danger with a failure sentence when onActionClick returns a rejected Promise, and returns the control to enabled", async () => {
    const user = userEvent.setup();
    const onActionClick = vi.fn(() => Promise.reject(new Error("network down")));

    await renderLayout({
      run: BASE_RUN,
      currentStatus: BASE_STATUS,
      actionsView: buildSinglePrimaryActionsView(),
      onActionClick,
    });

    const primaryButton = screen.getByRole("button", { name: /start executor/i });
    await user.click(primaryButton);

    expect(onActionClick).toHaveBeenCalledWith("start");

    const escalated = await screen.findByTestId("current-status-block");
    expect(escalated).toHaveAttribute("data-tone", "danger");
    expect(screen.getByTestId("current-status-detail")).toHaveTextContent(
      /didn.?t go through/i,
    );

    const buttonAfterFailure = screen.getByRole("button", { name: /start executor/i });
    expect(buttonAfterFailure).not.toBeDisabled();
  });

  it("does not escalate tone when onActionClick succeeds", async () => {
    const user = userEvent.setup();
    const onActionClick = vi.fn(() => Promise.resolve());

    await renderLayout({
      run: BASE_RUN,
      currentStatus: BASE_STATUS,
      actionsView: buildSinglePrimaryActionsView(),
      onActionClick,
    });

    const primaryButton = screen.getByRole("button", { name: /start executor/i });
    await user.click(primaryButton);

    expect(onActionClick).toHaveBeenCalledWith("start");
    expect(screen.getByTestId("current-status-block")).toHaveAttribute("data-tone", "info");
    expect(screen.queryByTestId("current-status-detail")).not.toBeInTheDocument();
  });
});
