// @vitest-environment jsdom
//
// ============================================================
// Intake route composition tests (task 14.2)
// ============================================================
//
// Covers Requirement 5.9: `DetailDisclosure`'s "Show details" affordance
// gates the four intake-step sections (Parsed frontmatter / Run config /
// Intake validation report / Raw handoff) - none of that content renders
// until the Operator opens it.
//
// Covers Requirement 6.4: the intake route's default composition renders
// identity (`IdentityStrip`), current status (`CurrentStatusBlock`), the
// next action area (`NextActionArea`), and a collapsed progression rail
// (`ProgressionRail`) all at once, without expanding anything.
//
// This file mounts the REAL `Route.options.component` exported by
// `intake.tsx` (rather than re-deriving the composition), mirroring the
// approach `RunStatusTrackerLayout.test.tsx`/`RunWorkbenchLayout.test.tsx`
// use for router-dependent presentational composition: a small in-memory
// router registers the `/runs/$runId/intake` path plus destinations the
// page's `Link`s can resolve to, and `@/features/relay-runs/api` is mocked
// so no real network calls occur.

import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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

import type { RelayArtifact, RelayRun, RelayRunEvent } from "@/features/relay-runs";

vi.mock("@/features/relay-runs/api", async () => {
  const actual = await vi.importActual<typeof import("@/features/relay-runs/api")>(
    "@/features/relay-runs/api",
  );
  return {
    ...actual,
    approveIntake: vi.fn().mockResolvedValue({ success: true }),
  };
});

import { Route as IntakeRoute, buildIntakeActionsView } from "./intake";

// ------------------------------------------------------------
// Fixtures
// ------------------------------------------------------------

const RUN_ID = "run-intake-1";

// `intake_needs_review` gives a clear reviewable state so `isReviewable` is
// true and "Approve Intake" renders as the primary Next_Safe_Action.
const BASE_RUN: RelayRun = {
  id: RUN_ID,
  name: "Ship the intake composition",
  title: "Ship the intake composition",
  repo: "acme/relay-ui",
  branch: "feature/intake-tracker",
  activeStep: "intake",
  status: "intake_needs_review",
  lifecycleState: "intake",
  createdAt: "2024-01-01T00:00:00.000Z",
  updatedAt: "2024-01-01T00:00:00.000Z",
  summary: "",
  model: "claude-sonnet",
  riskLevel: "low",
  validation: { errors: 0, warnings: 0, passed: 0 },
  artifacts: [],
  latestEvents: [],
  statusSeverity: "warning",
  state: "needs_review",
} as unknown as RelayRun;

const BASE_ARTIFACTS: RelayArtifact[] = [
  {
    id: "artifact-frontmatter",
    label: "Parsed Frontmatter",
    path: "/artifacts/artifact-frontmatter",
    kind: "parsed_frontmatter",
    status: "ready",
    filename: "parsed_frontmatter.json",
    preview: JSON.stringify({ repo: "acme/relay-ui", branch: "feature/intake-tracker" }),
  },
  {
    id: "artifact-run-config",
    label: "Run Config",
    path: "/artifacts/artifact-run-config",
    kind: "run_config",
    status: "ready",
    filename: "run_config.json",
    preview: JSON.stringify({ executor_adapter: "opencode_go" }),
  },
  {
    id: "artifact-intake-validation",
    label: "Intake Validation Report",
    path: "/artifacts/artifact-intake-validation",
    kind: "intake_validation_report",
    status: "ready",
    filename: "intake_validation_report.json",
    preview: JSON.stringify({ errors: 0, warnings: 0 }),
  },
  {
    id: "artifact-handoff",
    label: "Planner Handoff",
    path: "/artifacts/artifact-handoff",
    kind: "planner_handoff",
    status: "ready",
    filename: "handoff.md",
    preview: "# Handoff\n\nRAW-HANDOFF-MARKER",
  },
];

const BASE_EVENTS: RelayRunEvent[] = [
  {
    id: "event-1",
    runId: RUN_ID,
    kind: "status_change",
    message: "Intake received from Planner handoff",
    createdAt: "2024-01-01T00:00:00.000Z",
  },
];

// ------------------------------------------------------------
// Render harness
// ------------------------------------------------------------

function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false },
    },
  });
}

interface RenderIntakeOptions {
  run?: RelayRun;
  artifacts?: RelayArtifact[];
  events?: RelayRunEvent[];
}

// Pre-seeding the QueryClient cache (below) means the route's `useQuery`
// calls resolve from cache without a network round-trip, but React Query
// still reports `isLoading: true` for exactly one render tick before the
// cached data is applied (it initializes each observer asynchronously).
// The route renders `RunWorkbenchLoadingState` for that first tick. Callers
// therefore await the resolved run title before asserting on the loaded
// composition, mirroring how real navigations settle.
async function renderIntakeRoute(options: RenderIntakeOptions = {}) {
  const run = options.run ?? BASE_RUN;
  const artifacts = options.artifacts ?? BASE_ARTIFACTS;
  const events = options.events ?? BASE_EVENTS;

  const queryClient = makeQueryClient();
  // Pre-seed the exact query keys `runDetailQueryOptions`/
  // `runArtifactsQueryOptions`/`runEventsQueryOptions` use, so the route's
  // `useQuery` calls resolve from cache without hitting the network
  // (mirroring `relayRunKeys` from `queries.ts`).
  queryClient.setQueryData(["relay-runs", "detail", run.id], run);
  queryClient.setQueryData(["relay-runs", "detail", run.id, "artifacts"], artifacts);
  queryClient.setQueryData(["relay-runs", "detail", run.id, "events"], events);

  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const intakeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/intake",
    component: IntakeRoute.options.component,
  });

  const prepareRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId/prepare",
    component: () => <div>prepare-destination</div>,
  });

  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: () => <div>runs-registry-destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([intakeRoute, prepareRoute, runsRoute]),
    history: createMemoryHistory({ initialEntries: [`/runs/${run.id}/intake`] }),
    defaultPendingMinMs: 0,
  });

  await router.load();

  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );

  // Wait for the loaded composition (identity title) to appear before
  // handing control back to the test.
  await screen.findByText(run.title);

  return { ...result, queryClient };
}

// ------------------------------------------------------------
// 1. Default composition (Req 5.9, 6.4)
// ------------------------------------------------------------

describe("Intake route - default composition (Req 6.4)", () => {
  it("renders identity, current status, next action, and collapsed progression without expanding anything", async () => {
    await renderIntakeRoute();

    // IdentityStrip - run identity (title/id, repo, branch, model) and the
    // compact pipeline-position overview.
    expect(screen.getByText(BASE_RUN.title)).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: /pipeline position/i }),
    ).toBeInTheDocument();

    // CurrentStatusBlock - the single current-status sentence for the
    // intake not-blocked case.
    const statusBlock = screen.getByTestId("current-status-block");
    expect(statusBlock).toBeInTheDocument();
    expect(screen.getByTestId("current-status-headline").textContent).toMatch(
      /waiting for you to review the incoming handoff/i,
    );

    // NextActionArea - "Approve Intake" is the primary Next_Safe_Action for
    // the `intake_needs_review` status.
    expect(
      screen.getByRole("button", { name: "Approve Intake" }),
    ).toBeInTheDocument();

    // ProgressionRail - collapsed by default, showing the seeded event.
    const progressionRail = screen.getByTestId("progression-rail");
    expect(progressionRail).toBeInTheDocument();
    expect(progressionRail.textContent).toContain(
      "Intake received from Planner handoff",
    );

    // Nothing from DetailDisclosure's lazy sections is visible yet.
    expect(screen.queryByText("Parsed frontmatter")).not.toBeInTheDocument();
    expect(screen.queryByText("Run config")).not.toBeInTheDocument();
    expect(screen.queryByText("Intake validation report")).not.toBeInTheDocument();
    expect(screen.queryByText("Raw handoff")).not.toBeInTheDocument();
    expect(screen.queryByText("RAW-HANDOFF-MARKER")).not.toBeInTheDocument();

    // The outer "Show details" affordance itself is present but collapsed.
    const showDetailsToggle = screen.getByRole("button", { name: /show details/i });
    expect(showDetailsToggle).toHaveAttribute("aria-expanded", "false");
  });
});

// ------------------------------------------------------------
// 2. DetailDisclosure reveals exactly the four intake sections (Req 5.9)
// ------------------------------------------------------------

describe("Intake route - DetailDisclosure reveals the four intake sections (Req 5.9)", () => {
  it("reveals the four intake-step section labels only after opening Show details, and not before", async () => {
    const user = userEvent.setup();
    await renderIntakeRoute();

    // Not before opening.
    expect(screen.queryByText("Parsed frontmatter")).not.toBeInTheDocument();
    expect(screen.queryByText("Run config")).not.toBeInTheDocument();
    expect(screen.queryByText("Intake validation report")).not.toBeInTheDocument();
    expect(screen.queryByText("Raw handoff")).not.toBeInTheDocument();

    const showDetailsToggle = screen.getByRole("button", { name: /show details/i });
    await user.click(showDetailsToggle);

    // Exactly the four intake-step sections appear as section labels.
    const sections = screen.getAllByTestId("detail-section");
    expect(sections).toHaveLength(4);

    expect(screen.getByText("Parsed frontmatter")).toBeInTheDocument();
    expect(screen.getByText("Run config")).toBeInTheDocument();
    expect(screen.getByText("Intake validation report")).toBeInTheDocument();
    expect(screen.getByText("Raw handoff")).toBeInTheDocument();

    // Each section's content is still lazy - the section labels are visible,
    // but the raw handoff content is not rendered until that specific
    // section is individually opened.
    expect(screen.queryByText("RAW-HANDOFF-MARKER")).not.toBeInTheDocument();

    const rawHandoffToggle = screen.getByRole("button", { name: "Raw handoff" });
    await user.click(rawHandoffToggle);

    expect(await screen.findByText("Planner Handoff")).toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// 3. Preservation property test (step-navigation-missing bugfix, task 2)
// ------------------------------------------------------------
//
// Property 2: Preservation - Existing Navigation and Gated Action Behavior
// Unchanged (design.md). Observed on UNFIXED code: for randomly generated
// `isReviewable`/`isApproved` booleans, `buildIntakeActionsView`'s existing
// `proceedToPrepare` control has `enabled === isApproved` and the same
// `unavailableReason` text as observed today. Re-run unchanged after the
// fix (task 3.5) to confirm no regression.
//
// Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5

describe("buildIntakeActionsView — Preservation: proceedToPrepare gating is unchanged (Property 2)", () => {
  it("proceedToPrepare.enabled === isApproved, with unavailableReason text present iff not approved, for any isReviewable/isApproved combination", () => {
    fc.assert(
      fc.property(fc.boolean(), fc.boolean(), (isReviewable, isApproved) => {
        const view = buildIntakeActionsView({ isReviewable, isApproved });
        const proceedToPrepare = view.controls.find(
          (control) => control.id === "proceedToPrepare",
        );

        expect(proceedToPrepare).toBeDefined();
        expect(proceedToPrepare!.enabled).toBe(isApproved);
        expect(proceedToPrepare!.label).toBe("Proceed to Compile / Render");

        if (isApproved) {
          expect(proceedToPrepare!.unavailableReason).toBeUndefined();
        } else {
          expect(proceedToPrepare!.unavailableReason).toBe(
            "Approve intake before moving to Compile / Render.",
          );
        }
      }),
      { numRuns: 100 },
    );
  });
});
