import { createFileRoute } from "@tanstack/react-router";
import { RelayCanonicalRunWorkbench } from "@/components/relay/RelayCanonicalRunWorkbench";

export const Route = createFileRoute("/runs/$runId/audit")({
  component: AuditRoutePage,
});

function AuditRoutePage() {
  const { runId } = Route.useParams();
  return <RelayCanonicalRunWorkbench runId={runId} stage="audit" />;
}
