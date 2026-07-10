import { Link } from "@tanstack/react-router";
import { ArrowLeft, ArrowRight } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
  WorkflowPlanPass,
  WorkflowPlanSummary,
} from "@/features/relay-plans";
import { workflowRunStageRoute } from "@/features/relay-runs";

interface RelayCanonicalPlanPassDetailProps {
  plan: WorkflowPlanSummary;
  pass: WorkflowPlanPass;
}

export function RelayCanonicalPlanPassDetail({
  plan,
  pass,
}: RelayCanonicalPlanPassDetailProps) {
  return (
    <div className="space-y-5">
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link to="/plans/$planId" params={{ planId: plan.planId }}>
          <ArrowLeft className="size-4" />
          Back to Plan
        </Link>
      </Button>
      <section className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-xl font-semibold">
                Pass {pass.number}: {pass.name}
              </h1>
              <Badge variant="outline">{pass.status}</Badge>
            </div>
            <p className="mt-2 font-mono text-xs text-muted-foreground">
              {pass.passId} · {pass.repoTarget}
            </p>
          </div>
          <Button asChild size="sm">
            <Link
              to="/runs/new"
              search={{
                planId: plan.planId,
                passId: pass.passId,
                passNumber: pass.number,
              }}
            >
              Create Managed Run
              <ArrowRight className="size-3.5" />
            </Link>
          </Button>
        </div>
        <p className="mt-4 text-xs text-muted-foreground">
          Dependencies:{" "}
          {pass.dependsOn.length > 0 ? pass.dependsOn.join(", ") : "None"}
        </p>
      </section>

      <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
        <header className="border-b border-[var(--relay-row-border)] px-5 py-3">
          <h2 className="text-sm font-semibold">Runs for this pass</h2>
        </header>
        {pass.runs.length === 0 ? (
          <p className="p-5 text-sm text-muted-foreground">
            No Runs have been created for this pass.
          </p>
        ) : (
          <div className="divide-y divide-[var(--relay-row-border)]">
            {pass.runs.map((run) => (
              <Link
                key={run.runId}
                to={workflowRunStageRoute(run.stage)}
                params={{ runId: run.runId }}
                className="flex items-center justify-between gap-3 px-5 py-4 hover:bg-[var(--relay-content-bg)]"
              >
                <div>
                  <p className="font-mono text-xs">{run.runId}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {run.branch} · {run.baseCommit}
                  </p>
                </div>
                <Badge variant="outline">{run.status}</Badge>
              </Link>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
