/**
 * relayPlanReviewWorkflow.ts
 *
 * Pure review-gate derivation helpers for the plan review workbench.
 *
 * All functions are pure (no side effects, no API calls) and derive UI labels,
 * badge variants, and allowed action checks from the backend reviewGate payload.
 * Frontend must not re-implement approval-gate policy; use these helpers to
 * surface backend-derived allowedActions.
 */

import type {
  IntentDriftReviewAPI,
  PlanAttemptReviewGateAPI,
} from "@/features/relay-plans";

// ─── Drift Finding Shapes ─────────────────────────────────────────────────────

export interface DriftFinding {
  findingId?: string;
  severity?: string;
  summary: string;
  evidence: string[];
  suggestedResolution?: string;
}

/**
 * Parse the findingsJson field from an IntentDriftReviewAPI into a typed array.
 * Returns an empty array on malformed JSON or missing field.
 */
export function parseDriftFindings(findingsJson: string): DriftFinding[] {
  if (!findingsJson || findingsJson.trim() === "") return [];

  try {
    const parsed = JSON.parse(findingsJson) as unknown;

    if (!Array.isArray(parsed)) return [];

    return parsed
      .filter((item): item is Record<string, unknown> => {
        return typeof item === "object" && item !== null && !Array.isArray(item);
      })
      .map((item) => ({
        findingId: typeof item.findingId === "string" ? item.findingId : undefined,
        severity: typeof item.severity === "string" ? item.severity : undefined,
        summary: typeof item.summary === "string" ? item.summary : "(no summary)",
        evidence: Array.isArray(item.evidence)
          ? item.evidence.filter((e): e is string => typeof e === "string")
          : [],
        suggestedResolution:
          typeof item.suggestedResolution === "string"
            ? item.suggestedResolution
            : undefined,
      }));
  } catch {
    return [];
  }
}

// ─── Badge Variants ───────────────────────────────────────────────────────────

type BadgeVariant = "success" | "warning" | "destructive" | "info" | "outline";

/**
 * Map an alignment, approval gate status, or severity string to a badge variant.
 *
 * - aligned / ready / info / low → success or info
 * - minor_drift / medium / ack_required → warning
 * - major_drift / high / revision / blocker / blocked → destructive
 * - unclear / manual_review → warning (never success)
 * - unknown → outline
 */
export function getDriftBadgeVariant(value: string): BadgeVariant {
  const normalized = (value ?? "").toLowerCase().replace(/-/g, "_");

  switch (normalized) {
    case "aligned":
    case "ready":
    case "low":
    case "pass":
    case "approved":
      return "success";

    case "info":
    case "review_not_required":
      return "info";

    case "minor_drift":
    case "medium":
    case "drift_acknowledgement_required":
    case "ack_required":
    case "manual_review_available":
    case "unclear":
      return "warning";

    case "major_drift":
    case "high":
    case "critical":
    case "revision_required":
    case "drift_review_blocked":
    case "blocked":
    case "voided":
    case "superseded":
      return "destructive";

    default:
      return "outline";
  }
}

// ─── Review Gate Labels ───────────────────────────────────────────────────────

/**
 * Map a backend workflowState string to a concise human-readable label.
 */
export function getReviewGateLabel(workflowState: string): string {
  switch (workflowState) {
    case "review_not_required":
      return "Drift Review Disabled";
    case "manual_review_available":
      return "Draft Attempt Ready";
    case "automatic_review_pending_or_failed":
      return "Automatic Review Pending";
    case "external_review_required":
      return "External Review Required";
    case "approval_ready":
      return "Approval Ready";
    case "drift_acknowledgement_required":
      return "Acknowledgement Required";
    case "revision_required":
      return "Revision Required";
    case "drift_review_blocked":
      return "Review Blocked";
    case "ready_for_submission":
      return "Ready for Submission";
    case "submitted":
      return "Submitted";
    case "voided":
      return "Voided";
    case "superseded":
      return "Superseded by Revision";
    default:
      return workflowState ?? "Unknown";
  }
}

// ─── Action Gate Derivations ──────────────────────────────────────────────────

/**
 * Returns true if the review gate allows running a drift review.
 * Derived exclusively from backend allowedActions.
 */
export function canRunDriftReview(gate?: PlanAttemptReviewGateAPI): boolean {
  if (!gate) return false;
  return gate.allowedActions.includes("run_drift_review");
}

export type ApproveKind = "standard" | "drift_ack" | "no_review_ack";

/**
 * Returns the kind of approval available, or false if no approval action
 * is currently allowed. Priority: standard > drift_ack > no_review_ack.
 * Derived exclusively from backend allowedActions.
 */
export function canApprove(gate?: PlanAttemptReviewGateAPI): ApproveKind | false {
  if (!gate) return false;

  if (gate.allowedActions.includes("approve_plan_attempt")) {
    return "standard";
  }
  if (gate.allowedActions.includes("approve_plan_attempt_with_drift_acknowledgement")) {
    return "drift_ack";
  }
  if (gate.allowedActions.includes("approve_plan_attempt_with_no_review_acknowledgement")) {
    return "no_review_ack";
  }

  return false;
}

/**
 * Returns true if the review gate allows final submission of an approved attempt.
 * Requires both "submit_plan_attempt" in allowedActions AND workflowState
 * being "ready_for_submission". Derived exclusively from backend gate state.
 */
export function canSubmitAttempt(gate?: PlanAttemptReviewGateAPI): boolean {
  if (!gate) return false;
  return (
    gate.allowedActions.includes("submit_plan_attempt") &&
    gate.workflowState === "ready_for_submission"
  );
}

/**
 * Returns true if the review gate allows revising the current attempt.
 * Derived exclusively from backend allowedActions.
 */
export function canRevise(gate?: PlanAttemptReviewGateAPI): boolean {
  if (!gate) return false;
  return gate.allowedActions.includes("revise_plan_attempt");
}

/**
 * Returns true if the review gate allows voiding the current attempt.
 * Derived exclusively from backend allowedActions.
 */
export function canVoid(gate?: PlanAttemptReviewGateAPI): boolean {
  if (!gate) return false;
  return gate.allowedActions.includes("void_plan_attempt");
}

// ─── Drift Review Display Helpers ─────────────────────────────────────────────

/**
 * Format a confidence value (0–1 float) as a percentage string with one
 * decimal place, e.g. 0.923 → "92.3%".
 */
export function formatConfidence(confidence: number): string {
  return `${(confidence * 100).toFixed(1)}%`;
}

/**
 * Return a human-readable label for the review source field.
 */
export function getReviewSourceLabel(
  reviewSource: IntentDriftReviewAPI["reviewSource"],
): string {
  switch (reviewSource) {
    case "internal":
      return "Internal (model call)";
    case "external":
      return "External (separate reviewer)";
    default:
      return String(reviewSource ?? "Unknown");
  }
}
