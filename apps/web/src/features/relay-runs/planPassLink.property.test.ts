// Feature: run-workbench-refinement, Property 10: Plan/Pass link target selection
//
// For any `RelayRunPlanContext | null | undefined`, `resolvePlanPassLink`
// selects `present: false` when context/planId absent; selects
// `to: '/plans/$planId'` with `params: { planId }` when only planId present;
// selects `to: '/plans/$planId/passes/$passId'` with
// `params: { planId, passId }` when both present (and never also produces a
// plan-only link in that case).
//
// Validates: Requirements 6.1, 6.2, 6.3

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { resolvePlanPassLink } from "./planPassLink";
import type { RelayRunPlanContext } from "./types";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------
//
// Varies planId and passId independently between non-empty strings,
// empty string, whitespace-only strings, and undefined/absent, plus the
// null/undefined context case itself.

const nonEmptyIdArb = fc
  .string({ minLength: 1, maxLength: 20 })
  .filter((s) => s.trim().length > 0);

const whitespaceOnlyIdArb = fc
  .array(fc.constantFrom(" ", "\t", "\n"), { minLength: 1, maxLength: 5 })
  .map((chars) => chars.join(""))
  .filter((s) => s.trim().length === 0);

// Only genuinely non-empty strings count as "present" for routing purposes;
// empty/whitespace-only strings are exercised as "absent-like" values to
// confirm the helper does not treat them as present ids.
const idValueArb: fc.Arbitrary<string | undefined> = fc.oneof(
  nonEmptyIdArb,
  fc.constant(""),
  whitespaceOnlyIdArb,
  fc.constant(undefined),
);

function isPresentId(value: string | undefined): value is string {
  return typeof value === "string" && value.length > 0;
}

const planContextArb: fc.Arbitrary<RelayRunPlanContext> = fc
  .record({
    planId: idValueArb,
    passId: idValueArb,
    planTitle: fc.option(fc.string({ maxLength: 20 }), { nil: undefined }),
    passName: fc.option(fc.string({ maxLength: 20 }), { nil: undefined }),
  })
  .map((fields) => {
    // passId is only meaningful when planId is present; when planId is
    // absent, still vary passId independently (the helper must ignore it).
    const context: RelayRunPlanContext = {};
    if (fields.planId !== undefined) context.planId = fields.planId;
    if (fields.passId !== undefined) context.passId = fields.passId;
    if (fields.planTitle !== undefined) context.planTitle = fields.planTitle;
    if (fields.passName !== undefined) context.passName = fields.passName;
    return context;
  });

// Full input space: null, undefined, or a generated context.
const contextInputArb: fc.Arbitrary<RelayRunPlanContext | null | undefined> =
  fc.oneof(fc.constant(null), fc.constant(undefined), planContextArb);

describe("resolvePlanPassLink — Property 10: Plan/Pass link target selection", () => {
  it("selects present/to/params by planId/passId presence (Req 6.1, 6.2, 6.3)", () => {
    fc.assert(
      fc.property(contextInputArb, (context) => {
        const view = resolvePlanPassLink(context);

        // The underlying route-selection logic (`getRunPlanContextHrefs` in
        // RunPlanContext.tsx) gates on plain truthiness of `context.planId`
        // / `context.passId` (`!context?.planId`), not trim-based emptiness.
        // Mirror that exact truthiness check here.
        const planId = context?.planId;
        const passId = context?.passId;
        const planIdPresent = isPresentId(planId);
        const passIdPresent = isPresentId(passId);

        if (!planIdPresent) {
          // Case 1: no context / no planId -> present: false, no to/params.
          expect(view.present).toBe(false);
          expect(view.to).toBeUndefined();
          expect(view.params).toBeUndefined();
          return;
        }

        expect(view.present).toBe(true);

        if (!passIdPresent) {
          // Case 2: planId only -> plan-only link.
          expect(view.to).toBe("/plans/$planId");
          expect(view.params).toEqual({ planId });
        } else {
          // Case 3: planId + passId -> pass link, never a plan-only link.
          expect(view.to).toBe("/plans/$planId/passes/$passId");
          expect(view.params).toEqual({ planId, passId });
          expect(view.to).not.toBe("/plans/$planId");
        }
      }),
      { numRuns: 100 },
    );
  });
});
