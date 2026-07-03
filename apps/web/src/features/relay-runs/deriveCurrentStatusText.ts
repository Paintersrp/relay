// ============================================================
// Run Status Tracker Redesign — deriveCurrentStatusText
// ============================================================
//
// Produces the single `CurrentStatusView` rendered by `CurrentStatusBlock`
// (Requirement 2). This function introduces NO new status classification:
// it consumes the *existing*, unchanged Visual_State_Module display-state
// value for the Active_Route_Step (`getExecuteDisplayState` /
// `getAuditDisplayState` / `getCompileRenderDisplayState` / the existing
// intake blocked/not-blocked check) and renders it as exactly one
// plain-language sentence + tone, per the "Per-Stage Current Status" table
// in design.md. See design.md's "Function: deriveCurrentStatusText"
// pseudocode section for the authoritative contract.

import type { RelayRunStep } from "./types";
import type { CurrentStatusView, Tone } from "./runStatusTrackerViews";

import type { RunStageTone } from "@/components/relay/RunStagePrimitives";
import type { ExecuteDisplayState } from "@/routes/runs/$runId/runExecuteVisualState";
import {
  getAuditStateCardCopy,
  type AuditDisplayState,
} from "@/routes/runs/$runId/runAuditVisualState";
import {
  getCompileRenderStateCardCopy,
  type CompileRenderDisplayState,
} from "@/routes/runs/$runId/runCompileRenderVisualState";

// ------------------------------------------------------------
// Intake's display state, restated as a blocked/not-blocked check
// ------------------------------------------------------------
//
// Per Requirements 2.3/2.4 and the design's Per-Stage Current Status
// table, intake only distinguishes "blocked" vs "not blocked" for the
// headline — it does not reuse the finer-grained `IntakeDisplayState`
// (review/received/approved/blocked/default) from
// `runIntakeVisualState.ts`. Callers derive this boolean-shaped value
// from the intake page data (e.g. `intakeDisplayState === "blocked"`)
// before invoking this function.
export type IntakeBlockedState = "blocked" | "not_blocked";

// ------------------------------------------------------------
// Context — supporting values that influence ONLY `detail`, never the
// classification expressed by `headline`/`tone` (Requirement 2.10).
// ------------------------------------------------------------

export interface StatusTextContext {
  /** Passed straight through to `CurrentStatusView.updatedAt`. */
  updatedAt: string;
  /** Count of current blockers, if known. Only ever affects `detail`. */
  blockerCount?: number;
  /** Count of outstanding revision requirements, if known. Only ever affects `detail`. */
  revisionRequirementCount?: number;
  /** Count of current warnings, if known. Only ever affects `detail`. */
  warningCount?: number;
}

function mapRunStageTone(tone: RunStageTone): Tone {
  return tone === "default" ? "neutral" : tone;
}

function buildDetail(context: StatusTextContext): string | undefined {
  const parts: string[] = [];

  if (context.blockerCount) {
    parts.push(
      `${context.blockerCount} blocker${context.blockerCount === 1 ? "" : "s"}`,
    );
  }
  if (context.revisionRequirementCount) {
    parts.push(
      `${context.revisionRequirementCount} revision requirement${
        context.revisionRequirementCount === 1 ? "" : "s"
      }`,
    );
  }
  if (context.warningCount) {
    parts.push(
      `${context.warningCount} warning${context.warningCount === 1 ? "" : "s"}`,
    );
  }

  if (parts.length === 0) {
    return undefined;
  }

  return `${parts.join(", ")} to review.`;
}

function assertUnreachable(value: never): never {
  throw new Error(`deriveCurrentStatusText: unhandled state ${String(value)}`);
}

// ------------------------------------------------------------
// Per-step headline/tone lookups
// ------------------------------------------------------------
//
// Each lookup below is an exhaustive switch over the corresponding
// Visual_State_Module's display-state enum (Requirement 2.8). Headlines
// come from the design's "Per-Stage Current Status" table wherever that
// table defines one for the state. For display-state values the table
// does not enumerate, the headline/tone fall back to the existing
// `getXStateCardCopy`'s `title`/`tone` for that state — this keeps the
// function total without inventing new classification copy.

function deriveIntakeStatus(
  displayState: IntakeBlockedState,
): { headline: string; tone: Tone } {
  switch (displayState) {
    case "not_blocked":
      return {
        headline: "Waiting for you to review the incoming handoff.",
        tone: "info",
      };
    case "blocked":
      return {
        headline: "Intake is blocked — review before this run can proceed.",
        tone: "danger",
      };
    default:
      return assertUnreachable(displayState);
  }
}

function derivePrepareStatus(
  displayState: CompileRenderDisplayState,
): { headline: string; tone: Tone } {
  switch (displayState) {
    case "ready_to_compile":
      return { headline: "Ready to compile the packet.", tone: "info" };
    case "compiling":
      return { headline: "Compiling the packet.", tone: "info" };
    case "rendering_brief":
      return { headline: "Rendering the executor brief.", tone: "info" };
    case "repairing":
      return { headline: "Attempting a repair.", tone: "warning" };
    case "approving":
      return { headline: "Recording your approval.", tone: "info" };
    case "packet_invalid":
      return {
        headline: "Packet validation failed — review before continuing.",
        tone: "danger",
      };
    case "brief_ready":
      return {
        headline: "Brief is ready for your approval.",
        tone: "warning",
      };
    case "approved":
      return {
        headline: "Approved. Ready to move to Execute.",
        tone: "success",
      };
    // Not enumerated in the design's Per-Stage Current Status table —
    // fall back to the existing getCompileRenderStateCardCopy title/tone.
    case "blocked":
    case "repair_validated":
    case "packet_validated": {
      const copy = getCompileRenderStateCardCopy(displayState);
      return { headline: copy.title, tone: mapRunStageTone(copy.tone) };
    }
    default:
      return assertUnreachable(displayState);
  }
}

function deriveExecuteStatus(
  displayState: ExecuteDisplayState,
): { headline: string; tone: Tone } {
  switch (displayState) {
    case "ready":
      return { headline: "Ready to start the executor.", tone: "info" };
    case "running":
      return { headline: "Executor is running.", tone: "info" };
    case "validating":
      return {
        headline: "Executor finished — running validation.",
        tone: "info",
      };
    case "complete":
      return {
        headline: "Execution complete. Ready for audit.",
        tone: "success",
      };
    case "failed":
      return {
        headline: "Executor is blocked — review before retrying.",
        tone: "danger",
      };
    case "blocked":
      return { headline: "Execute is not available yet.", tone: "warning" };
    default:
      return assertUnreachable(displayState);
  }
}

function deriveAuditStatus(
  displayState: AuditDisplayState,
): { headline: string; tone: Tone } {
  switch (displayState) {
    case "audit_candidate":
    case "audit_candidate_with_executor_blocker": {
      const copy = getAuditStateCardCopy(displayState);
      return {
        headline: "Ready to generate the audit packet.",
        tone: mapRunStageTone(copy.tone),
      };
    }
    case "audit_ready":
      return {
        headline: "Audit packet is ready for your decision.",
        tone: "success",
      };
    case "revision_required":
      return {
        headline: "Revision requested — update and regenerate.",
        tone: "warning",
      };
    case "accepted":
      return {
        headline: "Approved. Ready to close the run.",
        tone: "success",
      };
    case "accepted_with_warnings":
      return {
        headline: "Approved with warnings. Ready to close the run.",
        tone: "warning",
      };
    case "completed":
      return { headline: "Run closed.", tone: "success" };
    case "blocked":
    case "validation_failed":
      return {
        headline: "Audit is blocked — review before continuing.",
        tone: "danger",
      };
    // Not enumerated in the design's Per-Stage Current Status table —
    // fall back to the existing getAuditStateCardCopy title/tone.
    case "validation_required":
    case "validation_running":
    case "validation_accepted":
    case "validation_passed":
    case "generating_audit":
    case "submitting_manual":
    case "approving":
    case "preparing_commit_message":
    case "closing": {
      const copy = getAuditStateCardCopy(displayState);
      return { headline: copy.title, tone: mapRunStageTone(copy.tone) };
    }
    default:
      return assertUnreachable(displayState);
  }
}

// ------------------------------------------------------------
// Public dispatcher
// ------------------------------------------------------------

export function deriveCurrentStatusText(
  step: "intake",
  displayState: IntakeBlockedState,
  context: StatusTextContext,
): CurrentStatusView;
export function deriveCurrentStatusText(
  step: "prepare",
  displayState: CompileRenderDisplayState,
  context: StatusTextContext,
): CurrentStatusView;
export function deriveCurrentStatusText(
  step: "execute",
  displayState: ExecuteDisplayState,
  context: StatusTextContext,
): CurrentStatusView;
export function deriveCurrentStatusText(
  step: "audit",
  displayState: AuditDisplayState,
  context: StatusTextContext,
): CurrentStatusView;
export function deriveCurrentStatusText(
  step: RelayRunStep,
  displayState:
    | IntakeBlockedState
    | CompileRenderDisplayState
    | ExecuteDisplayState
    | AuditDisplayState,
  context: StatusTextContext,
): CurrentStatusView {
  const { headline, tone } = (() => {
    switch (step) {
      case "intake":
        return deriveIntakeStatus(displayState as IntakeBlockedState);
      case "prepare":
        return derivePrepareStatus(displayState as CompileRenderDisplayState);
      case "execute":
        return deriveExecuteStatus(displayState as ExecuteDisplayState);
      case "audit":
        return deriveAuditStatus(displayState as AuditDisplayState);
      default:
        return assertUnreachable(step);
    }
  })();

  return {
    tone,
    headline,
    detail: buildDetail(context),
    updatedAt: context.updatedAt,
  };
}
