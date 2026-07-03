// @vitest-environment jsdom
//
// ============================================================
// Execute route composition tests (task 11.2)
// ============================================================
//
// Covers:
//   - Requirement 6.4: the default view renders IdentityStrip,
//     CurrentStatusBlock, NextActionArea, and a collapsed ProgressionRail
//     simultaneously, without expanding anything.
//   - Requirement 5.6: opening Detail_Disclosure reveals exactly the four
//     execute-step sections ("Full logs", "Executor result", "Changed
//     files", "Validation report") — not before opening.
//
// The execute route (`execute.tsx`) fetches run/artifacts/events via
// `useQuery` + the existing `api.ts` GET endpoints, so this harness stubs
// `globalThis.fetch` over those endpoints (mirroring the
// `installApiStub`/`shell-test-utils.tsx` convention, also used by
// `prepare.test.tsx`) rather than mocking `useQuery` directly, and mounts
// the real route component inside a small in-memory router (mirroring
// `RunStatusTrackerLayout.test.tsx`).

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import fc from "fast-check";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

// Bug condition exploration (step-navigation-missing, task 1) — spy on the
// existing EXECUTE_ACTION_HANDLERS-backed mutation functions `execute.tsx`
// invokes, so the exploration property test below can assert that
// clicking a "proceed to audit" control (once one exists) does NOT invoke
// any of them, i.e. `NOT mutatesCanonicalRunStatus(result)`.
vi.mock("@/features/relay-runs", async () => {
  const actual = await vi.importActual<typeof import("@/features/relay-runs")>(
    "@/features/relay-runs",
  );
  return {
    ...actual,
    executeRun: vi.fn().mockResolvedValue({ success: true }),
    cancelRun: vi.fn().mockResolvedValue({ success: true }),
    recoverRun: vi.fn().mockResolvedValue({ success: true }),
  };
});

import type { RelayRunEvent } from "@/features/relay-runs";
import {
  executeRun as executeRunMock,
  cancelRun as cancelRunMock,
  recoverRun as recoverRunMock,
} from "@/features/relay-runs";
import {
  formatExecutorPacket,
  deriveLiveExecutorProgress,
  isExecuteLiveStatus,
  Route as ExecuteRouteImport,
} from "./execute";

const sampleToolUsePacket = JSON.stringify({
  type: "tool_use",
  timestamp: 1782593090986,
  sessionID: "ses_abc123",
  part: {
    type: "tool",
    tool: "read",
    callID: "call_xyz789",
    state: {
      status: "completed",
      input: {
        filePath: "D:\\Code\\relay\\docs\\generated\\agent-references\\index.json",
        limit: 30,
      },
      output: "docs/generated/agent-references/index.json",
    },
  },
});

// ------------------------------------------------------------
// Fetch stub over the existing GET endpoints this route relies on
// (`GET /api/runs/:id`, `GET /api/runs/:id/artifacts`,
// `GET /api/runs/:id/events`).
// ------------------------------------------------------------

const RUN_ID = "run-execute-1";

interface RunFixture {
  run: Record<string, unknown>;
  artifacts?: Array<Record<string, unknown>>;
  events?: Array<Record<string, unknown>>;
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body ?? null), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function installFetchStub(fixture: RunFixture): () => void {
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

    if (pathname === `/api/runs/${RUN_ID}`) {
      return jsonResponse(fixture.run);
    }
    if (pathname === `/api/runs/${RUN_ID}/artifacts`) {
      return jsonResponse(fixture.artifacts ?? []);
    }
    if (pathname === `/api/runs/${RUN_ID}/events`) {
      return jsonResponse(fixture.events ?? []);
    }

    return jsonResponse({});
  });

  globalThis.fetch = stub as unknown as typeof fetch;

  return () => {
    globalThis.fetch = previousFetch;
  };
}

// ------------------------------------------------------------
// Fixture — status "approved_for_executor" makes `canStart` true (and is
// NOT a live/polling status per `isExecuteLiveStatus`), giving the route a
// primary "Start" action without any refetch-interval timers running
// during the test.
// ------------------------------------------------------------

function makeRunFixture(status: string = "approved_for_executor"): RunFixture {
  return {
    run: {
      id: RUN_ID,
      title: "Ship the execute tracker composition",
      name: "Ship the execute tracker composition",
      repo: "acme/relay-ui",
      branch: "feature/execute-tracker",
      status,
      activeStep: "execute",
      lifecycleState: "execute",
      createdAt: "2024-01-01T00:00:00.000Z",
      updatedAt: "2024-01-01T00:00:00.000Z",
      model: "claude-sonnet",
    },
    artifacts: [],
    events: [
      {
        id: "evt-1",
        runId: RUN_ID,
        kind: "log",
        message: "Executor brief approved.",
        createdAt: "2023-12-31T23:00:00.000Z",
      },
    ],
  };
}

// ------------------------------------------------------------
// Render harness — mounts the real execute route component inside a small
// in-memory router (mirroring RunStatusTrackerLayout.test.tsx /
// prepare.test.tsx), over a test-scoped QueryClientProvider and the fetch
// stub above.
// ------------------------------------------------------------

function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, gcTime: 0 },
    },
  });
}

async function renderExecuteRoute() {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const executeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/execute",
    component: ExecuteRouteImport.options.component,
  });

  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: () => <div>runs-registry-destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([executeRoute, runsRoute]),
    history: createMemoryHistory({
      initialEntries: [`/runs/${RUN_ID}/execute`],
    }),
    defaultPendingMinMs: 0,
  });
  await router.load();

  const queryClient = makeQueryClient();
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );

  // Wait for the run/artifacts/events queries to resolve past the loading
  // state into the tracker layout.
  await screen.findByTestId("run-status-tracker-layout");

  return result;
}

// ------------------------------------------------------------
// Bug condition exploration render harness (step-navigation-missing,
// task 1) — additionally registers a `/runs/$runId/audit` destination
// route so the exploration property test can assert on where the router
// actually lands after clicking a "proceed to audit" control, and exposes
// the `router` so the test can inspect `router.state.location`.
// ------------------------------------------------------------

async function renderExecuteRouteForExploration() {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const executeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/execute",
    component: ExecuteRouteImport.options.component,
  });

  const auditRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/audit",
    component: () => <div>audit-destination</div>,
  });

  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: () => <div>runs-registry-destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([executeRoute, auditRoute, runsRoute]),
    history: createMemoryHistory({
      initialEntries: [`/runs/${RUN_ID}/execute`],
    }),
    defaultPendingMinMs: 0,
  });
  await router.load();

  const queryClient = makeQueryClient();
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );

  await screen.findByTestId("run-status-tracker-layout");

  return { ...result, router };
}

// ------------------------------------------------------------
// Test lifecycle
// ------------------------------------------------------------

const cleanups: Array<() => void> = [];

beforeEach(() => {
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  for (const restore of cleanups.splice(0)) restore();
  vi.restoreAllMocks();
});

// ------------------------------------------------------------
// 1. Default composition — Req 6.4
// ------------------------------------------------------------

describe("Execute route — default composition (Req 6.4)", () => {
  it("renders identity, status, action, and a collapsed progression rail without expanding any Detail_Disclosure section", async () => {
    cleanups.push(installFetchStub(makeRunFixture()));

    await renderExecuteRoute();

    // IdentityStrip — run identity (title, repo, pipeline position group).
    expect(
      screen.getByText("Ship the execute tracker composition"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: /pipeline position/i }),
    ).toBeInTheDocument();

    // CurrentStatusBlock — exactly one current-status headline.
    const statusBlock = screen.getByTestId("current-status-block");
    expect(statusBlock).toBeInTheDocument();
    expect(screen.getByTestId("current-status-headline")).toHaveTextContent(
      /ready to start the executor/i,
    );

    // NextActionArea — primary "Start" control, since
    // status=approved_for_executor makes canStart true.
    const primaryButton = screen.getByRole("button", { name: "Start" });
    expect(primaryButton).toBeInTheDocument();
    expect(primaryButton).not.toBeDisabled();

    // ProgressionRail — present and collapsed (populated state; only one
    // entry here so no "Show full history" affordance is needed, but the
    // region itself renders inline without any extra expansion).
    const progressionRail = screen.getByTestId("progression-rail");
    expect(progressionRail).toBeInTheDocument();
    expect(
      within(progressionRail).getByText("Executor brief approved."),
    ).toBeInTheDocument();

    // Nothing from Detail_Disclosure's lazy sections is visible yet — the
    // outer "Show details" affordance is present but collapsed, and none
    // of the four execute-step section labels/content render.
    const showDetailsToggle = screen.getByRole("button", {
      name: /show details/i,
    });
    expect(showDetailsToggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Full logs")).not.toBeInTheDocument();
    expect(screen.queryByText("Executor result")).not.toBeInTheDocument();
    expect(screen.queryByText("Changed files")).not.toBeInTheDocument();
    expect(screen.queryByText("Validation report")).not.toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// 2. Opening Detail_Disclosure reveals exactly the four execute-step
//    sections, and not before (Req 5.6)
// ------------------------------------------------------------

describe("Execute route — Detail_Disclosure execute-step sections (Req 5.6)", () => {
  it("reveals exactly the four execute-step sections after opening 'Show details', and none before", async () => {
    const user = userEvent.setup();
    cleanups.push(installFetchStub(makeRunFixture()));

    await renderExecuteRoute();

    const sectionLabels = [
      "Full logs",
      "Executor result",
      "Changed files",
      "Validation report",
    ];

    // Not visible before opening.
    for (const label of sectionLabels) {
      expect(screen.queryByText(label)).not.toBeInTheDocument();
    }

    const showDetailsToggle = screen.getByRole("button", {
      name: /show details/i,
    });
    await user.click(showDetailsToggle);

    expect(showDetailsToggle).toHaveAttribute("aria-expanded", "true");

    // Exactly the four execute-step sections render as disclosure triggers
    // inside the sections list, once opened.
    const sectionsList = screen.getByTestId("detail-disclosure-sections");
    const sectionItems = within(sectionsList).getAllByTestId("detail-section");
    expect(sectionItems).toHaveLength(4);

    for (const label of sectionLabels) {
      expect(
        within(sectionsList).getByRole("button", { name: label }),
      ).toBeInTheDocument();
    }
  });
});

describe("isExecuteLiveStatus", () => {
  it("returns true for active execute statuses", () => {
    expect(isExecuteLiveStatus("executor_dispatched")).toBe(true);
    expect(isExecuteLiveStatus("executor_running")).toBe(true);
    expect(isExecuteLiveStatus("local_validation_running")).toBe(true);
  });

  it("returns false for terminal or unrelated statuses", () => {
    expect(isExecuteLiveStatus("executor_done")).toBe(false);
    expect(isExecuteLiveStatus("approved_for_executor")).toBe(false);
    expect(isExecuteLiveStatus(undefined)).toBe(false);
  });
});

describe("formatExecutorPacket", () => {
  it("renders the sample tool_use/read packet as a readable progress row", () => {
    const lines = formatExecutorPacket(sampleToolUsePacket);
    expect(lines.length).toBeGreaterThan(0);
    expect(lines[0]).toMatch(/tool\s+read\s+completed/);
    expect(lines[0]).toContain(
      "docs/generated/agent-references/index.json",
    );
    expect(lines[0]).not.toContain("D:\\\\Code\\\\relay");
    expect(lines[0]).not.toContain('"type": "tool_use"');
  });

  it("falls back to a trimmed text line for invalid JSON", () => {
    const lines = formatExecutorPacket("some raw text that is not json");
    expect(lines.length).toBe(1);
    expect(lines[0]).toContain("some raw text");
  });

  it("returns an empty array for empty input", () => {
    expect(formatExecutorPacket("")).toEqual([]);
  });
});

describe("deriveLiveExecutorProgress", () => {
  it("includes event messages and does not include raw artifact previews", () => {
    const events = [
      {
        id: "1",
        runId: "42",
        kind: "log" as const,
        message: "Executor started",
        createdAt: "2026-06-27T10:00:00.000Z",
      },
      {
        id: "2",
        runId: "42",
        kind: "log" as const,
        message: "Read file executor.go",
        createdAt: "2026-06-27T10:00:01.000Z",
      },
    ];
    const artifacts = [
      {
        id: "a1",
        label: "Executor Stdout",
        path: "/api/runs/42/artifacts/executor_stdout",
        kind: "executor_stdout",
        status: "ready",
        filename: "executor_stdout.txt",
        preview: sampleToolUsePacket,
        createdAt: "2026-06-27T10:00:01.000Z",
      },
      {
        id: "a2",
        label: "Command Log",
        path: "/api/runs/42/artifacts/command_log",
        kind: "command_log",
        status: "ready",
        filename: "command_log.txt",
        preview: "Command: opencode run...",
        createdAt: "2026-06-27T10:00:02.000Z",
      },
    ];

    const lines = deriveLiveExecutorProgress(events, artifacts);
    expect(lines.length).toBe(2);
    expect(lines[0]).toContain("Executor started");
    expect(lines[1]).toContain("Read file executor.go");
  });

  it("does not render raw JSON artifact previews in live progress", () => {
    const events: RelayRunEvent[] = [];
    const artifacts = [
      {
        id: "a1",
        label: "Executor Stdout",
        path: "/api/runs/42/artifacts/executor_stdout",
        kind: "executor_stdout",
        status: "ready",
        filename: "executor_stdout.txt",
        preview: sampleToolUsePacket,
        createdAt: "2026-06-27T10:00:01.000Z",
      },
    ];

    const lines = deriveLiveExecutorProgress(events, artifacts);
    expect(lines.length).toBe(0);
  });

  it("does not contain raw JSON or protocol fields in any line", () => {
    const events = [
      {
        id: "1",
        runId: "42",
        kind: "log" as const,
        message: "Executor dispatched: opencode ...",
        createdAt: "2026-06-27T10:00:00.000Z",
      },
    ];
    const artifacts = [
      {
        id: "a1",
        label: "Executor Stdout",
        path: "/api/runs/42/artifacts/executor_stdout",
        kind: "executor_stdout",
        status: "ready",
        filename: "executor_stdout.txt",
        preview: sampleToolUsePacket,
        createdAt: "2026-06-27T10:00:01.000Z",
      },
    ];

    const lines = deriveLiveExecutorProgress(events, artifacts);
    for (const line of lines) {
      expect(line).not.toContain("sessionID");
      expect(line).not.toContain('"timestamp"');
      expect(line).not.toContain('"type": "tool_use"');
      expect(line).not.toContain('"part"');
    }
  });

  it("limits output to the most recent 100 lines", () => {
    const events = Array.from({ length: 120 }, (_, i) => ({
      id: String(i),
      runId: "42",
      kind: "log" as const,
      message: `event ${i}`,
      createdAt: new Date(1e12 + i * 1000).toISOString(),
    }));
    const lines = deriveLiveExecutorProgress(events, []);
    expect(lines.length).toBe(100);
    expect(lines[lines.length - 1]).toContain("event 119");
  });
});

// ============================================================
// Bug condition exploration test (step-navigation-missing, task 1)
// ============================================================
//
// **Property 1: Bug Condition** - Forward Step Navigation Available on
// Prepare and Execute.
//
// CRITICAL: this test MUST FAIL on unfixed code. Its failure confirms the
// bug exists — `execute.tsx`'s `onActionClick` has no branch for a
// "proceedToAudit" id, and `deriveExecuteActions` has no such candidate
// yet, so no on-screen control exists that navigates from execute to
// audit.
//
// Scoped PBT approach (per tasks.md task 1): scope to representative
// gating states within the execute route via
// `fc.record({ canStart: fc.boolean(), canRecover: fc.boolean(), canCancel:
// fc.boolean() })`, driven through the route's underlying run `status`
// (approved_for_executor => canStart; executor_dispatched/executor_running
// => canCancel; otherwise neither, mirroring execute.tsx's own gating) so
// the rendered route reflects each combination.
//
// **Validates: Requirements 1.1, 1.2, 1.3**

const EXECUTE_STATUS_FOR_GATING_ARB = fc.constantFrom(
  "approved_for_executor", // canStart true
  "executor_dispatched", // canCancel true
  "executor_running", // canCancel true
  "executor_done", // all false
);

describe("Execute route — Property 1: pipeline navigation to Audit (IdentityStrip)", () => {
  it(
    "renders a clickable 'Audit' pipeline-position control that navigates to audit without mutating (Req 1.1, 1.2, 1.3)",
    { timeout: 30_000 },
    async () => {
      await fc.assert(
        fc.asyncProperty(EXECUTE_STATUS_FOR_GATING_ARB, async (status) => {
          vi.clearAllMocks();
          cleanups.push(installFetchStub(makeRunFixture(status)));

          const { unmount, router } = await renderExecuteRouteForExploration();

          try {
            // EXPECTED (fixed) behavior: the IdentityStrip's pipeline
            // position overview renders a clickable "Audit" stage control,
            // regardless of gating status.
            const pipelinePosition = screen.getByRole("group", {
              name: /pipeline position/i,
            });
            const auditStageButton = within(pipelinePosition).getByRole(
              "button",
              { name: /audit/i },
            );
            expect(auditStageButton).toBeInTheDocument();

            await userEvent.click(auditStageButton);

            // Clicking navigates to the run's audit route ...
            expect(router.state.location.pathname).toBe(
              `/runs/${RUN_ID}/audit`,
            );

            // ... and does NOT invoke any EXECUTE_ACTION_HANDLERS mutation
            // (NOT mutatesCanonicalRunStatus(result)).
            expect(executeRunMock).not.toHaveBeenCalled();
            expect(cancelRunMock).not.toHaveBeenCalled();
            expect(recoverRunMock).not.toHaveBeenCalled();
          } finally {
            unmount();
          }
        }),
        { numRuns: 4 },
      );
    },
  );
});
