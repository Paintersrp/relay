// ============================================================
// Shared shell integration-test harness (wave-8 integration tests)
// ============================================================
//
// This helper renders the recomposed `AppShell` inside an in-memory TanStack
// Router with a small set of representative routes, wrapped in a test-scoped
// `QueryClientProvider`. It also installs a `fetch` stub over the existing
// API_Contract endpoints so `useShellData` / `useRunHierarchy` resolve without
// a real backend.
//
// It is intentionally reusable: tasks 13.2 (overlay behavior), 13.3
// (Home_Overview + route preservation), and 13.4 (responsive behavior) build
// their own `// @vitest-environment jsdom` test files and call `renderShell`
// (and `installApiStub`) from here rather than duplicating the router/Query
// wiring.
//
// The harness does NOT introduce any production code — it only composes
// existing shell/route pieces for testing.

import { render, type RenderResult } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  type AnyRouter,
} from "@tanstack/react-router";
import { vi } from "vitest";

import { AppShell } from "@/components/relay/AppShell";

// ------------------------------------------------------------
// API fetch stub over the existing endpoints
// ------------------------------------------------------------

/** Fixtures returned by the stubbed API_Contract endpoints. */
export interface ApiStubFixtures {
  /** `GET /api/runs` — list of runs (raw API shape; normalized by the app). */
  runs?: unknown[];
  /** `GET /api/plans` — plan list response body. */
  plans?: { plans?: unknown[]; [k: string]: unknown };
  /** `GET /api/projects` — project list response body. */
  projects?: { projects?: unknown[]; success?: boolean; count?: number; [k: string]: unknown };
  /** `GET /api/runs/{id}` — single run detail, keyed by run id. */
  runDetail?: Record<string, unknown>;
  /** `GET /api/plans/{id}` — plan detail (with `passes[]`), keyed by plan id. */
  planDetail?: Record<string, unknown>;
  /**
   * Optional per-path override. Return a value to serve it as JSON, or throw to
   * simulate a network/HTTP failure. Return `undefined` to fall through to the
   * default routing below.
   */
  onRequest?: (pathname: string, url: string) => unknown | undefined;
}

function jsonResponse(body: unknown, status = 200): Response {
  const text = JSON.stringify(body ?? null);
  return new Response(text, {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/**
 * Install a `fetch` stub over `globalThis.fetch` that serves the existing
 * API_Contract GET endpoints from the supplied fixtures. Returns a restore
 * function; callers typically restore in `afterEach`.
 *
 * Defaults are empty collections, which is sufficient for shell-presence
 * assertions (the shell renders regardless of list contents).
 */
export function installApiStub(fixtures: ApiStubFixtures = {}): () => void {
  const previousFetch = globalThis.fetch;

  const stub = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === "string" ? input : input.toString();
    const pathname = (() => {
      try {
        return new URL(url).pathname;
      } catch {
        return url;
      }
    })();

    if (fixtures.onRequest) {
      const override = fixtures.onRequest(pathname, url);
      if (override !== undefined) {
        return jsonResponse(override);
      }
    }

    // GET /api/runs/{id}
    const runDetailMatch = pathname.match(/^\/api\/runs\/([^/]+)$/);
    if (runDetailMatch) {
      const id = decodeURIComponent(runDetailMatch[1]);
      const detail = fixtures.runDetail?.[id] ?? { id, status: "draft" };
      return jsonResponse(detail);
    }

    // GET /api/plans/{id}
    const planDetailMatch = pathname.match(/^\/api\/plans\/([^/]+)$/);
    if (planDetailMatch && pathname !== "/api/plans") {
      const id = decodeURIComponent(planDetailMatch[1]);
      const detail = fixtures.planDetail?.[id] ?? { planId: id, passes: [] };
      return jsonResponse(detail);
    }

    if (pathname === "/api/runs") {
      return jsonResponse(fixtures.runs ?? []);
    }
    if (pathname === "/api/plans") {
      return jsonResponse(fixtures.plans ?? { plans: [] });
    }
    if (pathname === "/api/projects") {
      return jsonResponse(
        fixtures.projects ?? { success: true, count: 0, projects: [] },
      );
    }

    // Unknown endpoint — empty JSON object keeps callers from hard-failing.
    return jsonResponse({});
  });

  globalThis.fetch = stub as unknown as typeof fetch;

  return () => {
    globalThis.fetch = previousFetch;
  };
}

// ------------------------------------------------------------
// In-memory router around AppShell
// ------------------------------------------------------------

/** Marker rendered by every stub route so tests can assert content presence. */
export const ROUTE_CONTENT_TESTID = "route-content";

function RouteStub({ label }: { label: string }) {
  return <div data-testid={ROUTE_CONTENT_TESTID}>{label}</div>;
}

/**
 * Build the representative route tree used by the shell integration tests. The
 * AppShell is mounted once at the root (mirroring `__root.tsx`) and every leaf
 * route renders a lightweight stub through the `<Outlet />`.
 *
 * The paths cover: application root, the runs domain (list + new + a run-scoped
 * stage route), the plans domain (list + detail + pass), and the projects
 * domain (list + detail).
 */
function buildRouteTree() {
  const rootRoute = createRootRoute({
    component: () => (
      <AppShell>
        <Outlet />
      </AppShell>
    ),
  });

  const routes = [
    createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => <RouteStub label="home" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs", component: () => <RouteStub label="runs" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/new", component: () => <RouteStub label="run-new" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId", component: () => <RouteStub label="run-detail" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId/intake", component: () => <RouteStub label="run-intake" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId/prepare", component: () => <RouteStub label="run-prepare" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId/execute", component: () => <RouteStub label="run-execute" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/runs/$runId/audit", component: () => <RouteStub label="run-audit" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/plans", component: () => <RouteStub label="plans" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/plans/$planId", component: () => <RouteStub label="plan-detail" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/plans/$planId/passes/$passId", component: () => <RouteStub label="pass-detail" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/projects", component: () => <RouteStub label="projects" /> }),
    createRoute({ getParentRoute: () => rootRoute, path: "/projects/$projectId", component: () => <RouteStub label="project-detail" /> }),
  ];

  return rootRoute.addChildren(routes);
}

export interface RenderShellOptions {
  /** Initial pathname to render (default `/`). */
  initialPath?: string;
  /** Pre-installed API fixtures (a stub is installed automatically). */
  fixtures?: ApiStubFixtures;
  /** Provide a pre-configured QueryClient (otherwise a test client is built). */
  queryClient?: QueryClient;
}

export interface RenderShellResult extends RenderResult {
  router: AnyRouter;
  queryClient: QueryClient;
  /** Restore the original `globalThis.fetch`. */
  restore: () => void;
}

/** A QueryClient tuned for tests: no retries, no refetch-on-focus. */
export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, gcTime: 0 },
    },
  });
}

/**
 * Render the AppShell inside an in-memory router at `initialPath`, wrapped in a
 * test QueryClientProvider, with the API layer stubbed. Waits for the router to
 * be ready before returning so route content is present.
 */
export async function renderShell(
  options: RenderShellOptions = {},
): Promise<RenderShellResult> {
  const { initialPath = "/", fixtures, queryClient } = options;

  const restore = installApiStub(fixtures ?? {});
  const client = queryClient ?? createTestQueryClient();

  const router = createRouter({
    routeTree: buildRouteTree(),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
    defaultPendingMinMs: 0,
  });

  await router.load();

  const result = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );

  return { ...result, router: router as AnyRouter, queryClient: client, restore };
}
