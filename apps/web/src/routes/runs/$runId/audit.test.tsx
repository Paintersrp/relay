// @vitest-environment jsdom
//
// ============================================================
// Audit route composition tests (task 12.2)
// ============================================================
//
// Covers the audit route's composition after task 12.1 rewired it onto
// `RunStatusTrackerLayout`:
//
//   - the default view renders identity/status/action/collapsed-progression
//     simultaneously, without expanding anything                    (Req 5.7, 6.4)
//   - opening `DetailDisclosure`'s "Show details" affordance reveals exactly
//     the five audit-step sections ("Audit packet preview", "Commit message
//     preview", "Input summary", "Validation report", "Revision
//     requirements") and none of them are present before opening
//                                                                     (Req 5.7, 6.4)
//
// The route's own detail-section content (packet preview markup, validation
// report interaction, etc.) is exercised by the route's existing
// `deriveAuditData` unit coverage and the shared `DetailDisclosure`/
// `RunStatusTrackerLayout` component tests — this file only asserts the
// composition wiring done in `audit.tsx`.

import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, type RenderResult } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

import { Route as AuditRouteModule } from "./audit";
import { relayRunKeys } from "@/features/relay-runs";
import type {
  RelayArtifact,
  RelayAuditStatus,
  RelayRun,
  RelayRunEvent,
} from "@/features/relay-runs";

// The real file-route's component. Reused directly (rather than calling
// `.update()` on the shared `Route` singleton also mutated by
// `routeTree.gen.ts`) so this test never touches that singleton's
// id/path/parentRoute wiring — see the render harness below.
const AuditPageComponent = AuditRouteModule.options.component;

const RUN_ID = "run-audit-composition-1";

// `validation_passed` is one of the statuses `isAuditCandidateStatus`
// recognizes, and pairing it with a `validation_run_json` artifact makes
// `evaluateValidationGate` report `validationAllowsAudit: true` — giving a
// clean, single-primary-action fixture: `canGenerateAudit` is the only
// enabled audit action, so "Generate Audit" renders as the sole primary
// Next_Action_Area control.
const RUN: RelayRun = {
  id: RUN_ID,
  name: "Ship the audit tracker composition test",
  repo: "acme/relay-ui",
  branch: "feature/audit-tracker",
  activeStep: "audit",
  status: "validation_passed",
  lifecycleState: "audit",
  createdAt: "2024-01-01T00:00:00.000Z",
  updatedAt: "2024-01-02T00:00:00.000Z",
  summary: "",
  model: "claude-sonnet",
  riskLevel: "low",
  validation: { errors: 0, warnings: 0, passed: 3 },
  artifacts: [],
  latestEvents: [],
  statusSeverity: "success",
  state: "validation_passed",
  title: "Ship the audit tracker composition test",
  packetId: "",
  executor: "opencode_go",
  executorAdapter: "opencode_go",
  validationSummary: { errors: 0, warnings: 0, passed: 3 },
  approvalGate: { label: "Intake Approval", state: "approved" },
  logPreview: { lines: [], truncated: false },
  stepLabels: {
    intake: "Intake / Configure",
    prepare: "Compile / Render",
    execute: "Execute",
    audit: "Audit / Close",
  },
};

const ARTIFACTS: RelayArtifact[] = [
  {
    id: "art-validation-run",
    label: "Validation Run",
    path: "/artifacts/validation_run.json",
    kind: "validation",
    storageKind: "validation_run_json",
    status: "ready",
    filename: "validation_run.json",
    createdAt: "2024-01-01T09:00:00.000Z",
  },
];

// Four events so the default-collapsed Progression_Rail (collapsedCount=3)
// has a genuine "Show full history" affordance to assert against without
// clicking it.
const EVENTS: RelayRunEvent[] = [
  {
    id: "evt-1",
    runId: RUN_ID,
    kind: "log",
    message: "Executor finished successfully",
    createdAt: "2024-01-01T10:00:00.000Z",
  },
  {
    id: "evt-2",
    runId: RUN_ID,
    kind: "validation_run",
    message: "Local validation passed",
    createdAt: "2024-01-01T11:00:00.000Z",
  },
  {
    id: "evt-3",
    runId: RUN_ID,
    kind: "log",
    message: "Preparing audit evidence",
    createdAt: "2024-01-01T12:00:00.000Z",
  },
  {
    id: "evt-4",
    runId: RUN_ID,
    kind: "status_change",
    message: "Status changed to validation_passed",
    createdAt: "2024-01-01T13:00:00.000Z",
  },
];

const AUDIT_STATUS: RelayAuditStatus = {
  runId: RUN_ID,
  runStatus: "validation_passed",
  auditState: "candidate",
  canGenerateAudit: true,
  canSubmitDecision: false,
  canApprove: false,
  canRequestRevision: false,
  canCloseRun: false,
  blockers: [],
  warnings: [],
  revisionRequirements: [],
  localOnly: true,
};

const FIVE_SECTION_LABELS = [
  "Audit packet preview",
  "Commit message preview",
  "Input summary",
  "Validation report",
  "Revision requirements",
];

/**
 * Mounts the real audit route component inside a small in-memory router
 * (mirroring the parent `/runs/$runId` -> `/audit` chain generated by
 * `routeTree.gen.ts`), with run/artifacts/events/auditStatus data seeded
 * directly into a test-scoped `QueryClient` via `setQueryData`. Seeding the
 * cache (rather than mocking `fetch`/`api.ts`) keeps every query already
 * "fresh" (within its `staleTime`) on mount, so no network call is made and
 * `isLoading` is `false` immediately.
 */
async function renderAuditRoute(): Promise<RenderResult> {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });

  const runsRunIdRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs/$runId",
    component: () => <Outlet />,
  });

  const auditRoute = createRoute({
    getParentRoute: () => runsRunIdRoute,
    path: "/audit",
    component: AuditPageComponent,
  });

  const runsIndexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/runs",
    component: () => <div>runs-registry-destination</div>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([
      runsRunIdRoute.addChildren([auditRoute]),
      runsIndexRoute,
    ]),
    history: createMemoryHistory({
      initialEntries: [`/runs/${RUN_ID}/audit`],
    }),
    defaultPendingMinMs: 0,
  });

  await router.load();

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, gcTime: 0 },
    },
  });
  queryClient.setQueryData(relayRunKeys.detail(RUN_ID), RUN);
  queryClient.setQueryData(relayRunKeys.artifacts(RUN_ID), ARTIFACTS);
  queryClient.setQueryData(relayRunKeys.events(RUN_ID), EVENTS);
  queryClient.setQueryData(relayRunKeys.auditStatus(RUN_ID), AUDIT_STATUS);

  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ------------------------------------------------------------
// Default view — identity/status/action/collapsed-progression render
// simultaneously, nothing from Detail_Disclosure is visible yet
// (Req 5.7, 6.4)
// ------------------------------------------------------------

describe("Audit route — default composition (Req 5.7, 6.4)", () => {
  it("renders identity, status, the primary action, and a collapsed progression rail without expanding anything", async () => {
    await renderAuditRoute();

    // IdentityStrip — run identity + compact pipeline-position overview.
    expect(screen.getByText(RUN.repo)).toBeInTheDocument();
    expect(screen.getByText(RUN.branch)).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: /pipeline position/i }),
    ).toBeInTheDocument();

    // CurrentStatusBlock — the single current-status sentence.
    const statusBlock = screen.getByTestId("current-status-block");
    expect(statusBlock).toBeInTheDocument();
    expect(screen.getByTestId("current-status-headline")).toHaveTextContent(
      "Audit can be generated",
    );

    // NextActionArea — canGenerateAudit is the only enabled action, so
    // "Generate Audit" is the sole primary control.
    expect(
      screen.getByRole("button", { name: "Generate Audit" }),
    ).toBeInTheDocument();

    // ProgressionRail — populated, collapsed to the 3 most recent entries;
    // the 4th only appears behind "Show full history".
    const rail = screen.getByTestId("progression-rail");
    expect(rail).toHaveAttribute("data-state", "populated");
    expect(screen.getAllByTestId("progression-entry")).toHaveLength(3);
    expect(screen.getByText("Status changed to validation_passed")).toBeInTheDocument();
    expect(screen.queryByText("Executor finished successfully")).not.toBeInTheDocument();

    const historyToggle = screen.getByRole("button", {
      name: /show full history \(4\)/i,
    });
    expect(historyToggle).toHaveAttribute("aria-expanded", "false");

    // Detail_Disclosure — collapsed by default; none of the five audit-step
    // sections (or their content) render before it is opened.
    const detailsToggle = screen.getByRole("button", { name: /show details/i });
    expect(detailsToggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("detail-disclosure-sections")).not.toBeInTheDocument();
    for (const label of FIVE_SECTION_LABELS) {
      expect(screen.queryByText(label)).not.toBeInTheDocument();
    }
  });
});

// ------------------------------------------------------------
// Opening Detail_Disclosure reveals exactly the five audit-step sections,
// and not before (Req 5.7, 6.4)
// ------------------------------------------------------------

describe("Audit route — Detail_Disclosure reveals the five audit-step sections (Req 5.7, 6.4)", () => {
  it("reveals exactly the five audit-step sections once 'Show details' is opened", async () => {
    const user = userEvent.setup();
    await renderAuditRoute();

    for (const label of FIVE_SECTION_LABELS) {
      expect(screen.queryByText(label)).not.toBeInTheDocument();
    }

    const detailsToggle = screen.getByRole("button", { name: /show details/i });
    await user.click(detailsToggle);

    expect(
      screen.getByRole("button", { name: /hide details/i }),
    ).toBeInTheDocument();

    const sectionsList = screen.getByTestId("detail-disclosure-sections");
    expect(sectionsList).toBeInTheDocument();

    const sectionItems = screen.getAllByTestId("detail-section");
    expect(sectionItems).toHaveLength(5);

    for (const label of FIVE_SECTION_LABELS) {
      expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    }
  });
});
