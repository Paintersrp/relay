// @vitest-environment jsdom
//
// ============================================================
// Example tests — RunStepper navigation and labels (task 3.3)
// ============================================================
//
// Covers, via rendering + interaction rather than property tests over the
// pure `derivePipelineStages` helper (already covered by
// `pipeline.canonicalClassification.property.test.ts` and
// `pipeline.derive.property.test.ts` / `pipeline.property.test.ts`):
//
//   - Selecting a navigable step navigates to that step's sub-route (updating
//     Active_Route_Step) via `router.navigate`, without changing
//     Canonical_Run_Status — the stepper's classification (which step is
//     `aria-current="step"`) stays derived from the `status` prop alone and is
//     unaffected by the route change.                        (Req 2.4, 2.6)
//   - Rendered step labels match `RELAY_RUN_STEPS` labels exactly, in order.
//                                                                (Req 2.7)
//   - A non-canonical Canonical_Run_Status renders none-active / all-upcoming
//     (no `aria-current="step"`, every step `data-stage-status="pending"`).
//                                                                (Req 2.4)

import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

import { RELAY_RUN_STEPS, type RelayRunStatus } from "@/features/relay-runs";
import { RunStepper } from "./RunStepper";

const RUN_ID = "run-42";

// `executor_running` maps to the "execute" canonical step per pipeline.ts'
// STATUS_TO_STAGE mapping, so it exercises the "current" branch.
const CANONICAL_STATUS: RelayRunStatus = "executor_running";

/**
 * Mount `RunStepper` inside a small in-memory router registering all four
 * stage routes plus the starting route, mirroring the router-setup convention
 * used by `Responsive.integration.test.tsx` / `HomeOverview.integration.test.tsx`.
 * Every stage route renders `RunStepper` with the SAME fixed `status` prop, so
 * navigating between routes never itself alters Canonical_Run_Status — any
 * change in the derived active step after navigation would only come from a
 * (incorrect) route-driven derivation.
 */
async function renderStepperRouter(
  initialPath: string,
  status: RelayRunStatus | string = CANONICAL_STATUS,
) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const stepperAt = (path: string) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path,
      component: () => (
        <RunStepper runId={RUN_ID} status={status as RelayRunStatus} />
      ),
    });

  const routes = [
    stepperAt("/runs/$runId/intake"),
    stepperAt("/runs/$runId/prepare"),
    stepperAt("/runs/$runId/execute"),
    stepperAt("/runs/$runId/audit"),
  ];

  const router = createRouter({
    routeTree: rootRoute.addChildren(routes),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
    defaultPendingMinMs: 0,
  });
  await router.load();

  const result = render(<RouterProvider router={router} />);
  return { router, ...result };
}

function stepperNav(): HTMLElement {
  return screen.getByRole("navigation", { name: "Run steps" });
}

// ------------------------------------------------------------
// Navigation without changing Canonical_Run_Status (Req 2.4, 2.6)
// ------------------------------------------------------------

describe("RunStepper — step selection navigates without changing Canonical_Run_Status", () => {
  it("navigates to the selected step's sub-route (updating the URL/Active_Route_Step)", async () => {
    const user = userEvent.setup();
    const { router } = await renderStepperRouter(`/runs/${RUN_ID}/intake`);

    const auditButton = within(stepperNav()).getByRole("button", {
      name: RELAY_RUN_STEPS.find((s) => s.key === "audit")!.label,
    });
    await user.click(auditButton);

    expect(router.state.location.pathname).toBe(`/runs/${RUN_ID}/audit`);
  });

  it("navigates with the correct runId route param for each navigable step", async () => {
    const user = userEvent.setup();
    const { router } = await renderStepperRouter(`/runs/${RUN_ID}/intake`);

    const executeButton = within(stepperNav()).getByRole("button", {
      name: RELAY_RUN_STEPS.find((s) => s.key === "execute")!.label,
    });
    await user.click(executeButton);

    expect(router.state.location.pathname).toBe(`/runs/${RUN_ID}/execute`);
  });

  it("does not change Canonical_Run_Status: the active step stays derived from `status` after navigating", async () => {
    const user = userEvent.setup();
    const { router } = await renderStepperRouter(`/runs/${RUN_ID}/intake`, CANONICAL_STATUS);

    // Before navigating: `status` = "executor_running" maps to "execute", so
    // the execute button is the sole aria-current="step".
    const executeLabel = RELAY_RUN_STEPS.find((s) => s.key === "execute")!.label;
    const auditLabel = RELAY_RUN_STEPS.find((s) => s.key === "audit")!.label;
    expect(
      within(stepperNav()).getByRole("button", { name: executeLabel }),
    ).toHaveAttribute("aria-current", "step");

    // Navigate to the audit sub-route (Active_Route_Step changes)...
    const auditButton = within(stepperNav()).getByRole("button", { name: auditLabel });
    await user.click(auditButton);
    expect(router.state.location.pathname).toBe(`/runs/${RUN_ID}/audit`);

    // ...but Canonical_Run_Status (the `status` prop) is unchanged — the
    // audit route also renders RunStepper with the SAME fixed status, and the
    // stepper still reports "execute" as current, never "audit". A stepper
    // that (incorrectly) derived from Active_Route_Step instead of
    // Canonical_Run_Status would show audit as current here.
    expect(
      within(stepperNav()).getByRole("button", { name: executeLabel }),
    ).toHaveAttribute("aria-current", "step");
    expect(
      within(stepperNav()).getByRole("button", { name: auditLabel }),
    ).not.toHaveAttribute("aria-current", "step");
  });
});

// ------------------------------------------------------------
// Labels stay in sync with RELAY_RUN_STEPS (Req 2.7)
// ------------------------------------------------------------

describe("RunStepper — labels stay in sync with RELAY_RUN_STEPS", () => {
  it("renders exactly the four RELAY_RUN_STEPS labels, in order", async () => {
    await renderStepperRouter(`/runs/${RUN_ID}/intake`);

    const buttons = within(stepperNav()).getAllByRole("button");
    expect(buttons.map((button) => button.textContent?.trim())).toEqual(
      RELAY_RUN_STEPS.map((step) => step.label),
    );
  });
});

// ------------------------------------------------------------
// Non-canonical Canonical_Run_Status: none-active / all-upcoming (Req 2.4)
// ------------------------------------------------------------

describe("RunStepper — non-canonical status renders none-active / all-upcoming", () => {
  it("marks no step as aria-current and every step data-stage-status=pending", async () => {
    await renderStepperRouter(`/runs/${RUN_ID}/intake`, "not-a-real-status");

    const buttons = within(stepperNav()).getAllByRole("button");
    expect(buttons).toHaveLength(4);

    for (const button of buttons) {
      expect(button).not.toHaveAttribute("aria-current");
      expect(button).toHaveAttribute("data-stage-status", "pending");
    }
  });
});
