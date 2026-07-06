import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";

import { RelayApiError } from "@/features/relay-runs";
import {
  getPlan,
  getPlanPass,
  getPlans,
  submitPlan,
  validatePlan,
} from "./api";

describe("relay-plans api", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterAll(() => {
    globalThis.fetch = originalFetch;
  });

  it("getPlans({ limit: 100 }) calls /api/plans with the limit query param", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ success: true, count: 0, plans: [] }),
    });
    globalThis.fetch = fetchSpy;

    await getPlans({ limit: 100 });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain("/api/plans?limit=100");
  });

  it("getPlans({ status: 'active', limit: 50 }) calls /api/plans with both query params", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ success: true, count: 0, plans: [] }),
    });
    globalThis.fetch = fetchSpy;

    await getPlans({ status: "active", limit: 50 });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain("/api/plans?status=active&limit=50");
  });

  it("getPlans() calls /api/plans without a query string", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ success: true, count: 0, plans: [] }),
    });
    globalThis.fetch = fetchSpy;

    await getPlans();

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toMatch(/\/api\/plans$/);
  });

  it("getPlan encodes the plan id", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          success: true,
          plan: { id: "1", planId: "plan/a b" },
          passes: [],
          completionReady: false,
        }),
    });
    globalThis.fetch = fetchSpy;

    await getPlan("plan/a b");

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain("/api/plans/plan%2Fa%20b");
  });

  it("getPlanPass calls the pass detail endpoint", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          success: true,
          plan: { id: "1", planId: "plan-1" },
          pass: { id: "2", passId: "PASS-001" },
          completionReady: false,
        }),
    });
    globalThis.fetch = fetchSpy;

    await getPlanPass("plan-1", "PASS-001");

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain(
      "/api/plans/plan-1/passes/PASS-001",
    );
  });

  it("getPlan normalizes pass context fields from snake_case payloads", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          success: true,
          plan: { id: "1", planId: "plan-1" },
          passes: [
            {
              id: "2",
              plan_row_id: "1",
              pass_id: "PASS-009",
              sequence: 9,
              name: "Source visibility",
              goal: "Expose source visibility",
              intended_execution_scope: ["apps/web"],
              non_goals: ["No workflow changes"],
              dependencies: ["PASS-008"],
              status: "planned",
              associated_runs: [],
              pass_type: "ui_visibility",
              context_plan: {
                required_repositories: ["relay", "relay-specs"],
                seed_search_terms: [
                  {
                    repo_id: "relay",
                    query: "context packet",
                    purpose: "Locate source visibility surfaces",
                    required: true,
                  },
                ],
                seed_files_to_read: [
                  {
                    repo_id: "relay",
                    path: "apps/web/src/routes/runs/$runId/intake.tsx",
                    purpose: "Add panel",
                    required: true,
                  },
                ],
                context_coverage_expectations: ["Show metadata only"],
                blocked_if_missing: ["No persisted provenance"],
              },
              source_snapshot_requirements: {
                require_git_status: true,
                require_commit_sha: false,
                allow_dirty_worktree: true,
              },
              handoff_readiness_criteria: ["Source metadata is visible"],
              context_budget: {
                max_files: 8,
                max_search_results: 12,
              },
              context_parse_warnings: ["example warning"],
            },
          ],
          completionReady: false,
        }),
    });
    globalThis.fetch = fetchSpy;

    const response = await getPlan("plan-1");

    expect(response.passes[0]).toMatchObject({
      passType: "ui_visibility",
      contextPlan: {
        requiredRepositories: ["relay", "relay-specs"],
        seedSearchTerms: [{ repoId: "relay", query: "context packet" }],
        seedFilesToRead: [
          { repoId: "relay", path: "apps/web/src/routes/runs/$runId/intake.tsx" },
        ],
        contextCoverageExpectations: ["Show metadata only"],
        blockedIfMissing: ["No persisted provenance"],
      },
      sourceSnapshotRequirements: {
        requireGitStatus: true,
        requireCommitSha: false,
        allowDirtyWorktree: true,
      },
      handoffReadinessCriteria: ["Source metadata is visible"],
      contextBudget: {
        maxFiles: 8,
        maxSearchResults: 12,
      },
      contextParseWarnings: ["example warning"],
    });
  });

  it("validatePlan posts plan JSON to /api/plans/validate", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          success: true,
          validation: { valid: true, issues: [] },
        }),
    });
    globalThis.fetch = fetchSpy;

    await validatePlan({
      plan: {
        plan_meta: {
          plan_id: "plan-1",
          schema_version: "1.0.0",
          created_at: "2026-06-21T00:00:00Z",
          title: "Plan",
          goal: "Goal",
          repo_target: "Paintersrp/relay",
          branch_context: "main",
          status: "active",
        },
        source_intent: { summary: "Summary" },
        passes: [],
      },
    });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain("/api/plans/validate");
    expect(fetchSpy.mock.calls[0][1]).toMatchObject({
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
      },
    });
    expect(fetchSpy.mock.calls[0][1]?.body).toContain('"plan_id":"plan-1"');
  });

  it("submitPlan posts plan JSON to /api/plans", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          success: true,
          plan: { id: "1", planId: "plan-1" },
          passes: [],
          validation: { valid: true, issues: [] },
        }),
    });
    globalThis.fetch = fetchSpy;

    await submitPlan({
      plan: {
        plan_meta: {
          plan_id: "plan-1",
          schema_version: "1.0.0",
          created_at: "2026-06-21T00:00:00Z",
          title: "Plan",
          goal: "Goal",
          repo_target: "Paintersrp/relay",
          branch_context: "main",
          status: "active",
        },
        source_intent: { summary: "Summary" },
        passes: [],
      },
    });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toMatch(/\/api\/plans$/);
    expect(fetchSpy.mock.calls[0][1]?.method).toBe("POST");
  });

  it("non-OK responses throw RelayApiError", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: false,
      status: 422,
      text: async () =>
        JSON.stringify({
          error: "validation_failed",
          message: "Plan invalid",
        }),
    });
    globalThis.fetch = fetchSpy;

    await expect(
      submitPlan({
        plan: {
          plan_meta: {
            plan_id: "plan-1",
            schema_version: "1.0.0",
            created_at: "2026-06-21T00:00:00Z",
            title: "Plan",
            goal: "Goal",
            repo_target: "Paintersrp/relay",
            branch_context: "main",
            status: "active",
          },
          source_intent: { summary: "Summary" },
          passes: [],
        },
      }),
    ).rejects.toThrow(RelayApiError);
  });

  it("malformed JSON success responses throw RelayApiError", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: async () => "{not json",
    });
    globalThis.fetch = fetchSpy;

    await expect(getPlans()).rejects.toThrow(RelayApiError);
  });
});
