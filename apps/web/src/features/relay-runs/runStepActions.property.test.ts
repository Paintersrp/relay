// Feature: run-workbench-refinement, Property 5: For any `RelayExecuteActions`
// or `RelayAuditActions` value (including all-false and `undefined`),
// `deriveExecuteActions` / `deriveAuditActions` returns pure
// `ActionControlView` data (no `invoke` callable) for every candidate action
// for the step (none hidden from the returned data) and sets each control's
// `enabled` to true if and only if its corresponding `can*` flag is true,
// never enabling a control whose flag is false and never enabling any
// control when the actions object is `undefined`.
//
// Validates: Requirements 4.1, 4.2, 4.6, 4.8, 4.9, 8.5

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { deriveAuditActions, deriveExecuteActions } from "./runStepActions";
import type { RelayAuditActions, RelayExecuteActions } from "./types";

// ------------------------------------------------------------
// Candidate id/flag maps (mirrors runStepActions.ts priority order)
// ------------------------------------------------------------

const EXECUTE_ID_TO_FLAG: Record<string, keyof RelayExecuteActions> = {
  start: "canStart",
  recover: "canRecover",
  cancel: "canCancel",
};

const AUDIT_ID_TO_FLAG: Record<string, keyof RelayAuditActions> = {
  closeRun: "canCloseRun",
  approveAudit: "canApproveAudit",
  requestRevision: "canRequestRevision",
  generateAudit: "canGenerateAudit",
  submitManual: "canSubmitManual",
  prepareCommitMessage: "canPrepareCommitMessage",
};

const EXECUTE_CANDIDATE_IDS = Object.keys(EXECUTE_ID_TO_FLAG);
const AUDIT_CANDIDATE_IDS = Object.keys(AUDIT_ID_TO_FLAG);

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const executeActionsArb: fc.Arbitrary<RelayExecuteActions> = fc.record({
  canStart: fc.boolean(),
  canCancel: fc.boolean(),
  canRecover: fc.boolean(),
});

const auditActionsArb: fc.Arbitrary<RelayAuditActions> = fc.record({
  canGenerateAudit: fc.boolean(),
  canSubmitManual: fc.boolean(),
  canApproveAudit: fc.boolean(),
  canRequestRevision: fc.boolean(),
  canPrepareCommitMessage: fc.boolean(),
  canCloseRun: fc.boolean(),
});

// Includes the `undefined` case alongside arbitrary boolean combinations.
const executeActionsOrUndefinedArb: fc.Arbitrary<RelayExecuteActions | undefined> = fc.oneof(
  executeActionsArb,
  fc.constant(undefined),
);

const auditActionsOrUndefinedArb: fc.Arbitrary<RelayAuditActions | undefined> = fc.oneof(
  auditActionsArb,
  fc.constant(undefined),
);

describe("deriveExecuteActions / deriveAuditActions — Property 5: action enablement mirrors gating flags exactly", () => {
  it("execute: every candidate is present, enabled mirrors can* exactly, and no invoke field exists (Req 4.1, 4.2, 4.6, 4.8, 4.9, 8.5)", () => {
    fc.assert(
      fc.property(executeActionsOrUndefinedArb, (actions) => {
        const view = deriveExecuteActions(actions);

        // (1) Fixed candidate count, none omitted.
        expect(view.controls).toHaveLength(EXECUTE_CANDIDATE_IDS.length);
        const returnedIds = view.controls.map((c) => c.id);
        expect(new Set(returnedIds)).toEqual(new Set(EXECUTE_CANDIDATE_IDS));

        for (const control of view.controls) {
          // (2) enabled mirrors the corresponding can* flag exactly, or is
          // false for every control when actions is undefined.
          const flagKey = EXECUTE_ID_TO_FLAG[control.id];
          const expectedEnabled = actions ? actions[flagKey] : false;
          expect(control.enabled).toBe(expectedEnabled);

          // Never enable a control whose flag is false.
          if (!expectedEnabled) {
            expect(control.enabled).toBe(false);
          }

          // (3) No invoke/callable field exists on the control.
          expect(Object.keys(control)).not.toContain("invoke");
          expect((control as unknown as Record<string, unknown>).invoke).toBeUndefined();
        }

        if (!actions) {
          expect(view.controls.every((c) => c.enabled === false)).toBe(true);
        }
      }),
      { numRuns: 100 },
    );
  });

  it("audit: every candidate is present, enabled mirrors can* exactly, and no invoke field exists (Req 4.1, 4.2, 4.6, 4.8, 4.9, 8.5)", () => {
    fc.assert(
      fc.property(auditActionsOrUndefinedArb, (actions) => {
        const view = deriveAuditActions(actions);

        // (1) Fixed candidate count, none omitted.
        expect(view.controls).toHaveLength(AUDIT_CANDIDATE_IDS.length);
        const returnedIds = view.controls.map((c) => c.id);
        expect(new Set(returnedIds)).toEqual(new Set(AUDIT_CANDIDATE_IDS));

        for (const control of view.controls) {
          // (2) enabled mirrors the corresponding can* flag exactly, or is
          // false for every control when actions is undefined.
          const flagKey = AUDIT_ID_TO_FLAG[control.id];
          const expectedEnabled = actions ? actions[flagKey] : false;
          expect(control.enabled).toBe(expectedEnabled);

          // Never enable a control whose flag is false.
          if (!expectedEnabled) {
            expect(control.enabled).toBe(false);
          }

          // (3) No invoke/callable field exists on the control.
          expect(Object.keys(control)).not.toContain("invoke");
          expect((control as unknown as Record<string, unknown>).invoke).toBeUndefined();
        }

        if (!actions) {
          expect(view.controls.every((c) => c.enabled === false)).toBe(true);
        }
      }),
      { numRuns: 100 },
    );
  });
});

// Feature: run-workbench-refinement, Property 7: For any `RelayExecuteActions`
// or `RelayAuditActions` value, when at least one gating flag is true the
// helper's returned `StepActionsView` designates exactly one control as the
// primary Next_Safe_Action via `isPrimary`/`nextSafeActionId`, that control
// is the first flag-true action in the step's fixed priority order and is
// itself enabled, and identical inputs always yield the same designation;
// when no gating flag is true (or the actions object is `undefined`) no
// control is designated primary.
//
// Validates: Requirements 4.4, 4.5, 4.8, 4.9

// Fixed per-step priority orders (mirrors design.md / runStepActions.ts).
const EXECUTE_PRIORITY_IDS = ["start", "recover", "cancel"];
const AUDIT_PRIORITY_IDS = [
  "closeRun",
  "approveAudit",
  "requestRevision",
  "generateAudit",
  "submitManual",
  "prepareCommitMessage",
];

function expectedPrimaryId(
  actions: RelayExecuteActions | RelayAuditActions | undefined,
  priorityIds: string[],
  idToFlag: Record<string, keyof RelayExecuteActions> | Record<string, keyof RelayAuditActions>,
): string | undefined {
  if (!actions) return undefined;
  for (const id of priorityIds) {
    const flagKey = idToFlag[id as keyof typeof idToFlag];
    if ((actions as unknown as Record<string, unknown>)[flagKey as string]) {
      return id;
    }
  }
  return undefined;
}

describe("deriveExecuteActions / deriveAuditActions — Property 7: deterministic Next_Safe_Action selection by fixed priority", () => {
  it("execute: exactly one primary control chosen by fixed priority order, enabled, and deterministic (Req 4.4, 4.5, 4.8, 4.9)", () => {
    fc.assert(
      fc.property(executeActionsOrUndefinedArb, (actions) => {
        const view = deriveExecuteActions(actions);
        const expectedId = expectedPrimaryId(actions, EXECUTE_PRIORITY_IDS, EXECUTE_ID_TO_FLAG);

        const primaryControls = view.controls.filter((c) => c.isPrimary);

        if (expectedId !== undefined) {
          // Exactly one control designated primary.
          expect(primaryControls).toHaveLength(1);
          expect(primaryControls[0].id).toBe(expectedId);
          expect(view.nextSafeActionId).toBe(expectedId);
          // The primary control is itself enabled.
          expect(primaryControls[0].enabled).toBe(true);
        } else {
          // No gating flag true (or actions undefined): no primary.
          expect(primaryControls).toHaveLength(0);
          expect(view.nextSafeActionId).toBeUndefined();
        }

        // Determinism: calling again with the same input yields the same result.
        const view2 = deriveExecuteActions(actions);
        expect(view2.nextSafeActionId).toBe(view.nextSafeActionId);
      }),
      { numRuns: 100 },
    );
  });

  it("audit: exactly one primary control chosen by fixed priority order, enabled, and deterministic (Req 4.4, 4.5, 4.8, 4.9)", () => {
    fc.assert(
      fc.property(auditActionsOrUndefinedArb, (actions) => {
        const view = deriveAuditActions(actions);
        const expectedId = expectedPrimaryId(actions, AUDIT_PRIORITY_IDS, AUDIT_ID_TO_FLAG);

        const primaryControls = view.controls.filter((c) => c.isPrimary);

        if (expectedId !== undefined) {
          // Exactly one control designated primary.
          expect(primaryControls).toHaveLength(1);
          expect(primaryControls[0].id).toBe(expectedId);
          expect(view.nextSafeActionId).toBe(expectedId);
          // The primary control is itself enabled.
          expect(primaryControls[0].enabled).toBe(true);
        } else {
          // No gating flag true (or actions undefined): no primary.
          expect(primaryControls).toHaveLength(0);
          expect(view.nextSafeActionId).toBeUndefined();
        }

        // Determinism: calling again with the same input yields the same result.
        const view2 = deriveAuditActions(actions);
        expect(view2.nextSafeActionId).toBe(view.nextSafeActionId);
      }),
      { numRuns: 100 },
    );
  });
});

// Feature: run-workbench-refinement, Property 6: For any action whose
// gating flag is false, its control is disabled and displays the matching
// `*UnavailableReason` string when that string is present and non-empty,
// and displays no reason text when the matching reason string is absent or
// empty.
//
// Validates: Requirements 4.3

const EXECUTE_ID_TO_REASON_FIELD: Record<string, keyof RelayExecuteActions> = {
  start: "startUnavailableReason",
  recover: "recoverUnavailableReason",
  cancel: "cancelUnavailableReason",
};

const AUDIT_ID_TO_REASON_FIELD: Record<string, keyof RelayAuditActions> = {
  closeRun: "closeRunUnavailableReason",
  approveAudit: "approveAuditUnavailableReason",
  requestRevision: "requestRevisionUnavailableReason",
  generateAudit: "generateAuditUnavailableReason",
  submitManual: "submitManualUnavailableReason",
  prepareCommitMessage: "prepareCommitMessageUnavailableReason",
};

// Reason value arbitrary that varies across the three cases the task calls
// out explicitly: non-empty string, empty string, and undefined/absent.
const reasonValueArb: fc.Arbitrary<string | undefined> = fc.oneof(
  fc.string({ minLength: 1 }), // non-empty string
  fc.constant(""), // empty string
  fc.constant(undefined), // absent
);

const executeActionsWithReasonsArb: fc.Arbitrary<RelayExecuteActions> = fc.record({
  canStart: fc.boolean(),
  canCancel: fc.boolean(),
  canRecover: fc.boolean(),
  startUnavailableReason: reasonValueArb,
  cancelUnavailableReason: reasonValueArb,
  recoverUnavailableReason: reasonValueArb,
});

const auditActionsWithReasonsArb: fc.Arbitrary<RelayAuditActions> = fc.record({
  canGenerateAudit: fc.boolean(),
  canSubmitManual: fc.boolean(),
  canApproveAudit: fc.boolean(),
  canRequestRevision: fc.boolean(),
  canPrepareCommitMessage: fc.boolean(),
  canCloseRun: fc.boolean(),
  generateAuditUnavailableReason: reasonValueArb,
  submitManualUnavailableReason: reasonValueArb,
  approveAuditUnavailableReason: reasonValueArb,
  requestRevisionUnavailableReason: reasonValueArb,
  prepareCommitMessageUnavailableReason: reasonValueArb,
  closeRunUnavailableReason: reasonValueArb,
});

describe("deriveExecuteActions / deriveAuditActions — Property 6: disabled controls show reason only when present", () => {
  it("execute: disabled controls surface the matching reason iff present and non-empty; enabled controls carry no reason (Req 4.3)", () => {
    fc.assert(
      fc.property(executeActionsWithReasonsArb, (actions) => {
        const view = deriveExecuteActions(actions);

        for (const control of view.controls) {
          const reasonField = EXECUTE_ID_TO_REASON_FIELD[control.id];
          const sourceReason = actions[reasonField];
          const sourceReasonIsPresentAndNonEmpty =
            typeof sourceReason === "string" && sourceReason.length > 0;

          if (!control.enabled) {
            // Disabled control: reason shown iff source reason present/non-empty.
            if (sourceReasonIsPresentAndNonEmpty) {
              expect(control.unavailableReason).toBe(sourceReason);
            } else {
              expect(control.unavailableReason).toBeUndefined();
            }
          } else {
            // Enabled control: never carries a disabled-reason.
            expect(control.unavailableReason).toBeUndefined();
          }
        }
      }),
      { numRuns: 100 },
    );
  });

  it("audit: disabled controls surface the matching reason iff present and non-empty; enabled controls carry no reason (Req 4.3)", () => {
    fc.assert(
      fc.property(auditActionsWithReasonsArb, (actions) => {
        const view = deriveAuditActions(actions);

        for (const control of view.controls) {
          const reasonField = AUDIT_ID_TO_REASON_FIELD[control.id];
          const sourceReason = actions[reasonField];
          const sourceReasonIsPresentAndNonEmpty =
            typeof sourceReason === "string" && sourceReason.length > 0;

          if (!control.enabled) {
            // Disabled control: reason shown iff source reason present/non-empty.
            if (sourceReasonIsPresentAndNonEmpty) {
              expect(control.unavailableReason).toBe(sourceReason);
            } else {
              expect(control.unavailableReason).toBeUndefined();
            }
          } else {
            // Enabled control: never carries a disabled-reason.
            expect(control.unavailableReason).toBeUndefined();
          }
        }
      }),
      { numRuns: 100 },
    );
  });

  it("undefined actions: every control is disabled with no reason text for both execute and audit (Req 4.3)", () => {
    const executeView = deriveExecuteActions(undefined);
    for (const control of executeView.controls) {
      expect(control.enabled).toBe(false);
      expect(control.unavailableReason).toBeUndefined();
    }

    const auditView = deriveAuditActions(undefined);
    for (const control of auditView.controls) {
      expect(control.enabled).toBe(false);
      expect(control.unavailableReason).toBeUndefined();
    }
  });
});

// ============================================================
// Bugfix: step-navigation-missing — Property 2 (Preservation), task 2
// ============================================================
//
// Property 2: Preservation - Existing Navigation and Gated Action Behavior
// Unchanged (design.md). `deriveExecuteActions`/`deriveAuditActions` are
// untouched by the step-navigation-missing fix (navigation moved to
// `IdentityStrip` instead): for randomly generated canStart/canRecover/
// canCancel combinations (execute) and randomly generated
// `RelayAuditActions` combinations (audit), this captures that neither
// helper gained a navigation-only candidate.
//
// Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5

describe("deriveExecuteActions — Preservation: existing start/recover/cancel primary-selection is unchanged (Property 2)", () => {
  const executeGatingWithAtLeastOneTrueArb = executeActionsArb.filter((actions) =>
    Object.values(actions).some(Boolean),
  );

  it("selects the first-enabled-in-priority-order (start -> recover -> cancel) existing candidate as primary, for any gating combination with at least one true flag", () => {
    fc.assert(
      fc.property(executeGatingWithAtLeastOneTrueArb, (actions) => {
        const view = deriveExecuteActions(actions);
        const expectedPrimaryId = EXECUTE_PRIORITY_IDS.find(
          (id) => actions[EXECUTE_ID_TO_FLAG[id]],
        );

        const primaryControls = view.controls.filter((c) => c.isPrimary);
        expect(primaryControls).toHaveLength(1);
        expect(primaryControls[0].id).toBe(expectedPrimaryId);
        expect(view.nextSafeActionId).toBe(expectedPrimaryId);
        expect(primaryControls[0].enabled).toBe(true);

        for (const id of EXECUTE_CANDIDATE_IDS) {
          const control = view.controls.find((c) => c.id === id);
          expect(control?.enabled).toBe(actions[EXECUTE_ID_TO_FLAG[id]]);
        }

        // No navigation-only candidate exists yet on unfixed code.
        expect(view.controls.some((c) => c.id === "proceedToAudit")).toBe(false);
      }),
      { numRuns: 100 },
    );
  });
});

describe("deriveAuditActions — Preservation: audit route output is unchanged, no candidate added (Property 2)", () => {
  it("returns exactly the six existing audit candidates (no new candidate added) for any RelayAuditActions combination", () => {
    fc.assert(
      fc.property(auditActionsOrUndefinedArb, (actions) => {
        const view = deriveAuditActions(actions);

        // Exactly the pre-existing six candidates — no new one added.
        const returnedIds = view.controls.map((c) => c.id);
        expect(new Set(returnedIds)).toEqual(new Set(AUDIT_CANDIDATE_IDS));
        expect(view.controls).toHaveLength(AUDIT_CANDIDATE_IDS.length);

        const expectedPrimaryId = expectedPrimaryId_forAudit(actions);
        const primaryControls = view.controls.filter((c) => c.isPrimary);
        if (expectedPrimaryId !== undefined) {
          expect(primaryControls).toHaveLength(1);
          expect(primaryControls[0].id).toBe(expectedPrimaryId);
        } else {
          expect(primaryControls).toHaveLength(0);
        }
      }),
      { numRuns: 100 },
    );
  });
});

function expectedPrimaryId_forAudit(
  actions: RelayAuditActions | undefined,
): string | undefined {
  if (!actions) return undefined;
  for (const id of AUDIT_PRIORITY_IDS) {
    const flagKey = AUDIT_ID_TO_FLAG[id];
    if (actions[flagKey]) return id;
  }
  return undefined;
}

// ============================================================
// Step navigation fix (step-navigation-missing) — revised approach
// ============================================================
//
// The fix for missing forward/backward step navigation was implemented in
// `IdentityStrip` (every pipeline stage is now a clickable navigation
// control there), not as a per-step `deriveExecuteActions`/
// `buildPrepareActionsView` candidate. `deriveExecuteActions` therefore
// intentionally still returns exactly the three existing execute candidates
// (`start`/`recover`/`cancel`) with no added navigation-only candidate — see
// `IdentityStrip.test.tsx` for the pipeline-navigation property coverage.
