import { describe, expect, it } from "vitest";

import type { PlanAPIPass } from "@/features/relay-plans";
import { summarizePlanPassContext } from "./PlanPassContextPanel";

function buildPass(partial: Partial<PlanAPIPass>): PlanAPIPass {
  return {
    id: "1",
    planRowId: "10",
    passId: "PASS-009",
    sequence: 9,
    name: "Source visibility",
    goal: "Expose source metadata",
    intendedExecutionScope: [],
    nonGoals: [],
    dependencies: [],
    status: "planned",
    associatedRunIds: [],
    associatedRuns: [],
    createdAt: "2026-06-23T00:00:00Z",
    updatedAt: "2026-06-23T00:00:00Z",
    ...partial,
  };
}

describe("summarizePlanPassContext", () => {
  it("counts pass context coverage fields", () => {
    const summary = summarizePlanPassContext(
      buildPass({
        contextPlan: {
          requiredRepositories: ["relay", "relay-contracts"],
          seedSearchTerms: [
            { repoId: "relay", query: "context packet", purpose: "Locate UI" },
          ],
          seedFilesToRead: [
            {
              repoId: "relay",
              path: "apps/web/src/routes/runs/$runId/intake.tsx",
              purpose: "Add panel",
            },
          ],
          contextCoverageExpectations: ["Metadata only"],
          blockedIfMissing: ["No persisted provenance"],
        },
        handoffReadinessCriteria: ["Source metadata is visible"],
      }),
    );

    expect(summary).toEqual({
      requiredRepositoryCount: 2,
      seedSearchCount: 1,
      seedFileCount: 1,
      readinessCriteriaCount: 1,
      blockedIfMissingCount: 1,
    });
  });
});
