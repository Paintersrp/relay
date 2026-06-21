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

interface RelayPlanPassTimelineProps {
  passes: PlanAPIPass[];
}

export function RelayPlanPassTimeline({ passes }: RelayPlanPassTimelineProps) {
  const sortedPasses = sortPassesBySequence(passes);

  return (
    <div className="space-y-4">
      {sortedPasses.map((pass) => {
        const unmetDependencies = getUnmetDependencies(pass, sortedPasses);

        return (
          <article
            key={pass.id}
            className="rounded-xl border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4"
          >
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-start gap-3">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] text-sm font-semibold text-foreground">
                    {pass.sequence}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="text-base font-semibold text-foreground">
                        {pass.name}
                      </h3>
                      <Badge
                        variant={getPassStatusVariant(pass.status)}
                        className="text-[11px] font-medium"
                      >
                        {getPassStatusLabel(pass.status)}
                      </Badge>
                    </div>

                    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
                      <RelayMonoText className="text-[11px] text-muted-foreground">
                        {pass.passId}
                      </RelayMonoText>
                    </div>

                    <p className="mt-3 text-sm leading-6 text-muted-foreground">
                      {pass.goal}
                    </p>
                  </div>
                </div>

                {pass.intendedExecutionScope.length > 0 ? (
                  <div className="mt-4">
                    <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                      Intended Scope
                    </p>
                    <div className="mt-2 flex flex-wrap gap-2">
                      {pass.intendedExecutionScope.map((scope) => (
                        <span
                          key={scope}
                          className="rounded-full border border-[var(--relay-row-border)] bg-[var(--relay-panel-hover-bg)] px-2 py-1 font-mono text-[11px] text-muted-foreground"
                        >
                          {scope}
                        </span>
                      ))}
                    </div>
                  </div>
                ) : null}

                {(pass.dependencies.length > 0 || unmetDependencies.length > 0) ? (
                  <div className="mt-4 space-y-2">
                    {pass.dependencies.length > 0 ? (
                      <div>
                        <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                          Dependencies
                        </p>
                        <div className="mt-2 flex flex-wrap gap-2">
                          {pass.dependencies.map((dependency) => (
                            <Badge key={dependency} variant="outline" className="text-[11px]">
                              {dependency}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    ) : null}

                    {unmetDependencies.length > 0 ? (
                      <div>
                        <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                          Blockers
                        </p>
                        <div className="mt-2 flex flex-wrap gap-2">
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
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>

              <div className="flex shrink-0 items-center lg:justify-end">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled
                  title="Pass detail in UI-PLAN-04"
                >
                  Pass detail in UI-PLAN-04
                </Button>
              </div>
            </div>
          </article>
        );
      })}
    </div>
  );
}
