import { describe, expect, it } from "vitest";

import {
  getEditorLineCount,
  getPlanSubmissionPreview,
  parsePlanJson,
} from "./relayPlanSubmissionState";

const canonicalPlan = {
  plan_meta: {
    plan_id: "plan-example",
    schema_version: "1.0.0",
    created_at: "2026-06-21T00:00:00Z",
    title: "Descriptive plan title",
    goal: "High-level goal for this managed plan",
    repo_target: "owner/repo",
    branch_context: "main",
    status: "active",
  },
  source_intent: {
    summary: "Brief description of what this plan achieves.",
  },
  passes: [
    {
      pass_id: "PASS-002",
      sequence: 2,
      name: "Second pass name",
      goal: "Second pass goal",
      intended_execution_scope: ["path/to/second.ts"],
      non_goals: [],
      dependencies: ["PASS-001"],
      status: "planned",
    },
    {
      pass_id: "PASS-001",
      sequence: 1,
      name: "First pass name",
      goal: "Specific goal for this pass",
      intended_execution_scope: ["path/to/file.ts"],
      non_goals: ["Out-of-scope behavior for this pass"],
      dependencies: [],
      status: "planned",
    },
  ],
};

describe("relayPlanSubmissionState", () => {
  it("parses valid canonical JSON", () => {
    const result = parsePlanJson(JSON.stringify(canonicalPlan));

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.plan.plan_meta.plan_id).toBe("plan-example");
    }
  });

  it("returns a root parse issue for invalid JSON", () => {
    const result = parsePlanJson("{");

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.issue.path).toBe("root");
      expect(result.issue.code).toBe("json_parse_error");
    }
  });

  it("rejects visual-target-style camelCase JSON before API validation", () => {
    const result = parsePlanJson(
      JSON.stringify({
        planId: "plan-example",
        repo: "owner/repo",
        branch: "main",
        passes: [{ passId: "PASS-001", executionScope: ["path/to/file.ts"] }],
      }),
    );

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.issue.path).toBe("plan_meta");
    }
  });

  it("derives preview data from canonical fields", () => {
    const result = parsePlanJson(JSON.stringify(canonicalPlan));
    expect(result.ok).toBe(true);
    if (!result.ok) return;

    const preview = getPlanSubmissionPreview(result.plan);

    expect(preview.planId).toBe("plan-example");
    expect(preview.title).toBe("Descriptive plan title");
    expect(preview.repoTarget).toBe("owner/repo");
    expect(preview.branchContext).toBe("main");
    expect(preview.passCount).toBe(2);
    expect(preview.dependencyCount).toBe(1);
    expect(preview.passes.map((pass) => pass.passId)).toEqual([
      "PASS-001",
      "PASS-002",
    ]);
  });

  it("counts editor lines with a minimum of one", () => {
    expect(getEditorLineCount("")).toBe(1);
    expect(getEditorLineCount("one\ntwo\nthree")).toBe(3);
  });
});
