import { createFileRoute } from "@tanstack/react-router";
import { RelayCanonicalRunWorkbench } from "@/components/relay/RelayCanonicalRunWorkbench";

export const Route = createFileRoute("/runs/$runId/specification")({
  component: SpecificationRoutePage,
});

function SpecificationRoutePage() {
  const { runId } = Route.useParams();
  return <RelayCanonicalRunWorkbench runId={runId} stage="specification" />;
}
