import { describe, expect, it } from "vitest";

import {
  canCloseDiscoveryTask,
  canCompleteDiscoveryTask,
  canEditDiscoveryTask,
  canPromoteCandidate,
  canSelectCandidateForGeneratedPlan,
  groupCandidates,
  isCandidateTerminal,
} from "./status";
import type { RefactorCandidate, RefactorCandidateStatus } from "./types";

function candidate(
  candidateId: string,
  status: RefactorCandidateStatus,
): RefactorCandidate {
  return {
    candidateId,
    projectId: "proj-1",
    title: candidateId,
    problemSummary: "",
    currentBehavior: "",
    desiredBehavior: "",
    rationale: "",
    proposedPassName: "",
    proposedPassGoal: "",
    proposedPassScope: [],
    nonGoals: [],
    targetFiles: [],
    validationCommands: [],
    auditFocus: [],
    constraints: [],
    riskLevel: "medium",
    status,
    metadata: {},
    createdAt: "",
    updatedAt: "",
  };
}

const NON_READY_STATUSES: RefactorCandidateStatus[] = [
  "scheduled",
  "scheduled_revision_required",
  "completed",
  "completed_with_warnings",
  "deferred",
  "rejected",
  "superseded",
  "totally_unknown_status",
];

describe("candidate promotion gating", () => {
  it("allows promotion only for ready candidates", () => {
    expect(canPromoteCandidate("ready")).toBe(true);
    for (const status of NON_READY_STATUSES) {
      expect(canPromoteCandidate(status)).toBe(false);
    }
  });

  it("allows generated-plan selection only for ready candidates", () => {
    expect(canSelectCandidateForGeneratedPlan("ready")).toBe(true);
    for (const status of NON_READY_STATUSES) {
      expect(canSelectCandidateForGeneratedPlan(status)).toBe(false);
    }
  });
});

describe("candidate terminal detection", () => {
  it("marks completed/rejected/superseded variants as terminal", () => {
    expect(isCandidateTerminal("completed")).toBe(true);
    expect(isCandidateTerminal("completed_with_warnings")).toBe(true);
    expect(isCandidateTerminal("rejected")).toBe(true);
    expect(isCandidateTerminal("superseded")).toBe(true);
  });

  it("does not mark active states as terminal", () => {
    expect(isCandidateTerminal("ready")).toBe(false);
    expect(isCandidateTerminal("scheduled")).toBe(false);
    expect(isCandidateTerminal("deferred")).toBe(false);
  });
});

describe("groupCandidates", () => {
  it("places candidates into the expected buckets, unknown under other", () => {
    const candidates = [
      candidate("c-ready", "ready"),
      candidate("c-scheduled", "scheduled"),
      candidate("c-revision", "scheduled_revision_required"),
      candidate("c-completed", "completed"),
      candidate("c-completed-warn", "completed_with_warnings"),
      candidate("c-deferred", "deferred"),
      candidate("c-rejected", "rejected"),
      candidate("c-superseded", "superseded"),
      candidate("c-unknown", "weird_status"),
    ];

    const grouped = groupCandidates(candidates);

    expect(grouped.ready.map((c) => c.candidateId)).toEqual(["c-ready"]);
    expect(grouped.scheduled.map((c) => c.candidateId)).toEqual([
      "c-scheduled",
      "c-revision",
    ]);
    expect(grouped.completed.map((c) => c.candidateId)).toEqual([
      "c-completed",
      "c-completed-warn",
    ]);
    expect(grouped.inactive.map((c) => c.candidateId)).toEqual([
      "c-deferred",
      "c-rejected",
      "c-superseded",
    ]);
    expect(grouped.other.map((c) => c.candidateId)).toEqual(["c-unknown"]);
  });
});

describe("discovery task gating", () => {
  it("allows edit/complete/close only for open tasks", () => {
    expect(canEditDiscoveryTask("open")).toBe(true);
    expect(canCompleteDiscoveryTask("open")).toBe(true);
    expect(canCloseDiscoveryTask("open")).toBe(true);

    for (const status of ["completed", "closed", "superseded"]) {
      expect(canEditDiscoveryTask(status)).toBe(false);
      expect(canCompleteDiscoveryTask(status)).toBe(false);
      expect(canCloseDiscoveryTask(status)).toBe(false);
    }
  });
});
