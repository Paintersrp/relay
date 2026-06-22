import { describe, expect, it } from "vitest";

import { getRunPlanContextHrefs, hasRunPlanContext } from "./RunPlanContext";

describe("RunPlanContext helpers", () => {
  it("reports missing context as absent", () => {
    expect(hasRunPlanContext(undefined)).toBe(false);
    expect(hasRunPlanContext({})).toBe(false);
  });

  it("reports plan or pass IDs as context", () => {
    expect(hasRunPlanContext({ planId: "plan-1" })).toBe(true);
    expect(hasRunPlanContext({ passId: "PASS-001" })).toBe(true);
  });

  it("builds plan and pass route params when both IDs are present", () => {
    expect(
      getRunPlanContextHrefs({ planId: "plan-1", passId: "PASS-001" }),
    ).toEqual({
      planTo: "/plans/$planId",
      passTo: "/plans/$planId/passes/$passId",
      planParams: { planId: "plan-1" },
      passParams: { planId: "plan-1", passId: "PASS-001" },
    });
  });
});
