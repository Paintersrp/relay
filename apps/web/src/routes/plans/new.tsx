import { Link, createFileRoute } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayPlanSubmissionWorkbench } from "@/components/relay/RelayPlanSubmissionWorkbench";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

export const Route = createFileRoute("/plans/new")({
  component: NewPlanPage,
});

function NewPlanPage() {
  return (
    <AppPageFrame
      title="New Plan"
      description="Validate and submit a reviewed Plan of Passes JSON artifact."
      leading={
        <Button asChild variant="ghost" size="icon-sm" aria-label="Back to plans">
          <Link to="/plans">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
      }
      actions={
        <Badge variant="outline" className="rounded-sm px-2 py-0.5 text-[10px]">
          Draft
        </Badge>
      }
      bodyClassName="flex min-h-0 flex-col overflow-hidden p-0"
    >
      <RelayPlanSubmissionWorkbench />
    </AppPageFrame>
  );
}
