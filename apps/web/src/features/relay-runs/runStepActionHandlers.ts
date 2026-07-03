// ============================================================
// Run Workbench Refinement — Action invocation wiring (Requirement 4.9, 4.2, 8.5, 8.6)
// ============================================================
//
// This module is the route/component-level counterpart to the pure
// derivation helpers in `runStepActions.ts`. `deriveExecuteActions` and
// `deriveAuditActions` return only `ActionControlView`/`StepActionsView`
// view data — they never invoke, wrap, or hold a reference to any action
// request/invocation path. This module is where that invocation wiring
// actually lives: a small `id -> handler` map over the existing `api.ts`
// request functions, intended to be used by a route (or by the component
// composing `RunStepActionBar`) to build the `onActionClick` callback.
//
// None of the `api.ts` request functions are modified here, and none of
// their request contracts are altered — this module only references them.

import {
  auditRun,
  cancelRun,
  closeRun,
  executeRun,
  prepareCommitMessage,
  recoverRun,
} from "./api";
import type { AuditActionResponse, PrepareCommitMessageResponse } from "./api";
import type { RelayActionResponse } from "./types";

/**
 * A handler invokable with just the run id. This is intentionally narrower
 * than every `api.ts` action function's signature — some audit actions
 * (see `AUDIT_ACTION_HANDLERS` below) require additional payload data that
 * cannot be derived from a run id alone and are therefore not represented
 * as `RunIdActionHandler`s here.
 */
export type RunIdActionHandler = (runId: string) => Promise<unknown>;

/**
 * Execute step: every candidate action id from `deriveExecuteActions`
 * (`start`, `recover`, `cancel`) maps directly onto an existing `api.ts`
 * function that takes only the run id, so all three are represented here.
 */
export const EXECUTE_ACTION_HANDLERS: Record<string, RunIdActionHandler> = {
  start: (runId: string): Promise<RelayActionResponse> => executeRun(runId),
  recover: (runId: string): Promise<RelayActionResponse> => recoverRun(runId),
  cancel: (runId: string): Promise<RelayActionResponse> => cancelRun(runId),
};

/**
 * Audit step: only the candidate action ids from `deriveAuditActions` that
 * are invokable with just a run id are represented here:
 *   - `closeRun` -> `closeRun(id)`
 *   - `generateAudit` -> `auditRun(id)` (generating an audit packet IS the
 *     "start audit" call; there is no separate `generateAudit` function)
 *   - `prepareCommitMessage` -> `prepareCommitMessage(id)`
 *
 * `approveAudit` and `requestRevision` are intentionally NOT included:
 * `approveAudit(id, payload)` requires a decision (+ optional notes) and
 * `requestAuditRevision(id, payload?)` accepts optional notes/reason —
 * both need data collected from an Operator-facing form before invocation,
 * which is outside the scope of an id-only handler map. `submitManual` is
 * also excluded because `submitManualAuditPacket(id, payload)` requires a
 * markdown packet body plus a decision. The composing route's existing
 * form-based mutation handlers (see `audit.tsx`) remain responsible for
 * collecting that data and calling those functions directly; this map only
 * covers the direct-invoke subset.
 */
export const AUDIT_ACTION_HANDLERS: Record<string, RunIdActionHandler> = {
  closeRun: (runId: string): Promise<AuditActionResponse> => closeRun(runId),
  generateAudit: (runId: string): Promise<RelayActionResponse> => auditRun(runId),
  prepareCommitMessage: (
    runId: string,
  ): Promise<PrepareCommitMessageResponse> => prepareCommitMessage(runId),
};

/**
 * Audit action ids that are deliberately absent from `AUDIT_ACTION_HANDLERS`
 * because they require additional payload data (decision/notes/reason, or a
 * markdown packet body) collected via a form before invocation. Exported so
 * composing routes/tests can assert on this list rather than re-deriving it.
 */
export const AUDIT_ACTION_IDS_REQUIRING_FORM_DATA = [
  "approveAudit",
  "requestRevision",
  "submitManual",
] as const;

/**
 * Builds an `onActionClick` callback (matching `RunStepActionBar`'s
 * `onActionClick?: (id: string) => void` prop) from an `id -> handler` map
 * and a run id. Unknown ids (including the form-driven audit ids above)
 * are silently ignored here — routes that need to handle them should check
 * for those ids first and fall back to their own form-based handlers
 * before delegating remaining ids to this helper.
 */
export function createActionClickHandler(
  handlers: Record<string, RunIdActionHandler>,
  runId: string,
): (id: string) => void {
  return (id: string) => {
    const handler = handlers[id];
    if (handler) {
      void handler(runId);
    }
  };
}
