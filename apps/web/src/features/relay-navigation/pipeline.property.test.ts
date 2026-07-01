// Feature: frontend-shell-redesign, Property 11
//
// Property 11: Pipeline attention indicator
// Validates: Requirements 6.4
//
// For any canonical status, `derivePipelineStages` produces a stage with status
// "attention" IF AND ONLY IF the run's canonical status is in the closed
// blocked / awaiting-review set (`BLOCKED_STATUSES` ∪ `AWAITING_REVIEW_STATUSES`
// from statusSets.ts — the single authoritative source).
//
//   - When the status IS in the attention set: exactly the current-position
//     stage is marked "attention" and NO stage is marked "current".
//   - When the status is NOT in the attention set: NO stage is marked
//     "attention" and exactly one stage is marked "current".

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import type { RelayRunStatus } from "@/features/relay-runs";
import { derivePipelineStages } from "./pipeline";
import { AWAITING_REVIEW_STATUSES, BLOCKED_STATUSES } from "./statusSets";

// ------------------------------------------------------------
// Authoritative attention set (imported, never hardcoded)
// ------------------------------------------------------------

// The single source of truth for attention classification. Importing these
// constants ensures the test can never drift from the implementation's set.
const ATTENTION_STATUSES: readonly string[] = [
  ...BLOCKED_STATUSES,
  ...AWAITING_REVIEW_STATUSES,
];
const ATTENTION_SET: ReadonlySet<string> = new Set(ATTENTION_STATUSES);

// ------------------------------------------------------------
// Canonical status contract
// ------------------------------------------------------------

// The full canonical `RelayRunStatus` contract. Declared with an explicit
// `RelayRunStatus[]` annotation so the compiler flags drift if the canonical
// enum changes. This mirrors the exhaustive `STATUS_TO_STAGE` mapping in
// pipeline.ts.
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
  "audit_ready",
  "audit_ready_for_review",
  "revision_required",
  "accepted",
  "accepted_with_warnings",
  "validation_passed",
  "validation_failed_accepted",
  "validation_failed",
  "completed",
  "blocked",
];

// Canonical statuses that are NOT in the attention set.
const NON_ATTENTION_CANONICAL: readonly string[] = ALL_CANONICAL_STATUSES.filter(
  (status) => !ATTENTION_SET.has(status),
);

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

// (a) Statuses drawn from the closed attention set.
const attentionStatusArb: fc.Arbitrary<string> = fc.constantFrom(...ATTENTION_STATUSES);

// (b) Non-attention canonical statuses.
const nonAttentionCanonicalArb: fc.Arbitrary<string> = fc.constantFrom(
  ...NON_ATTENTION_CANONICAL,
);

// (b) Arbitrary unknown strings that are guaranteed NOT to be in the attention
// set (unknown / out-of-enum input must never surface an attention indicator).
const unknownStringArb: fc.Arbitrary<string> = fc
  .string({ minLength: 0, maxLength: 24 })
  .filter((s) => !ATTENTION_SET.has(s));

// The full non-attention input space: non-attention canonical statuses plus
// arbitrary unknown strings.
const nonAttentionStatusArb: fc.Arbitrary<string> = fc.oneof(
  nonAttentionCanonicalArb,
  unknownStringArb,
);

describe("derivePipelineStages — Property 11: pipeline attention indicator", () => {
  it("marks exactly the current-position stage 'attention' (and none 'current') for attention statuses", () => {
    fc.assert(
      fc.property(attentionStatusArb, (status) => {
        const stages = derivePipelineStages(status);

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

  it("marks no stage 'attention' and exactly one 'current' for non-attention statuses", () => {
    fc.assert(
      fc.property(nonAttentionStatusArb, (status) => {
        const stages = derivePipelineStages(status);

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
    // Draw across the union of attention and non-attention canonical statuses
    // plus unknown strings to exercise the biconditional directly.
    const anyStatusArb = fc.oneof(attentionStatusArb, nonAttentionStatusArb);

    fc.assert(
      fc.property(anyStatusArb, (status) => {
        const stages = derivePipelineStages(status);
        const hasAttention = stages.some((s) => s.status === "attention");

        expect(hasAttention).toBe(ATTENTION_SET.has(status));
      }),
      { numRuns: 200 },
    );
  });
});
