import { createFileRoute } from "@tanstack/react-router";

import { RelayRefactorBacklogPage } from "@/components/relay/RelayRefactorBacklogPage";

interface RefactorBacklogSearch {
  planId?: string;
  candidateId?: string;
}

export const Route = createFileRoute("/projects/$projectId/refactor-backlog")({
  validateSearch: (search: Record<string, unknown>): RefactorBacklogSearch => ({
    planId:
      typeof search.planId === "string" && search.planId.trim().length > 0
        ? search.planId
        : undefined,
    candidateId:
      typeof search.candidateId === "string" && search.candidateId.trim().length > 0
        ? search.candidateId
        : undefined,
  }),
  component: RefactorBacklogRoute,
});

function RefactorBacklogRoute() {
  const { projectId } = Route.useParams();
  const { planId, candidateId } = Route.useSearch();

  return (
    <section className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      <RelayRefactorBacklogPage
        projectId={projectId}
        planId={planId}
        candidateId={candidateId}
      />
    </section>
  );
}
