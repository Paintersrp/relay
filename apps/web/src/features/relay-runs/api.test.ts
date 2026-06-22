import { describe, it, expect, vi, beforeEach, afterAll } from 'vitest';
import {
  getRun,
  getRuns,
  validateRun,
  RelayApiError,
  submitPlannerHandoff,
} from './api';
import { runsListQueryOptions, runDetailQueryOptions } from './queries';

describe('Relay API Client Boundary (Real Data Wiring)', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterAll(() => {
    globalThis.fetch = originalFetch;
  });

  it("getRun('58') issues GET /api/runs/58 and does not call mock data", async () => {
    const mockRun = { id: 58, name: 'Real Run 58', status: 'intake_needs_review' };
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      text: async () => JSON.stringify(mockRun),
    });
    globalThis.fetch = fetchSpy;

    const run = await getRun('58');

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain('/api/runs/58');
    expect(run).toBeDefined();
    expect(run?.id).toBe('58');
    expect(run?.name).toBe('Real Run 58');
  });

  it("GET /api/runs/58 returning 404 surfaces a RelayApiError instead of mock/null fallback", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
    });
    globalThis.fetch = fetchSpy;

    await expect(getRun('58')).rejects.toThrow(RelayApiError);
  });

  it("GET /api/runs network failure surfaces a RelayApiError instead of mock runs", async () => {
    const fetchSpy = vi.fn().mockRejectedValue(new Error('Network offline'));
    globalThis.fetch = fetchSpy;

    await expect(getRuns()).rejects.toThrow(RelayApiError);
  });

  it("runsListQueryOptions queryFn uses getRuns which queries the real backend", async () => {
    const mockRuns = [{ id: 12, name: 'Backend Run' }];
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      text: async () => JSON.stringify(mockRuns),
    });
    globalThis.fetch = fetchSpy;

    const queryFn = runsListQueryOptions.queryFn;
    const result = await queryFn!({} as any);

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain('/api/runs');
    expect(result[0].id).toBe('12');
    expect(result[0].name).toBe('Backend Run');
  });

  it("runDetailQueryOptions queryFn uses getRun which queries the real backend for numeric ID", async () => {
    const mockRun = { id: 58, name: 'Backend Run 58' };
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      text: async () => JSON.stringify(mockRun),
    });
    globalThis.fetch = fetchSpy;

    const queryOpts = runDetailQueryOptions('58');
    const result = await queryOpts.queryFn!({} as any);

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain('/api/runs/58');
    expect(result?.id).toBe('58');
  });

  it("validateRun('58') issues POST /api/runs/58/validate", async () => {
    const mockResponse = { success: true, runId: '58', status: 'passed' };
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      text: async () => JSON.stringify(mockResponse),
    });
    globalThis.fetch = fetchSpy;

    const res = await validateRun('58');

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toContain('/api/runs/58/validate');
    expect(fetchSpy.mock.calls[0][1]?.method).toBe('POST');
    expect(res.success).toBe(true);
    expect(res.status).toBe('passed');
  });

  it("submitPlannerHandoff rejects passId without planId before fetch", async () => {
    const fetchSpy = vi.fn();
    globalThis.fetch = fetchSpy;

    await expect(
      submitPlannerHandoff({
        planner_handoff_markdown: "# handoff",
        passId: "UI-PLAN-01",
      }),
    ).rejects.toMatchObject({
      name: "RelayApiError",
      status: 400,
      endpoint: "/api/intake/planner-handoff",
      method: "POST",
    });

    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("submitPlannerHandoff accepts planId with passId and posts association fields", async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      text: async () =>
        JSON.stringify({
          success: true,
          runId: "42",
          status: "intake_received",
          lifecycleState: "intake",
          createdAt: "2026-06-21T00:00:00Z",
        }),
    });
    globalThis.fetch = fetchSpy;

    await submitPlannerHandoff({
      planner_handoff_markdown: "# handoff",
      planId: "plan-ui-04",
      passId: "pass-detail",
      plan_id: "plan-ui-04",
      pass_id: "pass-detail",
    });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const request = fetchSpy.mock.calls[0][1];
    expect(request?.method).toBe("POST");
    expect(JSON.parse(String(request?.body))).toMatchObject({
      planId: "plan-ui-04",
      passId: "pass-detail",
      plan_id: "plan-ui-04",
      pass_id: "pass-detail",
    });
  });
});
