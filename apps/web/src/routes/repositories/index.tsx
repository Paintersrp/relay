import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { CheckCircle2, Plus } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayRepositoriesRegistry } from "@/components/relay/RelayRepositoriesRegistry";
import { RelayRepositoryRegistrationDialog } from "@/components/relay/RelayRepositoryRegistrationDialog";
import { Button } from "@/components/ui/button";
import {
  workflowRepositoryTargetsQueryOptions,
  type WorkflowRepositoryRegistrationResult,
} from "@/features/relay-projects";

export const Route = createFileRoute("/repositories/")({
  component: RepositoriesListPage,
});

export function RepositoriesListPage() {
  const repositoriesQuery = useQuery(workflowRepositoryTargetsQueryOptions());
  const [registrationOpen, setRegistrationOpen] = React.useState(false);
  const [lastRegistration, setLastRegistration] =
    React.useState<WorkflowRepositoryRegistrationResult | null>(null);

  const openRegistration = () => {
    setRegistrationOpen(true);
  };

  return (
    <AppPageFrame
      title="Repositories"
      description="Manage global local-repository registrations used by Projects, Plans, and Runs."
      actions={
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={openRegistration}
        >
          <Plus className="size-3.5" />
          Register repository
        </Button>
      }
      bodyClassName="min-h-0 overflow-y-auto p-0"
    >
      {lastRegistration ? (
        <div className="mx-auto w-full max-w-5xl px-4 pt-4 sm:px-6 sm:pt-6">
          <div
            role="status"
            aria-live="polite"
            className="flex items-start gap-2 rounded border border-emerald-500/20 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-300"
          >
            <CheckCircle2 className="mt-0.5 size-4 shrink-0" aria-hidden />
            <span>
              Repository{" "}
              <span className="font-mono">
                {lastRegistration.repository.repoTarget}
              </span>{" "}
              was {lastRegistration.outcome}.
            </span>
          </div>
        </div>
      ) : null}

      <RelayRepositoriesRegistry
        repositories={repositoriesQuery.data?.repositories}
        isLoading={repositoriesQuery.isLoading}
        error={repositoriesQuery.error}
        onRegister={openRegistration}
      />

      <RelayRepositoryRegistrationDialog
        open={registrationOpen}
        onOpenChange={setRegistrationOpen}
        onCompleted={(result) => {
          setLastRegistration(result);
          setRegistrationOpen(false);
        }}
      />
    </AppPageFrame>
  );
}
