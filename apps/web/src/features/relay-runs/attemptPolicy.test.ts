import { describe, expect, it } from "vitest";

import type {
  WorkflowExecutionAttempt,
  WorkflowExecutionAttemptStatus,
  WorkflowExecutionAttemptSummary,
  WorkflowRunStatus,
} from "./types";
import {
  deriveWorkflowAttemptControlState,
  isNonterminalWorkflowAttemptStatus,
  isTerminalWorkflowAttemptStatus,
} from "./attemptPolicy";

const ATTEMPT_STATUSES: readonly WorkflowExecutionAttemptStatus[] = [
  "pending",
  "running",
  "succeeded",
  "failed",
  "cancelled",
  "timed_out",
];

const NONTERMINAL_ATTEMPT_STATUSES =
  new Set<WorkflowExecutionAttemptStatus>(["pending", "running"]);

const RUN_STATUSES: readonly WorkflowRunStatus[] = [
  "created",
  "setup_ready",
  "executing",
  "execution_failed",
  "cancelled",
  "validating",
  "validation_failed",
  "audit_ready",
  "needs_revision",
  "completed",
];

const STARTABLE_RUN_STATUSES = new Set<WorkflowRunStatus>([
  "setup_ready",
  "execution_failed",
  "cancelled",
]);

type SelectedAttempt =
  | WorkflowExecutionAttempt
  | WorkflowExecutionAttemptSummary
  | null;

interface StartCase {
  name: string;
  runStatus: WorkflowRunStatus;
  attempts: WorkflowExecutionAttemptSummary[];
  selected: SelectedAttempt;
  expected: boolean;
}

interface ControlCase {
  name: string;
  runStatus: WorkflowRunStatus;
  selected: SelectedAttempt;
  expected: boolean;
}

function makeSummary(
  status: WorkflowExecutionAttemptStatus,
  overrides: Partial<WorkflowExecutionAttemptSummary> = {},
): WorkflowExecutionAttemptSummary {
  return {
    attemptId: "attempt-1",
    attemptNumber: 1,
    adapter: "codex",
    model: "gpt-5.5",
    status,
    createdAt: "2026-07-11T00:00:00Z",
    artifacts: [],
    ...overrides,
  };
}

function makeDetailed(
  status: WorkflowExecutionAttemptStatus,
  overrides: Partial<WorkflowExecutionAttempt> = {},
): WorkflowExecutionAttempt {
  return {
    ...makeSummary(status),
    runId: "run-1",
    result: {},
    artifacts: [],
    liveStdout: "",
    liveStderr: "",
    liveStdoutTruncated: false,
    liveStderrTruncated: false,
    liveStdoutBytes: 0,
    liveStderrBytes: 0,
    ...overrides,
  };
}

const START_CASES: StartCase[] = RUN_STATUSES.flatMap((runStatus) => [
  {
    name: `${runStatus} without an attempt`,
    runStatus,
    attempts: [],
    selected: null,
    expected: STARTABLE_RUN_STATUSES.has(runStatus),
  },
  ...ATTEMPT_STATUSES.map((attemptStatus) => ({
    name: `${runStatus} with ${attemptStatus} in summaries only`,
    runStatus,
    attempts: [makeSummary(attemptStatus)],
    selected: null,
    expected:
      STARTABLE_RUN_STATUSES.has(runStatus) &&
      !NONTERMINAL_ATTEMPT_STATUSES.has(attemptStatus),
  })),
  ...ATTEMPT_STATUSES.map((attemptStatus) => ({
    name: `${runStatus} with ${attemptStatus} selected detail only`,
    runStatus,
    attempts: [],
    selected: makeDetailed(attemptStatus),
    expected:
      STARTABLE_RUN_STATUSES.has(runStatus) &&
      !NONTERMINAL_ATTEMPT_STATUSES.has(attemptStatus),
  })),
]);

const CANCEL_CASES: ControlCase[] = RUN_STATUSES.flatMap((runStatus) => [
  {
    name: `${runStatus} with no selected attempt`,
    runStatus,
    selected: null,
    expected: false,
  },
  ...(["summary", "detailed"] as const).flatMap((selectionKind) =>
    ATTEMPT_STATUSES.flatMap((attemptStatus) =>
      [false, true].map((cancellationRequested) => {
        const overrides = cancellationRequested
          ? { cancellationRequestedAt: "2026-07-11T00:01:00Z" }
          : {};
        const selected =
          selectionKind === "summary"
            ? makeSummary(attemptStatus, overrides)
            : makeDetailed(attemptStatus, overrides);
        return {
          name:
            `${runStatus} with ${selectionKind} ${attemptStatus} ` +
            `cancellationRequested=${cancellationRequested}`,
          runStatus,
          selected,
          expected:
            runStatus === "executing" &&
            NONTERMINAL_ATTEMPT_STATUSES.has(attemptStatus) &&
            !cancellationRequested,
        };
      }),
    ),
  ),
]);

const RECONCILE_CASES: ControlCase[] = RUN_STATUSES.flatMap((runStatus) => [
  {
    name: `${runStatus} with no selected attempt`,
    runStatus,
    selected: null,
    expected: false,
  },
  ...ATTEMPT_STATUSES.flatMap((attemptStatus) =>
    [false, true].map((cancellationRequested) => ({
      name:
        `${runStatus} with summary ${attemptStatus} ` +
        `cancellationRequested=${cancellationRequested}`,
      runStatus,
      selected: makeSummary(
        attemptStatus,
        cancellationRequested
          ? { cancellationRequestedAt: "2026-07-11T00:01:00Z" }
          : {},
      ),
      expected: false,
    })),
  ),
  ...ATTEMPT_STATUSES.flatMap((attemptStatus) =>
    ([undefined, false, true] as const).flatMap((cleanupPending) =>
      [false, true].map((cancellationRequested) => {
        const result =
          cleanupPending === undefined
            ? {}
            : { cleanup_pending: cleanupPending };
        const selected = makeDetailed(attemptStatus, {
          result,
          ...(cancellationRequested
            ? { cancellationRequestedAt: "2026-07-11T00:01:00Z" }
            : {}),
        });
        return {
          name:
            `${runStatus} with detailed ${attemptStatus} ` +
            `cleanupPending=${String(cleanupPending)} ` +
            `cancellationRequested=${cancellationRequested}`,
          runStatus,
          selected,
          expected:
            runStatus === "executing" &&
            NONTERMINAL_ATTEMPT_STATUSES.has(attemptStatus) &&
            cleanupPending === true,
        };
      }),
    ),
  ),
]);

describe("workflow attempt policy", () => {
  it.each(
    ATTEMPT_STATUSES.map((status) => [
      status,
      NONTERMINAL_ATTEMPT_STATUSES.has(status),
    ] as const),
  )("classifies %s terminality", (status, expectedNonterminal) => {
    expect(isNonterminalWorkflowAttemptStatus(status)).toBe(
      expectedNonterminal,
    );
    expect(isTerminalWorkflowAttemptStatus(status)).toBe(
      !expectedNonterminal,
    );
  });

  it.each(START_CASES)(
    "derives Start eligibility for $name",
    ({ runStatus, attempts, selected, expected }) => {
      expect(
        deriveWorkflowAttemptControlState(
          runStatus,
          attempts,
          selected,
        ).canStart,
      ).toBe(expected);
    },
  );

  it.each(CANCEL_CASES)(
    "derives Cancel eligibility for $name",
    ({ runStatus, selected, expected }) => {
      expect(
        deriveWorkflowAttemptControlState(
          runStatus,
          [],
          selected,
        ).canCancel,
      ).toBe(expected);
    },
  );

  it.each(RECONCILE_CASES)(
    "derives Reconcile eligibility for $name",
    ({ runStatus, selected, expected }) => {
      expect(
        deriveWorkflowAttemptControlState(
          runStatus,
          [],
          selected,
        ).canReconcile,
      ).toBe(expected);
    },
  );
});
