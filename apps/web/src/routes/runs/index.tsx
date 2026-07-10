import { useQuery } from "@tanstack/react-query";
import { Link, createFileRoute } from "@tanstack/react-router";
import { Plus } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayCanonicalRunsRegistry } from "@/components/relay/RelayCanonicalRunsRegistry";
import { Button } from "@/components/ui/button";
import { workflowRunsListQueryOptions } from "@/features/relay-runs";

export const Route = createFileRoute("/runs/")({
  component: RunsListPage,
});

function RunsListPage() {
  const query = useQuery(workflowRunsListQueryOptions({ limit: 100 }));
  return (
    <AppPageFrame
      title="Runs"
      description="Canonical managed, standalone, and remediation executions"
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to="/runs/new"><Plus className="size-3.5" /> New Run</Link>
        </Button>
      }
      bodyClassName="flex min-h-0 flex-col overflow-hidden p-0"
    >
      <RelayCanonicalRunsRegistry
        runs={query.data?.runs}
        isLoading={query.isLoading}
        error={query.error}
      />
    </AppPageFrame>
  );
}
