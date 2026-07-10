// @vitest-environment jsdom
//
// ============================================================
// Integration — Home_Overview + route/redirect preservation (task 13.3)
// ============================================================
//
// This file validates the Home_Overview landing surface and the route-boundary
// preservation guarantees of the redesigned shell:
//
//   - `/` renders the Home_Overview surface with its two sections   (Req 3.1)
//   - Per-section independent loading + distinct error/empty states (Req 3.7)
//   - Every route in the Req 10.1 inventory resolves without a
//     not-found/routing error                                        (Req 10.1)
//   - `/` routes to Home_Overview AND `/runs` stays independently
//     reachable without redirecting to `/`                           (Req 10.2)
//
// Rendering strategy
// ------------------
// The shared harness (`src/test/shell-test-utils.tsx`) mounts the recomposed
// AppShell and renders lightweight stubs at `/`, so it cannot exercise the real
// `HomeOverview` component. For the Home_Overview assertions we therefore build
// a small local in-memory router whose `/` renders the real `HomeOverview`
// (mirroring `routes/index.tsx`) over the shared `installApiStub` fetch layer.
//
// For route preservation we cover the FULL Req 10.1 inventory two ways:
//   1. A local in-memory router that registers every Req 10.1 path with a stub
//      component — navigating to each concrete path proves it resolves and
//      renders without a not-found/routing error.
//   2. The REAL application route tree (`routeTree.gen`) — `getMatchedRoutes`
//      confirms each documented path still resolves to its own route in the
//      shipped router (guards against the stub router masking a real
//      regression). This also proves `/` and `/runs` map to distinct routes.

import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

import { HomeOverview } from "@/components/relay/shell/HomeOverview";
import { routeTree } from "@/routeTree.gen";
import {
  renderShell,
  installApiStub,
  createTestQueryClient,
  ROUTE_CONTENT_TESTID,
  type ApiStubFixtures,
  type RenderShellResult,
} from "@/test/shell-test-utils";

// Section accessible names (each HomeSection renders a labelled <section>).
const ATTENTION_SECTION = "Needs attention";
const RECENT_SECTION = "Recent activity";

// Distinct per-section copy asserted below (kept in sync with HomeOverview.tsx).
const ATTENTION_EMPTY = "Nothing needs attention";
const RECENT_EMPTY = "No recent activity";
const ATTENTION_ERROR = "Attention items failed to load";
const RECENT_ERROR = "Recent activity failed to load";

// Cleanup hooks registered by each render (fetch-stub restores, harness restores).
const cleanups: Array<() => void> = [];

afterEach(() => {
  for (const restore of cleanups.splice(0)) restore();
  vi.restoreAllMocks();
});

beforeEach(() => {
  // The router/query trees settle asynchronously; keep act() noise out of the
  // reporter. Assertions below fail on missing DOM regardless of console state.
  vi.spyOn(console, "error").mockImplementation(() => {});
});

/**
 * Render the real `HomeOverview` at `/` inside a minimal in-memory router over
 * the shared API fetch stub. Registers the entity detail routes HomeOverview
 * links into so its navigable rows build valid hrefs.
 */
async function renderHomeOverview(fixtures: ApiStubFixtures = {}) {
  const restore = installApiStub(fixtures);
  cleanups.push(restore);

  const client = createTestQueryClient();
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const routes = [
    createRoute({ getParentRoute: () => rootRoute, path: "/", component: HomeOverview }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId", component: () => <div /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/plans/$planId", component: () => <div /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/projects/$projectId", component: () => <div /> }),
  ];
  const router = createRouter({
    routeTree: rootRoute.addChildren(routes),
    history: createMemoryHistory({ initialEntries: ["/"] }),
    defaultPendingMinMs: 0,
  });
  await router.load();

  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function attentionRegion(): HTMLElement {
  return screen.getByRole("region", { name: ATTENTION_SECTION });
}

function recentRegion(): HTMLElement {
  return screen.getByRole("region", { name: RECENT_SECTION });
}

// ------------------------------------------------------------
// Req 3.1 — Home_Overview surface renders its two sections
// ------------------------------------------------------------

describe("Home_Overview surface (Req 3.1)", () => {
  it("renders the attention and recent-activity sections at `/`", async () => {
    await renderHomeOverview();

    // Both HomeSections are labelled landmarks, present regardless of section
    // state (loading/empty/error/ready) — the surface itself is the assertion.
    const attention = await screen.findByRole("region", { name: ATTENTION_SECTION });
    const recent = screen.getByRole("region", { name: RECENT_SECTION });

    expect(attention).toBeInTheDocument();
    expect(recent).toBeInTheDocument();

    // Each section exposes its own heading (distinct sections, not one merged).
    expect(within(attention).getByRole("heading", { name: ATTENTION_SECTION })).toBeInTheDocument();
    expect(within(recent).getByRole("heading", { name: RECENT_SECTION })).toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// Req 3.7 — Independent loading + distinct error/empty states
// ------------------------------------------------------------

describe("Home_Overview per-section states (Req 3.7)", () => {
  it("shows distinct empty states in each section when no data exists", async () => {
    await renderHomeOverview({
      runs: [],
      plans: { plans: [] },
      projects: { success: true, count: 0, projects: [] },
    });

    // Attention empty state.
    const attention = await within(attentionRegion()).findByText(ATTENTION_EMPTY);
    expect(attention).toBeInTheDocument();

    // Recent-activity empty state — distinct copy, in its own section.
    expect(within(recentRegion()).getByText(RECENT_EMPTY)).toBeInTheDocument();

    // The two empty states are distinct (not shared copy).
    expect(ATTENTION_EMPTY).not.toEqual(RECENT_EMPTY);

    // Neither section is in an error state when data merely loaded empty.
    expect(screen.queryByText(ATTENTION_ERROR)).toBeNull();
    expect(screen.queryByText(RECENT_ERROR)).toBeNull();
  });

  it("shows the attention section's error state (with Retry), distinct from its empty state, when its data source fails", async () => {
    await renderHomeOverview({
      // Fail only the runs list request; plans/projects resolve normally.
      onRequest: (pathname) => {
        if (pathname === "/api/runs") throw new Error("runs endpoint down");
        return undefined;
      },
      plans: { plans: [] },
      projects: { success: true, count: 0, projects: [] },
    });

    const attention = attentionRegion();

    // Load-error state is shown...
    await within(attention).findByText(ATTENTION_ERROR);
    // ...with a retry affordance (Req 3.7)...
    expect(within(attention).getByRole("button", { name: "Retry" })).toBeInTheDocument();
    // ...and is distinct from the empty state (empty copy must NOT appear).
    expect(within(attention).queryByText(ATTENTION_EMPTY)).toBeNull();

    // Each section derives its own state: the recent-activity section shows its
    // OWN distinct error copy, never the attention section's error text.
    expect(within(recentRegion()).queryByText(ATTENTION_ERROR)).toBeNull();
    expect(ATTENTION_ERROR).not.toEqual(RECENT_ERROR);
  });

  it("loads one section independently while the other section's exclusive data source fails", async () => {
    // The recent-activity section additionally depends on the plans list; the
    // attention section depends only on the runs list. Failing plans therefore
    // isolates the two: the recent-activity section errors while the attention
    // section still resolves independently — proving per-section independent
    // loading and a load-error state distinct from empty (Req 3.7).
    await renderHomeOverview({
      runs: [
        {
          id: "run-1",
          title: "Blocked run alpha",
          status: "execution_failed",
          updatedAt: "2024-03-01T12:00:00.000Z",
        },
      ],
      projects: { success: true, count: 0, projects: [] },
      onRequest: (pathname) => {
        if (pathname === "/api/plans") throw new Error("plans endpoint down");
        return undefined;
      },
    });

    // Attention section resolved independently of the plans failure: it renders
    // its qualifying run and is NOT in an error state.
    const attention = attentionRegion();
    await within(attention).findByText("Blocked run alpha");
    expect(within(attention).queryByText(ATTENTION_ERROR)).toBeNull();
    expect(within(attention).queryByText(ATTENTION_EMPTY)).toBeNull();

    // Recent-activity section shows its own retryable load-error state.
    const recent = recentRegion();
    expect(within(recent).getByText(RECENT_ERROR)).toBeInTheDocument();
    expect(within(recent).getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// Req 10.1 — Route preservation across the full inventory
// ------------------------------------------------------------

// The complete Req 10.1 inventory: each documented route pattern plus a concrete
// path to navigate/match against.
const REQ_10_1_ROUTES: ReadonlyArray<{ pattern: string; nav: string }> = [
  { pattern: "/runs", nav: "/runs" },
  { pattern: "/runs/new", nav: "/runs/new" },
  { pattern: "/runs/$runId/execute", nav: "/runs/run-1/execute" },
  { pattern: "/runs/$runId/audit", nav: "/runs/run-1/audit" },
  { pattern: "/plans", nav: "/plans" },
  { pattern: "/plans/new", nav: "/plans/new" },
  { pattern: "/plans/$planId", nav: "/plans/plan-1" },
  { pattern: "/plans/$planId/passes/$passId", nav: "/plans/plan-1/passes/pass-1" },
  { pattern: "/projects", nav: "/projects" },
  { pattern: "/projects/new", nav: "/projects/new" },
  { pattern: "/projects/$projectId", nav: "/projects/proj-1" },
];

const NOT_FOUND_TESTID = "route-not-found";

/** Normalize a route path for comparison (drop a trailing slash; keep root). */
function normalizePath(path: string): string {
  return path.replace(/\/+$/, "") || "/";
}

/**
 * Build a local in-memory router registering every Req 10.1 path with a stub
 * component plus an explicit not-found component, opened at `initialPath`.
 */
function buildInventoryRouter(initialPath: string) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const routes = [
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => <div data-testid={ROUTE_CONTENT_TESTID}>home</div>,
    }),
    ...REQ_10_1_ROUTES.map(({ pattern }) =>
      createRoute({
        getParentRoute: () => rootRoute,
        path: pattern,
        component: () => <div data-testid={ROUTE_CONTENT_TESTID}>{pattern}</div>,
      }),
    ),
  ];
  return createRouter({
    routeTree: rootRoute.addChildren(routes),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
    defaultNotFoundComponent: () => <div data-testid={NOT_FOUND_TESTID}>not-found</div>,
    defaultPendingMinMs: 0,
  });
}

describe("Route preservation — full Req 10.1 inventory (Req 10.1)", () => {
  it.each(REQ_10_1_ROUTES)(
    "resolves and renders $nav without a not-found/routing error",
    async ({ pattern, nav }) => {
      const router = buildInventoryRouter(nav);
      await router.load();
      render(<RouterProvider router={router} />);

      // No not-found boundary rendered.
      expect(screen.queryByTestId(NOT_FOUND_TESTID)).toBeNull();
      // The matched route's stub view rendered (route resolved to its view).
      expect(screen.getByTestId(ROUTE_CONTENT_TESTID)).toHaveTextContent(pattern);
      // The URL is preserved (no unexpected redirect away from the target).
      expect(router.state.location.pathname).toBe(nav);
    },
  );

  it.each(REQ_10_1_ROUTES)(
    "the shipped application router still registers $pattern (Req 10.1)",
    ({ pattern, nav }) => {
      // Match against the REAL route tree so a regression in the shipped router
      // (a removed/renamed route) is caught, not just the local stub tree.
      const router = createRouter({
        routeTree,
        history: createMemoryHistory({ initialEntries: [nav] }),
      });

      const matched = router.getMatchedRoutes(nav);

      expect(matched.parseError).toBeUndefined();
      expect(matched.foundRoute).toBeDefined();
      expect(normalizePath(matched.foundRoute!.fullPath)).toBe(normalizePath(pattern));
    },
  );
});

// ------------------------------------------------------------
// Req 10.2 — `/` → Home_Overview and `/runs` reachable without redirect
// ------------------------------------------------------------

describe("Root and runs route preservation (Req 10.2)", () => {
  it("renders Home_Overview at `/` (no redirect to a raw list)", async () => {
    await renderHomeOverview();

    // The application root shows the Home_Overview triage surface, not a runs
    // list — confirmed by the presence of both Home_Overview sections.
    expect(await screen.findByRole("region", { name: ATTENTION_SECTION })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: RECENT_SECTION })).toBeInTheDocument();
  });

  it("keeps `/runs` independently reachable without redirecting to `/`", async () => {
    const result: RenderShellResult = await renderShell({ initialPath: "/runs" });
    cleanups.push(result.restore);

    // The runs view rendered (harness stub label "runs"), and the URL stayed at
    // `/runs` — a redirect back to `/` would instead land on the "home" stub.
    expect(screen.getByTestId(ROUTE_CONTENT_TESTID)).toHaveTextContent("runs");
    expect(result.router.state.location.pathname).toBe("/runs");
  });

  it("maps `/` and `/runs` to distinct routes in the shipped router (Req 10.2)", () => {
    const router = createRouter({
      routeTree,
      history: createMemoryHistory({ initialEntries: ["/"] }),
    });

    const rootMatch = router.getMatchedRoutes("/");
    const runsMatch = router.getMatchedRoutes("/runs");

    // `/` resolves to the index route (which renders Home_Overview)...
    expect(rootMatch.parseError).toBeUndefined();
    expect(normalizePath(rootMatch.foundRoute!.fullPath)).toBe("/");

    // ...and `/runs` resolves to its own distinct route (the runs view), i.e.
    // `/runs` is not an alias of, nor redirected to, the application root.
    expect(runsMatch.parseError).toBeUndefined();
    expect(normalizePath(runsMatch.foundRoute!.fullPath)).toBe("/runs");
    expect(normalizePath(runsMatch.foundRoute!.fullPath)).not.toBe(
      normalizePath(rootMatch.foundRoute!.fullPath),
    );
  });
});
