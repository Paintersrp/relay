// @vitest-environment jsdom

// Feature: run-status-tracker-redesign, Property 1: Single status source
//
// For an arbitrarily generated `RelayRunStep` + matching Visual_State_Module
// display-state + `StatusTextContext` (blocker/revision/warning counts) fed
// into the real `deriveCurrentStatusText`, plus an arbitrarily generated
// `RelayRunEvent[]` fed into the real `deriveProgressionLog` (including
// "overlap" scenarios where an event's message is deliberately drawn from
// the same closed pool of fixed per-stage headline sentences — excluding
// the one sentence that is the *current* headline, so the scenario stays
// consistent with Requirement 4.5 while still exercising headline-shaped
// text elsewhere in the log), rendering `RunStatusTrackerLayout` with the
// resulting `CurrentStatusView`/`ProgressionEntry[]` must produce the exact
// `headline` text in exactly one place in the rendered output, and no
// `ProgressionEntry.label` may exactly equal that headline.
//
// Validates: Requirements 1.3, 2.1, 2.2, 2.8, 2.9, 2.11, 4.5

import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { RunStatusTrackerLayout } from "./RunStatusTrackerLayout";
import {
  deriveCurrentStatusText,
  type IntakeBlockedState,
  type StatusTextContext,
} from "@/features/relay-runs/deriveCurrentStatusText";
import { deriveProgressionLog } from "@/features/relay-runs/deriveProgressionLog";
import type { CompileRenderDisplayState } from "@/routes/runs/$runId/runCompileRenderVisualState";
import type { ExecuteDisplayState } from "@/routes/runs/$runId/runExecuteVisualState";
import type { AuditDisplayState } from "@/routes/runs/$runId/runAuditVisualState";
import type {
  RelayRun,
  RelayRunEvent,
  RelayRunStep,
} from "@/features/relay-runs/runStatusTrackerViews";
import type { RelayRunEventKind } from "@/features/relay-runs/types";

// ------------------------------------------------------------
// Fixed per-stage headline sentences (from deriveCurrentStatusText's
// per-state switch statements / design.md's Per-Stage Current Status
// table). Used to build "overlap" scenarios: progression event messages
// deliberately drawn from this same closed pool of status-sentence-shaped
// text (excluding the current scenario's own headline) to prove the
// exactly-once assertion below is a meaningful check, not one that
// vacuously passes just because random strings never happen to look like
// a status sentence.
// ------------------------------------------------------------

const ALL_FIXED_HEADLINES: string[] = [
  // intake
  "Waiting for you to review the incoming handoff.",
  "Intake is blocked — review before this run can proceed.",
  // prepare
  "Ready to compile the packet.",
  "Compiling the packet.",
  "Rendering the executor brief.",
  "Attempting a repair.",
  "Recording your approval.",
  "Packet validation failed — review before continuing.",
  "Brief is ready for your approval.",
  "Approved. Ready to move to Execute.",
  // execute
  "Ready to start the executor.",
  "Executor is running.",
  "Executor finished — running validation.",
  "Execution complete. Ready for audit.",
  "Executor is blocked — review before retrying.",
  "Execute is not available yet.",
  // audit
  "Ready to generate the audit packet.",
  "Audit packet is ready for your decision.",
  "Revision requested — update and regenerate.",
  "Approved. Ready to close the run.",
  "Approved with warnings. Ready to close the run.",
  "Run closed.",
  "Audit is blocked — review before continuing.",
];

// ------------------------------------------------------------
// Per-step display-state arbitraries and a step-aware dispatcher that
// calls the real (overloaded) `deriveCurrentStatusText` without losing
// type-safety.
// ------------------------------------------------------------

const INTAKE_STATES: IntakeBlockedState[] = ["blocked", "not_blocked"];

const PREPARE_STATES: CompileRenderDisplayState[] = [
  "blocked",
  "ready_to_compile",
  "compiling",
  "packet_invalid",
  "repairing",
  "repair_validated",
  "packet_validated",
  "rendering_brief",
  "brief_ready",
  "approving",
  "approved",
];

const EXECUTE_STATES: ExecuteDisplayState[] = [
  "blocked",
  "ready",
  "running",
  "validating",
  "complete",
  "failed",
];

const AUDIT_STATES: AuditDisplayState[] = [
  "blocked",
  "validation_required",
  "validation_running",
  "validation_failed",
  "validation_accepted",
  "validation_passed",
  "audit_candidate",
  "audit_candidate_with_executor_blocker",
  "generating_audit",
  "audit_ready",
  "submitting_manual",
  "approving",
  "accepted",
  "accepted_with_warnings",
  "revision_required",
  "preparing_commit_message",
  "closing",
  "completed",
];

interface StepScenario {
  step: RelayRunStep;
  displayState:
    | IntakeBlockedState
    | CompileRenderDisplayState
    | ExecuteDisplayState
    | AuditDisplayState;
}

const stepScenarioArb: fc.Arbitrary<StepScenario> = fc.oneof(
  fc.constantFrom(...INTAKE_STATES).map((displayState) => ({
    step: "intake" as const,
    displayState,
  })),
  fc.constantFrom(...PREPARE_STATES).map((displayState) => ({
    step: "prepare" as const,
    displayState,
  })),
  fc.constantFrom(...EXECUTE_STATES).map((displayState) => ({
    step: "execute" as const,
    displayState,
  })),
  fc.constantFrom(...AUDIT_STATES).map((displayState) => ({
    step: "audit" as const,
    displayState,
  })),
);

function computeCurrentStatus(scenario: StepScenario, context: StatusTextContext) {
  switch (scenario.step) {
    case "intake":
      return deriveCurrentStatusText(
        "intake",
        scenario.displayState as IntakeBlockedState,
        context,
      );
    case "prepare":
      return deriveCurrentStatusText(
        "prepare",
        scenario.displayState as CompileRenderDisplayState,
        context,
      );
    case "execute":
      return deriveCurrentStatusText(
        "execute",
        scenario.displayState as ExecuteDisplayState,
        context,
      );
    case "audit":
      return deriveCurrentStatusText(
        "audit",
        scenario.displayState as AuditDisplayState,
        context,
      );
  }
}

// ------------------------------------------------------------
// StatusTextContext arbitrary — blocker/revision/warning counts only ever
// influence `detail`, never `headline`'s classification (Requirement 2.10),
// but are varied here for realistic "blocker combinations" coverage per the
// task description.
// ------------------------------------------------------------

const contextArb: fc.Arbitrary<StatusTextContext> = fc.record({
  updatedAt: fc
    .date({
      min: new Date("2020-01-01T00:00:00.000Z"),
      max: new Date("2030-01-01T00:00:00.000Z"),
      noInvalidDate: true,
    })
    .map((d) => d.toISOString()),
  blockerCount: fc.option(fc.nat({ max: 5 }), { nil: undefined }),
  revisionRequirementCount: fc.option(fc.nat({ max: 5 }), { nil: undefined }),
  warningCount: fc.option(fc.nat({ max: 5 }), { nil: undefined }),
});

// ------------------------------------------------------------
// RelayRunEvent[] arbitrary — event messages are either plain random text
// or (deliberately, for "overlap" coverage) one of the *other* fixed
// headline sentences (never the current scenario's own headline, which
// would violate Requirement 4.5 by construction rather than by chance).
// ------------------------------------------------------------

const EVENT_KINDS: RelayRunEventKind[] = [
  "log",
  "status_change",
  "artifact_created",
  "validation_run",
  "step_transition",
];

const isoTimestampArb: fc.Arbitrary<string> = fc
  .date({
    min: new Date("2020-01-01T00:00:00.000Z"),
    max: new Date("2030-01-01T00:00:00.000Z"),
    noInvalidDate: true,
  })
  .map((d) => d.toISOString());

function messageArb(currentHeadline: string): fc.Arbitrary<string> {
  const otherHeadlines = ALL_FIXED_HEADLINES.filter((h) => h !== currentHeadline);
  return fc.oneof(
    fc.string({ minLength: 1, maxLength: 40 }),
    fc.constantFrom(...otherHeadlines),
  );
}

function eventArb(id: string, currentHeadline: string): fc.Arbitrary<RelayRunEvent> {
  return fc.record({
    id: fc.constant(id),
    runId: fc.constant("run-tracker-1"),
    kind: fc.constantFrom(...EVENT_KINDS),
    message: messageArb(currentHeadline),
    createdAt: isoTimestampArb,
  });
}

function eventsArb(currentHeadline: string): fc.Arbitrary<RelayRunEvent[]> {
  return fc.integer({ min: 0, max: 12 }).chain((length) =>
    fc.tuple(
      ...Array.from({ length }, (_, index) =>
        eventArb(`event-${index}`, currentHeadline),
      ),
    ),
  );
}

// ------------------------------------------------------------
// Run fixture
// ------------------------------------------------------------

const BASE_RUN: RelayRun = {
  id: "run-tracker-property-1",
  title: "Ship the tracker redesign",
  name: "Ship the tracker redesign",
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
} as unknown as RelayRun;

// ------------------------------------------------------------
// Property
// ------------------------------------------------------------

// Combined arbitrary: (scenario, context) determine `headline`, which then
// constrains the `events` arbitrary (excluding the current headline from
// the "overlap" pool per Requirement 4.5). Chained into a single arbitrary
// so `fc.assert`/`fc.property` receives one generator, as fast-check
// requires (nesting `fc.property` calls is not itself a valid property).
const scenarioWithEventsArb = fc
  .tuple(stepScenarioArb, contextArb)
  .chain(([scenario, context]) => {
    const currentStatus = computeCurrentStatus(scenario, context);
    return eventsArb(currentStatus.headline).map((events) => ({
      scenario,
      currentStatus,
      events,
    }));
  });

describe("RunStatusTrackerLayout — Property 1: Single status source", () => {
  it("renders headline exactly once and never lets a progression entry duplicate it (Req 1.3, 2.1, 2.2, 2.8, 2.9, 2.11, 4.5)", () => {
    fc.assert(
      fc.property(scenarioWithEventsArb, ({ scenario, currentStatus, events }) => {
        const headline = currentStatus.headline;
        const progression = deriveProgressionLog(events);

        // Precondition established by construction (Requirement 4.5): no
        // generated progression entry's label exactly equals the current
        // headline.
        for (const entry of progression) {
          expect(entry.label).not.toBe(headline);
        }

        cleanup();
        render(
          <RunStatusTrackerLayout
            run={{ ...BASE_RUN, activeStep: scenario.step }}
            currentStep={scenario.step}
            currentStatus={currentStatus}
            progression={progression}
            detailSections={[]}
          />,
        );

        try {
          // Requirement 2.1/2.11/1.3: the exact headline text appears in
          // exactly one place in the rendered output — only inside
          // CurrentStatusBlock, never repeated inside IdentityStrip,
          // NextActionArea, or ProgressionRail.
          const matches = screen.queryAllByText(headline, { exact: true });
          expect(matches).toHaveLength(1);
          expect(
            matches[0].closest('[data-testid="current-status-block"]'),
          ).not.toBeNull();
        } finally {
          cleanup();
        }
      }),
      { numRuns: 100 },
    );
  });
});
