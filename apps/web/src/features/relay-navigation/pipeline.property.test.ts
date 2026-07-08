// Feature: frontend-shell-redesign, Property 11
//
// Property 11: Pipeline attention indicator
// Validates: Requirements 6.4
//
// For any canonical status, `derivePipelineStages` produces a stage with status
// "attention" IF AND ONLY IF the run's canonical status is in the closed
// blocked / awaiting-review set (`BLOCKED_STATUSES` ∪ `AWAITING_REVIEW_STATUSES`
// from statusSets.ts — the single authoritative source).

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import type { WorkflowRunStatus } from "@/features/relay-runs";
import { derivePipelineStages, resolveWorkflowStage } from "./pipeline";
import { AWAITING_REVIEW_STATUSES, BLOCKED_STATUSES } from "./statusSets";

const ATTENTION_STATUSES: readonly string[] = [
  ...BLOCKED_STATUSES,
  ...AWAITING_REVIEW_STATUSES,
];
const ATTENTION_SET: ReadonlySet<string> = new Set(ATTENTION_STATUSES);

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

const NON_ATTENTION_CANONICAL: readonly WorkflowRunStatus[] = ALL_CANONICAL_STATUSES.filter(
  (status) => !ATTENTION_SET.has(status),
);

const attentionStatusArb: fc.Arbitrary<WorkflowRunStatus> = fc.constantFrom(
  ...(ATTENTION_STATUSES as WorkflowRunStatus[]),
);

const nonAttentionCanonicalArb: fc.Arbitrary<WorkflowRunStatus> = fc.constantFrom(
  ...NON_ATTENTION_CANONICAL,
);

describe("derivePipelineStages — Property 11: pipeline attention indicator", () => {
  it("marks exactly the current-position stage 'attention' (and none 'current') for attention statuses", () => {
    fc.assert(
      fc.property(attentionStatusArb, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(durableStage, durableStage, status);

        const attentionCount = stages.filter((s) => s.status === "attention").length;
        const currentCount = stages.filter((s) => s.status === "current").length;

        // Exactly one stage carries the attention indicator ...
        expect(attentionCount).toBe(1);
        // ... and no stage is marked "current" (the attention marker replaces
        // the current marker on the affected stage).
        expect(currentCount).toBe(0);
      }),
      { numRuns: 200 },
    );
  });

  it("marks no stage 'attention' and exactly one 'current' for non-attention canonical statuses", () => {
    fc.assert(
      fc.property(nonAttentionCanonicalArb, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(durableStage, durableStage, status);

        const attentionCount = stages.filter((s) => s.status === "attention").length;
        const currentCount = stages.filter((s) => s.status === "current").length;

        // No attention indicator for statuses outside the closed set ...
        expect(attentionCount).toBe(0);
        // ... and exactly one stage marks the current position.
        expect(currentCount).toBe(1);
      }),
      { numRuns: 200 },
    );
  });

  it("surfaces attention IF AND ONLY IF the status is in the closed attention set", () => {
    const anyStatusArb = fc.oneof(attentionStatusArb, nonAttentionCanonicalArb);

    fc.assert(
      fc.property(anyStatusArb, (status) => {
        const durableStage = resolveWorkflowStage(status);
        const stages = derivePipelineStages(durableStage, durableStage, status);
        const hasAttention = stages.some((s) => s.status === "attention");

        expect(hasAttention).toBe(ATTENTION_SET.has(status));
      }),
      { numRuns: 200 },
    );
  });
});
