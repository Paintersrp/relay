import type { BadgeProps } from "@/components/ui/badge";
import type {
  PlanAPIPass,
  PlanAPIPassStatus,
  PlanAPIReadPlan,
  PlanAPIStatus,
} from "@/features/relay-plans";

export type RelayPlanRegistryFilter =
  | "all"
  | "active"
  | "completion_ready"
  | "needs_attention"
  | "complete"
  | "abandoned";

export type RelayPlanAttention =
  | "none"
  | "completion-ready"
  | "in-progress"
  | "next-pass-ready"
  | "no-runnable-pass"
  | "abandoned";

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  dateStyle: "medium",
  timeStyle: "short",
});

const relativeDateFormatter = new Intl.RelativeTimeFormat("en-US", {
  numeric: "auto",
});

export function isTerminalPassStatus(status: PlanAPIPassStatus): boolean {
  return status === "completed" || status === "skipped";
}

export function sortPassesBySequence(passes: PlanAPIPass[]): PlanAPIPass[] {
  return [...passes].sort((a, b) => a.sequence - b.sequence);
}

export function getPassStatusCounts(passes: PlanAPIPass[]) {
  return passes.reduce(
    (counts, pass) => {
      switch (pass.status) {
        case "completed":
          counts.completed += 1;
          break;
        case "skipped":
          counts.skipped += 1;
          break;
        case "in_progress":
          counts.inProgress += 1;
          break;
        case "planned":
          counts.planned += 1;
          break;
      }

      counts.total += 1;
      return counts;
    },
    {
      completed: 0,
      skipped: 0,
      inProgress: 0,
      planned: 0,
      total: 0,
    },
  );
}

export function getCurrentPass(passes: PlanAPIPass[]): PlanAPIPass | undefined {
  return sortPassesBySequence(passes).find((pass) => pass.status === "in_progress");
}

export function getUnmetDependencies(
  pass: PlanAPIPass,
  passes: PlanAPIPass[],
): string[] {
  const passMap = new Map(passes.map((candidate) => [candidate.passId, candidate]));

  return pass.dependencies.filter((dependencyId) => {
    const dependency = passMap.get(dependencyId);
    return !dependency || !isTerminalPassStatus(dependency.status);
  });
}

export function getNextRunnablePass(
  passes: PlanAPIPass[],
): PlanAPIPass | undefined {
  return sortPassesBySequence(passes).find(
    (pass) =>
      pass.status === "planned" && getUnmetDependencies(pass, passes).length === 0,
  );
}

export function getPlanStatusLabel(status: PlanAPIStatus): string {
  switch (status) {
    case "active":
      return "Active";
    case "complete":
      return "Complete";
    case "abandoned":
      return "Abandoned";
  }
}

export function getPassStatusLabel(status: PlanAPIPassStatus): string {
  switch (status) {
    case "planned":
      return "Planned";
    case "in_progress":
      return "In Progress";
    case "completed":
      return "Completed";
    case "skipped":
      return "Skipped";
  }
}

export function getPlanAttention(plan: PlanAPIReadPlan): RelayPlanAttention {
  if (plan.status === "abandoned") {
    return "abandoned";
  }

  if (plan.status === "complete") {
    return "none";
  }

  if (plan.completionReady) {
    return "completion-ready";
  }

  if ((plan.inProgressPassCount ?? 0) > 0) {
    return "in-progress";
  }

  if ((plan.plannedPassCount ?? 0) > 0) {
    return "next-pass-ready";
  }

  if (typeof plan.passCount === "number" && plan.passCount > 0) {
    return "no-runnable-pass";
  }

  return "none";
}

export function getPlanAttentionLabel(attention: RelayPlanAttention): string {
  switch (attention) {
    case "completion-ready":
      return "Completion Ready";
    case "in-progress":
      return "In Progress";
    case "next-pass-ready":
      return "Next Pass Ready";
    case "no-runnable-pass":
      return "Needs Attention";
    case "abandoned":
      return "Abandoned";
    case "none":
      return "None";
  }
}

export function getPlanAttentionVariant(
  attention: RelayPlanAttention,
): BadgeProps["variant"] {
  switch (attention) {
    case "completion-ready":
      return "warning";
    case "in-progress":
      return "running";
    case "next-pass-ready":
      return "info";
    case "no-runnable-pass":
    case "abandoned":
      return "destructive";
    case "none":
      return "outline";
  }
}

export function getPlanStatusVariant(status: PlanAPIStatus): BadgeProps["variant"] {
  switch (status) {
    case "active":
      return "running";
    case "complete":
      return "success";
    case "abandoned":
      return "destructive";
  }
}

export function getPassStatusVariant(
  status: PlanAPIPassStatus,
): BadgeProps["variant"] {
  switch (status) {
    case "planned":
      return "outline";
    case "in_progress":
      return "running";
    case "completed":
      return "success";
    case "skipped":
      return "secondary";
  }
}

export function formatPlanDate(iso: string): string {
  return dateFormatter.format(new Date(iso));
}

export function formatPlanDateRelative(iso: string): string {
  const now = Date.now();
  const then = new Date(iso).getTime();
  const diffSeconds = Math.round((then - now) / 1000);
  const diffMinutes = Math.round(diffSeconds / 60);
  const diffHours = Math.round(diffMinutes / 60);
  const diffDays = Math.round(diffHours / 24);

  if (Math.abs(diffSeconds) < 60) {
    return relativeDateFormatter.format(diffSeconds, "second");
  }

  if (Math.abs(diffMinutes) < 60) {
    return relativeDateFormatter.format(diffMinutes, "minute");
  }

  if (Math.abs(diffHours) < 24) {
    return relativeDateFormatter.format(diffHours, "hour");
  }

  return relativeDateFormatter.format(diffDays, "day");
}
