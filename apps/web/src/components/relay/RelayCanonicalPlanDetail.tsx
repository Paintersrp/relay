import { Link } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";

import { RelayArtifactViewer } from "@/components/relay/RelayArtifactViewer";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { WorkflowPlanDetail } from "@/features/relay-plans";

interface RelayCanonicalPlanDetailProps {
  detail: WorkflowPlanDetail;
}

// Read-only presentation of a historical Plan. Legacy Plan writes are retired:
// this surface exposes no Plan mutation control and authorizes no execution.
export function RelayCanonicalPlanDetail({
  detail,
}: RelayCanonicalPlanDetailProps) {
  const plan = detail.plan;

  return (
    <div className="space-y-5">
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link to="/plans">
          <ArrowLeft className="size-4" />
          Back to Plans
        </Link>
      </Button>

      <section className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-xl font-semibold">{plan.featureSlug}</h1>
              <Badge
                variant={plan.status === "completed" ? "success" : "running"}
              >
                {plan.status}
              </Badge>
              <Badge variant="outline">{plan.project.name}</Badge>
              <Badge
                variant={
                  plan.project.status === "archived" ? "secondary" : "success"
                }
              >
                {plan.project.status}
              </Badge>
            </div>
            <p className="mt-2 break-all font-mono text-xs text-muted-foreground">
              {plan.planId} · {plan.canonicalSha256}
            </p>
          </div>
          <Badge variant="secondary" className="self-start">
            Historical record
          </Badge>
        </div>
      </section>

      <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
        <header className="border-b border-[var(--relay-row-border)] px-5 py-3">
          <h2 className="text-sm font-semibold">Passes</h2>
        </header>
        <div className="divide-y divide-[var(--relay-row-border)]">
          {detail.passes.map((pass) => (
            <div key={pass.passId} className="px-5 py-4">
              <Link
                to="/plans/$planId/passes/$passId"
                params={{ planId: plan.planId, passId: pass.passId }}
                className="block min-w-0 hover:underline"
              >
                <p className="font-medium">
                  Pass {pass.number}: {pass.name}
                </p>
                <p className="mt-1 font-mono text-[10px] text-muted-foreground">
                  {pass.passId} · {pass.repoTarget} · {pass.status}
                </p>
              </Link>
            </div>
          ))}
        </div>
      </section>

      <section className="grid gap-4 lg:grid-cols-2">
        <div className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5">
          <h2 className="text-sm font-semibold">Repository targets</h2>
          {detail.repositories.map((repository) => (
            <p
              key={`${repository.repoTarget}-${repository.sequence}`}
              className="mt-3 break-all font-mono text-xs"
            >
              {repository.repoTarget} · {repository.branch} ·{" "}
              {repository.planningBaseCommit}
            </p>
          ))}
        </div>
        <div className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5">
          <h2 className="text-sm font-semibold">Canonical artifacts</h2>
          {detail.artifacts.map((artifact) => (
            <RelayArtifactViewer
              key={artifact.artifactId}
              artifact={artifact}
              className="mt-3 p-3"
            />
          ))}
        </div>
      </section>
    </div>
  );
}
