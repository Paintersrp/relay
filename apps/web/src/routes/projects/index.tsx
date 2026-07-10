import { useQuery } from "@tanstack/react-query";
import { Link, createFileRoute } from "@tanstack/react-router";
import { Plus } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayProjectsRegistry } from "@/components/relay/RelayProjectsRegistry";
import { Button } from "@/components/ui/button";
import { workflowProjectsListQueryOptions } from "@/features/relay-projects";

export const Route = createFileRoute("/projects/")({
  component: ProjectsListPage,
});

function ProjectsListPage() {
  const projectsQuery = useQuery(workflowProjectsListQueryOptions({ limit: 100 }));

  return (
    <AppPageFrame
      title="Projects"
      description="Organize Plans, repository references, and Project Notes."
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to="/projects/new">
            <Plus className="size-3.5" />
            New Project
          </Link>
        </Button>
      }
      bodyClassName="flex min-h-0 flex-col overflow-hidden p-0"
    >
      <RelayProjectsRegistry
        projects={projectsQuery.data?.projects}
        isLoading={projectsQuery.isLoading}
        error={projectsQuery.error}
      />
    </AppPageFrame>
  );
}
