// Feature: frontend-shell-redesign, Property 10
//
// Property 10: Pipeline derivation well-formedness and determinism
// Validates: Requirements 6.1, 6.2, 6.3, 6.7
//
// For any canonical `RelayRunStatus` (and any arbitrary unknown string),
// `derivePipelineStages`:
//   - Returns exactly four stages equal to `PIPELINE_STAGE_ORDER` in order,
//     each carrying the correct label (`PIPELINE_STAGE_LABELS`) and route
//     (`PIPELINE_STAGE_ROUTES`) (Requirement 6.1).
//   - Marks exactly one stage as the CURRENT POSITION, expressed as exactly one
//     stage whose status is in {"current", "attention"}. The implementation
//     marks the current-position stage "attention" (instead of "current") when
//     the run is in the closed attention set — this reconciles Property 10 with
//     the single-enum stage-status design. All stages before the current
//     position are "completed"; all stages after are "pending" (Requirement 6.2).
//   - Is deterministic: two calls with the same status are deep-equal
//     (Requirements 6.3, 6.7).
//   - Is total: arbitrary unknown / out-of-enum strings default to Intake as
//     the current position.

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import type { RelayRunStatus } from "@/features/relay-runs";
import {
  derivePipelineStages,
  PIPELINE_STAGE_LABELS,
  PIPELINE_STAGE_ORDER,
  PIPELINE_STAGE_ROUTES,
} from "./pipeline";

// ------------------------------------------------------------
// Canonical status contract
// ------------------------------------------------------------

// The full canonical `RelayRunStatus` contract, mirroring the exhaustive
// `STATUS_TO_STAGE` mapping in pipeline.ts. Declared with an explicit
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

// The set of statuses that map to the Intake stage (per STATUS_TO_STAGE). Used
// to assert the totality default: unknown strings behave like an Intake status.
const INTAKE_STATUSES: ReadonlySet<string> = new Set<string>([
  "draft",
  "needs_cleanup",
  "intake_received",
  "intake_needs_review",
  "validated",
]);

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const canonicalStatusArb: fc.Arbitrary<string> = fc.constantFrom(
  ...ALL_CANONICAL_STATUSES,
);

// Arbitrary unknown strings exercise the total-function / default-to-Intake
// behavior. Excludes any canonical status so the branch is unambiguous.
const canonicalSet: ReadonlySet<string> = new Set<string>(ALL_CANONICAL_STATUSES);
const unknownStringArb: fc.Arbitrary<string> = fc
  .string({ minLength: 0, maxLength: 24 })
  .filter((s) => !canonicalSet.has(s));

const anyStatusArb: fc.Arbitrary<string> = fc.oneof(
  canonicalStatusArb,
  unknownStringArb,
);

describe("derivePipelineStages — Property 10: derivation well-formedness and determinism", () => {
  it("returns exactly four stages in PIPELINE_STAGE_ORDER with correct labels and routes (Req 6.1)", () => {
    fc.assert(
      fc.property(anyStatusArb, (status) => {
        const stages = derivePipelineStages(status);

        expect(stages).toHaveLength(PIPELINE_STAGE_ORDER.length);

        stages.forEach((stage, index) => {
          const expectedStep = PIPELINE_STAGE_ORDER[index];
          expect(stage.step).toBe(expectedStep);
          expect(stage.label).toBe(PIPELINE_STAGE_LABELS[expectedStep]);
          expect(stage.to).toBe(PIPELINE_STAGE_ROUTES[expectedStep]);
        });
      }),
      { numRuns: 200 },
    );
  });

  it("marks exactly one current position with completed-before / pending-after ordering (Req 6.2)", () => {
    fc.assert(
      fc.property(anyStatusArb, (status) => {
        const stages = derivePipelineStages(status);

        // The current position is the single stage whose status is in
        // {"current", "attention"}.
        const currentPositionIndices = stages
          .map((s, i) => (s.status === "current" || s.status === "attention" ? i : -1))
          .filter((i) => i !== -1);

        expect(currentPositionIndices).toHaveLength(1);
        const currentIndex = currentPositionIndices[0];

        stages.forEach((stage, index) => {
          if (index < currentIndex) {
            expect(stage.status).toBe("completed");
          } else if (index > currentIndex) {
            expect(stage.status).toBe("pending");
          } else {
            expect(["current", "attention"]).toContain(stage.status);
          }
        });
      }),
      { numRuns: 200 },
    );
  });

  it("is deterministic — two calls with the same status are deep-equal (Req 6.3, 6.7)", () => {
    fc.assert(
      fc.property(anyStatusArb, (status) => {
        const first = derivePipelineStages(status);
        const second = derivePipelineStages(status);
        expect(first).toEqual(second);
      }),
      { numRuns: 200 },
    );
  });

  it("is total — arbitrary unknown strings default to Intake as the current position", () => {
    fc.assert(
      fc.property(unknownStringArb, (status) => {
        const stages = derivePipelineStages(status);

        const currentPositionIndex = stages.findIndex(
          (s) => s.status === "current" || s.status === "attention",
        );

        // Intake is the first stage; unknown input defaults there.
        expect(currentPositionIndex).toBe(0);
        expect(stages[0].step).toBe(PIPELINE_STAGE_ORDER[0]);
      }),
      { numRuns: 200 },
    );
  });

  // Cross-check the totality default against the known Intake status set: an
  // unknown string produces the same shape as any Intake-mapped canonical
  // status.
  it("unknown strings match the shape of an Intake-mapped canonical status", () => {
    const intakeSample = [...INTAKE_STATUSES][0];
    const reference = derivePipelineStages(intakeSample);
    const unknown = derivePipelineStages("totally-not-a-status-xyz");
    expect(unknown).toEqual(reference);
  });
});
