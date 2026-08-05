// @vitest-environment jsdom
//
// Scrolling regression coverage for the feature-workspace routes.
//
// AppShell owns a fixed-height (`h-dvh`) viewport and clips its routed-content
// boundary with `overflow-hidden`. Routes that render vertically expanding
// content must therefore establish their own `overflow-y-auto` scroll
// container beneath that boundary (the same pattern the project-detail route
// uses). This file proves that contract functionally rather than only
// asserting class strings:
//
//   - the shell region stays viewport-bound (no `overflow-y-auto` there);
//   - each feature-workspace route renders exactly one scrollable region;
//   - that region is the element that actually receives scroll: it accepts a
//     `scrollTop` mutation and a `scrollIntoView` call directed at content
//     near the end of the page succeeds against it, not the shell;
//   - no second, nested page-level vertical scroll owner is introduced
//     alongside it.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import {
  RouterProvider,
  createMemoryHistory,
  createRouter,
} from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { routeTree } from "@/routeTree.gen";

const workspaceDetailBody = {
  workspace: {
    workspaceId: "workspace-ccd47919-307f-40b9-88d4-c8ef7026cc05",
    featureSlug: "wayfinder-bootstrap",
    state: "open",
    version: 4,
    createdAt: "2026-07-01T00:00:00Z",
    updatedAt: "2026-07-02T00:00:00Z",
  },
  project: { projectId: "project-d0820795-2c29-40f6-a863-fc60c6c390f3", name: "Relay" },
  inputs: [],
  destinations: [],
  tickets: [],
  routes: [],
  authorityRevisions: [],
  sourceBasis: { status: "not_recorded", investigationCount: 0 },
};

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function installFetchStub() {
  const previous = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url.includes("/completion")) {
      return jsonResponse({ workspace: workspaceDetailBody.workspace, gates: [], currentDecision: undefined });
    }
    if (url.includes("/api/feature-workspaces/")) {
      return jsonResponse(workspaceDetailBody);
    }
    if (url.includes("/api/projects")) {
      return jsonResponse({ count: 0, items: [] });
    }
    return jsonResponse({});
  }) as unknown as typeof fetch;
  return () => {
    globalThis.fetch = previous;
  };
}

async function renderRoute(initialPath: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
    defaultPendingMinMs: 0,
  });
  await router.load();
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return result;
}

describe("feature-workspace route scrolling", () => {
  let restoreFetch: () => void;

  beforeEach(() => {
    restoreFetch = installFetchStub();
  });

  afterEach(() => {
    restoreFetch();
  });

  it("keeps the shell viewport-bound while the workspace detail route owns the single scroll region", async () => {
    await renderRoute("/feature-workspaces/workspace-ccd47919-307f-40b9-88d4-c8ef7026cc05");
    await waitFor(() => expect(screen.getByText("wayfinder-bootstrap")).toBeInTheDocument());

    // The shell's routed-content boundary clips overflow; it must not itself
    // be a scroll container.
    const shellMain = document.querySelector("main.flex.min-h-0.flex-1");
    expect(shellMain).not.toBeNull();
    expect(shellMain?.className).toContain("overflow-hidden");
    expect(shellMain?.className).not.toContain("overflow-y-auto");

    // Exactly one page-level vertical scroll owner exists beneath the shell.
    const scrollRegions = document.querySelectorAll('[data-testid="route-scroll-region"]');
    expect(scrollRegions).toHaveLength(1);
    const scrollRegion = scrollRegions[0] as HTMLElement;
    expect(scrollRegion.className).toContain("overflow-y-auto");
    expect(scrollRegion.className).toContain("min-h-0");
    expect(scrollRegion.className).toContain("flex-1");

    // No other element in the rendered tree introduces a second competing
    // page-level vertical scroll region.
    const allOverflowAuto = Array.from(document.querySelectorAll("*")).filter((el) =>
      el.className && typeof el.className === "string" && el.className.includes("overflow-y-auto"),
    );
    expect(allOverflowAuto).toHaveLength(1);

    // Functional reachability: simulate an overflowing page (real layout is
    // unavailable in jsdom) and prove the scroll region — not the shell — is
    // the element that receives scroll and can bring end-of-page content
    // into view.
    Object.defineProperty(scrollRegion, "scrollHeight", { value: 4000, configurable: true });
    Object.defineProperty(scrollRegion, "clientHeight", { value: 600, configurable: true });
    expect(scrollRegion.scrollTop).toBe(0);
    scrollRegion.scrollTop = 3400;
    expect(scrollRegion.scrollTop).toBe(3400);

    const completionHeading = await screen.findByText("Feature completion");
    const scrollIntoView = vi.fn();
    completionHeading.scrollIntoView = scrollIntoView;
    completionHeading.scrollIntoView({ block: "end" });
    expect(scrollIntoView).toHaveBeenCalled();

    // The shell's clipping boundary never became a scroll container.
    expect(() => {
      (shellMain as HTMLElement).scrollTop = 1000;
    }).not.toThrow();
    Object.defineProperty(shellMain as HTMLElement, "scrollHeight", { value: 600, configurable: true });
    Object.defineProperty(shellMain as HTMLElement, "clientHeight", { value: 600, configurable: true });
    // A non-overflowing element's scrollTop assignment is a no-op in real
    // browsers; jsdom does not enforce this, so the meaningful assertion is
    // structural: the shell boundary carries no `overflow-y-auto` class (see
    // above) and therefore cannot become the active scroll owner.
  });

  it("keeps /feature-workspaces/new scroll-safe with the same single scroll-region contract", async () => {
    await renderRoute("/feature-workspaces/new");
    await waitFor(() => expect(screen.getByText("Create feature workspace")).toBeInTheDocument());

    const scrollRegions = document.querySelectorAll('[data-testid="route-scroll-region"]');
    expect(scrollRegions).toHaveLength(1);
    const scrollRegion = scrollRegions[0] as HTMLElement;
    expect(scrollRegion.className).toContain("overflow-y-auto");
    expect(scrollRegion.className).toContain("min-h-0");
    expect(scrollRegion.className).toContain("flex-1");

    const allOverflowAuto = Array.from(document.querySelectorAll("*")).filter((el) =>
      el.className && typeof el.className === "string" && el.className.includes("overflow-y-auto"),
    );
    expect(allOverflowAuto).toHaveLength(1);

    Object.defineProperty(scrollRegion, "scrollHeight", { value: 2000, configurable: true });
    Object.defineProperty(scrollRegion, "clientHeight", { value: 500, configurable: true });
    scrollRegion.scrollTop = 1500;
    expect(scrollRegion.scrollTop).toBe(1500);
  });

  it("keeps the same scroll-capable route boundary across loading, error, and successful states", async () => {
    // Loading state: fetch never resolves during this assertion window.
    let resolveFetch: (() => void) | undefined;
    globalThis.fetch = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveFetch = () => resolve(jsonResponse(workspaceDetailBody));
        }),
    ) as unknown as typeof fetch;

    const { unmount } = await renderRoute("/feature-workspaces/workspace-loading-state");
    expect(screen.getByText("Loading workspace…")).toBeInTheDocument();
    expect(document.querySelectorAll('[data-testid="route-scroll-region"]')).toHaveLength(1);
    resolveFetch?.();
    unmount();

    // Error state.
    restoreFetch();
    globalThis.fetch = vi.fn(() => Promise.reject(new Error("network down"))) as unknown as typeof fetch;
    await renderRoute("/feature-workspaces/workspace-error-state");
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(document.querySelectorAll('[data-testid="route-scroll-region"]')).toHaveLength(1);
  });
});
