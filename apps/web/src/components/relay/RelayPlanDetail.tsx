import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, Copy, Workflow } from "lucide-react";

import { RelayMetaRow, RelayMonoText } from "@/components/relay/RelayMeta";
import { RelayPlanPassTimeline } from "@/components/relay/RelayPlanPassTimeline";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import {
  formatPlanDate,
  getCurrentPass,
  getNextRunnablePass,
  getPassStatusCounts,
  getPlanStatusLabel,
  getPlanStatusVariant,
  sortPassesBySequence,
} from "@/components/relay/relayPlanVisualState";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { PlanAPIPass, PlanAPIReadPlan } from "@/features/relay-plans";

interface RelayPlanDetailProps {
  plan: PlanAPIReadPlan;
  passes: PlanAPIPass[];
  completionReady: boolean;
}

type CopyState = "idle" | "copied" | "failed";

function getHeroState(
  currentPass: PlanAPIPass | undefined,
  nextPass: PlanAPIPass | undefined,
  completionReady: boolean,
  passCount: number,
) {
  if (currentPass) {
    return {
      eyebrow: "Current Pass",
      title: currentPass.name,
      subtitle: currentPass.goal,
      badgeLabel: "Plan Active",
      badgeVariant: "running" as const,
      meta: currentPass.passId,
    };
  }

  if (nextPass) {
    return {
      eyebrow: "Next Runnable Pass",
      title: nextPass.name,
      subtitle: nextPass.goal,
      badgeLabel: "Next Pass Ready",
      badgeVariant: "info" as const,
      meta: nextPass.passId,
    };
  }

  if (completionReady) {
    return {
      eyebrow: "Plan State",
      title: "Plan completion ready",
      subtitle: "All passes are terminal and the plan is ready for completion handling.",
      badgeLabel: "Completion Ready",
      badgeVariant: "warning" as const,
      meta: undefined,
    };
  }

  if (passCount === 0) {
    return {
      eyebrow: "Plan State",
      title: "No passes in plan",
      subtitle: "This managed plan does not contain any pass records yet.",
      badgeLabel: "Empty",
      badgeVariant: "secondary" as const,
      meta: undefined,
    };
  }

  return {
    eyebrow: "Plan State",
    title: "No runnable pass",
    subtitle: "Remaining work is blocked by unmet dependencies or waiting state transitions.",
    badgeLabel: "Needs Attention",
    badgeVariant: "destructive" as const,
    meta: undefined,
  };
}

export function RelayPlanDetail({
  plan,
  passes,
  completionReady,
}: RelayPlanDetailProps) {
  const sortedPasses = React.useMemo(() => sortPassesBySequence(passes), [passes]);
  const counts = React.useMemo(() => getPassStatusCounts(sortedPasses), [sortedPasses]);
  const currentPass = React.useMemo(() => getCurrentPass(sortedPasses), [sortedPasses]);
  const nextPass = React.useMemo(() => getNextRunnablePass(sortedPasses), [sortedPasses]);
  const heroState = getHeroState(currentPass, nextPass, completionReady, sortedPasses.length);
  const [copyState, setCopyState] = React.useState<CopyState>("idle");

  const progressSegments =
    counts.total > 0
      ? [
          {
            label: "Completed",
            value: counts.completed,
            className: "bg-[var(--success)]",
          },
          {
            label: "In Progress",
            value: counts.inProgress,
            className: "bg-[var(--relay-accent)]",
          },
          {
            label: "Planned",
            value: counts.planned,
            className: "bg-[var(--info)]",
          },
          {
            label: "Skipped",
            value: counts.skipped,
            className: "bg-muted-foreground/50",
          },
        ]
      : [];

  const copyContext = async () => {
    const context = [
      `Plan: ${plan.title}`,
      `Plan ID: ${plan.planId}`,
      `Repository: ${plan.repoTarget}`,
      `Branch: ${plan.branchContext}`,
      `Status: ${plan.status}`,
      `Completion ready: ${completionReady ? "yes" : "no"}`,
      currentPass ? `Current pass: ${currentPass.passId} — ${currentPass.name}` : "",
      `Passes: ${counts.completed} completed, ${counts.inProgress} in progress, ${counts.planned} planned, ${counts.skipped} skipped`,
    ]
      .filter(Boolean)
      .join("\n");

    try {
      if (!navigator.clipboard) {
        throw new Error("Clipboard API unavailable");
      }

      await navigator.clipboard.writeText(context);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  };

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-4 sm:p-6">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link to="/plans" className="inline-flex items-center gap-1">
                <ArrowLeft className="size-3.5" />
                Plans
              </Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>{plan.title}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>

      <section className="rounded-2xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-2xl font-semibold tracking-tight text-foreground">
                {plan.title}
              </h1>
              <Badge
                variant={getPlanStatusVariant(plan.status)}
                className="text-[11px] font-medium"
              >
                {getPlanStatusLabel(plan.status)}
              </Badge>
              {completionReady ? (
                <Badge variant="warning" className="text-[11px] font-medium">
                  Completion Ready
                </Badge>
              ) : null}
            </div>

            <p className="mt-3 text-sm leading-6 text-muted-foreground">{plan.goal}</p>

            <RelayMetaRow className="mt-4 gap-x-3 gap-y-2">
              <RelayMonoText>{plan.planId}</RelayMonoText>
              <RelayMonoText>{plan.repoTarget}</RelayMonoText>
              <RelayMonoText>{plan.branchContext}</RelayMonoText>
              {plan.sourceArtifactPath ? (
                <RelayMonoText className="break-all">{plan.sourceArtifactPath}</RelayMonoText>
              ) : null}
              <span title={formatPlanDate(plan.createdAt)}>
                Created {formatPlanDate(plan.createdAt)}
              </span>
            </RelayMetaRow>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={copyContext}>
              <Copy className="size-3.5" />
              {copyState === "copied"
                ? "Copied"
                : copyState === "failed"
                  ? "Copy failed"
                  : "Copy context"}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled
              title="Pass detail arrives in UI-PLAN-04"
            >
              Open current pass
            </Button>
          </div>
        </div>
      </section>

      <section className="grid gap-6 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
        <div className="rounded-2xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5">
          <div className="flex items-start gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] text-[var(--relay-accent)]">
              <Workflow className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                {heroState.eyebrow}
              </p>
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <h2 className="text-xl font-semibold text-foreground">{heroState.title}</h2>
                <Badge variant={heroState.badgeVariant} className="text-[11px] font-medium">
                  {heroState.badgeLabel}
                </Badge>
                {heroState.meta ? (
                  <RelayMonoText className="text-[11px] text-muted-foreground">
                    {heroState.meta}
                  </RelayMonoText>
                ) : null}
              </div>
              <p className="mt-3 text-sm leading-6 text-muted-foreground">
                {heroState.subtitle}
              </p>
            </div>
          </div>
        </div>

        <div className="rounded-2xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                Progress Summary
              </p>
              <h2 className="mt-2 text-lg font-semibold text-foreground">
                {counts.completed + counts.skipped} of {counts.total} terminal
              </h2>
            </div>
            {completionReady ? (
              <Badge variant="warning" className="text-[11px] font-medium">
                Completion Ready
              </Badge>
            ) : null}
          </div>

          <div className="mt-4 h-3 overflow-hidden rounded-full bg-[var(--relay-row-border)]">
            {progressSegments.length > 0 ? (
              <div className="flex h-full w-full">
                {progressSegments.map((segment) =>
                  segment.value > 0 ? (
                    <div
                      key={segment.label}
                      className={segment.className}
                      style={{ width: `${(segment.value / counts.total) * 100}%` }}
                      title={`${segment.label}: ${segment.value}`}
                    />
                  ) : null,
                )}
              </div>
            ) : (
              <div className="h-full w-full bg-[var(--relay-row-border)]" />
            )}
          </div>

          <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div className="rounded-xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] p-3">
              <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                Completed
              </p>
              <p className="mt-2 text-lg font-semibold text-foreground">{counts.completed}</p>
            </div>
            <div className="rounded-xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] p-3">
              <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                In Progress
              </p>
              <p className="mt-2 text-lg font-semibold text-foreground">{counts.inProgress}</p>
            </div>
            <div className="rounded-xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] p-3">
              <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                Planned
              </p>
              <p className="mt-2 text-lg font-semibold text-foreground">{counts.planned}</p>
            </div>
            <div className="rounded-xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] p-3">
              <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                Skipped
              </p>
              <p className="mt-2 text-lg font-semibold text-foreground">{counts.skipped}</p>
            </div>
          </div>
        </div>
      </section>

      {sortedPasses.length > 0 ? (
        <section className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                Pass Timeline
              </p>
              <h2 className="mt-2 text-xl font-semibold text-foreground">Execution passes</h2>
            </div>
            <span className="text-sm text-muted-foreground">{sortedPasses.length} passes</span>
          </div>

          <RelayPlanPassTimeline passes={sortedPasses} />
        </section>
      ) : (
        <RelayStateSurface
          tone="empty"
          title="No passes available"
          description="This plan detail record loaded successfully, but it does not include any pass entries yet."
        />
      )}
    </div>
  );
}
