// @vitest-environment jsdom
//
// ============================================================
// Example test — PlanPassJumpLink client-side navigation (task 8.5)
// ============================================================
//
// Covers Requirement 6.6: activating the Plan_Pass_Link navigates via
// TanStack Router's client-side routing (History API), never a full page
// reload. This is verified by mounting the link inside a real in-memory
// router (mirroring the convention in `RunStepper.test.tsx`), clicking it,
// and asserting the router's own location state updated to the expected
// destination path.

import { describe, expect, it } from "vitest";
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

import type { PlanPassLinkView } from "@/features/relay-runs/runWorkbenchViews";
import { PlanPassJumpLink } from "./PlanPassJumpLink";

const RUN_ID = "run-42";
const PLAN_ID = "plan-7";
const PASS_ID = "pass-3";

const planOnlyView: PlanPassLinkView = {
  present: true,
  to: "/plans/$planId",
  params: { planId: PLAN_ID },
  displayLabel: "Plan plan-7",
  accessibleName: "Jump to Plan plan-7",
};

const planPassView: PlanPassLinkView = {
  present: true,
  to: "/plans/$planId/passes/$passId",
  params: { planId: PLAN_ID, passId: PASS_ID },
  displayLabel: "Pass pass-3",
  accessibleName: "Jump to Plan plan-7 Pass pass-3",
};

const absentView: PlanPassLinkView = {
  present: false,
  displayLabel: "",
  accessibleName: "",
};

/**
 * Mount `PlanPassJumpLink` inside a small in-memory router registering the
 * starting run route plus both possible plan/pass destination routes,
 * mirroring the router-setup convention used by `RunStepper.test.tsx`.
 */
async function renderJumpLinkRouter(view: PlanPassLinkView) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const startRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId",
    component: () => <PlanPassJumpLink view={view} />,
  });

  const planRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/plans/$planId",
    component: () => <div>Plan destination</div>,
  });

  const planPassRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/plans/$planId/passes/$passId",
    component: () => <div>Plan pass destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([startRoute, planRoute, planPassRoute]),
    history: createMemoryHistory({ initialEntries: [`/runs/${RUN_ID}`] }),
    defaultPendingMinMs: 0,
  });
  await router.load();

  const result = render(<RouterProvider router={router} />);
  return { router, ...result };
}

// ------------------------------------------------------------
// Client-side navigation on activation (Req 6.6)
// ------------------------------------------------------------

describe("PlanPassJumpLink — activating the link navigates via client-side routing", () => {
  it("navigates the router's location to the plan destination without a full page reload", async () => {
    const user = userEvent.setup();
    const { router } = await renderJumpLinkRouter(planOnlyView);

    const link = screen.getByRole("link", { name: planOnlyView.accessibleName });
    await user.click(link);

    expect(router.state.location.pathname).toBe(`/plans/${PLAN_ID}`);
    await screen.findByText("Plan destination");
  });

  it("navigates the router's location to the plan+pass destination without a full page reload", async () => {
    const user = userEvent.setup();
    const { router } = await renderJumpLinkRouter(planPassView);

    const link = screen.getByRole("link", { name: planPassView.accessibleName });
    await user.click(link);

    expect(router.state.location.pathname).toBe(
      `/plans/${PLAN_ID}/passes/${PASS_ID}`,
    );
    await screen.findByText("Plan pass destination");
  });
});

// ------------------------------------------------------------
// Regression: absent view renders nothing (Req 6)
// ------------------------------------------------------------

describe("PlanPassJumpLink — absent view", () => {
  it("renders nothing when view.present is false", async () => {
    await renderJumpLinkRouter(absentView);

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
