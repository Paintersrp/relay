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
function normalizeRunFixture(run: any) {
  if (!run || typeof run !== "object") return run;
  return {
    runId: run.runId || run.id || "run-default",
    featureSlug: run.featureSlug || run.title || run.name || "feature-default",
    repoTarget: run.repoTarget || "relay",
    status: run.status || "created",
    stage: run.stage || "specification",
    branch: run.branch || "feat/simplification",
    baseCommit: run.baseCommit || "a".repeat(40),
    canonicalSha256: run.canonicalSha256 || "b".repeat(64),
    createdAt: run.createdAt || run.updatedAt || new Date().toISOString(),
    updatedAt: run.updatedAt || new Date().toISOString(),
    planId: run.planId,
    passId: run.passId,
    passNumber: run.passNumber,
    project: run.project || (run.projectId ? { projectId: run.projectId, name: run.projectName || "Project", status: "active" } : undefined),
    remediatesRunId: run.remediatesRunId,
    latestAttempt: run.latestAttempt,
    currentPacket: run.currentPacket,
    latestDecision: run.latestDecision,
  };
}

function normalizePlanFixture(plan: any) {
  if (!plan || typeof plan !== "object") return plan;
  return {
    planId: plan.planId || plan.id || "plan-default",
    featureSlug: plan.featureSlug || plan.title || "feature-default",
    status: plan.status || "active",
    canonicalSha256: plan.canonicalSha256 || "b".repeat(64),
    createdAt: plan.createdAt || plan.updatedAt || new Date().toISOString(),
    updatedAt: plan.updatedAt || new Date().toISOString(),
    passCount: plan.passCount || 0,
    completedPassCount: plan.completedPassCount || 0,
    project: plan.project || {
      projectId: plan.projectId || "project-default",
      name: plan.projectName || "Project Default",
      status: "active",
    },
  };
}

function normalizeProjectFixture(proj: any) {
  if (!proj || typeof proj !== "object") return proj;
  return {
    projectId: proj.projectId || proj.id || "project-default",
    name: proj.name || "Project Default",
    description: proj.description || "",
    status: proj.status || "active",
    createdAt: proj.createdAt || proj.updatedAt || new Date().toISOString(),
    updatedAt: proj.updatedAt || new Date().toISOString(),
  };
}

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
      const detail = fixtures.runDetail?.[id];
      if (detail && "run" in (detail as any)) {
        return jsonResponse(detail);
      }
      const runObj = detail || { id, status: "draft" };
      return jsonResponse({
        run: normalizeRunFixture(runObj),
        attempts: (runObj as any).attempts || [],
        artifacts: (runObj as any).artifacts || [],
      });
    }

    // GET /api/plans/{id}
    const planDetailMatch = pathname.match(/^\/api\/plans\/([^/]+)$/);
    if (planDetailMatch && pathname !== "/api/plans") {
      const id = decodeURIComponent(planDetailMatch[1]);
      const detail = fixtures.planDetail?.[id];
      if (detail && "plan" in (detail as any)) {
        return jsonResponse(detail);
      }
      const planObj = detail || { planId: id, passes: [] };
      return jsonResponse({
        plan: normalizePlanFixture(planObj),
        repositories: (planObj as any).repositories || [],
        passes: ((planObj as any).passes || []).map((pass: any) => ({
          passId: pass.passId || pass.id || "pass-default",
          number: pass.number || pass.sequence || 1,
          name: pass.name || "Pass One",
          repoTarget: pass.repoTarget || "relay",
          status: pass.status || "planned",
          dependsOn: pass.dependsOn || [],
          createdAt: pass.createdAt || new Date().toISOString(),
          updatedAt: pass.updatedAt || new Date().toISOString(),
          runs: pass.runs || [],
        })),
        artifacts: (planObj as any).artifacts || [],
      });
    }

    if (pathname === "/api/runs") {
      const rawRuns = fixtures.runs ?? [];
      const items = Array.isArray(rawRuns) ? rawRuns.map(normalizeRunFixture) : [];
      return jsonResponse({
        count: items.length,
        items,
      });
    }
    if (pathname === "/api/plans") {
      const rawPlans = fixtures.plans?.plans ?? fixtures.plans?.items ?? [];
      const items = Array.isArray(rawPlans) ? rawPlans.map(normalizePlanFixture) : [];
      return jsonResponse({
        count: items.length,
        items,
      });
    }
    if (pathname === "/api/projects") {
      const rawProjects = fixtures.projects?.projects ?? fixtures.projects?.items ?? [];
      const items = Array.isArray(rawProjects) ? rawProjects.map(normalizeProjectFixture) : [];
      return jsonResponse({
        count: items.length,
        items,
      });
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
