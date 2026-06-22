import { Link } from "@tanstack/react-router";
import { Copy, ExternalLink } from "lucide-react";

import {
  getPassStatusLabel,
  getUnmetDependencies,
  sortPassesBySequence,
} from "@/components/relay/relayPlanVisualState";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { PlanAPIPass } from "@/features/relay-plans";
import { cn } from "@/lib/utils";

interface RelayPlanPassTimelineProps {
  planId: string;
  passes: PlanAPIPass[];
}

function copyText(text: string) {
  void navigator.clipboard?.writeText(text);
}

function getStatusDotClass(pass: PlanAPIPass): string {
  switch (pass.status) {
    case "completed":
      return "bg-[var(--success)]";
    case "in_progress":
      return "bg-[var(--relay-accent)]";
    case "planned":
      return "bg-muted-foreground/70";
    case "skipped":
      return "bg-muted-foreground/45";
  }
}

function getStatusBadgeClass(pass: PlanAPIPass): string {
  switch (pass.status) {
    case "completed":
      return "border-[var(--success)]/35 bg-[var(--success)]/10 text-[var(--success)]";
    case "in_progress":
      return "border-[var(--relay-accent)]/35 bg-[var(--relay-accent)]/10 text-[var(--relay-accent)]";
    case "planned":
      return "border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] text-muted-foreground";
    case "skipped":
      return "border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] text-muted-foreground/80";
  }
}

function getRowAccentClass(pass: PlanAPIPass, unmetDependencies: string[]): string | null {
  if (pass.status === "in_progress") return "bg-[var(--relay-accent)]";
  if (unmetDependencies.length > 0) return "bg-destructive";
  if (pass.status === "completed") return "bg-[var(--success)]/65";
  return null;
}

function getActionConfig(pass: PlanAPIPass, unmetDependencies: string[]) {
  if (pass.status === "planned" && unmetDependencies.length > 0) {
    return {
      label: "Open",
      title: "Open blocked pass detail",
      icon: <ExternalLink className="size-3" />,
    };
  }

  if (pass.status === "planned") {
    return {
      label: "Open",
      title: "Open pass detail",
      icon: <ExternalLink className="size-3" />,
    };
  }

  return {
    label: pass.status === "in_progress" ? "Open" : "View",
    title: "Open pass detail",
    icon: <ExternalLink className="size-3" />,
  };
}

export function RelayPlanPassTimeline({ planId, passes }: RelayPlanPassTimelineProps) {
  const sortedPasses = sortPassesBySequence(passes);

  if (sortedPasses.length === 0) {
    return (
      <div className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-5 py-8 text-center text-xs text-muted-foreground">
        No passes defined
      </div>
    );
  }

  return (
    <div className="overflow-hidden border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      {sortedPasses.map((pass) => {
        const unmetDependencies = getUnmetDependencies(pass, sortedPasses);
        const accentClass = getRowAccentClass(pass, unmetDependencies);
        const isCompleted = pass.status === "completed";
        const isCurrent = pass.status === "in_progress";
        const action = getActionConfig(pass, unmetDependencies);

        return (
          <article
            key={pass.id}
            className={cn(
              "relative border-b border-[var(--relay-row-border)] transition-colors last:border-b-0",
              isCurrent && "bg-[var(--relay-panel-hover-bg)]",
            )}
          >
            {accentClass ? (
              <div className={cn("absolute inset-y-0 left-0 w-[2px]", accentClass)} />
            ) : null}

            <div className="flex flex-col gap-3 py-3 pr-4 pl-5 sm:flex-row sm:items-start">
              <div className="flex w-5 shrink-0 flex-col items-center gap-1.5 pt-0.5">
                <span className="font-mono text-[10px] leading-none text-muted-foreground">
                  {pass.sequence}
                </span>
                <span className={cn("h-1.5 w-1.5 rounded-full", getStatusDotClass(pass))} />
              </div>

              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span
                    className={cn(
                      "text-[13px] font-medium leading-snug",
                      isCompleted ? "text-muted-foreground" : "text-foreground",
                    )}
                  >
                    {pass.name}
                  </span>
                  <span className="font-mono text-[10px] text-muted-foreground">
                    {pass.passId}
                  </span>
                  <Badge
                    variant="outline"
                    className={cn(
                      "h-auto rounded-sm px-1.5 py-px text-[9px] font-medium tracking-wide",
                      getStatusBadgeClass(pass),
                    )}
                  >
                    {getPassStatusLabel(pass.status)}
                  </Badge>
                </div>

                {!isCompleted && pass.goal ? (
                  <div className="mt-0.5 truncate text-[11px] leading-snug text-muted-foreground">
                    {pass.goal}
                  </div>
                ) : null}

                {isCurrent && pass.intendedExecutionScope.length > 0 ? (
                  <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">
                    {pass.intendedExecutionScope.join(", ")}
                  </div>
                ) : null}

                {unmetDependencies.length > 0 ? (
                  <div className="mt-2 flex flex-wrap items-center gap-1">
                    {unmetDependencies.map((dependency) => (
                      <span
                        key={`${pass.id}-${dependency}`}
                        className="inline-flex items-center rounded-sm border border-destructive/35 bg-destructive/10 px-1.5 py-px font-mono text-[10px] text-destructive"
                      >
                        Blocked by {dependency}
                      </span>
                    ))}
                  </div>
                ) : pass.dependencies.length > 0 ? (
                  <div className="mt-2 flex flex-wrap items-center gap-1">
                    {pass.dependencies.map((dependency) => (
                      <span
                        key={`${pass.id}-${dependency}`}
                        className="inline-flex items-center rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-1.5 py-px font-mono text-[10px] text-muted-foreground"
                      >
                        {dependency}
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>

              <div className="flex shrink-0 items-center gap-2 self-start sm:pt-0.5">
                <span className="min-w-[68px] text-right font-mono text-[10px] text-muted-foreground/80">
                  No run yet
                </span>

                <Button
                  asChild
                  variant="outline"
                  size="xs"
                  title={action.title}
                  className="rounded-sm px-2 text-[11px]"
                >
                  <Link
                    to="/plans/$planId/passes/$passId"
                    params={{ planId, passId: pass.passId }}
                  >
                    {action.icon}
                    {action.label}
                  </Link>
                </Button>

                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="rounded-sm text-muted-foreground hover:text-foreground"
                  onClick={() => copyText(pass.passId)}
                  title={`Copy ${pass.passId}`}
                >
                  <Copy className="size-3" />
                  <span className="sr-only">Copy pass ID</span>
                </Button>
              </div>
            </div>
          </article>
        );
      })}
    </div>
  );
}
