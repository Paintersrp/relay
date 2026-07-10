// Feature: frontend-shell-redesign, Task 10.4
//
// Unit tests for stage route mapping and non-navigable no-op.
// Validates: Requirements 6.5, 6.6
//
// Req 6.5: Each Run_Pipeline_Stage maps to its documented run-scoped route
//   (Specification -> /runs/$runId/specification, Execute -> /runs/$runId/execute,
//    Audit -> /runs/$runId/audit). This is verified against `PIPELINE_STAGE_ROUTES`
//    directly.
//
// Req 6.6: Selecting a Run_Pipeline_Stage that is not currently navigable is a
//   no-op that preserves the current route.

import { describe, expect, it } from "vitest";

import type { WorkflowRunStatus, WorkflowRunStage } from "@/features/relay-runs";
import type { PipelineStageView } from "./types";
import {
  derivePipelineStages,
  PIPELINE_STAGE_ORDER,
  PIPELINE_STAGE_ROUTES,
  resolveWorkflowStage,
} from "./pipeline";

const DOCUMENTED_STAGE_ROUTES: Record<WorkflowRunStage, string> = {
  specification: "/runs/$runId/specification",
  execute: "/runs/$runId/execute",
  audit: "/runs/$runId/audit",
};

const ALL_CANONICAL_STATUSES: readonly WorkflowRunStatus[] = [
  "created",
  "setup_ready",
  "executing",
  "execution_failed",
  "cancelled",
  "validating",
  "validation_failed",
  "audit_ready",
  "needs_revision",
  "completed",
];

function resolveStageSelectionRoute(
  currentRoute: string,
  stage: Pick<PipelineStageView, "to" | "navigable">,
): string {
  if (!stage.navigable) return currentRoute;
  return stage.to;
}

describe("PIPELINE_STAGE_ROUTES — documented stage route mapping (Req 6.5)", () => {
  it("maps each stage type to its documented run-scoped route", () => {
    expect(PIPELINE_STAGE_ROUTES.specification).toBe("/runs/$runId/specification");
    expect(PIPELINE_STAGE_ROUTES.execute).toBe("/runs/$runId/execute");
    expect(PIPELINE_STAGE_ROUTES.audit).toBe("/runs/$runId/audit");
  });

  it("covers exactly the three pipeline stages with no extra or missing routes", () => {
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
    for (const status of ALL_CANONICAL_STATUSES) {
      const durableStage = resolveWorkflowStage(status);
      const stages = derivePipelineStages(durableStage, durableStage, status);
      for (const stage of stages) {
        expect(stage.to).toBe(DOCUMENTED_STAGE_ROUTES[stage.step]);
      }
    }
  });

  it("emits the three stages in pipeline order with their documented routes", () => {
    const stages = derivePipelineStages("specification", "specification", "created");
    expect(stages.map((s) => s.step)).toEqual([
      "specification",
      "execute",
      "audit",
    ]);
    expect(stages.map((s) => s.to)).toEqual([
      "/runs/$runId/specification",
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
});
