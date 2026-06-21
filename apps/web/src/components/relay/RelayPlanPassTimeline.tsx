import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RelayMonoText } from "@/components/relay/RelayMeta";
import {
  getPassStatusLabel,
  getPassStatusVariant,
  getUnmetDependencies,
  sortPassesBySequence,
} from "@/components/relay/relayPlanVisualState";
import type { PlanAPIPass } from "@/features/relay-plans";
import { cn } from "@/lib/utils";

interface RelayPlanPassTimelineProps {
  passes: PlanAPIPass[];
}

function getRowTone(pass: PlanAPIPass, unmetDependencies: string[]): string {
  if (pass.status === "completed") {
    return "border-[var(--success)]/35 bg-[var(--relay-panel-bg)]";
  }

  if (pass.status === "in_progress") {
    return "border-[var(--relay-accent)]/45 bg-[var(--relay-panel-hover-bg)]";
  }

  if (unmetDependencies.length > 0) {
    return "border-destructive/35 bg-destructive/5";
  }

  return "border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]";
}

function getRailTone(pass: PlanAPIPass, unmetDependencies: string[]): string {
  if (pass.status === "completed") return "bg-[var(--success)]";
  if (pass.status === "in_progress") return "bg-[var(--relay-accent)]";
  if (pass.status === "skipped") return "bg-muted-foreground";
  if (unmetDependencies.length > 0) return "bg-destructive";
  return "bg-[var(--relay-row-border)]";
}

function getActionLabel(pass: PlanAPIPass): string {
  switch (pass.status) {
    case "in_progress":
      return "Open";
    case "completed":
    case "skipped":
      return "View";
    case "planned":
      return "Waiting";
  }
}

export function RelayPlanPassTimeline({ passes }: RelayPlanPassTimelineProps) {
  const sortedPasses = sortPassesBySequence(passes);

  return (
    <div className="overflow-hidden rounded-xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      {sortedPasses.map((pass, index) => {
        const unmetDependencies = getUnmetDependencies(pass, sortedPasses);
        const isLast = index === sortedPasses.length - 1;

        return (
          <article
            key={pass.id}
            className={cn(
              "relative grid gap-4 border-b p-4 last:border-b-0 lg:grid-cols-[3rem_minmax(0,1fr)_8rem]",
              getRowTone(pass, unmetDependencies),
            )}
          >
            <div className="relative flex lg:justify-center">
              {!isLast ? (
                <span className="absolute left-[1.125rem] top-10 hidden h-[calc(100%+1rem)] w-px bg-[var(--relay-row-border)] lg:block" />
              ) : null}
              <span
                className={cn(
                  "z-10 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-sm font-semibold text-background",
                  getRailTone(pass, unmetDependencies),
                )}
              >
                {pass.sequence}
              </span>
            </div>

            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="min-w-0 text-base font-semibold text-foreground">
                  {pass.name}
                </h3>
                <Badge
                  variant={getPassStatusVariant(pass.status)}
                  className="text-[11px] font-medium"
                >
                  {getPassStatusLabel(pass.status)}
                </Badge>
                {unmetDependencies.length > 0 ? (
                  <Badge variant="destructive" className="text-[11px] font-medium">
                    Blocked
                  </Badge>
                ) : null}
              </div>

              <RelayMonoText className="mt-1 block text-[11px] text-muted-foreground">
                {pass.passId}
              </RelayMonoText>

              <p className="mt-2 text-sm leading-6 text-muted-foreground">{pass.goal}</p>

              <div className="mt-3 flex flex-wrap gap-2">
                {pass.intendedExecutionScope.map((scope) => (
                  <span
                    key={scope}
                    className="rounded-full border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] px-2 py-1 font-mono text-[11px] text-muted-foreground"
                  >
                    {scope}
                  </span>
                ))}
              </div>

              {pass.dependencies.length > 0 || unmetDependencies.length > 0 ? (
                <div className="mt-3 flex flex-wrap gap-2">
                  {pass.dependencies.map((dependency) => (
                    <Badge key={dependency} variant="outline" className="text-[11px]">
                      Depends on {dependency}
                    </Badge>
                  ))}
                  {unmetDependencies.map((dependency) => (
                    <Badge
                      key={`${pass.id}-${dependency}`}
                      variant="destructive"
                      className="text-[11px]"
                    >
                      Blocked by {dependency}
                    </Badge>
                  ))}
                </div>
              ) : null}
            </div>

            <div className="flex items-start lg:justify-end">
              {pass.status === "planned" ? (
                <span className="rounded-md border border-[var(--relay-row-border)] px-3 py-1.5 text-xs font-medium text-muted-foreground">
                  {unmetDependencies.length > 0 ? "Blocked" : getActionLabel(pass)}
                </span>
              ) : (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled
                  title="Pass detail arrives in UI-PLAN-04"
                >
                  {getActionLabel(pass)}
                </Button>
              )}
            </div>
          </article>
        );
      })}
    </div>
  );
}
