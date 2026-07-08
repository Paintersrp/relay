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
    ["created", "specification"],
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
});
