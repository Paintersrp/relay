// @vitest-environment jsdom
//
// ============================================================
// Prepare route composition tests (task 13.2)
// ============================================================
//
// Covers:
//   - Requirement 5.8: opening Detail_Disclosure reveals exactly the five
//     prepare-step sections (canonical packet, packet validation report,
//     executor brief, brief validation issues, repair result) — not
//     before opening.
//   - Requirement 6.4: the default view renders IdentityStrip,
//     CurrentStatusBlock, NextActionArea, and a collapsed ProgressionRail
//     simultaneously, without expanding anything.
//
// The prepare route (`prepare.tsx`) fetches run/artifacts/events via
// `useQuery` + the existing `api.ts` GET endpoints, so this harness stubs
// `globalThis.fetch` over those endpoints (mirroring the
// `installApiStub`/`shell-test-utils.tsx` convention) rather than mocking
// `useQuery` directly, and mounts the real route component inside a small
// in-memory router (mirroring `RunStatusTrackerLayout.test.tsx`).

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
// existing mutation functions `prepare.tsx` invokes, so the exploration
// property test below can assert that clicking a "proceed to execute"
// control (once one exists) does NOT invoke any of them, i.e.
// `NOT mutatesCanonicalRunStatus(result)`.
vi.mock("@/features/relay-runs", async () => {
  const actual = await vi.importActual<typeof import("@/features/relay-runs")>(
    "@/features/relay-runs",
  );
  return {
    ...actual,
    prepareRun: vi.fn().mockResolvedValue({ success: true }),
    renderBrief: vi.fn().mockResolvedValue({ success: true }),
    approveBrief: vi.fn().mockResolvedValue({ success: true }),
    repairValidation: vi.fn().mockResolvedValue({ success: true }),
  };
});

import { Route as PrepareRouteImport, buildPrepareActionsView } from "./prepare";
import {
  prepareRun as prepareRunMock,
  renderBrief as renderBriefMock,
  approveBrief as approveBriefMock,
  repairValidation as repairValidationMock,
} from "@/features/relay-runs";

const RUN_ID = "run-prepare-1";

// ------------------------------------------------------------
// Fetch stub over the existing GET endpoints this route relies on
// (`GET /api/runs/:id`, `GET /api/runs/:id/artifacts`,
// `GET /api/runs/:id/events`).
// ------------------------------------------------------------

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
// Fixture — status "approved_for_prepare" makes `canCompile` true, giving
// the route a primary "Run Compile" action.
// ------------------------------------------------------------

function makeRunFixture(status: string = "approved_for_prepare"): RunFixture {
  return {
    run: {
      id: RUN_ID,
      title: "Ship the prepare tracker composition",
      name: "Ship the prepare tracker composition",
      repo: "acme/relay-ui",
      branch: "feature/prepare-tracker",
      status,
      activeStep: "prepare",
      lifecycleState: "prepare",
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
        message: "Intake approved.",
        createdAt: "2023-12-31T23:00:00.000Z",
      },
    ],
  };
}

// ------------------------------------------------------------
// Render harness — mounts the real prepare route component inside a small
// in-memory router (mirroring RunStatusTrackerLayout.test.tsx), over a
// test-scoped QueryClientProvider and the fetch stub above.
// ------------------------------------------------------------

function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, gcTime: 0 },
    },
  });
}

async function renderPrepareRoute() {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const prepareRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/prepare",
    component: PrepareRouteImport.options.component,
  });

  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: () => <div>runs-registry-destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([prepareRoute, runsRoute]),
    history: createMemoryHistory({
      initialEntries: [`/runs/${RUN_ID}/prepare`],
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
// task 1) — additionally registers a `/runs/$runId/execute` destination
// route so the exploration property test can assert on where the router
// actually lands after clicking a "proceed to execute" control, and
// exposes the `router` so the test can inspect `router.state.location`.
// ------------------------------------------------------------

async function renderPrepareRouteForExploration(_status: string) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const prepareRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/prepare",
    component: PrepareRouteImport.options.component,
  });

  const executeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/execute",
    component: () => <div>execute-destination</div>,
  });

  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: () => <div>runs-registry-destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([prepareRoute, executeRoute, runsRoute]),
    history: createMemoryHistory({
      initialEntries: [`/runs/${RUN_ID}/prepare`],
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

describe("Prepare route — default composition (Req 6.4)", () => {
  it("renders identity, status, action, and a collapsed progression rail without expanding any Detail_Disclosure section", async () => {
    cleanups.push(installFetchStub(makeRunFixture()));

    await renderPrepareRoute();

    // IdentityStrip — run identity (title, repo, pipeline position group).
    expect(
      screen.getByText("Ship the prepare tracker composition"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: /pipeline position/i }),
    ).toBeInTheDocument();

    // CurrentStatusBlock — exactly one current-status headline.
    const statusBlock = screen.getByTestId("current-status-block");
    expect(statusBlock).toBeInTheDocument();
    expect(screen.getByTestId("current-status-headline")).toHaveTextContent(
      /ready to compile/i,
    );

    // NextActionArea — primary "Run Compile" control, since
    // status=approved_for_prepare makes canCompile true.
    const primaryButton = screen.getByRole("button", { name: "Run Compile" });
    expect(primaryButton).toBeInTheDocument();
    expect(primaryButton).not.toBeDisabled();

    // ProgressionRail — present and collapsed (populated state, no
    // "Show full history" needed here since there's only one entry, but
    // the region itself renders inline without any extra expansion).
    const progressionRail = screen.getByTestId("progression-rail");
    expect(progressionRail).toBeInTheDocument();
    expect(within(progressionRail).getByText("Intake approved.")).toBeInTheDocument();

    // Nothing from Detail_Disclosure's lazy sections is visible yet — the
    // outer "Show details" affordance is present but collapsed, and none
    // of the five prepare-step section labels/content render.
    const showDetailsToggle = screen.getByRole("button", {
      name: /show details/i,
    });
    expect(showDetailsToggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Canonical packet")).not.toBeInTheDocument();
    expect(screen.queryByText("Packet validation report")).not.toBeInTheDocument();
    expect(screen.queryByText("Executor brief")).not.toBeInTheDocument();
    expect(screen.queryByText("Brief validation issues")).not.toBeInTheDocument();
    expect(screen.queryByText("Repair result")).not.toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// 2. Opening Detail_Disclosure reveals exactly the five prepare-step
//    sections, and not before (Req 5.8)
// ------------------------------------------------------------

describe("Prepare route — Detail_Disclosure prepare-step sections (Req 5.8)", () => {
  it("reveals exactly the five prepare-step sections after opening 'Show details', and none before", async () => {
    const user = userEvent.setup();
    cleanups.push(installFetchStub(makeRunFixture()));

    await renderPrepareRoute();

    const sectionLabels = [
      "Canonical packet",
      "Packet validation report",
      "Executor brief",
      "Brief validation issues",
      "Repair result",
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

    // Exactly the five prepare-step sections render as disclosure triggers
    // inside the sections list, once opened.
    const sectionsList = screen.getByTestId("detail-disclosure-sections");
    const sectionItems = within(sectionsList).getAllByTestId("detail-section");
    expect(sectionItems).toHaveLength(5);

    for (const label of sectionLabels) {
      expect(
        within(sectionsList).getByRole("button", { name: label }),
      ).toBeInTheDocument();
    }
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
// bug exists — `buildPrepareActionsView` has no `proceedToExecute`
// candidate yet, so no on-screen control exists that navigates from
// prepare to execute.
//
// Scoped PBT approach (per tasks.md task 1): `isBugCondition(X) =
// X.currentStep IN {'prepare', 'execute'}` is deterministic per-route, so
// this scopes the property to representative gating states within the
// prepare route via `fc.constantFrom` over prepare's five representative
// statuses (mirrors design.md's Bug Details examples).
//
// **Validates: Requirements 1.1, 1.2, 1.3**

const PREPARE_STATUS_ARB = fc.constantFrom(
  "approved_for_prepare",
  "packet_validation_failed",
  "packet_validated",
  "repair_validated",
  "brief_ready_for_review",
);

describe("Prepare route — Property 1: pipeline navigation to Execute (IdentityStrip)", () => {
  it(
    "renders a clickable 'Execute' pipeline-position control that navigates to execute without mutating (Req 1.1, 1.2, 1.3)",
    { timeout: 30_000 },
    async () => {
      await fc.assert(
        fc.asyncProperty(PREPARE_STATUS_ARB, async (status) => {
          vi.clearAllMocks();
          cleanups.push(installFetchStub(makeRunFixture(status)));

          const { unmount, router } = await renderPrepareRouteForExploration(status);

          try {
            // EXPECTED (fixed) behavior: the IdentityStrip's pipeline
            // position overview renders a clickable "Execute" stage
            // control, regardless of gating status.
            const pipelinePosition = screen.getByRole("group", {
              name: /pipeline position/i,
            });
            const executeStageButton = within(pipelinePosition).getByRole(
              "button",
              { name: /execute/i },
            );
            expect(executeStageButton).toBeInTheDocument();

            await userEvent.click(executeStageButton);

            // Clicking navigates to the run's execute route ...
            expect(router.state.location.pathname).toBe(
              `/runs/${RUN_ID}/execute`,
            );

            // ... and does NOT invoke any mutation
            // (NOT mutatesCanonicalRunStatus(result)).
            expect(prepareRunMock).not.toHaveBeenCalled();
            expect(renderBriefMock).not.toHaveBeenCalled();
            expect(approveBriefMock).not.toHaveBeenCalled();
            expect(repairValidationMock).not.toHaveBeenCalled();
          } finally {
            unmount();
          }
        }),
        { numRuns: 5 },
      );
    },
  );
});

// ------------------------------------------------------------
// 3. Preservation property test (step-navigation-missing bugfix, task 2)
// ------------------------------------------------------------
//
// Property 2: Preservation - Existing Navigation and Gated Action Behavior
// Unchanged (design.md). Observed on UNFIXED code (this route file does not
// yet have a `proceedToExecute` candidate): for randomly generated
// canCompile/canRetryCompile/canAttemptRepair/canRenderBrief/canApproveBrief
// combinations where at least one is true, the existing candidate selected
// primary (first-enabled-in-priority-order: compile -> retryCompile ->
// attemptRepair -> renderBrief -> approveBrief) is observed on unfixed code.
// This test is written to be re-run unchanged after the fix (task 3.5) to
// confirm the new trailing `proceedToExecute` candidate never preempts an
// already-enabled existing candidate as primary.
//
// Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5

const PREPARE_PRIORITY_IDS = [
  "compile",
  "retryCompile",
  "attemptRepair",
  "renderBrief",
  "approveBrief",
];

const prepareGatingArb = fc
  .record({
    canCompile: fc.boolean(),
    canRetryCompile: fc.boolean(),
    canAttemptRepair: fc.boolean(),
    canRenderBrief: fc.boolean(),
    canApproveBrief: fc.boolean(),
  })
  .filter((gating) => Object.values(gating).some(Boolean));

describe("buildPrepareActionsView — Preservation: existing candidate primary-selection is unchanged (Property 2)", () => {
  it("selects the first-enabled-in-priority-order existing candidate as primary, for any gating combination with at least one true flag", () => {
    fc.assert(
      fc.property(prepareGatingArb, (gating) => {
        const view = buildPrepareActionsView(gating);

        const flagById: Record<string, boolean> = {
          compile: gating.canCompile,
          retryCompile: gating.canRetryCompile,
          attemptRepair: gating.canAttemptRepair,
          renderBrief: gating.canRenderBrief,
          approveBrief: gating.canApproveBrief,
        };
        const expectedPrimaryId = PREPARE_PRIORITY_IDS.find(
          (id) => flagById[id],
        );

        const primaryControls = view.controls.filter((c) => c.isPrimary);
        expect(primaryControls).toHaveLength(1);
        expect(primaryControls[0].id).toBe(expectedPrimaryId);
        expect(view.nextSafeActionId).toBe(expectedPrimaryId);
        expect(primaryControls[0].enabled).toBe(true);

        // Every existing candidate's enabled value mirrors its own can* flag
        // exactly, regardless of which one ends up primary.
        for (const id of PREPARE_PRIORITY_IDS) {
          const control = view.controls.find((c) => c.id === id);
          expect(control?.enabled).toBe(flagById[id]);
        }
      }),
      { numRuns: 100 },
    );
  });
});
