// Feature: frontend-shell-redesign, Property 12
//
// Property 12: Status-color total and deterministic mapping
// Validates: Requirements 7.4, 7.5
//
// For any status string — a canonical `RelayRunStatus` or an arbitrary string —
// `resolveStatusColorToken` is:
//   - TOTAL: it always returns one of the six valid `--relay-status-*` tokens
//     (Requirement 7.5 guarantees a defined default neutral token for any input).
//   - DETERMINISTIC: the same input always resolves to the same token across
//     repeated calls (Requirement 7.4 — an identical canonical `status` resolves
//     to the same status color token on every surface).
// Additionally, any unmapped / unknown (non-canonical) status resolves to the
// default `--relay-status-neutral` token (Requirement 7.5).

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { resolveStatusColorToken, type StatusColorToken } from "./statusColor";
import type { RelayRunStatus } from "@/features/relay-runs";

// ------------------------------------------------------------
// The closed set of six valid status color tokens (Requirement 7.5).
// Enumerated here so membership is asserted independently of the source module.
// ------------------------------------------------------------
const VALID_TOKENS: ReadonlySet<StatusColorToken> = new Set<StatusColorToken>([
  "--relay-status-running",
  "--relay-status-blocked",
  "--relay-status-complete",
  "--relay-status-audit",
  "--relay-status-validation",
  "--relay-status-neutral",
]);

const NEUTRAL_TOKEN: StatusColorToken = "--relay-status-neutral";

// ------------------------------------------------------------
// The canonical RelayRunStatus values (mirrors relay-runs/types.ts).
// ------------------------------------------------------------
const CANONICAL_STATUSES: readonly RelayRunStatus[] = [
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

const CANONICAL_STATUS_SET: ReadonlySet<string> = new Set(CANONICAL_STATUSES);

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

// Known canonical status values.
const canonicalStatusArb = fc.constantFrom(...CANONICAL_STATUSES);

// Arbitrary strings (includes empty, mixed casing, unicode, near-misses).
const arbitraryStringArb = fc.string({ minLength: 0, maxLength: 30 });

// Any status: canonical or arbitrary string.
const anyStatusArb = fc.oneof(canonicalStatusArb, arbitraryStringArb);

// Arbitrary strings that are NOT canonical status values (unmapped / unknown).
const nonCanonicalStringArb = arbitraryStringArb.filter(
  (s) => !CANONICAL_STATUS_SET.has(s),
);

describe("resolveStatusColorToken — Property 12: total and deterministic mapping", () => {
  it("is TOTAL: every input resolves to one of the six valid tokens", () => {
    fc.assert(
      fc.property(anyStatusArb, (status) => {
        const token = resolveStatusColorToken(status);
        expect(VALID_TOKENS.has(token)).toBe(true);
      }),
      { numRuns: 300 },
    );
  });

  it("is DETERMINISTIC: repeated calls with the same input yield the same token", () => {
    fc.assert(
      fc.property(anyStatusArb, (status) => {
        const first = resolveStatusColorToken(status);
        const second = resolveStatusColorToken(status);
        const third = resolveStatusColorToken(status);
        expect(second).toBe(first);
        expect(third).toBe(first);
      }),
      { numRuns: 300 },
    );
  });

  it("resolves unmapped / unknown (non-canonical) statuses to the neutral default token", () => {
    fc.assert(
      fc.property(nonCanonicalStringArb, (status) => {
        expect(resolveStatusColorToken(status)).toBe(NEUTRAL_TOKEN);
      }),
      { numRuns: 200 },
    );
  });

  it("resolves every canonical status to a valid token (total over the canonical enum)", () => {
    for (const status of CANONICAL_STATUSES) {
      const token = resolveStatusColorToken(status);
      expect(VALID_TOKENS.has(token)).toBe(true);
    }
  });
});
