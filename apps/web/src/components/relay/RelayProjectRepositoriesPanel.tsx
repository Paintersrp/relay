import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderGit2, Loader2, Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  attachWorkflowProjectRepository,
  detachWorkflowProjectRepository,
  workflowProjectKeys,
  workflowRepositoryTargetsQueryOptions,
  type WorkflowProjectRepositoryReference,
} from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

interface RelayProjectRepositoriesPanelProps {
  projectId: string;
  repositories: WorkflowProjectRepositoryReference[];
}

function repositoryErrorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  if (error instanceof Error) return error.message;
  return "Repository reference update failed.";
}

export function RelayProjectRepositoriesPanel({
  projectId,
  repositories,
}: RelayProjectRepositoriesPanelProps) {
  const queryClient = useQueryClient();
  const repositoryTargetsQuery = useQuery(workflowRepositoryTargetsQueryOptions());
  const [selectedTarget, setSelectedTarget] = React.useState("");
  const [detachTarget, setDetachTarget] = React.useState<string | null>(null);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  const invalidateProject = React.useCallback(() => {
    void queryClient.invalidateQueries({
      queryKey: workflowProjectKeys.details(),
    });
  }, [queryClient]);

  const attachMutation = useMutation({
    mutationFn: (repoTarget: string) =>
      attachWorkflowProjectRepository(projectId, repoTarget),
    onSuccess: () => {
      setSelectedTarget("");
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(repositoryErrorMessage(error)),
  });

  const detachMutation = useMutation({
    mutationFn: (repoTarget: string) =>
      detachWorkflowProjectRepository(projectId, repoTarget),
    onSuccess: () => {
      setDetachTarget(null);
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(repositoryErrorMessage(error)),
  });

  const attachedTargets = React.useMemo(
    () => new Set(repositories.map((repository) => repository.repoTarget.toLowerCase())),
    [repositories],
  );
  const globalRepositories = repositoryTargetsQuery.data?.repositories ?? [];
  const availableRepositories = globalRepositories.filter(
    (repository) => !attachedTargets.has(repository.repoTarget.toLowerCase()),
  );
  const repositoryByTarget = React.useMemo(
    () => new Map(globalRepositories.map((repository) => [repository.repoTarget, repository])),
    [globalRepositories],
  );

  return (
    <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-5 py-3">
        <div>
          <h2 className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
            Repository References
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Attach existing global repository targets without copying configuration.
          </p>
        </div>
        <span className="font-mono text-[10px] text-muted-foreground">
          {repositories.length} attached
        </span>
      </div>

      <div className="space-y-4 p-5">
        {errorMessage ? (
          <div
            role="alert"
            className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
          >
            {errorMessage}
          </div>
        ) : null}

        <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div className="min-w-0 flex-1 space-y-1.5">
            <label
              htmlFor="project-repository-target"
              className="text-xs font-medium text-foreground"
            >
              Global repository target
            </label>
            <Select
              value={selectedTarget}
              onValueChange={setSelectedTarget}
              disabled={repositoryTargetsQuery.isLoading || availableRepositories.length === 0}
            >
              <SelectTrigger id="project-repository-target" className="w-full">
                <SelectValue
                  placeholder={
                    repositoryTargetsQuery.isLoading
                      ? "Loading repository targets..."
                      : availableRepositories.length === 0
                        ? "No unattached repository targets"
                        : "Select a repository target"
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {availableRepositories.map((repository) => (
                  <SelectItem key={repository.repoTarget} value={repository.repoTarget}>
                    {repository.repoTarget}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button
            type="button"
            size="sm"
            disabled={!selectedTarget || attachMutation.isPending}
            onClick={() => attachMutation.mutate(selectedTarget)}
          >
            {attachMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Plus className="size-3.5" />
            )}
            Attach
          </Button>
        </div>

        {repositoryTargetsQuery.isError ? (
          <p role="alert" className="text-sm text-destructive">
            Global repository targets could not be loaded.
          </p>
        ) : null}

        {repositories.length === 0 ? (
          <div className="rounded border border-dashed border-[var(--relay-row-border)] p-6 text-center">
            <FolderGit2 className="mx-auto size-7 text-muted-foreground/60" />
            <p className="mt-2 text-sm font-medium text-foreground">
              No repository targets attached
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              Attach a global target to make the Project easier to organize.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-[var(--relay-row-border)] border border-[var(--relay-row-border)]">
            {repositories.map((reference) => {
              const repository = repositoryByTarget.get(reference.repoTarget);
              return (
                <div
                  key={reference.repoTarget}
                  className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <FolderGit2 className="size-4 text-muted-foreground" />
                      <span className="font-mono text-sm text-foreground">
                        {reference.repoTarget}
                      </span>
                    </div>
                    <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">
                      {repository?.localPath || "Global repository details unavailable"}
                    </p>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="text-destructive hover:bg-destructive/10"
                    onClick={() => {
                      setErrorMessage(null);
                      setDetachTarget(reference.repoTarget);
                    }}
                  >
                    <Trash2 className="size-3.5" />
                    Detach
                  </Button>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <Dialog
        open={detachTarget !== null}
        onOpenChange={(open) => {
          if (!open && !detachMutation.isPending) setDetachTarget(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Detach repository target?</DialogTitle>
            <DialogDescription>
              This removes only the Project reference. The global repository target and its configuration remain unchanged.
            </DialogDescription>
          </DialogHeader>
          {errorMessage ? (
            <div
              role="alert"
              aria-live="assertive"
              className="mt-4 rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {errorMessage}
            </div>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={detachMutation.isPending}
              onClick={() => setDetachTarget(null)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={!detachTarget || detachMutation.isPending}
              onClick={() => {
                if (detachTarget) detachMutation.mutate(detachTarget);
              }}
            >
              {detachMutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Trash2 className="size-3.5" />
              )}
              Detach
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
