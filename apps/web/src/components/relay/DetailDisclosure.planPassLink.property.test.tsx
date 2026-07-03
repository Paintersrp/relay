// @vitest-environment jsdom
//
// ============================================================
// Feature: run-status-tracker-redesign, Property 6: Plan/Pass never inlined
// ============================================================
//
// Validates: Requirement 5.5
//
// For any generated `PlanPassLinkView` (plan-only, plan+pass, or absent)
// and for the "no plan/pass context" case (the `planPassLinkView` prop
// itself omitted), rendering `DetailDisclosure` with `sections=[]` and
// opening the outer "Show details" affordance must produce, inside the
// disclosure's content region, either:
//   - nothing plan/pass-related at all (no link, no leaked text), or
//   - exactly the single navigation-link-shaped output `PlanPassJumpLink`
//     itself would produce for that view (one `role="link"` element whose
//     accessible name is `view.accessibleName` and whose entire visible
//     text is `view.displayLabel`) — and nothing more.
//
// This proves `DetailDisclosure` never inlines full Plan/Pass content: it
// either renders the existing `PlanPassJumpLink` unchanged, or renders
// nothing.

import { describe, expect, it } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import fc from "fast-check";

import type { DetailSection } from "@/features/relay-runs/runStatusTrackerViews";
import type { PlanPassLinkView } from "@/features/relay-runs/runWorkbenchViews";
import { DetailDisclosure } from "./DetailDisclosure";

const RUN_ID = "run-42";
const NO_SECTIONS: DetailSection[] = [];

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------
//
// Route params must be safe URL segments, so ids are restricted to a plain
// alphanumeric charset.
const idCharArb = fc.constantFrom(
  ..."abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789".split(
    "",
  ),
);
const idArb = fc
  .array(idCharArb, { minLength: 1, maxLength: 10 })
  .map((chars) => chars.join(""));

// Display/accessible-name text is normalized to collapsed, trimmed
// whitespace so it matches exactly what the DOM accessible-name
// computation would produce (which also collapses/trims whitespace),
// keeping exact-string assertions reliable regardless of generated content
// (including text that looks like arbitrary "plan body"/"pass body" prose).
const safeTextArb = fc
  .string({ minLength: 1, maxLength: 40 })
  .map((s) => s.replace(/\s+/g, " ").trim())
  .filter((s) => s.length > 0);

const planOnlyViewArb: fc.Arbitrary<PlanPassLinkView> = fc.record({
  present: fc.constant(true as const),
  to: fc.constant("/plans/$planId" as const),
  params: idArb.map((planId) => ({ planId })),
  displayLabel: safeTextArb,
  accessibleName: safeTextArb,
});

const planPassViewArb: fc.Arbitrary<PlanPassLinkView> = fc.record({
  present: fc.constant(true as const),
  to: fc.constant("/plans/$planId/passes/$passId" as const),
  params: fc.tuple(idArb, idArb).map(([planId, passId]) => ({
    planId,
    passId,
  })),
  displayLabel: safeTextArb,
  accessibleName: safeTextArb,
});

const absentViewArb: fc.Arbitrary<PlanPassLinkView> = fc.constant({
  present: false,
  displayLabel: "",
  accessibleName: "",
});

// Full input space: a present-with-context view (plan-only or plan+pass),
// an explicit absent view, or the "no plan/pass context" case where the
// `planPassLinkView` prop itself is undefined.
const planPassLinkPropArb: fc.Arbitrary<PlanPassLinkView | undefined> =
  fc.oneof(planOnlyViewArb, planPassViewArb, absentViewArb, fc.constant(undefined));

// ------------------------------------------------------------
// Render helper
// ------------------------------------------------------------

async function renderDetailDisclosure(planPassLinkView: PlanPassLinkView | undefined) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const startRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId",
    component: () => (
      <DetailDisclosure
        sections={NO_SECTIONS}
        planPassLinkView={planPassLinkView}
        currentStep="execute"
      />
    ),
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

  return render(<RouterProvider router={router} />);
}

function isPresentWithContext(
  view: PlanPassLinkView | undefined,
): view is PlanPassLinkView & {
  to: NonNullable<PlanPassLinkView["to"]>;
  params: NonNullable<PlanPassLinkView["params"]>;
} {
  return Boolean(view?.present && view.to && view.params);
}

describe("DetailDisclosure — Property 6: Plan/Pass never inlined", () => {
  it(
    "renders only PlanPassJumpLink's own link output (or nothing), never inlined plan/pass content (Req 5.5)",
    async () => {
      await fc.assert(
        fc.asyncProperty(planPassLinkPropArb, async (planPassLinkView) => {
          const { unmount } = await renderDetailDisclosure(planPassLinkView);

          try {
            // Open the outer "Show details" disclosure.
            const trigger = screen.getByRole("button", { name: /show details/i });
            fireEvent.click(trigger);

            const content = screen.getByTestId("detail-disclosure-content");

            if (!isPresentWithContext(planPassLinkView)) {
              // No plan/pass context (undefined prop, or an explicit
              // present:false view): nothing plan/pass-related renders —
              // no link, no leaked text of any kind.
              expect(within(content).queryByRole("link")).not.toBeInTheDocument();
              expect(content.textContent ?? "").toBe("");
              return;
            }

            // Present-with-context case: exactly one navigation-link-shaped
            // element, matching PlanPassJumpLink's own output exactly, and
            // nothing else inside the disclosure's content region (no
            // inlined plan/pass body content, since `sections` is empty).
            const links = within(content).getAllByRole("link");
            expect(links).toHaveLength(1);

            const [link] = links;
            expect(link).toHaveAccessibleName(planPassLinkView.accessibleName);
            expect(link.textContent).toBe(planPassLinkView.displayLabel);

            // The link is the sole content of the disclosure region — its
            // entire text content equals exactly the link's text, proving
            // DetailDisclosure added nothing beyond PlanPassJumpLink's own
            // rendered output.
            expect(content.textContent).toBe(planPassLinkView.displayLabel);
          } finally {
            unmount();
          }
        }),
        { numRuns: 100 },
      );
    },
    30000,
  );
});
