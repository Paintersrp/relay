import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";

import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { workflowRunStageRoute } from "@/features/relay-runs";
import type {
  WorkflowRunStage,
  WorkflowRunSummary,
} from "@/features/relay-runs";

interface RelayCanonicalRunsRegistryProps {
  runs?: WorkflowRunSummary[];
  isLoading?: boolean;
  error?: unknown;
}

export function RelayCanonicalRunsRegistry({
  runs = [],
  isLoading = false,
  error,
}: RelayCanonicalRunsRegistryProps) {
  const [stage, setStage] = React.useState<WorkflowRunStage | "all">("all");
  const rows = React.useMemo(
    () =>
      [...runs]
        .filter((run) => stage === "all" || run.stage === stage)
        .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)),
    [runs, stage],
  );

  if (isLoading) {
    return (
      <div className="space-y-3 p-4">
        {Array.from({ length: 5 }).map((_, index) => (
          <Skeleton key={index} className="h-20 w-full rounded" />
        ))}
      </div>
    );
  }
  if (error) {
    return (
      <div className="p-4">
        <RelayStateSurface
          tone="danger"
          title="Runs failed to load"
          description="Relay could not load the canonical Run registry."
        />
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="flex items-center gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          Stage
          <select
            aria-label="Run stage"
            value={stage}
            onChange={(event) =>
              setStage(event.target.value as WorkflowRunStage | "all")
            }
            className="h-8 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2 text-xs"
          >
            <option value="all">All</option>
            <option value="specification">Specification</option>
            <option value="execute">Execute</option>
            <option value="audit">Audit</option>
          </select>
        </label>
        <span className="ml-auto font-mono text-[10px] text-muted-foreground">
          {rows.length} shown / {runs.length} total
        </span>
      </div>

      {rows.length === 0 ? (
        <div className="p-4">
          <RelayStateSurface
            tone="empty"
            title="No Runs match"
            description="Create a Managed or Standalone Run from a canonical Execution Spec."
            action={
              <Button asChild variant="outline" size="sm">
                <Link to="/runs/new">Create Run</Link>
              </Button>
            }
          />
        </div>
      ) : (
        <div className="min-h-0 flex-1 divide-y divide-[var(--relay-row-border)] overflow-y-auto">
          {rows.map((run) => (
            <Link
              key={run.runId}
              to={workflowRunStageRoute(run.stage)}
              params={{ runId: run.runId }}
              className="flex flex-col gap-3 px-5 py-4 hover:bg-[var(--relay-panel-hover-bg)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--relay-accent)] sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{run.featureSlug}</span>
                  <Badge variant="outline">{run.status}</Badge>
                  <Badge variant="secondary">{run.stage}</Badge>
                  {run.project ? (
                    <Badge variant="outline">
                      {run.project.name}
                      {run.project.status === "archived" ? " · archived" : ""}
                    </Badge>
                  ) : (
                    <Badge variant="outline">Standalone</Badge>
                  )}
                </div>
                <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">
                  {run.runId} · {run.repoTarget} · {run.branch}
                </p>
              </div>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                {new Date(run.updatedAt).toLocaleString()}
                <ChevronRight className="size-4" />
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
