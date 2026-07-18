import { createFileRoute } from "@tanstack/react-router";
import { RelayTicketFrontier } from "@/components/relay/RelayTicketFrontier";

export const Route = createFileRoute("/feature-workspaces/$workspaceId/tickets")({ component: TicketFrontierPage });

function TicketFrontierPage() {
  const { workspaceId } = Route.useParams();
  return <div className="mx-auto w-full max-w-5xl p-6"><RelayTicketFrontier workspaceId={workspaceId} /></div>;
}
