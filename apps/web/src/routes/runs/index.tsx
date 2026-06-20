import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { runsListQueryOptions } from "@/features/relay-runs";
import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayRunsRegistry } from "@/components/relay/RelayRunsRegistry";
import { Button } from "@/components/ui/button";
import { PlusCircle } from "lucide-react";

export const Route = createFileRoute("/runs/")({
  component: RunsListPage,
});

function RunsListPage() {
  const { data: runs, isLoading } = useQuery(runsListQueryOptions);

  return (
    <AppPageFrame
      title="Runs"
      description="Handoff orchestration runs"
      actions={
        <Button size="sm" asChild className="gap-1.5">
          <Link to="/runs/new">
            <PlusCircle className="w-4 h-4" />
            New Run
          </Link>
        </Button>
      }
      bodyClassName="flex flex-col gap-3"
    >
      <RelayRunsRegistry runs={runs} isLoading={isLoading} />
    </AppPageFrame>
  );
}
