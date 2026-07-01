// @vitest-environment jsdom
//
// ============================================================
// Integration — Responsive behavior across the 1024px breakpoint (task 13.4)
// ============================================================
//
// Exercises the shell's responsive contract around the desktop breakpoint of
// 1024 CSS pixels (Requirement 8):
//
//   - >= 1024px: Activity_Rail + Top_Bar + content region render together   (Req 8.1)
//               and the Run_Workbench uses a side-by-side resizable split    (Req 8.1)
//   - <  1024px: the Activity_Rail collapses to a single trigger that hides
//               the primary-domain destinations until activated, and
//               activating it reveals Projects / Plans / Runs                (Req 8.2, 8.3)
//               and the Run_Workbench stacks the Inspector below the main
//               content instead of a side-by-side split                      (Req 8.5)
//   - The Scope_Switcher stays a keyboard-operable control that shows its
//     label regardless of viewport                                          (Req 8.4)
//
// Two behaviors are driven differently and are tested accordingly:
//   * The Activity_Rail desktop/mobile split is CSS-driven (Tailwind `lg:`
//     utilities). jsdom applies no CSS, so BOTH the desktop rail and the mobile
//     Sheet trigger exist in the DOM regardless of viewport. The rail-collapse
//     contract is therefore asserted structurally: the mobile trigger exists
//     and activating it reveals the domains inside the opened Sheet.
//   * The Run_Workbench split-vs-stack is JS-driven via `useIsDesktop()` (a
//     `useSyncExternalStore` read of `window.matchMedia("(min-width: 1024px)")`).
//     We control `matchMedia` per-test and assert the conditional render.

import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import {
  within,
  render,
  waitFor,
  type RenderResult,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

import {
  renderShell,
  type RenderShellResult,
} from "@/test/shell-test-utils";
import { RunWorkbenchLayout } from "@/components/relay/RunWorkbenchLayout";
import type { RelayRun } from "@/features/relay-runs";

// ------------------------------------------------------------
// matchMedia control
// ------------------------------------------------------------

const DESKTOP_QUERY = "(min-width: 1024px)";

// `react-resizable-panels` (used by the side-by-side workbench layout)
// constructs a `ResizeObserver`, which jsdom does not implement. Provide a
// no-op shim so the desktop split-pane branch can mount under test.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

/**
 * Override `window.matchMedia` so `(min-width: 1024px)` reports the desired
 * match state. `useIsDesktop` reads this synchronously during render, so it
 * MUST be installed before rendering. Every other query reports no match.
 */
function setViewport(isDesktop: boolean): void {
  window.matchMedia = vi.fn(
    (query: string): MediaQueryList =>
      ({
        matches: query === DESKTOP_QUERY ? isDesktop : false,
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList,
  ) as unknown as typeof window.matchMedia;
}

// A projects fixture so the Scope_Switcher has options and renders enabled
// (the control is disabled only when no scope options exist).
const PROJECTS_FIXTURE = {
  success: true,
  count: 1,
  projects: [
    {
      projectId: "proj-1",
      name: "Project One",
      status: "active",
      updatedAt: "2024-01-01T00:00:00.000Z",
      createdAt: "2024-01-01T00:00:00.000Z",
    },
  ],
};

// ------------------------------------------------------------
// Run_Workbench render harness (renderShell mounts route stubs, not the
// workbench, so the workbench is rendered directly inside its own router).
// ------------------------------------------------------------

const WORKBENCH_RUN: RelayRun = {
  id: "run-1",
  title: "Responsive Test Run",
  name: "Responsive Test Run",
  status: "draft",
  activeStep: "intake",
  repo: "acme/relay",
  branch: "main",
  executor: "kiro",
  packetId: "",
  updatedAt: "2024-01-01T00:00:00.000Z",
} as unknown as RelayRun;

const INSPECTOR_BODY_TEXT = "inspector-panel-body";
const MAIN_BODY_TEXT = "workbench-main-body";

async function renderWorkbench(): Promise<RenderResult> {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const intakeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/intake",
    component: () => (
      <RunWorkbenchLayout
        run={WORKBENCH_RUN}
        currentStep="intake"
        mainContent={<div>{MAIN_BODY_TEXT}</div>}
        inspectorPanels={{ logs: <div>{INSPECTOR_BODY_TEXT}</div> }}
        inspectorTabs={[{ key: "logs", label: "Logs" }]}
      />
    ),
  });

  // Additional routes referenced by the workbench (`/runs` back link + the
  // stage routes the RunStepper can navigate to) so the router resolves them.
  const stageStub = (label: string) => () => <div>{label}</div>;
  const routes = [
    intakeRoute,
    createRoute({ getParentRoute: () => rootRoute, path: "/runs", component: stageStub("runs") }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId/prepare", component: stageStub("prepare") }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId/execute", component: stageStub("execute") }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId/audit", component: stageStub("audit") }),
  ];

  const router = createRouter({
    routeTree: rootRoute.addChildren(routes),
    history: createMemoryHistory({ initialEntries: ["/runs/run-1/intake"] }),
  });

  await router.load();

  return render(<RouterProvider router={router} />);
}

// ------------------------------------------------------------
// Test lifecycle
// ------------------------------------------------------------

let active: RenderShellResult | null = null;
const originalMatchMedia = window.matchMedia;

beforeEach(() => {
  // Silence act()/query-settling noise from async shell data.
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  active?.restore();
  active = null;
  window.matchMedia = originalMatchMedia;
  vi.restoreAllMocks();
});

function getTopBar(result: RenderShellResult): HTMLElement {
  const header = result.container.querySelector("header");
  if (!header) throw new Error("Top_Bar <header> not found in shell");
  return header;
}

// ============================================================
// >= 1024px (desktop): rail + top bar + content + side-by-side workbench
// ============================================================

describe("At or above the 1024px breakpoint (Req 8.1)", () => {
  it("renders the Activity_Rail, Top_Bar, and content region simultaneously", async () => {
    setViewport(true);
    active = await renderShell({ initialPath: "/runs" });
    const screen = active;

    // Activity_Rail (primary navigation landmark).
    expect(screen.getByRole("navigation", { name: "Primary" })).toBeInTheDocument();
    // Top_Bar (banner region hosting global context).
    expect(getTopBar(screen)).toBeInTheDocument();
    // Content region (the routed page renders through the Outlet).
    expect(screen.getByTestId("route-content")).toBeInTheDocument();
  });

  it("renders the Run_Workbench as a side-by-side resizable split pane", async () => {
    setViewport(true);
    const screen = await renderWorkbench();

    // The side-by-side layout renders the resizable panel group + handle.
    expect(
      screen.container.querySelector('[data-slot="resizable-panel-group"]'),
    ).not.toBeNull();
    expect(
      screen.container.querySelector('[data-slot="resizable-handle"]'),
    ).not.toBeNull();

    // Both the main content and the Inspector panel are present.
    expect(screen.getByText(MAIN_BODY_TEXT)).toBeInTheDocument();
    expect(screen.getByText(INSPECTOR_BODY_TEXT)).toBeInTheDocument();
  });
});

// ============================================================
// < 1024px (mobile): collapsed rail trigger + stacked workbench
// ============================================================

describe("Below the 1024px breakpoint (Req 8.2, 8.3, 8.5)", () => {
  it("collapses the Activity_Rail to a trigger that reveals the domains on activation", async () => {
    setViewport(false);
    active = await renderShell({ initialPath: "/runs" });
    const screen = active;
    const user = userEvent.setup();

    // The collapsed rail control (single trigger) is present (Req 8.2).
    const trigger = screen.getByRole("button", { name: "Open navigation" });
    expect(trigger).toBeInTheDocument();

    // Its Sheet is closed initially: the primary-domain destinations are not
    // yet revealed through a dialog surface.
    expect(screen.queryByRole("dialog")).toBeNull();

    // Activating the control reveals Projects / Plans / Runs (Req 8.3).
    await user.click(trigger);

    const sheet = await screen.findByRole("dialog");
    for (const name of ["Projects", "Plans", "Runs"] as const) {
      const link = within(sheet).getByRole("link", { name });
      expect(link).toBeInTheDocument();
      expect(link).toHaveAttribute("href");
    }
  });

  it("stacks the Inspector below the main content instead of a side-by-side split", async () => {
    setViewport(false);
    const screen = await renderWorkbench();

    // No resizable split pane is rendered below the breakpoint (Req 8.5).
    expect(
      screen.container.querySelector('[data-slot="resizable-panel-group"]'),
    ).toBeNull();
    expect(
      screen.container.querySelector('[data-slot="resizable-handle"]'),
    ).toBeNull();

    // Main content and the Inspector (an <aside>) are both present, and the
    // Inspector is stacked AFTER (below) the main content in document order.
    const main = screen.container.querySelector("main");
    const inspector = screen.container.querySelector("aside");
    expect(main).not.toBeNull();
    expect(inspector).not.toBeNull();
    expect(screen.getByText(MAIN_BODY_TEXT)).toBeInTheDocument();
    expect(screen.getByText(INSPECTOR_BODY_TEXT)).toBeInTheDocument();

    // DOCUMENT_POSITION_FOLLOWING => inspector appears after main.
    expect(
      main!.compareDocumentPosition(inspector!) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });
});

// ============================================================
// Scope_Switcher keyboard-operable with its label at every viewport (Req 8.4)
// ============================================================

describe("Scope_Switcher stays keyboard-operable with its label (Req 8.4)", () => {
  for (const isDesktop of [true, false] as const) {
    const widthLabel = isDesktop ? ">= 1024px" : "< 1024px";

    it(`exposes an enabled, focusable scope control showing its label at ${widthLabel}`, async () => {
      setViewport(isDesktop);
      active = await renderShell({
        initialPath: "/projects/proj-1",
        fixtures: { projects: PROJECTS_FIXTURE },
      });
      const screen = active;

      // The Scope_Switcher control (accessible name "Active scope") is present
      // in the Top_Bar and remains keyboard-operable (enabled, focusable). It
      // becomes enabled once the composed scope options resolve.
      const topBar = within(getTopBar(screen));
      await waitFor(() => {
        expect(topBar.getByLabelText("Active scope")).toBeEnabled();
      });
      const scope = topBar.getByLabelText("Active scope");
      expect(scope).toBeInTheDocument();

      scope.focus();
      expect(scope).toHaveFocus();

      // It displays the active scope label (never hidden at narrow widths).
      expect(scope).toHaveTextContent("Project One");
    });
  }
});
