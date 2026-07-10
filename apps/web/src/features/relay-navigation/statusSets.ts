// ============================================================
// Relay Navigation — Canonical attention status sets
// ============================================================
//
// Single shared source for attention classification. `BLOCKED_STATUSES` and
// `AWAITING_REVIEW_STATUSES` drive BOTH the Home_Overview attention section
// (Requirement 3.2) and the Run_Pipeline attention indicator (Requirement 6.4).
//
// This set is a UI/view-level classification only (Requirement 3.11) — it is
// NOT a new canonical status taxonomy. It is CLOSED and AUTHORITATIVE: it MUST
// NOT be expanded to additional canonical `status` values without an explicit
// update to Requirement 3.2.
//
// An existing broader helper such as `getRelayAttentionReason` may be reused
// only to produce a display label or icon (Requirement 3.12); it MUST NOT be
// used to add Runs to the Home_Overview attention section or to add stages to
// the Run_Pipeline attention classification. These constants remain the sole
// authority for both.
//
// Values are drawn from the canonical `WorkflowRunStatus` contract in the
// relay-runs feature (types.ts). This module does not redefine
// that contract.

export const BLOCKED_STATUSES = ["execution_failed", "cancelled"] as const;

export const AWAITING_REVIEW_STATUSES = [
  "audit_ready",
  "needs_revision",
] as const;

export type BlockedStatus = (typeof BLOCKED_STATUSES)[number];
export type AwaitingReviewStatus = (typeof AWAITING_REVIEW_STATUSES)[number];

/** Union of all canonical statuses treated as "attention" at the view level. */
export type AttentionStatus = BlockedStatus | AwaitingReviewStatus;
