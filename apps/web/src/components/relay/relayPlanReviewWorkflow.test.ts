import { describe, expect, it } from "vitest";
import {
  parseDriftFindings,
  getDriftBadgeVariant,
  getReviewGateLabel,
  canRunDriftReview,
  canApprove,
  canSubmitAttempt,
  canRevise,
  canVoid,
  formatConfidence,
} from "./relayPlanReviewWorkflow";
import type { PlanAttemptReviewGateAPI } from "@/features/relay-plans";

// ─── parseDriftFindings ───────────────────────────────────────────────────────

describe("parseDriftFindings", () => {
  it("returns an empty array for empty string", () => {
    expect(parseDriftFindings("")).toEqual([]);
  });

  it("returns an empty array for whitespace string", () => {
    expect(parseDriftFindings("   ")).toEqual([]);
  });

  it("returns an empty array for malformed JSON", () => {
    expect(parseDriftFindings("{not-json}")).toEqual([]);
  });

  it("returns an empty array when JSON is not an array", () => {
    expect(parseDriftFindings('{"finding":"something"}')).toEqual([]);
  });

  it("parses a valid findings array", () => {
    const json = JSON.stringify([
      {
        findingId: "f1",
        severity: "high",
        summary: "Critical drift detected",
        evidence: ["Line 42 changed"],
        suggestedResolution: "Revert changes",
      },
    ]);
    const result = parseDriftFindings(json);
    expect(result).toHaveLength(1);
    expect(result[0]!.findingId).toBe("f1");
    expect(result[0]!.severity).toBe("high");
    expect(result[0]!.summary).toBe("Critical drift detected");
    expect(result[0]!.evidence).toEqual(["Line 42 changed"]);
    expect(result[0]!.suggestedResolution).toBe("Revert changes");
  });

  it("defaults summary to placeholder when missing", () => {
    const json = JSON.stringify([{ evidence: [] }]);
    const result = parseDriftFindings(json);
    expect(result[0]!.summary).toBe("(no summary)");
  });

  it("filters out non-object array entries", () => {
    const json = JSON.stringify([
      { summary: "ok", evidence: [] },
      "not-an-object",
      42,
    ]);
    const result = parseDriftFindings(json);
    expect(result).toHaveLength(1);
    expect(result[0]!.summary).toBe("ok");
  });
});

// ─── getDriftBadgeVariant ─────────────────────────────────────────────────────

describe("getDriftBadgeVariant", () => {
  it("maps 'aligned' to success", () => {
    expect(getDriftBadgeVariant("aligned")).toBe("success");
  });

  it("maps 'minor_drift' to warning", () => {
    expect(getDriftBadgeVariant("minor_drift")).toBe("warning");
  });

  it("maps 'major_drift' to destructive", () => {
    expect(getDriftBadgeVariant("major_drift")).toBe("destructive");
  });

  it("maps 'unclear' to warning (not success)", () => {
    const variant = getDriftBadgeVariant("unclear");
    expect(variant).toBe("warning");
  });

  it("maps 'revision_required' to destructive", () => {
    expect(getDriftBadgeVariant("revision_required")).toBe("destructive");
  });

  it("maps 'low' to success", () => {
    expect(getDriftBadgeVariant("low")).toBe("success");
  });

  it("maps 'high' to destructive", () => {
    expect(getDriftBadgeVariant("high")).toBe("destructive");
  });

  it("maps unknown string to outline", () => {
    expect(getDriftBadgeVariant("some_unknown_value")).toBe("outline");
  });
});

// ─── getReviewGateLabel ───────────────────────────────────────────────────────

describe("getReviewGateLabel", () => {
  const cases: [string, string][] = [
    ["review_not_required", "Drift Review Disabled"],
    ["manual_review_available", "Draft Attempt Ready"],
    ["automatic_review_pending_or_failed", "Automatic Review Pending"],
    ["external_review_required", "External Review Required"],
    ["approval_ready", "Approval Ready"],
    ["drift_acknowledgement_required", "Acknowledgement Required"],
    ["revision_required", "Revision Required"],
    ["drift_review_blocked", "Review Blocked"],
    ["ready_for_submission", "Ready for Submission"],
    ["submitted", "Submitted"],
    ["voided", "Voided"],
    ["superseded", "Superseded by Revision"],
  ];

  for (const [state, label] of cases) {
    it(`maps '${state}' to '${label}'`, () => {
      expect(getReviewGateLabel(state)).toBe(label);
    });
  }

  it("returns the raw state string for unknown states", () => {
    expect(getReviewGateLabel("some_new_state")).toBe("some_new_state");
  });
});

// ─── Gate Action Helpers ──────────────────────────────────────────────────────

function makeGate(
  overrides: Partial<PlanAttemptReviewGateAPI>,
): PlanAttemptReviewGateAPI {
  return {
    workflowState: "manual_review_available",
    driftReviewMode: "manual",
    modelTier: "standard",
    reviewRequired: true,
    modelCallAllowed: false,
    allowedActions: [],
    ...overrides,
  };
}

describe("canRunDriftReview", () => {
  it("returns false when gate is undefined", () => {
    expect(canRunDriftReview(undefined)).toBe(false);
  });

  it("returns false when allowedActions does not include run_drift_review", () => {
    expect(canRunDriftReview(makeGate({ allowedActions: ["approve_plan_attempt"] }))).toBe(false);
  });

  it("returns true when allowedActions includes run_drift_review", () => {
    expect(canRunDriftReview(makeGate({ allowedActions: ["run_drift_review"] }))).toBe(true);
  });
});

describe("canApprove", () => {
  it("returns false when gate is undefined", () => {
    expect(canApprove(undefined)).toBe(false);
  });

  it("returns false when no approval action is allowed", () => {
    expect(canApprove(makeGate({ allowedActions: ["run_drift_review"] }))).toBe(false);
  });

  it("returns 'standard' when approve_plan_attempt is allowed", () => {
    expect(canApprove(makeGate({ allowedActions: ["approve_plan_attempt"] }))).toBe("standard");
  });

  it("returns 'drift_ack' when drift acknowledgement approval is allowed but not standard", () => {
    expect(
      canApprove(
        makeGate({
          allowedActions: ["approve_plan_attempt_with_drift_acknowledgement"],
        }),
      ),
    ).toBe("drift_ack");
  });

  it("returns 'no_review_ack' when no-review acknowledgement is allowed but not others", () => {
    expect(
      canApprove(
        makeGate({
          allowedActions: ["approve_plan_attempt_with_no_review_acknowledgement"],
        }),
      ),
    ).toBe("no_review_ack");
  });

  it("prefers standard over drift_ack when both are present", () => {
    expect(
      canApprove(
        makeGate({
          allowedActions: [
            "approve_plan_attempt",
            "approve_plan_attempt_with_drift_acknowledgement",
          ],
        }),
      ),
    ).toBe("standard");
  });
});

describe("canSubmitAttempt", () => {
  it("returns false when gate is undefined", () => {
    expect(canSubmitAttempt(undefined)).toBe(false);
  });

  it("returns false when action is allowed but state is not ready_for_submission", () => {
    expect(
      canSubmitAttempt(
        makeGate({
          workflowState: "approval_ready",
          allowedActions: ["submit_plan_attempt"],
        }),
      ),
    ).toBe(false);
  });

  it("returns false when state is ready_for_submission but action is not in allowedActions", () => {
    expect(
      canSubmitAttempt(
        makeGate({
          workflowState: "ready_for_submission",
          allowedActions: [],
        }),
      ),
    ).toBe(false);
  });

  it("returns true when state is ready_for_submission and submit_plan_attempt is allowed", () => {
    expect(
      canSubmitAttempt(
        makeGate({
          workflowState: "ready_for_submission",
          allowedActions: ["submit_plan_attempt"],
        }),
      ),
    ).toBe(true);
  });
});

describe("canRevise", () => {
  it("returns false when gate is undefined", () => {
    expect(canRevise(undefined)).toBe(false);
  });

  it("returns true when revise_plan_attempt is in allowedActions", () => {
    expect(canRevise(makeGate({ allowedActions: ["revise_plan_attempt"] }))).toBe(true);
  });
});

describe("canVoid", () => {
  it("returns false when gate is undefined", () => {
    expect(canVoid(undefined)).toBe(false);
  });

  it("returns true when void_plan_attempt is in allowedActions", () => {
    expect(canVoid(makeGate({ allowedActions: ["void_plan_attempt"] }))).toBe(true);
  });
});

// ─── formatConfidence ─────────────────────────────────────────────────────────

describe("formatConfidence", () => {
  it("formats 1.0 as 100.0%", () => {
    expect(formatConfidence(1.0)).toBe("100.0%");
  });

  it("formats 0.0 as 0.0%", () => {
    expect(formatConfidence(0.0)).toBe("0.0%");
  });

  it("formats 0.923 as 92.3%", () => {
    expect(formatConfidence(0.923)).toBe("92.3%");
  });

  it("formats 0.5 as 50.0%", () => {
    expect(formatConfidence(0.5)).toBe("50.0%");
  });
});
