// ============================================================
// STUB — Intake route redirected to Specification (Pass 4)
// ============================================================
//
// The legacy Intake stage has been replaced by the canonical Specification
// stage. This stub redirects any visitor to /runs/$runId/specification.
// The file is preserved for Pass 5 deletion; the active source has been
// removed.

import { createFileRoute, Navigate } from "@tanstack/react-router";

export const Route = createFileRoute("/runs/$runId/intake")({
  component: IntakeRedirect,
});

function IntakeRedirect() {
  const { runId } = Route.useParams();
  return (
    <Navigate
      to="/runs/$runId/specification"
      params={{ runId }}
      replace
    />
  );
}
