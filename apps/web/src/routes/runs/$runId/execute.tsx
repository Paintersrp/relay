import { createFileRoute } from "@tanstack/react-router";
import { RelayCanonicalRunWorkbench } from "@/components/relay/RelayCanonicalRunWorkbench";

export const Route = createFileRoute("/runs/$runId/execute")({
  component: ExecuteRoutePage,
});

function ExecuteRoutePage() {
  const { runId } = Route.useParams();
  return <RelayCanonicalRunWorkbench runId={runId} stage="execute" />;
}
