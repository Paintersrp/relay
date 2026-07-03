// Unit tests for `deriveCurrentStatusText`.
//
// Covers:
// (1) One assertion per (step, displayState) pair explicitly listed in
//     design.md's "Per-Stage Current Status" table, checking the exact
//     `headline` string and `tone` value.
// (2) A totality check: for every display-state value each
//     Visual_State_Module (`IntakeBlockedState`, `CompileRenderDisplayState`,
//     `ExecuteDisplayState`, `AuditDisplayState`) can produce — including
//     states NOT explicitly listed in the design table (which fall back to
//     `getCompileRenderStateCardCopy`/`getAuditStateCardCopy`) — calling
//     `deriveCurrentStatusText` does not throw and returns a non-empty
//     `headline` and a valid `tone`.
//
// Validates: Requirements 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9

import { describe, expect, it } from "vitest";

import {
  deriveCurrentStatusText,
  type IntakeBlockedState,
} from "./deriveCurrentStatusText";
import type { Tone } from "./runStatusTrackerViews";
import type { CompileRenderDisplayState } from "@/routes/runs/$runId/runCompileRenderVisualState";
import type { ExecuteDisplayState } from "@/routes/runs/$runId/runExecuteVisualState";
import type { AuditDisplayState } from "@/routes/runs/$runId/runAuditVisualState";

const VALID_TONES: Tone[] = ["neutral", "info", "success", "warning", "danger"];

const baseContext = { updatedAt: "2024-01-01T00:00:00.000Z" };

// ------------------------------------------------------------
// (1) Per-Stage Current Status table — exact (step, displayState) pairs
// ------------------------------------------------------------

describe("deriveCurrentStatusText — Per-Stage Current Status table", () => {
  it.each<
    [
      step: "intake",
      displayState: IntakeBlockedState,
      headline: string,
      tone: Tone,
    ]
  >([
    [
      "intake",
      "not_blocked",
      "Waiting for you to review the incoming handoff.",
      "info",
    ],
    [
      "intake",
      "blocked",
      "Intake is blocked — review before this run can proceed.",
      "danger",
    ],
  ])("intake / %s", (step, displayState, headline, tone) => {
    const result = deriveCurrentStatusText(step, displayState, baseContext);
    expect(result.headline).toBe(headline);
    expect(result.tone).toBe(tone);
  });

  it.each<
    [
      step: "prepare",
      displayState: CompileRenderDisplayState,
      headline: string,
      tone: Tone,
    ]
  >([
    ["prepare", "ready_to_compile", "Ready to compile the packet.", "info"],
    ["prepare", "compiling", "Compiling the packet.", "info"],
    ["prepare", "rendering_brief", "Rendering the executor brief.", "info"],
    ["prepare", "repairing", "Attempting a repair.", "warning"],
    ["prepare", "approving", "Recording your approval.", "info"],
    [
      "prepare",
      "packet_invalid",
      "Packet validation failed — review before continuing.",
      "danger",
    ],
    [
      "prepare",
      "brief_ready",
      "Brief is ready for your approval.",
      "warning",
    ],
    [
      "prepare",
      "approved",
      "Approved. Ready to move to Execute.",
      "success",
    ],
  ])("prepare / %s", (step, displayState, headline, tone) => {
    const result = deriveCurrentStatusText(step, displayState, baseContext);
    expect(result.headline).toBe(headline);
    expect(result.tone).toBe(tone);
  });

  it.each<
    [
      step: "execute",
      displayState: ExecuteDisplayState,
      headline: string,
      tone: Tone,
    ]
  >([
    ["execute", "ready", "Ready to start the executor.", "info"],
    ["execute", "running", "Executor is running.", "info"],
    [
      "execute",
      "validating",
      "Executor finished — running validation.",
      "info",
    ],
    [
      "execute",
      "complete",
      "Execution complete. Ready for audit.",
      "success",
    ],
    [
      "execute",
      "failed",
      "Executor is blocked — review before retrying.",
      "danger",
    ],
    ["execute", "blocked", "Execute is not available yet.", "warning"],
  ])("execute / %s", (step, displayState, headline, tone) => {
    const result = deriveCurrentStatusText(step, displayState, baseContext);
    expect(result.headline).toBe(headline);
    expect(result.tone).toBe(tone);
  });

  it.each<
    [
      step: "audit",
      displayState: AuditDisplayState,
      headline: string,
      tone: Tone,
    ]
  >([
    [
      "audit",
      "audit_candidate",
      "Ready to generate the audit packet.",
      "success",
    ],
    [
      "audit",
      "audit_candidate_with_executor_blocker",
      "Ready to generate the audit packet.",
      "warning",
    ],
    [
      "audit",
      "audit_ready",
      "Audit packet is ready for your decision.",
      "success",
    ],
    [
      "audit",
      "revision_required",
      "Revision requested — update and regenerate.",
      "warning",
    ],
    ["audit", "accepted", "Approved. Ready to close the run.", "success"],
    [
      "audit",
      "accepted_with_warnings",
      "Approved with warnings. Ready to close the run.",
      "warning",
    ],
    ["audit", "completed", "Run closed.", "success"],
    [
      "audit",
      "blocked",
      "Audit is blocked — review before continuing.",
      "danger",
    ],
    [
      "audit",
      "validation_failed",
      "Audit is blocked — review before continuing.",
      "danger",
    ],
  ])("audit / %s", (step, displayState, headline, tone) => {
    const result = deriveCurrentStatusText(step, displayState, baseContext);
    expect(result.headline).toBe(headline);
    expect(result.tone).toBe(tone);
  });
});

// ------------------------------------------------------------
// (2) Totality check — every value each Visual_State_Module can produce,
// including fallback states not explicitly listed in the design table.
// ------------------------------------------------------------

describe("deriveCurrentStatusText — totality over every Visual_State_Module value", () => {
  const INTAKE_STATES: IntakeBlockedState[] = ["blocked", "not_blocked"];

  const PREPARE_STATES: CompileRenderDisplayState[] = [
    "blocked",
    "ready_to_compile",
    "compiling",
    "packet_invalid",
    "repairing",
    "repair_validated",
    "packet_validated",
    "rendering_brief",
    "brief_ready",
    "approving",
    "approved",
  ];

  const EXECUTE_STATES: ExecuteDisplayState[] = [
    "blocked",
    "ready",
    "running",
    "validating",
    "complete",
    "failed",
  ];

  const AUDIT_STATES: AuditDisplayState[] = [
    "blocked",
    "validation_required",
    "validation_running",
    "validation_failed",
    "validation_accepted",
    "validation_passed",
    "audit_candidate",
    "audit_candidate_with_executor_blocker",
    "generating_audit",
    "audit_ready",
    "submitting_manual",
    "approving",
    "accepted",
    "accepted_with_warnings",
    "revision_required",
    "preparing_commit_message",
    "closing",
    "completed",
  ];

  it.each(INTAKE_STATES)(
    "intake / %s does not throw and returns a valid CurrentStatusView",
    (displayState) => {
      let result;
      expect(() => {
        result = deriveCurrentStatusText("intake", displayState, baseContext);
      }).not.toThrow();
      expect(result!.headline.length).toBeGreaterThan(0);
      expect(VALID_TONES).toContain(result!.tone);
    },
  );

  it.each(PREPARE_STATES)(
    "prepare / %s does not throw and returns a valid CurrentStatusView",
    (displayState) => {
      let result;
      expect(() => {
        result = deriveCurrentStatusText(
          "prepare",
          displayState,
          baseContext,
        );
      }).not.toThrow();
      expect(result!.headline.length).toBeGreaterThan(0);
      expect(VALID_TONES).toContain(result!.tone);
    },
  );

  it.each(EXECUTE_STATES)(
    "execute / %s does not throw and returns a valid CurrentStatusView",
    (displayState) => {
      let result;
      expect(() => {
        result = deriveCurrentStatusText(
          "execute",
          displayState,
          baseContext,
        );
      }).not.toThrow();
      expect(result!.headline.length).toBeGreaterThan(0);
      expect(VALID_TONES).toContain(result!.tone);
    },
  );

  it.each(AUDIT_STATES)(
    "audit / %s does not throw and returns a valid CurrentStatusView",
    (displayState) => {
      let result;
      expect(() => {
        result = deriveCurrentStatusText("audit", displayState, baseContext);
      }).not.toThrow();
      expect(result!.headline.length).toBeGreaterThan(0);
      expect(VALID_TONES).toContain(result!.tone);
    },
  );
});
