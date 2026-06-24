import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { RelayRefactorCandidateForm } from "./RelayRefactorCandidateForm";
import { RelayRefactorCandidatePanel } from "./RelayRefactorCandidatePanel";
import { RelayRefactorDiscoveryTaskForm } from "./RelayRefactorDiscoveryTaskForm";
import { RelayRefactorOnlyPlanReviewPanel } from "./RelayRefactorOnlyPlanReviewPanel";
import {
  canCompleteDiscoveryTask,
  canEditDiscoveryTask,
  canSelectCandidateForGeneratedPlan,
  canSupersedeDiscoveryTask,
  closeRefactorDiscoveryTask,
  completeRefactorDiscoveryTask,
  groupCandidates,
  refactorCandidatesQueryOptions,
  refactorDiscoveryStatusLabel,
  refactorDiscoveryTasksQueryOptions,
  relayRefactorKeys,
  supersedeRefactorDiscoveryTask,
} from "@/features/relay-refactors";
import type {
  RefactorCandidate,
  RefactorDiscoveryTask,
} from "@/features/relay-refactors";

const labelClass = "text-xs font-semibold uppercase tracking-wider text-muted-foreground";

interface RelayRefactorBacklogPageProps {
  projectId: string;
  planId?: string;
  candidateId?: string;
}

type DiscoveryForm =
  | { mode: "none" }
  | { mode: "new" }
  | { mode: "edit"; task: RefactorDiscoveryTask };

type CandidateForm =
  | { mode: "none" }
  | { mode: "new" }
  | { mode: "edit"; candidate: RefactorCandidate };

export function RelayRefactorBacklogPage({
  projectId,
  planId,
  candidateId,
}: RelayRefactorBacklogPageProps) {
  const discoveryQuery = useQuery(
    refactorDiscoveryTasksQueryOptions(projectId, { limit: 100 }),
  );
  const candidatesQuery = useQuery(
    refactorCandidatesQueryOptions(projectId, { limit: 100 }),
  );

  const [discoveryForm, setDiscoveryForm] = React.useState<DiscoveryForm>({ mode: "none" });
  const [candidateForm, setCandidateForm] = React.useState<CandidateForm>({ mode: "none" });
  const [selectedIds, setSelectedIds] = React.useState<string[]>([]);
  const [showGenerate, setShowGenerate] = React.useState(false);

  const candidates = candidatesQuery.data?.candidates ?? [];
  const discoveryTasks = discoveryQuery.data?.discoveryTasks ?? [];
  const grouped = React.useMemo(() => groupCandidates(candidates), [candidates]);

  // Keep selection limited to candidates that are still ready/selectable.
  React.useEffect(() => {
    const selectable = new Set(
      candidates
        .filter((candidate) => canSelectCandidateForGeneratedPlan(candidate.status))
        .map((candidate) => candidate.candidateId),
    );
    setSelectedIds((prev) => prev.filter((id) => selectable.has(id)));
  }, [candidates]);

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((value) => value !== id) : [...prev, id],
    );
  };

  const isLoading = discoveryQuery.isLoading || candidatesQuery.isLoading;
  const loadError = discoveryQuery.error || candidatesQuery.error;

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-4 sm:px-6 sm:py-5">
      {/* Header */}
      <header className="space-y-3 border-b border-[var(--relay-row-border)] pb-4">
        <div className="flex items-center gap-2">
          <Button asChild variant="ghost" size="sm" className="h-8 w-8 p-0">
            <Link to="/projects/$projectId" params={{ projectId }}>
              <ArrowLeft className="h-4 w-4" />
              <span className="sr-only">Back to project</span>
            </Link>
          </Button>
          <span className="text-sm font-medium text-muted-foreground">Back to Project</span>
        </div>

        <div className="space-y-1">
          <h1 className="text-lg font-semibold text-foreground">Refactor Backlog</h1>
          <p className="font-mono text-xs text-muted-foreground">Project: {projectId}</p>
        </div>

        <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">
          Discovery tasks are manual analysis prompts. Candidates must be
          pass-ready before scheduling.
        </p>

        {planId ? (
          <div className="rounded border border-info/25 bg-info/10 p-3 text-xs text-info">
            <p className="font-semibold">Plan context: {planId}</p>
            <p className="mt-1 text-info/90">
              Promotion actions below target this plan only.
            </p>
          </div>
        ) : null}

        {candidateId ? (
          <p className="font-mono text-[11px] text-muted-foreground">
            Focused candidate: {candidateId}
          </p>
        ) : null}
      </header>

      {/* Action area */}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          onClick={() => setDiscoveryForm({ mode: "new" })}
        >
          New Discovery Task
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => setCandidateForm({ mode: "new" })}
        >
          New Pass-Ready Candidate
        </Button>
        <Button
          size="sm"
          onClick={() => setShowGenerate((value) => !value)}
          disabled={selectedIds.length === 0}
        >
          Generate Refactor-Only Plan ({selectedIds.length})
        </Button>
      </div>

      {loadError ? (
        <RelayStateSurface
          tone="danger"
          title="Refactor backlog failed to load"
          description="Relay could not load discovery tasks or candidates for this project. Check the API process and try again."
          metadata={`Project ID: ${projectId}`}
        />
      ) : null}

      {isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full rounded" />
          <Skeleton className="h-24 w-full rounded" />
          <Skeleton className="h-24 w-full rounded" />
        </div>
      ) : null}

      {!isLoading && !loadError ? (
        <>
          {discoveryForm.mode !== "none" ? (
            <RelayRefactorDiscoveryTaskForm
              projectId={projectId}
              task={discoveryForm.mode === "edit" ? discoveryForm.task : undefined}
              onClose={() => setDiscoveryForm({ mode: "none" })}
            />
          ) : null}

          {candidateForm.mode !== "none" ? (
            <RelayRefactorCandidateForm
              projectId={projectId}
              candidate={
                candidateForm.mode === "edit" ? candidateForm.candidate : undefined
              }
              onClose={() => setCandidateForm({ mode: "none" })}
            />
          ) : null}

          {showGenerate ? (
            <RelayRefactorOnlyPlanReviewPanel
              projectId={projectId}
              selectedCandidateIds={selectedIds}
              onGenerated={() => setSelectedIds([])}
            />
          ) : null}

          {/* Discovery tasks */}
          <section className="space-y-3">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold text-foreground">Discovery Tasks</h2>
              <Badge variant="outline">{discoveryTasks.length}</Badge>
            </div>
            {discoveryTasks.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                No discovery tasks yet. Add a manual analysis prompt to get started.
              </p>
            ) : (
              <div className="space-y-3">
                {discoveryTasks.map((task) => (
                  <DiscoveryTaskCard
                    key={task.discoveryTaskId}
                    projectId={projectId}
                    task={task}
                    onEdit={(t) => setDiscoveryForm({ mode: "edit", task: t })}
                  />
                ))}
              </div>
            )}
          </section>

          {/* Candidates */}
          <RelayRefactorCandidatePanel
            projectId={projectId}
            planId={planId}
            candidates={candidates}
            selectedIds={selectedIds}
            onToggleSelect={toggleSelect}
            onEdit={(candidate) => setCandidateForm({ mode: "edit", candidate })}
          />

          {candidates.length === 0 && discoveryTasks.length === 0 ? (
            <RelayStateSurface
              tone="empty"
              title="No refactor backlog items"
              description="Create a discovery task or a pass-ready candidate to populate the backlog."
              metadata={`Ready candidates: ${grouped.ready.length}`}
            />
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function DiscoveryTaskCard({
  projectId,
  task,
  onEdit,
}: {
  projectId: string;
  task: RefactorDiscoveryTask;
  onEdit: (task: RefactorDiscoveryTask) => void;
}) {
  const queryClient = useQueryClient();
  const [supersedeOpen, setSupersedeOpen] = React.useState(false);
  const [supersededBy, setSupersededBy] = React.useState("");
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: relayRefactorKeys.project(projectId) });

  const completeMutation = useMutation({
    mutationFn: () => completeRefactorDiscoveryTask(projectId, task.discoveryTaskId, {}),
    onSuccess: () => void invalidate(),
    onError: (err: unknown) =>
      setErrorMsg(err instanceof Error ? err.message : "Failed to complete task"),
  });

  const closeMutation = useMutation({
    mutationFn: () => closeRefactorDiscoveryTask(projectId, task.discoveryTaskId, {}),
    onSuccess: () => void invalidate(),
    onError: (err: unknown) =>
      setErrorMsg(err instanceof Error ? err.message : "Failed to close task"),
  });

  const supersedeMutation = useMutation({
    mutationFn: () =>
      supersedeRefactorDiscoveryTask(projectId, task.discoveryTaskId, {
        superseded_by_task_id: supersededBy.trim() || undefined,
      }),
    onSuccess: () => {
      setSupersedeOpen(false);
      setSupersededBy("");
      void invalidate();
    },
    onError: (err: unknown) =>
      setErrorMsg(err instanceof Error ? err.message : "Failed to supersede task"),
  });

  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 space-y-2">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-foreground">{task.title}</span>
            <Badge variant="outline">{refactorDiscoveryStatusLabel(task.status)}</Badge>
            {task.priority ? (
              <Badge variant="secondary">{task.priority}</Badge>
            ) : null}
          </div>
          <p className="font-mono text-[11px] text-muted-foreground">
            ID: {task.discoveryTaskId}
          </p>
        </div>
      </div>

      <p className="text-xs leading-relaxed text-muted-foreground">{task.analysisPrompt}</p>

      <p className="font-mono text-[11px] text-muted-foreground">
        Scope: {task.targetScope.kind}
        {task.targetScope.values.length > 0
          ? ` — ${task.targetScope.values.join(", ")}`
          : ""}
      </p>

      {task.closureReason ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-semibold">Closure reason:</span> {task.closureReason}
        </p>
      ) : null}

      {errorMsg ? (
        <p className="text-xs text-destructive">{errorMsg}</p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        {canEditDiscoveryTask(task.status) ? (
          <Button variant="outline" size="xs" onClick={() => onEdit(task)}>
            Edit
          </Button>
        ) : null}
        {canCompleteDiscoveryTask(task.status) ? (
          <Button
            variant="outline"
            size="xs"
            onClick={() => completeMutation.mutate()}
            disabled={completeMutation.isPending}
          >
            Complete
          </Button>
        ) : null}
        {canEditDiscoveryTask(task.status) ? (
          <Button
            variant="outline"
            size="xs"
            onClick={() => closeMutation.mutate()}
            disabled={closeMutation.isPending}
          >
            Close
          </Button>
        ) : null}
        {canSupersedeDiscoveryTask(task.status) ? (
          <Button
            variant="outline"
            size="xs"
            onClick={() => setSupersedeOpen((value) => !value)}
          >
            Supersede
          </Button>
        ) : null}
      </div>

      {supersedeOpen ? (
        <div className="space-y-2 rounded border border-[var(--relay-row-border)] bg-[var(--relay-page-body-bg)] p-3">
          <p className={labelClass}>Superseded by task ID</p>
          <Input
            value={supersededBy}
            onChange={(e) => setSupersededBy(e.target.value)}
          />
          <div className="flex items-center justify-end gap-2">
            <Button variant="ghost" size="xs" onClick={() => setSupersedeOpen(false)}>
              Cancel
            </Button>
            <Button
              size="xs"
              onClick={() => supersedeMutation.mutate()}
              disabled={supersedeMutation.isPending}
            >
              Confirm supersede
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
