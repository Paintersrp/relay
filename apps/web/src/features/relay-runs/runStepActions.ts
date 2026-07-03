// ============================================================
// Run Workbench Refinement — Next_Safe_Action derivation (Requirement 4)
// ============================================================
//
// Pure, presentation-only helpers that derive `StepActionsView` (a list of
// `ActionControlView` plus an optional `nextSafeActionId`) from the existing
// `RelayExecuteActions` / `RelayAuditActions` gating shapes.
//
// These helpers are pure view-data derivation ONLY. They MUST NOT invoke,
// wrap, or hold a reference to any action request/invocation path (e.g.
// `executeRun`, `cancelRun`, `auditRun`, etc. from `api.ts`). Mapping an
// `ActionControlView.id` to its concrete `api.ts` request function, and
// triggering it on Operator interaction, is owned by the consuming
// route/component layer (see task 5.6), never by this module.

import type {
  ActionControlView,
  RelayAuditActions,
  RelayExecuteActions,
  StepActionsView,
} from "./runWorkbenchViews";

// ------------------------------------------------------------
// Execute step
// ------------------------------------------------------------

interface ExecuteCandidate {
  id: string;
  label: string;
  enabled: boolean;
  unavailableReason?: string;
}

/**
 * Fixed per-step priority order for selecting the Next_Safe_Action, per
 * design.md: "Execute priority: canStart -> canRecover -> canCancel."
 */
function buildExecuteCandidates(actions: RelayExecuteActions): ExecuteCandidate[] {
  return [
    {
      id: "start",
      label: "Start",
      enabled: actions.canStart,
      unavailableReason: actions.startUnavailableReason,
    },
    {
      id: "recover",
      label: "Recover",
      enabled: actions.canRecover,
      unavailableReason: actions.recoverUnavailableReason,
    },
    {
      id: "cancel",
      label: "Cancel",
      enabled: actions.canCancel,
      unavailableReason: actions.cancelUnavailableReason,
    },
  ];
}

// ------------------------------------------------------------
// Audit step
// ------------------------------------------------------------

interface AuditCandidate {
  id: string;
  label: string;
  enabled: boolean;
  unavailableReason?: string;
}

/**
 * Fixed per-step priority order for selecting the Next_Safe_Action, per
 * design.md: "Audit priority: canCloseRun -> canApproveAudit ->
 * canRequestRevision -> canGenerateAudit -> canSubmitManual ->
 * canPrepareCommitMessage."
 */
function buildAuditCandidates(actions: RelayAuditActions): AuditCandidate[] {
  return [
    {
      id: "closeRun",
      label: "Close Run",
      enabled: actions.canCloseRun,
      unavailableReason: actions.closeRunUnavailableReason,
    },
    {
      id: "approveAudit",
      label: "Approve Audit",
      enabled: actions.canApproveAudit,
      unavailableReason: actions.approveAuditUnavailableReason,
    },
    {
      id: "requestRevision",
      label: "Request Revision",
      enabled: actions.canRequestRevision,
      unavailableReason: actions.requestRevisionUnavailableReason,
    },
    {
      id: "generateAudit",
      label: "Generate Audit",
      enabled: actions.canGenerateAudit,
      unavailableReason: actions.generateAuditUnavailableReason,
    },
    {
      id: "submitManual",
      label: "Submit Manual",
      enabled: actions.canSubmitManual,
      unavailableReason: actions.submitManualUnavailableReason,
    },
    {
      id: "prepareCommitMessage",
      label: "Prepare Commit Message",
      enabled: actions.canPrepareCommitMessage,
      unavailableReason: actions.prepareCommitMessageUnavailableReason,
    },
  ];
}

// ------------------------------------------------------------
// Shared assembly logic
// ------------------------------------------------------------

/**
 * Assembles a `StepActionsView` from an ordered list of candidates (already
 * in the step's fixed priority order).
 *
 * - Every candidate becomes an `ActionControlView`; none are omitted.
 * - `enabled` mirrors the candidate's `can*` flag exactly.
 * - `unavailableReason` is carried only when the control is disabled AND the
 *   reason string is present and non-empty.
 * - The first candidate (in priority order) whose flag is true is
 *   designated the Next_Safe_Action: its control gets `isPrimary: true` and
 *   its id becomes `nextSafeActionId`. When no flag is true, no control is
 *   primary and `nextSafeActionId` is left unset.
 */
function buildStepActionsView(
  candidates: Array<{ id: string; label: string; enabled: boolean; unavailableReason?: string }>
): StepActionsView {
  const nextSafeActionId = candidates.find((candidate) => candidate.enabled)?.id;

  const controls: ActionControlView[] = candidates.map((candidate) => {
    const isPrimary = candidate.id === nextSafeActionId;
    const hasReason =
      !candidate.enabled &&
      typeof candidate.unavailableReason === "string" &&
      candidate.unavailableReason.length > 0;

    return {
      id: candidate.id,
      label: candidate.label,
      enabled: candidate.enabled,
      isPrimary,
      ...(hasReason ? { unavailableReason: candidate.unavailableReason } : {}),
    };
  });

  return {
    controls,
    ...(nextSafeActionId ? { nextSafeActionId } : {}),
  };
}

// ------------------------------------------------------------
// Public helpers
// ------------------------------------------------------------

/**
 * Derives the execute step's `StepActionsView` from `RelayExecuteActions`.
 *
 * Returns a control for every candidate action (`start`, `recover`,
 * `cancel`) even when disabled or when `actions` is `undefined` (in which
 * case every control is disabled with no reason and no primary is
 * designated).
 */
export function deriveExecuteActions(actions: RelayExecuteActions | undefined): StepActionsView {
  if (!actions) {
    return buildStepActionsView(
      buildExecuteCandidates({
        canStart: false,
        canCancel: false,
        canRecover: false,
      })
    );
  }

  return buildStepActionsView(buildExecuteCandidates(actions));
}

/**
 * Derives the audit step's `StepActionsView` from `RelayAuditActions`.
 *
 * Returns a control for every candidate action (`closeRun`, `approveAudit`,
 * `requestRevision`, `generateAudit`, `submitManual`,
 * `prepareCommitMessage`) even when disabled or when `actions` is
 * `undefined` (in which case every control is disabled with no reason and
 * no primary is designated).
 */
export function deriveAuditActions(actions: RelayAuditActions | undefined): StepActionsView {
  if (!actions) {
    return buildStepActionsView(
      buildAuditCandidates({
        canGenerateAudit: false,
        canSubmitManual: false,
        canApproveAudit: false,
        canRequestRevision: false,
        canPrepareCommitMessage: false,
        canCloseRun: false,
      })
    );
  }

  return buildStepActionsView(buildAuditCandidates(actions));
}
