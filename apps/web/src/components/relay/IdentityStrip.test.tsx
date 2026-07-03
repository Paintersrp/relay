// @vitest-environment jsdom
//
// ============================================================
// Example/unit tests — IdentityStrip (task 4.2)
// ============================================================
//
// Covers Requirement 1.1: identity fields (title/id, repo, branch, model)
// render exactly what `deriveRunIdentity(run)` produces.
//
// Covers Requirement 1.2: the compact four-step position overview reflects
// `derivePipelineStages(run.status)`'s completed/current/attention/pending
// classification.
//
// Covers Requirement 1.3: no `StatusBadge` and no current-status prose
// sentence renders anywhere in `IdentityStrip`'s output — that content
// lives solely in `CurrentStatusBlock`.
//
// Covers Requirement 1.4: the position overview is a compact element
// within this component (exactly one `role="group"` position overview,
// not an independent full-width stepper region).

import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import fc from "fast-check";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

import { IdentityStrip } from "./IdentityStrip";
import { deriveRunIdentity } from "@/features/relay-runs/runIdentity";
import { derivePipelineStages, PIPELINE_STAGE_ORDER } from "@/features/relay-navigation/pipeline";
import { getRelayStatusConfig } from "./relayVisualState";
import type { RelayRun, RelayRunStatus, RelayRunStep } from "@/features/relay-runs";

// ------------------------------------------------------------
// Fixtures
// ------------------------------------------------------------

function makeRun(overrides: Partial<RelayRun> = {}): RelayRun {
  return {
    id: "run-identity-1",
    name: "Ship the tracker redesign",
    title: "Ship the tracker redesign",
    repo: "acme/relay-ui",
    branch: "feature/tracker",
    activeStep: "execute",
    status: "executor_running",
    lifecycleState: "execute",
    createdAt: "2024-01-01T00:00:00.000Z",
    updatedAt: "2024-01-01T00:00:00.000Z",
    summary: "",
    model: "claude-sonnet",
    riskLevel: "low",
    validation: { errors: 0, warnings: 0 },
    artifacts: [],
    latestEvents: [],
    statusSeverity: "info",
    state: "running",
    ...overrides,
  } as unknown as RelayRun;
}

function positionOverview(): HTMLElement {
  return screen.getByRole("group", { name: "Pipeline position" });
}

// ------------------------------------------------------------
// Requirement 1.1 — identity fields pass through from deriveRunIdentity
// ------------------------------------------------------------

describe("IdentityStrip — identity fields pass through from deriveRunIdentity (Req 1.1)", () => {
  it("renders runId, primaryText, repo, branch, and model matching deriveRunIdentity(run) output", () => {
    const run = makeRun();
    const identity = deriveRunIdentity(run);

    render(<IdentityStrip run={run} currentStep="execute" />);

    expect(screen.getByText(identity.runId)).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 1, name: identity.primaryText }),
    ).toBeInTheDocument();
    expect(screen.getByText(identity.repo)).toBeInTheDocument();
    expect(identity.showBranch).toBe(true);
    expect(screen.getByText(identity.branch!)).toBeInTheDocument();
    expect(identity.showModel).toBe(true);
    expect(screen.getByText(identity.model!)).toBeInTheDocument();
  });

  it("falls back to the run id as the primary heading when title is empty, per deriveRunIdentity", () => {
    const run = makeRun({ title: "   " });
    const identity = deriveRunIdentity(run);
    expect(identity.primaryText).toBe(run.id);

    render(<IdentityStrip run={run} currentStep="execute" />);

    expect(
      screen.getByRole("heading", { level: 1, name: run.id }),
    ).toBeInTheDocument();
  });

  it("omits branch and model entirely when the source values are blank, per deriveRunIdentity", () => {
    const run = makeRun({ branch: "  ", model: "" });
    const identity = deriveRunIdentity(run);
    expect(identity.showBranch).toBe(false);
    expect(identity.showModel).toBe(false);

    const { container } = render(<IdentityStrip run={run} currentStep="execute" />);

    // The GitBranch/Bot meta-row entries are conditionally rendered off
    // showBranch/showModel; their icons are the reliable presence markers
    // since the underlying source values are blank/whitespace.
    expect(container.querySelector(".lucide-git-branch")).toBeNull();
    expect(container.querySelector(".lucide-bot")).toBeNull();
  });
});

// ------------------------------------------------------------
// Requirement 1.2 — four-step overview reflects derivePipelineStages
// ------------------------------------------------------------

describe("IdentityStrip — four-step overview reflects derivePipelineStages (Req 1.2)", () => {
  it("renders exactly four stage segments, in pipeline order, matching derivePipelineStages(run.status)'s classification and labels", () => {
    const run = makeRun({ status: "executor_running" as RelayRunStatus });
    const stages = derivePipelineStages(run.status);

    render(<IdentityStrip run={run} currentStep="execute" />);

    const overview = positionOverview();
    const segments = within(overview).getAllByText(
      (_, element) => element?.hasAttribute("data-stage-status") ?? false,
    );

    expect(segments).toHaveLength(4);
    expect(segments.map((segment) => segment.getAttribute("data-stage-status"))).toEqual(
      stages.map((stage) => stage.status),
    );
    expect(segments.map((segment) => segment.textContent?.trim())).toEqual(
      stages.map((stage) => stage.label),
    );

    // Exactly one stage is the current position ("current" for a
    // non-attention in-progress status), matching Requirement 1.2/6.2. The
    // rendered segment for that stage carries "location" rather than "step"
    // here because `currentStep="execute"` also matches the active route.
    const currentIndex = stages.findIndex((stage) => stage.status === "current");
    expect(currentIndex).toBeGreaterThanOrEqual(0);
    expect(segments[currentIndex]).toHaveAttribute("aria-current", "location");
  });

  it("marks the affected stage as 'attention' when status is in the closed blocked set, matching derivePipelineStages", () => {
    const run = makeRun({ status: "executor_blocked" as RelayRunStatus });
    const stages = derivePipelineStages(run.status);
    expect(stages.some((stage) => stage.status === "attention")).toBe(true);

    render(<IdentityStrip run={run} currentStep="execute" />);

    const overview = positionOverview();
    const segments = within(overview).getAllByText(
      (_, element) => element?.hasAttribute("data-stage-status") ?? false,
    );
    expect(segments.map((segment) => segment.getAttribute("data-stage-status"))).toEqual(
      stages.map((stage) => stage.status),
    );
  });

  it("marks every stage 'pending' and none current when status does not map to a canonical stage", () => {
    const run = makeRun({ status: "not-a-real-status" as RelayRunStatus });
    const stages = derivePipelineStages(run.status);
    expect(stages.every((stage) => stage.status === "pending")).toBe(true);

    render(<IdentityStrip run={run} currentStep="execute" />);

    const overview = positionOverview();
    const segments = within(overview).getAllByText(
      (_, element) => element?.hasAttribute("data-stage-status") ?? false,
    );
    for (const segment of segments) {
      expect(segment).toHaveAttribute("data-stage-status", "pending");
      expect(segment).not.toHaveAttribute("aria-current", "step");
    }
  });

  it("is the only pipeline-position group rendered (compact, not an independent full-width stepper) (Req 1.4)", () => {
    const run = makeRun();
    render(<IdentityStrip run={run} currentStep="execute" />);

    expect(screen.getAllByRole("group", { name: "Pipeline position" })).toHaveLength(1);
  });
});

// ------------------------------------------------------------
// Requirement 1.3 — no StatusBadge and no current-status prose
// ------------------------------------------------------------

describe("IdentityStrip — no StatusBadge or current-status prose rendered (Req 1.3)", () => {
  it("renders no element carrying a status-pill test id (StatusBadge's marker)", () => {
    const run = makeRun({ status: "executor_running" as RelayRunStatus });
    const { container } = render(<IdentityStrip run={run} currentStep="execute" />);

    expect(container.querySelector('[data-testid^="status-pill-"]')).toBeNull();
  });

  it("renders none of the current-status label text that StatusBadge would show for this run's status", () => {
    const run = makeRun({ status: "executor_running" as RelayRunStatus });
    const config = getRelayStatusConfig(run.status);

    render(<IdentityStrip run={run} currentStep="execute" />);

    // The status config label ("Running") is StatusBadge/current-status
    // prose, not one of the four stage labels — it must not appear at all.
    expect(screen.queryByText(config.label)).not.toBeInTheDocument();
  });

  it("renders no standalone status-prose sentence across a variety of statuses", () => {
    const statuses: RelayRunStatus[] = [
      "executor_blocked",
      "revision_required",
      "accepted",
      "completed",
    ];

    for (const status of statuses) {
      const run = makeRun({ status });
      const config = getRelayStatusConfig(status);
      const { unmount, container } = render(<IdentityStrip run={run} currentStep="execute" />);

      expect(container.querySelector('[data-testid^="status-pill-"]')).toBeNull();
      expect(screen.queryByText(config.label)).not.toBeInTheDocument();

      unmount();
    }
  });
});

// ------------------------------------------------------------
// Pipeline navigation (step-navigation-missing fix)
// ------------------------------------------------------------
//
// The original run-status-tracker-redesign made IdentityStrip's position
// overview purely decorative, which removed the Operator's ability to move
// between steps (forward, backward, or directly) from anywhere but a
// gated Next_Safe_Action button. The fix restores real cross-step
// navigation here: every one of the four pipeline stage segments is a
// clickable control that navigates to that step's run route, regardless of
// its completed/current/pending/attention status — mirroring the prior
// `RunStepper` component's behavior before the redesign removed it.

async function renderIdentityStripWithRouter(run: RelayRun, currentStep: RelayRunStep) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const stepRoutes = PIPELINE_STAGE_ORDER.map((step) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path: `/runs/$runId/${step}`,
      component: () => (
        <div>
          <IdentityStrip run={run} currentStep={step} />
          <span>{`${step}-destination`}</span>
        </div>
      ),
    }),
  );

  const router = createRouter({
    routeTree: rootRoute.addChildren(stepRoutes),
    history: createMemoryHistory({
      initialEntries: [`/runs/${run.id}/${currentStep}`],
    }),
    defaultPendingMinMs: 0,
  });
  await router.load();

  const result = render(<RouterProvider router={router} />);
  await screen.findByRole("group", { name: "Pipeline position" });
  return { ...result, router };
}

describe("IdentityStrip — Pipeline navigation: every stage is a clickable control that navigates to its route", () => {
  it("renders all four pipeline stages as buttons, each navigating to its own step route regardless of stage status", async () => {
    const user = userEvent.setup();
    const run = makeRun({ status: "executor_running" as RelayRunStatus });

    for (const targetStep of PIPELINE_STAGE_ORDER) {
      const { unmount, router } = await renderIdentityStripWithRouter(run, "execute");

      const overview = screen.getByRole("group", { name: "Pipeline position" });
      const stageButtons = within(overview).getAllByRole("button");
      expect(stageButtons).toHaveLength(4);

      const targetIndex = PIPELINE_STAGE_ORDER.indexOf(targetStep);
      await user.click(stageButtons[targetIndex]);

      expect(router.state.location.pathname).toBe(`/runs/${run.id}/${targetStep}`);
      expect(await screen.findByText(`${targetStep}-destination`)).toBeInTheDocument();

      unmount();
    }
  });

  it("navigates backward from a later step to an earlier step (e.g. execute -> intake)", async () => {
    const user = userEvent.setup();
    const run = makeRun({ status: "executor_running" as RelayRunStatus });
    const { router } = await renderIdentityStripWithRouter(run, "execute");

    const overview = screen.getByRole("group", { name: "Pipeline position" });
    const intakeButton = within(overview).getByRole("button", { name: /intake/i });
    await user.click(intakeButton);

    expect(router.state.location.pathname).toBe(`/runs/${run.id}/intake`);
    expect(await screen.findByText("intake-destination")).toBeInTheDocument();
  });

  it("navigates forward from an earlier step to a later step (e.g. execute -> audit)", async () => {
    const user = userEvent.setup();
    const run = makeRun({ status: "executor_running" as RelayRunStatus });
    const { router } = await renderIdentityStripWithRouter(run, "execute");

    const overview = screen.getByRole("group", { name: "Pipeline position" });
    const auditButton = within(overview).getByRole("button", { name: /audit/i });
    await user.click(auditButton);

    expect(router.state.location.pathname).toBe(`/runs/${run.id}/audit`);
    expect(await screen.findByText("audit-destination")).toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// Preservation property test — stage-status fidelity
// ------------------------------------------------------------
//
// Property 2: Preservation - the pipeline navigation fix does not change
// how stage status is derived or displayed: for randomly generated
// `run.status` values, `IdentityStrip`'s stage statuses still match
// `derivePipelineStages(run.status)` exactly, and every stage remains
// navigable via its own click handler (never non-interactive), independent
// of `run.status`.
//
// Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5

const ALL_CANONICAL_STATUSES_FOR_PRESERVATION: readonly RelayRunStatus[] = [
  "draft",
  "needs_cleanup",
  "intake_received",
  "intake_needs_review",
  "validated",
  "approved_for_prepare",
  "packet_validated",
  "packet_validation_failed",
  "repair_validated",
  "brief_ready_for_review",
  "approved_for_executor",
  "executor_dispatched",
  "executor_running",
  "executor_done",
  "executor_blocked",
  "agent_done",
  "agent_blocked",
  "agent_result_needs_review",
  "blocked",
  "audit_ready",
  "audit_ready_for_review",
  "revision_required",
  "accepted",
  "accepted_with_warnings",
  "validation_passed",
  "validation_failed_accepted",
  "validation_failed",
  "completed",
];

const runStatusArb: fc.Arbitrary<RelayRunStatus> = fc.constantFrom(
  ...ALL_CANONICAL_STATUSES_FOR_PRESERVATION,
);

describe("IdentityStrip — Preservation: stage-status fidelity, and every stage remains clickable (Property 2)", () => {
  it("renders exactly four clickable pipeline-stage buttons for any run.status", () => {
    fc.assert(
      fc.property(runStatusArb, (status) => {
        const run = makeRun({ status });
        const { unmount } = render(<IdentityStrip run={run} currentStep="execute" />);

        const overview = positionOverview();
        expect(within(overview).getAllByRole("button")).toHaveLength(4);

        unmount();
      }),
      { numRuns: 100 },
    );
  });

  it("renders stage statuses matching derivePipelineStages(run.status) exactly, for any run.status", () => {
    fc.assert(
      fc.property(runStatusArb, (status) => {
        const run = makeRun({ status });
        const stages = derivePipelineStages(run.status);

        const { unmount } = render(<IdentityStrip run={run} currentStep="execute" />);

        const overview = positionOverview();
        const segments = within(overview).getAllByText(
          (_, element) => element?.hasAttribute("data-stage-status") ?? false,
        );

        expect(segments.map((segment) => segment.getAttribute("data-stage-status"))).toEqual(
          stages.map((stage) => stage.status),
        );
        expect(segments.map((segment) => segment.textContent?.trim())).toEqual(
          stages.map((stage) => stage.label),
        );

        unmount();
      }),
      { numRuns: 100 },
    );
  });
});
