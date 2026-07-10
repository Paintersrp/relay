import { Link, createFileRoute } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayRunSubmissionWorkbench } from "@/components/relay/RelayRunSubmissionWorkbench";
import { Button } from "@/components/ui/button";

interface NewRunSearch {
  planId?: string;
  passId?: string;
  passNumber?: number;
  remediatesRunId?: string;
}

function optionalSearchString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

export const Route = createFileRoute("/runs/new")({
  validateSearch: (search: Record<string, unknown>): NewRunSearch => ({
    planId: optionalSearchString(search.planId),
    passId: optionalSearchString(search.passId),
    passNumber:
      typeof search.passNumber === "number" &&
      Number.isInteger(search.passNumber) &&
      search.passNumber > 0
        ? search.passNumber
        : typeof search.passNumber === "string" &&
            /^[1-9][0-9]*$/.test(search.passNumber)
          ? Number(search.passNumber)
          : undefined,
    remediatesRunId: optionalSearchString(search.remediatesRunId),
  }),
  component: NewRunPage,
});

function NewRunPage() {
  const search = Route.useSearch();
  return (
    <AppPageFrame
      title="New Run"
      description="Create a Managed, Standalone, or remediation Run from canonical Execution Spec JSON."
      leading={
        <Button asChild variant="ghost" size="icon-sm" aria-label="Back to Runs">
          <Link to="/runs">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
      }
      bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden p-0"
    >
      <RelayRunSubmissionWorkbench
        planId={search.planId}
        passId={search.passId}
        passNumber={search.passNumber}
        remediatesRunId={search.remediatesRunId}
      />
    </AppPageFrame>
  );
}
