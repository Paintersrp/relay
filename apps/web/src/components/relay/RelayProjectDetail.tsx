import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Archive, ArrowLeft, Edit2, Loader2, RotateCcw } from "lucide-react";

import { RelayProjectNotesPanel } from "@/components/relay/RelayProjectNotesPanel";
import { RelayProjectPlansPanel } from "@/components/relay/RelayProjectPlansPanel";
import { RelayProjectRepositoriesPanel } from "@/components/relay/RelayProjectRepositoriesPanel";
import { formatPlanDate } from "@/components/relay/relayPlanVisualState";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  archiveWorkflowProject,
  restoreWorkflowProject,
  updateWorkflowProject,
  workflowProjectKeys,
  type WorkflowProjectDetail,
} from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

interface RelayProjectDetailProps {
  detail: WorkflowProjectDetail;
}

function projectErrorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  if (error instanceof Error) return error.message;
  return "Project update failed.";
}

export function RelayProjectDetail({ detail }: RelayProjectDetailProps) {
  const queryClient = useQueryClient();
  const project = detail.project;
  const [editOpen, setEditOpen] = React.useState(false);
  const [archiveOpen, setArchiveOpen] = React.useState(false);
  const [name, setName] = React.useState(project.name);
  const [description, setDescription] = React.useState(project.description);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (!editOpen) {
      setName(project.name);
      setDescription(project.description);
    }
  }, [editOpen, project.description, project.name]);

  const invalidateProject = React.useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: workflowProjectKeys.all });
  }, [queryClient]);

  const updateMutation = useMutation({
    mutationFn: () => updateWorkflowProject(project.projectId, {
      name: name.trim(),
      description: description.trim(),
    }),
    onSuccess: () => {
      setEditOpen(false);
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(projectErrorMessage(error)),
  });

  const lifecycleMutation = useMutation({
    mutationFn: () => project.status === "active"
      ? archiveWorkflowProject(project.projectId)
      : restoreWorkflowProject(project.projectId),
    onSuccess: () => {
      setArchiveOpen(false);
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(projectErrorMessage(error)),
  });

  const submitEdit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setErrorMessage(null);
    updateMutation.mutate();
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Button asChild variant="ghost" size="sm" className="h-8 w-8 p-0">
          <Link to="/projects">
            <ArrowLeft className="size-4" />
            <span className="sr-only">Back to Projects</span>
          </Link>
        </Button>
        <span className="text-sm font-medium text-muted-foreground">Back to Projects</span>
      </div>

      {errorMessage ? (
        <div
          role="alert"
          className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
        >
          {errorMessage}
        </div>
      ) : null}

      <section className="space-y-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
        <div className="flex flex-col gap-4 border-b border-[var(--relay-row-border)] pb-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-lg font-semibold text-foreground">{project.name}</h1>
              <Badge variant={project.status === "archived" ? "secondary" : "success"}>
                {project.status === "archived" ? "Archived" : "Active"}
              </Badge>
            </div>
            <p className="break-all font-mono text-xs text-muted-foreground">
              {project.projectId}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                setErrorMessage(null);
                setEditOpen(true);
              }}
            >
              <Edit2 className="size-3.5" />
              Edit Project
            </Button>
            {project.status === "active" ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setErrorMessage(null);
                  setArchiveOpen(true);
                }}
              >
                <Archive className="size-3.5" />
                Archive Project
              </Button>
            ) : (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={lifecycleMutation.isPending}
                onClick={() => lifecycleMutation.mutate()}
              >
                {lifecycleMutation.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <RotateCcw className="size-3.5" />
                )}
                Restore Project
              </Button>
            )}
          </div>
        </div>

        <div className="space-y-1">
          <h2 className="text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
            Description
          </h2>
          <p className="whitespace-pre-wrap text-sm leading-relaxed text-foreground/90">
            {project.description || "No description provided."}
          </p>
        </div>

        <dl className="grid gap-4 border-t border-[var(--relay-row-border)] pt-4 text-xs sm:grid-cols-2">
          <div>
            <dt className="font-semibold text-muted-foreground">Created</dt>
            <dd className="mt-1 text-foreground">{formatPlanDate(project.createdAt)}</dd>
          </div>
          <div>
            <dt className="font-semibold text-muted-foreground">Last Updated</dt>
            <dd className="mt-1 text-foreground">{formatPlanDate(project.updatedAt)}</dd>
          </div>
        </dl>
      </section>

      <RelayProjectPlansPanel project={project} plans={detail.plans} />
      <RelayProjectRepositoriesPanel
        projectId={project.projectId}
        repositories={detail.repositories}
      />
      <RelayProjectNotesPanel projectId={project.projectId} notes={detail.notes} />

      <Dialog
        open={editOpen}
        onOpenChange={(open) => {
          if (!updateMutation.isPending) setEditOpen(open);
        }}
      >
        <DialogContent>
          <form onSubmit={submitEdit}>
            <DialogHeader>
              <DialogTitle>Edit Project</DialogTitle>
              <DialogDescription>
                Update the organizational name and description. Canonical artifacts and attached work are not modified.
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
            <div className="space-y-4 py-4">
              <div className="space-y-1.5">
                <Label htmlFor="edit-project-name">Project Name</Label>
                <Input
                  id="edit-project-name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  required
                  disabled={updateMutation.isPending}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-project-description">Description</Label>
                <Textarea
                  id="edit-project-description"
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  rows={5}
                  disabled={updateMutation.isPending}
                />
              </div>
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={updateMutation.isPending}
                onClick={() => setEditOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={updateMutation.isPending || name.trim().length === 0}
              >
                {updateMutation.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : null}
                Save Project
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={archiveOpen}
        onOpenChange={(open) => {
          if (!lifecycleMutation.isPending) setArchiveOpen(open);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Archive Project?</DialogTitle>
            <DialogDescription>
              Existing Plans and Runs remain available, but this Project cannot receive newly submitted or moved Plans until restored.
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
              disabled={lifecycleMutation.isPending}
              onClick={() => setArchiveOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={lifecycleMutation.isPending}
              onClick={() => lifecycleMutation.mutate()}
            >
              {lifecycleMutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Archive className="size-3.5" />
              )}
              Archive Project
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <span className="sr-only" aria-live="polite">
        Project {project.name} is {project.status}.
      </span>
    </div>
  );
}
