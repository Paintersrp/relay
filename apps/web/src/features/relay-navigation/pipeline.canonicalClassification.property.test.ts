// Feature: run-workbench-refinement, Property 3: For any canonical RelayRunStatus,
// the status-to-step mapping consumed by the Run_Stepper (derivePipelineStages)
// returns exactly the four canonical steps in intake, prepare, execute, audit
// order, marks exactly one step as the active position (current or attention),
// classifies every step ordered before the active one as completed and every
// step ordered after it as upcoming, and assigns each step exactly one of
// those three mutually exclusive states.
//
// Validates: Requirements 2.1, 2.2, 2.3
//
// This property targets canonical statuses specifically (statuses that map to
// one of the four RELAY_RUN_STEPS keys via the fixed status-to-step mapping in
// pipeline.ts). The non-canonical / fallback behavior (Requirement 2.4) is
// covered separately by pipeline.derive.property.test.ts and is out of scope
// for this property.

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { RELAY_RUN_STEPS, type RelayRunStatus } from "@/features/relay-runs";
import { derivePipelineStages, PIPELINE_STAGE_ORDER } from "./pipeline";

// ------------------------------------------------------------
// Canonical status contract
// ------------------------------------------------------------
//
// The full canonical `RelayRunStatus` contract, mirroring the exhaustive
// `STATUS_TO_STAGE` mapping in pipeline.ts. Every one of these statuses maps
// to exactly one of the four canonical step keys, so every value drawn from
// this arbitrary exercises the "active position" branch, never the
// non-canonical fallback branch.
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

const canonicalStatusArb: fc.Arbitrary<RelayRunStatus> = fc.constantFrom(
  ...ALL_CANONICAL_STATUSES,
);

describe("derivePipelineStages — Property 3: pipeline classification over canonical statuses", () => {
  it("returns exactly the four canonical steps in intake, prepare, execute, audit order", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const stages = derivePipelineStages(status);

        expect(stages).toHaveLength(4);
        expect(stages.map((s) => s.step)).toEqual(["intake", "prepare", "execute", "audit"]);
        expect(stages.map((s) => s.step)).toEqual(PIPELINE_STAGE_ORDER);
        expect(stages.map((s) => s.step)).toEqual(RELAY_RUN_STEPS.map((s) => s.key));
      }),
      { numRuns: 100 },
    );
  });

  it("marks exactly one step as the active position (current or attention)", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const stages = derivePipelineStages(status);

        const activeIndices = stages
          .map((stage, index) => ({ stage, index }))
          .filter(({ stage }) => stage.status === "current" || stage.status === "attention")
          .map(({ index }) => index);

        expect(activeIndices).toHaveLength(1);
      }),
      { numRuns: 100 },
    );
  });

  it("classifies steps before the active position as completed and steps after it as upcoming (pending)", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const stages = derivePipelineStages(status);

        const activeIndex = stages.findIndex(
          (stage) => stage.status === "current" || stage.status === "attention",
        );
        expect(activeIndex).toBeGreaterThanOrEqual(0);

        stages.forEach((stage, index) => {
          if (index < activeIndex) {
            expect(stage.status).toBe("completed");
          } else if (index > activeIndex) {
            expect(stage.status).toBe("pending");
          } else {
            expect(["current", "attention"]).toContain(stage.status);
          }
        });
      }),
      { numRuns: 100 },
    );
  });

  it("assigns each step exactly one of the three mutually exclusive states", () => {
    fc.assert(
      fc.property(canonicalStatusArb, (status) => {
        const stages = derivePipelineStages(status);

        stages.forEach((stage) => {
          // "current" and "attention" are both classified as the single
          // "active position" state per Property 3; together with
          // "completed" and "pending" (upcoming) this closes the set of
          // valid statuses to exactly three mutually exclusive buckets:
          // completed | active (current/attention) | upcoming (pending).
          expect(["completed", "current", "attention", "pending"]).toContain(stage.status);

          const bucket =
            stage.status === "completed"
              ? "completed"
              : stage.status === "pending"
                ? "upcoming"
                : "active";
          expect(["completed", "active", "upcoming"]).toContain(bucket);
        });

        // Exactly one step per bucket-count invariant: every step belongs to
        // exactly one of the three buckets (trivially true given the status
        // field is a single scalar), and the three buckets partition all
        // four steps with no overlap and no omission.
        const completedCount = stages.filter((s) => s.status === "completed").length;
        const activeCount = stages.filter((s) => s.status === "current" || s.status === "attention")
          .length;
        const upcomingCount = stages.filter((s) => s.status === "pending").length;

        expect(completedCount + activeCount + upcomingCount).toBe(4);
        expect(activeCount).toBe(1);
      }),
      { numRuns: 100 },
    );
  });
});
