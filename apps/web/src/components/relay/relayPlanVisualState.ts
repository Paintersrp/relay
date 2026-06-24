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

export interface PlanProgressSummary {
  total: number;
  completed: number;
  inProgress: number;
  planned: number;
  skipped: number;
  terminal: number;
  label: string;
  dotCount: number;
  filledDots: number;
}

export interface RelayPlanDetailProgress {
  total: number;
  completed: number;
  inProgress: number;
  planned: number;
  skipped: number;
  terminal: number;
  segmentCount: number;
  completedSegments: number;
  skippedSegments: number;
  inProgressSegments: number;
}

export interface RelayPlanDetailCardState {
  key: "active" | "completion_ready" | "complete" | "abandoned";
  eyebrow: string;
  eyebrowClassName: string;
  accentClassName: string;
  title: string;
  subtitle?: string;
}

export type PlanRegistryPassSummary =
  | {
      kind: "current" | "next";
      passId?: string;
      title: string;
      subtitle?: string;
    }
  | {
      kind: "fallback";
      title: string;
      subtitle?: string;
    };

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

function clampCount(value: number, total: number): number {
  return Math.min(total, Math.max(0, value));
}

export function isTerminalPassStatus(status: PlanAPIPassStatus): boolean {
  return status === "completed" || status === "skipped";
}

export function isActivePassStatus(status: PlanAPIPassStatus): boolean {
  return (
    status === "handoff_ready" ||
    status === "run_created" ||
    status === "in_progress" ||
    status === "audit_ready" ||
    status === "revision_required" ||
    status === "blocked"
  );
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
        case "ready_for_planner":
          counts.readyForPlanner += 1;
          break;
        case "handoff_ready":
          counts.handoffReady += 1;
          break;
        case "run_created":
          counts.runCreated += 1;
          break;
        case "audit_ready":
          counts.auditReady += 1;
          break;
        case "revision_required":
          counts.revisionRequired += 1;
          break;
        case "blocked":
          counts.blocked += 1;
          break;
      }

      counts.total += 1;
      counts.terminal = counts.completed + counts.skipped;
      return counts;
    },
    {
      completed: 0,
      skipped: 0,
      inProgress: 0,
      planned: 0,
      readyForPlanner: 0,
      handoffReady: 0,
      runCreated: 0,
      auditReady: 0,
      revisionRequired: 0,
      blocked: 0,
      terminal: 0,
      total: 0,
    },
  );
}

export function getCurrentPass(passes: PlanAPIPass[]): PlanAPIPass | undefined {
  return sortPassesBySequence(passes).find((pass) => isActivePassStatus(pass.status));
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
      (pass.status === "planned" || pass.status === "ready_for_planner") &&
      getUnmetDependencies(pass, passes).length === 0,
  );
}

export function getPlanDetailCardState(args: {
  plan: Pick<PlanAPIReadPlan, "status">;
  completionReady: boolean;
  currentPass?: Pick<PlanAPIPass, "name" | "goal">;
}): RelayPlanDetailCardState {
  const { plan, completionReady, currentPass } = args;

  if (plan.status === "abandoned") {
    return {
      key: "abandoned",
      eyebrow: "PLAN ABANDONED",
      eyebrowClassName: "text-muted-foreground",
      accentClassName: "bg-muted-foreground/45",
      title: "This plan is no longer active",
    };
  }

  if (plan.status === "complete") {
    return {
      key: "complete",
      eyebrow: "PLAN COMPLETE",
      eyebrowClassName: "text-[var(--success)]",
      accentClassName: "bg-[var(--success)]",
      title: "All planned passes completed successfully",
    };
  }

  if (completionReady) {
    return {
      key: "completion_ready",
      eyebrow: "COMPLETION READY",
      eyebrowClassName: "text-[var(--warning)]",
      accentClassName: "bg-[var(--warning)]",
      title: "All passes terminal — ready for closeout review",
      subtitle: "Plan status remains active until a supported completion action exists.",
    };
  }

  if (currentPass) {
    return {
      key: "active",
      eyebrow: "PLAN ACTIVE",
      eyebrowClassName: "text-[var(--relay-accent)]",
      accentClassName: "bg-[var(--relay-accent)]",
      title: currentPass.name,
      subtitle: currentPass.goal,
    };
  }

  return {
    key: "active",
    eyebrow: "PLAN ACTIVE",
    eyebrowClassName: "text-[var(--relay-accent)]",
    accentClassName: "bg-[var(--relay-accent)]",
    title: "No pass currently in progress",
  };
}

export function getPlanDetailProgress(
  passes: PlanAPIPass[],
  maxSegments = 12,
): RelayPlanDetailProgress {
  const counts = getPassStatusCounts(passes);
  const segmentCount = counts.total > 0 ? Math.min(counts.total, maxSegments) : 0;

  if (segmentCount === 0) {
    return {
      ...counts,
      segmentCount: 0,
      completedSegments: 0,
      skippedSegments: 0,
      inProgressSegments: 0,
    };
  }

  const completedSegments =
    counts.total <= maxSegments
      ? counts.completed
      : Math.min(
          segmentCount,
          Math.round((counts.completed / counts.total) * maxSegments),
        );
  const skippedSegments =
    counts.total <= maxSegments
      ? counts.skipped
      : Math.min(
          Math.max(0, segmentCount - completedSegments),
          Math.round((counts.skipped / counts.total) * maxSegments),
        );
  const inProgressSegments =
    counts.total <= maxSegments
      ? counts.inProgress
      : Math.min(
          Math.max(0, segmentCount - completedSegments - skippedSegments),
          Math.round((counts.inProgress / counts.total) * maxSegments),
        );

  return {
    ...counts,
    segmentCount,
    completedSegments,
    skippedSegments,
    inProgressSegments,
  };
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
    case "ready_for_planner":
      return "Ready for Planner";
    case "handoff_ready":
      return "Handoff Ready";
    case "run_created":
      return "Run Created";
    case "in_progress":
      return "In Progress";
    case "audit_ready":
      return "Audit Ready";
    case "completed":
      return "Completed";
    case "revision_required":
      return "Revision Required";
    case "blocked":
      return "Blocked";
    case "skipped":
      return "Skipped";
    default:
      return `Unknown: ${status}`;
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

export function getPlanProgressSummary(plan: PlanAPIReadPlan): PlanProgressSummary {
  const total = Math.max(0, plan.passCount ?? 0);
  const completedCount = plan.completedPassCount;
  const skippedCount = plan.skippedPassCount;
  const hasCompletedCount = typeof completedCount === "number";
  const hasSkippedCount = typeof skippedCount === "number";
  const completed = clampCount(
    hasCompletedCount
      ? completedCount
      : plan.completionReady
        ? hasSkippedCount
          ? 0
          : total
        : 0,
    total,
  );
  const inProgress = clampCount(plan.inProgressPassCount ?? 0, total);
  const planned = clampCount(plan.plannedPassCount ?? 0, total);
  const skipped = clampCount(skippedCount ?? 0, total);
  const explicitTerminal = hasCompletedCount || hasSkippedCount;
  const terminal = plan.completionReady && !explicitTerminal
    ? total
    : clampCount(completed + skipped, total);
  const filledDots = plan.completionReady ? terminal : completed;

  return {
    total,
    completed,
    inProgress,
    planned,
    skipped,
    terminal,
    label: `${filledDots} / ${total}`,
    dotCount: total,
    filledDots,
  };
}

export function getPlanRegistryPassSummary(
  plan: PlanAPIReadPlan,
): PlanRegistryPassSummary {
  if (plan.status === "complete") {
    return {
      kind: "fallback",
      title: "ALL COMPLETE",
    };
  }

  if (plan.status === "abandoned") {
    return {
      kind: "fallback",
      title: "—",
    };
  }

  if (plan.completionReady) {
    return {
      kind: "fallback",
      title: "READY FOR CLOSEOUT",
      subtitle: "All passes terminal",
    };
  }

  if (plan.currentPassName || plan.currentPassId || plan.currentPassGoal) {
    return {
      kind: "current",
      passId: plan.currentPassId,
      title: plan.currentPassName ?? "Current pass",
      subtitle: plan.currentPassGoal,
    };
  }

  if (plan.nextPassName || plan.nextPassId || plan.nextPassGoal) {
    return {
      kind: "next",
      passId: plan.nextPassId,
      title: plan.nextPassName ?? "Next pass",
      subtitle: plan.nextPassGoal,
    };
  }

  return {
    kind: "fallback",
    title: "—",
  };
}

export function getPassStatusVariant(
  status: PlanAPIPassStatus,
): BadgeProps["variant"] {
  switch (status) {
    case "planned":
      return "outline";
    case "ready_for_planner":
      return "info";
    case "handoff_ready":
      return "warning";
    case "run_created":
      return "running";
    case "in_progress":
      return "running";
    case "audit_ready":
      return "warning";
    case "completed":
      return "success";
    case "revision_required":
      return "warning";
    case "blocked":
      return "destructive";
    case "skipped":
      return "secondary";
    default:
      return "outline";
  }
}

export function formatPlanDate(iso: string): string {
  const date = new Date(iso);

  if (Number.isNaN(date.getTime())) {
    return "Unknown";
  }

  return dateFormatter.format(date);
}

export function formatPlanDateRelative(iso: string): string {
  const now = Date.now();
  const then = new Date(iso).getTime();

  if (Number.isNaN(then)) {
    return "Unknown";
  }

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
