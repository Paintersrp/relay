// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  ACTIVE_ATTEMPT_REFRESH_MS,
  RelayCanonicalRunWorkbench,
} from "./RelayCanonicalRunWorkbench";

const mocks = vi.hoisted(() => ({
  getRun: vi.fn(),
  getSpecification: vi.fn(),
  getAttempt: vi.fn(),
  getAuditStatus: vi.fn(),
  startAttempt: vi.fn(),
  cancelAttempt: vi.fn(),
  reconcileAttempt: vi.fn(),
  prepareAudit: vi.fn(),
  link: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, onClick, params: _params, ...props }: any) => (
    <a
      href="#"
      data-to={to}
      onClick={(event) => {
        onClick?.(event);
        if (!event.defaultPrevented) mocks.link(to);
      }}
      {...props}
    >
      {children}
    </a>
  ),
  Navigate: (props: unknown) => {
    mocks.navigate(props);
    return <div data-testid="run-stage-redirect" />;
  },
}));

vi.mock("@/features/relay-runs", () => ({
  workflowRunDetailQueryOptions: (runId: string) => ({
    queryKey: ["workflow-runs", "detail", runId],
    queryFn: mocks.getRun,
    retry: false,
  }),
  workflowSpecificationQueryOptions: (runId: string) => ({
    queryKey: ["workflow-runs", "detail", runId, "specification"],
    queryFn: mocks.getSpecification,
    retry: false,
  }),
  workflowAttemptQueryOptions: (runId: string, attemptId: string) => ({
    queryKey: ["workflow-runs", "detail", runId, "attempt", attemptId],
    queryFn: mocks.getAttempt,
    retry: false,
  }),
  workflowAuditStatusQueryOptions: (runId: string) => ({
    queryKey: ["workflow-runs", "detail", runId, "audit"],
    queryFn: mocks.getAuditStatus,
    retry: false,
  }),
  startWorkflowAttempt: mocks.startAttempt,
  cancelWorkflowAttempt: mocks.cancelAttempt,
  reconcileWorkflowAttempt: mocks.reconcileAttempt,
  prepareWorkflowAudit: mocks.prepareAudit,
  workflowApiUrl: (path: string) => `http://localhost:8080${path}`,
  workflowRunKeys: {
    detail: (runId: string) => ["workflow-runs", "detail", runId],
    attempt: (runId: string, attemptId: string) => [
      "workflow-runs",
      "detail",
      runId,
      "attempt",
      attemptId,
    ],
    audit: (runId: string) => ["workflow-runs", "detail", runId, "audit"],
  },
  workflowRunStageRoute: (stage: string) => `/runs/$runId/${stage}`,
  EXECUTOR_ADAPTER_OPTIONS: [
    { value: "codex", label: "Codex", description: "Codex" },
  ],
  getDefaultModelForAdapter: () => "gpt-5.5",
  getModelOptionsForAdapter: () => [
    { value: "gpt-5.5", label: "GPT 5.5" },
  ],
}));

type RunStage = "specification" | "execute" | "audit";
type RunStatus =
  | "created"
  | "setup_ready"
  | "executing"
  | "execution_failed"
  | "cancelled"
  | "validating"
  | "validation_failed"
  | "audit_ready"
  | "needs_revision"
  | "completed";
type AttemptStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "failed"
  | "cancelled"
  | "timed_out";

function makeRun(
  status: RunStatus = "setup_ready",
  stage: RunStage = "specification",
) {
  return {
    runId: "run-1",
    featureSlug: "feature",
    repoTarget: "relay",
    status,
    stage,
    branch: "feat/simplification",
    baseCommit: "a".repeat(40),
    canonicalSha256: "b".repeat(64),
    createdAt: "2026-07-08T00:00:00Z",
    updatedAt: "2026-07-08T00:00:00Z",
  };
}

function makeSummary(
  status: AttemptStatus = "running",
  overrides: Record<string, unknown> = {},
) {
  return {
    attemptId: "attempt-1",
    attemptNumber: 1,
    adapter: "codex",
    model: "gpt-5.5",
    status,
    createdAt: "2026-07-08T00:00:00Z",
    artifacts: [],
    ...overrides,
  };
}

function makeDetailedAttempt(
  status: AttemptStatus = "running",
  overrides: Record<string, unknown> = {},
) {
  return {
    ...makeSummary(status),
    runId: "run-1",
    result: {},
    artifacts: [
      {
        artifactId: "artifact-1",
        kind: "executor_stdout",
        mediaType: "text/plain",
        sha256: "c".repeat(64),
        sizeBytes: 14,
        createdAt: "2026-07-08T00:00:00Z",
      },
    ],
    liveStdout: "current output",
    liveStderr: "",
    liveStdoutTruncated: false,
    liveStderrTruncated: false,
    liveStdoutBytes: 14,
    liveStderrBytes: 0,
    ...overrides,
  };
}

function makeDetail(
  run = makeRun(),
  attempts: ReturnType<typeof makeSummary>[] = [],
) {
  return {
    run,
    attempts,
    artifacts: [],
  };
}

function renderWorkbench(stage: RunStage) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const renderResult = render(
    <QueryClientProvider client={queryClient}>
      <RelayCanonicalRunWorkbench runId="run-1" stage={stage} />
    </QueryClientProvider>,
  );
  return { ...renderResult, queryClient };
}

describe("RelayCanonicalRunWorkbench canonical lifecycle and navigation", () => {
  beforeEach(() => {
    mocks.getRun.mockReset();
    mocks.getSpecification.mockReset();
    mocks.getAttempt.mockReset();
    mocks.getAuditStatus.mockReset();
    mocks.startAttempt.mockReset();
    mocks.cancelAttempt.mockReset();
    mocks.reconcileAttempt.mockReset();
    mocks.prepareAudit.mockReset();
    mocks.link.mockReset();
    mocks.getRun.mockResolvedValue(makeDetail());
    mocks.getAuditStatus.mockResolvedValue({
      runId: "run-1",
      runStatus: "audit_ready",
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it.each([
    ["setup_ready", "specification"],
    ["execution_failed", "execute"],
    ["cancelled", "execute"],
  ] as const)(
    "enables Start only for the backend-supported %s lifecycle",
    async (status, durableStage) => {
      mocks.getRun.mockResolvedValue(
        makeDetail(makeRun(status, durableStage)),
      );

      renderWorkbench("execute");

      expect(
        await screen.findByRole("button", { name: "Start attempt" }),
      ).toBeEnabled();
    },
  );

  it.each([
    ["executing", "execute"],
    ["validating", "audit"],
    ["audit_ready", "audit"],
    ["completed", "audit"],
  ] as const)(
    "disables Start for the ineligible %s lifecycle",
    async (status, durableStage) => {
      mocks.getRun.mockResolvedValue(
        makeDetail(makeRun(status, durableStage)),
      );

      renderWorkbench("execute");

      expect(
        await screen.findByRole("button", { name: "Start attempt" }),
      ).toBeDisabled();
    },
  );

  it("disables Start for the created lifecycle at specification stage", async () => {
    mocks.getRun.mockResolvedValue(
      makeDetail(makeRun("created", "specification")),
    );
    mocks.getSpecification.mockResolvedValue({
      run: makeRun("created", "specification"),
      executionSpec: { artifactId: "spec-art", kind: "execution_spec", mediaType: "application/json", sha256: "spec-sha", sizeBytes: 10, contentUrl: "/spec", createdAt: "2026" },
      executorBrief: { artifactId: "brief-art", kind: "executor_brief", mediaType: "application/json", sha256: "brief-sha", sizeBytes: 10, contentUrl: "/brief", createdAt: "2026" },
    });

    renderWorkbench("specification");

    // At specification stage, the Execute panel is not rendered, so no Start button
    expect(
      await screen.findByText("Canonical execution inputs"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Start attempt" }),
    ).not.toBeInTheDocument();
  });

  it("allows active cancellation but does not expose generic reconciliation", async () => {
    const user = userEvent.setup();
    const summary = makeSummary("running");
    const detailed = makeDetailedAttempt("running");
    mocks.getRun.mockResolvedValue(
      makeDetail(makeRun("executing", "execute"), [summary]),
    );
    mocks.getAttempt.mockResolvedValue(detailed);
    mocks.cancelAttempt.mockResolvedValue({
      ...detailed,
      cancellationRequestedAt: "2026-07-08T00:01:00Z",
    });

    renderWorkbench("execute");

    const cancel = await screen.findByRole("button", { name: "Cancel" });
    expect(cancel).toBeEnabled();
    expect(
      screen.queryByRole("button", { name: "Reconcile cleanup" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start attempt" })).toBeDisabled();

    await user.click(cancel);

    await waitFor(() => {
      expect(mocks.cancelAttempt).toHaveBeenCalledWith("run-1", "attempt-1");
    });
  });

  it("exposes reconciliation only for a nonterminal cleanup-pending attempt", async () => {
    const user = userEvent.setup();
    const summary = makeSummary("running", {
      cancellationRequestedAt: "2026-07-08T00:01:00Z",
    });
    const cleanupPending = makeDetailedAttempt("running", {
      cancellationRequestedAt: "2026-07-08T00:01:00Z",
      result: {
        cleanup_pending: true,
        pending_terminal_status: "cancelled",
        termination_verified: false,
      },
    });
    mocks.getRun.mockResolvedValue(
      makeDetail(makeRun("executing", "execute"), [summary]),
    );
    mocks.getAttempt.mockResolvedValue(cleanupPending);
    mocks.reconcileAttempt.mockResolvedValue(
      makeDetailedAttempt("cancelled", {
        finishedAt: "2026-07-08T00:02:00Z",
        result: {
          cleanup_pending: false,
          termination_verified: true,
        },
      }),
    );

    renderWorkbench("execute");

    const reconcile = await screen.findByRole("button", {
      name: "Reconcile cleanup",
    });
    expect(reconcile).toBeEnabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(
      screen.getByText(/Durable process cleanup is pending/),
    ).toBeInTheDocument();

    await user.click(reconcile);

    await waitFor(() => {
      expect(mocks.reconcileAttempt).toHaveBeenCalledWith(
        "run-1",
        "attempt-1",
      );
    });
  });

  it("disables terminal attempt controls while retaining an eligible retry", async () => {
    const summary = makeSummary("failed", {
      finishedAt: "2026-07-08T00:02:00Z",
    });
    mocks.getRun.mockResolvedValue(
      makeDetail(makeRun("execution_failed", "execute"), [summary]),
    );
    mocks.getAttempt.mockResolvedValue(
      makeDetailedAttempt("failed", {
        finishedAt: "2026-07-08T00:02:00Z",
      }),
    );

    renderWorkbench("execute");

    expect(await screen.findByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Reconcile cleanup" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start attempt" })).toBeEnabled();
  });

  it("loads detailed output and reduced artifacts for an existing attempt summary", async () => {
    const summary = makeSummary("running");
    mocks.getRun.mockResolvedValue(
      makeDetail(makeRun("executing", "execute"), [summary]),
    );
    mocks.getAttempt.mockResolvedValue(makeDetailedAttempt("running"));

    renderWorkbench("execute");

    expect(await screen.findByText("current output")).toBeInTheDocument();
    expect(mocks.getAttempt).toHaveBeenCalledTimes(1);
    expect(screen.getByText("executor_stdout")).toBeInTheDocument();
    expect(screen.queryByText("No captured output.")).not.toBeInTheDocument();
  });

  it("refreshes every nonterminal detailed attempt and stops at an exact terminal status", async () => {
    vi.useFakeTimers();
    const summary = makeSummary("pending");
    mocks.getRun.mockResolvedValue(
      makeDetail(makeRun("executing", "execute"), [summary]),
    );
    mocks.getAttempt
      .mockResolvedValueOnce(makeDetailedAttempt("pending"))
      .mockResolvedValueOnce(
        makeDetailedAttempt("running", {
          liveStdout: "running output",
          liveStdoutBytes: 14,
        }),
      )
      .mockResolvedValueOnce(
        makeDetailedAttempt("succeeded", {
          liveStdout: "terminal output",
          liveStdoutBytes: 15,
          finishedAt: "2026-07-08T00:03:00Z",
        }),
      );

    renderWorkbench("execute");
    
    // Advance timers in small increments to let sequential React Query promises resolve
    for (let i = 0; i < 5; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10);
      });
    }

    expect(screen.getByLabelText("Attempt output")).toHaveTextContent(
      "current output",
    );

    // Advance time to trigger the second call (pending -> running)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ACTIVE_ATTEMPT_REFRESH_MS);
    });
    // Flush microtasks for query resolution
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByLabelText("Attempt output")).toHaveTextContent(
      "running output",
    );

    // Advance time to trigger the third call (running -> succeeded)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ACTIVE_ATTEMPT_REFRESH_MS);
    });
    // Flush microtasks for query resolution
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByLabelText("Attempt output")).toHaveTextContent(
      "terminal output",
    );
    expect(mocks.getAttempt).toHaveBeenCalledTimes(3);
    expect(mocks.startAttempt).not.toHaveBeenCalled();

    // Advance time again to confirm it stopped polling (succeeded is terminal)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ACTIVE_ATTEMPT_REFRESH_MS * 2);
    });
    expect(mocks.getAttempt).toHaveBeenCalledTimes(3);
  });

  it("refreshes durable Run state after a naturally failed attempt and clears the stale active summary", async () => {
    const runningSummary = makeSummary("running");
    const failedSummary = makeSummary("failed", {
      finishedAt: "2026-07-08T00:03:00Z",
    });
    mocks.getRun
      .mockResolvedValueOnce(
        makeDetail(makeRun("executing", "execute"), [runningSummary]),
      )
      .mockResolvedValueOnce(
        makeDetail(makeRun("execution_failed", "execute"), [failedSummary]),
      );
    mocks.getAttempt.mockResolvedValue(makeDetailedAttempt("running"));

    const { queryClient } = renderWorkbench("execute");
    expect(await screen.findByText("current output")).toBeInTheDocument();

    act(() => {
      queryClient.setQueryData(
        ["workflow-runs", "detail", "run-1", "attempt", "attempt-1"],
        makeDetailedAttempt("failed", {
          finishedAt: "2026-07-08T00:03:00Z",
        }),
      );
    });

    await waitFor(() => expect(mocks.getRun).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Start attempt" })).toBeEnabled(),
    );
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  });

  it("refreshes durable Run state after natural success and exposes the Audit stage", async () => {
    const runningSummary = makeSummary("running");
    const succeededSummary = makeSummary("succeeded", {
      finishedAt: "2026-07-08T00:03:00Z",
    });
    mocks.getRun
      .mockResolvedValueOnce(
        makeDetail(makeRun("executing", "execute"), [runningSummary]),
      )
      .mockResolvedValueOnce(
        makeDetail(makeRun("audit_ready", "audit"), [succeededSummary]),
      );
    mocks.getAttempt.mockResolvedValue(makeDetailedAttempt("running"));

    const { queryClient } = renderWorkbench("execute");
    expect(await screen.findByText("current output")).toBeInTheDocument();

    act(() => {
      queryClient.setQueryData(
        ["workflow-runs", "detail", "run-1", "attempt", "attempt-1"],
        makeDetailedAttempt("succeeded", {
          finishedAt: "2026-07-08T00:03:00Z",
        }),
      );
    });

    await waitFor(() => expect(mocks.getRun).toHaveBeenCalledTimes(2));
    const navigation = screen.getByRole("navigation", { name: "Run stages" });
    await waitFor(() =>
      expect(within(navigation).getByRole("link", { name: "Audit" })).not.toHaveAttribute(
        "aria-disabled",
        "true",
      ),
    );
  });

  it.each([
    ["audit", "setup_ready", "specification", "execute"],
    ["audit", "executing", "execute", "execute"],
  ] as const)(
    "redirects direct %s access when the durable lifecycle is %s",
    async (requestedStage, status, runStage, expectedRedirectStage) => {
      mocks.getRun.mockResolvedValueOnce(
        makeDetail(makeRun(status, runStage)),
      );

      renderWorkbench(requestedStage);

      await waitFor(() => {
        const calls = mocks.navigate.mock.calls;
        expect(calls.length).toBeGreaterThan(0);
        const lastCall = calls[calls.length - 1];
        expect(lastCall).toEqual([{
          to: `/runs/$runId/${expectedRedirectStage}`,
          params: { runId: "run-1" },
          replace: true,
        }]);
      });
    },
  );



  it("uses the selected route only for current presentation and returns by keyboard to the durable Execute stage", async () => {
    const user = userEvent.setup();
    mocks.getRun.mockResolvedValue(
      makeDetail(makeRun("executing", "execute")),
    );
    mocks.getSpecification.mockResolvedValue({
      run: makeRun("executing", "execute"),
      executionSpec: { artifactId: "spec-art", kind: "execution_spec", mediaType: "application/json", sha256: "spec-sha", sizeBytes: 10, contentUrl: "/spec", createdAt: "2026" },
      executorBrief: { artifactId: "brief-art", kind: "executor_brief", mediaType: "application/json", sha256: "brief-sha", sizeBytes: 10, contentUrl: "/brief", createdAt: "2026" },
    });

    renderWorkbench("specification");

    expect(await screen.findByText("Canonical execution inputs")).toBeInTheDocument();
    
    // Check that we can navigate (Specification link is active, Execute link is available)
    const stagesNav = screen.getByRole("navigation", { name: "Run stages" });
    const executeLink = within(stagesNav).getByRole("link", { name: "Execute" });
    expect(executeLink).not.toHaveAttribute("aria-disabled", "true");

    await user.click(executeLink);
    expect(mocks.link).toHaveBeenCalledWith("/runs/$runId/execute");
  });

  it("redirects direct URLs that exceed the durable backend stage to the current durable stage", async () => {
    mocks.getRun.mockResolvedValueOnce(
      makeDetail(makeRun("created", "specification")),
    );

    renderWorkbench("execute");

    await waitFor(() => {
      const calls = mocks.navigate.mock.calls;
      expect(calls.length).toBeGreaterThan(0);
      const lastCall = calls[calls.length - 1];
      expect(lastCall).toEqual([{
        to: "/runs/$runId/specification",
        params: { runId: "run-1" },
        replace: true,
      }]);
    });
  });


});
