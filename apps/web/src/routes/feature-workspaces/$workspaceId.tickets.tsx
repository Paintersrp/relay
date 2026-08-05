import { createFileRoute } from "@tanstack/react-router";
import { RelayTicketFrontier } from "@/components/relay/RelayTicketFrontier";

export const Route = createFileRoute("/feature-workspaces/$workspaceId/tickets")({ component: TicketFrontierPage });

function TicketFrontierPage() {
  const { workspaceId } = Route.useParams();
  return (
    <section data-testid="route-scroll-region" className="min-h-0 flex-1 overflow-y-auto bg-[var(--relay-page-body-bg)]">
      <div className="mx-auto w-full max-w-5xl p-6">
        <RelayTicketFrontier workspaceId={workspaceId} />
      </div>
    </section>
  );
}
