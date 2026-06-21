import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Plus } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayPlansRegistry } from "@/components/relay/RelayPlansRegistry";
import { Button } from "@/components/ui/button";
import { plansListQueryOptions } from "@/features/relay-plans";

export const Route = createFileRoute("/plans/")({
  component: PlansListPage,
});

function PlansListPage() {
  const { data, isLoading, error } = useQuery(plansListQueryOptions({ limit: 100 }));

  return (
    <AppPageFrame
      title="Plans"
      description="Managed multi-pass orchestration plans"
      actions={
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled
          title="Plan submission arrives in UI-PLAN-03"
        >
          <Plus className="size-3.5" />
          New Plan
        </Button>
      }
      bodyClassName="flex min-h-0 flex-col overflow-hidden p-0"
    >
      <RelayPlansRegistry plans={data?.plans} isLoading={isLoading} error={error} />
    </AppPageFrame>
  );
}
