import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { plansListQueryOptions } from "@/features/relay-plans";
import { Badge } from "@/components/ui/badge";

interface RelayProjectPlansPanelProps {
  projectId: string;
}

export function RelayProjectPlansPanel({ projectId }: RelayProjectPlansPanelProps) {
  const plansQuery = useQuery(plansListQueryOptions({ projectId, limit: 100 }));

  if (plansQuery.isLoading) {
    return (
      <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
        <div className="border-b border-[var(--relay-row-border)] px-5 py-3">
          <h2 className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
            Managed Plans
          </h2>
        </div>
        <div className="p-5 text-sm text-muted-foreground">Loading plans...</div>
      </section>
    );
  }

  if (plansQuery.isError) {
    return (
      <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
        <div className="border-b border-[var(--relay-row-border)] px-5 py-3">
          <h2 className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
            Managed Plans
          </h2>
        </div>
        <div className="p-5 text-sm text-destructive">
          Failed to load plans: {plansQuery.error instanceof Error ? plansQuery.error.message : "Unknown error"}
        </div>
      </section>
    );
  }

  const plans = plansQuery.data?.plans || [];

  return (
    <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      <div className="border-b border-[var(--relay-row-border)] px-5 py-3">
        <h2 className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
          Managed Plans
        </h2>
      </div>

      {plans.length === 0 ? (
        <div className="p-5 text-sm text-muted-foreground">
          No managed plans are currently scoped to this project.
        </div>
      ) : (
        <div className="divide-y divide-[var(--relay-row-border)]">
          {plans.map((plan) => (
            <Link
              key={plan.id}
              to="/plans/$planId"
              params={{ planId: plan.planId }}
              className="block px-5 py-3 hover:bg-[var(--relay-content-bg)] transition-colors"
            >
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-sm">{plan.title}</span>
                    <Badge variant={plan.status === "active" ? "running" : "outline"} className="text-xs">
                      {plan.status}
                    </Badge>
                  </div>
                  <div className="font-mono text-xs text-muted-foreground">
                    {plan.planId}
                  </div>
                  {plan.goal && (
                    <div className="text-xs text-muted-foreground line-clamp-2">
                      {plan.goal}
                    </div>
                  )}
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span>Project: {plan.projectId || projectId}</span>
                    <span>•</span>
                    <span>{plan.passCount} passes</span>
                    {plan.completedPassCount !== undefined && (
                      <>
                        <span>•</span>
                        <span>{plan.completedPassCount} completed</span>
                      </>
                    )}
                    {plan.completionReady && (
                      <>
                        <span>•</span>
                        <Badge variant="warning" className="text-xs">Completion Ready</Badge>
                      </>
                    )}
                  </div>
                </div>
                <div className="text-xs text-muted-foreground whitespace-nowrap">
                  {new Date(plan.updatedAt).toLocaleDateString()}
                </div>
              </div>
              {(plan.currentPassName || plan.nextPassName) && (
                <div className="mt-2 text-xs">
                  {plan.currentPassName && (
                    <div className="flex items-center gap-2">
                      <span className="text-muted-foreground">Current:</span>
                      <span>{plan.currentPassName}</span>
                    </div>
                  )}
                  {plan.nextPassName && (
                    <div className="flex items-center gap-2">
                      <span className="text-muted-foreground">Next:</span>
                      <span>{plan.nextPassName}</span>
                    </div>
                  )}
                </div>
              )}
            </Link>
          ))}
        </div>
      )}
    </section>
  );
}
