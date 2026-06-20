import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import {
  runsListQueryOptions,
  getActiveStepRoute,
  formatRunDate,
  formatRunDateRelative,
} from "@/features/relay-runs";
import { StatusBadge } from "@/components/relay/StatusBadge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { PlusCircle, GitBranch, Bot, ArrowRight, GitFork } from "lucide-react";

export const Route = createFileRoute("/runs/")({
  component: RunsListPage,
});

function RunsListPage() {
  const { data: runs, isLoading } = useQuery(runsListQueryOptions);

  return (
    <div className="flex flex-col flex-1 overflow-y-auto">
      {/* Page header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-border/60">
        <div>
          <h1 className="text-lg font-semibold">Runs</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Handoff orchestration runs
          </p>
        </div>
        <Button size="sm" asChild className="gap-1.5">
          <Link to="/runs/new">
            <PlusCircle className="w-4 h-4" />
            New Run
          </Link>
        </Button>
      </div>

      {/* Runs list */}
      <div className="flex flex-col gap-3 p-6">
        {isLoading
          ? Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-28 w-full rounded-lg" />
            ))
          : runs?.map((run) => (
              <Card
                key={run.id}
                className="border-border/60 bg-card/40 hover:bg-card/60 transition-colors"
              >
                <CardHeader className="p-4 pb-2">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex flex-col gap-0.5 min-w-0">
                      <CardTitle className="text-sm font-semibold leading-tight truncate">
                        {run.title}
                      </CardTitle>
                      {run.packetId && (
                        <p className="font-mono text-xs text-muted-foreground truncate">
                          {run.packetId}
                        </p>
                      )}
                    </div>
                    <StatusBadge status={run.status} className="shrink-0" />
                  </div>
                </CardHeader>
                <CardContent className="p-4 pt-2">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground mb-3">
                    <span className="flex items-center gap-1">
                      <GitFork className="w-3 h-3" />
                      {run.repo}
                    </span>
                    <span className="flex items-center gap-1">
                      <GitBranch className="w-3 h-3" />
                      {run.branch}
                    </span>
                    <span className="flex items-center gap-1">
                      <Bot className="w-3 h-3" />
                      {run.executor}
                    </span>
                    <span
                      className="ml-auto tabular-nums"
                      title={formatRunDate(run.updatedAt)}
                    >
                      {formatRunDateRelative(run.updatedAt)}
                    </span>
                  </div>

                  {/* Step progress indicators */}
                  <div className="flex items-center gap-1 mb-3">
                    {(["intake", "prepare", "execute", "audit"] as const).map(
                      (step) => {
                        const steps = ["intake", "prepare", "execute", "audit"];
                        const stepIdx = steps.indexOf(step);
                        const activeIdx = steps.indexOf(run.activeStep);
                        const isCompleted = stepIdx < activeIdx;
                        const isActive = step === run.activeStep;
                        return (
                          <span
                            key={step}
                            className={`text-xs px-2 py-0.5 rounded border transition-colors ${
                              isActive
                                ? "border-primary/40 bg-primary/10 text-primary"
                                : isCompleted
                                  ? "border-emerald-600/30 bg-emerald-600/10 text-emerald-400"
                                  : "border-border/40 text-muted-foreground/50"
                            }`}
                          >
                            {run.stepLabels[step]}
                          </span>
                        );
                      },
                    )}
                  </div>

                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      {run.validationSummary.errors > 0 && (
                        <Badge variant="destructive" className="text-xs">
                          {run.validationSummary.errors} error
                          {run.validationSummary.errors !== 1 ? "s" : ""}
                        </Badge>
                      )}
                      {run.validationSummary.warnings > 0 && (
                        <Badge variant="destructive" className="text-xs">
                          {run.validationSummary.warnings} warning
                          {run.validationSummary.warnings !== 1 ? "s" : ""}
                        </Badge>
                      )}
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      asChild
                      className="gap-1.5 h-7 text-xs"
                    >
                      <Link to={getActiveStepRoute(run)}>
                        Open Workbench
                        <ArrowRight className="w-3 h-3" />
                      </Link>
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))}
      </div>
    </div>
  );
}
