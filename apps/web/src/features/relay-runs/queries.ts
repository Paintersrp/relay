import { queryOptions } from "@tanstack/react-query";
import {
  getRuns,
  getRun,
  getRunArtifacts,
  getRunEvents,
  getAuditStatus,
  getArtifactContent,
  getArtifactContentByUrl,
} from "./api";
import type { RelayArtifact } from "./types";

// Query key factory
export const relayRunKeys = {
  all: ["relay-runs"] as const,
  list: () => [...relayRunKeys.all, "list"] as const,
  detail: (id: string) => [...relayRunKeys.all, "detail", id] as const,
  artifacts: (id: string) =>
    [...relayRunKeys.all, "detail", id, "artifacts"] as const,
  events: (id: string) =>
    [...relayRunKeys.all, "detail", id, "events"] as const,
  auditStatus: (id: string) =>
    [...relayRunKeys.all, "detail", id, "audit-status"] as const,
  artifactContent: (id: string, kind: string) =>
    [...relayRunKeys.all, "detail", id, "artifacts", kind] as const,
};

// Query options for all runs (list page)
export const runsListQueryOptions = queryOptions({
  queryKey: relayRunKeys.list(),
  queryFn: getRuns,
  staleTime: 5 * 60 * 1000,
});

// Query options for a single run (workbench step pages)
export function runDetailQueryOptions(id: string) {
  return queryOptions({
    queryKey: relayRunKeys.detail(id),
    queryFn: () => getRun(id),
    staleTime: 2 * 60 * 1000,
  });
}

// Query options for run artifacts
export function runArtifactsQueryOptions(id: string) {
  return queryOptions({
    queryKey: relayRunKeys.artifacts(id),
    queryFn: () => getRunArtifacts(id),
    staleTime: 2 * 60 * 1000,
  });
}

// Query options for run events
export function runEventsQueryOptions(id: string) {
  return queryOptions({
    queryKey: relayRunKeys.events(id),
    queryFn: () => getRunEvents(id),
    staleTime: 2 * 60 * 1000,
  });
}

// Execute-active polling helpers. These keep the execute page fresh while the
// executor is running without affecting default stale times on other routes.
export function isExecuteLiveStatus(status?: string): boolean {
  return (
    status === "executor_dispatched" ||
    status === "executor_running" ||
    status === "local_validation_running"
  );
}

export function executeActiveRunDetailQueryOptions(id: string) {
  return queryOptions({
    queryKey: relayRunKeys.detail(id),
    queryFn: () => getRun(id),
    staleTime: 0,
    refetchInterval: (data) =>
      isExecuteLiveStatus((data as { status?: string } | undefined)?.status)
        ? 1000
        : false,
    refetchIntervalInBackground: true,
  });
}

export function executeActiveRunArtifactsQueryOptions(
  id: string,
  status?: string,
) {
  const live = isExecuteLiveStatus(status);
  return queryOptions({
    queryKey: relayRunKeys.artifacts(id),
    queryFn: () => getRunArtifacts(id),
    staleTime: 0,
    refetchInterval: live ? 1500 : false,
    refetchIntervalInBackground: live,
  });
}

export function executeActiveRunEventsQueryOptions(
  id: string,
  status?: string,
) {
  const live = isExecuteLiveStatus(status);
  return queryOptions({
    queryKey: relayRunKeys.events(id),
    queryFn: () => getRunEvents(id),
    staleTime: 0,
    refetchInterval: live ? 1000 : false,
    refetchIntervalInBackground: live,
  });
}

export function auditStatusQueryOptions(id: string) {
  return queryOptions({
    queryKey: relayRunKeys.auditStatus(id),
    queryFn: () => getAuditStatus(id),
    staleTime: 30 * 1000,
  });
}

export function runArtifactContentQueryOptions(id: string, kind: string) {
  return queryOptions({
    queryKey: relayRunKeys.artifactContent(id, kind),
    queryFn: () => getArtifactContent(id, kind),
    staleTime: 2 * 60 * 1000,
  });
}

export function runArtifactContentQueryOptionsForArtifact(
  runId: string,
  artifact: RelayArtifact,
) {
  const identity =
    artifact.storageKind ||
    artifact.contentUrl ||
    artifact.path ||
    artifact.kind;
  const contentUrl = artifact.contentUrl || artifact.path;
  return queryOptions({
    queryKey: [
      ...relayRunKeys.all,
      "detail",
      runId,
      "artifacts-by-url",
      identity,
    ],
    queryFn: () => {
      if (contentUrl && contentUrl.startsWith("/api/runs/")) {
        return getArtifactContentByUrl(contentUrl);
      }
      return getArtifactContent(runId, artifact.kind);
    },
    staleTime: 2 * 60 * 1000,
  });
}

// Native Intl formatters — do not add date-fns (CR9)
const dateFormatter = new Intl.DateTimeFormat("en-US", {
  dateStyle: "medium",
  timeStyle: "short",
});

const relativeDateFormatter = new Intl.RelativeTimeFormat("en-US", {
  numeric: "auto",
});

export function formatRunDate(iso: string): string {
  return dateFormatter.format(new Date(iso));
}

export function formatRunDateRelative(iso: string): string {
  const now = Date.now();
  const then = new Date(iso).getTime();
  const diffSeconds = Math.round((then - now) / 1000);
  const diffMinutes = Math.round(diffSeconds / 60);
  const diffHours = Math.round(diffMinutes / 60);
  const diffDays = Math.round(diffHours / 24);

  if (Math.abs(diffSeconds) < 60)
    return relativeDateFormatter.format(diffSeconds, "second");
  if (Math.abs(diffMinutes) < 60)
    return relativeDateFormatter.format(diffMinutes, "minute");
  if (Math.abs(diffHours) < 24)
    return relativeDateFormatter.format(diffHours, "hour");
  return relativeDateFormatter.format(diffDays, "day");
}
