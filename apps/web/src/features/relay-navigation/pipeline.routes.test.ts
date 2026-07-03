// Feature: frontend-shell-redesign, Task 10.4
//
// Unit tests for stage route mapping and non-navigable no-op.
// Validates: Requirements 6.5, 6.6
//
// Req 6.5: Each Run_Pipeline_Stage maps to its documented run-scoped route
//   (Intake -> /runs/$runId/intake, Compile/Render -> /runs/$runId/prepare,
//    Execute -> /runs/$runId/execute, Audit -> /runs/$runId/audit). This is
//   verified against `PIPELINE_STAGE_ROUTES` directly and against the `to`
//   field carried by every `PipelineStageView` returned by
//   `derivePipelineStages` across the full canonical status contract.
//
// Req 6.6: Selecting a Run_Pipeline_Stage that is not currently navigable is a
//   no-op that preserves the current route. `derivePipelineStages` marks every
//   stage `navigable: true`, so the RunStepper guard `if (!stage.navigable)
//   return` is exercised here against a small pure reproduction of that guard
//   to document/verify the no-op contract without a DOM harness.

import { describe, expect, it } from "vitest";

import type { RelayRunStatus, RelayRunStep } from "@/features/relay-runs";
import type { PipelineStageView } from "./types";
import {
  derivePipelineStages,
  PIPELINE_STAGE_ORDER,
  PIPELINE_STAGE_ROUTES,
} from "./pipeline";

// ------------------------------------------------------------
// Documented stage -> route contract (Requirement 6.5)
// ------------------------------------------------------------

// Independent copy of the documented mapping. Kept separate from the SUT so the
// assertion checks the implementation against the requirement text rather than
// against itself.
const DOCUMENTED_STAGE_ROUTES: Record<RelayRunStep, string> = {
  intake: "/runs/$runId/intake",
  prepare: "/runs/$runId/prepare",
  execute: "/runs/$runId/execute",
  audit: "/runs/$runId/audit",
};

// Full canonical `RelayRunStatus` contract. Declared with an explicit
// `RelayRunStatus[]` annotation so the compiler flags drift if the canonical
// enum changes.
const ALL_CANONICAL_STATUSES: readonly RelayRunStatus[] = [
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

// ------------------------------------------------------------
// Pure reproduction of the RunStepper navigation guard (Requirement 6.6)
// ------------------------------------------------------------
//
// RunStepper.tsx inlines the selection handler as:
//   if (!stage.navigable) return
//   router.navigate({ to: STAGE_ROUTES[stage.step], params: { runId } })
//
// `resolveStageSelectionRoute` is a pure model of that contract: given the
// currently displayed route and a selected stage, it returns the route that
// should be active AFTER selection. A non-navigable stage yields the current
// route unchanged (a no-op); a navigable stage yields the stage's route.
function resolveStageSelectionRoute(
  currentRoute: string,
  stage: Pick<PipelineStageView, "to" | "navigable">,
): string {
  if (!stage.navigable) return currentRoute;
  return stage.to;
}

describe("PIPELINE_STAGE_ROUTES — documented stage route mapping (Req 6.5)", () => {
  it("maps each stage type to its documented run-scoped route", () => {
    expect(PIPELINE_STAGE_ROUTES.intake).toBe("/runs/$runId/intake");
    expect(PIPELINE_STAGE_ROUTES.prepare).toBe("/runs/$runId/prepare");
    expect(PIPELINE_STAGE_ROUTES.execute).toBe("/runs/$runId/execute");
    expect(PIPELINE_STAGE_ROUTES.audit).toBe("/runs/$runId/audit");
  });

  it("covers exactly the four pipeline stages with no extra or missing routes", () => {
    expect(Object.keys(PIPELINE_STAGE_ROUTES).sort()).toEqual(
      [...PIPELINE_STAGE_ORDER].sort(),
    );
  });

  it("matches the independently documented mapping for every stage", () => {
    for (const step of PIPELINE_STAGE_ORDER) {
      expect(PIPELINE_STAGE_ROUTES[step]).toBe(DOCUMENTED_STAGE_ROUTES[step]);
    }
  });
});

describe("derivePipelineStages — stage views carry the documented route (Req 6.5)", () => {
  it("assigns each derived stage the documented `to` route for its step", () => {
    // Exhaustive across the full canonical status contract: regardless of which
    // stage is current/attention, each stage's route is fixed by its step.
    for (const status of ALL_CANONICAL_STATUSES) {
      const stages = derivePipelineStages(status);
      for (const stage of stages) {
        expect(stage.to).toBe(DOCUMENTED_STAGE_ROUTES[stage.step]);
      }
    }
  });

  it("emits the four stages in pipeline order with their documented routes", () => {
    const stages = derivePipelineStages("draft");
    expect(stages.map((s) => s.step)).toEqual([
      "intake",
      "prepare",
      "execute",
      "audit",
    ]);
    expect(stages.map((s) => s.to)).toEqual([
      "/runs/$runId/intake",
      "/runs/$runId/prepare",
      "/runs/$runId/execute",
      "/runs/$runId/audit",
    ]);
  });
});

describe("stage selection — non-navigable no-op preserves the route (Req 6.6)", () => {
  const currentRoute = "/runs/$runId/execute";

  it("keeps the current route unchanged when a non-navigable stage is selected", () => {
    const nonNavigable: Pick<PipelineStageView, "to" | "navigable"> = {
      to: "/runs/$runId/audit",
      navigable: false,
    };
    expect(resolveStageSelectionRoute(currentRoute, nonNavigable)).toBe(
      currentRoute,
    );
  });

  it("navigates to the stage route when a navigable stage is selected", () => {
    const navigable: Pick<PipelineStageView, "to" | "navigable"> = {
      to: "/runs/$runId/audit",
      navigable: true,
    };
    expect(resolveStageSelectionRoute(currentRoute, navigable)).toBe(
      "/runs/$runId/audit",
    );
  });

  it("treats every derived stage as navigable, so selection routes to the stage's own route", () => {
    // `derivePipelineStages` currently marks all four stages navigable; confirm
    // that contract and that a navigable selection resolves to the stage route
    // rather than a no-op, across the full canonical status contract.
    for (const status of ALL_CANONICAL_STATUSES) {
      for (const stage of derivePipelineStages(status)) {
        expect(stage.navigable).toBe(true);
        expect(resolveStageSelectionRoute(currentRoute, stage)).toBe(stage.to);
      }
    }
  });
});
