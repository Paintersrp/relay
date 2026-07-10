import { describe, expect, it } from "vitest";

import {
  buildAttentionInputs,
  buildRunHierarchy,
  buildSearchCorpus,
  type ShellPlanLike,
  type ShellProjectLike,
  type ShellRunLike,
} from "./useShellData";
import { selectAttentionRuns } from "./attention";

describe("canonical shell composition", () => {
  it("uses canonical Project, Plan, pass, and Run identities without legacy lifecycle fields", () => {
    const projects: ShellProjectLike[] = [
      {
        projectId: "project-1",
        name: "Relay",
        updatedAt: "2026-07-08T00:00:00Z",
      },
    ];
    const plans: ShellPlanLike[] = [
      {
        planId: "plan-1",
        title: "feature",
        projectId: "project-1",
        projectName: "Relay",
        updatedAt: "2026-07-08T00:00:00Z",
      },
    ];
    const runs: ShellRunLike[] = [
      {
        id: "run-1",
        title: "feature",
        status: "audit_ready",
        updatedAt: "2026-07-08T00:00:00Z",
        projectId: "project-1",
        planId: "plan-1",
        passId: "pass-1",
        passNumber: 1,
      },
    ];

    const corpus = buildSearchCorpus({
      projects,
      plans,
      runs,
      passesByPlanId: {
        "plan-1": [{ passId: "pass-1", name: "Frontend", sequence: 1 }],
      },
    });

    expect(corpus.map((item) => item.type)).toEqual([
      "project",
      "plan",
      "pass",
      "run",
    ]);
    expect(selectAttentionRuns(buildAttentionInputs(runs)).items).toHaveLength(1);
  });

  it("resolves archived Managed Run Project context through the canonical Run projection", () => {
    const hierarchy = buildRunHierarchy(
      {
        runId: "run-1",
        featureSlug: "feature",
        repoTarget: "relay",
        status: "setup_ready",
        stage: "specification",
        branch: "feat/simplification",
        baseCommit: "a".repeat(40),
        canonicalSha256: "b".repeat(64),
        planId: "plan-1",
        passId: "pass-1",
        passNumber: 1,
        project: {
          projectId: "project-archived",
          name: "Archived Project",
          status: "archived",
        },
        createdAt: "2026-07-08T00:00:00Z",
        updatedAt: "2026-07-08T00:00:00Z",
      },
      [],
      [],
    );

    expect(hierarchy.project).toEqual({
      id: "project-archived",
      label: "Archived Project",
    });
    expect(hierarchy.plan?.id).toBe("plan-1");
    expect(hierarchy.pass?.sequence).toBe(1);
  });
});
