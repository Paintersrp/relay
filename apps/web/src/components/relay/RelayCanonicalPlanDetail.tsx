import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Loader2, MoveRight } from "lucide-react";

import { RelayArtifactViewer } from "@/components/relay/RelayArtifactViewer";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  moveWorkflowPlan,
  workflowPlanKeys,
  type WorkflowPlanDetail,
} from "@/features/relay-plans";
import type { WorkflowProject } from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

interface RelayCanonicalPlanDetailProps {
  detail: WorkflowPlanDetail;
  activeProjects: WorkflowProject[];
}

function errorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  return error instanceof Error ? error.message : "Plan move failed.";
}

export function RelayCanonicalPlanDetail({
  detail,
  activeProjects,
}: RelayCanonicalPlanDetailProps) {
  const queryClient = useQueryClient();
  const [moveOpen, setMoveOpen] = React.useState(false);
  const [destinationProjectId, setDestinationProjectId] = React.useState("");
  const [mutationError, setMutationError] = React.useState<string | null>(null);
  const plan = detail.plan;
  const destinations = activeProjects.filter(
    (project) => project.projectId !== plan.project.projectId,
  );

  const moveMutation = useMutation({
    mutationFn: () =>
      moveWorkflowPlan(plan.planId, { projectId: destinationProjectId }),
    onSuccess: () => {
      setMoveOpen(false);
      setDestinationProjectId("");
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: workflowPlanKeys.all });
    },
    onError: (error) => setMutationError(errorMessage(error)),
  });

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
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={destinations.length === 0}
            onClick={() => {
              setMutationError(null);
              setMoveOpen(true);
            }}
          >
            <MoveRight className="size-4" />
            Move Plan
          </Button>
        </div>
      </section>

      <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
        <header className="border-b border-[var(--relay-row-border)] px-5 py-3">
          <h2 className="text-sm font-semibold">Passes</h2>
        </header>
        <div className="divide-y divide-[var(--relay-row-border)]">
          {detail.passes.map((pass) => (
            <div
              key={pass.passId}
              className="flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between"
            >
              <Link
                to="/plans/$planId/passes/$passId"
                params={{ planId: plan.planId, passId: pass.passId }}
                className="min-w-0 hover:underline"
              >
                <p className="font-medium">
                  Pass {pass.number}: {pass.name}
                </p>
                <p className="mt-1 font-mono text-[10px] text-muted-foreground">
                  {pass.passId} · {pass.repoTarget} · {pass.status}
                </p>
              </Link>
              <Button asChild size="sm" variant="outline">
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

      <Dialog
        open={moveOpen}
        onOpenChange={(open) => {
          if (!moveMutation.isPending) setMoveOpen(open);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Move Plan to another Project</DialogTitle>
            <DialogDescription>
              This changes Relay organizational metadata only.
            </DialogDescription>
          </DialogHeader>
          {mutationError ? (
            <div
              role="alert"
              className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {mutationError}
            </div>
          ) : null}
          <div className="space-y-1.5 py-3">
            <Label htmlFor="move-plan-project">
              Active destination Project
            </Label>
            <select
              id="move-plan-project"
              value={destinationProjectId}
              onChange={(event) =>
                setDestinationProjectId(event.target.value)
              }
              disabled={moveMutation.isPending}
              className="h-9 w-full rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 text-sm"
            >
              <option value="">Select a Project</option>
              {destinations.map((project) => (
                <option key={project.projectId} value={project.projectId}>
                  {project.name}
                </option>
              ))}
            </select>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={moveMutation.isPending}
              onClick={() => setMoveOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={!destinationProjectId || moveMutation.isPending}
              onClick={() => moveMutation.mutate()}
            >
              {moveMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : null}
              Move Plan
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
