import * as React from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { createFeatureWorkspace } from "@/features/relay-feature-workspaces";
import { workflowProjectsListQueryOptions } from "@/features/relay-projects";

const workspaceCreateSearchSchema = z.object({
  projectId: z.string().optional(),
});

export const Route = createFileRoute("/feature-workspaces/new")({
  component: NewFeatureWorkspacePage,
  validateSearch: workspaceCreateSearchSchema,
});

function NewFeatureWorkspacePage() {
  const navigate = useNavigate();
  const { projectId: contextProjectId } = Route.useSearch();
  const projects = useQuery(workflowProjectsListQueryOptions({ status: "active", limit: 100 }));
  const [projectId, setProjectId] = React.useState(contextProjectId ?? "");
  const [featureSlug, setFeatureSlug] = React.useState("");

  React.useEffect(() => {
    if (contextProjectId) setProjectId(contextProjectId);
  }, [contextProjectId]);

  const contextProject = projects.data?.projects.find((project) => project.projectId === contextProjectId);
  const contextProjectMissing = Boolean(
    contextProjectId && !projects.isLoading && !projects.error && !contextProject,
  );

  const mutation = useMutation({
    mutationFn: () => createFeatureWorkspace({ projectId, featureSlug }),
    onSuccess: (workspace) =>
      void navigate({ to: "/feature-workspaces/$workspaceId", params: { workspaceId: workspace.workspaceId } }),
  });

  return (
    <section data-testid="route-scroll-region" className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      <main className="mx-auto max-w-xl p-6">
        <h1 className="text-xl font-semibold">Create feature workspace</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Create bounded operator state for discovery and governing authority. This does not create a Delivery
          Ticket or package.
        </p>
        {contextProjectId && contextProject ? (
          <div className="mt-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 text-sm">
            Creating for project: <span className="font-medium">{contextProject.name}</span>
          </div>
        ) : null}
        {contextProjectMissing ? (
          <p role="alert" className="mt-4 text-sm text-destructive">
            The requested project could not be found or is not eligible. Select a project below.
          </p>
        ) : null}
        <form
          className="mt-6 space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            mutation.mutate();
          }}
        >
          <div>
            <Label htmlFor="workspace-project">Project</Label>
            <select
              id="workspace-project"
              className="mt-1 w-full rounded border bg-background p-2"
              value={projectId}
              onChange={(event) => setProjectId(event.target.value)}
              required
            >
              <option value="">Select a Project</option>
              {projects.data?.projects.map((project) => (
                <option key={project.projectId} value={project.projectId}>
                  {project.name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <Label htmlFor="workspace-feature">Feature slug</Label>
            <Input id="workspace-feature" value={featureSlug} onChange={(event) => setFeatureSlug(event.target.value)} required />
          </div>
          {mutation.error ? (
            <p role="alert" className="text-sm text-destructive">
              {mutation.error.message}
            </p>
          ) : null}
          <Button type="submit" disabled={mutation.isPending || projects.isLoading}>
            Create workspace
          </Button>
        </form>
      </main>
    </section>
  );
}
