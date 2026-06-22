import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, Copy } from "lucide-react";

import { RelayPlanPassTimeline } from "@/components/relay/RelayPlanPassTimeline";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import {
  formatPlanDate,
  formatPlanDateRelative,
  getCurrentPass,
  getPassStatusCounts,
  getPlanDetailCardState,
  getPlanDetailProgress,
  getPlanStatusLabel,
  getPlanStatusVariant,
  sortPassesBySequence,
} from "@/components/relay/relayPlanVisualState";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { PlanAPIPass, PlanAPIReadPlan } from "@/features/relay-plans";
import { cn } from "@/lib/utils";

interface RelayPlanDetailProps {
  plan: PlanAPIReadPlan;
  passes: PlanAPIPass[];
  completionReady: boolean;
}

type CopyState = "idle" | "copied" | "failed";

function buildCopyContext(args: {
  plan: PlanAPIReadPlan;
  completionReady: boolean;
  counts: ReturnType<typeof getPassStatusCounts>;
  currentPass?: PlanAPIPass;
}) {
  const { plan, completionReady, counts, currentPass } = args;

  return [
    `Plan: ${plan.title}`,
    `Plan ID: ${plan.planId}`,
    `Repository: ${plan.repoTarget}`,
    `Branch: ${plan.branchContext}`,
    `Status: ${plan.status}`,
    `Completion ready: ${completionReady ? "yes" : "no"}`,
    `Terminal passes: ${counts.terminal} of ${counts.total}`,
    `Completed passes: ${counts.completed}`,
    `Skipped passes: ${counts.skipped}`,
    currentPass ? `Current pass: ${currentPass.passId} - ${currentPass.name}` : "",
    currentPass?.goal ? `Current pass goal: ${currentPass.goal}` : "",
    currentPass?.intendedExecutionScope.length
      ? `Current pass scope: ${currentPass.intendedExecutionScope.join(", ")}`
      : "",
    `Passes: ${counts.completed} completed, ${counts.inProgress} in progress, ${counts.planned} planned, ${counts.skipped} skipped`,
  ]
    .filter(Boolean)
    .join("\n");
}

export function RelayPlanDetail({
  plan,
  passes,
  completionReady,
}: RelayPlanDetailProps) {
  const sortedPasses = React.useMemo(() => sortPassesBySequence(passes), [passes]);
  const counts = React.useMemo(() => getPassStatusCounts(sortedPasses), [sortedPasses]);
  const progress = React.useMemo(
    () => getPlanDetailProgress(sortedPasses),
    [sortedPasses],
  );
  const currentPass = React.useMemo(() => getCurrentPass(sortedPasses), [sortedPasses]);
  const cardState = React.useMemo(
    () => getPlanDetailCardState({ plan, completionReady, currentPass }),
    [plan, completionReady, currentPass],
  );
  const [planIdCopyState, setPlanIdCopyState] = React.useState<CopyState>("idle");
  const [contextCopyState, setContextCopyState] = React.useState<CopyState>("idle");

  const copyPlanId = async () => {
    try {
      if (!navigator.clipboard) {
        throw new Error("Clipboard API unavailable");
      }

      await navigator.clipboard.writeText(plan.planId);
      setPlanIdCopyState("copied");
    } catch {
      setPlanIdCopyState("failed");
    }
  };

  const copyContext = async () => {
    try {
      if (!navigator.clipboard) {
        throw new Error("Clipboard API unavailable");
      }

      await navigator.clipboard.writeText(
        buildCopyContext({ plan, completionReady, counts, currentPass }),
      );
      setContextCopyState("copied");
    } catch {
      setContextCopyState("failed");
    }
  };

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 px-4 py-4 sm:px-6 sm:py-5">
      <section className="border-b border-[var(--relay-row-border)] pb-4">
        <div className="mb-3 flex items-center gap-1.5 text-xs">
          <Link
            to="/plans"
            className="inline-flex items-center gap-1 text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="size-3" />
            Plans
          </Link>
          <span className="text-muted-foreground/60">·</span>
          <span className="max-w-xs truncate text-[11px] text-muted-foreground sm:max-w-sm">
            {plan.title}
          </span>
        </div>

        <div className="mb-2.5 flex flex-wrap items-start gap-2.5">
          <h1 className="min-w-0 text-xl font-semibold tracking-tight text-foreground">
            {plan.title}
          </h1>
          <Badge
            variant={getPlanStatusVariant(plan.status)}
            className="h-auto rounded-sm px-2 py-0.5 text-[10px] font-medium tracking-wide"
          >
            {getPlanStatusLabel(plan.status)}
          </Badge>
          {completionReady && plan.status === "active" ? (
            <Badge
              variant="warning"
              className="h-auto rounded-sm px-2 py-0.5 text-[10px] font-medium tracking-wide"
            >
              Completion Ready
            </Badge>
          ) : null}
        </div>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px]">
          <button
            type="button"
            onClick={copyPlanId}
            className="group inline-flex items-center gap-1 font-mono text-muted-foreground transition-colors hover:text-foreground"
            title="Copy plan ID"
          >
            <span>{plan.planId}</span>
            <Copy className="size-3 opacity-50 transition-opacity group-hover:opacity-100" />
            {planIdCopyState === "copied" ? (
              <span className="text-[10px] text-[var(--success)]">Copied</span>
            ) : null}
            {planIdCopyState === "failed" ? (
              <span className="text-[10px] text-destructive">Copy failed</span>
            ) : null}
          </button>
          <span className="text-muted-foreground/60">·</span>
          <span className="font-mono text-muted-foreground">{plan.repoTarget}</span>
          <span className="text-muted-foreground/60">/</span>
          <span className="font-mono text-muted-foreground">{plan.branchContext}</span>
          {plan.sourceArtifactPath ? (
            <>
              <span className="text-muted-foreground/60">·</span>
              <span className="max-w-[280px] truncate font-mono text-muted-foreground">
                {plan.sourceArtifactPath}
              </span>
            </>
          ) : null}
          <span className="text-muted-foreground/60">·</span>
          <span className="text-muted-foreground" title={formatPlanDate(plan.updatedAt)}>
            Updated {formatPlanDateRelative(plan.updatedAt)}
          </span>
        </div>
      </section>

      <section className="relative border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-5 py-4">
        <div
          className={cn("absolute inset-y-0 left-0 w-[2px]", cardState.accentClassName)}
        />

        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0 flex-1 pl-1">
            <div
              className={cn(
                "font-mono text-[10px] uppercase tracking-[0.18em]",
                cardState.eyebrowClassName,
              )}
            >
              {cardState.eyebrow}
            </div>
            <div className="mt-1 text-sm font-medium leading-snug text-foreground">
              {cardState.title}
            </div>
            {cardState.subtitle ? (
              <div className="mt-1 max-w-xl text-xs leading-relaxed text-muted-foreground">
                {cardState.subtitle}
              </div>
            ) : null}
          </div>

          {plan.status === "active" ? (
            <div className="flex shrink-0 flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="xs"
                className="rounded-sm px-3 text-[11px]"
                onClick={copyContext}
              >
                {contextCopyState === "copied"
                  ? "Copied"
                  : contextCopyState === "failed"
                    ? "Copy failed"
                    : "Copy context"}
              </Button>
              {currentPass ? (
                <Button
                  asChild
                  variant="outline"
                  size="xs"
                  className="rounded-sm px-3 text-[11px]"
                >
                  <Link
                    to="/plans/$planId/passes/$passId"
                    params={{ planId: plan.planId, passId: currentPass.passId }}
                  >
                    Open current pass
                  </Link>
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      </section>

      {progress.total > 0 ? (
        <section className="flex flex-wrap items-center gap-4 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-5 py-3">
          <div className="flex gap-px">
            {Array.from({ length: progress.segmentCount }).map((_, index) => {
              let className = "bg-[var(--relay-row-border)]";
              if (index < progress.completedSegments) {
                className = "bg-[var(--success)]/70";
              } else if (
                index <
                progress.completedSegments + progress.skippedSegments
              ) {
                className = "bg-muted-foreground/55";
              } else if (
                index <
                progress.completedSegments +
                  progress.skippedSegments +
                  progress.inProgressSegments
              ) {
                className = "bg-[var(--relay-accent)]/80";
              }

              return (
                <div
                  key={`progress-segment-${index}`}
                  className={cn("h-1.5 rounded-[1px]", className)}
                  style={{ width: "8px" }}
                />
              );
            })}
          </div>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[11px]">
            <span className="text-muted-foreground">{progress.total} passes</span>
            {completionReady && plan.status === "active" ? (
              <>
                <span className="text-muted-foreground/60">·</span>
                <span className="text-warning">
                  {progress.terminal} terminal, ready for closeout
                </span>
              </>
            ) : null}
            {progress.completed > 0 ? (
              <>
                <span className="text-muted-foreground/60">·</span>
                <span className="text-[var(--success)]">
                  {progress.completed} completed
                </span>
              </>
            ) : null}
            {progress.inProgress > 0 ? (
              <>
                <span className="text-muted-foreground/60">·</span>
                <span className="text-[var(--relay-accent)]">
                  {progress.inProgress} in progress
                </span>
              </>
            ) : null}
            {progress.planned > 0 ? (
              <>
                <span className="text-muted-foreground/60">·</span>
                <span className="text-muted-foreground">{progress.planned} planned</span>
              </>
            ) : null}
            {progress.skipped > 0 ? (
              <>
                <span className="text-muted-foreground/60">·</span>
                <span className="text-muted-foreground/80">{progress.skipped} skipped</span>
              </>
            ) : null}
          </div>
        </section>
      ) : null}

      {sortedPasses.length > 0 ? (
        <section>
          <div className="mb-2">
            <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
              Passes — {sortedPasses.length}
            </span>
          </div>
          <RelayPlanPassTimeline planId={plan.planId} passes={sortedPasses} />
        </section>
      ) : (
        <RelayStateSurface
          tone="empty"
          title="No passes available"
          description="This plan detail loaded successfully, but it does not include any pass entries yet."
        />
      )}

      <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
        <div className="border-b border-[var(--relay-row-border)] px-5 py-2.5">
          <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
            Plan Context
          </span>
        </div>

        {plan.sourceIntentSummary || plan.goal ? (
          <div className="border-b border-[var(--relay-row-border)] px-5 py-3">
            <div className="mb-1.5 text-[10px] text-muted-foreground">Source intent</div>
            <div className="max-w-2xl text-xs leading-relaxed text-muted-foreground">
              {plan.sourceIntentSummary || plan.goal}
            </div>
          </div>
        ) : null}

        <div className="flex flex-wrap gap-x-8 gap-y-3 px-5 py-3">
          {plan.sourceArtifactPath ? (
            <div className="flex flex-col gap-0.5">
              <span className="text-[10px] text-muted-foreground">Artifact</span>
              <span className="break-all font-mono text-[11px] text-foreground">
                {plan.sourceArtifactPath}
              </span>
            </div>
          ) : null}
          <div className="flex flex-col gap-0.5">
            <span className="text-[10px] text-muted-foreground">Repo</span>
            <span className="break-all font-mono text-[11px] text-foreground">
              {plan.repoTarget}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[10px] text-muted-foreground">Branch</span>
            <span className="break-all font-mono text-[11px] text-foreground">
              {plan.branchContext}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[10px] text-muted-foreground">Plan ID</span>
            <span className="break-all font-mono text-[11px] text-foreground">
              {plan.planId}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[10px] text-muted-foreground">Updated</span>
            <span
              className="text-[11px] text-muted-foreground"
              title={formatPlanDate(plan.updatedAt)}
            >
              {formatPlanDateRelative(plan.updatedAt)}
            </span>
          </div>
        </div>
      </section>
    </div>
  );
}
