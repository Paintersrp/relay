// ============================================================
// Relay Refactor Backlog — pure status/action gating helpers (PASS-006)
//
// These helpers are pure (no React, no network) so they can be unit-tested and
// reused for UI gating. Unknown statuses are never promotable/selectable.
// ============================================================

import type {
  RefactorCandidate,
  RefactorCandidateStatus,
  RefactorDiscoveryTaskStatus,
} from "./types";

export function canEditDiscoveryTask(status: RefactorDiscoveryTaskStatus): boolean {
  return status === "open";
}

export function canCloseDiscoveryTask(status: RefactorDiscoveryTaskStatus): boolean {
  return status === "open";
}

export function canCompleteDiscoveryTask(status: RefactorDiscoveryTaskStatus): boolean {
  return status === "open";
}

export function canSupersedeDiscoveryTask(status: RefactorDiscoveryTaskStatus): boolean {
  return status === "open" || status === "completed";
}

export function canPromoteCandidate(status: RefactorCandidateStatus): boolean {
  return status === "ready";
}

export function canSelectCandidateForGeneratedPlan(
  status: RefactorCandidateStatus,
): boolean {
  return status === "ready";
}

export function canEditCandidate(status: RefactorCandidateStatus): boolean {
  return status === "ready";
}

export function isCandidateTerminal(status: RefactorCandidateStatus): boolean {
  return (
    status === "completed" ||
    status === "completed_with_warnings" ||
    status === "rejected" ||
    status === "superseded"
  );
}

const KNOWN_CANDIDATE_STATUSES: RefactorCandidateStatus[] = [
  "ready",
  "scheduled",
  "scheduled_revision_required",
  "completed",
  "completed_with_warnings",
  "deferred",
  "rejected",
  "superseded",
];

export interface GroupedCandidates {
  ready: RefactorCandidate[];
  scheduled: RefactorCandidate[];
  completed: RefactorCandidate[];
  inactive: RefactorCandidate[];
  other: RefactorCandidate[];
}

export function groupCandidates(candidates: RefactorCandidate[]): GroupedCandidates {
  return {
    ready: candidates.filter((candidate) => candidate.status === "ready"),
    scheduled: candidates.filter(
      (candidate) =>
        candidate.status === "scheduled" ||
        candidate.status === "scheduled_revision_required",
    ),
    completed: candidates.filter(
      (candidate) =>
        candidate.status === "completed" ||
        candidate.status === "completed_with_warnings",
    ),
    inactive: candidates.filter(
      (candidate) =>
        candidate.status === "deferred" ||
        candidate.status === "rejected" ||
        candidate.status === "superseded",
    ),
    other: candidates.filter(
      (candidate) => !KNOWN_CANDIDATE_STATUSES.includes(candidate.status),
    ),
  };
}

export function refactorCandidateStatusLabel(status: RefactorCandidateStatus): string {
  switch (status) {
    case "ready":
      return "Ready";
    case "scheduled":
      return "Scheduled";
    case "scheduled_revision_required":
      return "Revision required";
    case "completed":
      return "Completed";
    case "completed_with_warnings":
      return "Completed with warnings";
    case "deferred":
      return "Deferred";
    case "rejected":
      return "Rejected";
    case "superseded":
      return "Superseded";
    default:
      return status || "Unknown";
  }
}

export function refactorDiscoveryStatusLabel(
  status: RefactorDiscoveryTaskStatus,
): string {
  switch (status) {
    case "open":
      return "Open";
    case "completed":
      return "Completed";
    case "closed":
      return "Closed";
    case "superseded":
      return "Superseded";
    default:
      return status || "Unknown";
  }
}
