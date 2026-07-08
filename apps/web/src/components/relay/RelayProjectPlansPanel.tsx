import { Link } from "@tanstack/react-router";
import { ArrowRight, Plus } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
  WorkflowProject,
  WorkflowProjectPlanSummary,
} from "@/features/relay-projects";

interface RelayProjectPlansPanelProps {
  project: WorkflowProject;
  plans: WorkflowProjectPlanSummary[];
}

export function RelayProjectPlansPanel({
  project,
  plans,
}: RelayProjectPlansPanelProps) {
  const planSearch = { projectId: project.projectId };

  return (
    <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-5 py-3">
        <div>
          <h2 className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
            Attached Plans
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Plans are organized by this Project without giving it authority over canonical content or execution.
          </p>
        </div>
        {project.status === "active" ? (
          <Button asChild size="sm">
            <Link to="/plans/new" search={planSearch}>
              <Plus className="size-3.5" />
              Submit Plan
            </Link>
          </Button>
        ) : (
          <span className="max-w-xs text-right text-xs text-muted-foreground">
            Restore this Project before submitting or moving another Plan into it.
          </span>
        )}
      </div>

      {plans.length === 0 ? (
        <div className="p-5 text-sm text-muted-foreground">
          No Plans are attached to this Project.
        </div>
      ) : (
        <div className="divide-y divide-[var(--relay-row-border)]">
          {plans.map((plan) => {
            const planParams = { planId: plan.planId };
            return (
              <Link
                key={plan.planId}
                to="/plans/$planId"
                params={planParams}
                className="flex flex-col gap-3 px-5 py-3 transition-colors hover:bg-[var(--relay-content-bg)] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-[var(--relay-accent)] sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium text-foreground">{plan.featureSlug}</span>
                    <Badge variant={plan.status === "active" ? "running" : "outline"}>
                      {plan.status}
                    </Badge>
                  </div>
                  <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">
                    {plan.planId}
                  </p>
                </div>
                <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                  Open Plan
                  <ArrowRight className="size-3.5" />
                </span>
              </Link>
            );
          })}
        </div>
      )}
    </section>
  );
}
