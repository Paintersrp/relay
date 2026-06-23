import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Plus,
  Edit2,
  FolderGit2,
  AlertCircle,
  ToggleLeft,
  ToggleRight,
  Info,
  Calendar,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { formatPlanDate } from "@/components/relay/relayPlanVisualState";
import { RelayProjectRepositoryForm } from "./RelayProjectRepositoryForm";
import {
  setProjectRepositoryEnabled,
  relayProjectKeys,
} from "@/features/relay-projects";
import type { RelayProject, RelayProjectRepository } from "@/features/relay-projects/types";

interface RelayProjectDetailProps {
  project: RelayProject;
}

export function RelayProjectDetail({ project }: RelayProjectDetailProps) {
  const queryClient = useQueryClient();
  const [formMode, setFormMode] = React.useState<"none" | "add" | "edit">("none");
  const [editingRepo, setEditingRepo] = React.useState<RelayProjectRepository | undefined>(undefined);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const toggleEnabledMutation = useMutation({
    mutationFn: (variables: { repoId: string; enabled: boolean }) =>
      setProjectRepositoryEnabled(project.projectId, variables.repoId, variables.enabled),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: relayProjectKeys.detail(project.projectId),
      });
      void queryClient.invalidateQueries({
        queryKey: relayProjectKeys.all,
      });
    },
    onError: (err: any) => {
      setErrorMsg(err?.message || "Failed to update repository enabled state");
    },
  });

  const handleEditClick = (repo: RelayProjectRepository) => {
    setEditingRepo(repo);
    setFormMode("edit");
  };

  const handleAddClick = () => {
    setEditingRepo(undefined);
    setFormMode("add");
  };

  const handleFormClose = () => {
    setFormMode("none");
    setEditingRepo(undefined);
  };

  const handleToggleEnable = (repoId: string, currentEnabled: boolean) => {
    toggleEnabledMutation.mutate({ repoId, enabled: !currentEnabled });
  };

  const repositories = project.repositories ?? [];

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Button asChild variant="ghost" size="sm" className="h-8 w-8 p-0">
          <Link to="/projects">
            <ArrowLeft className="h-4 w-4" />
            <span className="sr-only">Back to projects</span>
          </Link>
        </Button>
        <span className="text-sm font-medium text-muted-foreground">Back to Projects</span>
      </div>

      {errorMsg && (
        <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive flex items-start gap-2">
          <AlertCircle className="size-4 shrink-0 mt-0.5" />
          <div className="flex-1">
            <p>{errorMsg}</p>
            <Button variant="link" size="sm" className="h-auto p-0 text-destructive underline mt-1" onClick={() => setErrorMsg(null)}>
              Dismiss
            </Button>
          </div>
        </div>
      )}

      {/* Project Details Panel */}
      <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6 space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b border-[var(--relay-row-border)] pb-4">
          <div className="space-y-1">
            <div className="flex items-center gap-3">
              <h2 className="text-lg font-semibold text-foreground">{project.name}</h2>
              <Badge variant={project.status === "archived" ? "secondary" : "success"}>
                {project.status === "archived" ? "Archived" : "Active"}
              </Badge>
            </div>
            <p className="font-mono text-xs text-muted-foreground">ID: {project.projectId}</p>
          </div>

          <div className="flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground">
            <Calendar className="size-3" />
            <span>Created: {formatPlanDate(project.createdAt)}</span>
          </div>
        </div>

        {project.description && (
          <div className="space-y-1">
            <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Description</h4>
            <p className="text-sm text-foreground/90 whitespace-pre-wrap">{project.description}</p>
          </div>
        )}

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 pt-2 text-xs">
          <div className="space-y-1">
            <span className="font-semibold text-muted-foreground">Default Repository ID:</span>
            <div className="font-mono text-foreground flex items-center gap-1.5">
              {project.defaultRepositoryId ? (
                <>
                  <FolderGit2 className="size-3.5 text-muted-foreground" />
                  {project.defaultRepositoryId}
                </>
              ) : (
                <span className="text-muted-foreground/60">—</span>
              )}
            </div>
          </div>

          <div className="space-y-1">
            <span className="font-semibold text-muted-foreground">Last Updated:</span>
            <div className="text-foreground">{formatPlanDate(project.updatedAt)}</div>
          </div>
        </div>
      </div>

      {/* Info notice about repository validation limitation */}
      <div className="rounded border border-info/20 bg-info/10 p-3.5 text-xs text-info flex items-start gap-2">
        <Info className="size-4 shrink-0 mt-0.5 text-info" />
        <p className="leading-normal">
          Repository readiness is shown from saved configuration only; validation/refresh is not available in this pass.
        </p>
      </div>

      {/* Repositories section */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-foreground">
            Registered Repositories ({repositories.length})
          </h3>
          {formMode === "none" && (
            <Button size="sm" onClick={handleAddClick} className="gap-1">
              <Plus className="size-3.5" />
              Add Repository
            </Button>
          )}
        </div>

        {formMode !== "none" && (
          <RelayProjectRepositoryForm
            projectId={project.projectId}
            repository={editingRepo}
            onSuccess={handleFormClose}
            onCancel={handleFormClose}
          />
        )}

        {repositories.length === 0 ? (
          <div className="rounded border border-dashed border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]/40 p-8 text-center">
            <FolderGit2 className="size-8 mx-auto text-muted-foreground/50 mb-2" />
            <p className="text-sm font-medium text-muted-foreground">No repositories registered yet.</p>
            <p className="text-xs text-muted-foreground/75 mt-1">
              Add a repository context to associate directories and branches with this project.
            </p>
            {formMode === "none" && (
              <Button size="sm" variant="outline" className="mt-4" onClick={handleAddClick}>
                <Plus className="size-3.5 mr-1" /> Add Repository
              </Button>
            )}
          </div>
        ) : (
          <div className="space-y-4">
            {/* Desktop Table View */}
            <div className="hidden border border-[var(--relay-row-border)] rounded overflow-hidden lg:block bg-[var(--relay-panel-bg)]">
              <table className="w-full text-left border-collapse table-fixed text-xs">
                <thead>
                  <tr className="border-b border-[var(--relay-row-border)] bg-muted/30 text-muted-foreground font-medium">
                    <th className="p-3 w-[15%]">Repo ID & Role</th>
                    <th className="p-3 w-[25%]">Local Filesystem Path</th>
                    <th className="p-3 w-[25%]">Remote URL & Label</th>
                    <th className="p-3 w-[12%]">Branch & Size</th>
                    <th className="p-3 w-[13%]">Policy Rules</th>
                    <th className="p-3 w-[10%] text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {repositories.map((repo) => (
                    <tr
                      key={repo.repoId}
                      className={cn(
                        "border-b border-[var(--relay-row-border)] last:border-none",
                        !repo.enabled && "opacity-60"
                      )}
                    >
                      <td className="p-3 align-top space-y-1">
                        <div className="font-semibold font-mono break-all">{repo.repoId}</div>
                        <Badge variant="outline" className="text-[10px] rounded px-1 py-0.5 uppercase tracking-wider">
                          {repo.role}
                        </Badge>
                      </td>

                      <td className="p-3 align-top font-mono break-all text-[11px]">
                        {repo.localPath}
                      </td>

                      <td className="p-3 align-top space-y-1 font-mono text-[11px] break-all">
                        {repo.remoteUrl ? (
                          <div className="text-foreground">{repo.remoteUrl}</div>
                        ) : (
                          <span className="text-muted-foreground/60">—</span>
                        )}
                        {repo.remoteLabel && (
                          <div className="text-[10px] text-muted-foreground">Label: {repo.remoteLabel}</div>
                        )}
                      </td>

                      <td className="p-3 align-top space-y-1">
                        <div className="font-mono">{repo.defaultBranch}</div>
                        <div className="text-[10px] text-muted-foreground">
                          Max: {(repo.maxFileSizeBytes / 1024).toFixed(0)} KB
                        </div>
                      </td>

                      <td className="p-3 align-top space-y-1">
                        <div className="text-[10px] text-muted-foreground">
                          Untracked: <span className="font-medium text-foreground">{repo.includeUntracked ? "Yes" : "No"}</span>
                        </div>
                        {repo.allowedRoots.length > 0 && (
                          <div className="text-[10px] text-muted-foreground truncate" title={repo.allowedRoots.join(", ")}>
                            Roots: {repo.allowedRoots.length} rules
                          </div>
                        )}
                        {repo.ignoredGlobs.length > 0 && (
                          <div className="text-[10px] text-muted-foreground truncate" title={repo.ignoredGlobs.join(", ")}>
                            Ignore: {repo.ignoredGlobs.length} rules
                          </div>
                        )}
                      </td>

                      <td className="p-3 align-top text-right space-y-2">
                        <div className="flex flex-col items-end gap-1.5">
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 px-2 text-xs gap-1"
                            onClick={() => handleEditClick(repo)}
                          >
                            <Edit2 className="size-3" />
                            Edit
                          </Button>

                          <Button
                            variant="ghost"
                            size="sm"
                            className={cn(
                              "h-7 px-2 text-xs gap-1.5 font-normal",
                              repo.enabled ? "text-success" : "text-muted-foreground"
                            )}
                            onClick={() => handleToggleEnable(repo.repoId, repo.enabled)}
                            disabled={toggleEnabledMutation.isPending}
                          >
                            {repo.enabled ? (
                              <>
                                <ToggleRight className="size-4 text-success" />
                                Enabled
                              </>
                            ) : (
                              <>
                                <ToggleLeft className="size-4 text-muted-foreground" />
                                Disabled
                              </>
                            )}
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {/* Mobile Card List View */}
            <div className="grid grid-cols-1 gap-4 lg:hidden">
              {repositories.map((repo) => (
                <div
                  key={repo.repoId}
                  className={cn(
                    "rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 space-y-3",
                    !repo.enabled && "opacity-60"
                  )}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <span className="font-semibold font-mono text-sm">{repo.repoId}</span>
                      <div className="mt-1 flex items-center gap-1.5">
                        <Badge variant="outline" className="text-[9px] rounded-none py-0 px-1 uppercase">
                          {repo.role}
                        </Badge>
                        <Badge variant={repo.enabled ? "success" : "secondary"} className="text-[9px] rounded-none py-0 px-1">
                          {repo.enabled ? "Enabled" : "Disabled"}
                        </Badge>
                      </div>
                    </div>

                    <div className="flex items-center gap-1">
                      <Button variant="ghost" size="sm" className="h-8 w-8 p-0" onClick={() => handleEditClick(repo)}>
                        <Edit2 className="size-3.5" />
                        <span className="sr-only">Edit repository</span>
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-8 w-8 p-0"
                        onClick={() => handleToggleEnable(repo.repoId, repo.enabled)}
                        disabled={toggleEnabledMutation.isPending}
                      >
                        {repo.enabled ? (
                          <ToggleRight className="size-4 text-success" />
                        ) : (
                          <ToggleLeft className="size-4 text-muted-foreground" />
                        )}
                        <span className="sr-only">Toggle enabled</span>
                      </Button>
                    </div>
                  </div>

                  <div className="space-y-2 text-xs border-t border-[var(--relay-row-border)] pt-2.5">
                    <div>
                      <span className="font-semibold text-muted-foreground block text-[10px] uppercase">
                        Local Path:
                      </span>
                      <span className="font-mono break-all text-[11px] block mt-0.5">{repo.localPath}</span>
                    </div>

                    {repo.remoteUrl && (
                      <div>
                        <span className="font-semibold text-muted-foreground block text-[10px] uppercase">
                          Remote URL:
                        </span>
                        <span className="font-mono break-all text-[11px] block mt-0.5">{repo.remoteUrl}</span>
                      </div>
                    )}

                    <div className="grid grid-cols-2 gap-2">
                      <div>
                        <span className="font-semibold text-muted-foreground block text-[10px] uppercase">
                          Branch:
                        </span>
                        <span className="font-mono">{repo.defaultBranch}</span>
                      </div>
                      <div>
                        <span className="font-semibold text-muted-foreground block text-[10px] uppercase">
                          Max Size limit:
                        </span>
                        <span>{(repo.maxFileSizeBytes / 1024).toFixed(0)} KB</span>
                      </div>
                    </div>

                    <div className="grid grid-cols-2 gap-2">
                      <div>
                        <span className="font-semibold text-muted-foreground block text-[10px] uppercase">
                          Untracked:
                        </span>
                        <span>{repo.includeUntracked ? "Included" : "Excluded"}</span>
                      </div>
                      <div>
                        <span className="font-semibold text-muted-foreground block text-[10px] uppercase">
                          Filters:
                        </span>
                        <span>
                          {repo.allowedRoots.length} allowed, {repo.ignoredGlobs.length} ignored
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
