import { FolderGit2, Plus } from "lucide-react";

import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { WorkflowRepositoryTarget } from "@/features/relay-projects";

interface RelayRepositoriesRegistryProps {
  repositories?: WorkflowRepositoryTarget[];
  isLoading: boolean;
  error: unknown;
  onRegister: () => void;
}

export function RelayRepositoriesRegistry({
  repositories,
  isLoading,
  error,
  onRegister,
}: RelayRepositoriesRegistryProps) {
  if (isLoading) {
    return (
      <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4 sm:p-6">
        <Skeleton className="h-20 w-full rounded" />
        <Skeleton className="h-20 w-full rounded" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="mx-auto w-full max-w-5xl p-4 sm:p-6">
        <RelayStateSurface
          tone="danger"
          title="Repositories failed to load"
          description="Relay could not load the global repository registry. Check the API process and try again."
        />
      </div>
    );
  }

  if (!repositories || repositories.length === 0) {
    return (
      <div className="mx-auto w-full max-w-5xl p-4 sm:p-6">
        <RelayStateSurface
          tone="empty"
          title="No repositories registered"
          description="Register a local Git worktree before attaching it to a Project or using it in workflow artifacts."
          action={
            <Button type="button" size="sm" onClick={onRegister}>
              <Plus className="size-3.5" />
              Register repository
            </Button>
          }
        />
      </div>
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4 sm:p-6">
      {repositories.map((repository) => (
        <article
          key={repository.repoTarget}
          className="flex flex-col gap-3 border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 sm:flex-row sm:items-center sm:justify-between"
        >
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <FolderGit2 className="size-4 text-muted-foreground" aria-hidden />
              <h2 className="font-mono text-sm font-semibold text-foreground">
                {repository.repoTarget}
              </h2>
            </div>
            <p className="mt-1 break-all font-mono text-xs text-muted-foreground">
              {repository.localPath}
            </p>
          </div>
          <p className="text-xs text-muted-foreground">
            Registered {repository.createdAt}
          </p>
        </article>
      ))}
    </div>
  );
}
