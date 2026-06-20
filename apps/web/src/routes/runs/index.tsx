import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { runsListQueryOptions } from "@/features/relay-runs";
import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayRunsRegistry } from "@/components/relay/RelayRunsRegistry";

export const Route = createFileRoute("/runs/")({
  component: RunsListPage,
});

function RunsListPage() {
  const { data: runs, isLoading } = useQuery(runsListQueryOptions);

  return (
    <AppPageFrame
      title="Runs"
      description="Handoff orchestration runs"
      bodyClassName="flex min-h-0 flex-col overflow-hidden p-0"
    >
      <RelayRunsRegistry runs={runs} isLoading={isLoading} />
    </AppPageFrame>
  );
}
